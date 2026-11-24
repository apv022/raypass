// The add and edit forms, one per item type. Values go into inputs through the escaping html``
// tag; at save the form is read back, compared with the item (lib.js diffItem) and sent as
// raypass.add or raypass.update. Secrets are never trimmed (DESIGN.md: used as given).
import { html, render, raw } from '/sol.js';
import {
  typeOf, TYPE_FIELDS, COMMON_FIELDS, diffItem, addArgs, splitTags, folderTree, cardBrand,
  formatCardNumber, normExpiry, seedWords, hostOf, luhn,
} from '/lib.js';
import { svg } from '/icons.js';
import { state, hooks, rp, fail, confirmBox, prefs } from '/core.js';
import { meterHtml, meter, wireReveal } from '/gate.js';
import { tileHtml } from '/detail.js';

const $ = (id) => document.getElementById(id);
const NETWORKS = ['bitcoin', 'ethereum', 'solana', 'litecoin', 'monero', 'cardano', 'polkadot', 'polygon', 'tron', 'dogecoin', 'cosmos', 'avalanche'];
const BRANDS = ['Visa', 'Mastercard', 'American Express', 'Discover', 'Diners Club', 'JCB', 'UnionPay', 'Maestro'];

/** Generator options from the person's last use (UI preference, not a secret). */
export const genOptions = () => ({ mode: 'chars', length: 20, upper: true, lower: true, digits: true, symbols: true, avoid_ambiguous: false, words: 5, separator: '-', capitalize: true, number: true, ...prefs.get('gen', {}) });

const text = (name, label, value, { type = 'text', ph = '', auto = 'off', mode = '', mono = false, attrs = '' } = {}) => html`
  <label class="e-row"><span class="e-label">${label}</span>
  <input name="${name}" type="${type}" value="${value ?? ''}" placeholder="${ph}" autocomplete="${auto}" ${mode ? html`inputmode="${mode}"` : ''} spellcheck="false" autocapitalize="off" class="${mono ? 'mono' : ''}" ${raw(attrs)}></label>`;

const secret = (name, label, value, { ph = '', gen = false, mono = true, meterOn = false } = {}) => html`
  <div class="e-row"><label class="e-label" for="e-${name}">${label}</label>
  <div class="pw-field e-pw"><input id="e-${name}" name="${name}" type="password" value="${value ?? ''}" placeholder="${ph}" autocomplete="new-password" spellcheck="false" autocapitalize="off" class="${mono ? 'mono' : ''}">
    <button type="button" class="ghost icon-btn reveal-btn" data-reveal="${name}" aria-label="Show" aria-pressed="false"><sol-icon name="eye"></sol-icon></button>
    ${gen ? html`<button type="button" class="ghost icon-btn" data-gen="${name}" aria-label="Generate a password" title="Generate a password">${svg('dice', 18)}</button>` : ''}
  </div>${meterOn ? meterHtml() : ''}</div>`;

const area = (name, label, value, { ph = '', rows = 3, mono = false, cls = '' } = {}) => html`
  <label class="e-row ${cls}"><span class="e-label">${label}</span><textarea name="${name}" rows="${rows}" placeholder="${ph}" spellcheck="${mono ? 'false' : 'true'}" class="${mono ? 'mono' : ''}">${value ?? ''}</textarea></label>`;

const urlInput = (u) => html`<div class="url-row"><input name="url" type="text" inputmode="url" value="${u}" placeholder="https://example.com" autocomplete="off" spellcheck="false" autocapitalize="off"><button type="button" class="ghost icon-btn" data-del-row aria-label="Remove website"><sol-icon name="minus"></sol-icon></button></div>`;
const fieldInput = (f) => html`<div class="cf-row"><input name="cf-name" value="${f.name || ''}" placeholder="Name" autocomplete="off" aria-label="Field name">
  <input name="cf-value" value="${f.value || ''}" placeholder="Value" autocomplete="off" spellcheck="false" aria-label="Field value" type="${f.hidden ? 'password' : 'text'}" class="${f.hidden ? 'mono' : ''}">
  <button type="button" class="ghost icon-btn" data-hide-field aria-pressed="${!!f.hidden}" aria-label="Hidden field" title="Hidden: masked until revealed"><sol-icon name="lock"></sol-icon></button>
  <button type="button" class="ghost icon-btn" data-del-row aria-label="Remove field"><sol-icon name="minus"></sol-icon></button></div>`;

