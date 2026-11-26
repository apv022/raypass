package urlmatch

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	for in, want := range map[string]string{
		"GitHub.com":                     "https://github.com",
		"HTTP://Example.COM.:8080/x?y=1": "http://example.com:8080",
		"https://[::1]:9000/":            "https://[::1]:9000",
		"androidapp://com.Example.App":   "androidapp://com.example.app",
		" https://a.b.co.uk/login ":      "https://a.b.co.uk",
	} {
		u, ok := Parse(in)
		if !ok || u.String() != want {
			t.Errorf("%q: got %q", in, u.String())
		}
	}
	for _, bad := range []string{"", "https://", "androidapp://"} {
		if _, ok := Parse(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestRegistrable(t *testing.T) {
	for in, want := range map[string]string{
		"github.com":              "github.com",
		"gist.github.com":         "github.com",
		"a.b.example.co.uk":       "example.co.uk",
		"alice.github.io":         "alice.github.io",
		"x.alice.github.io":       "alice.github.io",
		"bucket.s3.amazonaws.com": "bucket.s3.amazonaws.com",
		"x.alice.netlify.app":     "alice.netlify.app",
		"a.b.up.railway.app":      "b.up.railway.app",
		"docs.readthedocs.io":     "docs.readthedocs.io",
		"localhost":               "localhost",
		"192.168.1.1":             "192.168.1.1",
		"co.uk":                   "co.uk",
		"nas":                     "nas",
	} {
		if got := Registrable(in); got != want {
			t.Errorf("%s: got %s want %s", in, got, want)
		}
	}
}

func TestMatch(t *testing.T) {
	cases := []struct {
		saved, page string
		want        int
	}{
		{"https://github.com", "https://github.com/login", SameHost},
		{"github.com", "https://www.github.com/", SameHost},
		{"https://github.com", "https://gist.github.com", SameDomain},
		{"https://github.com", "http://github.com", SameHost},
		{"https://github.com", "https://github.com.evil.com", NoMatch},
		{"https://evilgithub.com", "https://github.com", NoMatch},
		{"https://alice.github.io", "https://bob.github.io", NoMatch},
		{"https://shop.example.co.uk", "https://login.example.co.uk", SameDomain},
		{"https://a.co.uk", "https://b.co.uk", NoMatch},
		{"http://192.168.1.1:8080", "http://192.168.1.1:8080/admin", SameHost},
		{"http://192.168.1.1:8080", "http://192.168.1.1:8081", NoMatch},
		{"http://localhost:3000", "http://localhost:3000/x", SameHost},
		{"http://localhost:3000", "http://localhost:4000", NoMatch},
		{"androidapp://com.example.app", "androidapp://com.example.app", SameHost},
		{"androidapp://com.example.app", "androidapp://com.example.app2", NoMatch},
		{"androidapp://com.example", "https://example.com", NoMatch},
		{"", "https://x.com", NoMatch},
		// Not web pages: the host alone must not let a login fill.
		{"https://bank.com", "javascript://bank.com/%0aalert(1)", NoMatch},
		{"https://bank.com", "ftp://bank.com", NoMatch},
		{"https://bank.com", "data://bank.com/x", NoMatch},
		{"https://bank.com", "javascript:alert(1)", NoMatch},
		{"https://bank.com", "data:text/html,https://bank.com", NoMatch},
		{"ftp://files.example.com", "ftp://files.example.com/pub", SameHost},
		{"ftp://files.example.com", "https://files.example.com", NoMatch},
		// Userinfo is not the host; trailing dots and case do not matter.
		{"https://bank.com", "https://bank.com@evil.com/", NoMatch},
		{"https://bank.com", "https://evil.com@bank.com/", SameHost},
		{"https://bank.com", "HTTPS://BANK.COM./login", SameHost},
		{"https://bank.com", "https://bank.com.evil.com.", NoMatch},
		// Private suffixes: every customer of a host is a different site.
		{"https://alice.herokuapp.com", "https://bob.herokuapp.com", NoMatch},
		{"https://shop.myshopify.com", "https://admin.shop.myshopify.com", SameDomain},
		{"https://a.up.railway.app", "https://b.up.railway.app", NoMatch},
		// Equivalent domains (equivalent.txt): one account, ranked below the same domain.
		{"https://x.com", "https://twitter.com/login", Equivalent},
		{"https://twitter.com", "https://mobile.x.com", Equivalent},
		{"https://accounts.google.com", "https://www.youtube.com", Equivalent},
		{"https://login.live.com", "https://outlook.com", Equivalent},
		{"https://icloud.com", "https://appleid.apple.com", Equivalent},
		{"https://x.com", "https://twitter.com.evil.com", NoMatch},
		{"https://x.com", "https://facebook.com", NoMatch},
		{"https://facebook.com", "https://instagram.com", NoMatch},
		{"https://x.com", "ftp://twitter.com", NoMatch},
		{"androidapp://com.twitter.android", "https://x.com", NoMatch},
		{"https://x.com", "http://twitter.com", Equivalent},
	}
	for _, c := range cases {
		if got := Match(c.saved, c.page); got != c.want {
			t.Errorf("Match(%q, %q) = %d, want %d", c.saved, c.page, got, c.want)
		}
	}
	if Best([]string{"https://other.com", "https://x.github.com"}, "https://github.com") != SameDomain {
		t.Fatal("best")
	}
	if Host("https://Mail.Google.com/x") != "mail.google.com" || Host("androidapp://a.b") != "a.b" {
		t.Fatal("host")
	}
}

func TestEquivalentFile(t *testing.T) {
	seen := map[string]bool{}
	groups := 0
	for _, line := range strings.Split(equivalentTxt, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") || strings.TrimSpace(line) == "" {
			continue
		}
		groups++
		ds := strings.Fields(line)
		if len(ds) < 2 {
			t.Errorf("a group needs two domains: %q", line)
		}
		for _, d := range ds {
			if seen[d] {
				t.Errorf("%s is in two groups", d)
			}
			seen[d] = true
			if Registrable(d) != d || d != strings.ToLower(d) {
				t.Errorf("%s is not a lowercase registrable domain", d)
			}
		}
	}
	if groups == 0 || !EquivalentDomains("x.com", "twitter.com") || EquivalentDomains("x.com", "x.com") || EquivalentDomains("x.com", "google.com") {
		t.Fatal("equivalent groups not loaded")
	}
	// A login saved for x.com, offered on twitter.com, prefers the x.com login of the same domain.
	if Best([]string{"https://twitter.com", "https://x.com"}, "https://x.com") != SameHost {
		t.Fatal("best prefers the same host")
	}
}

func TestInsecure(t *testing.T) {
	cases := []struct {
		urls []string
		page string
		want bool
	}{
		{[]string{"https://bank.com"}, "http://bank.com/login", true},
		{[]string{"bank.com"}, "http://www.bank.com/", true}, // no scheme means https
		{[]string{"https://bank.com"}, "http://login.bank.com/", true},
		{[]string{"https://bank.com"}, "https://bank.com/login", false},
		{[]string{"http://bank.com"}, "http://bank.com/login", false},
		{[]string{"https://bank.com", "http://bank.com"}, "http://bank.com/", false},
		{[]string{"https://other.com"}, "http://bank.com/", false}, // no match at all
		{[]string{"https://bank.com", "https://other.com"}, "http://bank.com/", true},
		{[]string{"http://localhost:8080"}, "http://localhost:8080/", false},
		{[]string{"https://localhost:8080"}, "http://localhost:8080/", true},
		{nil, "http://bank.com/", false},
		{[]string{"https://bank.com"}, "ftp://bank.com", false},
		{[]string{"https://x.com"}, "http://twitter.com/", true}, // equivalent domains count too
	}
	for _, c := range cases {
		if got := Insecure(c.urls, c.page); got != c.want {
			t.Errorf("Insecure(%q, %q) = %v, want %v", c.urls, c.page, got, c.want)
		}
	}
}
