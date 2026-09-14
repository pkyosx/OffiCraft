// Skeleton generated from server/ocserverd/upgrade.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
)

// upgradeTestTarball writes a gzip tarball at <dir>/release.tar.gz carrying one
// regular member per entry, and answers its path.
func upgradeTestTarball(t *testing.T, dir string, members map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range members {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	path := filepath.Join(dir, "release.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write tarball: %v", err)
	}
	return path
}

// upgradeTestDirEntries answers the names dir holds, so a failure that promises
// "nothing was changed" can be held to it.
func upgradeTestDirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// upgradeTestKnownRelease puts the server in the state a successful check
// leaves it in: GitHub answered, and the newest release it named is tag.
func upgradeTestKnownRelease(t *testing.T, api *apiServer, tag string) {
	t.Helper()
	api.updateMu.Lock()
	defer api.updateMu.Unlock()
	api.updateCheck = updateCheckState{
		checkedAt: time.Now(),
		lastOKAt:  time.Now(),
		ok:        true,
		rel:       githubRelease{TagName: tag},
	}
}

func upgradeTestReleaseServer(t *testing.T, tag, binary string) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	tarPath := upgradeTestTarball(t, dir, map[string]string{
		"release/" + serverBinaryName: binary,
	})
	tarball, err := os.ReadFile(tarPath)
	if err != nil {
		t.Fatalf("read tarball: %v", err)
	}
	digest := sha256.Sum256(tarball)
	sha := hex.EncodeToString(digest[:])
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/" + releaseRepo + "/releases":
			_ = json.NewEncoder(w).Encode([]githubRelease{{
				TagName: tag,
				HTMLURL: "https://example.invalid/releases/" + tag,
				Assets: []githubReleaseAsset{
					{Name: checksumsAssetName, BrowserDownloadURL: srv.URL + "/checksums.txt"},
					{Name: releaseAssetName(tag), BrowserDownloadURL: srv.URL + "/release.tar.gz"},
				},
			}})
		case "/checksums.txt":
			_, _ = io.WriteString(w, sha+"  "+releaseAssetName(tag)+"\n")
		case "/release.tar.gz":
			_, _ = w.Write(tarball)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPinUpgradeRelease(t *testing.T) {
	t.Run("pins the semver greatest release from a fresh authoritative read", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.RequestURI()
			_ = json.NewEncoder(w).Encode([]githubRelease{
				{TagName: "v0.9.9"},
				{TagName: "v1.2.3"},
			})
		}))
		defer srv.Close()

		api := &apiServer{releaseAPIBase: srv.URL}
		rel, fail := api.pinUpgradeRelease()

		if fail != nil {
			t.Fatalf("want no failure, got %d %q", fail.status, fail.message)
		}
		if rel.TagName != "v1.2.3" {
			t.Fatalf("pinned tag: %q", rel.TagName)
		}
		if gotPath != "/repos/pkyosx/OffiCraft/releases?per_page=20" {
			t.Fatalf("request path: %q", gotPath)
		}
	})

	t.Run("a GitHub 404 is reported as no published release", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		defer srv.Close()

		api := &apiServer{releaseAPIBase: srv.URL}
		rel, fail := api.pinUpgradeRelease()

		if rel.TagName != "" || rel.HTMLURL != "" || rel.Draft || rel.Prerelease || len(rel.Assets) != 0 {
			t.Fatalf("release: %#v", rel)
		}
		if fail == nil || fail.status != http.StatusConflict || fail.Error() != "no release is published on GitHub — nothing to install" {
			t.Fatalf("failure: %#v", fail)
		}
	})
}

func TestFindReleaseAsset(t *testing.T) {
	rel := githubRelease{
		TagName: "v1.2.3",
		Assets: []githubReleaseAsset{
			{Name: "checksums.txt", BrowserDownloadURL: "https://example.invalid/checksums.txt", Size: 130},
		},
	}

	t.Run("an asset the release carries resolves to its download entry", func(t *testing.T) {
		asset, fail := findReleaseAsset(rel, "checksums.txt")

		if fail != nil {
			t.Fatalf("want no failure, got %d %q", fail.status, fail.message)
		}
		want := githubReleaseAsset{
			Name:               "checksums.txt",
			BrowserDownloadURL: "https://example.invalid/checksums.txt",
			Size:               130,
		}
		if asset != want {
			t.Fatalf("asset: %#v", asset)
		}
	})

	t.Run("an asset the release does not carry refuses with 502", func(t *testing.T) {
		asset, fail := findReleaseAsset(rel, "officraft-v1.2.3-darwin-arm64.tar.gz")

		if asset != (githubReleaseAsset{}) {
			t.Fatalf("asset: %#v", asset)
		}
		if fail == nil {
			t.Fatalf("want a failure")
		}
		if fail.status != 502 {
			t.Fatalf("status: %d", fail.status)
		}
		if fail.Error() != `release v1.2.3 carries no "officraft-v1.2.3-darwin-arm64.tar.gz" asset — refusing an unverifiable install; nothing was changed` {
			t.Fatalf("message: %q", fail.Error())
		}
	})
}

