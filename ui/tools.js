// The generator, health report and settings screens. Each control calls an action
// (raypass.generate, health, settings, change_password, import, export, lock); the generator's
// options are a UI preference kept in localStorage, its output is kept only in memory.
import { html, render, raw } from '/sol.js';
import { healthLabel, durationLabel, isEncryptedExport, exportName, typeOf, scoreOf } from '/lib.js';
import { svg } from '/icons.js';
import { state, hooks, rp, act, fail, copy, confirmBox, askPassword, prefs, lockNow } from '/core.js';
import { meterHtml, meter, pwField, wireReveal } from '/gate.js';
import { runsHtml, tileHtml } from '/detail.js';
import { genOptions } from '/edit.js';

const $ = (id) => document.getElementById(id);

const toolBar = (title) => html`<header class="bar t-bar">
  <button class="plain back" type="button" data-t="back" aria-label="Back to the vault"><sol-icon name="back"></sol-icon><span>Vault</span></button>
  <h1 class="tool-title">${title}</h1></header>`;

// ---------- generator ----------

let genSeq = 0;
async function generate() {
  const seq = ++genSeq;
  try {
    const g = await rp('generate', genOptions());
    if (seq !== genSeq || state.view.kind !== 'generator') return;
    state.gen = g;
    paintGen();
  } catch (e) { fail(e); }
}

function paintGen() {
  const out = $('gen-out');
  if (!out || !state.gen) return;
  render(out, runsHtml(state.gen.password));
  out.classList.remove('pop'); void out.offsetWidth; out.classList.add('pop');
  meter($('gen-meter'), null, state.gen.entropy_bits);
}

function drawGenerator() {
  const o = genOptions();
  const chars = o.mode !== 'words';
  const sw = (k, label, sub = '') => html`<label class="opt"><span class="opt-text">${label}${sub ? html`<small>${sub}</small>` : ''}</span><input type="checkbox" role="switch" data-opt="${k}" ${o[k] ? raw('checked') : ''}></label>`;
  render($('tool'), html`${toolBar('Generator')}
    <div class="t-scroll"><div class="t-col">
      <div class="gen-card">
        <div class="gen-out mono" id="gen-out" aria-live="polite" aria-label="Generated password"></div>
        <div class="meter-wrap">${raw(String(meterHtml()).replace('class="meter"', 'class="meter" id="gen-meter"'))}</div>
        <div class="gen-acts">
          <button type="button" class="primary large" data-t="copy"><sol-icon name="copy"></sol-icon>Copy</button>
          <button type="button" class="large" data-t="regen"><sol-icon name="refresh"></sol-icon>Regenerate</button>
        </div>
      </div>
      <div class="segmented gen-mode" role="group" aria-label="Kind">
        <button type="button" data-mode="chars" aria-pressed="${chars}">Password</button>
        <button type="button" data-mode="words" aria-pressed="${!chars}">Passphrase</button>
      </div>
      <div class="group opts">
        ${chars ? html`
          <div class="opt slider"><span class="opt-text">Length</span><input type="range" min="4" max="128" value="${o.length}" data-opt="length" aria-label="Length"><output class="mono">${o.length}</output></div>
          ${sw('upper', 'Capital letters', 'A–Z')}${sw('lower', 'Small letters', 'a–z')}${sw('digits', 'Digits', '0–9')}${sw('symbols', 'Symbols', '!@#$%^&*')}
          ${sw('avoid_ambiguous', 'Avoid look-alikes', 'No 0 O o 1 l I |')}`
    : html`
          <div class="opt slider"><span class="opt-text">Words</span><input type="range" min="3" max="20" value="${o.words}" data-opt="words" aria-label="Words"><output class="mono">${o.words}</output></div>
          <label class="opt"><span class="opt-text">Separator</span><input class="sep mono" data-opt="separator" value="${o.separator}" maxlength="3" aria-label="Separator" spellcheck="false"></label>
          ${sw('capitalize', 'Capitalize words')}${sw('number', 'Add a number')}`}
      </div>
      <p class="group-footer">${chars ? 'Generated on this device with a cryptographic random source, without modulo bias.' : 'Words from the EFF large wordlist: each adds about 12.9 bits.'} Nothing you generate is saved unless you put it in an item.</p>
    </div></div>`);
  if (state.gen) paintGen(); else generate();
}

