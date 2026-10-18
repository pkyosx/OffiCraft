package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"ocserverd/txguard"
)

// 🔴 The key file sits next to the database but is deliberately NOT named
// officraft.db.*: every database copy (VACUUM INTO backups, the pre-migration
// snapshot) must stay unable to open a secret.
const (
	vaultKeyFileName = "vault.key"
	vaultKeyBytes    = 32
	vaultKeyVersion  = 1
	vaultNonceBytes  = 12
)

var (
	errVaultUnconfigured = errors.New("secret vault is not configured on this server")
	// A missing key with rows already sealed must never be "fixed" by minting a
	// fresh key: the new key cannot open the old rows, and every later reveal
	// would fail while create kept succeeding.
	errVaultKeyMissing = errors.New("secret vault key file is missing; existing secrets cannot be opened")
)

type secretVault struct {
	path string
	mu   txguard.Mutex
	aead cipher.AEAD
}

func newSecretVault(path string) *secretVault {
	return &secretVault{path: path}
}

func vaultKeyPathFor(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), vaultKeyFileName)
}

// cipherFor loads (or, when mayCreate answers true, first mints) the key.
// mayCreate == nil means "never create" — the read side.
func (v *secretVault) cipherFor(mayCreate func() (bool, error)) (cipher.AEAD, error) {
	if v == nil || v.path == "" {
		return nil, errVaultUnconfigured
	}
	unlock := v.mu.Acquire()
	defer unlock()
	if v.aead != nil {
		return v.aead, nil
	}
	key, err := readVaultKey(v.path)
	if errors.Is(err, fs.ErrNotExist) {
		if mayCreate == nil {
			return nil, errVaultKeyMissing
		}
		ok, cerr := mayCreate()
		if cerr != nil {
			return nil, cerr
		}
		if !ok {
			return nil, errVaultKeyMissing
		}
		key, err = createVaultKey(v.path)
	}
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCMWithNonceSize(block, vaultNonceBytes)
	if err != nil {
		return nil, err
	}
	v.aead = aead
	return aead, nil
}

func readVaultKey(path string) ([]byte, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("secret vault key %s is not a regular file", path)
	}
	if st.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(path, 0o600); err != nil {
			return nil, fmt.Errorf("secret vault key %s is readable by others (%o) and could not be narrowed: %w",
				path, st.Mode().Perm(), err)
		}
		log.Printf("[vault] WARNING: %s had mode %o; narrowed to 600", path, st.Mode().Perm())
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(key) != vaultKeyBytes {
		return nil, fmt.Errorf("secret vault key %s is %d bytes, want %d", path, len(key), vaultKeyBytes)
	}
	return key, nil
}

// createVaultKey publishes the key with link(2), which refuses to replace an
// existing file: two racing creators can never leave two different keys, and
// the loser reads the winner's.
func createVaultKey(path string) ([]byte, error) {
	key := make([]byte, vaultKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".vault.key.tmp-*")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return nil, err
	}
	if _, err := tmp.Write(key); err != nil {
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
	if err := os.Link(tmpPath, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return readVaultKey(path)
		}
		return nil, err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return key, nil
}

func sealSecretValue(aead cipher.AEAD, secretID, value string) (nonce, ct []byte, err error) {
	nonce = make([]byte, vaultNonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return nonce, aead.Seal(nil, nonce, []byte(value), []byte(secretID)), nil
}

// The error never carries the plaintext or the ciphertext.
func openSecretValue(aead cipher.AEAD, secretID string, keyVersion int, nonce, ct []byte) (string, error) {
	if keyVersion != vaultKeyVersion {
		return "", fmt.Errorf("secret %s is sealed under key version %d, which this server does not hold", secretID, keyVersion)
	}
	if len(nonce) != vaultNonceBytes {
		return "", fmt.Errorf("secret %s: stored nonce is %d bytes", secretID, len(nonce))
	}
	plain, err := aead.Open(nil, nonce, ct, []byte(secretID))
	if err != nil {
		return "", fmt.Errorf("secret %s could not be decrypted", secretID)
	}
	return string(plain), nil
}
