// Package gen generates passwords and passphrases with crypto/rand and reports their entropy.
//
// Invariants: every random choice is uniform (crypto/rand.Int, which rejects rather than
// reducing modulo, so there is no modulo bias); the reported entropy is the exact log2 of the
// number of outputs the options can produce, counting the "at least one of each selected
// class" rule; generated values are never logged.
//
// The word list is embedded from wordlist.txt. That file is the EFF large word list when it
// could be downloaded (`make wordlist`), else an honest generated fallback of the same size;
// WordlistSource says which.
package gen

import (
	"bufio"
	"crypto/rand"
	_ "embed"
	"errors"
	"math"
	"math/big"
	"strings"
	"unicode"
)

//go:embed wordlist.txt
var wordlistTxt string

// Words is the embedded word list.
var Words []string

// WordlistSource is "eff_large" for the EFF large word list, "fallback" for the generated one.
var WordlistSource string

func init() {
	sc := bufio.NewScanner(strings.NewReader(wordlistTxt))
	fallback := false
	seen := map[string]bool{}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "#") {
			fallback = fallback || strings.Contains(line, "FALLBACK")
			continue
		}
		if i := strings.IndexByte(line, '\t'); i >= 0 { // EFF format: "11111\tabacus"
			line = strings.TrimSpace(line[i+1:])
		}
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		Words = append(Words, line)
	}
	if len(Words) < 1024 {
		panic("gen: embedded word list is too short")
	}
	WordlistSource = "eff_large"
	if fallback || len(Words) != 7776 {
		WordlistSource = "fallback"
	}
}

// Character classes.
const (
	upperChars  = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	lowerChars  = "abcdefghijklmnopqrstuvwxyz"
	digitChars  = "0123456789"
	symbolChars = "!#$%&*+-=?@^_~.,:;/()[]{}"
	ambiguous   = "Il1O0o|`'\"5S2Z"
)

// Options are the generator settings (the raypass.generate action's input).
type Options struct {
	Mode           string // "chars" (default) or "words"
	Length         int    // chars: 4..128 (default 20)
	Upper          bool
	Lower          bool
	Digits         bool
	Symbols        bool
	AvoidAmbiguous bool
	Words          int    // words: 3..20 (default 5)
	Separator      string // default "-"
	Capitalize     bool
	Number         bool // append a random digit to one random word
}

// Defaults returns the default options: 20 characters of every class.
func Defaults() Options {
	return Options{Mode: "chars", Length: 20, Upper: true, Lower: true, Digits: true, Symbols: true, Words: 5, Separator: "-"}
}

// ErrOptions is returned for options that cannot produce a password.
var ErrOptions = errors.New("bad generator options")

// Generate makes a password and returns it with its entropy in bits.
func Generate(o Options) (string, float64, error) {
	switch o.Mode {
	case "", "chars":
		return chars(o)
	case "words":
		return words(o)
	}
	return "", 0, ErrOptions
}

func classes(o Options) []string {
	var cs []string
	for _, c := range []struct {
		on  bool
		set string
	}{{o.Upper, upperChars}, {o.Lower, lowerChars}, {o.Digits, digitChars}, {o.Symbols, symbolChars}} {
		if !c.on {
			continue
		}
		s := c.set
		if o.AvoidAmbiguous {
			s = strings.Map(func(r rune) rune {
				if strings.ContainsRune(ambiguous, r) {
					return -1
				}
				return r
			}, s)
		}
		cs = append(cs, s)
	}
	return cs
}

// Intn returns a uniform random int in [0, n).
func Intn(n int) int {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		panic("gen: crypto/rand failed: " + err.Error())
	}
	return int(v.Int64())
}

func chars(o Options) (string, float64, error) {
	if o.Length < 4 || o.Length > 128 {
		return "", 0, ErrOptions
	}
	cs := classes(o)
	if len(cs) == 0 {
		return "", 0, ErrOptions
	}
	all := strings.Join(cs, "")
	// Rejection sampling: draw uniformly from every allowed character and retry until each
	// selected class appears. Every acceptable password is equally likely, so the entropy is
	// exactly log2(count) below. Length >= 4 >= classes, and acceptance is high (> 50% for
	// length 4 with all classes), so this ends quickly.
	buf := make([]byte, o.Length)
	for {
		for i := range buf {
			buf[i] = all[Intn(len(all))]
		}
		ok := true
		for _, c := range cs {
			ok = ok && strings.ContainsAny(string(buf), c)
		}
		if ok {
			return string(buf), charsEntropy(cs, o.Length), nil
		}
	}
}

