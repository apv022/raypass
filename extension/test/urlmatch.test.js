// URL matching (design/DESIGN.md "URL matching"), mirrored from the urlmatch package.
import { test, assert } from './testing.js';

const U = globalThis.Raypass.url;

test('urlmatch: normalization', () => {
  assert.deepEqual(U.parse('GitHub.com/login'), { scheme: 'https', host: 'github.com', port: '443', special: false });
  assert.deepEqual(U.parse('HTTP://Example.COM:8080/x'), { scheme: 'http', host: 'example.com', port: '8080', special: false });
  assert.equal(U.parse('localhost:3000').port, '3000');
  assert.equal(U.parse('localhost:3000').special, true);
  assert.equal(U.parse('http://192.168.1.1/').special, true);
  assert.equal(U.parse('http://[::1]:8080/').host, '::1');
  assert.equal(U.parse('example.com.').host, 'example.com');
  assert.equal(U.parse(''), null);
  assert.equal(U.parse(null), null);
  assert.equal(U.parse('intranet').special, true, 'single-label names compare with their port');
  assert.deepEqual(U.parse('androidapp://com.example.App'), { scheme: 'androidapp', host: 'com.example.app', port: '', special: true });
});

test('urlmatch: registrable domain', () => {
  assert.equal(U.registrableDomain('github.com'), 'github.com');
  assert.equal(U.registrableDomain('gist.github.com'), 'github.com');
  assert.equal(U.registrableDomain('a.b.example.co.uk'), 'example.co.uk');
  assert.equal(U.registrableDomain('example.co.uk'), 'example.co.uk');
  assert.equal(U.registrableDomain('alice.github.io'), 'alice.github.io');
  assert.equal(U.registrableDomain('www.shop.com.au'), 'shop.com.au');
  assert.equal(U.registrableDomain('com'), 'com');
  assert.equal(U.registrableDomain('x.alice.github.io'), 'alice.github.io');
  assert.equal(U.registrableDomain('bucket.s3.amazonaws.com'), 'bucket.s3.amazonaws.com', 'longest suffix wins');
  assert.equal(U.registrableDomain('a.b.amazonaws.com'), 'amazonaws.com');
  assert.equal(U.registrableDomain('192.168.1.1'), '192.168.1.1');
});

test('urlmatch: matching and rank', () => {
  assert.equal(U.rank('https://github.com', 'https://github.com/login'), 2);
  assert.equal(U.rank('github.com', 'https://gist.github.com/'), 1);
  assert.equal(U.rank('https://github.com', 'http://github.com'), 2, 'scheme does not matter');
  assert.equal(U.rank('https://alice.github.io', 'https://bob.github.io'), 0, 'github.io is a public suffix');
  assert.equal(U.rank('https://a.example.co.uk', 'https://b.example.co.uk'), 1);
  assert.equal(U.rank('https://example.co.uk', 'https://other.co.uk'), 0);
  assert.equal(U.rank('http://192.168.1.1', 'http://192.168.1.1/admin'), 2);
  assert.equal(U.rank('http://192.168.1.1:8080', 'http://192.168.1.1'), 0, 'IPs match on host and port');
  assert.equal(U.rank('localhost:3000', 'http://localhost:3000/'), 2, 'explicit ports agree whatever the scheme');
  assert.equal(U.rank('localhost', 'http://localhost/'), 0, 'default ports differ: https 443, http 80');
  assert.equal(U.rank('http://localhost:3000', 'http://localhost:3000/'), 2);
  assert.equal(U.rank('http://localhost:3000', 'http://localhost:4000/'), 0);
  assert.equal(U.rank('androidapp://com.example', 'androidapp://com.example'), 2);
  assert.equal(U.rank('androidapp://com.example', 'https://example.com'), 0);
  assert.equal(U.rank('https://evil-github.com', 'https://github.com'), 0);
  assert.equal(U.rank('https://github.com.evil.com', 'https://github.com'), 0);
  assert.ok(U.matches('example.com', 'https://www.example.com/'));
  assert.equal(U.rank('example.com', 'https://www.example.com/'), 2, 'www. is the same host');
  assert.equal(U.rank('http://nas:5000', 'http://nas:5000/x'), 2);
  assert.equal(U.rank('http://nas:5000', 'http://nas:5001/'), 0);
});

