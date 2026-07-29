package catalog

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/jeni0101/vpn/server/internal/model"
)

type Signer struct {
	private ed25519.PrivateKey
}

func LoadOrCreateSigner(path string) (*Signer, error) {
	if data, err := os.ReadFile(path); err == nil {
		key, err := base64.RawStdEncoding.DecodeString(string(data))
		if err != nil || len(key) != ed25519.PrivateKeySize {
			return nil, errors.New("invalid catalog signing key")
		}
		return &Signer{private: ed25519.PrivateKey(key)}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	encoded := []byte(base64.RawStdEncoding.EncodeToString(private))
	tmp, err := os.CreateTemp(filepath.Dir(path), ".catalog-signing-*")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return nil, err
	}
	if _, err := tmp.Write(encoded); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return nil, err
	}
	return &Signer{private: private}, nil
}

func NewSigner(private ed25519.PrivateKey) (*Signer, error) {
	if len(private) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid catalog signing key")
	}
	return &Signer{private: append(ed25519.PrivateKey(nil), private...)}, nil
}

func (s *Signer) PublicKey() string {
	public := s.private.Public().(ed25519.PublicKey)
	return base64.RawStdEncoding.EncodeToString(public)
}

func (s *Signer) Sign(value *model.Catalog) error {
	value.Signature = ""
	value.SignedPayload = ""
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	signature := ed25519.Sign(s.private, payload)
	value.Signature = base64.RawStdEncoding.EncodeToString(signature)
	value.SignedPayload = base64.RawStdEncoding.EncodeToString(payload)
	return nil
}

func Verify(value model.Catalog, encodedPublicKey string) bool {
	public, err := base64.RawStdEncoding.DecodeString(encodedPublicKey)
	if err != nil || len(public) != ed25519.PublicKeySize {
		return false
	}
	signature, err := base64.RawStdEncoding.DecodeString(value.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return false
	}
	payload, err := base64.RawStdEncoding.DecodeString(value.SignedPayload)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(public), payload, signature) {
		return false
	}
	var signed model.Catalog
	if json.Unmarshal(payload, &signed) != nil ||
		signed.Signature != "" || signed.SignedPayload != "" {
		return false
	}
	value.Signature = ""
	value.SignedPayload = ""
	current, err := json.Marshal(value)
	if err != nil {
		return false
	}
	return string(current) == string(payload)
}
