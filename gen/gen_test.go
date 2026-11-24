package gen

import (
	"math"
	"strings"
	"testing"
)

func TestChars(t *testing.T) {
	o := Defaults()
	for i := 0; i < 200; i++ {
		p, bits, err := Generate(o)
		if err != nil {
			t.Fatal(err)
		}
		if len(p) != 20 || !strings.ContainsAny(p, upperChars) || !strings.ContainsAny(p, lowerChars) ||
			!strings.ContainsAny(p, digitChars) || !strings.ContainsAny(p, symbolChars) {
			t.Fatalf("missing a class in a %d-char password", len(p))
		}
		// 87 chars, 20 long: just under 20*log2(87) = 128.9.
		if bits > 20*math.Log2(87) || bits < 128 {
			t.Fatalf("bits %v", bits)
		}
	}
	o = Options{Mode: "chars", Length: 12, Digits: true, AvoidAmbiguous: true}
	p, bits, _ := Generate(o)
	if strings.ContainsAny(p, "0125") || math.Abs(bits-12*math.Log2(6)) > 1e-9 {
		t.Fatalf("digits only, avoid ambiguous: %v bits", bits)
	}
	for _, bad := range []Options{{Mode: "chars", Length: 3, Lower: true}, {Mode: "chars", Length: 129, Lower: true}, {Mode: "chars", Length: 10}, {Mode: "x"}} {
		if _, _, err := Generate(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

// The inclusion–exclusion count matches brute force on a tiny alphabet.
func TestCharsEntropyExact(t *testing.T) {
	cs := []string{"ab", "1"}
	// length 3 over {a,b,1} with at least one letter and one digit: 27 - 1 (111) - 8 (letters only) = 18
	if got := charsEntropy(cs, 3); math.Abs(got-math.Log2(18)) > 1e-9 {
		t.Fatal(got)
	}
	if got := charsEntropy([]string{upperChars, lowerChars, digitChars, symbolChars}, 128); math.IsInf(got, 0) || got < 800 {
		t.Fatal(got)
	}
}

func TestUniform(t *testing.T) {
	// Every digit of a 7-value set appears with roughly equal frequency (no modulo bias would show
	// as a skew towards low values for n not dividing 2^k).
	counts := map[int]int{}
	for i := 0; i < 70000; i++ {
		counts[Intn(7)]++
	}
	for k := 0; k < 7; k++ {
		if counts[k] < 9300 || counts[k] > 10700 {
			t.Fatalf("value %d drawn %d times", k, counts[k])
		}
	}
}

func TestWords(t *testing.T) {
	if len(Words) != 7776 {
		t.Fatalf("%d words", len(Words))
	}
	if WordlistSource != "eff_large" && WordlistSource != "fallback" {
		t.Fatal(WordlistSource)
	}
	p, bits, err := Generate(Options{Mode: "words", Words: 5, Separator: "-", Capitalize: true, Number: true})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(p, "-")
	if len(parts) != 5 || strings.ToUpper(parts[0][:1]) != parts[0][:1] || !strings.ContainsAny(p, digitChars) {
		t.Fatalf("shape of a passphrase wrong (%d parts)", len(parts))
	}
	want := 5*math.Log2(7776) + math.Log2(10) + math.Log2(5)
	if math.Abs(bits-want) > 1e-9 {
		t.Fatal(bits, want)
	}
	if _, _, err := Generate(Options{Mode: "words", Words: 2}); err == nil {
		t.Fatal("2 words accepted")
	}
}

func TestEstimate(t *testing.T) {
	if Estimate("password") != 0 || Estimate("") != 0 {
		t.Fatal("common")
	}
	if Estimate("aaaaaaaaaaaa") >= Estimate("kq8#Lm2!vZ") {
		t.Fatal("repeats should score lower")
	}
	if Estimate("Tr0ub4dor&3xyzQ!") < 60 {
		t.Fatal(Estimate("Tr0ub4dor&3xyzQ!"))
	}
}
