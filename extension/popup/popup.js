// The toolbar popup: unlock, the current tab's matches (fill, copy user, password, code), search
// over the whole vault, and the generator. Every vault call goes through the background (and
// from there through the native host); the popup only holds a secret for the instant of a copy.
// All data reaches the DOM through textContent.

const WEB_UI = 'http://raypass.localhost:7777/';
const $ = (id) => document.getElementById(id);

let tab = null;
let clipSeconds = 30;

// ---- talking to the background --------------------------------------------------------------

class PopupError extends Error {
  constructor(e) { super(e?.message || 'failed'); this.code = e?.code || 'internal'; }
}

async function send(m) {
  let r;
  try { r = await chrome.runtime.sendMessage(m); } catch (e) { throw new PopupError({ code: 'internal', message: String(e?.message || e) }); }
  if (!r) throw new PopupError({ code: 'internal', message: 'no answer' });
  if (!r.ok) throw new PopupError(r.error);
  return r;
}

async function api(action, args) {
  try {
    return (await send({ type: 'ext.call', action, args })).result;
  } catch (e) {
    if (e.code === 'forbidden' && e.message === 'locked') showUnlock();
    throw e;
  }
}

// ---- small DOM helpers ----------------------------------------------------------------------

function h(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

function button(label, title, onClick, cls = 'btn') {
  const b = h('button', cls, label);
  b.type = 'button';
  if (title) { b.title = title; b.setAttribute('aria-label', title); }
  b.addEventListener('click', (e) => { e.stopPropagation(); onClick(b); });
  return b;
}

let toastTimer = 0;
function toast(text) {
  const t = $('toast');
  t.textContent = text;
  t.classList.remove('hidden');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.add('hidden'), 2200);
}

function show(view) {
  for (const v of ['problem', 'unlock', 'main']) $('v-' + v).classList.toggle('hidden', v !== view);
  $('lock').classList.toggle('hidden', view !== 'main');
}

// ---- clipboard ------------------------------------------------------------------------------

async function copy(text, what, secret) {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    toast('Could not copy');
    return;
  }
  if (secret) {
    send({ type: 'ext.clipArm', seconds: clipSeconds }).catch(() => {});
    toast(`${what} copied · clears in ${clipSeconds} s`);
  } else {
    toast(`${what} copied`);
  }
}

// ---- views ----------------------------------------------------------------------------------

function problem(title, text, help) {
  $('problem-title').textContent = title;
  $('problem-text').textContent = text;
  $('problem-help').textContent = help === 'web' ? 'Open Raypass' : 'Set up';
  $('problem-help').onclick = () => help === 'web' ? openWeb() : chrome.runtime.openOptionsPage();
  show('problem');
}

function showError(e) {
  switch (e.code) {
    case 'host_missing':
      return problem('Connect the extension', 'The Raypass native host is not installed on this computer. Run raypass-native install once.', 'options');
    case 'unavailable':
      return problem('Sol is not running', 'Start Sol on this computer (run sol), then try again.', 'options');
    case 'sol_not_found':
      return problem('Sol not found', 'Install Sol, then run raypass-native install again so the host can find it.', 'options');
    case 'not_installed':
      return problem('Raypass is not in Sol', 'Install the Raypass app in Sol (sol app install).', 'options');
    case 'approval_required':
      return problem('Approval needed', e.message, 'web');
    default:
      return problem('Raypass is not reachable', e.message, 'options');
  }
}

function showUnlock() {
  show('unlock');
  $('master').value = '';
  $('unlock-error').textContent = '';
  setTimeout(() => $('master').focus(), 0);
}

$('unlock-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  const pw = $('master').value; // used exactly as typed: never trimmed (design/DESIGN.md)
  if (!pw) return;
  $('unlock-btn').disabled = true;
  $('unlock-btn').textContent = 'Unlocking…';
  $('unlock-error').textContent = '';
  try {
    await send({ type: 'ext.call', action: 'raypass.unlock', args: { password: pw } });
    $('master').value = '';
    await showMain();
  } catch (err) {
    $('unlock-error').textContent = err.code === 'unauthorized' ? 'Wrong password.' : err.message;
    $('master').select();
  } finally {
    $('unlock-btn').disabled = false;
    $('unlock-btn').textContent = 'Unlock';
  }
});

// ---- rows -----------------------------------------------------------------------------------

