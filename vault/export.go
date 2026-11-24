package vault

import (
	"encoding/json"
	"errors"
)

// ExportFile is the encrypted export format (design/DESIGN.md "Encrypted export file").
type ExportFile struct {
	RaypassExport int    `json:"raypass_export"`
	KDF           KDF    `json:"kdf"`
	Nonce         string `json:"nonce"`
	Data          string `json:"data"`
}

// SealExport encrypts plaintext (a Raypass JSON export) under password with fresh randomness.
func SealExport(password string, plaintext []byte, iter int) (ExportFile, error) {
	if iter < MinIter {
		return ExportFile{}, errors.New("kdf iteration count below the minimum")
	}
	return SealExportWith(password, plaintext, iter, RandomBytes(SaltSize), RandomBytes(NonceSize))
}

// SealExportWith is SealExport with fixed salt and nonce (test vectors only).
func SealExportWith(password string, plaintext []byte, iter int, salt, nonce []byte) (ExportFile, error) {
	if password == "" {
		return ExportFile{}, BadInput("an encrypted export needs a password")
	}
	k, err := DeriveKey(password, salt, iter)
	if err != nil {
		return ExportFile{}, err
	}
	defer Wipe(k)
	c, err := Seal(k, nonce, plaintext, AADExport)
	if err != nil {
		return ExportFile{}, err
	}
	return ExportFile{RaypassExport: 1, KDF: KDF{Alg: KDFAlg, Iter: iter, Salt: B64.EncodeToString(salt)},
		Nonce: B64.EncodeToString(nonce), Data: B64.EncodeToString(c)}, nil
}

// IsExportFile reports whether data looks like an encrypted Raypass export.
func IsExportFile(data []byte) bool {
	var f ExportFile
	return json.Unmarshal(data, &f) == nil && f.RaypassExport == 1 && f.Data != ""
}

// OpenExport decrypts an encrypted export. minIter is the smallest KDF round count accepted.
func OpenExport(data []byte, password string, minIter int) ([]byte, error) {
	var f ExportFile
	if json.Unmarshal(data, &f) != nil || f.RaypassExport != 1 || f.KDF.Alg != KDFAlg {
		return nil, BadInput("not an encrypted Raypass export")
	}
	if f.KDF.Iter < minIter || f.KDF.Iter > MaxIter {
		return nil, BadInput("the export's KDF settings are out of range")
	}
	salt, err1 := B64.DecodeString(f.KDF.Salt)
	nonce, err2 := B64.DecodeString(f.Nonce)
	c, err3 := B64.DecodeString(f.Data)
	if err1 != nil || err2 != nil || err3 != nil || len(salt) != SaltSize {
		return nil, BadInput("the export file is damaged")
	}
	k, err := DeriveKey(password, salt, f.KDF.Iter)
	if err != nil {
		return nil, err
	}
	defer Wipe(k)
	p, err := Open(k, nonce, c, AADExport)
	if err != nil {
		return nil, ErrWrongPassword
	}
	return p, nil
}
