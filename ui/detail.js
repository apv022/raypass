// The item detail pane: fields with copy buttons and reveal toggles, a live TOTP ring, a card
// face, a seed phrase word grid and the password history sheet. A secret reaches the DOM only
// while it is revealed (masked fields hold bullets, not the value), and always as text.
import { html, render, raw } from '/sol.js';
import { showMenu } from '/kit/kit.js';
import {
  typeOf, charRuns, ringState, remainingAt, formatCode, hostOf, openableUrl, formatDate, relativeTime,
  monogram, hueOf, cardBrand, formatCardNumber, maskCard, seedWords, strength, shortAddress,
} from '/lib.js';
import { svg, icon } from '/icons.js';
import { state, hooks, rp, act, fail, copy, dialog, confirmBox, startTotp, stopTotp } from '/core.js';

const $ = (id) => document.getElementById(id);
const MASK = '••••••••••••';

// ---------- small pieces ----------

/** The item's picture: a monogram on its own hue for logins, the type's glyph for the rest. */
export function tileHtml(it, size = 'm') {
  const t = typeOf(it.type);
  if (it.type === 'login' && (it.name || (it.urls || [])[0])) return html`<span class="tile t-${size}" data-var-h="${hueOf(hostOf((it.urls || [])[0] || '') || it.name)}" aria-hidden="true">${monogram(it.name, (it.urls || [])[0])}</span>`;
  return html`<span class="tile t-${size} ty" data-type="${it.type === 'login' ? 'login' : t.id}" aria-hidden="true">${svg(t.icon, size === 'l' ? 30 : size === 'm' ? 18 : 16)}</span>`;
}

/** A password as coloured runs (digits and symbols stand out, like a printed recovery key). */
export const runsHtml = (s) => html`${charRuns(s).map((r) => html`<span class="c-${r.kind}">${r.text}</span>`)}`;

const copyBtn = (key, label) => html`<button type="button" class="ghost icon-btn f-btn" data-copy="${key}" aria-label="Copy ${label}" title="Copy ${label}"><sol-icon name="copy"></sol-icon></button>`;
const revealBtn = (key, on) => html`<button type="button" class="ghost icon-btn f-btn" data-reveal-field="${key}" aria-pressed="${on}" aria-label="${on ? 'Hide' : 'Show'}" title="${on ? 'Hide' : 'Show'}">${on ? svg('eye-off', 18) : html`<sol-icon name="eye"></sol-icon>`}</button>`;

/** One field row. `secret` masks it until revealed; `body` overrides how the value is shown. */
function field(key, label, value, { secret = false, mono = false, body = null, extra = '', multiline = false } = {}) {
  if (value === undefined || value === null || value === '' || (Array.isArray(value) && !value.length)) return '';
  const on = state.reveal.has(key);
  const shown = secret && !on ? html`<span class="masked">${MASK}</span>` : body || html`${value}`;
  return html`<div class="field${multiline ? ' multi' : ''}" data-copy-row="${key}">
    <div class="f-main"><span class="f-label">${label}${extra}</span><span class="f-value${mono ? ' mono' : ''}${secret && on ? ' revealed' : ''}">${shown}</span></div>
    <div class="f-acts">${secret ? revealBtn(key, on) : ''}${copyBtn(key, label.toLowerCase())}</div>
  </div>`;
}

