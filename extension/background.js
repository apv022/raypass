// Raypass background: the only part of the extension that talks to the native host. It keeps
// no secret beyond the moment it passes one on: credentials go from the host straight to the
// frame that asked for them, and a submitted login waits (in memory and the browser's
// memory-only session storage, never on disk) only until the person answers the save bar.
//
// Rules this file enforces (design/DESIGN.md "Browser extension"):
//   - the URL given to raypass.match / raypass.fill / raypass.save_login is the URL the browser
//     reports for the sending frame (sender.url) or tab, never one from a message;
//   - only the top frame, or a frame of the same registrable domain as the top frame, is filled;
//   - content scripts can only ask for their own frame's matches and fills; every other action
//     is for the extension's own pages, and only the allowlisted ones;
//   - a login saved for https is filled into a plain http page only after the person confirmed
//     the warning (allow_insecure), in the page's own Raypass panel or in the popup;
//   - nothing calls the vault on its own (no badge counts on navigation): every vault call is
//     something the person did, so Raypass's auto-lock keeps working.
import './lib/urlmatch.js';
import './lib/messages.js';
import './lib/native.js';

const { url: U, msg: M, NativeClient, RaypassError, isLocked } = globalThis.Raypass;

const HOST = 'app.raypass.native';
const SAVE_TTL = 5 * 60 * 1000;
const USER_TTL = 10 * 60 * 1000;
const EXT_ORIGIN = chrome.runtime.getURL('');

const native = new NativeClient(() => chrome.runtime.connectNative(HOST), {
  lastError: (port) => chrome.runtime.lastError || port.error || null,
});

// ---- status cache ---------------------------------------------------------------------------

/** The last raypass.status we saw, adjusted by lock/unlock answers. Not a secret. */
let status = null;

async function call(action, args = {}) {
  try {
    const res = await native.call(action, args);
    if (action === 'raypass.status') status = res;
    if (action === 'raypass.unlock' && status) status = { ...status, locked: false };
    if (action === 'raypass.lock' && status) status = { ...status, locked: true };
    return res;
  } catch (e) {
    if (isLocked(e) && status) status = { ...status, locked: true };
    throw e;
  }
}

function errorOf(e) {
  return { code: e?.code || 'internal', message: String(e?.message || e || 'failed') };
}

// ---- frames ---------------------------------------------------------------------------------

/** tabId -> Map(frameId -> {url, login, focusedAt}); what content scripts reported. */
const frames = new Map();

function noteFrame(sender, m) {
  const tab = sender.tab.id;
  if (!frames.has(tab)) frames.set(tab, new Map());
  const f = frames.get(tab);
  const prev = f.get(sender.frameId) || {};
  f.set(sender.frameId, { url: sender.url, login: m.login, focusedAt: m.focused ? Date.now() : prev.focusedAt || 0 });
}

/** Whether the frame that sent a message may be filled (top frame, or same site as the top). */
function frameAllowed(sender) {
  if (!sender.tab || !U.fillable(sender.url)) return false;
  if (sender.frameId === 0) return true;
  return U.sameSite(sender.url, sender.tab.url);
}

/** The frame of tab to fill: the one with a focused login field, else the top frame. */
function pickFrame(tab) {
  const f = frames.get(tab.id);
  let best = null;
  if (f) {
    for (const [id, st] of f) {
      if (!st.login) continue;
      if (id !== 0 && !U.sameSite(st.url, tab.url)) continue;
      if (!best || st.focusedAt > best.focusedAt || (st.focusedAt === best.focusedAt && id === 0)) best = { id, ...st };
    }
  }
  return best ? { frameId: best.id, url: best.url } : { frameId: 0, url: tab.url };
}

chrome.tabs.onRemoved.addListener((tabId) => {
  frames.delete(tabId);
  dropPending(tabId);
  usernames.delete(tabId);
});

function toTab(tabId, frameId, message) {
  return chrome.tabs.sendMessage(tabId, message, { frameId }).catch(() => null);
}

