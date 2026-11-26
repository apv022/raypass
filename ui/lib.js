// Raypass UI helpers: pure functions with no DOM and no network, so ui/test can check them in a
// real browser. Nothing here keeps state; nothing here logs. Secrets may pass through these
// functions (strength, character runs) but are never stored or sent anywhere.

// ---------- item types ----------

/** The five item types, in sidebar order, with their labels and glyphs (ui/icons.js). */
export const TYPES = [
  { id: 'login', label: 'Login', plural: 'Logins', icon: 'key' },
  { id: 'note', label: 'Secure note', plural: 'Notes', icon: 'note' },
  { id: 'card', label: 'Card', plural: 'Cards', icon: 'card' },
  { id: 'identity', label: 'Identity', plural: 'Identities', icon: 'identity' },
  { id: 'wallet', label: 'Crypto wallet', plural: 'Wallets', icon: 'wallet' },
];
/** The type record for an id (logins for anything unknown, so a newer item type still shows). */
export const typeOf = (id) => TYPES.find((t) => t.id === id) || TYPES[0];

/** The fields each type edits, besides the common ones (name, folder, fav, tags, notes, fields). */
export const TYPE_FIELDS = {
  login: ['user', 'pass', 'urls', 'totp'],
  note: [],
  card: ['holder', 'number', 'exp', 'cvv', 'brand'],
  identity: ['first', 'last', 'email', 'phone', 'address', 'company'],
  wallet: ['network', 'address', 'seed', 'passphrase', 'privkey', 'derivation'],
};
/** Fields common to every type. */
export const COMMON_FIELDS = ['name', 'folder', 'fav', 'tags', 'notes', 'fields'];
/** Fields that are secret: masked until revealed, never shown in a list. */
export const SECRET_FIELDS = new Set(['pass', 'totp', 'number', 'cvv', 'seed', 'passphrase', 'privkey']);

// ---------- errors ----------

/** True for the error every vault action answers while locked (403 forbidden, message "locked"). */
export function isLocked(err) {
  return !!err && err.code === 'forbidden' && err.message === 'locked';
}

// ---------- strength ----------

const COMMON = new Set(('password passw0rd 123456 12345678 123456789 1234567890 qwerty qwertyuiop abc123 letmein ' +
  'welcome monkey dragon football baseball iloveyou admin login master hello freedom whatever ' +
  'trustno1 sunshine princess shadow superman michael charlie jessica 000000 111111 123123 ' +
  'changeme secret raypass password1 qwerty123 1q2w3e4r zaq12wsx asdfgh asdfghjkl starwars ' +
  'computer internet summer winter spring autumn love test guest root toor default pass').split(' '));
const ROWS = ['qwertyuiopasdfghjklzxcvbnm', '1234567890', 'abcdefghijklmnopqrstuvwxyz', 'qwertzuiop', 'azertyuiop'];

function poolOf(s) {
  let pool = 0;
  if (/[a-z]/.test(s)) pool += 26;
  if (/[A-Z]/.test(s)) pool += 26;
  if (/[0-9]/.test(s)) pool += 10;
  if (/[^a-zA-Z0-9]/.test(s)) pool += /[^\x20-\x7e]/.test(s) ? 100 : 33;
  return pool;
}

/** Counts characters that add little: repeats and steps along a row or the alphabet. */
function predictable(s) {
  const low = s.toLowerCase();
  let weak = 0;
  for (let i = 1; i < low.length; i++) {
    const a = low[i - 1], b = low[i];
    if (a === b) { weak++; continue; }
    const step = b.charCodeAt(0) - a.charCodeAt(0);
    if (Math.abs(step) === 1 && /[a-z0-9]/.test(a) && /[a-z0-9]/.test(b)) { weak++; continue; }
    if (ROWS.some((r) => { const j = r.indexOf(a); return j >= 0 && r[j + 1] === b; })) weak++;
  }
  return weak;
}

/**
 * Estimates a password's strength: {bits, score 0..4, label}. It is a deliberately simple
 * guesser (pool size times length, discounted for repeats, sequences, keyboard runs, common
 * passwords and word lists), good enough to steer people away from weak choices.
 */