/** Opens the editor: for `id` (the item in state.item) or a new item of `type`. */
export function openEditor(type, id = null) {
  const orig = id ? state.item : null;
  if (id && (!orig || orig.id !== id)) return;
  state.edit = { id, type: orig ? orig.type : type };
  drawEditor(orig || { type, name: '', folder: hooks.defaultFolder(), fav: state.view.kind === 'fav', urls: [], fields: [], tags: [] });
}

function typeBody(t, it) {
  switch (t) {
    case 'login': return html`
      <div class="group e-group">
        ${text('user', 'Username', it.user, { auto: 'off', ph: 'name@example.com' })}
        ${secret('pass', 'Password', it.pass, { gen: true, meterOn: true })}
        ${secret('totp', 'One-time code secret', it.totp, { ph: 'otpauth://… or base32 secret', mono: true })}
      </div>
      <h3 class="group-title">Websites</h3>
      <div class="group e-group"><div id="urls" class="url-list">${(it.urls?.length ? it.urls : ['']).map(urlInput)}</div>
        <button type="button" class="plain add-row" data-add="url"><sol-icon name="plus"></sol-icon>Add website</button></div>`;
    case 'card': return html`
      <div class="group e-group">
        ${text('holder', 'Cardholder', it.holder, { auto: 'off', ph: 'Name on card' })}
        ${secret('number', 'Number', it.number ? formatCardNumber(it.number) : '', { ph: '1234 5678 9012 3456' })}
        <div class="e-pair">${text('exp', 'Expiry', it.exp, { ph: 'MM/YY', mode: 'numeric', mono: true, attrs: 'maxlength="7"' })}${secret('cvv', 'Security code', it.cvv, { ph: '123' })}</div>
        ${text('brand', 'Brand', it.brand, { ph: 'Detected from the number', attrs: 'list="brands"' })}
      </div><datalist id="brands">${BRANDS.map((b) => html`<option value="${b}">`)}</datalist>`;
    case 'identity': return html`
      <div class="group e-group">
        <div class="e-pair">${text('first', 'First name', it.first)}${text('last', 'Last name', it.last)}</div>
        ${text('email', 'Email', it.email, { type: 'email', ph: 'name@example.com' })}
        ${text('phone', 'Phone', it.phone, { type: 'tel', ph: '+1 555 0100' })}
        ${text('company', 'Company', it.company)}
        ${area('address', 'Address', it.address, { rows: 3 })}
      </div>`;
    case 'wallet': return html`
      <div class="group e-group">
        ${text('network', 'Network', it.network, { ph: 'bitcoin, ethereum, …', attrs: 'list="networks"' })}
        ${text('address', 'Address', it.address, { mono: true, ph: 'Public address' })}
      </div><datalist id="networks">${NETWORKS.map((n) => html`<option value="${n}">`)}</datalist>
      <h3 class="group-title">Recovery</h3>
      <div class="group e-group">
        ${area('seed', 'Seed phrase', it.seed, { rows: 3, mono: true, ph: '12 or 24 words separated by spaces', cls: 'seed-area' })}
        <p class="e-hint" id="seed-count"></p>
        ${secret('passphrase', 'Passphrase (25th word)', it.passphrase)}
        ${secret('privkey', 'Private key', it.privkey)}
        ${text('derivation', 'Derivation path', it.derivation, { mono: true, ph: "m/44'/60'/0'/0/0" })}
      </div>`;
    default: return '';
  }
}