// charsEntropy is log2 of the number of length-L strings over the union of cs that contain at
// least one character of every class (inclusion–exclusion over the classes left out).
func charsEntropy(cs []string, l int) float64 {
	total := new(big.Int)
	n := len(cs)
	for mask := 0; mask < 1<<n; mask++ {
		size, bits := 0, 0
		for i, c := range cs {
			if mask&(1<<i) == 0 {
				size += len(c)
			} else {
				bits++
			}
		}
		term := new(big.Int).Exp(big.NewInt(int64(size)), big.NewInt(int64(l)), nil)
		if bits%2 == 1 {
			total.Sub(total, term)
		} else {
			total.Add(total, term)
		}
	}
	return log2Big(total)
}

func log2Big(x *big.Int) float64 {
	if x.Sign() <= 0 {
		return 0
	}
	f, _ := new(big.Float).SetInt(x).Float64()
	if !math.IsInf(f, 0) {
		return math.Log2(f)
	}
	shift := x.BitLen() - 64
	y := new(big.Int).Rsh(x, uint(shift))
	fy, _ := new(big.Float).SetInt(y).Float64()
	return math.Log2(fy) + float64(shift)
}

func words(o Options) (string, float64, error) {
	if o.Words < 3 || o.Words > 20 || len(o.Separator) > 8 {
		return "", 0, ErrOptions
	}
	ws := make([]string, o.Words)
	for i := range ws {
		w := Words[Intn(len(Words))]
		if o.Capitalize {
			r := []rune(w)
			r[0] = unicode.ToUpper(r[0])
			w = string(r)
		}
		ws[i] = w
	}
	bits := float64(o.Words) * math.Log2(float64(len(Words)))
	if o.Number {
		i := Intn(len(ws))
		ws[i] += string(rune('0' + Intn(10)))
		bits += math.Log2(10) + math.Log2(float64(len(ws)))
	}
	return strings.Join(ws, o.Separator), bits, nil
}

// Estimate is a rough strength estimate of an existing password in bits, for the health
// report: length times log2 of the pool of the character classes it uses, divided down for
// repeats and sequences, and 0 for a few very common passwords. It is a heuristic, not a
// guarantee; generated passwords report their exact entropy instead.
func Estimate(p string) float64 {
	if p == "" {
		return 0
	}
	if common[strings.ToLower(p)] {
		return 0
	}
	pool := 0
	var hasU, hasL, hasD, hasS, hasO bool
	for _, r := range p {
		switch {
		case r >= 'A' && r <= 'Z':
			hasU = true
		case r >= 'a' && r <= 'z':
			hasL = true
		case r >= '0' && r <= '9':
			hasD = true
		case r < 128:
			hasS = true
		default:
			hasO = true
		}
	}
	for _, c := range []struct {
		on bool
		n  int
	}{{hasU, 26}, {hasL, 26}, {hasD, 10}, {hasS, 33}, {hasO, 100}} {
		if c.on {
			pool += c.n
		}
	}
	// Effective length: runs of a repeated character and +1/-1 sequences count once.
	rs := []rune(p)
	eff := 1.0
	for i := 1; i < len(rs); i++ {
		d := rs[i] - rs[i-1]
		if d == 0 || d == 1 || d == -1 {
			eff += 0.25
			continue
		}
		eff++
	}
	return eff * math.Log2(float64(pool))
}

var common = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`password password1 password123 123456 12345678 123456789 1234567890 qwerty qwerty123
		qwertyuiop abc123 111111 123123 letmein welcome admin iloveyou monkey dragon football baseball sunshine
		princess master shadow trustno1 passw0rd p@ssw0rd 000000 654321 superman michael login starwars hello
		freedom whatever azerty changeme secret`) {
		common[w] = true
	}
}