export function strength(pw) {
  const s = String(pw ?? '');
  if (!s) return { bits: 0, score: 0, label: '' };
  const chars = [...s];
  const pool = poolOf(s);
  const eff = Math.max(1, chars.length - predictable(s) * 0.75);
  let bits = eff * Math.log2(Math.max(pool, 2));
  // Passphrases: words of letters split by one separator are worth a word list's entropy each.
  const words = s.split(/[^\p{L}]+/u).filter(Boolean);
  const letters = words.join('').length;
  if (words.length >= 3 && words.every((w) => w.length >= 3) && letters >= chars.length * 0.7) {
    bits = Math.min(bits, words.length * 12.9 + (/[0-9]/.test(s) ? 3.3 : 0) + (/[A-Z]/.test(s) ? 1 : 0));
  }
  const bare = s.toLowerCase().replace(/[0-9!@#$%^&*.?_-]+$/, '').replace(/^[0-9]+/, '');
  if (COMMON.has(s.toLowerCase()) || COMMON.has(bare) || COMMON.has(leet(bare))) bits = Math.min(bits, 8 + (s.length - bare.length) * 2);
  if (/^(.)\1*$/.test(s)) bits = Math.min(bits, 4);
  bits = Math.round(bits * 10) / 10;
  const score = scoreOf(bits);
  return { bits, score, label: LABELS[score] };
}

const LABELS = ['Very weak', 'Weak', 'Fair', 'Strong', 'Very strong'];
const leet = (s) => s.replace(/0/g, 'o').replace(/1/g, 'l').replace(/3/g, 'e').replace(/4|@/g, 'a').replace(/5|\$/g, 's').replace(/7/g, 't');
/** The 0..4 score for an entropy in bits (the generator's entropy_bits use the same scale). */
export function scoreOf(bits) {
  return bits < 28 ? 0 : bits < 40 ? 1 : bits < 60 ? 2 : bits < 80 ? 3 : 4;
}
/** The word for a score. */
export const scoreLabel = (score) => LABELS[Math.max(0, Math.min(4, score | 0))];

// ---------- character runs (passwords shown with digits and symbols coloured) ----------

/** Splits a string into runs of letters, digits and symbols: [{kind, text}]. */
export function charRuns(s) {
  const out = [];
  for (const c of String(s ?? '')) {
    const kind = /[0-9]/.test(c) ? 'digit' : /[\p{L}\p{M}]/u.test(c) ? 'letter' : 'symbol';
    const last = out[out.length - 1];
    if (last && last.kind === kind) last.text += c; else out.push({ kind, text: c });
  }
  return out;
}

// ---------- TOTP ----------

/** The ring for a TOTP code: {value 0..100 (time left), urgent (5 s or less), text}. */
export function ringState(remaining, period) {
  const p = Number(period) > 0 ? Number(period) : 30;
  const r = Math.max(0, Math.min(p, Number(remaining) || 0));
  return { value: Math.round((r / p) * 1000) / 10, urgent: r <= 5, text: String(Math.ceil(r)) };
}

/** Seconds left at `now` for a code fetched at `fetchedAt` with `remaining` seconds left then. */
export function remainingAt(remaining, fetchedAt, now) {
  return Math.max(0, remaining - (now - fetchedAt) / 1000);
}

/** Groups a code for reading: 123 456, 1234 5678, 123 4567; Steam's 5 letters stay whole. */
export function formatCode(code) {
  const c = String(code ?? '');
  if (!/^[0-9]+$/.test(c) || c.length < 6) return c;
  const cut = Math.floor(c.length / 2);
  return c.slice(0, cut) + ' ' + c.slice(cut);
}

// ---------- URLs ----------

/** Parses a stored URL the way the app matches them (default scheme https). null if unusable. */
export function parseUrl(u) {
  const s = String(u ?? '').trim();
  if (!s) return null;
  try { return new URL(/^[a-z][a-z0-9+.-]*:/i.test(s) ? s : 'https://' + s); } catch { return null; }
}

/** The host to show for a URL ("github.com" for https://www.github.com/login, the package for an Android app). */
export function hostOf(u) {
  const p = parseUrl(u);
  if (!p) return String(u ?? '').trim();
  if (p.protocol === 'androidapp:') return (p.host || p.pathname.replace(/^\/+/, '')) || String(u);
  const h = p.hostname.replace(/^www\./, '');
  return h + (p.port ? ':' + p.port : '');
}

/** A URL safe to open in a new tab (http or https only), or null: never javascript:, data: or app schemes. */
export function openableUrl(u) {
  const p = parseUrl(u);
  return p && (p.protocol === 'https:' || p.protocol === 'http:') ? p.href : null;
}

// ---------- dates ----------

const MIN = 60e3, HOUR = 60 * MIN, DAY = 24 * HOUR;
const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

/** A short absolute date: "3 Mar 2026", with the time when `withTime`. */
export function formatDate(ms, withTime = false) {
  const n = Number(ms);
  if (!n) return '';
  const d = new Date(n);
  const s = `${d.getDate()} ${MONTHS[d.getMonth()]} ${d.getFullYear()}`;
  return withTime ? `${s}, ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}` : s;
}

/** "just now", "5 min ago", "3 h ago", "yesterday", "4 days ago", else the date. */
export function relativeTime(ms, now = Date.now()) {
  const n = Number(ms);
  if (!n) return '';
  const d = now - n;
  if (d < MIN) return 'just now';
  if (d < HOUR) return `${Math.floor(d / MIN)} min ago`;
  if (d < DAY) return `${Math.floor(d / HOUR)} h ago`;
  if (d < 2 * DAY) return 'yesterday';
  if (d < 7 * DAY) return `${Math.floor(d / DAY)} days ago`;
  return formatDate(n);
}

/** "30 seconds", "1 minute", "15 minutes", "4 hours", "Never" for 0 (settings labels). */
export function durationLabel(n, unit) {
  const v = Number(n) || 0;
  if (v === 0) return 'Never';
  let secs = unit === 'min' ? v * 60 : v;
  if (secs % 3600 === 0) { const h = secs / 3600; return `${h} hour${h === 1 ? '' : 's'}`; }
  if (secs % 60 === 0) { const m = secs / 60; return `${m} minute${m === 1 ? '' : 's'}`; }
  return `${secs} second${secs === 1 ? '' : 's'}`;
}

// ---------- names and pictures ----------

/** The letter for a monogram tile: the first letter or digit of the name (or of the host). */
export function monogram(name, url) {
  for (const s of [name, url ? hostOf(url) : '']) {
    const m = String(s ?? '').match(/[\p{L}\p{N}]/u);
    if (m) return m[0].toLocaleUpperCase();
  }
  return '?';
}

/** A stable hue (0..359) for a string, so an item keeps its colour everywhere. */
export function hueOf(s) {
  let h = 0;
  for (const c of String(s ?? '')) h = (h * 31 + c.codePointAt(0)) % 360;
  return h;
}

/** The second line of a list row: the user, else the website, else what the item is. */
export function rowSubtitle(row) {
  if (!row) return '';
  if (row.user) return row.user;
  const u = (row.urls || []).find(Boolean);
  if (u) return hostOf(u);
  return typeOf(row.type).label;
}

// ---------- cards ----------

const BRANDS = [
  ['amex', 'American Express', /^3[47]/],
  ['visa', 'Visa', /^4/],
  ['mastercard', 'Mastercard', /^(5[1-5]|2(2[2-9][1-9]|2[3-9]|[3-6]|7[01]|720))/],
  ['discover', 'Discover', /^(6011|65|64[4-9])/],
  ['diners', 'Diners Club', /^(36|38|30[0-5])/],
  ['jcb', 'JCB', /^35(2[89]|[3-8])/],
  ['unionpay', 'UnionPay', /^62/],
  ['maestro', 'Maestro', /^(5018|5020|5038|6304|6759|676[1-3])/],
];
/** The card brand for a number: {id, name}, or null. */
export function cardBrand(number) {
  const d = String(number ?? '').replace(/\D/g, '');
  if (d.length < 2) return null;
  for (const [id, name, re] of BRANDS) if (re.test(d)) return { id, name };
  return null;
}
/** The number in reading groups: 4-4-4-4, American Express 4-6-5. */
export function formatCardNumber(number) {
  const d = String(number ?? '').replace(/\s+/g, '');
  if (!/^\d+$/.test(d)) return String(number ?? '');
  if (cardBrand(d)?.id === 'amex') return [d.slice(0, 4), d.slice(4, 10), d.slice(10)].filter(Boolean).join(' ');
  return d.replace(/(.{4})(?=.)/g, '$1 ');
}
/** "•••• 4242" (the last four digits only). */
export function maskCard(number) {
  const d = String(number ?? '').replace(/\D/g, '');
  return d.length >= 4 ? '•••• ' + d.slice(-4) : '••••';
}
/** The Luhn check; true for a plausible card number. */
export function luhn(number) {
  const d = String(number ?? '').replace(/\D/g, '');
  if (d.length < 12) return false;
  let sum = 0;
  for (let i = 0; i < d.length; i++) {
    let n = +d[d.length - 1 - i];
    if (i % 2) { n *= 2; if (n > 9) n -= 9; }
    sum += n;
  }
  return sum % 10 === 0;
}
/** Normalizes an expiry typed as 3/27, 03/2027, 0327 into MM/YY; returns the input if it can't. */
export function normExpiry(s) {
  const t = String(s ?? '').trim();
  let m = t.match(/^(\d{1,2})\s*[/\-. ]\s*(\d{2}|\d{4})$/) || t.match(/^(\d{2})(\d{2})$/);
  if (!m) return t;
  const mm = +m[1];
  if (mm < 1 || mm > 12) return t;
  return String(mm).padStart(2, '0') + '/' + m[2].slice(-2);
}

// ---------- wallets ----------

/** The words of a seed phrase (split on any whitespace). */
export function seedWords(seed) {
  return String(seed ?? '').trim().split(/\s+/).filter(Boolean);
}
/** An address shortened in the middle for a list or a heading: 0x12ab…89ef. */
export function shortAddress(a, keep = 6) {
  const s = String(a ?? '');
  return s.length <= keep * 2 + 1 ? s : s.slice(0, keep) + '…' + s.slice(-keep);
}

// ---------- tags, search, filters ----------

/** "a, b ,a" → ["a", "b"]: trimmed, de-duplicated, order kept. */
export function splitTags(s) {
  const out = [];
  for (const t of String(s ?? '').split(',')) { const v = t.trim(); if (v && !out.includes(v)) out.push(v); }
  return out;
}

/** True when every word of the query is in the row's name, user, websites, folder or tags. */
export function matchesQuery(row, query) {
  const words = String(query ?? '').toLowerCase().split(/\s+/).filter(Boolean);
  if (!words.length) return true;
  const hay = [row.name, row.user, ...(row.urls || []), row.folder, ...(row.tags || []), typeOf(row.type).plural].join('\n').toLowerCase();
  return words.every((w) => hay.includes(w));
}

/** True when the row belongs to a view: {kind: all|fav|type|folder|trash, type?, name?}. */
export function inView(row, view) {
  switch (view?.kind) {
    case 'fav': return !!row.fav;
    case 'type': return row.type === view.type;
    case 'folder': return row.folder === view.name || String(row.folder || '').startsWith(view.name + '/');
    default: return true;
  }
}

/**
 * Folders as a tree for the sidebar: [{name (full path), label (last part), depth, count}],
 * sorted by path. Parents of "Work/Servers" appear even when nothing is directly in them.
 */
export function folderTree(rows) {
  const counts = new Map();
  for (const r of rows || []) {
    const f = String(r.folder || '').replace(/^\/+|\/+$/g, '');
    if (!f) continue;
    const parts = f.split('/').filter(Boolean);
    for (let i = 1; i <= parts.length; i++) {
      const p = parts.slice(0, i).join('/');
      counts.set(p, (counts.get(p) || 0) + 1);
    }
  }
  return [...counts.keys()].sort((a, b) => a.localeCompare(b, undefined, { sensitivity: 'base' }))
    .map((name) => { const parts = name.split('/'); return { name, label: parts[parts.length - 1], depth: parts.length - 1, count: counts.get(name) }; });
}

// ---------- editing ----------

const isEmpty = (v) => v === undefined || v === null || v === '' || (Array.isArray(v) && v.length === 0);
const same = (a, b) => JSON.stringify(a ?? null) === JSON.stringify(b ?? null);

/**
 * The raypass.update patch from an item and its edited copy: only changed fields, a cleared
 * field as null (the action's way to clear), nothing for fields that were and stay empty.
 */
export function diffItem(orig, edited, fields) {
  const patch = {};
  for (const f of fields) {
    const a = orig ? orig[f] : undefined, b = edited[f];
    if (b === false) { if (a) patch[f] = false; continue; }
    if (isEmpty(b)) { if (!isEmpty(a) && a !== false) patch[f] = null; continue; }
    if (!same(a, b)) patch[f] = b;
  }
  return patch;
}

/** The raypass.add arguments for a new item: type, name and every non-empty field. */
export function addArgs(type, edited, fields) {
  const args = { type, name: edited.name };
  for (const f of fields) if (f !== 'name' && !isEmpty(edited[f]) && edited[f] !== false) args[f] = edited[f];
  return args;
}

// ---------- clipboard ----------

/**
 * What to do when a copied secret's time is up: "clear" when the clipboard still holds it,
 * "keep" when the person has copied something else since, "defer" when the clipboard can't be
 * read now (the page is in the background: try again when it is focused), and "clear" when it
 * can't be read even with focus (no permission): leaving a password behind is the worse risk.
 */
export function clipDecision({ readable, current, ours, focused }) {
  if (readable) return current === ours ? 'clear' : 'keep';
  return focused ? 'clear' : 'defer';
}

// ---------- import / export ----------

/**
 * Whether a picked file's bytes must go to raypass.import as base64 (encoding "base64") rather
 * than text: a zip (PK\x03\x04, such as Proton Pass's export), any file with a NUL byte in its
 * first 4 KiB, or bytes that are not UTF-8. Text formats (JSON, CSV) stay text.
 */
export function isBinaryImport(bytes) {
  if (bytes.length >= 4 && bytes[0] === 0x50 && bytes[1] === 0x4b && bytes[2] === 0x03 && bytes[3] === 0x04) return true;
  if (bytes.subarray(0, 4096).includes(0)) return true;
  try { new TextDecoder('utf-8', { fatal: true }).decode(bytes); return false; } catch { return true; }
}

/** Base64 of bytes, in chunks so a large file doesn't overflow the argument limit. */
export function bytesToBase64(bytes) {
  let s = '';
  for (let i = 0; i < bytes.length; i += 0x8000) s += String.fromCharCode.apply(null, bytes.subarray(i, i + 0x8000));
  return btoa(s);
}

/** The toast after an import: counts, then what could not be imported (r.notes). */
export function importSummary(r) {
  const n = r.imported ?? 0;
  let s = `Imported ${n} item${n === 1 ? '' : 's'}${r.skipped ? `, skipped ${r.skipped}` : ''}`;
  if (Array.isArray(r.notes) && r.notes.length) s += `. ${r.notes.join('. ')}.`;
  return s;
}

/** Guesses whether a file the person picked is an encrypted Raypass export (it needs a password). */
export function isEncryptedExport(text) {
  try { const j = JSON.parse(text); return !!j && typeof j === 'object' && j.raypass_export === 1 && typeof j.data === 'string'; } catch { return false; }
}

/** A file name for an export when the app gave none. */
export function exportName(format, now = new Date()) {
  const d = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`;
  return `raypass-${d}${format === 'csv' ? '.csv' : format === 'encrypted' ? '.raypass.json' : '.json'}`;
}

/** The health score's word: 0..100. */
export function healthLabel(score) {
  const s = Number(score) || 0;
  return s >= 90 ? 'Excellent' : s >= 75 ? 'Good' : s >= 50 ? 'Fair' : 'Needs attention';
}

// ---------- keyboard ----------

/** The index to select after an arrow key in a list of n rows (clamped; -1 when the list is empty). */
export function stepIndex(i, n, key) {
  if (n <= 0) return -1;
  if (key === 'ArrowDown') return i < 0 ? 0 : Math.min(n - 1, i + 1);
  if (key === 'ArrowUp') return i < 0 ? n - 1 : Math.max(0, i - 1);
  if (key === 'Home') return 0;
  if (key === 'End') return n - 1;
  return i;
}