func TestHttpGetAsset(t *testing.T) {
	t.Run("follows a redirect and returns the successful response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/redirect" {
				http.Redirect(w, r, "/asset", http.StatusFound)
				return
			}
			_, _ = io.WriteString(w, "release bytes")
		}))
		defer srv.Close()

		resp, fail := httpGetAsset(srv.URL+"/redirect", upgradeBodyBudget)
		if fail != nil {
			t.Fatalf("want no failure, got %d %q", fail.status, fail.message)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(body) != "release bytes" {
			t.Fatalf("body: %q", body)
		}
	})

	t.Run("a non-200 answer is refused and its body is closed", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			pipeAssetServer(t, pacedAssetServer(http.StatusNotFound, len("not found"), 0, 0, []string{"not found"}))
			closed := spyAssetBodyClose(t)

			resp, fail := httpGetAsset("https://example.invalid/missing", upgradeBodyBudget)
			if resp != nil {
				t.Fatalf("response: %#v", resp)
			}
			if fail == nil || fail.status != http.StatusBadGateway || fail.Error() != "the asset download answered 404 for https://example.invalid/missing — nothing was changed" {
				t.Fatalf("failure: %#v", fail)
			}
			if !closed.Load() {
				t.Error("the refused response's body was left open")
			}
		})
	})

	t.Run("the shared client dials with upgradeDialer, resolves proxies from the environment and negotiates HTTP/2 with a server that offers it", func(t *testing.T) {
		tr, ok := upgradeAssetClient().Transport.(*http.Transport)
		if !ok {
			t.Fatalf("Transport = %T, want *http.Transport", upgradeAssetClient().Transport)
		}
		if tr.DialContext == nil ||
			reflect.ValueOf(tr.DialContext).Pointer() != reflect.ValueOf(upgradeDialer.DialContext).Pointer() {
			t.Error("the transport does not dial with upgradeDialer itself, but a wrapper — which may put its own deadline on the connection and cut a download the stall guard would let finish")
		}
		// Identity, not non-nil: a function that resolves no proxy at all passes a nil check.
		if tr.Proxy == nil ||
			reflect.ValueOf(tr.Proxy).Pointer() != reflect.ValueOf(http.ProxyFromEnvironment).Pointer() {
			t.Error("Proxy is not http.ProxyFromEnvironment — a machine behind a corporate proxy would fail to download, silently")
		}
		if a, b := upgradeAssetClient(), upgradeAssetClient(); a.Transport != b.Transport {
			t.Error("each call builds its own Transport — every upgrade leaves an orphan behind, holding idle connections and their read loops")
		}

		srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "release bytes")
		}))
		srv.EnableHTTP2 = true
		srv.StartTLS()
		defer srv.Close()
		defer func(c *tls.Config) {
			tr.CloseIdleConnections()
			tr.TLSClientConfig = c
		}(tr.TLSClientConfig)
		cfg := &tls.Config{}
		if tr.TLSClientConfig != nil {
			cfg = tr.TLSClientConfig.Clone()
		}
		cfg.RootCAs = srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
		// The Transport adds h2 to its TLS config only once, on first use, so a
		// config swapped in by an earlier test may not carry it.
		cfg.NextProtos = []string{"h2", "http/1.1"}
		tr.TLSClientConfig = cfg

		resp, fail := httpGetAsset(srv.URL+"/asset", upgradeMetaBudget)
		if fail != nil {
			t.Fatalf("want no failure, got %d %q", fail.status, fail.message)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if resp.ProtoMajor != 2 || string(body) != "release bytes" {
			t.Fatalf("protocol %s, body %q — want HTTP/2, which this path had through http.DefaultTransport", resp.Proto, body)
		}
	})
}

func TestStallGuard(t *testing.T) {
	t.Run("a read that returns no bytes is not progress", func(t *testing.T) {
		const every = 60 * time.Millisecond
		fired := make(chan struct{})
		released := make(chan struct{})
		defer close(released)
		body := newStallGuard(io.NopCloser(emptyReader{released: released}), func() { close(fired) }, every)
		go func() { _, _ = io.ReadAll(body) }()
		select {
		case <-fired:
		case <-time.After(3 * time.Second):
			t.Fatal("never cancelled — empty reads are being treated as progress")
		}
	})

	t.Run("Close releases the request context", func(t *testing.T) {
		var released atomic.Bool
		body := newStallGuard(io.NopCloser(strings.NewReader("x")),
			func() { released.Store(true) }, time.Hour)
		if err := body.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		if !released.Load() {
			t.Error("Close did not release the request context — every fetch would leak one, plus an armed timer")
		}
	})
}

// emptyReader answers (0, nil) — a read that legally delivered nothing — for as
// long as the stream is open, and EOF once released. It must not end on its own:
// a stream that ends stops the reads, and a timer renewed by every read would
// then fire anyway and pass for one that ignores empty reads.
type emptyReader struct{ released <-chan struct{} }

func (r emptyReader) Read(p []byte) (int, error) {
	select {
	case <-r.released:
		return 0, io.EOF
	case <-time.After(5 * time.Millisecond):
		return 0, nil
	}
}

// upgradeTestPhaseFloor and upgradeTestSilenceFloor are RFC 6298's backoff
// after two and four losses from its 1s minimum retransmission timeout.
const (
	upgradeTestPhaseFloor   = 3 * time.Second
	upgradeTestSilenceFloor = 15 * time.Second
)

// pipeAssetServer routes every asset fetch for the rest of the test to serve
// over an in-memory connection. Only the byte source is replaced: the shipped
// Transport still runs its own TLS handshake, header wait and body framing,
// on the synctest clock. Proxies are switched off, or a proxy in the
// environment would be sent a CONNECT this server does not speak.
func pipeAssetServer(t *testing.T, serve func(conn net.Conn)) {
	t.Helper()
	tr := upgradeAssetSharedClient.Transport.(*http.Transport)
	dial, shipped, proxy := tr.DialContext, tr.TLSClientConfig, tr.Proxy
	tr.Proxy = nil
	tr.DialContext = func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		go serve(server)
		return client, nil
	}
	cfg := &tls.Config{}
	if shipped != nil {
		cfg = shipped.Clone()
	}
	_, cfg.RootCAs = upgradeTestPipeCert()
	cfg.ServerName = "example.invalid"
	tr.TLSClientConfig = cfg
	t.Cleanup(func() {
		tr.CloseIdleConnections()
		tr.DialContext, tr.TLSClientConfig, tr.Proxy = dial, shipped, proxy
	})
}

