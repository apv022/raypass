// Raypass UI core: the one place that calls actions, holds state, copies to the clipboard and
// locks. Invariants:
//  - every operation is a raypass.* action through sol.js (no other network);
//  - secrets (a revealed item, an edit draft, a generated password) live only in `state` in
//    memory and in the DOM while shown; wipe() drops all of them, and it runs on every lock,
//    including the `locked` error from any call;
//  - localStorage only ever holds UI preferences (generator options, sidebar), never items,
//    names or secrets;
//  - data reaches the DOM through html`` (escaped) or textContent only.
import { call, html, render } from '/sol.js';
import { toast } from '/kit/kit.js';
import { isLocked, clipDecision } from '/lib.js';

/** The UI state. `rows`/`trash` are list rows (no secrets); `item` is a revealed item. */
export const state = {
  status: null,          // raypass.status output
  rows: [],              // raypass.list (not trashed)
  trash: [],             // raypass.list {trash: true}
  view: { kind: 'all' }, // all | fav | type | folder | trash | generator | health | settings
  query: '',
  sel: null,             // selected item id
  item: null,            // the selected item, from raypass.get (secrets)
  reveal: new Set(),     // revealed field keys of the selected item
  edit: null,            // {id|null, type, draft} while editing or adding
  totp: null,            // {id, code, period, remaining, digits, at}
  gen: null,             // last generated password {password, entropy_bits}
  health: null,          // raypass.health output
  loaded: false,
};

/** Callbacks the app module sets: draw() repaints the vault, locked() shows the unlock screen. */
export const hooks = { draw() {}, locked() {} };

// ---------- preferences (UI only, never vault data) ----------

/** Per-browser UI preferences in localStorage; failures are ignored (private mode). */
export const prefs = {
  get(k, d) { try { const v = localStorage.getItem('raypass-' + k); return v === null ? d : JSON.parse(v); } catch { return d; } },
  set(k, v) { try { localStorage.setItem('raypass-' + k, JSON.stringify(v)); } catch { /* optional */ } },
};

// ---------- actions ----------

/**
 * Calls raypass.<name>. A `locked` answer wipes every secret and shows the unlock screen, then
 * rethrows (callers usually ignore it: the screen already says what happened). Every status
 * answer re-arms the auto-lock check from its `locks_in`.
 */
export async function rp(name, args) {
  try {
    const out = await call('raypass.' + name, args === undefined ? {} : args);
    if (name === 'status') { state.status = out; noteStatus(out); }
    else if (!NOT_USE.has(name)) {
      lockAt = 0; // the app re-armed its timer: auto_lock minutes from now
      if (name === 'settings' && state.status && out) state.status = { ...state.status, ...out };
    }
    armIdle();
    return out;
  } catch (e) {
    if (isLocked(e)) { wipe(); hooks.locked(); }
    throw e;
  }
}

/** Shows an error as a toast, except `locked` (the unlock screen is the message). */
export function fail(e) {
  if (isLocked(e)) return;
  toast(e?.message || String(e), { kind: 'error' });
}

/** Runs an action, toasting failures; resolves to its output or null. */
export async function act(name, args) {
  try { return await rp(name, args); } catch (e) { fail(e); return null; }
}

// ---------- lock and wipe ----------

/** Drops every secret the UI holds: revealed item, drafts, codes, generated passwords, dialogs. */
export function wipe() {
  state.item = null; state.edit = null; state.totp = null; state.gen = null; state.health = null;
  state.reveal.clear();
  state.rows = []; state.trash = []; state.loaded = false;
  stopTotp();
  clearTimeout(idleTimer); idleTimer = 0; lockAt = 0;
  for (const d of document.querySelectorAll('sol-dialog, sol-sheet')) { try { d.close(); } catch { /* not open */ } d.remove(); }
  for (const id of ['detail', 'tool', 'rows']) { const n = document.getElementById(id); if (n) n.replaceChildren(); }
}

/** Locks the vault now (raypass.lock), then shows the unlock screen. */
export async function lockNow() {
  wipe();
  try { await call('raypass.lock', {}); } catch { /* locked or unreachable: either way we are locked here */ }
  hooks.locked();
}

// Auto-lock: the app locks itself after s.auto_lock minutes without use, for every client at
// once; raypass.status says how many seconds are left (`locks_in`, and status itself does not
// count as use). This page never locks the vault on its own idleness (another client, such as
// the browser extension, may be using it): it arms one timer for the moment the app will lock,
// then asks status once. Locked: it wipes and shows the unlock screen. Not locked (someone used
// the vault meanwhile): it re-arms from the new `locks_in`. One check per lock deadline, never a
// poll.
let idleTimer = 0;
let lockAt = 0; // ms timestamp when the app will auto-lock, 0 = unknown or never
/** Actions that do not count as use of the vault (the app does not re-arm auto-lock for them). */
const NOT_USE = new Set(['status', 'generate', 'lock']);

/** Records the lock deadline from a raypass.status answer. */
function noteStatus(s) {
  lockAt = s && !s.locked && s.locks_in > 0 ? Date.now() + s.locks_in * 1000 : 0;
}

