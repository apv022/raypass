// URL normalization and matching, mirroring the urlmatch package (design/DESIGN.md "URL
// matching"). The vault does the authoritative matching in raypass.match and raypass.fill; this
// copy decides which frames may be filled (same registrable domain as the top frame) and labels
// hosts in the UI.
//
// A classic script, not a module, so the same file loads as a content script, as an import of
// the background module, and in the test page. It only adds globalThis.Raypass.url.
(function (root) {
  'use strict';

  // Public suffixes with more than one label: the same list as urlmatch/suffixes.txt
  // (cmd/extpack's test keeps them equal). Anything not listed has a one-label suffix, so the
  // registrable domain is the last two labels.
  const MULTI = new Set([
    'co.uk', 'org.uk', 'ac.uk', 'gov.uk', 'me.uk', 'net.uk', 'ltd.uk', 'plc.uk', 'sch.uk',
    'nhs.uk', 'police.uk', 'com.au', 'net.au', 'org.au', 'edu.au', 'gov.au', 'asn.au', 'id.au',
    'co.nz', 'org.nz', 'net.nz', 'govt.nz', 'ac.nz', 'co.jp', 'ne.jp', 'or.jp', 'ac.jp',
    'go.jp', 'ad.jp', 'ed.jp', 'gr.jp', 'lg.jp', 'co.kr', 'or.kr', 'ne.kr', 'go.kr', 'ac.kr',
    'co.in', 'net.in', 'org.in', 'firm.in', 'gen.in', 'ind.in', 'ac.in', 'gov.in', 'com.br',
    'net.br', 'org.br', 'gov.br', 'edu.br', 'com.cn', 'net.cn', 'org.cn', 'gov.cn', 'edu.cn',
    'com.tw', 'org.tw', 'net.tw', 'edu.tw', 'gov.tw', 'com.hk', 'org.hk', 'net.hk', 'edu.hk',
    'gov.hk', 'com.sg', 'org.sg', 'net.sg', 'edu.sg', 'gov.sg', 'com.my', 'org.my', 'net.my',
    'com.mx', 'org.mx', 'gob.mx', 'edu.mx', 'com.ar', 'org.ar', 'gob.ar', 'com.tr', 'org.tr',
    'gov.tr', 'edu.tr', 'co.za', 'org.za', 'gov.za', 'co.il', 'org.il', 'ac.il', 'gov.il',
    'com.ua', 'org.ua', 'net.ua', 'co.id', 'or.id', 'ac.id', 'go.id', 'com.ph', 'org.ph',
    'com.vn', 'net.vn', 'com.pk', 'org.pk', 'com.ng', 'org.ng', 'com.eg', 'com.sa', 'co.th',
    'in.th', 'ac.th', 'go.th', 'com.co', 'com.pe', 'com.ve', 'co.at', 'or.at', 'com.pl',
    'net.pl', 'org.pl', 'com.ru', 'com.es', 'org.es', 'com.gr', 'com.cy', 'com.mt', 'co.ke',
    'com.bd', 'com.np', 'com.lk', 'github.io', 'gitlab.io', 'bitbucket.io', 'codeberg.page',
    'readthedocs.io', 'gitbook.io', 'herokuapp.com', 'appspot.com', 'blogspot.com',
    'pythonanywhere.com', 'netlify.app', 'vercel.app', 'now.sh', 'pages.dev', 'workers.dev',
    'trycloudflare.com', 'web.app', 'firebaseapp.com', 'azurewebsites.net',
    'azurestaticapps.net', 'cloudapp.net', 'cloudfront.net', 's3.amazonaws.com',
    'cloudfunctions.net', 'fly.dev', 'onrender.com', 'glitch.me', 'repl.co', 'surge.sh',
    'deno.dev', 'up.railway.app', 'hf.space', 'streamlit.app', 'ngrok.io', 'ngrok.app',
    'ngrok-free.app', 'loca.lt', 'myshopify.com', 'wordpress.com', 'tumblr.com', 'substack.com',
    'webflow.io', 'neocities.org', '000webhostapp.com', 'sharepoint.com', 'ddns.net',
    'duckdns.org', 'no-ip.org', 'dyndns.org',
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
    if (!s.includes('://')) s = 'https://' + s;
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
    const special = literal(host);
    let port = url.port;
    if (!port) port = scheme === 'http' ? '80' : scheme === 'https' ? '443' : '';
    return { scheme, host, port, special };
  }

  /** Hosts compared with their port, not by domain: IPs, localhost, single-label names. */
  function literal(host) {
    return host.includes(':') || IPV4.test(host) || host === 'localhost' || host.endsWith('.localhost') || !host.includes('.');
  }

  /** The registrable domain (eTLD+1) of host, by the embedded suffix list; the host itself for literal hosts. */
  function registrableDomain(host) {
    host = String(host).toLowerCase().replace(/\.$/, '');
    if (literal(host)) return host;
    const labels = host.split('.');
    // The longest known suffix wins (s3.amazonaws.com over amazonaws.com).
    for (let i = 0; i < labels.length - 1; i++) {
      if (MULTI.has(labels.slice(i + 1).join('.'))) return labels.slice(i).join('.');
    }
    return labels.slice(-2).join('.');
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
    // Registrable-domain matching is for web pages only: javascript://bank.com/ or
    // ftp://bank.com has bank.com as its host but is not the bank's page. Anything not http(s)
    // on either side matches only the same scheme, host and port.
    if (pa.scheme !== 'androidapp' && (!web(pa.scheme) || !web(pb.scheme))) {
      return pa.scheme === pb.scheme && pa.host === pb.host && pa.port === pb.port ? 2 : 0;
    }
    const ka = siteKey(pa);
    if (!ka || ka !== siteKey(pb)) return 0;
    const bare = (h) => h.replace(/^www\./, '');
    return pa.host === pb.host || bare(pa.host) === bare(pb.host) ? 2 : 1;
  }

  /** The schemes of web pages, which match by registrable domain. */
  function web(scheme) { return scheme === 'http' || scheme === 'https'; }

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
