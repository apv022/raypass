// The setup and unlock screens, and the strength meter they share with the editor and settings.
// The master password is read from the field at submit, sent with raypass.setup / unlock, and
// the field is cleared right after: the page never keeps it.
import { html, render } from '/sol.js';
import { strength } from '/lib.js';
import { svg } from '/icons.js';
import { state, rp, fail } from '/core.js';

const $ = (id) => document.getElementById(id);

/** Markup for a strength meter; call meter(el, password) to update it. */
export const meterHtml = () => html`<div class="meter" data-score="none" aria-live="polite"><div class="meter-bar"><i></i><i></i><i></i><i></i></div><span class="meter-label"></span></div>`;

/** Updates a meter element for a password (or for given bits, from the generator). */
export function meter(el, pw, bitsOverride) {
  if (!el) return null;
  const s = bitsOverride !== undefined ? { bits: bitsOverride, ...strengthFromBits(bitsOverride) } : strength(pw);
  const empty = bitsOverride === undefined && !pw;
  el.dataset.score = empty ? 'none' : String(s.score);
  el.querySelector('.meter-label').textContent = empty ? '' : `${s.label} · ${Math.round(s.bits)} bits`;
  return s;
}
function strengthFromBits(bits) {
  const score = bits < 28 ? 0 : bits < 40 ? 1 : bits < 60 ? 2 : bits < 80 ? 3 : 4;
  return { score, label: ['Very weak', 'Weak', 'Fair', 'Strong', 'Very strong'][score] };
}

/** A password field with a reveal toggle. */
export const pwField = (name, label, { auto = 'current-password', autofocus = false, value = '' } = {}) => html`
  <div class="pw-field">
    <input name="${name}" type="password" autocomplete="${auto}" spellcheck="false" autocapitalize="off" aria-label="${label}" placeholder="${label}" value="${value}" ${autofocus ? html`autofocus` : ''}>
    <button type="button" class="ghost icon-btn reveal-btn" data-reveal="${name}" aria-label="Show password" aria-pressed="false"><sol-icon name="eye"></sol-icon></button>
  </div>`;

/** Wires every reveal toggle under root (type=password ↔ text). */
export function wireReveal(root) {
  root.addEventListener('click', (e) => {
    const b = e.target.closest('[data-reveal]');
    if (!b) return;
    const input = b.parentElement.querySelector('input');
    const show = input.type === 'password';
    input.type = show ? 'text' : 'password';
    b.setAttribute('aria-pressed', String(show));
    b.setAttribute('aria-label', show ? 'Hide password' : 'Show password');
    render(b, show ? svgHtml('eye-off') : html`<sol-icon name="eye"></sol-icon>`);
  });
}
const svgHtml = (n) => html`${svg(n, 18)}`;

/** Shows the first-run screen. */
export function showSetup(onDone) {
  render($('gate'), html`<form class="gate-card" id="setup" autocomplete="off" novalidate>
    <div class="gate-icon"><img src="/icon.svg" alt="" width="72" height="72"></div>
    <h1>Welcome to Raypass</h1>
    <p class="gate-sub">Choose a master password. It is the only key to your vault: nobody, not even Raypass, can recover it if you forget it.</p>
    <div class="gate-fields">
      ${pwField('pw', 'Master password', { auto: 'new-password', autofocus: true })}
      ${meterHtml()}
      ${pwField('pw2', 'Confirm master password', { auto: 'new-password' })}
    </div>
    <p class="form-err" role="alert"></p>
    <button class="primary large wide" type="submit">Create vault</button>
    <button class="plain" type="button" id="suggest">${svg('dice', 16)}<span>Suggest a passphrase</span></button>
    <p class="gate-foot">Your vault is encrypted on this device with AES-256. Sol only ever syncs ciphertext between your devices.</p>
  </form>`);
  const f = $('setup'), err = f.querySelector('.form-err');
  wireReveal(f);
  const m = f.querySelector('.meter');
  f.pw.addEventListener('input', () => { meter(m, f.pw.value); err.textContent = ''; });
  f.pw2.addEventListener('input', () => { err.textContent = ''; });
  $('suggest').addEventListener('click', async () => {
    try {
      const g = await rp('generate', { mode: 'words', words: 5, separator: '-', capitalize: true, number: true });
      for (const n of ['pw', 'pw2']) { f[n].value = g.password; f[n].type = 'text'; }
      for (const b of f.querySelectorAll('[data-reveal]')) b.setAttribute('aria-pressed', 'true');
      meter(m, g.password);
      err.textContent = 'Write it down and keep it somewhere safe.';
      err.dataset.kind = 'info';
    } catch (e) { fail(e); }
  });
  f.addEventListener('submit', async (e) => {
    e.preventDefault();
    delete err.dataset.kind;
    const pw = f.pw.value;
    const s = strength(pw);
    if (pw.length < 8) { err.textContent = 'Use at least 8 characters.'; f.pw.focus(); return; }
    if (s.score < 2) { err.textContent = 'This password is easy to guess. Make it longer, or use a passphrase of four or more words.'; f.pw.focus(); return; }
    if (pw !== f.pw2.value) { err.textContent = 'The passwords do not match.'; f.pw2.focus(); return; }
    const btn = f.querySelector('[type=submit]');
    btn.disabled = true; btn.textContent = 'Creating your vault…';
    try {
      await rp('setup', { password: pw });
      f.pw.value = ''; f.pw2.value = '';
      onDone();
    } catch (x) {
      btn.disabled = false; btn.textContent = 'Create vault';
      err.textContent = x?.message || String(x);
    }
  });
}

/** Shows the unlock screen. */
export function showUnlock(onDone) {
  const n = state.status?.items;
  render($('gate'), html`<form class="gate-card" id="unlock" autocomplete="off">
    <div class="gate-icon locked"><img src="/icon.svg" alt="" width="72" height="72"></div>
    <h1>Raypass is locked</h1>
    <p class="gate-sub">${typeof n === 'number' && n > 0 ? `Enter your master password to open ${n === 1 ? '1 item' : n + ' items'}.` : 'Enter your master password.'}</p>
    <div class="gate-fields">${pwField('pw', 'Master password', { autofocus: true })}</div>
    <p class="form-err" role="alert"></p>
    <button class="primary large wide" type="submit">${svg('unlock', 18)}<span>Unlock</span></button>
  </form>`);
  const f = $('unlock'), err = f.querySelector('.form-err');
  wireReveal(f);
  f.pw.addEventListener('input', () => { err.textContent = ''; f.classList.remove('shake'); });
  f.addEventListener('submit', async (e) => {
    e.preventDefault();
    const pw = f.pw.value;
    if (!pw) { f.pw.focus(); return; }
    const btn = f.querySelector('[type=submit]');
    btn.disabled = true;
    render(btn, html`<span>Unlocking…</span>`);
    try {
      await rp('unlock', { password: pw });
      f.pw.value = '';
      onDone();
    } catch (x) {
      btn.disabled = false;
      render(btn, html`${svg('unlock', 18)}<span>Unlock</span>`);
      f.pw.select();
      err.textContent = x?.code === 'unauthorized' ? 'Wrong password. Try again.' : (x?.message || String(x));
      f.classList.remove('shake'); void f.offsetWidth; f.classList.add('shake');
    }
  });
  queueMicrotask(() => f.pw.focus());
}
