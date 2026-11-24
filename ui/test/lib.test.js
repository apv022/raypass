// Tests for ui/lib.js, run in a real browser by cmd/browsertest. Test names never contain
// secrets; the passwords here are throwaway examples.
import { test, assert } from '/kit/testing.js';
import * as L from '../lib.js';

test('isLocked matches only the locked error', () => {
  assert.ok(L.isLocked({ code: 'forbidden', message: 'locked' }));
  assert.ok(!L.isLocked({ code: 'forbidden', message: 'not self' }));
  assert.ok(!L.isLocked({ code: 'unauthorized', message: 'locked' }));
  assert.ok(!L.isLocked(null));
});

test('strength orders weak to strong', () => {
  assert.equal(L.strength('').score, 0);
  assert.equal(L.strength('password').score, 0);
  assert.equal(L.strength('Password1!').score, 0);
  assert.equal(L.strength('aaaaaaaaaaaaaaaa').score, 0);
  assert.ok(L.strength('qwertyuiop').score <= 1);
  assert.ok(L.strength('abcdefgh123').score <= 2);
  assert.ok(L.strength('Tr0ub4dor&3').score >= 2);
  assert.equal(L.strength('x7#Kq9!vLm2@Pz4$Wn8^').score, 4);
  const pp = L.strength('correct-horse-battery-staple');
  assert.ok(pp.bits > 45 && pp.bits < 60, 'passphrase bits ' + pp.bits);
  assert.equal(L.strength('password').label, 'Very weak');
});

test('scoreOf thresholds', () => {
  assert.deepEqual([0, 27.9, 28, 40, 60, 80, 200].map(L.scoreOf), [0, 0, 1, 2, 3, 4, 4]);
  assert.equal(L.scoreLabel(9), 'Very strong');
});

test('charRuns groups letters, digits, symbols', () => {
  assert.deepEqual(L.charRuns('ab12#$c'), [{ kind: 'letter', text: 'ab' }, { kind: 'digit', text: '12' }, { kind: 'symbol', text: '#$' }, { kind: 'letter', text: 'c' }]);
  assert.deepEqual(L.charRuns(''), []);
  assert.equal(L.charRuns('éß')[0].kind, 'letter');
});

test('TOTP ring math', () => {
  assert.deepEqual(L.ringState(15, 30), { value: 50, urgent: false, text: '15' });
  assert.deepEqual(L.ringState(5, 30), { value: 16.7, urgent: true, text: '5' });
  assert.equal(L.ringState(40, 30).value, 100);
  assert.equal(L.ringState(-1, 30).value, 0);
  assert.equal(L.ringState(10, 0).value, 33.3);
  assert.equal(L.remainingAt(20, 1000, 6000), 15);
  assert.equal(L.remainingAt(2, 0, 10000), 0);
});

test('formatCode groups digits', () => {
  assert.equal(L.formatCode('123456'), '123 456');
  assert.equal(L.formatCode('12345678'), '1234 5678');
  assert.equal(L.formatCode('1234567'), '123 4567');
  assert.equal(L.formatCode('K7Q2F'), 'K7Q2F');
});

test('hostOf and openableUrl', () => {
  assert.equal(L.hostOf('https://www.github.com/login'), 'github.com');
  assert.equal(L.hostOf('github.com'), 'github.com');
  assert.equal(L.hostOf('http://localhost:8080/x'), 'localhost:8080');
  assert.equal(L.hostOf('androidapp://com.example.app'), 'com.example.app');
  assert.equal(L.hostOf(''), '');
  assert.equal(L.openableUrl('example.com/a'), 'https://example.com/a');
  assert.equal(L.openableUrl('javascript:alert(1)'), null);
  assert.equal(L.openableUrl('data:text/html,x'), null);
  assert.equal(L.openableUrl('androidapp://x'), null);
});