function drawEditor(it) {
  const t = typeOf(state.edit.type);
  const isNew = !state.edit.id;
  const folders = folderTree(state.rows);
  render($('detail'), html`<form id="editor" class="editor" autocomplete="off" novalidate>
    <header class="bar d-bar">
      <button type="button" class="plain" data-e="cancel">Cancel</button>
      <h2 class="e-title">${isNew ? `New ${t.label.toLowerCase()}` : `Edit ${t.label.toLowerCase()}`}</h2>
      <div class="bar-actions"><button type="submit" class="primary">${isNew ? 'Add' : 'Save'}</button></div>
    </header>
    <div class="d-scroll">
      <div class="d-hero e-hero">${tileHtml({ ...it, type: t.id }, 'l')}
        <input name="name" class="e-name" value="${it.name || ''}" placeholder="${t.id === 'login' ? 'Name, e.g. GitHub' : 'Name'}" aria-label="Name" autocomplete="off" required>
        <label class="e-fav" title="Favourite"><input type="checkbox" role="switch" name="fav" ${it.fav ? raw('checked') : ''}><span>Favourite</span></label>
      </div>
      ${typeBody(t.id, it)}
      <h3 class="group-title">${t.id === 'note' ? 'Note' : 'Notes'}</h3>
      <div class="group e-group">${area('notes', t.id === 'note' ? 'Note' : 'Notes', it.notes, { rows: t.id === 'note' ? 12 : 4, cls: 'bare' })}</div>
      <h3 class="group-title">Organize</h3>
      <div class="group e-group">
        ${text('folder', 'Folder', it.folder, { ph: 'None (use / for subfolders: Work/Servers)', attrs: 'list="folder-list"' })}
        ${text('tags', 'Tags', (it.tags || []).join(', '), { ph: 'Comma separated' })}
      </div><datalist id="folder-list">${folders.map((f) => html`<option value="${f.name}">`)}</datalist>
      <h3 class="group-title">Custom fields</h3>
      <div class="group e-group"><div id="cfields" class="cf-list">${(it.fields || []).map(fieldInput)}</div>
        <button type="button" class="plain add-row" data-add="field"><sol-icon name="plus"></sol-icon>Add field</button></div>
      <p class="form-err" role="alert"></p>
      ${isNew ? '' : html`<button type="button" class="danger wide e-trash" data-e="trash"><sol-icon name="trash"></sol-icon>Move to Trash</button>`}
    </div>
  </form>`);
  const f = $('editor');
  wireReveal(f);
  // the picture follows the name (and the first website) as you type
  const tile = () => { const t = f.querySelector('.e-hero > .tile'); const d = document.createElement('span'); render(d, tileHtml({ type: state.edit.type, name: f.elements.name.value, urls: [f.querySelector('[name="url"]')?.value || ''] }, 'l')); t.replaceWith(d.firstElementChild); };
  f.addEventListener('input', (e) => { if (e.target.name === 'name' || e.target.name === 'url') tile(); });
  const m = f.querySelector('.meter');
  if (m && f.elements.pass) { meter(m, f.elements.pass.value); f.elements.pass.addEventListener('input', () => meter(m, f.elements.pass.value)); }
  if (f.elements.seed) { const upd = () => { const n = seedWords(f.elements.seed.value).length; $('seed-count').textContent = n ? `${n} word${n === 1 ? '' : 's'}${[12, 15, 18, 21, 24].includes(n) ? '' : ' · seed phrases usually have 12 or 24'}` : ''; }; upd(); f.elements.seed.addEventListener('input', upd); }
  if (f.elements.number) {
    f.elements.number.addEventListener('blur', () => { if (/^[\d\s-]+$/.test(f.elements.number.value)) f.elements.number.value = formatCardNumber(f.elements.number.value.replace(/[\s-]/g, '')); });
    f.elements.number.addEventListener('input', () => { const b = cardBrand(f.elements.number.value); if (f.elements.brand) f.elements.brand.placeholder = b ? b.name : 'Detected from the number'; });
  }
  if (f.elements.exp) f.elements.exp.addEventListener('blur', () => { f.elements.exp.value = normExpiry(f.elements.exp.value); });
  state.edit.snap = JSON.stringify(collect(f)); // in memory only: it may hold secrets
  f.addEventListener('click', onClick);
  f.addEventListener('submit', (e) => { e.preventDefault(); save(f); });
  f.addEventListener('keydown', (e) => { if (e.key === 'Escape' && !e.defaultPrevented) { e.preventDefault(); cancel(); } if ((e.ctrlKey || e.metaKey) && e.key === 's') { e.preventDefault(); save(f); } });
  queueMicrotask(() => f.elements.name.focus());
}

async function onClick(e) {
  const f = $('editor');
  const b = e.target.closest('button');
  if (!b || !f) return;
  if (b.dataset.e === 'cancel') return cancel();
  if (b.dataset.e === 'trash') { const id = state.edit.id; state.edit = null; const { trashItems } = await import('/detail.js'); return trashItems([id]); }
  if (b.dataset.add === 'url') { const box = $('urls'); const d = document.createElement('div'); render(d, urlInput('')); box.append(...d.childNodes); box.lastElementChild.querySelector('input').focus(); return; }
  if (b.dataset.add === 'field') { const box = $('cfields'); const d = document.createElement('div'); render(d, fieldInput({})); box.append(...d.childNodes); box.lastElementChild.querySelector('input').focus(); return; }
  if (b.hasAttribute('data-del-row')) { b.parentElement.remove(); return; }
  if (b.hasAttribute('data-hide-field')) {
    const on = b.getAttribute('aria-pressed') !== 'true';
    b.setAttribute('aria-pressed', String(on));
    const v = b.parentElement.querySelector('[name="cf-value"]');
    v.type = on ? 'password' : 'text'; v.classList.toggle('mono', on);
    return;
  }
  if (b.dataset.gen) {
    try {
      const g = await rp('generate', genOptions());
      const input = f.elements[b.dataset.gen];
      input.value = g.password; input.type = 'text';
      const rb = input.parentElement.querySelector('[data-reveal]');
      if (rb) { rb.setAttribute('aria-pressed', 'true'); rb.setAttribute('aria-label', 'Hide'); render(rb, html`${svg('eye-off', 18)}`); }
      input.dispatchEvent(new Event('input'));
    } catch (x) { fail(x); }
  }
}