function setOpt(k, v) {
  const o = genOptions();
  o[k] = v;
  if (o.mode === 'chars' && !(o.upper || o.lower || o.digits || o.symbols)) { o.lower = true; }
  prefs.set('gen', o);
}

function wireGenerator(root) {
  let slideT = 0;
  root.addEventListener('input', (e) => {
    if (state.view.kind !== 'generator') return;
    const k = e.target.dataset.opt;
    if (!k) return;
    if (e.target.type === 'range') {
      e.target.nextElementSibling.textContent = e.target.value;
      setOpt(k, +e.target.value);
      clearTimeout(slideT); slideT = setTimeout(generate, 60);
    } else if (e.target.type === 'checkbox') {
      setOpt(k, e.target.checked);
      if (!e.target.checked && !['upper', 'lower', 'digits', 'symbols'].some((x) => genOptions()[x])) drawGenerator();
      generate();
    } else { setOpt(k, e.target.value); clearTimeout(slideT); slideT = setTimeout(generate, 150); }
  });
  root.addEventListener('click', (e) => {
    if (state.view.kind !== 'generator') return;
    const m = e.target.closest('[data-mode]');
    if (m) { setOpt('mode', m.dataset.mode); state.gen = null; drawGenerator(); return; }
    const t = e.target.closest('[data-t]');
    if (t?.dataset.t === 'copy' && state.gen) copy(state.gen.password, 'Password');
    if (t?.dataset.t === 'regen') generate();
  });
}

// ---------- health ----------

async function loadHealth() {
  try { state.health = await rp('health', {}); } catch (e) { fail(e); }
  if (state.view.kind === 'health') drawHealth();
}

function drawHealth() {
  const h = state.health;
  if (!h) {
    render($('tool'), html`${toolBar('Password health')}<div class="t-scroll"><div class="t-col"></div></div>`);
    loadHealth();
    return;
  }
  const rowOf = (id) => state.rows.find((r) => r.id === id);
  const itemRow = (id, sub) => { const r = rowOf(id); return r ? html`<button type="button" class="hrow" data-open="${r.id}">${tileHtml(r, 's')}<span class="hrow-text"><span class="row-title">${r.name}</span><span class="row-sub">${sub || r.user || typeOf(r.type).label}</span></span><sol-icon name="chevron"></sol-icon></button>` : ''; };
  const weak = h.weak || [], reused = h.reused || [], old = h.old || [];
  const score = Math.max(0, Math.min(100, Math.round(h.score ?? 100)));
  const issues = weak.length + reused.reduce((n, g) => n + g.length, 0) + old.length;
  render($('tool'), html`${toolBar('Password health')}
    <div class="t-scroll"><div class="t-col">
      <div class="health-hero" data-tone="${score >= 75 ? 'ok' : score >= 50 ? 'warn' : 'bad'}">
        <span class="big-ring" data-var-p="${score}"><svg viewBox="0 0 120 120" aria-hidden="true"><circle class="track" cx="60" cy="60" r="52"/><circle class="prog" cx="60" cy="60" r="52"/></svg><span class="big-n">${score}</span></span>
        <div><h2>${healthLabel(score)}</h2><p>${issues ? `${issues === 1 ? 'One login needs' : issues + ' logins need'} your attention. Change those passwords to make your vault stronger.` : 'No weak, reused or old passwords. Nice.'}</p></div>
      </div>
      <div class="health-tiles">
        <div class="htile" data-tone="${weak.length ? 'bad' : 'ok'}"><span class="htile-n">${weak.length}</span><span>Weak</span></div>
        <div class="htile" data-tone="${reused.length ? 'bad' : 'ok'}"><span class="htile-n">${reused.length}</span><span>Reused</span></div>
        <div class="htile" data-tone="${old.length ? 'warn' : 'ok'}"><span class="htile-n">${old.length}</span><span>Old</span></div>
      </div>
      ${weak.length ? html`<h3 class="group-title">Weak passwords</h3><div class="group hlist">${weak.map((id) => itemRow(id))}</div><p class="group-footer">Easy to guess. Replace them with generated passwords.</p>` : ''}
      ${reused.map((g, i) => html`<h3 class="group-title">${i === 0 ? 'Reused passwords' : ''}</h3><div class="group hlist">${g.map((id) => itemRow(id, `Same password as ${g.length - 1 === 1 ? 'one other item' : g.length - 1 + ' other items'}`))}</div>`)}
      ${reused.length ? html`<p class="group-footer">If one of these sites leaks, the others are open too.</p>` : ''}
      ${old.length ? html`<h3 class="group-title">Old passwords</h3><div class="group hlist">${old.map((id) => itemRow(id, 'Not changed for over a year'))}</div>` : ''}
    </div></div>`);
}

