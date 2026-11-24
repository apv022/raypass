// Raypass web UI: the page's controller. It decides which screen shows (setup, unlock, vault),
// draws the sidebar and the item list, routes (#/view/item), and handles the keyboard and the
// events stream. Item details, the editor and the tools live in their own modules; core.js holds
// the state and is the only caller of actions. See core.js for the invariants about secrets.
import { events, html, render, raw } from '/sol.js';
import { toast, showMenu, contextMenu } from '/kit/kit.js';
import { TYPES, typeOf, matchesQuery, inView, folderTree, rowSubtitle, stepIndex, isLocked, relativeTime } from '/lib.js';
import { svg, icon } from '/icons.js';
import { state, hooks, rp, act, fail, copy, wipe, lockNow, armIdle, stopTotp } from '/core.js';
import { showSetup, showUnlock } from '/gate.js';
import { drawDetail, loadItem, wireDetail, tileHtml, copyField, trashItems, restoreItems, purgeItems } from '/detail.js';
import { openEditor, cancel as cancelEdit } from '/edit.js';
import { drawTool, wireTools, refreshHealth } from '/tools.js';

const $ = (id) => document.getElementById(id);
const $app = $('app');
const phoneQ = matchMedia('(max-width: 760px)');
const TOOLS = [
  { kind: 'generator', label: 'Generator', icon: 'dice' },
  { kind: 'health', label: 'Password health', icon: 'shield' },
  { kind: 'settings', label: 'Settings', icon: 'settings' },
];
const TILE_COLORS = { all: 'gray', fav: 'gold', login: 'blue', note: 'yellow', card: 'green', identity: 'indigo', wallet: 'orange', trash: 'slate' };

// ---------- modes and screens ----------

function setMode(mode) {
  $app.dataset.mode = mode;
  if (mode !== 'vault') $('gate').hidden = false;
}
const isTool = (v = state.view) => TOOLS.some((t) => t.kind === v.kind);
function setScreen(s) { $app.dataset.screen = s; }

// ---------- routing: #/<view>[/<item id>] ----------

function viewKey(v) {
  if (v.kind === 'type') return v.type;
  if (v.kind === 'folder') return 'folder:' + encodeURIComponent(v.name);
  return v.kind;
}
function parseView(k) {
  if (!k) return null;
  if (TYPES.some((t) => t.id === k)) return { kind: 'type', type: k };
  if (k.startsWith('folder:')) { try { return { kind: 'folder', name: decodeURIComponent(k.slice(7)) }; } catch { return null; } }
  if (['all', 'fav', 'trash', 'generator', 'health', 'settings'].includes(k)) return { kind: k };
  return null;
}
function parseHash() {
  const [, k, id] = location.hash.split('/');
  const v = parseView(k);
  return v ? { view: v, sel: id && /^[0-9a-f]{1,32}$/.test(id) ? id : null } : null;
}
function setHash(push) {
  const h = '#/' + viewKey(state.view) + (state.sel && !isTool() ? '/' + state.sel : '');
  if (location.hash === h) return;
  if (push) history.pushState({ rp: 1 }, '', h); else history.replaceState({ rp: 1 }, '', h);
}

/** Shows a view (and optionally an item in it). */
function go(view, sel = null, focus) {
  if (state.edit) { cancelEdit().then((ok) => { if (ok) go(view, sel, focus); }); return; }
  const changed = viewKey(view) !== viewKey(state.view);
  state.view = view;
  if (changed) { state.query = ''; $('q').value = ''; }
  state.sel = null; state.item = null; state.reveal.clear(); state.totp = null; stopTotp();
  setHash(phoneQ.matches);
  setScreen(isTool() ? 'tool' : 'list');
  if (sel) select(sel); else draw();
  if (isTool()) drawTool(focus);
  if (!phoneQ.matches && !isTool()) $('rows').focus({ preventScroll: true });
}

