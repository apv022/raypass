// Live lock state from the app (GET /lockstate, a text/event-stream on this origin; see
// lockstate.go and design/DESIGN.md "Lock state"). The app pushes {"locked","locks_in"} at
// once and whenever the vault locks, unlocks or its auto-lock deadline moves, so the page wipes
// itself the moment the app auto-locks with no timer and no polling.
//
// EventSource reconnects by itself after a dropped connection (the app restarted, the laptop
// slept) and the app sends the current state first on every connection. It gives up only
// when an answer is not a stream (an error status): then this waits and opens a new one, 1 s,
// 2 s, 4 s … at most 30 s apart. EventSource and the timer are injectable for ui/test.

/** Whether v is a lock state message: {locked: boolean, locks_in: number ≥ 0}. */
export function parseLockState(data) {
  let v;
  try { v = JSON.parse(data); } catch { return null; }
  if (!v || typeof v !== 'object' || typeof v.locked !== 'boolean' ||
      typeof v.locks_in !== 'number' || !(v.locks_in >= 0)) return null;
  return { locked: v.locked, locks_in: v.locks_in };
}

/**
 * Follows /lockstate, calling onState({locked, locks_in}) for each message. Returns stop().
 * opts: url, EventSource, setTimeout, clearTimeout (tests).
 */
export function watchLockState(onState, opts = {}) {
  const ES = opts.EventSource || globalThis.EventSource;
  const url = opts.url || '/lockstate';
  const later = opts.setTimeout || globalThis.setTimeout.bind(globalThis);
  const cancel = opts.clearTimeout || globalThis.clearTimeout.bind(globalThis);
  let es = null, timer = 0, failures = 0, stopped = false;
  function open() {
    timer = 0;
    if (stopped) return;
    es = new ES(url);
    es.onmessage = (e) => {
      failures = 0;
      const s = parseLockState(e.data);
      if (s) onState(s);
    };
    es.onerror = () => {
      if (stopped || es.readyState !== 2 /* CLOSED: the browser will not retry */) return;
      es.close();
      es = null;
      const wait = Math.min(30000, 1000 * 2 ** Math.min(failures, 5));
      failures++;
      timer = later(open, wait);
    };
  }
  open();
  return function stop() {
    stopped = true;
    if (timer) cancel(timer);
    timer = 0;
    if (es) es.close();
    es = null;
  };
}