/** Reads the form into an item object with the type's fields. */
function collect(f) {
  const t = state.edit.type;
  const v = (n) => (f.elements[n] ? f.elements[n].value : undefined);
  const out = {
    name: v('name').trim(), folder: v('folder').trim().replace(/^\/+|\/+$/g, ''), fav: f.elements.fav.checked,
    tags: splitTags(v('tags')), notes: v('notes'),
    fields: [...f.querySelectorAll('.cf-row')].map((r) => ({ name: r.querySelector('[name="cf-name"]').value.trim(), value: r.querySelector('[name="cf-value"]').value, hidden: r.querySelector('[data-hide-field]').getAttribute('aria-pressed') === 'true' })).filter((x) => x.name || x.value),
  };
  if (/^\s*$/.test(out.notes)) out.notes = '';
  if (t === 'login') {
    out.user = v('user').trim(); out.pass = v('pass'); out.totp = v('totp').trim();
    out.urls = [...f.querySelectorAll('[name="url"]')].map((i) => i.value.trim()).filter(Boolean);
  } else if (t === 'card') {
    out.holder = v('holder').trim(); out.number = v('number').replace(/[\s-]/g, ''); out.exp = normExpiry(v('exp'));
    out.cvv = v('cvv').trim(); out.brand = v('brand').trim() || (out.number ? cardBrand(out.number)?.name || '' : '');
  } else if (t === 'identity') {
    for (const k of ['first', 'last', 'email', 'phone', 'company']) out[k] = v(k).trim();
    out.address = v('address').replace(/\s+$/, '');
  } else if (t === 'wallet') {
    out.network = v('network').trim(); out.address = v('address').trim(); out.seed = seedWords(v('seed')).join(' ');
    out.passphrase = v('passphrase'); out.privkey = v('privkey').trim(); out.derivation = v('derivation').trim();
  }
  return out;
}

function defaultName(d) {
  if (state.edit.type === 'login') return d.urls?.[0] ? hostOf(d.urls[0]) : d.user || '';
  if (state.edit.type === 'card') return d.brand ? `${d.brand}${d.number ? ' ' + d.number.slice(-4) : ''}` : '';
  if (state.edit.type === 'identity') return [d.first, d.last].filter(Boolean).join(' ');
  if (state.edit.type === 'wallet') return d.network ? d.network[0].toUpperCase() + d.network.slice(1) + ' wallet' : '';
  return '';
}

let saving = false;
async function save(f) {
  if (saving) return;
  const err = f.querySelector('.form-err');
  err.textContent = '';
  const d = collect(f);
  if (!d.name) d.name = defaultName(d);
  if (!d.name) { err.textContent = 'Give it a name.'; f.elements.name.focus(); return; }
  if (state.edit.type === 'card' && d.number && /^\d+$/.test(d.number) && !luhn(d.number)) {
    if (!await confirmBox('Check the card number', 'This card number doesn\'t look right (its check digit fails). Save it anyway?', 'Save')) return;
  }
  const fields = [...COMMON_FIELDS, ...TYPE_FIELDS[state.edit.type]];
  saving = true;
  try {
    if (!state.edit.id) {
      const r = await rp('add', addArgs(state.edit.type, d, fields));
      state.edit = null;
      hooks.toast('Added');
      await hooks.refresh();
      hooks.select(r.id);
    } else {
      const id = state.edit.id;
      const patch = diffItem(state.item, d, fields);
      state.edit = null;
      if (Object.keys(patch).length) { await rp('update', { id, ...patch }); hooks.toast('Saved'); }
      await hooks.refresh();
      hooks.select(id, { reload: true });
    }
  } catch (x) {
    fail(x);
    if (x?.code !== 'forbidden') err.textContent = x?.message || String(x);
  } finally { saving = false; }
}

/** Leaves the editor, asking first when something changed. */
export async function cancel() {
  const f = $('editor');
  if (!f || !state.edit) return true;
  if (JSON.stringify(collect(f)) !== state.edit.snap && !await confirmBox('Discard changes?', 'Your changes to this item will be lost.', 'Discard', { danger: true })) return false;
  const wasNew = !state.edit.id;
  state.edit = null;
  if (wasNew) hooks.back(); else hooks.draw();
  return true;
}