test('dates', () => {
  const now = new Date(2026, 10, 24, 12, 0).getTime();
  assert.equal(L.relativeTime(now - 10e3, now), 'just now');
  assert.equal(L.relativeTime(now - 5 * 60e3, now), '5 min ago');
  assert.equal(L.relativeTime(now - 3 * 3600e3, now), '3 h ago');
  assert.equal(L.relativeTime(now - 30 * 3600e3, now), 'yesterday');
  assert.equal(L.relativeTime(now - 4 * 86400e3, now), '4 days ago');
  assert.equal(L.relativeTime(new Date(2026, 2, 3).getTime(), now), '3 Mar 2026');
  assert.equal(L.relativeTime(0, now), '');
  assert.equal(L.formatDate(new Date(2026, 0, 5, 9, 7).getTime(), true), '5 Jan 2026, 09:07');
  assert.equal(L.durationLabel(0, 'min'), 'Never');
  assert.equal(L.durationLabel(15, 'min'), '15 minutes');
  assert.equal(L.durationLabel(60, 'min'), '1 hour');
  assert.equal(L.durationLabel(30, 's'), '30 seconds');
  assert.equal(L.durationLabel(60, 's'), '1 minute');
});

test('monogram, hue, subtitle', () => {
  assert.equal(L.monogram('github'), 'G');
  assert.equal(L.monogram('  #1 bank'), '1');
  assert.equal(L.monogram('', 'https://www.zoo.com'), 'Z');
  assert.equal(L.monogram('', ''), '?');
  assert.equal(L.hueOf('a'), L.hueOf('a'));
  assert.ok(L.hueOf('github') >= 0 && L.hueOf('github') < 360);
  assert.equal(L.rowSubtitle({ type: 'login', user: 'ada', urls: [] }), 'ada');
  assert.equal(L.rowSubtitle({ type: 'login', user: '', urls: ['https://x.org/a'] }), 'x.org');
  assert.equal(L.rowSubtitle({ type: 'card' }), 'Card');
});

test('cards', () => {
  assert.equal(L.cardBrand('4242424242424242').id, 'visa');
  assert.equal(L.cardBrand('5555 5555 5555 4444').id, 'mastercard');
  assert.equal(L.cardBrand('2223003122003222').id, 'mastercard');
  assert.equal(L.cardBrand('378282246310005').id, 'amex');
  assert.equal(L.cardBrand('6011111111111117').id, 'discover');
  assert.equal(L.cardBrand('9'), null);
  assert.equal(L.formatCardNumber('4242424242424242'), '4242 4242 4242 4242');
  assert.equal(L.formatCardNumber('378282246310005'), '3782 822463 10005');
  assert.equal(L.maskCard('4242 4242 4242 4242'), '•••• 4242');
  assert.ok(L.luhn('4242424242424242'));
  assert.ok(!L.luhn('4242424242424241'));
  assert.equal(L.normExpiry('3/27'), '03/27');
  assert.equal(L.normExpiry('03/2027'), '03/27');
  assert.equal(L.normExpiry('0327'), '03/27');
  assert.equal(L.normExpiry('13/27'), '13/27');
});

test('wallets', () => {
  assert.deepEqual(L.seedWords('  abandon  ability\nable '), ['abandon', 'ability', 'able']);
  assert.deepEqual(L.seedWords(''), []);
  assert.equal(L.shortAddress('0x1234567890abcdef1234'), '0x1234…ef1234');
  assert.equal(L.shortAddress('short'), 'short');
});