/** The value a copy key refers to on the current item. */
function valueOf(key) {
  const it = state.item;
  if (!it) return '';
  if (key === 'code') return state.totp?.code || '';
  if (key === 'fullname') return [it.first, it.last].filter(Boolean).join(' ');
  if (key.startsWith('url.')) return (it.urls || [])[+key.slice(4)] || '';
  if (key.startsWith('field.')) return (it.fields || [])[+key.slice(6)]?.value || '';
  if (key.startsWith('hist.')) return (it.history || [])[+key.slice(5)]?.pass || '';
  return it[key] ?? '';
}
const SECRET_KEYS = new Set(['pass', 'code', 'number', 'cvv', 'seed', 'passphrase', 'privkey', 'totp']);
const isSecretKey = (k) => SECRET_KEYS.has(k) || k.startsWith('hist.') || (k.startsWith('field.') && state.item?.fields?.[+k.slice(6)]?.hidden);
const LABELS = { pass: 'Password', user: 'Username', code: 'Code', number: 'Card number', cvv: 'Security code', seed: 'Seed phrase', passphrase: 'Passphrase', privkey: 'Private key', address: 'Address', email: 'Email', phone: 'Phone', holder: 'Name', exp: 'Expiry', fullname: 'Name', notes: 'Notes', company: 'Company', derivation: 'Derivation path', network: 'Network', brand: 'Brand' };
/** Copies a field of the current item (also used by keyboard shortcuts). */
export function copyField(key) {
  const v = valueOf(key);
  if (!v) return false;
  const label = LABELS[key] || (key.startsWith('url.') ? 'Website' : key.startsWith('hist.') ? 'Old password' : key.startsWith('field.') ? (state.item.fields[+key.slice(6)]?.name || 'Field') : 'Value');
  copy(v, label, { secret: isSecretKey(key) });
  return true;
}

// ---------- the pane ----------

/** The row (no secrets) for the selected id, from the list or the trash. */
const rowOf = (id) => state.rows.find((r) => r.id === id) || state.trash.find((r) => r.id === id);

let loadSeq = 0;
/** Loads the selected item with raypass.get and repaints; stale answers are dropped. */
export async function loadItem(id, { quiet = false } = {}) {
  const seq = ++loadSeq;
  if (!quiet) { state.item = null; state.reveal.clear(); state.totp = null; stopTotp(); drawDetail(); }
  try {
    const it = await rp('get', { id });
    if (seq !== loadSeq || state.sel !== id) return;
    state.item = { ...it, id };
    drawDetail();
    if (it.totp && !it.trashed) fetchTotp(id);
  } catch (e) {
    if (seq === loadSeq) fail(e);
  }
}

/** Repaints the detail pane for state.sel / state.item. */
export function drawDetail() {
  const root = $('detail');
  if (state.edit) return; // the editor owns the pane
  const id = state.sel;
  const it = state.item && state.item.id === id ? state.item : null;
  const row = it || rowOf(id);
  if (!id || !row) {
    stopTotp();
    const n = state.rows.length;
    render(root, n ? html`<div class="d-empty">${svg('raypass', 56)}<h2>Select an item</h2><p>${n === 1 ? '1 item' : n + ' items'} in your vault. Press / to search.</p></div>`
      : html`<div class="d-empty quiet">${svg('raypass', 64)}</div>`);
    return;
  }
  const t = typeOf(row.type);
  const trashed = !!row.trashed;
  const back = hooks.viewTitle();
  render(root, html`
    <header class="bar d-bar">
      <button class="plain back" type="button" data-act="back" aria-label="Back"><sol-icon name="back"></sol-icon><span>${back}</span></button>
      <div class="bar-actions">
        ${trashed ? html`<button type="button" class="tinted" data-act="restore">${svg('restore', 16)}<span>Restore</span></button>`
    : html`<button type="button" class="ghost icon-btn fav-btn" data-act="fav" aria-pressed="${!!row.fav}" aria-label="${row.fav ? 'Remove from Favourites' : 'Add to Favourites'}" title="${row.fav ? 'Remove from Favourites' : 'Add to Favourites'}"><sol-icon name="star"></sol-icon></button>
        <button type="button" class="tinted" data-act="edit" ${it ? '' : raw('disabled')}><sol-icon name="edit"></sol-icon><span>Edit</span></button>`}
        <button type="button" class="ghost icon-btn" data-act="more" aria-label="More"><sol-icon name="more"></sol-icon></button>
      </div>
    </header>
    <div class="d-scroll">
      ${trashed ? html`<div class="banner trash-banner">${svg('restore', 18)}<span class="grow">In the Trash since ${relativeTime(row.trashed)}. Restore it to use it again.</span><button type="button" class="danger" data-act="purge">Delete now</button></div>` : ''}
      <div class="d-hero">
        ${tileHtml(row, 'l')}
        <div class="d-hero-text"><h2 class="d-name">${row.name || 'Untitled'}</h2>
        <div class="d-sub">${t.label}${row.folder ? html` · <span class="d-folder"><sol-icon name="folder"></sol-icon>${row.folder}</span>` : ''}</div></div>
      </div>
      ${it ? body(it) : html`<div class="group pending" aria-hidden="true"><div class="field"></div><div class="field"></div></div>`}
    </div>`);
  if (it && it.totp && state.totp?.id === it.id) tickTotp();
}

