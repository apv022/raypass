// Raypass content script: finds login forms, puts a small Raypass icon in their fields, fills
// them on request, and notices submitted logins so the background can offer to save them.
//
// Runs in the extension's isolated world in every frame. Everything it draws lives in one
// element with a *closed* shadow root, so the page's scripts cannot read or restyle it. It never
// writes a secret anywhere in the page except into the field being filled, and it keeps
// credentials only for the moment of filling. Every click it acts on must be a real one
// (isTrusted): a page cannot script our buttons.
(function () {
  'use strict';
  if (globalThis.__raypassContent) return; // injected twice (e.g. after an extension reload)
  globalThis.__raypassContent = true;

  const F = globalThis.Raypass.forms;
  const TOP = window === window.top;

  // ---- messaging ----------------------------------------------------------------------------

  async function ask(m) {
    try {
      const r = await chrome.runtime.sendMessage(m);
      return r || { ok: false, error: { code: 'internal', message: 'no answer' } };
    } catch (e) {
      return { ok: false, error: { code: 'host_unavailable', message: String(e?.message || e) } };
    }
  }

  // ---- detection ----------------------------------------------------------------------------

  let found = [];
  let lastState = '';

  function scan() {
    try { found = F.detect(document); } catch { found = []; }
    renderIcons();
    reportState(false);
  }

  function reportState(focused) {
    const login = found.some((f) => f.kind !== 'signup');
    const key = login + ':' + focused;
    if (key === lastState && !focused) return;
    lastState = key;
    if (login || focused) ask({ type: 'cs.state', login, focused });
  }

  let scanTimer = 0;
  function scheduleScan() {
    clearTimeout(scanTimer);
    scanTimer = setTimeout(scan, 250);
  }

  // ---- our UI: one closed shadow root -------------------------------------------------------

  const CSS = `
    :host { all: initial; }
    * { box-sizing: border-box; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, Roboto, sans-serif; }
    .layer { position: fixed; inset: 0; pointer-events: none; }
    .icon { position: fixed; width: 20px; height: 20px; border: 0; padding: 0; margin: 0; border-radius: 6px;
      background: #E8A100; cursor: pointer; pointer-events: auto; display: grid; place-items: center;
      box-shadow: 0 1px 2px rgba(0,0,0,.2); opacity: .92; }
    .icon:hover { opacity: 1; background: #D39300; }
    .icon svg { width: 14px; height: 14px; display: block; }
    .panel { position: fixed; pointer-events: auto; min-width: 240px; max-width: 340px; max-height: 300px;
      overflow: auto; background: #fff; color: #1D1D1F; border-radius: 12px; padding: 4px;
      box-shadow: 0 1px 2px rgba(0,0,0,.06), 0 8px 24px rgba(0,0,0,.18); border: 0.5px solid rgba(60,60,67,.18);
      font-size: 13px; line-height: 18px; }
    .row { display: flex; flex-direction: column; align-items: flex-start; gap: 0; width: 100%; border: 0;
      background: transparent; color: inherit; text-align: left; padding: 6px 10px; border-radius: 8px; cursor: pointer; font-size: 13px; }
    .row:hover, .row:focus { background: rgba(232,161,0,.16); outline: none; }
    .name { font-weight: 600; }
    .user { color: rgba(60,60,67,.6); font-size: 12px; }
    .note { padding: 8px 10px; color: rgba(60,60,67,.6); }
    .head { padding: 6px 10px 4px; font-size: 11px; font-weight: 600; color: rgba(60,60,67,.6); letter-spacing: .02em; text-transform: uppercase; }
    .btn { border: 0; border-radius: 8px; padding: 0 12px; height: 28px; font-size: 13px; font-weight: 600; cursor: pointer;
      background: #F2F2F7; color: #1D1D1F; }
    .btn.primary { background: #E8A100; color: #1D1D1F; }
    .btn:hover { filter: brightness(.95); }
    .bar { position: fixed; top: 12px; right: 12px; pointer-events: auto; width: 320px; background: #fff; color: #1D1D1F;
      border-radius: 12px; padding: 12px; box-shadow: 0 1px 2px rgba(0,0,0,.06), 0 8px 24px rgba(0,0,0,.18);
      border: 0.5px solid rgba(60,60,67,.18); font-size: 13px; line-height: 18px; }
    .bar .title { font-weight: 600; margin-bottom: 2px; display: flex; align-items: center; gap: 8px; }
    .bar .sub { color: rgba(60,60,67,.6); margin-bottom: 10px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .bar .actions { display: flex; gap: 8px; justify-content: flex-end; }
    .badge { width: 18px; height: 18px; border-radius: 5px; background: #E8A100; display: grid; place-items: center; }
    .badge svg { width: 12px; height: 12px; }
    .toast { position: fixed; bottom: 16px; right: 16px; pointer-events: auto; background: #1D1D1F; color: #F5F5F7;
      border-radius: 10px; padding: 8px 12px; font-size: 13px; box-shadow: 0 8px 24px rgba(0,0,0,.25); max-width: 320px; }
    @media (prefers-color-scheme: dark) {
      .panel, .bar { background: #1C1C1E; color: #F5F5F7; border-color: rgba(84,84,88,.6); }
      .user, .note, .head, .bar .sub { color: rgba(235,235,245,.6); }
      .btn { background: #2C2C2E; color: #F5F5F7; }
      .btn.primary { background: #FFB524; color: #1D1D1F; }
      .row:hover, .row:focus { background: rgba(255,181,36,.16); }
      .toast { background: #F5F5F7; color: #1D1D1F; }
    }`;

  let host = null, shadow = null, layer = null;

  function ui() {
    if (host && host.isConnected) return layer;
    host = document.createElement('raypass-ui');
    for (const [k, v] of [['position', 'fixed'], ['top', '0'], ['left', '0'], ['width', '0'], ['height', '0'],
      ['z-index', '2147483647'], ['display', 'block'], ['margin', '0'], ['padding', '0'], ['border', '0']]) {
      host.style.setProperty(k, v, 'important');
    }
    shadow = host.attachShadow({ mode: 'closed' });
    try {
      const sheet = new CSSStyleSheet();
      sheet.replaceSync(CSS);
      shadow.adoptedStyleSheets = [sheet];
    } catch {
      const st = document.createElement('style');
      st.textContent = CSS;
      shadow.append(st);
    }
    layer = document.createElement('div');
    layer.className = 'layer';
    shadow.append(layer);
    document.documentElement.append(host);
    return layer;
  }

  function el(tag, cls, text) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  /** The Raypass mark: a key-ring "ray" glyph, drawn with DOM calls (no markup strings). */
  function mark() {
    const NS = 'http://www.w3.org/2000/svg';
    const svg = document.createElementNS(NS, 'svg');
    svg.setAttribute('viewBox', '0 0 16 16');
    const c = document.createElementNS(NS, 'circle');
    c.setAttribute('cx', '5.5'); c.setAttribute('cy', '8'); c.setAttribute('r', '3');
    c.setAttribute('fill', 'none'); c.setAttribute('stroke', '#1D1D1F'); c.setAttribute('stroke-width', '2');
    const p = document.createElementNS(NS, 'path');
    p.setAttribute('d', 'M8.5 8H14M12 8v2.5M14 8v2');
    p.setAttribute('fill', 'none'); p.setAttribute('stroke', '#1D1D1F'); p.setAttribute('stroke-width', '2');
    p.setAttribute('stroke-linecap', 'round');
    svg.append(c, p);
    return svg;
  }

  // ---- inline icons -------------------------------------------------------------------------

  /** field -> its icon button */
  let icons = new Map();

  function renderIcons() {
    const want = F.iconFields(found).filter((f) => f.getBoundingClientRect().width >= 60);
    const keep = new Map();
    for (const f of want) {
      let b = icons.get(f);
      if (!b) {
        b = el('button', 'icon');
        b.type = 'button';
        b.title = 'Raypass';
        b.setAttribute('aria-label', 'Fill with Raypass');
        b.tabIndex = -1;
        b.append(mark());
        b.addEventListener('mousedown', (e) => e.preventDefault()); // keep the field's focus
        b.addEventListener('click', (e) => {
          if (!e.isTrusted) return;
          e.preventDefault();
          e.stopPropagation();
          openPanel(f);
        });
        ui().append(b);
      }
      keep.set(f, b);
    }
    for (const [f, b] of icons) if (!keep.has(f)) b.remove();
    icons = keep;
    place();
  }

  function place() {
    for (const [f, b] of icons) {
      const r = f.getBoundingClientRect();
      const visible = r.width > 0 && r.bottom > 0 && r.top < innerHeight && f.isConnected;
      b.style.display = visible ? 'grid' : 'none';
      b.style.left = Math.round(r.right - 26) + 'px';
      b.style.top = Math.round(r.top + (r.height - 20) / 2) + 'px';
    }
    if (panel && panelField) positionPanel();
  }

  let placing = false;
  function schedulePlace() {
    if (placing) return;
    placing = true;
    requestAnimationFrame(() => { placing = false; place(); });
  }
  addEventListener('scroll', schedulePlace, { passive: true, capture: true });
  addEventListener('resize', schedulePlace, { passive: true });

  // ---- the matches panel --------------------------------------------------------------------

  let panel = null, panelField = null;

  function closePanel() {
    if (panel) panel.remove();
    panel = null;
    panelField = null;
  }

  function positionPanel() {
    const r = panelField.getBoundingClientRect();
    panel.style.left = Math.round(Math.max(4, Math.min(r.left, innerWidth - 250))) + 'px';
    const below = r.bottom + 4;
    panel.style.top = Math.round(below + 200 > innerHeight && r.top > 220 ? r.top - 4 - panel.offsetHeight : below) + 'px';
  }

  async function openPanel(field) {
    closePanel();
    panelField = field;
    panel = el('div', 'panel');
    panel.setAttribute('role', 'listbox');
    panel.append(el('div', 'note', 'Raypass…'));
    ui().append(panel);
    positionPanel();
    const r = await ask({ type: 'cs.matches' });
    if (!panel || panelField !== field) return;
    panel.replaceChildren();
    if (!r.ok) {
      panel.append(el('div', 'note', explain(r.error)));
    } else if (r.locked) {
      panel.append(el('div', 'note', 'Raypass is locked.'));
      const b = el('button', 'row', 'Unlock from the toolbar');
      b.addEventListener('click', (e) => { if (e.isTrusted) { ask({ type: 'cs.openPopup' }); closePanel(); } });
      panel.append(b);
    } else if (!r.items.length) {
      panel.append(el('div', 'note', 'No logins for this site.'));
    } else {
      panel.append(el('div', 'head', 'Raypass'));
      for (const it of r.items.slice(0, 20)) {
        const b = el('button', 'row');
        b.type = 'button';
        b.setAttribute('role', 'option');
        b.append(el('span', 'name', it.name || '(no name)'), el('span', 'user', it.user || ''));
        b.addEventListener('mousedown', (e) => e.preventDefault());
        b.addEventListener('click', (e) => {
          if (!e.isTrusted) return;
          closePanel();
          fillItem(it.id, field);
        });
        panel.append(b);
      }
    }
    positionPanel();
  }

  function explain(err) {
    switch (err?.code) {
      case 'host_missing': return 'The Raypass native host is not installed. See the extension options.';
      case 'unavailable': return 'Sol is not running.';
      case 'forbidden': return err.message === 'locked' ? 'Raypass is locked.' : 'Raypass does not fill here.';
      case 'not_installed': return 'Raypass is not installed in Sol.';
      default: return err?.message || 'Raypass is not available.';
    }
  }

  document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && panel) closePanel(); }, true);
  document.addEventListener('mousedown', (e) => { if (panel && e.target !== host) closePanel(); }, true);

  // ---- filling ------------------------------------------------------------------------------

  /** value we filled -> field, so a login we filled is not offered for saving again. */
  const filledByUs = new WeakMap();

  async function fillItem(id, field) {
    scan();
    const target = F.resultFor(found, field || document.activeElement);
    if (!target) return { filled: [], error: { code: 'not_found', message: 'no login form here' } };
    const r = await ask({ type: 'cs.fill', id });
    if (!r.ok) { toast(explain(r.error)); return { filled: [], error: r.error }; }
    const filled = F.fill(target, r);
    if (target.password && r.pass) filledByUs.set(target.password, r.pass);
    return { filled };
  }

  function fillPassword(password) {
    scan();
    const active = document.activeElement;
    let g = found.find((f) => f.newPasswords.includes(active) || f.password === active) ||
      found.find((f) => f.kind === 'signup') || found.find((f) => f.password);
    if (!g) return { filled: false };
    const fields = g.newPasswords.length ? g.newPasswords : [g.password];
    for (const f of fields) F.setValue(f, password);
    return { filled: true };
  }

  // ---- toast and save bar -------------------------------------------------------------------

  let toastEl = null, toastTimer = 0;
  function toast(text) {
    if (toastEl) toastEl.remove();
    toastEl = el('div', 'toast', text);
    toastEl.setAttribute('role', 'status');
    ui().append(toastEl);
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { toastEl?.remove(); toastEl = null; }, 4000);
  }

  let bar = null;
  function hideBar() { bar?.remove(); bar = null; }

  function showBar(save) {
    if (!TOP || !save) return;
    hideBar();
    bar = el('div', 'bar');
    bar.setAttribute('role', 'dialog');
    bar.setAttribute('aria-label', 'Save login to Raypass');
    const title = el('div', 'title');
    const badge = el('span', 'badge');
    badge.append(mark());
    title.append(badge, el('span', '', (save.update ? 'Update the password for ' : 'Save login for ') + (save.host || 'this site') + '?'));
    const sub = el('div', 'sub', save.user ? save.user : 'no username');
    const actions = el('div', 'actions');
    const no = el('button', 'btn', 'Not now');
    const yes = el('button', 'btn primary', save.update ? 'Update' : 'Save');
    no.type = yes.type = 'button';
    actions.append(no, yes);
    bar.append(title, sub, actions);
    const answer = async (e, ok) => {
      if (!e.isTrusted) return;
      yes.disabled = no.disabled = true;
      const r = await ask({ type: 'cs.saveAnswer', token: save.token, save: ok });
      if (!ok) { hideBar(); return; }
      if (r.ok) { hideBar(); toast(r.created === false ? 'Password updated in Raypass.' : 'Saved to Raypass.'); return; }
      yes.disabled = no.disabled = false;
      sub.textContent = r.error?.message === 'locked'
        ? 'Raypass is locked: unlock it from the toolbar, then save there or here.'
        : explain(r.error);
      if (r.error?.message === 'locked') ask({ type: 'cs.openPopup' });
    };
    no.addEventListener('click', (e) => answer(e, false));
    yes.addEventListener('click', (e) => answer(e, true));
    ui().append(bar);
  }

  // ---- noticing submitted logins ------------------------------------------------------------

  let lastSent = { user: null, pass: null, at: 0 };

  function groupFor(node) {
    if (!node) return null;
    return found.find((f) => f.group === node || f.group.contains(node)) || null;
  }

  function captured(g) {
    if (!g) return;
    if (g.kind === 'username') {
      const user = g.username?.value?.trim();
      if (user) ask({ type: 'cs.username', user: user.slice(0, 256) });
      return;
    }
    // A sign-up or change-password form saves the new password, not the current one.
    const pwField = g.newPasswords.find((f) => f.value) || g.password;
    const pass = pwField?.value || '';
    if (!pass || pass.length > 1024) return;
    const user = (g.username?.value || '').trim().slice(0, 256);
    if (filledByUs.get(pwField) === pass) return; // we filled it from the vault: nothing new
    const now = Date.now();
    if (lastSent.user === user && lastSent.pass === pass && now - lastSent.at < 5000) return;
    lastSent = { user, pass, at: now };
    ask({ type: 'cs.submitted', user, pass });
    setTimeout(() => { if (lastSent.at === now) lastSent = { user: null, pass: null, at: 0 }; }, 5000);
  }

  document.addEventListener('submit', (e) => {
    // Scripted submits (requestSubmit) count too: the values are still what the person typed,
    // and a fake one can at most show the bar, which saves nothing without a real click.
    if (!(e.target instanceof HTMLFormElement)) return;
    scan();
    captured(groupFor(e.target));
  }, true);

  document.addEventListener('click', (e) => {
    if (!e.isTrusted) return;
    const b = e.target instanceof Element && e.target.closest('button, input[type=submit], input[type=image], [role=button], a');
    if (!b || (host && b === host) || !F.isSubmitLike(b)) return;
    const g = groupFor(b);
    if (g) captured(g);
  }, true);

  document.addEventListener('keydown', (e) => {
    if (!e.isTrusted || e.key !== 'Enter' || !(e.target instanceof HTMLInputElement)) return;
    const g = groupFor(e.target);
    if (g) captured(g);
  }, true);

  document.addEventListener('focusin', (e) => {
    if (!e.isTrusted) return;
    if (F.iconFields(found).includes(e.target)) reportState(true);
  }, true);

  // ---- from the background ------------------------------------------------------------------

  chrome.runtime.onMessage.addListener((m, sender, reply) => {
    if (sender.id !== chrome.runtime.id || sender.tab || !m || typeof m.type !== 'string') return false;
    switch (m.type) {
      case 'bg.fill':
        if (typeof m.id !== 'string') return false;
        fillItem(m.id, document.activeElement).then(reply);
        return true;
      case 'bg.fillPassword':
        if (typeof m.password !== 'string') return false;
        reply(fillPassword(m.password));
        return false;
      case 'bg.showMatches': {
        scan();
        const a = document.activeElement;
        const field = F.iconFields(found).includes(a) ? a : F.iconFields(found)[0];
        if (field) openPanel(field);
        reply({});
        return false;
      }
      case 'bg.toast':
        if (typeof m.text === 'string') toast(m.text.slice(0, 200));
        reply({});
        return false;
      case 'bg.showSave':
        showBar(m.save);
        reply({});
        return false;
      case 'bg.hideSave':
        hideBar();
        reply({});
        return false;
    }
    return false;
  });

  // ---- start --------------------------------------------------------------------------------

  new MutationObserver((records) => {
    for (const r of records) {
      if (r.target === host || (host && host.contains(r.target))) continue;
      scheduleScan();
      return;
    }
  }).observe(document.documentElement, {
    childList: true, subtree: true, attributes: true, attributeFilter: ['type', 'style', 'class', 'hidden', 'disabled'],
  });

  scan();
  if (TOP) ask({ type: 'cs.pendingSave' }).then((r) => { if (r.ok && r.save) showBar(r.save); });
})();
