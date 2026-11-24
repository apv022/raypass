// The native client: correlation, errors, timeouts, disconnects, with a fake port.
import { test, assert } from './testing.js';

const { NativeClient, isLocked } = globalThis.Raypass;

function fakePort() {
  const listeners = { msg: [], disc: [] };
  const port = {
    sent: [],
    postMessage(m) { port.sent.push(m); },
    onMessage: { addListener: (f) => listeners.msg.push(f) },
    onDisconnect: { addListener: (f) => listeners.disc.push(f) },
    disconnect() { port.closed = true; },
    answer(m) { for (const f of listeners.msg) f(m); },
    drop() { for (const f of listeners.disc) f(); },
  };
  return port;
}

test('native: answers go to the right request', async () => {
  const port = fakePort();
  const c = new NativeClient(() => port, { idleMs: 0 });
  const a = c.call('raypass.status');
  const b = c.call('raypass.match', { url: 'https://x.example' });
  assert.equal(port.sent.length, 2);
  assert.deepEqual(port.sent[1], { id: 2, action: 'raypass.match', args: { url: 'https://x.example' } });
  port.answer({ id: 2, ok: true, result: ['b'] });
  port.answer({ id: 1, ok: false, error: { code: 'forbidden', message: 'locked' } });
  port.answer({ id: 99, ok: true, result: 'stray' });
  assert.deepEqual(await b, ['b']);
  await assert.rejects(a, (e) => isLocked(e));
});

test('native: a dropped port fails what is pending and reconnects', async () => {
  let n = 0, port;
  const c = new NativeClient(() => { n++; port = fakePort(); return port; }, {
    idleMs: 0, lastError: () => ({ message: 'Specified native messaging host not found.' }),
  });
  const p = c.call('raypass.status');
  port.drop();
  await assert.rejects(p, (e) => e.code === 'host_missing');
  const q = c.call('raypass.status');
  assert.equal(n, 2);
  port.answer({ id: 2, ok: true, result: { locked: false } });
  assert.deepEqual(await q, { locked: false });
});

test('native: a stopped host is not "missing"', async () => {
  let port;
  const c = new NativeClient(() => (port = fakePort()), { idleMs: 0, lastError: () => ({ message: 'Native host has exited.' }) });
  const p = c.call('raypass.status');
  port.drop();
  await assert.rejects(p, (e) => e.code === 'host_unavailable');
});

test('native: connect throwing is host_missing', async () => {
  const c = new NativeClient(() => { throw new Error('nope'); }, { idleMs: 0 });
  await assert.rejects(c.call('raypass.status'), (e) => e.code === 'host_missing');
});

test('native: idle port closes', async () => {
  const port = fakePort();
  const c = new NativeClient(() => port, { idleMs: 10 });
  const p = c.call('raypass.status');
  port.answer({ id: 1, ok: true, result: {} });
  await p;
  await new Promise((r) => setTimeout(r, 40));
  assert.ok(port.closed);
  assert.equal(c.port, null);
});
