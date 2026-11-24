// Login form detection and filling, used by the content script and tested against
// test/forms.html. Pure DOM reading (detect) and the one DOM write we allow (setValue: a value
// into the field the person asked us to fill).
//
// Detection groups the page's visible inputs by form (or, for form-less pages, by their nearest
// common container) and classifies each group:
//   login     a password field (the current password) and, usually, a username field
//   signup    new-password fields (sign up, change password): we offer saving, never filling
//             the new password from the vault
//   username  the first step of a multi-step login (identifier only, no password yet)
//   totp      a one-time-code step (autocomplete=one-time-code, name heuristics, or split
//             one-digit boxes)
// A classic script (see urlmatch.js); it adds globalThis.Raypass.forms.
(function (root) {
  'use strict';

  const TEXTY = new Set(['text', 'email', 'tel', 'number', '']);
  const USER_RE = /user|login|e-?mail|account|ident|handle|member|nick|benutzer|usuario|utilisateur|courriel|username/i;
  const NOT_USER_RE = /search|query|captcha|otp|one.?time|2fa|mfa|code|token|coupon|promo|zip|postal|city|street|address|first.?name|last.?name|given|family|company|subject|message|comment/i;
  const TOTP_RE = /otp|totp|2fa|mfa|one.?time|verif\w*.?code|auth\w*.?code|security.?code|two.?factor|token|passcode|pin.?code|sms.?code|^code$/i;
  const NEW_PW_RE = /new|confirm|repeat|retype|again|verify|re-?enter|signup|sign.?up|register|create/i;

  /** Lower-cased descriptive text of an input: attributes and label, for the heuristics. */
  function fieldText(el) {
    const parts = [el.name, el.id, el.getAttribute('placeholder'), el.getAttribute('aria-label'),
      el.getAttribute('autocomplete'), el.getAttribute('data-testid')];
    try { for (const l of el.labels || []) parts.push(l.textContent); } catch { /* detached */ }
    const lb = el.getAttribute('aria-labelledby');
    if (lb) {
      for (const id of lb.split(/\s+/)) {
        const t = el.ownerDocument.getElementById(id);
        if (t) parts.push(t.textContent);
      }
    }
    return parts.filter(Boolean).join(' ').toLowerCase().replace(/\s+/g, ' ').slice(0, 300);
  }

  function autocomplete(el) { return (el.getAttribute('autocomplete') || '').toLowerCase(); }

  /** Whether the person can see and type into el. */
  function usable(el) {
    if (el.disabled || el.readOnly) return false;
    if (el.getAttribute('aria-hidden') === 'true') return false;
    const win = el.ownerDocument.defaultView;
    const cs = win && win.getComputedStyle(el);
    if (cs && (cs.display === 'none' || cs.visibility === 'hidden' || cs.visibility === 'collapse' || Number(cs.opacity) === 0)) return false;
    const r = el.getBoundingClientRect();
    if (r.width < 2 || r.height < 2) return false;
    // Moved off the page to the left or top: a honeypot for bots, not a field for people.
    if (r.right + (win?.scrollX || 0) <= 0 || r.bottom + (win?.scrollY || 0) <= 0) return false;
    return !!(el.offsetParent || cs?.position === 'fixed');
  }

  function inputType(el) { return (el.getAttribute('type') || 'text').toLowerCase(); }

  // Inputs once seen as type=password: a "show password" toggle makes them type=text, and they
  // are still the password. Kept here, never as a mark in the page's DOM.
  const wasPassword = new WeakSet();

  /** Whether el is a password input, or one a "show password" toggle turned into text. */
  function isPassword(el) {
    if (inputType(el) === 'password') { wasPassword.add(el); return true; }
    const ac = autocomplete(el);
    return wasPassword.has(el) || ac === 'current-password' || ac === 'new-password';
  }

  function isNewPassword(el) {
    const ac = autocomplete(el);
    if (ac === 'new-password') return true;
    if (ac === 'current-password') return false;
    return NEW_PW_RE.test(fieldText(el));
  }

  /** Whether el looks like a one-time-code field. */
  function isTotp(el) {
    if (autocomplete(el) === 'one-time-code') return true;
    if (inputType(el) === 'password' || inputType(el) === 'email') return false;
    const t = fieldText(el);
    if (USER_RE.test(t) && !/code/.test(t)) return false;
    if (!TOTP_RE.test(t)) return false;
    const ml = Number(el.getAttribute('maxlength') || 0);
    return !ml || (ml >= 4 && ml <= 10) || ml === 1;
  }

  /** Score of el as the username of a login whose password field is pw (higher is likelier). */
  function usernameScore(el, pw, nearest) {
    const ac = autocomplete(el), type = inputType(el), t = fieldText(el);
    if (ac === 'one-time-code' || isTotp(el)) return -100;
    let s = 0;
    if (/\busername\b/.test(ac)) s += 10;
    if (/\bemail\b/.test(ac)) s += 6;
    if (type === 'email') s += 5;
    if (type === 'tel') s += 1;
    if (USER_RE.test(t)) s += 4;
    if (NOT_USER_RE.test(t)) s -= 6;
    if (pw) {
      const before = !!(el.compareDocumentPosition(pw) & Node.DOCUMENT_POSITION_FOLLOWING);
      s += before ? 1 : -3;
      if (el === nearest) s += 3;
    }
    return s;
  }

  /** The element the inputs of a form-less page are grouped under. */
  function groupOf(el) {
    if (el.form) return el.form;
    // Climb until the container also holds a button or another input: that is the "form".
    let n = el.parentElement;
    for (let i = 0; n && i < 6; i++, n = n.parentElement) {
      if (n.querySelectorAll('input, button, [role=button]').length > 1) return n;
    }
    return el.ownerDocument.body;
  }

  /** Split one-digit code boxes in group: 4–8 consecutive inputs of maxlength 1. */
  function splitCode(inputs) {
    const ones = inputs.filter((el) => el.getAttribute('maxlength') === '1' && !isPassword(el));
    return ones.length >= 4 && ones.length <= 8 ? ones : null;
  }

  /**
   * Finds the login-related groups in doc (or under any root element). Each result is
   * {kind, group, username, password, newPasswords, totp} where totp is an array (one field, or
   * the boxes of a split code).
   */
  function detect(doc) {
    const all = [...doc.querySelectorAll('input')].filter((el) => {
      const t = inputType(el);
      return (TEXTY.has(t) || t === 'password') && usable(el);
    });
    const groups = new Map();
    for (const el of all) {
      const g = groupOf(el);
      if (!groups.has(g)) groups.set(g, []);
      groups.get(g).push(el);
    }
    const out = [];
    for (const [group, inputs] of groups) {
      const pws = inputs.filter(isPassword);
      const texts = inputs.filter((el) => !isPassword(el) && inputType(el) !== 'number' || (inputType(el) === 'number' && isTotp(el)));
      const split = splitCode(inputs);
      const totp = split || texts.filter(isTotp).slice(0, 1);
      if (pws.length) {
        const current = pws.filter((p) => !isNewPassword(p));
        const fresh = pws.filter(isNewPassword);
        // Two passwords, neither marked: a sign-up form with a confirmation.
        let pw = current[0] || null;
        // Two password fields and neither says current-password: a password and its
        // confirmation (sign-up), whatever their names.
        if (pws.length === 2 && !pws.some((p) => autocomplete(p) === 'current-password')) {
          fresh.splice(0, fresh.length, ...pws);
          current.length = 0;
          pw = null;
        }
        const anchor = pw || fresh[0];
        const cands = texts.filter((el) => !totp.includes(el));
        const before = cands.filter((el) => el.compareDocumentPosition(anchor) & Node.DOCUMENT_POSITION_FOLLOWING);
        const nearest = before[before.length - 1];
        let best = null, bestScore = -1;
        for (const el of cands) {
          const s = usernameScore(el, anchor, nearest);
          if (s > bestScore) { best = el; bestScore = s; }
        }
        out.push({
          kind: pw ? 'login' : 'signup', group,
          username: bestScore >= 1 ? best : null,
          password: pw, newPasswords: fresh, totp: totp.length ? totp : [],
        });
        continue;
      }
      if (totp.length) {
        out.push({ kind: 'totp', group, username: null, password: null, newPasswords: [], totp });
        continue;
      }
      // Identifier-only step: a strong username field and few other inputs (not a contact form).
      let best = null, bestScore = 0;
      for (const el of texts) {
        const s = usernameScore(el, null, null);
        if (s > bestScore) { best = el; bestScore = s; }
      }
      const strong = best && (/\busername\b/.test(autocomplete(best)) || (bestScore >= 6 && texts.length <= 2));
      // Newsletter and contact forms also have one strong email field; their words give them away.
      const chatter = /newsletter|subscribe|abonn|contact us|get in touch|updates|waitlist|notify me/i.test(group.textContent || '');
      if (strong && !chatter && !group.querySelector('textarea')) {
        out.push({ kind: 'username', group, username: best, password: null, newPasswords: [], totp: [] });
      }
    }
    return out;
  }

  /** The fields of detect()'s results that get an inline icon. */
  function iconFields(found) {
    const set = new Set();
    for (const f of found) {
      if (f.kind === 'login' || f.kind === 'username') {
        if (f.username) set.add(f.username);
        if (f.password) set.add(f.password);
      }
      if (f.kind === 'login' || f.kind === 'totp') for (const t of f.totp) set.add(t);
    }
    return [...set];
  }

  /**
   * Sets el's value the way typing would: through the native setter (so frameworks that wrap
   * the value property, React among them, see a change) followed by input and change events.
   */
  function setValue(el, value) {
    const win = el.ownerDocument.defaultView;
    const proto = el instanceof win.HTMLTextAreaElement ? win.HTMLTextAreaElement.prototype : win.HTMLInputElement.prototype;
    const setter = Object.getOwnPropertyDescriptor(proto, 'value').set;
    el.focus({ preventScroll: true });
    el.dispatchEvent(new win.KeyboardEvent('keydown', { bubbles: true, key: 'Unidentified' }));
    setter.call(el, value);
    el.dispatchEvent(new win.InputEvent('input', { bubbles: true, composed: true, inputType: 'insertReplacementText', data: value }));
    el.dispatchEvent(new win.KeyboardEvent('keyup', { bubbles: true, key: 'Unidentified' }));
    el.dispatchEvent(new win.Event('change', { bubbles: true }));
  }

  /** Fills a one-time code into one field or across split boxes. */
  function fillCode(fields, code) {
    if (!fields.length || !code) return;
    if (fields.length === 1) { setValue(fields[0], code); return; }
    [...code].slice(0, fields.length).forEach((d, i) => setValue(fields[i], d));
  }

  /**
   * Fills a login result of detect() with creds {user, pass, totp}. Returns which fields it
   * filled. Fields of other kinds are left alone; the new password of a sign-up form never
   * comes from here.
   */
  function fill(found, creds) {
    const done = [];
    if (found.username && creds.user && found.username.value !== creds.user) {
      setValue(found.username, creds.user); done.push('user');
    }
    if (found.password && creds.pass) { setValue(found.password, creds.pass); done.push('pass'); }
    if (found.totp.length && creds.totp) { fillCode(found.totp, creds.totp); done.push('totp'); }
    return done;
  }

  /** Picks, among detect()'s results, the one that holds el (or the first fillable one). */
  function resultFor(found, el) {
    if (el) {
      for (const f of found) {
        if (f.username === el || f.password === el || f.totp.includes(el) || f.newPasswords.includes(el)) return f;
      }
    }
    return found.find((f) => f.kind === 'login') || found.find((f) => f.kind === 'username') ||
      found.find((f) => f.kind === 'totp') || null;
  }

  /** Whether a button's text says it submits a login (for forms that submit by script). */
  function isSubmitLike(el) {
    if (!el) return false;
    const tag = el.tagName;
    const type = (el.getAttribute('type') || '').toLowerCase();
    if (tag === 'BUTTON' && (type === '' || type === 'submit')) return true;
    if (tag === 'INPUT' && (type === 'submit' || type === 'image')) return true;
    const t = (el.textContent + ' ' + (el.getAttribute('aria-label') || '') + ' ' + (el.value || '')).toLowerCase();
    return /log ?in|sign ?in|sign ?up|continue|next|submit|register|create account|anmelden|connexion|entrar/.test(t);
  }

  const ns = root.Raypass || (root.Raypass = {});
  ns.forms = { detect, iconFields, setValue, fill, fillCode, resultFor, isSubmitLike, usable, isTotp, isPassword, fieldText };
})(globalThis);