test('urlmatch: frame policy', () => {
  assert.ok(U.sameSite('https://login.example.com/frame', 'https://www.example.com/'));
  assert.ok(!U.sameSite('https://ads.tracker.net/', 'https://www.example.com/'));
  assert.ok(!U.sameSite('about:blank', 'https://www.example.com/'));
  assert.ok(!U.sameSite('data:text/html,x', 'https://www.example.com/'));
  assert.ok(!U.sameSite('https://example.com/', 'chrome://newtab/'));
  assert.ok(U.fillable('http://example.com/'));
  assert.ok(!U.fillable('file:///etc/passwd'));
  assert.ok(!U.fillable('chrome-extension://abc/popup.html'));
  assert.equal(U.displayHost('https://www.example.com/x'), 'www.example.com');
});

// The urlmatch package's TestMatch table, case for case, so Go and the extension agree.
test('urlmatch: the Go TestMatch table', () => {
  const cases = [
    ['https://github.com', 'https://github.com/login', 2],
    ['github.com', 'https://www.github.com/', 2],
    ['https://github.com', 'https://gist.github.com', 1],
    ['https://github.com', 'http://github.com', 2],
    ['https://github.com', 'https://github.com.evil.com', 0],
    ['https://evilgithub.com', 'https://github.com', 0],
    ['https://alice.github.io', 'https://bob.github.io', 0],
    ['https://shop.example.co.uk', 'https://login.example.co.uk', 1],
    ['https://a.co.uk', 'https://b.co.uk', 0],
    ['http://192.168.1.1:8080', 'http://192.168.1.1:8080/admin', 2],
    ['http://192.168.1.1:8080', 'http://192.168.1.1:8081', 0],
    ['http://localhost:3000', 'http://localhost:3000/x', 2],
    ['http://localhost:3000', 'http://localhost:4000', 0],
    ['androidapp://com.example.app', 'androidapp://com.example.app', 2],
    ['androidapp://com.example.app', 'androidapp://com.example.app2', 0],
    ['androidapp://com.example', 'https://example.com', 0],
    ['', 'https://x.com', 0],
    // Not web pages: the host alone must not let a login fill.
    ['https://bank.com', 'javascript://bank.com/%0aalert(1)', 0],
    ['https://bank.com', 'ftp://bank.com', 0],
    ['https://bank.com', 'data://bank.com/x', 0],
    ['https://bank.com', 'javascript:alert(1)', 0],
    ['https://bank.com', 'data:text/html,https://bank.com', 0],
    ['ftp://files.example.com', 'ftp://files.example.com/pub', 2],
    ['ftp://files.example.com', 'https://files.example.com', 0],
    // Userinfo is not the host; trailing dots and case do not matter.
    ['https://bank.com', 'https://bank.com@evil.com/', 0],
    ['https://bank.com', 'https://evil.com@bank.com/', 2],
    ['https://bank.com', 'HTTPS://BANK.COM./login', 2],
    ['https://bank.com', 'https://bank.com.evil.com.', 0],
    // Private suffixes: every customer of a host is a different site.
    ['https://alice.herokuapp.com', 'https://bob.herokuapp.com', 0],
    ['https://shop.myshopify.com', 'https://admin.shop.myshopify.com', 1],
    ['https://a.up.railway.app', 'https://b.up.railway.app', 0],
  ];
  for (const [a, b, want] of cases) assert.equal(U.rank(a, b), want, `rank(${a}, ${b})`);
});
