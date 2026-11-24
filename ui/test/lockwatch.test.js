// Tests for ui/lockwatch.js with a fake EventSource and a manual timer.
import { test, assert } from '/kit/testing.js';
import { parseLockState, watchLockState } from '../lockwatch.js';

class FakeES {
  static all = [];
  constructor(url) { this.url = url; this.readyState = 0; this.closed = false; FakeES.all.push(this); }
  close() { this.closed = true; this.readyState = 2; }
  send(obj) { this.readyState = 1; this.onmessage({ data: typeof obj === 'string' ? obj : JSON.stringify(obj) }); }
  drop() { this.readyState = 0; this.onerror(); } // the browser reconnects by itself
  refuse() { this.readyState = 2; this.onerror(); } // an error status: the browser gives up
}

function harness() {
  FakeES.all = [];
  const timers = [];
  const got = [];
  const stop = watchLockState((s) => got.push(s), {
    EventSource: FakeES,
    setTimeout: (f, ms) => { timers.push({ f, ms }); return timers.length; },
    clearTimeout: (id) => { timers[id - 1].f = null; },
  });
  return { got, timers, stop };
}

test('lockwatch: parses only well-formed states', () => {
  assert.deepEqual(parseLockState('{"locked":true,"locks_in":0}'), { locked: true, locks_in: 0 });
  assert.deepEqual(parseLockState('{"locked":false,"locks_in":900,"x":1}'), { locked: false, locks_in: 900 });
  for (const bad of ['', 'nope', 'null', '[]', '{"locked":"yes","locks_in":0}', '{"locked":true}', '{"locked":true,"locks_in":-1}']) {
    assert.equal(parseLockState(bad), null, bad);
  }
});

test('lockwatch: delivers states from /lockstate', () => {
  const h = harness();
  assert.equal(FakeES.all.length, 1);
  assert.equal(FakeES.all[0].url, '/lockstate');
  FakeES.all[0].send({ locked: false, locks_in: 900 });
  FakeES.all[0].send('garbage');
  FakeES.all[0].send({ locked: true, locks_in: 0 });
  assert.deepEqual(h.got, [{ locked: false, locks_in: 900 }, { locked: true, locks_in: 0 }]);
  h.stop();
  assert.ok(FakeES.all[0].closed);
});

test('lockwatch: a dropped connection is left to the browser; a refused one is reopened with backoff', () => {
  const h = harness();
  FakeES.all[0].drop();
  assert.equal(h.timers.length, 0, 'EventSource retries a drop by itself');
  FakeES.all[0].refuse();
  assert.equal(h.timers.length, 1);
  assert.equal(h.timers[0].ms, 1000);
  h.timers[0].f();
  assert.equal(FakeES.all.length, 2);
  FakeES.all[1].refuse();
  assert.equal(h.timers[1].ms, 2000);
  h.timers[1].f();
  FakeES.all[2].send({ locked: true, locks_in: 0 }); // connected again: backoff resets
  FakeES.all[2].refuse();
  assert.equal(h.timers[2].ms, 1000);
  for (let i = 0; i < 10; i++) { h.timers[h.timers.length - 1].f(); FakeES.all[FakeES.all.length - 1].refuse(); }
  assert.equal(h.timers[h.timers.length - 1].ms, 30000, 'at most 30 s apart');
  h.stop();
  assert.equal(h.timers[h.timers.length - 1].f, null, 'stop cancels the pending reopen');
  assert.deepEqual(h.got, [{ locked: true, locks_in: 0 }]);
});