var upgradeTestPipeCert = sync.OnceValues(func() (tls.Certificate, *x509.CertPool) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		DNSNames:     []string{"example.invalid"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(100 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		panic(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
})

// acceptPipeRequest completes the server side of the TLS handshake and reads
// the request, answering false once the client has gone.
func acceptPipeRequest(conn net.Conn) (net.Conn, bool) {
	cert, _ := upgradeTestPipeCert()
	tc := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
	if _, err := http.ReadRequest(bufio.NewReader(tc)); err != nil {
		return nil, false
	}
	return tc, true
}

// pacedAssetServer answers status headerDelay after the request, with a body
// framed by Content-Length, or chunked when length is negative, and writes
// chunks gap apart, each gap preceding its chunk. Whatever the framing still
// owes is never sent.
func pacedAssetServer(status, length int, headerDelay, gap time.Duration, chunks []string) func(net.Conn) {
	framing := fmt.Sprintf("Content-Length: %d", length)
	if length < 0 {
		framing = "Transfer-Encoding: chunked"
	}
	return func(conn net.Conn) {
		defer conn.Close()
		tc, gone, ok := answerPipeRequestAfter(conn, headerDelay)
		if !ok {
			return
		}
		if _, err := fmt.Fprintf(tc, "HTTP/1.1 %d %s\r\n%s\r\n\r\n", status, http.StatusText(status), framing); err != nil {
			return
		}
		for _, c := range chunks {
			select {
			case <-gone:
				return
			case <-time.After(gap):
			}
			if length < 0 {
				c = fmt.Sprintf("%x\r\n%s\r\n", len(c), c)
			}
			if _, err := io.WriteString(tc, c); err != nil {
				return
			}
		}
		<-gone
	}
}

// answerPipeRequestAfter accepts the request and holds it for delay before the
// caller answers. gone closes once the client has left.
func answerPipeRequestAfter(conn net.Conn, delay time.Duration) (net.Conn, <-chan struct{}, bool) {
	tc, ok := acceptPipeRequest(conn)
	if !ok {
		return nil, nil, false
	}
	// A pipe has no buffer: unless something keeps reading, the client's
	// close_notify blocks behind our write and the bubble never drains.
	gone := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, tc)
		close(gone)
	}()
	select {
	case <-gone:
		return nil, nil, false
	case <-time.After(delay):
	}
	return tc, gone, true
}

// redirectFirstHop answers the first connection with a 302, headerDelay after
// its request, to a second host — which the Transport must dial afresh — and
// hands every later connection to next.
func redirectFirstHop(headerDelay time.Duration, next func(net.Conn)) func(net.Conn) {
	var dials atomic.Int32
	return func(conn net.Conn) {
		if dials.Add(1) > 1 {
			next(conn)
			return
		}
		defer conn.Close()
		tc, gone, ok := answerPipeRequestAfter(conn, headerDelay)
		if !ok {
			return
		}
		if _, err := io.WriteString(tc, "HTTP/1.1 302 Found\r\nLocation: https://cdn.example.invalid/release.tar.gz\r\nContent-Length: 0\r\n\r\n"); err != nil {
			return
		}
		<-gone
	}
}

// spyAssetBodyClose reports whether the body of the response the shipped
// Transport returned was closed.
func spyAssetBodyClose(t *testing.T) *atomic.Bool {
	t.Helper()
	closed := &atomic.Bool{}
	shipped := upgradeAssetSharedClient.Transport
	upgradeAssetSharedClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp, err := shipped.RoundTrip(req)
		if resp != nil {
			resp.Body = &closeSpyBody{ReadCloser: resp.Body, closed: closed}
		}
		return resp, err
	})
	t.Cleanup(func() { upgradeAssetSharedClient.Transport = shipped })
	return closed
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type closeSpyBody struct {
	io.ReadCloser
	closed *atomic.Bool
}

func (b *closeSpyBody) Close() error {
	b.closed.Store(true)
	return b.ReadCloser.Close()
}

func byteChunks(s string) []string {
	chunks := make([]string, len(s))
	for i := range s {
		chunks[i] = s[i : i+1]
	}
	return chunks
}

// wantCutAt requires a fetch to have been cut at bound: not before it, and not
// a second or more after.
func wantCutAt(t *testing.T, elapsed, bound time.Duration) {
	t.Helper()
	const slack = time.Second
	if elapsed < bound {
		t.Errorf("cut after %v, before its bound of %v", elapsed, bound)
	} else if elapsed >= bound+slack {
		t.Errorf("cut after %v, %v or more past its bound of %v", elapsed, slack, bound)
	}
}