// ---------- settings ----------

const AUTO_LOCK = [1, 5, 15, 30, 60, 240, 0];
const CLIP = [10, 20, 30, 60, 90, 0];

function drawSettings(focus) {
  const st = state.status || {};
  const opt = (vals, cur, unit) => html`${[...new Set([...vals, cur ?? vals[0]])].sort((a, b) => (a || 1e9) - (b || 1e9)).map((v) => html`<option value="${v}" ${v === cur ? raw('selected') : ''}>${durationLabel(v, unit)}</option>`)}`;
  render($('tool'), html`${toolBar('Settings')}
    <div class="t-scroll"><div class="t-col">
      ${st.orphaned ? html`<div class="banner warn-banner">${svg('shield', 18)}<span>${st.orphaned} entries from another vault (set up on another device while offline) can't be read with this vault's key. They are kept, not deleted.</span></div>` : ''}
      <h3 class="group-title">Security</h3>
      <div class="group">
        <label class="opt"><span class="opt-text">Lock after<small>Without any use, on every device</small></span><select data-set="auto_lock">${opt(AUTO_LOCK, st.auto_lock ?? 15, 'min')}</select></label>
        <label class="opt"><span class="opt-text">Clear copied secrets<small>Only if the clipboard still holds them</small></span><select data-set="clip_clear">${opt(CLIP, st.clip_clear ?? 30, 's')}</select></label>
        <div class="opt"><span class="opt-text">Lock now<small>Ctrl+L</small></span><button type="button" data-s="lock"><sol-icon name="lock"></sol-icon>Lock</button></div>
      </div>
      <h3 class="group-title">Master password</h3>
      <form class="group pwchange" id="pwchange" autocomplete="off">
        <div class="opt col">${pwField('old', 'Current master password')}</div>
        <div class="opt col">${pwField('new', 'New master password', { auto: 'new-password' })}${meterHtml()}</div>
        <div class="opt col">${pwField('new2', 'Confirm new master password', { auto: 'new-password' })}</div>
        <div class="opt"><p class="form-err" role="alert"></p><button type="submit" class="tinted">Change password</button></div>
      </form>
      <p class="group-footer">Your items are not re-encrypted: the vault key is wrapped again with the new password, on every device. Anyone who knew the old password and copied your data before can still read what it held then.</p>
      <h3 class="group-title" id="import-title">Import</h3>
      <div class="group">
        <label class="opt"><span class="opt-text">From</span><select id="imp-format">
          <option value="auto">Detect automatically</option><option value="bitwarden_json">Bitwarden (JSON)</option><option value="bitwarden_csv">Bitwarden (CSV)</option>
          <option value="chrome_csv">Chrome, Edge, Brave (CSV)</option><option value="firefox_csv">Firefox (CSV)</option><option value="1password_csv">1Password (CSV)</option>
          <option value="keepass_csv">KeePass, KeePassXC (CSV)</option><option value="lastpass_csv">LastPass (CSV)</option><option value="raypass_json">Raypass (JSON)</option><option value="raypass_encrypted">Raypass (encrypted)</option></select></label>
        <div class="opt"><span class="opt-text">File<small id="imp-name">JSON or CSV exported from your old password manager</small></span><button type="button" data-s="pick"><sol-icon name="upload"></sol-icon>Choose file…</button></div>
        <input type="file" id="imp-file" accept=".json,.csv,.txt,application/json,text/csv" hidden>
      </div>
      <h3 class="group-title">Export</h3>
      <div class="group">
        <div class="opt"><span class="opt-text">Format</span><div class="segmented" role="group" aria-label="Export format">
          <button type="button" data-exp="encrypted" aria-pressed="true">Encrypted</button><button type="button" data-exp="json" aria-pressed="false">JSON</button><button type="button" data-exp="csv" aria-pressed="false">CSV</button></div></div>
        <div class="opt"><span class="opt-text" id="exp-note">Protected with a password you choose. Import it into Raypass on any device.</span><button type="button" data-s="export"><sol-icon name="download"></sol-icon>Export…</button></div>
      </div>
      <h3 class="group-title">Keyboard</h3>
      <div class="group keys">
        ${[['/', 'Search'], ['↑ ↓', 'Move through the list'], ['Ctrl C', 'Copy the password'], ['Ctrl Shift C', 'Copy the username'], ['Ctrl Alt C', 'Copy the one-time code'], ['N', 'New item'], ['E', 'Edit the item'], ['Ctrl L', 'Lock']].map(([k, d]) => html`<div class="opt"><span class="opt-text">${d}</span><span class="kbd">${k.split(' ').map((x) => html`<kbd>${x}</kbd>`)}</span></div>`)}
      </div>
      <h3 class="group-title">Vault</h3>
      <div class="group">
        <div class="opt"><span class="opt-text">Items</span><span class="row-value">${state.rows.length}</span></div>
        <div class="opt"><span class="opt-text">In Trash</span><span class="row-value">${state.trash.length}</span></div>
        <div class="opt"><span class="opt-text">Key derivation</span><span class="row-value">PBKDF2-SHA256, ${Number(st.kdf_iter || 600000).toLocaleString('en')} rounds</span></div>
        <div class="opt"><span class="opt-text">Encryption</span><span class="row-value">AES-256-GCM</span></div>
      </div>
    </div></div>`);
  const f = $('pwchange');
  wireReveal(f);
  const m = f.querySelector('.meter');
  f.elements.new.addEventListener('input', () => meter(m, f.elements.new.value));
  if (focus === 'import') queueMicrotask(() => $('import-title').scrollIntoView({ block: 'start' }));
}