function healthNote(it) {
  const h = state.health;
  if (!h || it.type !== 'login' || !it.pass) return '';
  const notes = [];
  if ((h.weak || []).includes(it.id)) notes.push('This password is weak.');
  const group = (h.reused || []).find((g) => g.includes(it.id));
  if (group) notes.push(`This password is also used by ${group.length - 1 === 1 ? 'another item' : `${group.length - 1} other items`}.`);
  if ((h.old || []).includes(it.id)) notes.push('This password has not been changed for a long time.');
  return notes.length ? html`<div class="banner warn-banner">${svg('shield', 18)}<span class="grow">${notes.join(' ')}</span><button type="button" class="tinted" data-act="edit">Change</button></div>` : '';
}

function body(it) {
  const parts = [];
  if (it.type === 'login') {
    const s = it.pass ? strength(it.pass) : null;
    parts.push(healthNote(it));
    parts.push(group([
      field('user', 'Username', it.user),
      field('pass', 'Password', it.pass, { secret: true, mono: true, body: runsHtml(it.pass), extra: s && s.score < 2 ? html`<span class="badge err">${s.label}</span>` : '' }),
      it.totp ? totpRow() : '',
    ]));
    if ((it.urls || []).length) parts.push(group(it.urls.map((u, i) => urlRow(u, i)), 'Websites'));
  } else if (it.type === 'card') {
    parts.push(cardFace(it));
    parts.push(group([
      field('holder', 'Cardholder', it.holder),
      field('number', 'Number', it.number, { secret: true, mono: true, body: html`${formatCardNumber(it.number)}` }),
      field('exp', 'Expiry', it.exp, { mono: true }),
      field('cvv', 'Security code', it.cvv, { secret: true, mono: true }),
      field('brand', 'Brand', it.brand || cardBrand(it.number)?.name),
    ]));
  } else if (it.type === 'identity') {
    parts.push(group([
      field('fullname', 'Name', [it.first, it.last].filter(Boolean).join(' ')),
      field('email', 'Email', it.email),
      field('phone', 'Phone', it.phone),
      field('company', 'Company', it.company),
      field('address', 'Address', it.address, { multiline: true }),
    ]));
  } else if (it.type === 'wallet') {
    const words = seedWords(it.seed);
    parts.push(group([
      field('network', 'Network', it.network),
      field('address', 'Address', it.address, { mono: true, body: html`<span class="addr">${it.address}</span>` }),
    ]));
    if (it.seed) {
      const on = state.reveal.has('seed');
      parts.push(html`<h3 class="group-title">Recovery</h3><div class="group">
        <div class="field seed-field" data-copy-row="seed">
          <div class="f-main"><span class="f-label">Seed phrase · ${words.length} words</span>
          ${on ? html`<ol class="seed-grid">${words.map((w, i) => html`<li><span class="n">${i + 1}</span><span class="w">${w}</span></li>`)}</ol>`
    : html`<span class="f-value"><span class="masked">${MASK}</span></span>`}</div>
          <div class="f-acts">${revealBtn('seed', on)}${copyBtn('seed', 'seed phrase')}</div>
        </div>
        ${field('passphrase', 'Passphrase (25th word)', it.passphrase, { secret: true, mono: true })}
        ${field('privkey', 'Private key', it.privkey, { secret: true, mono: true, body: html`<span class="addr">${it.privkey}</span>` })}
        ${field('derivation', 'Derivation path', it.derivation, { mono: true })}
      </div>`);
    } else {
      parts.push(group([
        field('passphrase', 'Passphrase (25th word)', it.passphrase, { secret: true, mono: true }),
        field('privkey', 'Private key', it.privkey, { secret: true, mono: true, body: html`<span class="addr">${it.privkey}</span>` }),
        field('derivation', 'Derivation path', it.derivation, { mono: true }),
      ], 'Recovery'));
    }
  }
  // the common tail: notes, custom fields, tags, history, dates
  if (it.notes) {
    parts.push(html`<h3 class="group-title">${it.type === 'note' ? 'Note' : 'Notes'}</h3><div class="group"><div class="field multi notes-field" data-copy-row="notes">
      <div class="f-main"><span class="f-value notes">${it.notes}</span></div><div class="f-acts">${copyBtn('notes', 'notes')}</div></div></div>`);
  }
  if ((it.fields || []).length) {
    parts.push(group(it.fields.map((f, i) => field('field.' + i, f.name || 'Field', f.value || ' ', { secret: !!f.hidden, mono: !!f.hidden })), 'Fields'));
  }
  if ((it.tags || []).length) parts.push(html`<div class="tags">${it.tags.map((t) => html`<span class="chip tag"><sol-icon name="tag"></sol-icon>${t}</span>`)}</div>`);
  if (it.type === 'login' && (it.history || []).length) {
    parts.push(html`<div class="group"><button type="button" class="field link-row" data-act="history">
      <span class="f-main"><span class="f-value">${svg('history', 18)} Password history</span></span><span class="row-value">${it.history.length}</span><sol-icon name="chevron"></sol-icon></button></div>`);
  }
  parts.push(html`<p class="d-meta">${it.updated ? `Modified ${relativeTime(it.updated)}` : ''}${it.created ? ` · Created ${formatDate(it.created)}` : ''}</p>`);
  return html`${parts}`;
}

