// Validation of the messages the background accepts. Two trust levels:
//   content  from our content script in a web page: it may ask for matches and fills for the
//            frame it runs in (the background takes the URL from the browser, never from the
//            message), report a submitted login, and answer the save bar. Nothing else.
//   ext      from our own pages (popup, options): any allowlisted native action.
// Anything else, any extra field, or any field of the wrong type is refused. A classic script
// (see urlmatch.js); it adds globalThis.Raypass.msg.
(function (root) {
  'use strict';

  /** The native actions the host forwards (design/DESIGN.md "Browser extension"). */
  const ACTIONS = ['raypass.status', 'raypass.unlock', 'raypass.lock', 'raypass.match', 'raypass.fill',
    'raypass.save_login', 'raypass.generate', 'raypass.totp', 'raypass.list', 'raypass.get'];

  const ID = /^[0-9a-f]{16}$/;
  const TOKEN = /^[0-9a-f]{32}$/;

  const str = (max) => (v) => typeof v === 'string' && v.length <= max;
  const bool = (v) => typeof v === 'boolean';
  const itemId = (v) => typeof v === 'string' && ID.test(v);
  const token = (v) => typeof v === 'string' && TOKEN.test(v);
  const tabId = (v) => Number.isInteger(v) && v >= 0;
  const seconds = (v) => Number.isInteger(v) && v >= 0 && v <= 3600;
  const plainObject = (v) => v !== null && typeof v === 'object' && !Array.isArray(v) &&
    Object.getPrototypeOf(v) === Object.prototype;
  const args = (v) => v === undefined || (plainObject(v) && JSON.stringify(v).length <= 64 * 1024);

  // type -> {field: check}; a field whose check accepts undefined is optional.
  const CONTENT = {
    'cs.state': { login: bool, focused: bool },
    'cs.matches': {},
    'cs.fill': { id: itemId },
    'cs.username': { user: str(256) },
    'cs.submitted': { user: str(256), pass: (v) => typeof v === 'string' && v.length > 0 && v.length <= 1024 },
    'cs.pendingSave': {},
    'cs.saveAnswer': { token, save: bool },
    'cs.openPopup': {},
  };

  const EXT = {
    'ext.call': { action: (v) => ACTIONS.includes(v), args },
    'ext.fillTab': { tabId, id: itemId },
    'ext.fillGenerated': { tabId, password: (v) => typeof v === 'string' && v.length > 0 && v.length <= 1024 },
    'ext.pendingSave': { tabId },
    'ext.saveAnswer': { tabId, token, save: bool },
    'ext.clipArm': { seconds },
    'ext.ping': {},
    'ext.status': {},
  };

  function check(table, m) {
    if (!plainObject(m) || typeof m.type !== 'string' || !Object.hasOwn(table, m.type)) {
      throw new Error('unknown message');
    }
    const spec = table[m.type];
    for (const k of Object.keys(m)) {
      if (k !== 'type' && !Object.hasOwn(spec, k)) throw new Error(`unexpected field ${k}`);
    }
    for (const [k, ok] of Object.entries(spec)) {
      if (!ok(m[k])) throw new Error(`bad field ${k}`);
    }
    return m;
  }

  /** Validates a message from a content script; throws on anything not allowed. */
  function content(m) { return check(CONTENT, m); }
  /** Validates a message from one of the extension's own pages; throws likewise. */
  function ext(m) { return check(EXT, m); }

  const ns = root.Raypass || (root.Raypass = {});
  ns.msg = { ACTIONS, content, ext };
})(globalThis);