/** Live TOTP code display: fetches when asked, again when the period rolls over. */
function totpButton(id, sub) {
  let timer = 0, tick = 0;
  const b = button('Code', 'Copy the one-time code', async () => {
    try {
      const t = await api('raypass.totp', { id });
      await copy(t.code, 'Code', true);
      render(t);
    } catch (e) { toast(e.message); }
  });
  function render(t) {
    clearTimeout(timer); clearInterval(tick);
    const until = Date.now() + t.remaining * 1000;
    const box = h('span', 'totp');
    const NS = 'http://www.w3.org/2000/svg';
    const svg = document.createElementNS(NS, 'svg');
    svg.setAttribute('viewBox', '0 0 14 14'); svg.setAttribute('class', 'ring');
    const bg = document.createElementNS(NS, 'circle');
    const fg = document.createElementNS(NS, 'circle');
    for (const c of [bg, fg]) { c.setAttribute('cx', '7'); c.setAttribute('cy', '7'); c.setAttribute('r', '5'); }
    bg.setAttribute('class', 'bg'); fg.setAttribute('class', 'fg');
    const len = 2 * Math.PI * 5;
    fg.setAttribute('stroke-dasharray', String(len));
    svg.append(bg, fg);
    const code = t.code.length > 6 ? t.code : t.code.replace(/^(\d{3})(\d+)$/, '$1 $2');
    box.append(svg, h('span', '', code));
    sub.replaceChildren(box);
    const draw = () => {
      const left = Math.max(0, until - Date.now());
      fg.setAttribute('stroke-dashoffset', String(len * (1 - left / (t.period * 1000))));
    };
    draw();
    tick = setInterval(draw, 1000);
    // The code changes at the end of its period: fetch the next one then (time, not state).
    timer = setTimeout(async () => {
      try { render(await api('raypass.totp', { id })); } catch { clearInterval(tick); }
    }, t.remaining * 1000 + 300);
  }
  return b;
}

function itemRow(it, { fill, passFor }) {
  const li = h('li', 'item');
  li.append(h('span', 'avatar', (it.name || '?').trim().charAt(0) || '?'));
  const meta = h('div', 'meta');
  const sub = h('div', 'sub', it.type === 'login' || !it.type ? (it.user || '—') : it.type);
  meta.append(h('div', 'name', it.name || '(no name)'), sub);
  li.append(meta);
  const acts = h('div', 'acts');
  if (it.type === 'login' || !it.type) {
    if (it.user) acts.append(button('User', 'Copy the username', () => copy(it.user, 'Username', false)));
    acts.append(button('Pass', 'Copy the password', async () => {
      try {
        const p = await passFor(it);
        if (p) await copy(p, 'Password', true); else toast('No password saved');
      } catch (e) { toast(e.message); }
    }));
    if (it.has_totp) acts.append(totpButton(it.id, sub));
    if (fill) {
      acts.append(button('Fill', 'Fill this login into the page', async () => {
        try {
          await send({ type: 'ext.fillTab', tabId: tab.id, id: it.id });
          window.close();
        } catch (e) { toast(e.message); }
      }, 'btn primary'));
    }
  }
  li.append(acts);
  return li;
}

// ---- this site ------------------------------------------------------------------------------

function fillable(u) { return typeof u === 'string' && /^https?:\/\//i.test(u); }

async function loadMatches() {
  const list = $('matches');
  list.replaceChildren();
  $('matches-empty').classList.add('hidden');
  if (!tab || !fillable(tab.url)) {
    $('matches-empty').textContent = 'Open a web page to see its logins.';
    $('matches-empty').classList.remove('hidden');
    return;
  }
  const rows = await api('raypass.match', { url: tab.url });
  // fill takes the tab's URL as the browser reports it; the vault refuses a URL that does not match.
  const passFor = async (it) => (await api('raypass.fill', { id: it.id, url: tab.url })).pass;
  for (const it of rows || []) list.append(itemRow(it, { fill: true, passFor }));
  if (!rows?.length) {
    $('matches-empty').textContent = 'No logins for this site.';
    $('matches-empty').classList.remove('hidden');
  }
}

// ---- search ---------------------------------------------------------------------------------

let searchSeq = 0, searchTimer = 0;
async function search() {
  const seq = ++searchSeq;
  const q = $('q').value.trim();
  const rows = await api('raypass.list', q ? { query: q } : {});
  if (seq !== searchSeq) return;
  const list = $('results');
  list.replaceChildren();
  const passFor = async (it) => (await api('raypass.get', { id: it.id })).pass;
  for (const it of (rows || []).slice(0, 100)) list.append(itemRow(it, { fill: false, passFor }));
  $('results-empty').classList.toggle('hidden', !!rows?.length);
}
$('q').addEventListener('input', () => {
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => search().catch((e) => toast(e.message)), 120);
});

// ---- generator ------------------------------------------------------------------------------

let genMode = 'chars';
let generated = '';

