// Package totp computes one-time codes: HOTP (RFC 4226), TOTP (RFC 6238) with SHA1, SHA256 or
// SHA512, 6 to 8 digits and any period, and Steam Guard codes. It parses what people paste:
// otpauth:// URIs, steam:// secrets and bare base32 secrets.
//
// Invariant: errors never contain the secret.
package totp

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"hash"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Algorithms.
const (
	SHA1   = "SHA1"
	SHA256 = "SHA256"
	SHA512 = "SHA512"
)

// Params is a parsed TOTP configuration.
type Params struct {
	Secret    []byte
	Algorithm string // SHA1, SHA256 or SHA512
	Digits    int    // 6..8 (5 for Steam)
	Period    int    // seconds
	Issuer    string
	Account   string
	Steam     bool
}

// ErrInvalid is returned for anything that is not a usable TOTP secret or URI.
var ErrInvalid = errors.New("not a valid TOTP secret or otpauth URI")

// steamChars is Steam Guard's code alphabet.
const steamChars = "23456789BCDFGHJKMNPQRTVWXY"

// Parse reads an otpauth://totp/… URI, a steam://SECRET (or otpauth://steam/…, or an otpauth
// URI with encoder=steam), or a bare base32 secret (spaces and dashes ignored, any case,
// padding optional).
func Parse(s string) (Params, error) {
	s = strings.TrimSpace(s)
	p := Params{Algorithm: SHA1, Digits: 6, Period: 30}
	low := strings.ToLower(s)
	switch {
	case strings.HasPrefix(low, "steam://"):
		sec, err := DecodeSecret(s[len("steam://"):])
		if err != nil {
			return Params{}, err
		}
		p.Secret, p.Steam, p.Digits = sec, true, 5
		return p, nil
	case strings.HasPrefix(low, "otpauth://"):
		return parseURI(s, p)
	}
	sec, err := DecodeSecret(s)
	if err != nil {
		return Params{}, err
	}
	p.Secret = sec
	return p, nil
}

func parseURI(s string, p Params) (Params, error) {
	u, err := url.Parse(s)
	if err != nil {
		return Params{}, ErrInvalid
	}
	kind := strings.ToLower(u.Host)
	if kind != "totp" && kind != "steam" {
		return Params{}, ErrInvalid // hotp (counter based) is not supported: nothing could keep the counter in sync
	}
	q := u.Query()
	sec, err := DecodeSecret(q.Get("secret"))
	if err != nil {
		return Params{}, err
	}
	p.Secret = sec
	label := strings.TrimPrefix(u.Path, "/")
	if i := strings.Index(label, ":"); i >= 0 {
		p.Issuer, p.Account = strings.TrimSpace(label[:i]), strings.TrimSpace(label[i+1:])
	} else {
		p.Account = strings.TrimSpace(label)
	}
	if iss := q.Get("issuer"); iss != "" {
		p.Issuer = iss
	}
	if kind == "steam" || strings.EqualFold(q.Get("encoder"), "steam") || strings.EqualFold(p.Issuer, "steam") && q.Get("digits") == "5" {
		p.Steam, p.Digits = true, 5
		return p, nil
	}
	if a := q.Get("algorithm"); a != "" {
		switch strings.ToUpper(strings.ReplaceAll(a, "-", "")) {
		case "SHA1":
			p.Algorithm = SHA1
		case "SHA256":
			p.Algorithm = SHA256
		case "SHA512":
			p.Algorithm = SHA512
		default:
			return Params{}, ErrInvalid
		}
	}
	if d := q.Get("digits"); d != "" {
		n, err := strconv.Atoi(d)
		if err != nil || n < 6 || n > 8 {
			return Params{}, ErrInvalid
		}
		p.Digits = n
	}
	if d := q.Get("period"); d != "" {
		n, err := strconv.Atoi(d)
		if err != nil || n < 1 || n > 3600 {
			return Params{}, ErrInvalid
		}
		p.Period = n
	}
	return p, nil
}

// DecodeSecret decodes a base32 secret, ignoring spaces, dashes, case and padding.
func DecodeSecret(s string) ([]byte, error) {
	s = strings.ToUpper(strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '\t' || r == '=' {
			return -1
		}
		return r
	}, s))
	if s == "" {
		return nil, ErrInvalid
	}
	b, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
	if err != nil || len(b) == 0 {
		return nil, ErrInvalid
	}
	return b, nil
}

func hasher(alg string) func() hash.Hash {
	switch alg {
	case SHA256:
		return sha256.New
	case SHA512:
		return sha512.New
	}
	return sha1.New
}

// truncated is RFC 4226's dynamic truncation of HMAC(secret, counter): a 31-bit number.
func truncated(secret []byte, counter uint64, alg string) uint32 {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	m := hmac.New(hasher(alg), secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	return binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
}

// HOTP is RFC 4226: the code for counter, zero-padded to digits.
func HOTP(secret []byte, counter uint64, digits int, alg string) string {
	v := truncated(secret, counter, alg)
	mod := uint32(1)
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	s := strconv.FormatUint(uint64(v%mod), 10)
	for len(s) < digits {
		s = "0" + s
	}
	return s
}

// Steam is the Steam Guard code for counter (5 characters of steamChars, HMAC-SHA1).
func Steam(secret []byte, counter uint64) string {
	v := truncated(secret, counter, SHA1)
	var b [5]byte
	for i := range b {
		b[i] = steamChars[v%uint32(len(steamChars))]
		v /= uint32(len(steamChars))
	}
	return string(b[:])
}

// Code returns the code at t and how many seconds it stays valid.
func (p Params) Code(t time.Time) (code string, remaining int) {
	period := int64(p.Period)
	if period <= 0 {
		period = 30
	}
	sec := t.Unix()
	counter := uint64(sec / period)
	remaining = int(period - sec%period)
	if p.Steam {
		return Steam(p.Secret, counter), remaining
	}
	return HOTP(p.Secret, counter, p.Digits, p.Algorithm), remaining
}