function group(rows, title) {
  const r = rows.filter((x) => x !== '');
  if (!r.length) return '';
  return html`${title ? html`<h3 class="group-title">${title}</h3>` : ''}<div class="group">${r}</div>`;
}

function urlRow(u, i) {
  const open = openableUrl(u);
  return html`<div class="field" data-copy-row="url.${i}">
    <div class="f-main"><span class="f-label">${i === 0 ? 'Website' : 'Website ' + (i + 1)}</span><span class="f-value" title="${u}">${hostOf(u)}</span></div>
    <div class="f-acts">${open ? html`<a class="btn ghost icon-btn f-btn" href="${open}" target="_blank" rel="noopener noreferrer" aria-label="Open ${hostOf(u)}" title="Open">${svg('open', 18)}</a>` : ''}${copyBtn('url.' + i, 'website')}</div>
  </div>`;
}

// ---------- the card face ----------

function cardFace(it) {
  const b = cardBrand(it.number);
  const brand = it.brand || b?.name || 'Card';
  const on = state.reveal.has('number');
  return html`<div class="cc" data-brand="${b?.id || 'other'}" ${['visa', 'mastercard', 'amex', 'discover'].includes(b?.id) ? '' : html`data-var-h="${hueOf(brand + (it.name || ''))}"`}>
    <div class="cc-top"><span class="cc-chip" aria-hidden="true"></span><span class="cc-brand">${brand}</span></div>
    <div class="cc-num mono">${on ? formatCardNumber(it.number) : maskCard(it.number)}</div>
    <div class="cc-bottom"><span class="cc-holder">${it.holder || ''}</span><span class="cc-exp mono">${it.exp || ''}</span></div>
  </div>`;
}