/** Selects an item (null clears); shows it in the detail pane. */
function select(id, { reload = false, push = phoneQ.matches } = {}) {
  if (state.edit) { cancelEdit().then((ok) => { if (ok) select(id, { reload, push }); }); return; }
  const same = id === state.sel;
  state.sel = id;
  if (!same) { state.item = null; state.reveal.clear(); state.totp = null; stopTotp(); }
  setHash(push && !!id && !same);
  if (id) setScreen('detail'); else if (!isTool()) setScreen('list');
  drawList();
  if (id && (!same || reload || !state.item)) loadItem(id); else drawDetail();
}

function back() {
  if (history.state?.rp && phoneQ.matches) { history.back(); return; }
  if (state.sel) { select(null); return; }
  setScreen('side');
}
addEventListener('popstate', () => {
  if (state.edit) { state.edit = null; }
  const r = parseHash();
  if (!r) { if (phoneQ.matches) setScreen('side'); return; }
  const viewChanged = viewKey(r.view) !== viewKey(state.view);
  state.view = r.view;
  if (viewChanged) { state.query = ''; $('q').value = ''; }
  if (r.sel !== state.sel) { state.sel = r.sel; state.item = null; state.reveal.clear(); state.totp = null; stopTotp(); if (r.sel) loadItem(r.sel); }
  setScreen(isTool() ? 'tool' : r.sel ? 'detail' : 'list');
  draw();
  if (isTool()) drawTool();
});

function viewTitle(v = state.view) {
  switch (v.kind) {
    case 'all': return 'All items';
    case 'fav': return 'Favourites';
    case 'type': return typeOf(v.type).plural;
    case 'folder': return v.name.split('/').pop();
    case 'trash': return 'Trash';
    default: return TOOLS.find((t) => t.kind === v.kind)?.label || '';
  }
}

// ---------- data ----------

const byName = (a, b) => (b.fav ? 1 : 0) - (a.fav ? 1 : 0) || String(a.name).localeCompare(String(b.name), undefined, { sensitivity: 'base' });
let refreshing = null, again = false;
/** Reloads the list, the trash and the health report (one at a time; calls meanwhile share one more). */
async function refresh() {
  if ($app.dataset.mode !== 'vault') return;
  if (refreshing) { again = true; return refreshing; }
  refreshing = (async () => {
    try {
      const [rows, trash] = await Promise.all([rp('list', {}), rp('list', { trash: true })]);
      state.rows = (rows || []).filter((r) => !r.trashed).sort(byName);
      state.trash = (trash || []).sort((a, b) => (b.trashed || 0) - (a.trashed || 0));
      state.loaded = true;
      const row = state.rows.find((r) => r.id === state.sel) || state.trash.find((r) => r.id === state.sel);
      if (state.sel && !row && !state.edit) { state.sel = null; state.item = null; setHash(false); if (phoneQ.matches && $app.dataset.screen === 'detail') setScreen('list'); }
      draw();
      if (row && state.item && !state.edit && (row.updated !== state.item.updated || !!row.trashed !== !!state.item.trashed)) loadItem(row.id, { quiet: true });
      refreshHealth();
    } catch (e) { fail(e); }
  })();
  await refreshing;
  refreshing = null;
  if (again) { again = false; await refresh(); }
}

let evTimer = 0;
let stopEvents = null;
function listen() {
  if (stopEvents) return;
  // Entries of the vault and keys feeds arrive here (from this device or another): no polling.
  stopEvents = events({}, (ev) => {
    if (ev.type !== 'entry' && ev.type !== 'feed') return;
    clearTimeout(evTimer);
    evTimer = setTimeout(refresh, 120);
  }, () => { /* EventSource reconnects by itself */ });
}

// ---------- drawing ----------

function draw() {
  if ($app.dataset.mode !== 'vault') return;
  drawSide();
  drawList();
  if (!isTool()) drawDetail();
  $app.toggleAttribute('data-tool', isTool());
}

function count(v) {
  if (v.kind === 'trash') return state.trash.length;
  return state.rows.filter((r) => inView(r, v)).length;
}