// ---- fill -----------------------------------------------------------------------------------

/**
 * Fills the best match into tab (Ctrl+Shift+L, the context menu): one match fills, several
 * show the dropdown in the frame, none or locked say so there.
 */
async function fillBest(tab, frameId, frameUrl) {
  if (!tab || tab.id === undefined) return;
  let target = frameId === undefined ? pickFrame(tab) : { frameId, url: frameUrl || tab.url };
  if (target.frameId !== 0 && !U.sameSite(target.url, tab.url)) {
    await toTab(tab.id, target.frameId, { type: 'bg.toast', text: 'Raypass does not fill frames from another site.' });
    return;
  }
  if (!U.fillable(target.url)) return;
  let rows;
  try {
    rows = await call('raypass.match', { url: target.url });
  } catch (e) {
    if (isLocked(e)) {
      openPopup();
      await toTab(tab.id, target.frameId, { type: 'bg.toast', text: 'Raypass is locked. Unlock it from the toolbar.' });
    } else {
      await toTab(tab.id, target.frameId, { type: 'bg.toast', text: errorOf(e).message });
    }
    return;
  }
  if (!rows?.length) {
    await toTab(tab.id, target.frameId, { type: 'bg.toast', text: 'No Raypass logins for this site.' });
  } else if (rows.length === 1) {
    await toTab(tab.id, target.frameId, { type: 'bg.fill', id: rows[0].id });
  } else {
    await toTab(tab.id, target.frameId, { type: 'bg.showMatches' });
  }
}

function openPopup() {
  try { chrome.action.openPopup?.()?.catch?.(() => {}); } catch { /* needs a gesture in some browsers */ }
}

// ---- saving ---------------------------------------------------------------------------------

/** tabId -> {user, site, at}: the identifier typed on the first step of a multi-step login. */
const usernames = new Map();

/** tabId -> {token, url, user, pass, update, locked, at}: a login waiting for the save bar. */
const pending = new Map();

function randomToken() {
  const b = crypto.getRandomValues(new Uint8Array(16));
  return [...b].map((x) => x.toString(16).padStart(2, '0')).join('');
}

function siteOf(u) {
  const p = U.parse(u);
  return p ? (p.special ? p.host + ':' + p.port : U.registrableDomain(p.host)) : '';
}

async function setPending(tabId, p) {
  pending.set(tabId, p);
  try { await chrome.storage.session.set({ ['save:' + tabId]: p }); } catch { /* memory copy still works */ }
  chrome.action.setBadgeText({ tabId, text: '+' }).catch(() => {});
}

async function getPending(tabId) {
  let p = pending.get(tabId);
  if (!p) {
    try { p = (await chrome.storage.session.get('save:' + tabId))['save:' + tabId]; } catch { p = null; }
  }
  if (p && Date.now() - p.at > SAVE_TTL) { await dropPending(tabId); p = null; }
  return p || null;
}

async function dropPending(tabId) {
  pending.delete(tabId);
  try { await chrome.storage.session.remove('save:' + tabId); } catch { /* fine */ }
  chrome.action.setBadgeText({ tabId, text: '' }).catch(() => {});
}

/** What the bar and the popup may see of a pending save: no password. */
function summary(p) {
  return p && { token: p.token, host: U.displayHost(p.url), user: p.user, update: p.update, locked: p.locked };
}

async function onSubmitted(sender, user, pass) {
  if (!frameAllowed(sender)) return;
  const tabId = sender.tab.id;
  if (!user) {
    const u = usernames.get(tabId);
    if (u && u.site === siteOf(sender.url) && Date.now() - u.at < USER_TTL) user = u.user;
  }
  let update = false, locked = false;
  try {
    const rows = await call('raypass.match', { url: sender.url });
    const same = (rows || []).find((r) => (r.user || '') === user);
    if (same) {
      // Only compared here, never sent to the page: an http page may check an https login.
      const cur = await call('raypass.fill', { id: same.id, url: sender.url, allow_insecure: true });
      if (cur.pass === pass) return; // nothing new: no bar
      update = true;
    }
  } catch (e) {
    if (!isLocked(e)) return; // host missing etc.: nowhere to save to anyway
    locked = true;
  }
  const p = { token: randomToken(), url: sender.url, user, pass, update, locked, at: Date.now() };
  await setPending(tabId, p);
  await toTab(tabId, 0, { type: 'bg.showSave', save: summary(p) });
}

