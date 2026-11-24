// URL normalization and matching, mirroring the urlmatch package (design/DESIGN.md "URL
// matching"). The vault does the authoritative matching in raypass.match and raypass.fill; this
// copy decides which frames may be filled (same registrable domain as the top frame) and labels
// hosts in the UI.
//
// A classic script, not a module, so the same file loads as a content script, as an import of
// the background module, and in the test page. It only adds globalThis.Raypass.url.
(function (root) {
  'use strict';

  // Public suffixes with more than one label. Anything not listed has a one-label suffix, so
  // the registrable domain is the last two labels.
  const MULTI = new Set([
    'co.uk', 'org.uk', 'ac.uk', 'gov.uk', 'me.uk', 'ltd.uk', 'plc.uk', 'net.uk', 'sch.uk', 'nhs.uk',
    'co.jp', 'ne.jp', 'or.jp', 'ac.jp', 'go.jp', 'ed.jp',
    'com.au', 'net.au', 'org.au', 'edu.au', 'gov.au', 'id.au',
    'co.nz', 'org.nz', 'net.nz', 'govt.nz', 'ac.nz',
    'com.br', 'net.br', 'org.br', 'gov.br', 'com.cn', 'net.cn', 'org.cn', 'gov.cn', 'edu.cn',
    'com.tw', 'org.tw', 'com.hk', 'org.hk', 'co.in', 'net.in', 'org.in', 'gov.in', 'ac.in',
    'co.za', 'org.za', 'com.mx', 'org.mx', 'com.ar', 'com.tr', 'org.tr', 'co.kr', 'or.kr',
    'com.sg', 'com.my', 'co.id', 'co.il', 'org.il', 'com.ua', 'com.pl', 'co.th', 'com.vn',
    'com.ph', 'com.pk', 'com.eg', 'com.sa', 'com.co', 'com.pe', 'com.ng', 'co.ke', 'com.es',
    'github.io', 'gitlab.io', 'herokuapp.com', 'vercel.app', 'netlify.app', 'pages.dev',
    'workers.dev', 'web.app', 'firebaseapp.com', 'azurewebsites.net', 'cloudfront.net',
    'blogspot.com', 'appspot.com', 'onrender.com', 'fly.dev', 'glitch.me', 'repl.co',
  ]);

  const IPV4 = /^\d{1,3}(\.\d{1,3}){3}$/;

  /**
   * Parses u into {scheme, host, port, special}, or null when it is not a URL we can match.
   * Scheme and host are lower-cased; a missing scheme means https. `special` is true for IP
   * addresses and localhost (matched on host and port, not on a domain).
   */
  function parse(u) {
    if (typeof u !== 'string') return null;
    let s = u.trim();
    if (!s) return null;
    // "example.com/login" and "example.com:8443" have no scheme; "mailto:x" has one.
    if (!/^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//.test(s) && !/^[a-zA-Z][a-zA-Z0-9+.-]*:(?!\d)/.test(s)) {
      s = 'https://' + s;
    }
    const scheme = /^([a-zA-Z][a-zA-Z0-9+.-]*):/.exec(s)[1].toLowerCase();
    if (scheme === 'androidapp') {
      const pkg = s.slice('androidapp://'.length).replace(/\/+$/, '');
      return pkg ? { scheme, host: pkg.toLowerCase(), port: '', special: true } : null;
    }
    let url;
    try { url = new URL(s); } catch { return null; }
    let host = url.hostname.toLowerCase().replace(/\.$/, '');
    if (!host) return null;
    const v6 = host.startsWith('[');
    if (v6) host = host.slice(1, -1);
    const special = v6 || IPV4.test(host) || host === 'localhost' || host.endsWith('.localhost');
    let port = url.port;
    if (!port) port = scheme === 'http' ? '80' : scheme === 'https' ? '443' : '';
    return { scheme, host, port, special };
  }

  /** The registrable domain (eTLD+1) of host, by the embedded suffix list. */
  function registrableDomain(host) {
    const labels = String(host).toLowerCase().replace(/\.$/, '').split('.').filter(Boolean);
    if (labels.length <= 2) return labels.join('.');
    const last2 = labels.slice(-2).join('.');
    const n = MULTI.has(last2) ? 3 : 2;
    return labels.slice(-n).join('.');
  }

  /** The key two URLs must share to match: registrable domain, or host:port for IPs/localhost. */
  function siteKey(p) {
    if (!p) return '';
    if (p.scheme === 'androidapp') return 'androidapp:' + p.host;
    if (p.special) return p.host + ':' + p.port;
    return registrableDomain(p.host);
  }

  /** How well item URL a matches page URL b: 2 same host, 1 same site, 0 no match. */
  function rank(a, b) {
    const pa = parse(a), pb = parse(b);
    if (!pa || !pb) return 0;
    if ((pa.scheme === 'androidapp') !== (pb.scheme === 'androidapp')) return 0;
    const ka = siteKey(pa);
    if (!ka || ka !== siteKey(pb)) return 0;
    return pa.host === pb.host ? 2 : 1;
  }

  /** Whether item URL a matches page URL b. */
  function matches(a, b) { return rank(a, b) > 0; }

  /**
   * Whether a frame at frameUrl may be filled in a tab whose top frame is at topUrl: the top
   * frame itself, or a web frame of the same site. Opaque and non-web frames never are.
   */
  function sameSite(frameUrl, topUrl) {
    const f = parse(frameUrl), t = parse(topUrl);
    if (!f || !t) return false;
    if (!/^https?$/.test(f.scheme) || !/^https?$/.test(t.scheme)) return false;
    return siteKey(f) === siteKey(t);
  }

  /** Whether the page is one we may fill at all (http or https). */
  function fillable(u) {
    const p = parse(u);
    return !!p && (p.scheme === 'https' || p.scheme === 'http');
  }

  /** The host to show a person for u ("" if none). */
  function displayHost(u) {
    const p = parse(u);
    return p ? p.host : '';
  }

  const ns = root.Raypass || (root.Raypass = {});
  ns.url = { parse, registrableDomain, rank, matches, sameSite, fillable, displayHost };
})(globalThis);
