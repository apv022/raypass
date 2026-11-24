package vault

import (
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"

	"github.com/apv022/sol/sdk/go/crdt"
)

// KDF is the key-derivation parameters stored next to a wrapped key.
type KDF struct {
	Alg  string `json:"alg"`
	Iter int    `json:"iter"`
	Salt string `json:"salt"`
}

// Keyring is the body of an entry of feed "keys": the vault key wrapped under a key derived from
// the master password. Field order is the order of design/DESIGN.md.
type Keyring struct {
	V       int        `json:"v"`
	Type    string     `json:"type"`
	ID      string     `json:"id"`
	KDF     KDF        `json:"kdf"`
	Nonce   string     `json:"nonce"`
	Wrapped string     `json:"wrapped"`
	Check   string     `json:"check"`
	Stamp   crdt.Stamp `json:"stamp"`
}

// KeyringParams fixes every random input of a keyring, for test vectors. Production code uses
// NewKeyring, which draws them from crypto/rand.
type KeyringParams struct {
	ID    string
	Salt  []byte
	Nonce []byte
	Iter  int
	Stamp crdt.Stamp
}

// RandomHex returns n random bytes as lowercase hex (ids are 8 bytes, 16 hex chars).
func RandomHex(n int) string {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic("raypass: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// RandomBytes returns n bytes from crypto/rand.
func RandomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic("raypass: crypto/rand failed: " + err.Error())
	}
	return b
}

// NewVaultKey makes a fresh random vault key.
func NewVaultKey() []byte { return RandomBytes(KeySize) }

// NewKeyring makes the first keyring of a vault: a new random id, salt and nonce, wrapping vk
// under password. iter must be at least MinIter.
func NewKeyring(password string, vk []byte, iter int, stamp crdt.Stamp) (Keyring, error) {
	if iter < MinIter {
		return Keyring{}, errors.New("kdf iteration count below the minimum")
	}
	return WrapKeyring(password, vk, KeyringParams{ID: RandomHex(8), Salt: RandomBytes(SaltSize),
		Nonce: RandomBytes(NonceSize), Iter: iter, Stamp: stamp})
}

// Rewrap makes the keyring that replaces kr after a password change: same id, new salt, nonce
// and wrap of the same vk.
func Rewrap(kr Keyring, password string, vk []byte, iter int, stamp crdt.Stamp) (Keyring, error) {
	if iter < MinIter {
		return Keyring{}, errors.New("kdf iteration count below the minimum")
	}
	return WrapKeyring(password, vk, KeyringParams{ID: kr.ID, Salt: RandomBytes(SaltSize),
		Nonce: RandomBytes(NonceSize), Iter: iter, Stamp: stamp})
}

// WrapKeyring builds a keyring from fixed parameters (no minimum on Iter: vectors use few rounds).
func WrapKeyring(password string, vk []byte, p KeyringParams) (Keyring, error) {
	if len(vk) != KeySize || len(p.Salt) != SaltSize {
		return Keyring{}, errors.New("bad key or salt size")
	}
	mk, err := DeriveKey(password, p.Salt, p.Iter)
	if err != nil {
		return Keyring{}, err
	}
	defer Wipe(mk)
	wrapped, err := Seal(mk, p.Nonce, vk, AADKeyring)
	if err != nil {
		return Keyring{}, err
	}
	return Keyring{V: 1, Type: "keyring", ID: p.ID,
		KDF:   KDF{Alg: KDFAlg, Iter: p.Iter, Salt: B64.EncodeToString(p.Salt)},
		Nonce: B64.EncodeToString(p.Nonce), Wrapped: B64.EncodeToString(wrapped),
		Check: B64.EncodeToString(Check(vk)), Stamp: p.Stamp}, nil
}

// Valid reports whether the keyring is well-formed enough to try (version, type, id, sizes,
// KDF). minIter is the smallest round count accepted (MinIter in production).
func (kr Keyring) Valid(minIter int) bool {
	salt, err1 := B64.DecodeString(kr.KDF.Salt)
	nonce, err2 := B64.DecodeString(kr.Nonce)
	w, err3 := B64.DecodeString(kr.Wrapped)
	c, err4 := B64.DecodeString(kr.Check)
	return kr.V == 1 && kr.Type == "keyring" && ValidID(kr.ID) && kr.KDF.Alg == KDFAlg &&
		kr.KDF.Iter >= minIter && kr.KDF.Iter <= MaxIter &&
		err1 == nil && err2 == nil && err3 == nil && err4 == nil &&
		len(salt) == SaltSize && len(nonce) == NonceSize && len(w) == KeySize+16 && len(c) == 32
}

// Unwrap derives the master key from password and returns the vault key. A wrong password
// gives ErrWrongPassword; a keyring whose check does not match the unwrapped key ErrCorrupt.
func (kr Keyring) Unwrap(password string) ([]byte, error) {
	salt, err := B64.DecodeString(kr.KDF.Salt)
	if err != nil {
		return nil, ErrCorrupt
	}
	nonce, _ := B64.DecodeString(kr.Nonce)
	wrapped, _ := B64.DecodeString(kr.Wrapped)
	check, _ := B64.DecodeString(kr.Check)
	mk, err := DeriveKey(password, salt, kr.KDF.Iter)
	if err != nil {
		return nil, ErrCorrupt
	}
	defer Wipe(mk)
	vk, err := Open(mk, nonce, wrapped, AADKeyring)
	if err != nil {
		return nil, ErrWrongPassword
	}
	if len(vk) != KeySize || !hmac.Equal(Check(vk), check) {
		Wipe(vk)
		return nil, ErrCorrupt
	}
	return vk, nil
}

// ParseKeyring decodes a "keys" feed entry body. ok is false for anything that is not a
// version-1 keyring.
func ParseKeyring(body []byte) (Keyring, bool) {
	var kr Keyring
	if json.Unmarshal(body, &kr) != nil || kr.V != 1 || kr.Type != "keyring" {
		return Keyring{}, false
	}
	return kr, true
}

// Keyrings is the result of choosing among every keyring in the "keys" feed.
type Keyrings struct {
	// Current is the keyring to unlock with: the newest (greatest stamp) of the vault's id.
	Current Keyring
	// OK is false when there is no valid keyring at all (the vault was never set up).
	OK bool
	// Others are the ids of other vaults (set up on another device while offline). Their
	// entries stay in the feed and are reported as orphaned.
	Others []string
}

// ChooseKeyring picks the vault and its current keyring (design/DESIGN.md): the vault is the id
// of the keyring with the smallest stamp; its current keyring the one with the greatest stamp.
// Invalid keyrings (minIter, sizes) are ignored.
func ChooseKeyring(all []Keyring, minIter int) Keyrings {
	var valid []Keyring
	for _, kr := range all {
		if kr.Valid(minIter) {
			valid = append(valid, kr)
		}
	}
	if len(valid) == 0 {
		return Keyrings{}
	}
	sort.SliceStable(valid, func(i, j int) bool { return valid[i].Stamp.Less(valid[j].Stamp) })
	id := valid[0].ID
	out := Keyrings{OK: true}
	seen := map[string]bool{id: true}
	for _, kr := range valid {
		if kr.ID == id {
			out.Current = kr // ascending: the last one wins
		} else if !seen[kr.ID] {
			seen[kr.ID] = true
			out.Others = append(out.Others, kr.ID)
		}
	}
	return out
}

// ValidID reports whether s is 16 lowercase hex characters (item and keyring ids).
func ValidID(s string) bool {
	if len(s) != 16 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