/** The person answered the save bar (or the popup): save or forget the pending login. */
async function onSaveAnswer(tabId, token, save) {
  const p = await getPending(tabId);
  if (!p || p.token !== token) throw new RaypassError('not_found', 'nothing to save');
  if (!save) { await dropPending(tabId); return { saved: false }; }
  const res = await call('raypass.save_login', { url: p.url, user: p.user, pass: p.pass });
  await dropPending(tabId);
  return { saved: true, created: !!res?.created };
}

// ---- clipboard ------------------------------------------------------------------------------

/** Clears the clipboard after the popup copied a secret (the popup itself is gone by then). */
async function armClipboard(seconds) {
  const s = seconds || status?.clip_clear || 30;
  await chrome.alarms.create('raypass-clip', { when: Date.now() + s * 1000 });
}

async function clearClipboard() {
  if (chrome.offscreen) {
    try {
      if (!(await chrome.offscreen.hasDocument?.())) {
        await chrome.offscreen.createDocument({
          url: 'offscreen/offscreen.html', reasons: ['CLIPBOARD'],
          justification: 'Clear a password Raypass copied once its time is up',
        });
      }
      await chrome.runtime.sendMessage({ type: 'offscreen.clear' });
      await chrome.offscreen.closeDocument();
    } catch { /* nothing more we can do */ }
    return;
  }
  // Firefox: the background is a page with a document.
  try {
    const onCopy = (e) => { e.clipboardData.setData('text/plain', ''); e.preventDefault(); };
    document.addEventListener('copy', onCopy, { once: true });
    document.execCommand('copy');
  } catch {
    try { await navigator.clipboard.writeText(''); } catch { /* gone */ }
  }
}

chrome.alarms.onAlarm.addListener((a) => { if (a.name === 'raypass-clip') clearClipboard(); });

// ---- messages -------------------------------------------------------------------------------

async function onContent(m, sender) {
  switch (m.type) {
    case 'cs.state':
      noteFrame(sender, m);
      return {};
    case 'cs.matches': {
      if (!frameAllowed(sender)) throw new RaypassError('forbidden', 'Raypass does not fill this frame');
      try {
        return { items: (await call('raypass.match', { url: sender.url })) || [] };
      } catch (e) {
        if (isLocked(e)) return { locked: true, items: [] };
        throw e;
      }
    }
    case 'cs.fill': {
      if (!frameAllowed(sender)) throw new RaypassError('forbidden', 'Raypass does not fill this frame');
      // allowInsecure: the person clicked "Fill anyway" on the insecure-page warning.
      const args = { id: m.id, url: sender.url };
      if (m.allowInsecure === true) args.allow_insecure = true;
      const r = await call('raypass.fill', args);
      return { user: r.user || '', pass: r.pass || '', totp: r.totp || '' };
    }
    case 'cs.username':
      if (frameAllowed(sender) && m.user) usernames.set(sender.tab.id, { user: m.user, site: siteOf(sender.url), at: Date.now() });
      return {};
    case 'cs.submitted':
      onSubmitted(sender, m.user, m.pass);
      return {};
    case 'cs.pendingSave':
      if (sender.frameId !== 0) return { save: null };
      return { save: summary(await getPending(sender.tab.id)) };
    case 'cs.saveAnswer':
      if (sender.frameId !== 0) throw new RaypassError('forbidden', 'not the top frame');
      return onSaveAnswer(sender.tab.id, m.token, m.save);
    case 'cs.openPopup':
      openPopup();
      return {};
  }
  throw new RaypassError('bad_request', 'unknown message');
}

