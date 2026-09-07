package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStatBirthTime(t *testing.T) {
	dir := t.TempDir()

	before := time.Now()
	older := filepath.Join(dir, "older")
	if err := os.WriteFile(older, []byte("first"), 0o644); err != nil {
		t.Fatalf("stage %s: %v", older, err)
	}
	newer := filepath.Join(dir, "newer")
	if err := os.WriteFile(newer, []byte("second"), 0o644); err != nil {
		t.Fatalf("stage %s: %v", newer, err)
	}
	after := time.Now()

	olderBirth, err := statBirthTime(older)
	if err != nil {
		t.Fatalf("statBirthTime(%s) = %v, want a birth time", older, err)
	}
	if olderBirth.Before(before.Add(-time.Second)) || olderBirth.After(after.Add(time.Second)) {
		t.Errorf("birth time = %s, want it inside the window the file was created in (%s..%s)",
			olderBirth, before, after)
	}

	newerBirth, err := statBirthTime(newer)
	if err != nil {
		t.Fatalf("statBirthTime(%s) = %v, want a birth time", newer, err)
	}
	if newerBirth.Before(olderBirth) {
		t.Errorf("the file created second was born at %s, before the first one's %s", newerBirth, olderBirth)
	}

	if err := os.WriteFile(older, []byte("rewritten and much longer"), 0o644); err != nil {
		t.Fatalf("rewrite %s: %v", older, err)
	}
	rewritten, err := statBirthTime(older)
	if err != nil {
		t.Fatalf("statBirthTime after rewrite = %v", err)
	}
	if !rewritten.Equal(olderBirth) {
		t.Errorf("birth time after rewriting the file = %s, want the original %s", rewritten, olderBirth)
	}

	missing := filepath.Join(dir, "never-created")
	got, err := statBirthTime(missing)
	if !os.IsNotExist(err) {
		t.Errorf("statBirthTime(%s) err = %v, want a not-exist error", missing, err)
	}
	if !got.IsZero() {
		t.Errorf("statBirthTime(%s) = %s, want the zero time on failure", missing, got)
	}
}