function genArgs() {
  if (genMode === 'words') {
    return { mode: 'words', words: Number($('nwords').value), separator: $('sep').value, capitalize: $('cap').checked, number: $('number').checked };
  }
  return { mode: 'chars', length: Number($('len').value), upper: $('upper').checked, lower: $('lower').checked,
    digits: $('digits').checked, symbols: $('symbols').checked, avoid_ambiguous: $('avoid').checked };
}

async function generate() {
  try {
    const r = await api('raypass.generate', genArgs());
    generated = r.password;
    $('gen-pass').textContent = generated;
    $('gen-bits').textContent = `${Math.round(r.entropy_bits)} bits`;
  } catch (e) {
    $('gen-pass').textContent = '';
    $('gen-bits').textContent = e.message;
  }
}

for (const b of document.querySelectorAll('[data-mode]')) {
  b.addEventListener('click', () => {
    genMode = b.dataset.mode;
    for (const o of document.querySelectorAll('[data-mode]')) o.setAttribute('aria-pressed', String(o === b));
    for (const o of document.querySelectorAll('.opt.chars')) o.classList.toggle('hidden', genMode !== 'chars');
    for (const o of document.querySelectorAll('.opt.words')) o.classList.toggle('hidden', genMode !== 'words');
    generate();
  });
}
for (const id of ['len', 'nwords']) {
  $(id).addEventListener('input', () => { $(id + '-v').textContent = $(id).value; });
  $(id).addEventListener('change', generate);
}
for (const id of ['upper', 'lower', 'digits', 'symbols', 'avoid', 'cap', 'number', 'sep']) $(id).addEventListener('change', generate);
$('gen-again').addEventListener('click', generate);
$('gen-copy').addEventListener('click', () => generated && copy(generated, 'Password', true));
$('gen-fill').addEventListener('click', async () => {
  if (!generated || !tab) return;
  try {
    await send({ type: 'ext.fillGenerated', tabId: tab.id, password: generated });
    await copy(generated, 'Password', true); // so it can be pasted into the confirmation too
  } catch (e) { toast(e.message); }
});

// ---- tabs -----------------------------------------------------------------------------------

let genLoaded = false;
for (const b of document.querySelectorAll('[data-tab]')) {
  b.addEventListener('click', () => {
    for (const o of document.querySelectorAll('[data-tab]')) o.setAttribute('aria-selected', String(o === b));
    for (const t of ['site', 'search', 'gen']) $('t-' + t).classList.toggle('hidden', t !== b.dataset.tab);
    if (b.dataset.tab === 'search') { $('q').focus(); search().catch((e) => toast(e.message)); }
    if (b.dataset.tab === 'gen' && !genLoaded) { genLoaded = true; generate(); }
  });
}

// ---- pending save ---------------------------------------------------------------------------

async function loadSave() {
  if (!tab) return;
  let r;
  try { r = await send({ type: 'ext.pendingSave', tabId: tab.id }); } catch { return; }
  const s = r.save;
  $('save').classList.toggle('hidden', !s);
  if (!s) return;
  $('save-title').textContent = (s.update ? 'Update the password for ' : 'Save login for ') + (s.host || 'this site') + '?';
  $('save-user').textContent = s.user || 'no username';
  $('save-yes').textContent = s.update ? 'Update' : 'Save';
  const answer = async (save) => {
    try {
      await send({ type: 'ext.saveAnswer', tabId: tab.id, token: s.token, save });
      $('save').classList.add('hidden');
      if (save) { toast('Saved'); loadMatches().catch(() => {}); }
    } catch (e) { toast(e.message); }
  };
  $('save-yes').onclick = () => answer(true);
  $('save-no').onclick = () => answer(false);
}

// ---- start ----------------------------------------------------------------------------------

function openWeb() { chrome.tabs.create({ url: WEB_UI }); window.close(); }
$('open-web').addEventListener('click', openWeb);
$('lock').addEventListener('click', async () => {
  try { await api('raypass.lock'); } catch { /* shows below */ }
  showUnlock();
});
$('problem-retry').addEventListener('click', () => start());

async function showMain() {
  show('main');
  await Promise.all([loadSave(), loadMatches().catch((e) => { if (e.code !== 'forbidden') toast(e.message); })]);
}

async function start() {
  [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  $('host').textContent = tab && fillable(tab.url) ? new URL(tab.url).hostname : '';
  let st;
  try {
    st = (await send({ type: 'ext.status' })).result;
  } catch (e) {
    showError(e);
    return;
  }
  if (st.clip_clear > 0) clipSeconds = st.clip_clear;
  if (!st.initialized) return problem('Set up Raypass', 'Create your vault in the Raypass web app first.', 'web');
  if (st.locked) { showUnlock(); loadSave(); return; }
  await showMain();
}

start();