async function onExt(m) {
  switch (m.type) {
    case 'ext.call':
      return { result: await call(m.action, m.args || {}) };
    case 'ext.status':
      return { result: await call('raypass.status') };
    case 'ext.fillTab': {
      const tab = await chrome.tabs.get(m.tabId);
      const t = pickFrame(tab);
      const fill = { type: 'bg.fill', id: m.id };
      if (m.allowInsecure === true) fill.allowInsecure = true; // confirmed in the popup
      const r = await chrome.tabs.sendMessage(tab.id, fill, { frameId: t.frameId }).catch(() => null);
      if (!r) throw new RaypassError('not_found', 'no login form on this page');
      if (r.error) throw new RaypassError(r.error.code, r.error.message);
      return r;
    }
    case 'ext.fillGenerated': {
      const tab = await chrome.tabs.get(m.tabId);
      const t = pickFrame(tab);
      if (t.frameId !== 0 && !U.sameSite(t.url, tab.url)) throw new RaypassError('forbidden', 'frame from another site');
      const r = await chrome.tabs.sendMessage(tab.id, { type: 'bg.fillPassword', password: m.password }, { frameId: t.frameId }).catch(() => null);
      if (!r?.filled) throw new RaypassError('not_found', 'no password field on this page');
      return r;
    }
    case 'ext.pendingSave':
      return { save: summary(await getPending(m.tabId)) };
    case 'ext.saveAnswer': {
      const r = await onSaveAnswer(m.tabId, m.token, m.save);
      if (r.saved) toTab(m.tabId, 0, { type: 'bg.toast', text: 'Saved to Raypass.' });
      toTab(m.tabId, 0, { type: 'bg.hideSave' });
      return r;
    }
    case 'ext.clipArm':
      await armClipboard(m.seconds);
      return {};
    case 'ext.ping': {
      const host = await native.call('ping');
      let st = null, err = null;
      try { st = await call('raypass.status'); } catch (e) { err = errorOf(e); }
      return { host, status: st, error: err };
    }
  }
  throw new RaypassError('bad_request', 'unknown message');
}

chrome.runtime.onMessage.addListener((m, sender, reply) => {
  if (sender.id !== chrome.runtime.id) return false;
  if (m && typeof m.type === 'string' && m.type.startsWith('offscreen.')) return false;
  const fromExt = typeof sender.url === 'string' && sender.url.startsWith(EXT_ORIGIN);
  let work;
  try {
    if (fromExt) work = onExt(M.ext(m));
    else if (sender.tab && Number.isInteger(sender.frameId)) work = onContent(M.content(m), sender);
    else throw new RaypassError('forbidden', 'unknown sender');
  } catch (e) {
    reply({ ok: false, error: { code: 'bad_request', message: e.message } });
    return false;
  }
  work.then((r) => reply({ ok: true, ...r }), (e) => reply({ ok: false, error: errorOf(e) }));
  return true; // reply comes later
});

// ---- menus, keys, toolbar -------------------------------------------------------------------

chrome.runtime.onInstalled.addListener(() => {
  chrome.contextMenus.removeAll(() => {
    chrome.contextMenus.create({
      id: 'raypass-fill', title: 'Fill with Raypass', contexts: ['editable'],
      documentUrlPatterns: ['http://*/*', 'https://*/*'],
    });
  });
});

chrome.contextMenus.onClicked.addListener((info, tab) => {
  if (info.menuItemId === 'raypass-fill') fillBest(tab, info.frameId ?? 0, info.frameUrl || info.pageUrl);
});

chrome.commands.onCommand.addListener(async (command, tab) => {
  if (command !== 'fill') return;
  if (!tab) [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  fillBest(tab);
});

chrome.action.setBadgeBackgroundColor?.({ color: '#E8A100' });