func TestFetchExpectedSHA(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	sumsRelease := githubRelease{
		TagName: "v1.2.3",
		Assets:  []githubReleaseAsset{{Name: checksumsAssetName, BrowserDownloadURL: "https://example.invalid/checksums.txt"}},
	}

	t.Run("extracts the matching digest and tolerates binary mode", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "not a checksum line\n"+digest+"  *target.tar.gz\n")
		}))
		defer srv.Close()

		rel := githubRelease{
			TagName: "v1.2.3",
			Assets:  []githubReleaseAsset{{Name: checksumsAssetName, BrowserDownloadURL: srv.URL}},
		}
		got, fail := fetchExpectedSHA(rel, "target.tar.gz")

		if fail != nil {
			t.Fatalf("want no failure, got %d %q", fail.status, fail.message)
		}
		if got != digest {
			t.Fatalf("digest: %q", got)
		}
	})

	t.Run("a checksums.txt that keeps arriving within the metadata budget yields its digest", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			const gap = 50 * time.Second
			line := digest + "  target.tar.gz\n"
			pipeAssetServer(t, pacedAssetServer(http.StatusOK, len(line), 0, gap, []string{line[:40], line[40:]}))

			started := time.Now()
			got, fail := fetchExpectedSHA(sumsRelease, "target.tar.gz")
			elapsed := time.Since(started)

			if fail != nil {
				t.Fatalf("a checksums.txt taking %v, never silent for more than %v, was cut: %d %q", elapsed, gap, fail.status, fail.message)
			}
			if got != digest {
				t.Fatalf("digest: %q", got)
			}
			if elapsed < 2*gap {
				t.Fatalf("the fetch took %v, not the paced %v — this case proves nothing", elapsed, 2*gap)
			}
		})
	})

	t.Run("a checksums.txt that never finishes is cut at the metadata budget, not before", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			const gap = 30 * time.Second
			trickle := byteChunks(strings.Repeat("x", int(2*upgradeMetaBudget/gap)))
			pipeAssetServer(t, pacedAssetServer(http.StatusOK, len(trickle)+1, 0, gap, trickle))

			started := time.Now()
			_, fail := fetchExpectedSHA(sumsRelease, "target.tar.gz")

			if fail == nil || fail.status != http.StatusBadGateway {
				t.Fatalf("failure: %#v", fail)
			}
			wantCutAt(t, time.Since(started), upgradeMetaBudget)
		})
	})

	t.Run("a checksums.txt that sends its headers and then nothing is cut at the stall timeout, not before", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			pipeAssetServer(t, pacedAssetServer(http.StatusOK, len(digest)+len("  target.tar.gz\n"), 0, 0, nil))

			started := time.Now()
			_, fail := fetchExpectedSHA(sumsRelease, "target.tar.gz")

			if fail == nil || fail.status != http.StatusBadGateway {
				t.Fatalf("failure: %#v", fail)
			}
			wantCutAt(t, time.Since(started), upgradeStallTimeout)
		})
	})

	t.Run("a checksum entry for another asset is refused", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, digest+"  other.tar.gz\n")
		}))
		defer srv.Close()

		rel := githubRelease{
			TagName: "v1.2.3",
			Assets:  []githubReleaseAsset{{Name: checksumsAssetName, BrowserDownloadURL: srv.URL}},
		}
		got, fail := fetchExpectedSHA(rel, "target.tar.gz")

		if got != "" {
			t.Fatalf("digest: %q", got)
		}
		if fail == nil || fail.status != http.StatusBadGateway || fail.Error() != "release v1.2.3's checksums.txt carries no sha256 for target.tar.gz — refusing an unverifiable download; nothing was changed" {
			t.Fatalf("failure: %#v", fail)
		}
	})
}

func TestIsLowerHex64(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want bool
	}{
		{name: "lowercase digits and letters", text: "0123456789abcdef", want: true},
		{name: "uppercase is not lower hex", text: "0123456789ABCDEF", want: false},
		{name: "punctuation is rejected", text: "0123456789abcdef-", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isLowerHex64(tc.text); got != tc.want {
				t.Fatalf("isLowerHex64(%q): want %v, got %v", tc.text, tc.want, got)
			}
		})
	}
}

func TestUpgradeTargetPath(t *testing.T) {
	t.Run("uses the explicit test seam", func(t *testing.T) {
		api := &apiServer{upgradeExeOverride: filepath.Join(t.TempDir(), "ocserverd")}
		got, err := api.upgradeTargetPath()
		if err != nil {
			t.Fatalf("upgradeTargetPath: %v", err)
		}
		if got != api.upgradeExeOverride {
			t.Fatalf("target: %q", got)
		}
	})

	t.Run("resolves the running executable when no seam is set", func(t *testing.T) {
		api := &apiServer{}
		got, err := api.upgradeTargetPath()
		if err != nil {
			t.Fatalf("upgradeTargetPath: %v", err)
		}
		exe, err := os.Executable()
		if err != nil {
			t.Fatalf("os.Executable: %v", err)
		}
		want, err := filepath.EvalSymlinks(exe)
		if err != nil {
			want = exe
		}
		if got != want {
			t.Fatalf("target: want %q, got %q", want, got)
		}
	})
}

