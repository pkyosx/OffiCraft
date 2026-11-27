package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadClaudeLoginAccount(t *testing.T) {
	cases := []struct {
		name      string
		nested    string
		top       string
		wantID    string
		wantLabel string
	}{
		{
			name:   "the account uuid joins the org uuid and the email carries the parenthesised org",
			nested: `{"oauthAccount":{"accountUuid":" au-1 ","organizationUuid":"org-1","emailAddress":"kyle@x.io","organizationName":"OffiCraft"}}`,
			wantID: "au-1/org-1", wantLabel: "kyle@x.io(OffiCraft)",
		},
		{
			name:   "no org yields a bare account uuid and an unsuffixed label",
			nested: `{"oauthAccount":{"accountUuid":"au-1","emailAddress":"kyle@x.io"}}`,
			wantID: "au-1", wantLabel: "kyle@x.io",
		},
		{
			name:   "legacy config without an accountUuid falls back to userID, and no email to the display name",
			nested: `{"userID":"legacy-1","oauthAccount":{"displayName":"Kyle","organizationName":"OffiCraft"}}`,
			wantID: "legacy-1", wantLabel: "Kyle(OffiCraft)",
		},
		{
			name:   "each field resolves independently across the two files",
			nested: `{"oauthAccount":{"accountUuid":"au-1","emailAddress":"kyle@x.io"}}`,
			top:    `{"oauthAccount":{"organizationUuid":"org-2","organizationName":"OffiCraft"}}`,
			wantID: "au-1/org-2", wantLabel: "kyle@x.io(OffiCraft)",
		},
		{
			name:   "an unparseable file is skipped, not fatal",
			nested: `{"oauthAccount":`,
			top:    `{"oauthAccount":{"accountUuid":"au-9","organizationUuid":"org-9"}}`,
			wantID: "au-9/org-9",
		},
		{
			name:   "a non-string field never reaches the key or the label",
			nested: `{"oauthAccount":{"accountUuid":7,"emailAddress":null,"displayName":"Kyle"},"userID":"legacy-1"}`,
			wantID: "legacy-1", wantLabel: "Kyle",
		},
		{
			name: "no identity anywhere binds nothing",
			top:  `{"oauthAccount":{"emailAddress":"kyle@x.io"}}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			if c.nested != "" {
				if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(home, ".claude", ".claude.json"), []byte(c.nested), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if c.top != "" {
				if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(c.top), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got := readClaudeLoginAccount(home, os.ReadFile)
			if want := (claudeLoginAccount{ID: c.wantID, Label: c.wantLabel}); got != want {
				t.Errorf("readClaudeLoginAccount = %+v, want %+v", got, want)
			}
		})
	}

	t.Run("an unstated HOME binds nothing rather than reading relative paths", func(t *testing.T) {
		read := func(path string) ([]byte, error) {
			t.Errorf("read %s with no HOME", path)
			return nil, os.ErrNotExist
		}
		if got := readClaudeLoginAccount("", read); got != (claudeLoginAccount{}) {
			t.Errorf("readClaudeLoginAccount = %+v, want nothing", got)
		}
	})
}
