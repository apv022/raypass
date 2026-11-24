package totp

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// RFC 4226 appendix D: secret "12345678901234567890", counters 0..9.
func TestHOTPRFC4226(t *testing.T) {
	want := []string{"755224", "287082", "359152", "969429", "338314", "254676", "287922", "162583", "399871", "520489"}
	for i, w := range want {
		if got := HOTP([]byte("12345678901234567890"), uint64(i), 6, SHA1); got != w {
			t.Errorf("counter %d: got %s want %s", i, got, w)
		}
	}
}

// RFC 6238 appendix B, read from the shared testdata/vectors.json so Java checks the same list.
func TestTOTPRFC6238(t *testing.T) {
	raw, err := os.ReadFile("../testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		TOTP []struct {
			SecretASCII string `json:"secret_ascii"`
			URI         string `json:"uri"`
			Algorithm   string `json:"algorithm"`
			Digits      int    `json:"digits"`
			Period      int    `json:"period"`
			Time        int64  `json:"time"`
			Code        string `json:"code"`
		} `json:"totp"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.TOTP) < 18 {
		t.Fatalf("only %d totp vectors", len(v.TOTP))
	}
	for i, c := range v.TOTP {
		var p Params
		if c.URI != "" {
			p, err = Parse(c.URI)
			if err != nil {
				t.Fatalf("vector %d: parse: %v", i, err)
			}
		} else {
			p = Params{Secret: []byte(c.SecretASCII), Algorithm: c.Algorithm, Digits: c.Digits, Period: c.Period}
		}
		got, _ := p.Code(time.Unix(c.Time, 0))
		if got != c.Code {
			t.Errorf("vector %d (%s, t=%d): got %s want %s", i, c.Algorithm, c.Time, got, c.Code)
		}
	}
}

func TestParse(t *testing.T) {
	p, err := Parse("otpauth://totp/ACME%20Co:john@example.com?secret=HXDMVJECJJWSRB3HWIZR4IFUGFTMXBOZ&issuer=ACME%20Co&algorithm=SHA256&digits=8&period=60")
	if err != nil {
		t.Fatal(err)
	}
	if p.Issuer != "ACME Co" || p.Account != "john@example.com" || p.Algorithm != SHA256 || p.Digits != 8 || p.Period != 60 || len(p.Secret) != 20 {
		t.Fatalf("%+v", p)
	}
	p, err = Parse("otpauth://totp/Example:alice?secret=JBSWY3DPEHPK3PXP")
	if err != nil || p.Issuer != "Example" || p.Algorithm != SHA1 || p.Digits != 6 || p.Period != 30 {
		t.Fatalf("%+v %v", p, err)
	}
	p, err = Parse("  jbsw y3dp ehpk 3pxp ")
	if err != nil || string(p.Secret) != "Hello!\xde\xad\xbe\xef" {
		t.Fatalf("bare: %v", err)
	}
	p, err = Parse("steam://JBSWY3DPEHPK3PXP")
	if err != nil || !p.Steam || p.Digits != 5 {
		t.Fatalf("steam: %+v %v", p, err)
	}
	code, rem := p.Code(time.Unix(1000, 0))
	if len(code) != 5 || strings.Trim(code, steamChars) != "" || rem != 20 {
		t.Fatalf("steam code %q %d", code, rem)
	}
	if p, err = Parse("otpauth://totp/Steam:me?secret=JBSWY3DPEHPK3PXP&encoder=steam"); err != nil || !p.Steam {
		t.Fatal("encoder=steam")
	}
	for _, bad := range []string{"", "otpauth://hotp/x?secret=JBSWY3DPEHPK3PXP&counter=1", "otpauth://totp/x?secret=JBSWY3DPEHPK3PXP&digits=12",
		"otpauth://totp/x?secret=JBSWY3DPEHPK3PXP&algorithm=MD5", "otpauth://totp/x", "not base32!", "otpauth://totp/x?secret=JBSWY3DPEHPK3PXP&period=0"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestRemaining(t *testing.T) {
	p := Params{Secret: []byte("x"), Algorithm: SHA1, Digits: 6, Period: 30}
	if _, rem := p.Code(time.Unix(59, 0)); rem != 1 {
		t.Fatal(rem)
	}
	if _, rem := p.Code(time.Unix(60, 0)); rem != 30 {
		t.Fatal(rem)
	}
}