func TestDownloadUpgradeTarball(t *testing.T) {
	const body = "verified release bytes"
	checksum := sha256.Sum256([]byte(body))
	wantSHA := hex.EncodeToString(checksum[:])
	tarball := githubReleaseAsset{Name: "release.tar.gz", BrowserDownloadURL: "https://example.invalid/release.tar.gz"}

	t.Run("streams the body into the requested directory and verifies its digest", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, body)
		}))
		defer srv.Close()
		dir := t.TempDir()

		path, fail := downloadUpgradeTarball(githubReleaseAsset{
			Name: "release.tar.gz", BrowserDownloadURL: srv.URL,
		}, wantSHA, dir)

		if fail != nil {
			t.Fatalf("want no failure, got %d %q", fail.status, fail.message)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read staged body: %v", err)
		}
		if string(data) != body {
			t.Fatalf("staged body: %q", data)
		}
		if filepath.Dir(path) != dir || !strings.HasPrefix(filepath.Base(path), ".officraft-upgrade-") {
			t.Fatalf("staged path: %q", path)
		}
	})

	t.Run("a tarball that keeps arriving until just short of the body budget completes", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			const gap = 30 * time.Second
			payload := strings.Repeat("r", int((upgradeBodyBudget-time.Minute)/gap))
			sum := sha256.Sum256([]byte(payload))
			pipeAssetServer(t, pacedAssetServer(http.StatusOK, len(payload), 0, gap, byteChunks(payload)))
			dir := t.TempDir()

			started := time.Now()
			path, fail := downloadUpgradeTarball(tarball, hex.EncodeToString(sum[:]), dir)
			elapsed := time.Since(started)

			if fail != nil {
				t.Fatalf("a download taking %v, never silent for more than %v, was cut: %d %q", elapsed, gap, fail.status, fail.message)
			}
			if want := time.Duration(len(payload)) * gap; elapsed < want {
				t.Fatalf("the download took %v, not the paced %v — this case proves nothing", elapsed, want)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read staged body: %v", err)
			}
			if string(data) != payload {
				t.Fatalf("staged body: %q", data)
			}
		})
	})

	t.Run("a digest mismatch removes the partial staging file", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, body)
		}))
		defer srv.Close()
		dir := t.TempDir()

		path, fail := downloadUpgradeTarball(githubReleaseAsset{
			Name: "release.tar.gz", BrowserDownloadURL: srv.URL,
		}, strings.Repeat("0", 64), dir)

		if path != "" {
			t.Fatalf("path: %q", path)
		}
		if fail == nil || fail.status != http.StatusBadGateway {
			t.Fatalf("failure: %#v", fail)
		}
		if got := upgradeTestDirEntries(t, dir); len(got) != 0 {
			t.Fatalf("staging directory: %v", got)
		}
	})

	t.Run("a tarball that never finishes is cut at the body budget, not before, and stages nothing", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			const gap = 30 * time.Second
			trickle := byteChunks(strings.Repeat("r", int(2*upgradeBodyBudget/gap)))
			pipeAssetServer(t, pacedAssetServer(http.StatusOK, len(trickle)+1, 0, gap, trickle))
			dir := t.TempDir()

			started := time.Now()
			path, fail := downloadUpgradeTarball(tarball, wantSHA, dir)

			if path != "" || fail == nil || fail.status != http.StatusBadGateway {
				t.Fatalf("path %q, failure %#v", path, fail)
			}
			wantCutAt(t, time.Since(started), upgradeBodyBudget)
			if got := upgradeTestDirEntries(t, dir); len(got) != 0 {
				t.Fatalf("staging directory: %v", got)
			}
		})
	})

	// Each hop's headers arrive this long after its request — inside the header
	// timeout, so the stall timer, which starts at the final hop's headers, is
	// the only bound left to cut these.
	const hopDelay = 20 * time.Second
	for _, tc := range []struct {
		name     string
		length   int
		sent     []string
		redirect bool
	}{
		{name: "a tarball that sends its headers and then nothing is cut at the stall timeout after its headers, not before", length: len(body)},
		{name: "a tarball that goes silent part way is cut at the stall timeout after its headers, not before", length: len(body), sent: []string{body[:4]}},
		{name: "a chunked tarball that goes silent part way is cut at the stall timeout after its headers, not before", length: -1, sent: []string{body[:4]}},
		{name: "a redirected tarball that goes silent part way is cut at the stall timeout after the final hop's headers, not before", length: len(body), sent: []string{body[:4]}, redirect: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				serve, hops := pacedAssetServer(http.StatusOK, tc.length, hopDelay, 0, tc.sent), 1
				if tc.redirect {
					serve, hops = redirectFirstHop(hopDelay, serve), 2
				}
				pipeAssetServer(t, serve)

				started := time.Now()
				path, fail := downloadUpgradeTarball(tarball, wantSHA, t.TempDir())

				if path != "" || fail == nil || fail.status != http.StatusBadGateway {
					t.Fatalf("path %q, failure %#v", path, fail)
				}
				wantCutAt(t, time.Since(started)-time.Duration(hops)*hopDelay, upgradeStallTimeout)
			})
		})
	}

	t.Run("a server that never answers the request is cut at the response header timeout, not before", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			pipeAssetServer(t, func(conn net.Conn) {
				defer conn.Close()
				if tc, ok := acceptPipeRequest(conn); ok {
					_, _ = io.Copy(io.Discard, tc)
				}
			})

			started := time.Now()
			path, fail := downloadUpgradeTarball(tarball, wantSHA, t.TempDir())

			if path != "" || fail == nil || fail.status != http.StatusBadGateway {
				t.Fatalf("path %q, failure %#v", path, fail)
			}
			wantCutAt(t, time.Since(started), upgradeHeaderTimeout)
		})
	})

	t.Run("a TLS handshake that never completes is cut at the TLS handshake timeout, not before", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			pipeAssetServer(t, func(conn net.Conn) {
				defer conn.Close()
				_, _ = io.Copy(io.Discard, conn)
			})

			started := time.Now()
			path, fail := downloadUpgradeTarball(tarball, wantSHA, t.TempDir())

			if path != "" || fail == nil || fail.status != http.StatusBadGateway {
				t.Fatalf("path %q, failure %#v", path, fail)
			}
			wantCutAt(t, time.Since(started), upgradeTLSHandshakeTimeout)
		})
	})

	t.Run("a connect that never completes is abandoned at the dial timeout, not before", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			defer func(c func(context.Context, string, string, syscall.RawConn) error) {
				upgradeDialer.ControlContext = c
			}(upgradeDialer.ControlContext)
			upgradeDialer.ControlContext = func(ctx context.Context, _, _ string, _ syscall.RawConn) error {
				<-ctx.Done()
				return ctx.Err()
			}

			started := time.Now()
			path, fail := downloadUpgradeTarball(githubReleaseAsset{
				Name: "release.tar.gz", BrowserDownloadURL: "http://127.0.0.1:1/release.tar.gz",
			}, wantSHA, t.TempDir())

			if path != "" || fail == nil || fail.status != http.StatusBadGateway {
				t.Fatalf("path %q, failure %#v", path, fail)
			}
			wantCutAt(t, time.Since(started), upgradeDialTimeout)
		})
	})
}

