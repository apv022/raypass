// Message validation: what the background accepts from content scripts and from its own pages.
import { test, assert } from './testing.js';

const M = globalThis.Raypass.msg;
const bad = (fn) => assert.throws(fn);

test('messages: content scripts get only their own operations', () => {
  M.content({ type: 'cs.matches' });
  M.content({ type: 'cs.fill', id: '0123456789abcdef' });
  M.content({ type: 'cs.state', login: true, focused: false });
  M.content({ type: 'cs.submitted', user: 'alice', pass: 'p' });
  M.content({ type: 'cs.saveAnswer', token: 'a'.repeat(32), save: true });
  bad(() => M.content({ type: 'ext.call', action: 'raypass.get', args: {} }));
  bad(() => M.content({ type: 'cs.fill', id: '0123456789abcdef', url: 'https://evil.example' }));
  bad(() => M.content({ type: 'cs.matches', url: 'https://evil.example' }));
  bad(() => M.content({ type: 'cs.fill', id: '../../etc' }));
  bad(() => M.content({ type: 'cs.fill', id: 'ABCDEF0123456789' }));
  bad(() => M.content({ type: 'cs.submitted', user: 'a', pass: '' }));
  bad(() => M.content({ type: 'cs.submitted', user: 'a', pass: 'x'.repeat(1025) }));
  bad(() => M.content({ type: 'cs.state', login: 'yes', focused: false }));
  bad(() => M.content({ type: 'cs.saveAnswer', token: 'short', save: true }));
  bad(() => M.content(null));
  bad(() => M.content([]));
  bad(() => M.content({ type: 'toString' }));
  bad(() => M.content({ type: '__proto__' }));
});

test('messages: extension pages get the allowlisted actions only', () => {
  for (const a of M.ACTIONS) M.ext({ type: 'ext.call', action: a, args: {} });
  M.ext({ type: 'ext.call', action: 'raypass.status' });
  for (const a of ['raypass.export', 'raypass.purge', 'raypass.setup', 'raypass.change_password', 'raypass.import', 'files.list']) {
    bad(() => M.ext({ type: 'ext.call', action: a, args: {} }));
  }
  bad(() => M.ext({ type: 'ext.call', action: 'raypass.get', args: [] }));
  bad(() => M.ext({ type: 'ext.call', action: 'raypass.get', args: { x: 'y'.repeat(70000) } }));
  M.ext({ type: 'ext.fillTab', tabId: 3, id: '0123456789abcdef' });
  bad(() => M.ext({ type: 'ext.fillTab', tabId: -1, id: '0123456789abcdef' }));
  bad(() => M.ext({ type: 'ext.clipArm', seconds: 1e9 }));
  M.ext({ type: 'ext.clipArm', seconds: 30 });
  assert.equal(M.ACTIONS.length, 10);
});