function drawSide() {
  const cur = viewKey(state.view);
  const tiles = [{ kind: 'all', label: 'All', icon: 'tray', key: 'all' }, { kind: 'fav', label: 'Favourites', icon: 'star', key: 'fav' },
    ...TYPES.map((t) => ({ kind: 'type', type: t.id, label: t.plural, icon: t.icon, key: t.id })), { kind: 'trash', label: 'Trash', icon: 'trash', key: 'trash' }];
  render($('tiles'), html`${tiles.map((t) => html`<button type="button" class="tile-btn" role="listitem" data-view="${viewKey(t)}" data-color="${TILE_COLORS[t.key]}" aria-current="${cur === viewKey(t)}">
    <span class="tile-top"><span class="disc">${icon(t.icon, 16)}</span><span class="n">${state.loaded ? count(t) : ''}</span></span><span class="tile-name">${t.label}</span></button>`)}`);
  const folders = folderTree(state.rows);
  $('folders-title').hidden = !folders.length;
  render($('folders'), html`${folders.map((f) => html`<button type="button" class="nav-item fold" data-view="${viewKey({ kind: 'folder', name: f.name })}" data-depth="${Math.min(f.depth, 4)}" aria-current="${cur === viewKey({ kind: 'folder', name: f.name }) ? 'page' : 'false'}" title="${f.name}">
    <sol-icon name="folder"></sol-icon><span class="grow nav-label-text">${f.label}</span><span class="nav-count">${f.count}</span></button>`)}`);
  const h = state.health;
  const issues = h ? (h.weak || []).length + (h.reused || []).length + (h.old || []).length : 0;
  render($('tools'), html`${TOOLS.map((t) => html`<button type="button" class="nav-item" data-view="${t.kind}" aria-current="${cur === t.kind ? 'page' : 'false'}">
    ${icon(t.icon, 18)}<span class="grow">${t.label}</span>${t.kind === 'health' && issues ? html`<span class="badge warn">${issues}</span>` : ''}</button>`)}`);
}

function visibleRows() {
  const src = state.view.kind === 'trash' ? state.trash : state.rows.filter((r) => inView(r, state.view));
  return state.query ? src.filter((r) => matchesQuery(r, state.query)) : src;
}

function drawList() {
  if (isTool()) return;
  const v = state.view;
  const rows = visibleRows();
  $('list-title').textContent = state.query ? 'Search' : viewTitle();
  $('back-side').hidden = false;
  render($('list-actions'), v.kind === 'trash'
    ? (state.trash.length ? html`<button type="button" class="plain danger-text" data-list="empty">Empty</button>` : html``)
    : html`<button type="button" class="ghost icon-btn" data-list="new" aria-label="New item (N)" title="New item (N)"><sol-icon name="plus"></sol-icon></button>`);
  if (!state.loaded) { render($('rows'), html``); return; }
  if (!rows.length) {
    render($('rows'), html`<div class="empty">${emptyState(v)}</div>`);
    return;
  }
  const selIdx = rows.findIndex((r) => r.id === state.sel);
  render($('rows'), html`<p class="list-count">${rows.length === 1 ? '1 item' : rows.length + ' items'}${state.query ? html` matching “${state.query}”` : ''}</p>${rows.map((r, i) => html`<div class="row" role="option" data-id="${r.id}" aria-selected="${r.id === state.sel}" tabindex="${i === (selIdx < 0 ? 0 : selIdx) ? 0 : -1}">
    ${tileHtml(r, 'm')}
    <span class="row-text"><span class="row-title">${r.name || 'Untitled'}</span><span class="row-sub">${v.kind === 'trash' ? `Deleted ${relativeTime(r.trashed)}` : rowSubtitle(r)}</span></span>
    <span class="row-marks">${r.has_totp ? html`<span title="Has a one-time code">${svg('timer', 15)}</span>` : ''}${r.fav && v.kind !== 'fav' ? html`<span class="fav-mark" title="Favourite"><sol-icon name="star"></sol-icon></span>` : ''}</span>
  </div>`)}`);
  const sel = $('rows').querySelector('[aria-selected="true"]');
  if (sel) sel.scrollIntoView({ block: 'nearest' });
}

