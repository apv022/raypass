package vault

import (
	"encoding/json"
	"errors"

	"github.com/apv022/sol/sdk/go/crdt"
)

// Entry is the body of an entry of feed "vault": one encrypted batch of map ops.
type Entry struct {
	V int    `json:"v"`
	K string `json:"k"` // keyring id
	N string `json:"n"` // b64 nonce
	C string `json:"c"` // b64 ciphertext+tag
}

// Batch is the plaintext of an Entry.
type Batch struct {
	Ops []crdt.MapOp `json:"ops"`
}

// MaxBody is the largest feed entry body Sol accepts (sol.MaxBodySize, 64 KiB).
const MaxBody = 64 << 10

// maxPlain is the largest batch plaintext whose encrypted entry still fits MaxBody: the body is
// {"v":1,"k":"<16>","n":"<16>","c":"<b64>"} (about 60 bytes of frame) and base64 grows by 4/3.
// The margin keeps us clear of the limit whatever the daemon counts.
const maxPlain = (MaxBody-512)/4*3 - 16

// MaxOpSize is the largest single op (its JSON) a batch can hold; values bigger than this
// cannot be stored. Item validation keeps every field well below it.
const MaxOpSize = maxPlain - 16

// MarshalBatch is the exact plaintext of a batch: json.Marshal of {"ops": [...]}.
func MarshalBatch(ops []crdt.MapOp) ([]byte, error) {
	if ops == nil {
		ops = []crdt.MapOp{}
	}
	return json.Marshal(Batch{Ops: ops})
}

// EncryptBatch seals a batch plaintext under vk with a random nonce.
func EncryptBatch(vk []byte, keyringID string, plaintext []byte) (Entry, error) {
	return EncryptBatchNonce(vk, keyringID, plaintext, RandomBytes(NonceSize))
}

// EncryptBatchNonce is EncryptBatch with a fixed nonce (test vectors only).
func EncryptBatchNonce(vk []byte, keyringID string, plaintext, nonce []byte) (Entry, error) {
	c, err := Seal(vk, nonce, plaintext, AADVault)
	if err != nil {
		return Entry{}, err
	}
	return Entry{V: 1, K: keyringID, N: B64.EncodeToString(nonce), C: B64.EncodeToString(c)}, nil
}

// ParseEntry decodes a "vault" feed entry body; ok is false for anything that is not a
// version-1 entry.
func ParseEntry(body []byte) (Entry, bool) {
	var e Entry
	if json.Unmarshal(body, &e) != nil || e.V != 1 || e.N == "" || e.C == "" {
		return Entry{}, false
	}
	return e, true
}

// Decrypt opens the entry with vk and returns its ops.
func (e Entry) Decrypt(vk []byte) ([]crdt.MapOp, error) {
	nonce, err1 := B64.DecodeString(e.N)
	c, err2 := B64.DecodeString(e.C)
	if err1 != nil || err2 != nil {
		return nil, ErrCorrupt
	}
	p, err := Open(vk, nonce, c, AADVault)
	if err != nil {
		return nil, err
	}
	defer Wipe(p)
	var b Batch
	if err := json.Unmarshal(p, &b); err != nil {
		return nil, ErrCorrupt
	}
	return b.Ops, nil
}

// EncryptOps splits ops into batches that each fit one feed entry and encrypts them. Ops are
// independent, so a split batch means nothing more than several entries.
func EncryptOps(vk []byte, keyringID string, ops []crdt.MapOp) ([]Entry, error) {
	batches, err := SplitOps(ops)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, b := range batches {
		p, err := MarshalBatch(b)
		if err != nil {
			return nil, err
		}
		e, err := EncryptBatch(vk, keyringID, p)
		Wipe(p)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// SplitOps groups ops, in order, into batches whose plaintext is at most maxPlain bytes.
func SplitOps(ops []crdt.MapOp) ([][]crdt.MapOp, error) {
	var out [][]crdt.MapOp
	var cur []crdt.MapOp
	size := len(`{"ops":[]}`)
	for _, op := range ops {
		b, err := json.Marshal(op)
		if err != nil {
			return nil, err
		}
		n := len(b)
		Wipe(b)
		if n > MaxOpSize {
			return nil, errors.New("a value is too large to store")
		}
		if len(cur) > 0 && size+1+n > maxPlain {
			out = append(out, cur)
			cur, size = nil, len(`{"ops":[]}`)
		}
		if len(cur) > 0 {
			size++ // comma
		}
		cur = append(cur, op)
		size += n
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out, nil
}
