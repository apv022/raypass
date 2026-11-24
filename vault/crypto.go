// Package vault is Raypass's crypto and data model (design/DESIGN.md "Crypto" and "Item
// model"): the keyring that wraps the vault key under the master password, the encrypted feed
// entries that carry batches of CRDT map ops, the folded last-writer-wins map, and the typed
// item view with the operations that change it.
//
// Invariants:
//   - the byte format is the interop contract with the Android app and testdata/vectors.json:
//     PBKDF2-HMAC-SHA256, AES-256-GCM with 12-byte random nonces and fixed AAD strings, standard
//     padded base64, the SDK's crdt.MapOp JSON;
//   - nothing returned for a feed body holds plaintext other than version, keyring id, KDF
//     parameters, nonces and stamps;
//   - key material is held in []byte so it can be wiped (Wipe) when the vault locks;
//   - errors never contain passwords, keys or plaintext.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

// The AAD and HMAC labels. Changing any of them breaks every existing vault.
const (
	AADKeyring = "raypass/keyring/v1"
	AADVault   = "raypass/vault/v1"
	AADExport  = "raypass/export/v1"
	CheckLabel = "raypass/check/v1"
)

// KDF parameters.
const (
	KDFAlg = "pbkdf2-sha256"
	// DefaultIter is the PBKDF2 round count for new keyrings and exports.
	DefaultIter = 600_000
	// MinIter is the smallest round count a keyring or export may declare; anything lower is
	// refused (it would make brute force cheap). Test vectors use less and call DeriveKey directly.
	MinIter = 100_000
	// MaxIter bounds the work a crafted keyring can make us do.
	MaxIter = 10_000_000

	KeySize   = 32
	SaltSize  = 16
	NonceSize = 12
)

// Errors. ErrWrongPassword is the only one a person normally sees.
var (
	ErrWrongPassword = errors.New("wrong password")
	ErrCorrupt       = errors.New("corrupt data")
)

// B64 is the base64 of the format: standard alphabet with padding.
var B64 = base64.StdEncoding

// DeriveKey is PBKDF2-HMAC-SHA256(password, salt, iter) → 32 bytes. The password is used as
// given (its UTF-8 bytes; no trimming or normalization).
func DeriveKey(password string, salt []byte, iter int) ([]byte, error) {
	if iter < 1 || iter > MaxIter {
		return nil, fmt.Errorf("bad kdf iteration count %d", iter)
	}
	return pbkdf2.Key(sha256.New, password, salt, iter, KeySize)
}

// Seal is AES-256-GCM encryption: ciphertext with the 16-byte tag appended.
func Seal(key, nonce, plaintext []byte, aad string) ([]byte, error) {
	g, err := gcm(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != NonceSize {
		return nil, errors.New("bad nonce size")
	}
	return g.Seal(nil, nonce, plaintext, []byte(aad)), nil
}

// Open is the inverse of Seal. A wrong key or tampered data gives ErrCorrupt.
func Open(key, nonce, ciphertext []byte, aad string) ([]byte, error) {
	g, err := gcm(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != NonceSize {
		return nil, ErrCorrupt
	}
	p, err := g.Open(nil, nonce, ciphertext, []byte(aad))
	if err != nil {
		return nil, ErrCorrupt
	}
	return p, nil
}

func gcm(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, errors.New("bad key size")
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

// Check is HMAC-SHA256(vk, CheckLabel): stored in the keyring so a device can tell that the key
// it unwrapped is the vault key.
func Check(vk []byte) []byte {
	m := hmac.New(sha256.New, vk)
	m.Write([]byte(CheckLabel))
	return m.Sum(nil)
}

// Wipe zeroes b. Go may have copied key bytes elsewhere (the GC moves nothing, but derived
// values such as AES round keys live in cipher objects); wiping what we hold is still the
// difference between "a heap dump after lock shows the key" and "it does not, mostly".
func Wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