function emptyState(v) {
  if (state.query) return html`<sol-icon name="search"></sol-icon><h3>No results</h3><p>Nothing matches “${state.query}”${v.kind !== 'all' ? ` in ${viewTitle()}` : ''}.</p>${v.kind !== 'all' ? html`<button type="button" class="tinted" data-list="search-all">Search all items</button>` : ''}`;
  if (v.kind === 'trash') return html`<sol-icon name="trash"></sol-icon><h3>Trash is empty</h3><p>Items you delete stay here until you empty the Trash.</p>`;
  if (v.kind === 'fav') return html`<sol-icon name="star"></sol-icon><h3>No favourites</h3><p>Star the items you use most and they show up here, and first in every list.</p>`;
  if (v.kind === 'folder') return html`<sol-icon name="folder"></sol-icon><h3>Empty folder</h3>`;
  const t = v.kind === 'type' ? typeOf(v.type) : null;
  return html`${svg(t ? t.icon : 'raypass', 40, 'empty-ico')}<h3>${t ? `No ${t.plural.toLowerCase()} yet` : 'Your vault is empty'}</h3><p>${t ? `Add your first ${t.label.toLowerCase()}.` : 'Add a login, a card, a note or a wallet, or import from another password manager.'}</p>
    <button type="button" class="primary" data-list="new">${t ? `New ${t.label.toLowerCase()}` : 'New item'}</button>${t ? '' : html` <button type="button" data-list="import">Import…</button>`}`;
}

// ---------- new and edit ----------

function newItem(anchor) {
  if (state.view.kind === 'type') { startNew(state.view.type); return; }
  showMenu(TYPES.map((t) => ({ label: t.label, icon: { login: 'user', note: 'file', card: 'tag', identity: 'people', wallet: 'storage' }[t.id], onSelect: () => startNew(t.id) })), { anchor, placement: phoneQ.matches ? 'bottom-end' : 'bottom' });
}
function startNew(type) {
  if (state.edit) return;
  if (isTool() || state.view.kind === 'trash') { state.view = { kind: 'all' }; setHash(false); }
  state.sel = null; state.item = null; stopTotp();
  $app.toggleAttribute('data-tool', false);
  drawSide(); drawList();
  if (phoneQ.matches) { history.pushState({ rp: 1 }, '', location.hash); }
  setScreen('detail');
  openEditor(type);
}
function edit() {
  if (!state.item || state.item.trashed || state.edit) return;
  openEditor(state.item.type, state.item.id);
}

// ---------- lock / unlock ----------

let gateShown = '';
function showLocked() {
  if (gateShown === 'unlock') return;
  gateShown = 'unlock';
  wipe();
  if (state.status) state.status.locked = true;
  setMode('locked');
  showUnlock(onUnlocked);
}

async function onUnlocked() {
  gateShown = '';
  try { state.status = await rp('status'); } catch (e) { fail(e); }
  $('gate').replaceChildren();
  setMode('vault');
  const r = parseHash();
  if (r) { state.view = r.view; state.sel = r.sel; }
  setScreen(phoneQ.matches ? (r ? (isTool() ? 'tool' : r.sel ? 'detail' : 'list') : 'side') : isTool() ? 'tool' : 'list');
  draw();
  if (isTool()) drawTool();
  armIdle();
  listen();
  await refresh();
  if (state.sel) loadItem(state.sel);
  if (!phoneQ.matches && !isTool()) $('q').focus();
}

// ---------- wiring ----------