/** Re-arms the auto-lock check from the last known deadline (or the setting, before one is known). */
export function armIdle() {
  clearTimeout(idleTimer);
  idleTimer = 0;
  const s = state.status;
  if (!s || s.locked || !s.initialized || !s.auto_lock) { lockAt = 0; return; }
  if (!lockAt) lockAt = Date.now() + s.auto_lock * 60e3;
  idleTimer = setTimeout(checkLocked, Math.max(1000, lockAt - Date.now() + 500));
}

async function checkLocked() {
  idleTimer = 0;
  let s;
  try { s = await call('raypass.status', {}); } catch (e) {
    if (isLocked(e)) { wipe(); hooks.locked(); }
    return;
  }
  state.status = s;
  noteStatus(s);
  if (s.locked) { wipe(); hooks.locked(); return; }
  armIdle();
}

// ---------- TOTP timer (one at a time, for the item on screen) ----------

let totpTick = 0;
/** Stops the code countdown. */
export function stopTotp() { clearInterval(totpTick); totpTick = 0; }
/** Starts the once-a-second countdown; `onTick` repaints the ring, `onExpire` fetches a new code. */
export function startTotp(onTick) { stopTotp(); totpTick = setInterval(onTick, 250); }

// ---------- clipboard ----------

let clip = null; // {value, timer}
async function writeClip(text) {
  if (navigator.clipboard?.writeText) { await navigator.clipboard.writeText(text); return; }
  const t = document.createElement('textarea');
  t.value = text; t.setAttribute('readonly', ''); t.className = 'offscreen';
  document.body.append(t); t.select();
  const ok = document.execCommand('copy');
  t.remove();
  if (!ok) throw new Error('Copying is not allowed here');
}

/**
 * Copies `value`. Secrets are cleared from the clipboard after s.clip_clear seconds, but only if
 * the clipboard still holds what we put there (lib.js clipDecision).
 */
export async function copy(value, label, { secret = true } = {}) {
  if (value === undefined || value === null || value === '') return;
  try { await writeClip(String(value)); } catch (e) { toast('Could not copy: ' + (e?.message || e), { kind: 'error' }); return; }
  const secs = state.status?.clip_clear ?? 30;
  if (clip) clearTimeout(clip.timer);
  clip = null;
  if (secret && secs > 0) {
    clip = { value: String(value), timer: setTimeout(clearClip, secs * 1000) };
    toast(`${label} copied · clears in ${secs} s`);
  } else toast(`${label} copied`);
}

async function clearClip() {
  if (!clip) return;
  let readable = false, current = '';
  try { current = await navigator.clipboard.readText(); readable = true; } catch { readable = false; }
  const what = clipDecision({ readable, current, ours: clip.value, focused: document.hasFocus() });
  if (what === 'defer') { addEventListener('focus', clearClip, { once: true }); return; }
  if (what === 'clear') { try { await writeClip(''); } catch { /* nothing more we can do */ } }
  clip = null;
}

// ---------- dialogs ----------

/** Opens a <sol-dialog> with `body` (html``) and resolves with what `wire(dlg, done)` passes to done. */
export function dialog(heading, body, wire) {
  return new Promise((resolve) => {
    const d = document.createElement('sol-dialog');
    d.setAttribute('heading', heading);
    const box = document.createElement('div');
    render(box, body);
    d.append(box);
    let result = null;
    const done = (v) => { result = v; d.close(); };
    d.addEventListener('close', () => { d.remove(); resolve(result); });
    document.body.append(d);
    wire(d, done);
    queueMicrotask(() => { d.show(); d.querySelector('[autofocus]')?.focus(); });
  });
}

/** Asks to confirm; resolves true or false. */
export async function confirmBox(heading, message, okLabel, { danger = false } = {}) {
  const r = await dialog(heading, html`<p class="dlg-text">${message}</p>
    <div class="dlg-actions"><button type="button" data-v="no">Cancel</button><button type="button" class="${danger ? 'danger primary' : 'primary'}" data-v="yes" autofocus>${okLabel}</button></div>`,
  (d, done) => d.addEventListener('click', (e) => { const b = e.target.closest('[data-v]'); if (b) done(b.dataset.v === 'yes'); }));
  return r === true;
}

/** Asks for a password; resolves to it, or null when cancelled. Never trimmed (DESIGN.md). */
export async function askPassword(heading, message, okLabel, { confirm = false } = {}) {
  return dialog(heading, html`<form class="dlg-form" autocomplete="off">
      <p class="dlg-text">${message}</p>
      <label>Password<input name="p1" type="password" autocomplete="new-password" autofocus required></label>
      ${confirm ? html`<label>Confirm<input name="p2" type="password" autocomplete="new-password" required></label>` : ''}
      <p class="form-err" role="alert"></p>
      <div class="dlg-actions"><button type="button" data-v="no">Cancel</button><button type="submit" class="primary">${okLabel}</button></div>
    </form>`,
  (d, done) => {
    const f = d.querySelector('form');
    d.querySelector('[data-v="no"]').addEventListener('click', () => done(null));
    f.addEventListener('submit', (e) => {
      e.preventDefault();
      const p1 = f.p1.value;
      if (confirm && p1 !== f.p2.value) { f.querySelector('.form-err').textContent = 'The passwords do not match.'; return; }
      done(p1);
    });
  });
}