// ---------- TOTP ----------

const CIRC = 2 * Math.PI * 15.5;
function totpRow() {
  const t = state.totp;
  return html`<div class="field totp" data-copy-row="code">
    <div class="f-main"><span class="f-label">One-time code</span><span class="f-value code mono" id="totp-code">${t ? formatCode(t.code) : '··· ···'}</span></div>
    <div class="f-acts"><span class="ring" id="totp-ring" role="timer" aria-label="Seconds left">
      <svg viewBox="0 0 36 36" aria-hidden="true"><circle class="track" cx="18" cy="18" r="15.5"/><circle class="prog" cx="18" cy="18" r="15.5" stroke-dasharray="${CIRC.toFixed(2)}"/></svg>
      <span class="ring-n" id="totp-n"></span></span>${copyBtn('code', 'code')}</div>
  </div>`;
}

let totpBusy = false;
async function fetchTotp(id) {
  if (totpBusy) return;
  totpBusy = true;
  try {
    const r = await rp('totp', { id });
    if (state.sel !== id || !state.item) return;
    state.totp = { ...r, id, at: Date.now() };
    const code = $('totp-code');
    if (code) code.textContent = formatCode(r.code);
    startTotp(tickTotp);
    tickTotp();
  } catch (e) { fail(e); } finally { totpBusy = false; }
}

/** Repaints the ring; fetches the next code when the period ends. */
function tickTotp() {
  const t = state.totp;
  if (!t || !state.item || t.id !== state.item.id) { stopTotp(); return; }
  const left = remainingAt(t.remaining, t.at, Date.now());
  const ring = $('totp-ring');
  if (ring) {
    const r = ringState(left, t.period);
    ring.style.setProperty('--off', String(CIRC * (1 - r.value / 100)));
    ring.toggleAttribute('data-urgent', r.urgent);
    const n = $('totp-n');
    if (n && n.textContent !== r.text) n.textContent = r.text;
    const code = $('totp-code');
    if (code && code.textContent !== formatCode(t.code)) code.textContent = formatCode(t.code);
  }
  if (left <= 0) { stopTotp(); fetchTotp(t.id); }
}

// ---------- history sheet ----------

function showHistory() {
  const it = state.item;
  if (!it) return;
  const shown = new Set();
  const list = () => html`<div class="group hist">${it.history.map((h, i) => html`<div class="field" data-copy-row="hist.${i}">
      <div class="f-main"><span class="f-label">${h.until ? `Used until ${formatDate(h.until, true)}` : 'Earlier password'}</span>
      <span class="f-value mono">${shown.has(i) ? runsHtml(h.pass) : html`<span class="masked">${MASK}</span>`}</span></div>
      <div class="f-acts"><button type="button" class="ghost icon-btn f-btn" data-hist="${i}" aria-pressed="${shown.has(i)}" aria-label="${shown.has(i) ? 'Hide' : 'Show'}">${shown.has(i) ? svg('eye-off', 18) : html`<sol-icon name="eye"></sol-icon>`}</button>${copyBtn('hist.' + i, 'old password')}</div></div>`)}</div>`;
  dialog('Password history', html`<p class="dlg-text">Earlier passwords of ${it.name}, newest first.</p><div class="hist-box"></div>`, (d) => {
    const box = d.querySelector('.hist-box');
    render(box, list());
    d.addEventListener('click', (e) => {
      const h = e.target.closest('[data-hist]');
      if (h) { const i = +h.dataset.hist; if (shown.has(i)) shown.delete(i); else shown.add(i); render(box, list()); return; }
      const c = e.target.closest('[data-copy]');
      if (c) copyField(c.dataset.copy);
    });
  });
}

// ---------- actions ----------

async function setFav() {
  const row = rowOf(state.sel);
  if (!row) return;
  const fav = !row.fav;
  row.fav = fav; if (state.item) state.item.fav = fav;
  hooks.draw();
  if (await act('update', { id: row.id, fav }) === null) row.fav = !fav;
  hooks.refresh();
}