func TestUpgradeShippedBounds(t *testing.T) {
	t.Run("silence is caught first, the metadata fetch next, the tarball last", func(t *testing.T) {
		// Let the metadata budget grow past the tarball's backstop and a dribbling
		// checksums.txt may hold runUpgrade's lock as long as a whole download.
		if upgradeStallTimeout >= upgradeMetaBudget {
			t.Errorf("upgradeStallTimeout = %v, upgradeMetaBudget = %v — silence must be caught before either fetch's budget expires, or the stall guard never gets to act",
				upgradeStallTimeout, upgradeMetaBudget)
		}
		if upgradeMetaBudget >= upgradeBodyBudget {
			t.Errorf("upgradeMetaBudget = %v, upgradeBodyBudget = %v — the few-hundred-byte fetch must be bounded more tightly than the multi-megabyte one, which is the whole reason they are two budgets",
				upgradeMetaBudget, upgradeBodyBudget)
		}
	})

	t.Run("silence and the metadata fetch are bounded in minutes, not hours", func(t *testing.T) {
		const ceiling = 5 * time.Minute
		if upgradeStallTimeout > ceiling {
			t.Errorf("upgradeStallTimeout = %v, want at most %v — a dead source would wedge the upgrade for that long", upgradeStallTimeout, ceiling)
		}
		if upgradeMetaBudget > ceiling {
			t.Errorf("upgradeMetaBudget = %v, want at most %v — a trickling checksums.txt would wedge the upgrade for that long", upgradeMetaBudget, ceiling)
		}
	})

	t.Run("a release of upgradeMaxBytes finishes within the tarball budget at the 2026-09-14 link rate", func(t *testing.T) {
		const incidentBytesPerSecond = 198 * 1000
		need := time.Duration(upgradeMaxBytes) * time.Second / incidentBytesPerSecond
		if upgradeBodyBudget < need {
			t.Errorf("upgradeBodyBudget = %v, want at least %v — the largest release this path accepts would be cut mid-download on the 2026-09-14 link", upgradeBodyBudget, need)
		}
	})

	t.Run("the metadata budget covers the connect, TLS and header phases of both the asset URL and its redirect hop", func(t *testing.T) {
		if hop := upgradeDialTimeout + upgradeTLSHandshakeTimeout + upgradeHeaderTimeout; upgradeMetaBudget < 2*hop {
			t.Errorf("upgradeMetaBudget = %v, want at least 2 × (dial %v + TLS %v + header %v) — each phase timeout applies again on the CDN hop GitHub redirects to while the budget spans both, so it would cut a fetch its own phases still allow",
				upgradeMetaBudget, upgradeDialTimeout, upgradeTLSHandshakeTimeout, upgradeHeaderTimeout)
		}
	})

	t.Run("no bound gives up inside TCP's retransmission backoff", func(t *testing.T) {
		for _, b := range []struct {
			name  string
			got   time.Duration
			floor time.Duration
		}{
			{"upgradeDialTimeout", upgradeDialTimeout, upgradeTestPhaseFloor},
			{"upgradeTLSHandshakeTimeout", upgradeTLSHandshakeTimeout, upgradeTestPhaseFloor},
			{"upgradeHeaderTimeout", upgradeHeaderTimeout, upgradeTestPhaseFloor},
			{"upgradeStallTimeout", upgradeStallTimeout, upgradeTestSilenceFloor},
		} {
			if b.got < b.floor {
				t.Errorf("%s = %v, want at least %v — a lossy but working link would fail the upgrade", b.name, b.got, b.floor)
			}
		}
	})
}

