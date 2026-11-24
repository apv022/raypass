package urlmatch

import "testing"

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
		{"https://github.com", "https://github.com/login", 2},
		{"github.com", "https://www.github.com/", 2},
		{"https://github.com", "https://gist.github.com", 1},
		{"https://github.com", "http://github.com", 2},
		{"https://github.com", "https://github.com.evil.com", 0},
		{"https://evilgithub.com", "https://github.com", 0},
		{"https://alice.github.io", "https://bob.github.io", 0},
		{"https://shop.example.co.uk", "https://login.example.co.uk", 1},
		{"https://a.co.uk", "https://b.co.uk", 0},
		{"http://192.168.1.1:8080", "http://192.168.1.1:8080/admin", 2},
		{"http://192.168.1.1:8080", "http://192.168.1.1:8081", 0},
		{"http://localhost:3000", "http://localhost:3000/x", 2},
		{"http://localhost:3000", "http://localhost:4000", 0},
		{"androidapp://com.example.app", "androidapp://com.example.app", 2},
		{"androidapp://com.example.app", "androidapp://com.example.app2", 0},
		{"androidapp://com.example", "https://example.com", 0},
		{"", "https://x.com", 0},
	}
	for _, c := range cases {
		if got := Match(c.saved, c.page); got != c.want {
			t.Errorf("Match(%q, %q) = %d, want %d", c.saved, c.page, got, c.want)
		}
	}
	if Best([]string{"https://other.com", "https://x.github.com"}, "https://github.com") != 1 {
		t.Fatal("best")
	}
	if Host("https://Mail.Google.com/x") != "mail.google.com" || Host("androidapp://a.b") != "a.b" {
		t.Fatal("host")
	}
}
