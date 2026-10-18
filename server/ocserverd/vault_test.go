package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestReadVaultKey(t *testing.T) {
	t.Run("a key readable by others is narrowed to 0600", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), vaultKeyFileName)
		if err := os.WriteFile(path, bytes.Repeat([]byte{7}, vaultKeyBytes), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := readVaultKey(path); err != nil {
			t.Fatalf("readVaultKey: %v", err)
		}
		st, _ := os.Stat(path)
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("mode %o, want 600", st.Mode().Perm())
		}
	})

	t.Run("a key of the wrong size is refused", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), vaultKeyFileName)
		if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readVaultKey(path); err == nil {
			t.Fatal("a 5-byte key was accepted")
		}
	})

	t.Run("a symlinked key is refused", func(t *testing.T) {
		dir := t.TempDir()
		real := filepath.Join(dir, "elsewhere")
		if err := os.WriteFile(real, bytes.Repeat([]byte{7}, vaultKeyBytes), 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, vaultKeyFileName)
		if err := os.Symlink(real, path); err != nil {
			t.Fatal(err)
		}
		if _, err := readVaultKey(path); err == nil {
			t.Fatal("a symlinked key was accepted")
		}
	})
}

func TestCreateVaultKey(t *testing.T) {
	t.Run("racing creators all end up with the one published key", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), vaultKeyFileName)
		keys := make([][]byte, 8)
		var wg sync.WaitGroup
		for i := range keys {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				k, err := createVaultKey(path)
				if err != nil {
					t.Errorf("createVaultKey: %v", err)
				}
				keys[i] = k
			}(i)
		}
		wg.Wait()
		onDisk, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for i, k := range keys {
			if !bytes.Equal(k, onDisk) {
				t.Fatalf("creator %d holds a key that is not the published one", i)
			}
		}
		entries, _ := os.ReadDir(filepath.Dir(path))
		if len(entries) != 1 {
			t.Fatalf("temp files left behind: %v", entries)
		}
	})
}

func TestSecretVaultCipherFor(t *testing.T) {
	t.Run("the read side never mints a key", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), vaultKeyFileName)
		_, err := newSecretVault(path).cipherFor(nil)
		if !errors.Is(err, errVaultKeyMissing) {
			t.Fatalf("err = %v, want errVaultKeyMissing", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("a key was minted on read: %v", err)
		}
	})

	t.Run("an unwired vault refuses instead of panicking", func(t *testing.T) {
		var v *secretVault
		if _, err := v.cipherFor(nil); !errors.Is(err, errVaultUnconfigured) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestOpenSecretValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), vaultKeyFileName)
	aead, err := newSecretVault(path).cipherFor(func() (bool, error) { return true, nil })
	if err != nil {
		t.Fatal(err)
	}
	nonce, ct, err := sealSecretValue(aead, "sec-aaaaaaaaaaaa", "fake-value-1")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("the sealing id opens it", func(t *testing.T) {
		got, err := openSecretValue(aead, "sec-aaaaaaaaaaaa", vaultKeyVersion, nonce, ct)
		if err != nil || got != "fake-value-1" {
			t.Fatalf("open = %q, %v", got, err)
		}
	})

	t.Run("an unknown key version is refused without trying to decrypt", func(t *testing.T) {
		if _, err := openSecretValue(aead, "sec-aaaaaaaaaaaa", vaultKeyVersion+1, nonce, ct); err == nil {
			t.Fatal("a row sealed under an unknown key version was opened")
		}
	})

	t.Run("another id cannot open it and the error carries no value", func(t *testing.T) {
		_, err := openSecretValue(aead, "sec-bbbbbbbbbbbb", vaultKeyVersion, nonce, ct)
		if err == nil {
			t.Fatal("ciphertext opened under a different id")
		}
		if bytes.Contains([]byte(err.Error()), []byte("fake-value-1")) {
			t.Fatalf("error leaks the value: %v", err)
		}
	})
}
