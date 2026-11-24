// Package urlmatch decides whether a saved login belongs to a page or an app
// (design/DESIGN.md "URL matching").
//
// Rules: URLs are normalized (lowercase scheme and host, https when no scheme is given). Two web
// (http or https) URLs match when their registrable domains (eTLD+1, from an embedded short list of multi-part
// public suffixes, else the last two labels) are equal; for IP addresses and localhost, host
// and port must be equal. Any other scheme matches only the same scheme, host and port (so
// javascript://bank.com/ is not bank.com). Android apps are androidapp://<package> and match exactly.
// A login saved with an https URL that matches an http page is Insecure: clients warn before
// filling it.
//
// Invariant: matching is pure string work; nothing is fetched from the network.
package urlmatch

import (
	_ "embed"
	"net"
	"net/url"
	"strings"
)

//go:embed suffixes.txt
var suffixesTxt string

var suffixes = map[string]bool{}

func init() {
	for _, line := range strings.Split(suffixesTxt, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		for _, s := range strings.Fields(line) {
			suffixes[strings.ToLower(s)] = true
		}
	}
}

// URL is a normalized URL.
type URL struct {
	Scheme  string // lowercase; "androidapp" for apps
	Host    string // lowercase, no trailing dot, no brackets for IPv6
	Port    string // as given, or the scheme's default ("443" for https, "80" for http)
	Android string // the package of an androidapp:// URL
}

// Parse normalizes a URL as typed or saved. ok is false for something that has no host.
func Parse(raw string) (URL, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return URL{}, false
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return URL{}, false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "androidapp" {
		pkg := strings.ToLower(u.Host + strings.TrimSuffix(u.Path, "/"))
		if pkg == "" || strings.ContainsAny(pkg, "/?#") {
			return URL{}, false
		}
		return URL{Scheme: scheme, Android: pkg}, true
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return URL{}, false
	}
	port := u.Port()
	if port == "" {
		switch scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return URL{Scheme: scheme, Host: host, Port: port}, true
}

// String is the normalized form.
func (u URL) String() string {
	if u.Android != "" {
		return "androidapp://" + u.Android
	}
	h := u.Host
	if strings.Contains(h, ":") {
		h = "[" + h + "]"
	}
	if (u.Scheme == "https" && u.Port == "443") || (u.Scheme == "http" && u.Port == "80") || u.Port == "" {
		return u.Scheme + "://" + h
	}
	return u.Scheme + "://" + h + ":" + u.Port
}

// literal reports hosts that are compared with their port, not by registrable domain.
func literal(host string) bool {
	return net.ParseIP(host) != nil || host == "localhost" || strings.HasSuffix(host, ".localhost") || !strings.Contains(host, ".")
}

// Registrable returns the registrable domain of a host (eTLD+1), or the host itself for IP
// addresses, localhost and single-label names.
func Registrable(host string) string {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if literal(host) {
		return host
	}
	labels := strings.Split(host, ".")
	// The longest known suffix wins (s3.amazonaws.com over amazonaws.com).
	for i := 0; i < len(labels)-1; i++ {
		if suffixes[strings.Join(labels[i+1:], ".")] {
			return strings.Join(labels[i:], ".")
		}
	}
	if len(labels) <= 2 {
		return host
	}
	return strings.Join(labels[len(labels)-2:], ".")
}

// Host returns the lowercased host of a URL ("" if none), the package for androidapp:// URLs.
func Host(raw string) string {
	u, ok := Parse(raw)
	if !ok {
		return ""
	}
	if u.Android != "" {
		return u.Android
	}
	return u.Host
}

// Match scores how well saved matches page: 0 no match, 1 same registrable domain, 2 the same
// host (or the same app).
func Match(saved, page string) int {
	a, ok1 := Parse(saved)
	b, ok2 := Parse(page)
	if !ok1 || !ok2 {
		return 0
	}
	if a.Android != "" || b.Android != "" {
		if a.Android != "" && a.Android == b.Android {
			return 2
		}
		return 0
	}
	// Registrable-domain matching is for web pages only. A javascript://bank.com/ or
	// ftp://bank.com URL has bank.com as its host but is not the bank's page: anything not
	// http(s) on either side matches only the same scheme, host and port.
	if !web(a.Scheme) || !web(b.Scheme) {
		if a.Scheme == b.Scheme && a.Host == b.Host && a.Port == b.Port {
			return 2
		}
		return 0
	}
	if literal(a.Host) || literal(b.Host) {
		if a.Host == b.Host && a.Port == b.Port {
			return 2
		}
		return 0
	}
	if a.Host == b.Host || strings.TrimPrefix(a.Host, "www.") == strings.TrimPrefix(b.Host, "www.") {
		return 2
	}
	if Registrable(a.Host) == Registrable(b.Host) {
		return 1
	}
	return 0
}

// web reports the schemes of web pages, which match by registrable domain.
func web(scheme string) bool { return scheme == "http" || scheme == "https" }

// Insecure reports whether page is a plain http page that urls match only through https URLs:
// a login saved for a secure site, offered on an insecure page of the same site, where anyone on
// the network can read what is typed. A login that also has a matching http URL was saved for
// the insecure page on purpose, so it is not reported.
func Insecure(urls []string, page string) bool {
	p, ok := Parse(page)
	if !ok || p.Scheme != "http" {
		return false
	}
	secure := false
	for _, raw := range urls {
		if Match(raw, page) == 0 {
			continue
		}
		u, _ := Parse(raw)
		if u.Scheme != "https" {
			return false
		}
		secure = true
	}
	return secure
}

// Best is the best Match of any of urls against page.
func Best(urls []string, page string) int {
	best := 0
	for _, u := range urls {
		if s := Match(u, page); s > best {
			best = s
		}
	}
	return best
}
