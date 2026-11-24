// The login form detector and the filler, against the fixture page forms.html.
import { test, assert } from './testing.js';

const F = globalThis.Raypass.forms;

const frame = document.createElement('iframe');
frame.src = 'forms.html';
frame.style.width = '900px';
frame.style.height = '3000px';
frame.style.border = '0';
document.body.append(frame);
await new Promise((r) => frame.addEventListener('load', r, { once: true }));
const doc = frame.contentDocument;

const detect = (id) => F.detect(doc.getElementById(id));
const expect = (id, what) => doc.querySelector(`#${id} [data-expect="${what}"]`);

function one(id, kind) {
  const found = detect(id);
  assert.equal(found.length, 1, `${id}: ${found.length} results (${found.map((f) => f.kind)})`);
  assert.equal(found[0].kind, kind, `${id}: kind ${found[0].kind}`);
  return found[0];
}

function login(id) {
  const f = one(id, 'login');
  assert.equal(f.username, expect(id, 'user'), `${id}: username is ${f.username && f.username.name}`);
  assert.equal(f.password, expect(id, 'pass'), `${id}: password is ${f.password && f.password.name}`);
  return f;
}

test('forms: plain login', () => { login('simple'); });
test('forms: email username, checkbox ignored', () => { login('email-first'); });
test('forms: the search box is not the username', () => {
  const found = detect('search-and-login');
  assert.equal(found.length, 1);
  assert.equal(found[0].username, expect('search-and-login', 'user'));
});
test('forms: hidden, off-page and invisible fields are skipped', () => { login('honeypot'); });
test('forms: login with a 2FA field', () => {
  const f = login('login-with-totp');
  assert.deepEqual(f.totp, [expect('login-with-totp', 'totp')]);
});
test('forms: form-less login', () => { login('formless'); });

test('forms: sign-up with new-password fields', () => {
  const f = one('signup', 'signup');
  assert.equal(f.password, null);
  assert.equal(f.newPasswords.length, 2);
  assert.equal(f.username, expect('signup', 'user'));
});
test('forms: sign-up with an unmarked confirmation field', () => {
  const f = one('signup-unmarked', 'signup');
  assert.equal(f.newPasswords.length, 2);
  assert.equal(f.username, expect('signup-unmarked', 'user'));
});
test('forms: change password fills the current one only', () => {
  const f = login('change');
  assert.equal(f.newPasswords.length, 2);
});

test('forms: identifier-only first step', () => {
  const f = one('multistep', 'username');
  assert.equal(f.username, expect('multistep', 'user'));
});
test('forms: one-time code by autocomplete', () => {
  assert.deepEqual(one('totp-ac', 'totp').totp, [expect('totp-ac', 'totp')]);
});
test('forms: one-time code by name', () => {
  assert.deepEqual(one('totp-name', 'totp').totp, [expect('totp-name', 'totp')]);
});
test('forms: split one-digit code boxes', () => {
  assert.equal(one('split', 'totp').totp.length, 6);
});

test('forms: newsletter, contact and dead fields are not logins', () => {
  assert.deepEqual(detect('newsletter'), []);
  assert.deepEqual(detect('contact'), []);
  assert.deepEqual(detect('dead'), []);
});

test('forms: a password field added later turns the step into a login', () => {
  one('dynamic', 'username');
  const pw = doc.createElement('input');
  pw.type = 'password';
  pw.name = 'password';
  doc.getElementById('dynamic-form').append(pw);
  const f = one('dynamic', 'login');
  assert.equal(f.password, pw);
  assert.equal(f.username.name, 'email');
});

test('forms: a "show password" toggle keeps the field a password', () => {
  const f = login('simple');
  f.password.type = 'text';
  try {
    assert.equal(detect('simple')[0].password, f.password);
  } finally {
    f.password.type = 'password';
  }
});

test('forms: the whole document is scanned too', () => {
  const kinds = F.detect(doc).map((f) => f.kind);
  assert.ok(kinds.filter((k) => k === 'login').length >= 7, kinds.join(','));
});

test('forms: fill uses the native setter and fires input/change (React-style tracking)', () => {
  const user = doc.getElementById('react-user');
  const pass = doc.getElementById('react-pass');
  // What React does: shadow the value property on the node to track changes made through it.
  let tracked = '';
  const proto = Object.getOwnPropertyDescriptor(frame.contentWindow.HTMLInputElement.prototype, 'value');
  Object.defineProperty(user, 'value', {
    configurable: true,
    get() { return proto.get.call(this); },
    set(v) { tracked = v; proto.set.call(this, v); },
  });
  const events = [];
  for (const t of ['keydown', 'input', 'keyup', 'change']) {
    user.addEventListener(t, (e) => events.push(`${t}:${e.bubbles}:${user.value}`));
  }
  const f = F.resultFor(F.detect(doc.getElementById('react')), user);
  const done = F.fill(f, { user: 'alice', pass: 'hunter2' });
  assert.deepEqual(done, ['user', 'pass']);
  assert.equal(tracked, '', 'went around the instance setter, so the framework sees a change');
  assert.equal(user.value, 'alice');
  assert.equal(pass.value, 'hunter2');
  assert.deepEqual(events, ['keydown:true:', 'input:true:alice', 'keyup:true:alice', 'change:true:alice']);
  delete user.value;
});

test('forms: codes fill split boxes digit by digit', () => {
  const f = one('split', 'totp');
  F.fill(f, { totp: '123456' });
  assert.equal(f.totp.map((b) => b.value).join(''), '123456');
});

test('forms: fill leaves fields it has nothing for alone', () => {
  const f = one('multistep', 'username');
  assert.deepEqual(F.fill(f, { user: 'bob', pass: 'x' }), ['user']);
  assert.equal(f.username.value, 'bob');
});

test('forms: submit-like buttons', () => {
  const b = (html) => { const d = doc.createElement('div'); d.innerHTML = html; return d.firstElementChild; };
  assert.ok(F.isSubmitLike(b('<button>Whatever</button>')));
  assert.ok(!F.isSubmitLike(b('<button type="button">Show</button>')));
  assert.ok(F.isSubmitLike(b('<div role="button">Sign in</div>')));
  assert.ok(F.isSubmitLike(b('<input type="submit" value="Go">')));
  assert.ok(!F.isSubmitLike(b('<a href="/help">Help</a>')));
});