/** Moves items to the Trash. */
export async function trashItems(ids) {
  const r = await act('trash', { ids });
  if (r) { hooks.toast(ids.length === 1 ? 'Moved to Trash' : `${r.trashed} items moved to Trash`); if (ids.includes(state.sel)) hooks.select(null); }
  await hooks.refresh();
}
/** Restores items from the Trash. */
export async function restoreItems(ids) {
  const r = await act('restore', { ids });
  if (r) { hooks.toast('Restored'); if (ids.includes(state.sel)) hooks.select(null); }
  await hooks.refresh();
}
/** Deletes items in the Trash for good, after asking. */
export async function purgeItems(ids) {
  const one = ids.length === 1 ? rowOf(ids[0])?.name || 'this item' : `${ids.length} items`;
  if (!await confirmBox('Delete for good?', `${one} will be deleted from all your devices. This can't be undone.`, 'Delete', { danger: true })) return;
  const r = await act('purge', { ids });
  if (r) { hooks.toast('Deleted'); if (ids.includes(state.sel)) hooks.select(null); }
  await hooks.refresh();
}

function moreMenu(anchor) {
  const row = rowOf(state.sel), it = state.item;
  if (!row) return;
  const items = [];
  if (row.trashed) {
    items.push({ label: 'Restore', icon: 'refresh', onSelect: () => restoreItems([row.id]) });
    items.push({ separator: true }, { label: 'Delete now', icon: 'trash', danger: true, onSelect: () => purgeItems([row.id]) });
  } else {
    if (it?.type === 'login') {
      if (it.user) items.push({ label: 'Copy username', icon: 'user', onSelect: () => copyField('user') });
      if (it.pass) items.push({ label: 'Copy password', icon: 'copy', onSelect: () => copyField('pass') });
      if (state.totp?.code) items.push({ label: 'Copy code', icon: 'clock', onSelect: () => copyField('code') });
      if (it.history?.length) items.push({ label: 'Password history', icon: 'refresh', onSelect: showHistory });
      if (items.length) items.push({ separator: true });
    }
    items.push({ label: 'Edit', icon: 'edit', disabled: !it, onSelect: () => hooks.edit() });
    items.push({ label: row.fav ? 'Remove from Favourites' : 'Add to Favourites', icon: 'star', onSelect: setFav });
    items.push({ separator: true }, { label: 'Move to Trash', icon: 'trash', danger: true, onSelect: () => trashItems([row.id]) });
  }
  showMenu(items, { anchor, placement: 'bottom-end' });
}

/** Wires the pane's clicks once. */
export function wireDetail() {
  const root = $('detail');
  root.addEventListener('click', (e) => {
    const a = e.target.closest('[data-act]');
    if (a && root.contains(a) && !state.edit) {
      switch (a.dataset.act) {
        case 'back': hooks.back(); return;
        case 'fav': setFav(); return;
        case 'edit': hooks.edit(); return;
        case 'more': moreMenu(a); return;
        case 'restore': restoreItems([state.sel]); return;
        case 'purge': purgeItems([state.sel]); return;
        case 'history': showHistory(); return;
        case 'new': hooks.newItem(a); return;
        case 'import': hooks.go({ kind: 'settings' }, null, 'import'); return;
      }
    }
    if (state.edit) return;
    const rv = e.target.closest('[data-reveal-field]');
    if (rv) { const k = rv.dataset.revealField; if (state.reveal.has(k)) state.reveal.delete(k); else state.reveal.add(k); drawDetail(); return; }
    const c = e.target.closest('[data-copy]');
    if (c) { copyField(c.dataset.copy); return; }
    if (e.target.closest('a, button, .seed-grid, .notes')) return;
    if (getSelection()?.toString()) return; // the person is selecting text
    const row = e.target.closest('[data-copy-row]');
    if (row) copyField(row.dataset.copyRow);
  });
}