func TestExtractServerBinary(t *testing.T) {
	t.Run("the ocserverd member is extracted as an executable at any depth", func(t *testing.T) {
		dir := t.TempDir()
		tarPath := upgradeTestTarball(t, dir, map[string]string{
			"officraft-v1.2.3/ocserverd": "candidate binary",
		})

		path, fail := extractServerBinary(tarPath, dir)

		if fail != nil {
			t.Fatalf("want no failure, got %d %q", fail.status, fail.message)
		}
		if filepath.Dir(path) != dir {
			t.Fatalf("path: %q", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(data) != "candidate binary" {
			t.Fatalf("extracted: %q", data)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Fatalf("mode: %v", info.Mode().Perm())
		}
	})

	t.Run("a tarball carrying no ocserverd member refuses with 502 and stages nothing", func(t *testing.T) {
		dir := t.TempDir()
		tarPath := upgradeTestTarball(t, dir, map[string]string{"README.md": "not a binary"})

		path, fail := extractServerBinary(tarPath, dir)

		if path != "" {
			t.Fatalf("path: %q", path)
		}
		if fail == nil {
			t.Fatalf("want a failure")
		}
		if fail.status != 502 {
			t.Fatalf("status: %d", fail.status)
		}
		if fail.Error() != `the release tarball carries no "ocserverd" member — nothing was changed` {
			t.Fatalf("message: %q", fail.Error())
		}
		if got := upgradeTestDirEntries(t, dir); !reflect.DeepEqual(got, []string{"release.tar.gz"}) {
			t.Fatalf("staging directory: %v", got)
		}
	})

	t.Run("an asset that is not a gzip tarball refuses with 502 and stages nothing", func(t *testing.T) {
		dir := t.TempDir()
		tarPath := filepath.Join(dir, "release.tar.gz")
		if err := os.WriteFile(tarPath, []byte("this is not a tarball"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}

		path, fail := extractServerBinary(tarPath, dir)

		if path != "" {
			t.Fatalf("path: %q", path)
		}
		if fail == nil {
			t.Fatalf("want a failure")
		}
		if fail.status != 502 {
			t.Fatalf("status: %d", fail.status)
		}
		if fail.Error() != "the downloaded asset is not a gzip tarball — nothing was changed: gzip: invalid header" {
			t.Fatalf("message: %q", fail.Error())
		}
		if got := upgradeTestDirEntries(t, dir); !reflect.DeepEqual(got, []string{"release.tar.gz"}) {
			t.Fatalf("staging directory: %v", got)
		}
	})

	t.Run("a tarball that is not on disk refuses with 500", func(t *testing.T) {
		dir := t.TempDir()
		missing := filepath.Join(dir, "release.tar.gz")

		path, fail := extractServerBinary(missing, dir)

		if path != "" {
			t.Fatalf("path: %q", path)
		}
		if fail == nil {
			t.Fatalf("want a failure")
		}
		if fail.status != 500 {
			t.Fatalf("status: %d", fail.status)
		}
		if fail.Error() != "cannot reopen the downloaded tarball — nothing was changed: open "+missing+": no such file or directory" {
			t.Fatalf("message: %q", fail.Error())
		}
	})
}

func TestSmokeTestBinary(t *testing.T) {
	t.Run("a candidate whose --help exits 0 passes", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ocserverd")
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write: %v", err)
		}

		if fail := smokeTestBinary(path); fail != nil {
			t.Fatalf("want no failure, got %d %q", fail.status, fail.message)
		}
	})

	t.Run("a candidate whose --help exits non-zero is refused with 502", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ocserverd")
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
			t.Fatalf("write: %v", err)
		}

		fail := smokeTestBinary(path)

		if fail == nil {
			t.Fatalf("want a failure")
		}
		if fail.status != 502 {
			t.Fatalf("status: %d", fail.status)
		}
		if fail.Error() != "the downloaded binary failed its --help smoke test — refusing to install it; nothing was changed: exit status 3" {
			t.Fatalf("message: %q", fail.Error())
		}
	})

	t.Run("a candidate that cannot start on this machine is refused with 502", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ocserverd")
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}

		fail := smokeTestBinary(path)

		if fail == nil {
			t.Fatalf("want a failure")
		}
		if fail.status != 502 {
			t.Fatalf("status: %d", fail.status)
		}
		if fail.Error() != "the downloaded binary cannot start on this machine (wrong platform build?) — nothing was changed: fork/exec "+path+": permission denied" {
			t.Fatalf("message: %q", fail.Error())
		}
	})
}

func TestExecuteUpgrade(t *testing.T) {
	t.Run("verifies, smoke-tests, backs up, and atomically swaps the running binary", func(t *testing.T) {
		srv := upgradeTestReleaseServer(t, "v1.2.3", "#!/bin/sh\nexit 0\n")
		dir := t.TempDir()
		exe := filepath.Join(dir, "ocserverd")
		if err := os.WriteFile(exe, []byte("running binary"), 0o755); err != nil {
			t.Fatalf("write old binary: %v", err)
		}
		api := &apiServer{releaseAPIBase: srv.URL, upgradeExeOverride: exe}

		version, fail := api.executeUpgrade()

		if fail != nil {
			t.Fatalf("want no failure, got %d %q", fail.status, fail.message)
		}
		if version != "v1.2.3" {
			t.Fatalf("version: %q", version)
		}
		newBinary, err := os.ReadFile(exe)
		if err != nil {
			t.Fatalf("read new binary: %v", err)
		}
		if string(newBinary) != "#!/bin/sh\nexit 0\n" {
			t.Fatalf("new binary: %q", newBinary)
		}
		backup, err := os.ReadFile(exe + ".bak")
		if err != nil {
			t.Fatalf("read backup: %v", err)
		}
		if string(backup) != "running binary" {
			t.Fatalf("backup: %q", backup)
		}
		if got := upgradeTestDirEntries(t, dir); !reflect.DeepEqual(got, []string{"ocserverd", "ocserverd.bak"}) {
			t.Fatalf("binary directory: %v", got)
		}
	})

	t.Run("a pinned release that is not newer leaves the running binary alone", func(t *testing.T) {
		srv := upgradeTestReleaseServer(t, "v0.0.0", "#!/bin/sh\nexit 0\n")
		dir := t.TempDir()
		exe := filepath.Join(dir, "ocserverd")
		if err := os.WriteFile(exe, []byte("running binary"), 0o755); err != nil {
			t.Fatalf("write old binary: %v", err)
		}
		api := &apiServer{releaseAPIBase: srv.URL, upgradeExeOverride: exe}

		version, fail := api.executeUpgrade()

		if version != "" {
			t.Fatalf("version: %q", version)
		}
		if fail == nil || fail.status != http.StatusConflict || fail.Error() != "GitHub's current latest (v0.0.0) is not newer than the running build (0.0.0) — nothing newer to install" {
			t.Fatalf("failure: %#v", fail)
		}
		if got := upgradeTestDirEntries(t, dir); !reflect.DeepEqual(got, []string{"ocserverd"}) {
			t.Fatalf("binary directory: %v", got)
		}
	})
}

func TestRestartIntoUpgradedBinary(t *testing.T) {
	if os.Getenv("UPGRADE_RESTART_HELPER") == "1" {
		restartIntoUpgradedBinary(os.Getenv("UPGRADE_RESTART_TARGET"))
		return
	}

	dir := t.TempDir()
	marker := filepath.Join(dir, "restart.txt")
	target := filepath.Join(dir, "replacement.sh")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nprintf '%s' \"$*\" > \"$UPGRADE_RESTART_MARKER\"\n"), 0o755); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRestartIntoUpgradedBinary$", "-test.v")
	cmd.Env = append(os.Environ(),
		"UPGRADE_RESTART_HELPER=1",
		"UPGRADE_RESTART_TARGET="+target,
		"UPGRADE_RESTART_MARKER="+marker,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restart helper: %v\n%s", err, output)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read restart marker: %v", err)
	}
	if !strings.Contains(string(data), "-test.run=^TestRestartIntoUpgradedBinary$") {
		t.Fatalf("re-exec arguments: %q", data)
	}
}

