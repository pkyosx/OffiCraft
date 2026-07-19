package main

// main_test.go — the publisher CLI's behaviour lock, driven end to end
// against an httptest stand-in of ocupdaterd's publish/promote face (the
// REAL ocupdaterd handler is locked by its own suite; here the contract is
// what THIS client sends and how it reports).

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeUpdater records what arrives on /api/publish and /api/promote and
// answers like ocupdaterd would.
type fakeUpdater struct {
	t           *testing.T
	token       string
	publishSHA  string // the sha256 field the last publish carried
	publishBody []byte // the binary bytes the last publish carried
	publishVer  string // the version field the last publish carried ("" = omitted)
	promoteVer  string // the version the last promote carried
}

func (f *fakeUpdater) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/publish", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+f.token {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":{"code":"unauthorized","message":"this publish token is not valid"}}`)
			return
		}
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			f.t.Fatalf("fake updater: parse multipart: %v", err)
		}
		f.publishSHA = r.FormValue("sha256")
		f.publishVer = r.FormValue("version")
		file, _, err := r.FormFile("binary")
		if err != nil {
			f.t.Fatalf("fake updater: no binary part: %v", err)
		}
		defer file.Close()
		f.publishBody, _ = io.ReadAll(file)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version": "v260714-0001", "git_sha": r.FormValue("git_sha"),
			"sha256": f.publishSHA, "size": len(f.publishBody),
			"notes": r.FormValue("notes"), "published_at": 1.0,
			"channel_ga": false, "ga_at": nil,
		})
	})
	mux.HandleFunc("POST /api/promote", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+f.token {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":{"code":"unauthorized","message":"this publish token is not valid"}}`)
			return
		}
		var body struct {
			Version string `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Fatalf("fake updater: promote body: %v", err)
		}
		f.promoteVer = body.Version
		if body.Version == "v269999-0001" {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":{"code":"not_found","message":"version \"v269999-0001\" is not published here — publish it first, then promote"}}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version": body.Version, "git_sha": "cafe123", "sha256": strings.Repeat("a", 64),
			"size": 4, "notes": "", "published_at": 1.0,
			"channel_ga": true, "ga_at": 1752480000.0,
		})
	})
	return mux
}

// run executes realMain with the given env pairs, capturing output.
func run(argv []string, env map[string]string) (int, string) {
	var out bytes.Buffer
	rc := realMain(argv, func(k string) string { return env[k] }, &out)
	return rc, out.String()
}

func writeArtifact(t *testing.T, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ocserverd")
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	return path
}

func TestPublishHappyPath(t *testing.T) {
	fake := &fakeUpdater{t: t, token: "ocu-pub-test"}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	artifact := []byte("fake ocserverd bytes")
	path := writeArtifact(t, artifact)

	rc, out := run(
		[]string{"publish", "--file", path, "--notes", "test", "--git-sha", "cafe123"},
		map[string]string{envUpdaterURL: srv.URL, envPublishToken: "ocu-pub-test"})
	if rc != 0 {
		t.Fatalf("publish rc=%d, want 0 — output: %s", rc, out)
	}
	// The client computed the digest itself and uploaded the exact bytes.
	if !bytes.Equal(fake.publishBody, artifact) {
		t.Fatalf("uploaded bytes differ from the artifact")
	}
	wantSHA := sha256hex(artifact)
	if fake.publishSHA != wantSHA {
		t.Fatalf("uploaded sha256 = %q, want %q", fake.publishSHA, wantSHA)
	}
	if fake.publishVer != "" {
		t.Fatalf("publish must OMIT version by default (server generates), sent %q", fake.publishVer)
	}
	// The report tells the publisher the released version + the next step.
	for _, want := range []string{"published v260714-0001 (beta channel)", "ocrelease promote v260714-0001"} {
		if !strings.Contains(out, want) {
			t.Fatalf("publish output lacks %q:\n%s", want, out)
		}
	}
}

func TestPublishExplicitVersionRidesThrough(t *testing.T) {
	fake := &fakeUpdater{t: t, token: "ocu-pub-test"}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	path := writeArtifact(t, []byte("bytes"))

	rc, out := run(
		[]string{"publish", "--file", path, "--version", "v260714-0042"},
		map[string]string{envUpdaterURL: srv.URL, envPublishToken: "ocu-pub-test"})
	if rc != 0 {
		t.Fatalf("publish rc=%d, want 0 — output: %s", rc, out)
	}
	if fake.publishVer != "v260714-0042" {
		t.Fatalf("explicit --version must ride through, sent %q", fake.publishVer)
	}
}

func TestPromoteHappyPath(t *testing.T) {
	fake := &fakeUpdater{t: t, token: "ocu-pub-test"}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	rc, out := run([]string{"promote", "v260714-0001"},
		map[string]string{envUpdaterURL: srv.URL, envPublishToken: "ocu-pub-test"})
	if rc != 0 {
		t.Fatalf("promote rc=%d, want 0 — output: %s", rc, out)
	}
	if fake.promoteVer != "v260714-0001" {
		t.Fatalf("promote sent version %q, want v260714-0001", fake.promoteVer)
	}
	if !strings.Contains(out, "v260714-0001 is GA") {
		t.Fatalf("promote output lacks the GA confirmation:\n%s", out)
	}
}

func TestPromoteUnknownVersionSurfacesServerMessage(t *testing.T) {
	fake := &fakeUpdater{t: t, token: "ocu-pub-test"}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	rc, out := run([]string{"promote", "v269999-0001"},
		map[string]string{envUpdaterURL: srv.URL, envPublishToken: "ocu-pub-test"})
	if rc != 1 {
		t.Fatalf("promote of unknown version rc=%d, want 1 — output: %s", rc, out)
	}
	if !strings.Contains(out, "not published here") {
		t.Fatalf("the server's own message must surface verbatim:\n%s", out)
	}
}

func TestBadTokenSurfacesRefusal(t *testing.T) {
	fake := &fakeUpdater{t: t, token: "ocu-pub-right"}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	path := writeArtifact(t, []byte("bytes"))

	rc, out := run([]string{"publish", "--file", path},
		map[string]string{envUpdaterURL: srv.URL, envPublishToken: "ocu-pub-wrong"})
	if rc != 1 || !strings.Contains(out, "publish refused (401)") {
		t.Fatalf("bad token: rc=%d output=%s", rc, out)
	}
}

func TestMissingCredentialsIsUsageError(t *testing.T) {
	path := writeArtifact(t, []byte("bytes"))
	for _, env := range []map[string]string{
		{},
		{envUpdaterURL: "http://127.0.0.1:1"},
		{envPublishToken: "ocu-pub-x"},
	} {
		rc, out := run([]string{"publish", "--file", path}, env)
		if rc != 2 || !strings.Contains(out, envPublishToken) {
			t.Fatalf("missing creds env=%v: rc=%d output=%s", env, rc, out)
		}
		rc, out = run([]string{"promote", "v260714-0001"}, env)
		if rc != 2 || !strings.Contains(out, envPublishToken) {
			t.Fatalf("missing creds (promote) env=%v: rc=%d output=%s", env, rc, out)
		}
	}
}

func TestUsageFaces(t *testing.T) {
	if rc, out := run([]string{"--help"}, nil); rc != 0 || !strings.Contains(out, "publish") {
		t.Fatalf("--help: rc=%d output=%s", rc, out)
	}
	if rc, _ := run(nil, nil); rc != 2 {
		t.Fatalf("no args must be a usage error (rc 2)")
	}
	if rc, out := run([]string{"bogus"}, nil); rc != 2 || !strings.Contains(out, "unknown subcommand") {
		t.Fatalf("unknown subcommand: rc=%d output=%s", rc, out)
	}
	if rc, out := run([]string{"publish"}, map[string]string{envUpdaterURL: "http://x", envPublishToken: "t"}); rc != 2 || !strings.Contains(out, "--file") {
		t.Fatalf("publish without --file: rc=%d output=%s", rc, out)
	}
	if rc, _ := run([]string{"promote"}, map[string]string{envUpdaterURL: "http://x", envPublishToken: "t"}); rc != 2 {
		t.Fatalf("promote without a version must be a usage error (rc 2)")
	}
}

// TestReleaseLabel: the r-N serial (T-e9d1) leads the human label when present,
// falling back to the bare version against a pre-serial updater.
func TestReleaseLabel(t *testing.T) {
	withTag := releaseFace{Version: "v260715-0099", ReleaseTag: "r-99"}
	if got := withTag.releaseLabel(); got != "r-99 (v260715-0099)" {
		t.Fatalf("with tag: got %q", got)
	}
	legacy := releaseFace{Version: "v260715-0099"}
	if got := legacy.releaseLabel(); got != "v260715-0099" {
		t.Fatalf("pre-serial updater: got %q", got)
	}
}

// sha256hex mirrors the tiny helper the server suite uses.
func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
