// The client side of the native messaging host (cmd/raypass-native): one port, requests
// correlated by id, every request with a timeout, every pending request failed when the port
// drops. connect() is injected (chrome.runtime.connectNative in the background, a fake in tests).
// A classic script (see urlmatch.js); it adds globalThis.Raypass.NativeClient.
(function (root) {
  'use strict';

  /** An error with the host's {code, message}. */
  class RaypassError extends Error {
    constructor(code, message) {
      super(message || code);
      this.code = code;
    }
  }

  // How long the extension waits for each action: a little longer than the host's own timeout,
  // so the host's "timeout" answer wins when sol hangs.
  const TIMEOUTS = { 'raypass.unlock': 95000, ping: 5000 };
  const DEFAULT_TIMEOUT = 25000;

  class NativeClient {
    /**
     * connect() returns a Port ({postMessage, onMessage, onDisconnect, disconnect});
     * lastError(port) says why it disconnected (chrome.runtime.lastError, or Firefox's port.error).
     */
    constructor(connect, { lastError = () => null, idleMs = 60000 } = {}) {
      this.connect = connect;
      // The port is closed after idleMs without requests: an open native port keeps Chrome's
      // service worker (and the host process) alive for nothing.
      this.idleMs = idleMs;
      this.idleTimer = null;
      this.lastError = lastError;
      this.port = null;
      this.next = 1;
      this.pending = new Map();
    }

    ensure() {
      if (this.port) return this.port;
      const port = this.connect();
      this.port = port;
      port.onMessage.addListener((m) => this.onMessage(m));
      port.onDisconnect.addListener(() => {
        if (this.port !== port) return;
        this.port = null;
        const err = this.lastError(port);
        // Chrome's text for a host that is not installed (or not allowed for this extension).
        const missing = err && /not found|forbidden|not exist|No such native/i.test(err.message || '');
        this.drop(new RaypassError(missing ? 'host_missing' : 'host_unavailable',
          missing ? 'the Raypass native host is not installed (raypass-native install)' : 'the Raypass native host stopped'));
      });
      return port;
    }

    drop(err) {
      for (const p of this.pending.values()) { clearTimeout(p.timer); p.reject(err); }
      this.pending.clear();
    }

    idle() {
      clearTimeout(this.idleTimer);
      if (this.pending.size === 0 && this.idleMs > 0) {
        this.idleTimer = setTimeout(() => { if (this.pending.size === 0) this.close(); }, this.idleMs);
      }
    }

    onMessage(m) {
      if (!m || typeof m !== 'object') return;
      const p = this.pending.get(m.id);
      if (!p) return;
      this.pending.delete(m.id);
      clearTimeout(p.timer);
      this.idle();
      if (m.ok === true) p.resolve(m.result);
      else p.reject(new RaypassError(m.error?.code || 'internal', m.error?.message || 'failed'));
    }

    /** Runs action with args through the host; resolves with its result. */
    call(action, args = {}) {
      return new Promise((resolve, reject) => {
        let port;
        try { port = this.ensure(); } catch (e) {
          reject(new RaypassError('host_missing', String(e?.message || e)));
          return;
        }
        clearTimeout(this.idleTimer);
        const id = this.next++;
        const timer = setTimeout(() => {
          this.pending.delete(id);
          this.idle();
          reject(new RaypassError('timeout', 'Raypass did not answer in time'));
        }, TIMEOUTS[action] || DEFAULT_TIMEOUT);
        this.pending.set(id, { resolve, reject, timer });
        try {
          port.postMessage({ id, action, args });
        } catch (e) {
          this.pending.delete(id);
          clearTimeout(timer);
          this.port = null;
          reject(new RaypassError('host_unavailable', String(e?.message || e)));
        }
      });
    }

    /** Closes the port (the host exits when its stdin closes). */
    close() {
      if (this.port) { try { this.port.disconnect(); } catch { /* gone */ } }
      this.port = null;
      this.drop(new RaypassError('host_unavailable', 'closed'));
    }
  }

  /** Whether err is the vault's "locked" answer (forbidden/locked, design/DESIGN.md). */
  function isLocked(err) { return !!err && err.code === 'forbidden' && err.message === 'locked'; }

  const ns = root.Raypass || (root.Raypass = {});
  ns.NativeClient = NativeClient;
  ns.RaypassError = RaypassError;
  ns.isLocked = isLocked;
})(globalThis);