test('tags, search and views', () => {
  assert.deepEqual(L.splitTags(' a, b ,a,, '), ['a', 'b']);
  const row = { type: 'login', name: 'GitHub', user: 'ada', urls: ['https://github.com'], folder: 'Work/Dev', tags: ['code'], fav: true };
  assert.ok(L.matchesQuery(row, 'git ada'));
  assert.ok(L.matchesQuery(row, 'CODE'));
  assert.ok(L.matchesQuery(row, 'work'));
  assert.ok(!L.matchesQuery(row, 'gitlab'));
  assert.ok(L.matchesQuery(row, ''));
  assert.ok(L.inView(row, { kind: 'fav' }));
  assert.ok(L.inView(row, { kind: 'type', type: 'login' }));
  assert.ok(!L.inView(row, { kind: 'type', type: 'card' }));
  assert.ok(L.inView(row, { kind: 'folder', name: 'Work' }));
  assert.ok(!L.inView(row, { kind: 'folder', name: 'Wor' }));
});

test('folderTree builds parents and counts', () => {
  const t = L.folderTree([{ folder: 'Work/Servers' }, { folder: 'Work' }, { folder: 'home' }, { folder: '' }]);
  assert.deepEqual(t, [
    { name: 'home', label: 'home', depth: 0, count: 1 },
    { name: 'Work', label: 'Work', depth: 0, count: 2 },
    { name: 'Work/Servers', label: 'Servers', depth: 1, count: 1 },
  ]);
});

test('diffItem and addArgs', () => {
  const orig = { name: 'A', user: 'u', pass: 'p', urls: ['x'], notes: '', fav: false };
  const ed = { name: 'B', user: '', pass: 'p', urls: ['x', 'y'], notes: '', fav: true };
  assert.deepEqual(L.diffItem(orig, ed, ['name', 'user', 'pass', 'urls', 'notes', 'fav']), { name: 'B', user: null, urls: ['x', 'y'], fav: true });
  assert.deepEqual(L.diffItem({ fav: true }, { fav: false }, ['fav']), { fav: false });
  assert.deepEqual(L.diffItem({}, { fav: false }, ['fav']), {});
  assert.deepEqual(L.addArgs('login', { name: 'N', user: 'u', pass: '', urls: [], fav: false }, ['name', 'user', 'pass', 'urls', 'fav']), { type: 'login', name: 'N', user: 'u' });
});

test('clipDecision', () => {
  assert.equal(L.clipDecision({ readable: true, current: 's', ours: 's', focused: true }), 'clear');
  assert.equal(L.clipDecision({ readable: true, current: 'other', ours: 's', focused: true }), 'keep');
  assert.equal(L.clipDecision({ readable: false, focused: false }), 'defer');
  assert.equal(L.clipDecision({ readable: false, focused: true }), 'clear');
});

test('import and export helpers', () => {
  assert.ok(L.isEncryptedExport('{"raypass_export":1,"data":"x"}'));
  assert.ok(!L.isEncryptedExport('{"items":[]}'));
  assert.ok(!L.isEncryptedExport('name,url'));
  const d = new Date(2026, 10, 4);
  assert.equal(L.exportName('csv', d), 'raypass-2026-11-04.csv');
  assert.equal(L.exportName('encrypted', d), 'raypass-2026-11-04.raypass.json');
  assert.equal(L.healthLabel(95), 'Excellent');
  assert.equal(L.healthLabel(10), 'Needs attention');
});

test('stepIndex', () => {
  assert.equal(L.stepIndex(-1, 3, 'ArrowDown'), 0);
  assert.equal(L.stepIndex(2, 3, 'ArrowDown'), 2);
  assert.equal(L.stepIndex(0, 3, 'ArrowUp'), 0);
  assert.equal(L.stepIndex(-1, 3, 'ArrowUp'), 2);
  assert.equal(L.stepIndex(1, 3, 'End'), 2);
  assert.equal(L.stepIndex(1, 0, 'ArrowDown'), -1);
});

test('type metadata', () => {
  assert.equal(L.TYPES.length, 5);
  assert.equal(L.typeOf('wallet').plural, 'Wallets');
  assert.equal(L.typeOf('nope').id, 'login');
  for (const t of L.TYPES) assert.ok(Array.isArray(L.TYPE_FIELDS[t.id]));
});