func TestRunUpgrade(t *testing.T) {
	srv := upgradeTestReleaseServer(t, "v1.2.3", "#!/bin/sh\nexit 0\n")
	dir := t.TempDir()
	exe := filepath.Join(dir, "ocserverd")
	if err := os.WriteFile(exe, []byte("running binary"), 0o755); err != nil {
		t.Fatalf("write old binary: %v", err)
	}
	api := &apiServer{releaseAPIBase: srv.URL, upgradeExeOverride: exe}
	upgradeTestKnownRelease(t, api, "v1.2.3")

	version, path, fail := api.runUpgrade()

	if fail != nil {
		t.Fatalf("want no failure, got %d %q", fail.status, fail.message)
	}
	if version != "v1.2.3" || path != exe {
		t.Fatalf("result: version=%q path=%q", version, path)
	}
}

func TestScheduleUpgradeRestart(t *testing.T) {
	called := make(chan string, 1)
	api := &apiServer{}
	api.upgradeRestart = func(path string) {
		if !api.stationShuttingDown.Load() {
			t.Errorf("restart seam ran before shutdown marker was set")
		}
		called <- path
	}

	api.scheduleUpgradeRestart("/tmp/ocserverd")
	select {
	case got := <-called:
		if got != "/tmp/ocserverd" {
			t.Fatalf("restart path: %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("restart seam was not called")
	}
	if api.stationShuttingDown.Load() {
		t.Fatal("shutdown marker was not cleared after restart seam returned")
	}
}

func TestHandleUpgradeApiUpdateUpgradePost(t *testing.T) {
	t.Run("a trigger with no newer release known answers 409", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/update/upgrade", owner, "")

		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"no newer release is known — the running build is the latest published on GitHub (use 檢查更新 to re-check)")
		dashboard.wantFrames()
	})

	t.Run("a trigger while an upgrade is already running answers 409", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		upgradeTestKnownRelease(t, api, "v9.9.9")
		api.upgradeMu.Lock()
		defer api.upgradeMu.Unlock()
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/update/upgrade", owner, "")

		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "an upgrade is already in progress")
		dashboard.wantFrames()
	})

	t.Run("a trigger that cannot reach GitHub answers 502 and leaves the binary alone", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dir := t.TempDir()
		exe := filepath.Join(dir, "ocserverd")
		if err := os.WriteFile(exe, []byte("running binary"), 0o755); err != nil {
			t.Fatalf("write: %v", err)
		}
		api.upgradeExeOverride = exe
		api.releaseAPIBase = "http://127.0.0.1:1"
		upgradeTestKnownRelease(t, api, "v9.9.9")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/update/upgrade", owner, "")

		if status != 502 {
			t.Fatalf("want 502, got %d (%v)", status, data)
		}
		apiWantError(t, data, "internal_error",
			"cannot reach GitHub to pin the release — nothing was changed: "+
				`Get "http://127.0.0.1:1/repos/pkyosx/OffiCraft/releases?per_page=20": `+
				"dial tcp 127.0.0.1:1: connect: connection refused")
		if got := upgradeTestDirEntries(t, dir); !reflect.DeepEqual(got, []string{"ocserverd"}) {
			t.Fatalf("binary directory: %v", got)
		}
		data0, err := os.ReadFile(exe)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(data0) != "running binary" {
			t.Fatalf("binary: %q", data0)
		}
		dashboard.wantFrames()
	})

	t.Run("a well-formed POST /api/update/upgrade answers 200 after the swap lands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		srv := upgradeTestReleaseServer(t, "v1.2.3", "#!/bin/sh\nexit 0\n")
		dir := t.TempDir()
		exe := filepath.Join(dir, "ocserverd")
		if err := os.WriteFile(exe, []byte("running binary"), 0o755); err != nil {
			t.Fatalf("write old binary: %v", err)
		}
		api.releaseAPIBase = srv.URL
		api.upgradeExeOverride = exe
		upgradeTestKnownRelease(t, api, "v1.2.3")
		restarted := make(chan string, 1)
		api.upgradeRestart = func(path string) { restarted <- path }
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/update/upgrade", owner, "")

		if status != http.StatusOK {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"status":         "restarting",
			"target_version": "v1.2.3",
		})
		select {
		case got := <-restarted:
			if got != exe {
				t.Fatalf("restart path: %q", got)
			}
		case <-time.After(time.Second):
			t.Fatal("restart seam was not called")
		}
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)
		status, data := apiJSON(t, h, "POST", "/api/update/upgrade", "", "")
		if status != http.StatusUnauthorized {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("a plain authenticated agent answers 403", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		status, data := apiJSON(t, h, "POST", "/api/update/upgrade", agent, "")
		if status != http.StatusForbidden {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("the POST path reaches the upgrade handler and returns its domain conflict", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		status, data := apiJSON(t, h, "POST", "/api/update/upgrade", owner, "")
		if status != http.StatusConflict {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"no newer release is known — the running build is the latest published on GitHub (use 檢查更新 to re-check)")
	})

	t.Run("an ignored request body does not change the latest-release conflict", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/update/upgrade", owner, "{{{")
		if status != http.StatusConflict {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"no newer release is known — the running build is the latest published on GitHub (use 檢查更新 to re-check)")
	})
}