hooks.draw = draw;
hooks.locked = showLocked;
hooks.select = (id, o) => select(id, { ...o, push: false });
hooks.refresh = refresh;
hooks.back = back;
hooks.edit = edit;
hooks.newItem = newItem;
hooks.go = go;
hooks.viewTitle = () => viewTitle();
hooks.toast = (m, kind) => toast(m, kind ? { kind } : undefined);
hooks.armIdle = armIdle;
hooks.defaultFolder = () => (state.view.kind === 'folder' ? state.view.name : '');
hooks.openItem = (id) => go({ kind: 'all' }, id);

$('side').addEventListener('click', (e) => {
  const v = e.target.closest('[data-view]');
  if (v) { const view = parseView(v.dataset.view); if (view) go(view); }
});
$('lock').addEventListener('click', () => lockNow());
$('new').addEventListener('click', (e) => newItem(e.currentTarget));
$('back-side').addEventListener('click', () => back());
$('listpane').addEventListener('click', async (e) => {
  const b = e.target.closest('[data-list]');
  if (b) {
    const k = b.dataset.list;
    if (k === 'new') newItem(b);
    if (k === 'import') go({ kind: 'settings' }, null, 'import');
    if (k === 'search-all') { const q = state.query; go({ kind: 'all' }); state.query = q; $('q').value = q; drawList(); }
    if (k === 'empty' && state.trash.length) {
      const { confirmBox } = await import('/core.js');
      if (await confirmBox('Empty the Trash?', `${state.trash.length === 1 ? 'One item' : state.trash.length + ' items'} will be deleted from all your devices. This can't be undone.`, 'Empty Trash', { danger: true })) {
        const r = await act('purge', { all_trash: true });
        if (r) toast(`Deleted ${r.purged} item${r.purged === 1 ? '' : 's'}`);
        if (state.view.kind === 'trash') select(null);
        refresh();
      }
    }
    return;
  }
  const row = e.target.closest('.row');
  if (row) select(row.dataset.id);
});

contextMenu($('rows'), (ev) => {
  const row = ev.target.closest?.('.row');
  if (!row) return [];
  const r = state.rows.find((x) => x.id === row.dataset.id) || state.trash.find((x) => x.id === row.dataset.id);
  if (!r) return [];
  if (r.trashed) return [{ label: 'Restore', icon: 'refresh', onSelect: () => restoreItems([r.id]) }, { separator: true }, { label: 'Delete now', icon: 'trash', danger: true, onSelect: () => purgeItems([r.id]) }];
  const copyOf = async (k, label) => {
    try { const it = await rp('get', { id: r.id }); copy(it[k], label); } catch (x) { fail(x); }
  };
  const items = [];
  if (r.type === 'login') {
    if (r.user) items.push({ label: 'Copy username', icon: 'user', onSelect: () => copy(r.user, 'Username', { secret: false }) });
    items.push({ label: 'Copy password', icon: 'copy', onSelect: () => copyOf('pass', 'Password') });
    if (r.has_totp) items.push({ label: 'Copy code', icon: 'clock', onSelect: async () => { try { const t = await rp('totp', { id: r.id }); copy(t.code, 'Code'); } catch (x) { fail(x); } } });
    items.push({ separator: true });
  }
  items.push({ label: r.fav ? 'Remove from Favourites' : 'Add to Favourites', icon: 'star', onSelect: async () => { await act('update', { id: r.id, fav: !r.fav }); refresh(); } });
  items.push({ separator: true }, { label: 'Move to Trash', icon: 'trash', danger: true, onSelect: () => trashItems([r.id]) });
  return items;
});

let searchT = 0;
$('q').addEventListener('input', () => {
  clearTimeout(searchT);
  searchT = setTimeout(() => { state.query = $('q').value.trim(); drawList(); }, 40);
});

/** Moves the selection with the arrow keys (from the search field or the list). */
function step(key) {
  const rows = visibleRows();
  const i = rows.findIndex((r) => r.id === state.sel);
  const n = stepIndex(i, rows.length, key);
  if (n < 0 || n === i) return;
  select(rows[n].id, { push: false });
  return rows[n].id;
}