let expFormat = 'encrypted';
const EXP_NOTES = {
  encrypted: 'Protected with a password you choose. Import it into Raypass on any device.',
  json: 'Not encrypted: everything, passwords included, in plain text. Delete the file when you are done.',
  csv: 'Not encrypted: logins as a spreadsheet other password managers can import. Delete the file when you are done.',
};

async function doExport() {
  let password;
  if (expFormat === 'encrypted') {
    password = await askPassword('Export password', 'Choose a password for the export file. You need it to import the file again.', 'Export', { confirm: true });
    if (password === null) return;
    if (!password) { hooks.toast('Choose a password for the export', 'error'); return; }
  } else if (!await confirmBox('Export without encryption?', 'The file will hold all your passwords in plain text. Anyone who gets it can read them.', 'Export', { danger: true })) return;
  try {
    const r = await rp('export', expFormat === 'encrypted' ? { format: expFormat, password } : { format: expFormat });
    const blob = new Blob([r.data], { type: expFormat === 'csv' ? 'text/csv' : 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url; a.download = r.filename || exportName(expFormat); a.className = 'offscreen';
    document.body.append(a); a.click(); a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
    hooks.toast('Exported');
  } catch (e) { fail(e); }
}

async function doImport(file) {
  const data = await file.text();
  let format = $('imp-format').value;
  const args = { format, data };
  if (format === 'raypass_encrypted' || (format === 'auto' && isEncryptedExport(data))) {
    const pw = await askPassword('Encrypted export', `Enter the password of ${file.name}.`, 'Import');
    if (pw === null) return;
    args.password = pw;
  }
  try {
    const r = await rp('import', args);
    hooks.toast(`Imported ${r.imported} item${r.imported === 1 ? '' : 's'}${r.skipped ? `, skipped ${r.skipped}` : ''}`);
    await hooks.refresh();
  } catch (e) { fail(e); }
}

async function changePassword(f) {
  const err = f.querySelector('.form-err');
  err.textContent = '';
  const old = f.elements.old.value, nw = f.elements.new.value;
  if (!old) { f.elements.old.focus(); return; }
  if (nw.length < 8 || scoreOf(meter(f.querySelector('.meter'), nw).bits) < 2) { err.textContent = 'Choose a stronger new password.'; f.elements.new.focus(); return; }
  if (nw !== f.elements.new2.value) { err.textContent = 'The new passwords do not match.'; f.elements.new2.focus(); return; }
  const btn = f.querySelector('[type=submit]');
  btn.disabled = true; btn.textContent = 'Changing…';
  try {
    await rp('change_password', { old, new: nw });
    for (const n of ['old', 'new', 'new2']) f.elements[n].value = '';
    meter(f.querySelector('.meter'), '');
    hooks.toast('Master password changed');
  } catch (e) {
    err.textContent = e?.code === 'unauthorized' ? 'The current password is wrong.' : e?.message || String(e);
  } finally { btn.disabled = false; btn.textContent = 'Change password'; }
}

function wireSettings(root) {
  root.addEventListener('change', async (e) => {
    if (state.view.kind !== 'settings') return;
    const k = e.target.dataset.set;
    if (k) {
      const r = await act('settings', { [k]: +e.target.value });
      if (r) { state.status = { ...state.status, ...r }; hooks.toast('Saved'); hooks.armIdle(); }
      return;
    }
    if (e.target.id === 'imp-file' && e.target.files[0]) {
      const file = e.target.files[0];
      $('imp-name').textContent = file.name;
      await doImport(file);
      e.target.value = '';
    }
  });
  root.addEventListener('click', (e) => {
    if (state.view.kind !== 'settings') return;
    const x = e.target.closest('[data-exp]');
    if (x) {
      expFormat = x.dataset.exp;
      for (const b of root.querySelectorAll('[data-exp]')) b.setAttribute('aria-pressed', String(b === x));
      $('exp-note').textContent = EXP_NOTES[expFormat];
      return;
    }
    const s = e.target.closest('[data-s]');
    if (!s) return;
    if (s.dataset.s === 'lock') lockNow();
    if (s.dataset.s === 'pick') $('imp-file').click();
    if (s.dataset.s === 'export') doExport();
  });
  root.addEventListener('submit', (e) => { if (e.target.id === 'pwchange') { e.preventDefault(); changePassword(e.target); } });
}

// ---------- entry points ----------

/** Paints the tool for state.view (generator, health, settings). */
export function drawTool(focus) {
  const k = state.view.kind;
  if (k === 'generator') drawGenerator();
  else if (k === 'health') drawHealth();
  else if (k === 'settings') { expFormat = 'encrypted'; drawSettings(focus); }
}

/** Reloads the health report (after a change) and repaints if it is on screen. */
export const refreshHealth = loadHealth;

/** Wires the tool pane once. */
export function wireTools() {
  const root = $('tool');
  wireGenerator(root);
  wireSettings(root);
  root.addEventListener('click', (e) => {
    const o = e.target.closest('[data-open]');
    if (o) hooks.openItem(o.dataset.open);
    if (e.target.closest('[data-t="back"]')) hooks.back();
  });
}