document.addEventListener('keydown', (e) => {
  if ($app.dataset.mode !== 'vault') return;
  const mod = e.ctrlKey || e.metaKey;
  if (mod && !e.shiftKey && !e.altKey && (e.key === 'l' || e.key === 'L')) { e.preventDefault(); lockNow(); return; }
  if (document.querySelector('sol-dialog dialog[open], .menu')) return;
  const t = e.target;
  const typing = /INPUT|TEXTAREA|SELECT/.test(t.tagName) || t.isContentEditable;
  if (t.id === 'q') {
    if (e.key === '/' && !t.value) { e.preventDefault(); return; } // already searching
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); const id = step(e.key); if (id && phoneQ.matches) return; }
    else if (e.key === 'Enter') { e.preventDefault(); const first = visibleRows()[0]; if (first) { select(state.sel || first.id); $('rows').querySelector('[aria-selected="true"]')?.focus(); } }
    else if (e.key === 'Escape') { e.preventDefault(); if (t.value) { t.value = ''; state.query = ''; drawList(); } else t.blur(); }
    return;
  }
  if (typing || state.edit) return;
  if (mod && (e.key === 'c' || e.key === 'C') && state.item && !getSelection()?.toString()) {
    const k = e.altKey ? 'code' : e.shiftKey ? 'user' : 'pass';
    if (copyField(k)) e.preventDefault();
    return;
  }
  if (mod || e.altKey) return;
  if (e.key === '/') { e.preventDefault(); if (isTool()) go({ kind: 'all' }); $('q').focus(); $('q').select(); return; }
  if (isTool()) return;
  if (e.key === 'n' || e.key === 'N') { e.preventDefault(); newItem($('new')); return; }
  if ((e.key === 'e' || e.key === 'E') && state.item) { e.preventDefault(); edit(); return; }
  if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(e.key) && (t === document.body || $('rows').contains(t) || $('listpane').contains(t) || $('detail').contains(t))) {
    e.preventDefault();
    step(e.key);
    $('rows').querySelector('[aria-selected="true"]')?.focus({ preventScroll: true });
    return;
  }
  if (e.key === 'Enter' && t.classList?.contains('row')) { e.preventDefault(); select(t.dataset.id, { reload: false }); return; }
  if (e.key === 'Escape' && phoneQ.matches && $app.dataset.screen === 'detail') { back(); }
  if ((e.key === 'Delete' || e.key === 'Backspace') && t.classList?.contains('row') && state.sel) {
    e.preventDefault();
    if (state.view.kind === 'trash') purgeItems([state.sel]); else trashItems([state.sel]);
  }
});

// Coming back to the page: a single status check (the app may have auto-locked meanwhile).
document.addEventListener('visibilitychange', async () => {
  if (document.visibilityState !== 'visible' || $app.dataset.mode !== 'vault') return;
  try { const s = await rp('status'); state.status = s; if (s.locked) showLocked(); } catch (e) { if (!isLocked(e)) fail(e); }
});

phoneQ.addEventListener('change', () => {
  if ($app.dataset.mode !== 'vault') return;
  setScreen(isTool() ? 'tool' : state.sel || state.edit ? 'detail' : phoneQ.matches ? 'list' : 'list');
});

wireDetail();
wireTools();

// ---------- start ----------

async function boot() {
  let st;
  try { st = await rp('status'); } catch (e) {
    if (isLocked(e)) return;
    render($('gate'), html`<div class="gate-card"><div class="gate-icon"><img src="/icon.svg" alt="" width="72" height="72"></div><h1>Raypass is not reachable</h1><p class="gate-sub">${e?.message || String(e)}</p><button class="primary large wide" type="button" id="retry">Try again</button></div>`);
    setMode('error');
    $('retry').addEventListener('click', boot);
    return;
  }
  state.status = st;
  if (!st.initialized) { setMode('setup'); showSetup(onUnlocked); return; }
  if (st.locked) { showLocked(); return; }
  onUnlocked();
}
boot();
