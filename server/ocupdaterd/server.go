package main

// server.go — the HTTP surface + serve assembly.
//
// API face (dual beta/GA channels landed 2026-07-14 — publish lands in beta,
// promote stamps GA; the deployment-location design card is still open, the
// version-format and invite-code cards are CLOSED):
//
//	POST /api/publish            Bearer publish-token — multipart binary + metadata
//	                             (version optional: server generates vYYMMDD-NNNN);
//	                             the new release is BETA-channel only
//	POST /api/promote            Bearer publish-token — {"version": ...} stamps that
//	                             release GA (idempotent; promote order = GA order)
//	GET  /api/latest?channel=    Bearer invite-code   — newest version's metadata on
//	                             one channel (ga | beta; DEFAULT ga — a client that
//	                             says nothing gets the stable channel)
//	GET  /api/binary?version=    Bearer invite-code   — download that exact version
//	                             (any channel: channel only decides who "latest" is)
//	GET  /install.sh?code=&channel=  invite-code in query — dynamic install script
//	                             pinned to that channel's latest (default ga)
//
// plus the management PORTAL (portal.go): GET /portal/ serves the embedded
// single-file web UI, /portal/api/* is its JSON face (public first-run
// set-password + login; everything else gated by a portal session token).
// /api/latest doubles as the fleet heartbeat: the invite row records the
// check time + the client's self-reported ?current_version=/?current_sha=.
//
// Deliberately a plain stdlib mux, NOT ocserverd's RouteSpec/OpenAPI plumbing:
// this daemon is outside the frozen spec/openapi.json wire and outside
// conformance — five routes do not need a table + boot assertions yet.
//
// Error shape reuses the repo-wide envelope: {"error":{"code","message"}}.

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// maxBinaryBytes caps one uploaded binary. The real artifacts run ~25MB
// (ocserverd with embeds); 256MB leaves headroom without letting a bad client
// fill the disk in one request.
const maxBinaryBytes = 256 << 20

type updaterServer struct {
	store *Store
}

func newHandler(store *Store) http.Handler {
	s := &updaterServer{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/publish", s.handlePublish)
	mux.HandleFunc("POST /api/promote", s.handlePromote)
	mux.HandleFunc("GET /api/latest", s.handleLatest)
	mux.HandleFunc("GET /api/binary", s.handleBinary)
	mux.HandleFunc("GET /install.sh", s.handleInstallScript)
	// The management portal (portal.go): a browser UI + its /portal/api/*
	// face, session-gated behind the portal password.
	s.registerPortal(mux)
	return mux
}

// ── response writers (repo-wide error envelope) ──────────────────────────────

func writeJSON(w http.ResponseWriter, status int, body any) {
	raw, err := json.Marshal(body)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

func errorCodeForStatus(status int) string {
	switch status {
	case 400, 422:
		return "validation_error"
	case 401:
		return "unauthorized"
	case 404:
		return "not_found"
	case 409:
		return "conflict"
	}
	if status >= 500 {
		return "internal_error"
	}
	return "client_error"
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]map[string]string{
		"error": {"code": errorCodeForStatus(status), "message": message},
	})
}

// ── auth ─────────────────────────────────────────────────────────────────────

// bearerToken pulls `Authorization: Bearer <credential>` (scheme
// case-insensitive; a bare scheme-less value is tolerated, mirroring
// ocserverd's extractToken).
func bearerToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return ""
	}
	if scheme, rest, found := strings.Cut(header, " "); found && strings.EqualFold(scheme, "bearer") {
		return strings.TrimSpace(rest)
	}
	return header
}

// authenticate resolves the presented credential against the store, requiring
// the given kind. On failure it has already written the 401 and returns nil.
// Unknown / revoked / wrong-kind are ONE indistinguishable "no".
func (s *updaterServer) authenticate(w http.ResponseWriter, r *http.Request, kind, presented string) *Credential {
	if presented == "" {
		writeError(w, http.StatusUnauthorized, "missing credentials")
		return nil
	}
	cred, err := s.store.LiveCredentialByHash(hashSecret(presented), kind)
	if err != nil {
		internalError(w, err)
		return nil
	}
	if cred == nil {
		if kind == kindPublish {
			writeError(w, http.StatusUnauthorized, "this publish token is not valid (unknown or revoked) — mint one on the updater host: ocupdaterd mint-publish-token")
		} else {
			writeError(w, http.StatusUnauthorized, "this invite code is not valid (unknown or revoked) — ask the operator for a fresh invite")
		}
		return nil
	}
	return cred
}

func internalError(w http.ResponseWriter, err error) {
	writeError(w, http.StatusInternalServerError, "internal error: "+err.Error())
}

// ── handlers ─────────────────────────────────────────────────────────────────

// handlePublish — POST /api/publish (Bearer publish-token). Multipart form:
// file field "binary" + fields sha256 (required — the uploader's own digest;
// the server recomputes and REFUSES a mismatch, so a corrupt upload can never
// become the latest release), version (OPTIONAL — omitted/empty means the
// server generates the next date-form vYYMMDD-NNNN and returns it in the 201
// body; a supplied one must match that shape, see version.go), git_sha, notes.
func (s *updaterServer) handlePublish(w http.ResponseWriter, r *http.Request) {
	if s.authenticate(w, r, kindPublish, bearerToken(r)) == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBinaryBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "cannot read the multipart upload: "+err.Error())
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	version := strings.TrimSpace(r.FormValue("version"))
	claimedSHA := strings.ToLower(strings.TrimSpace(r.FormValue("sha256")))
	gitSHA := strings.TrimSpace(r.FormValue("git_sha"))
	notes := r.FormValue("notes")

	if version != "" && !isValidVersion(version) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"version %q does not match the date-form shape vYYMMDD-NNNN (e.g. %s0001) — or just omit the version field and the server will generate the next one",
			version, versionPrefix(time.Now())))
		return
	}
	if len(claimedSHA) != 64 || !isLowerHex(claimedSHA) {
		writeError(w, http.StatusBadRequest, "sha256 is required and must be the 64-hex-char digest of the uploaded binary (compute it with: shasum -a 256 <file>)")
		return
	}
	if version != "" {
		if existing, err := s.store.ReleaseByVersion(version); err != nil {
			internalError(w, err)
			return
		} else if existing != nil {
			writeError(w, http.StatusConflict, fmt.Sprintf("version %q is already published — pick a new version string (published versions are immutable)", version))
			return
		}
	}

	file, _, err := r.FormFile("binary")
	if err != nil {
		writeError(w, http.StatusBadRequest, `the multipart upload needs a file field named "binary" (the artifact itself)`)
		return
	}
	defer file.Close()

	// Stream to a temp file in blobs/ while hashing; only a digest-verified
	// upload is renamed into its content-addressed home.
	tmp, err := os.CreateTemp(s.store.blobsDir(), ".upload-*")
	if err != nil {
		internalError(w, err)
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op after the success rename
	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, hasher), file)
	closeErr := tmp.Close()
	if err != nil || closeErr != nil {
		internalError(w, errors.Join(err, closeErr))
		return
	}
	actualSHA := hex.EncodeToString(hasher.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(actualSHA), []byte(claimedSHA)) != 1 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"sha256 mismatch: you declared %s but the uploaded bytes hash to %s — the upload is corrupt or the digest is stale; nothing was published, re-upload",
			claimedSHA, actualSHA))
		return
	}
	blobPath := filepath.Join(s.store.blobsDir(), actualSHA)
	if err := os.Rename(tmpPath, blobPath); err != nil {
		internalError(w, err)
		return
	}
	rel, err := s.insertRelease(Release{
		Version:  version, // "" = server-generated (vYYMMDD-NNNN)
		GitSHA:   gitSHA,
		SHA256:   actualSHA,
		Size:     size,
		Notes:    notes,
		BlobPath: blobPath,
	})
	if IsUniqueVersionErr(err) {
		// Only reachable on a client-chosen version: the pre-check above raced
		// another publish of the same string. Same answer as the pre-check.
		writeError(w, http.StatusConflict, fmt.Sprintf("version %q is already published — pick a new version string (published versions are immutable)", version))
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, releaseDTO(rel))
}

// insertRelease records the release row, generating the version when the
// publisher did not choose one: next per-day serial under today's prefix
// (vYYMMDD-NNNN, version.go). Allocation races (two publishes reading the
// same MAX) surface as the version UNIQUE constraint and are retried with a
// fresh serial; a client-chosen version passes straight through and keeps its
// UNIQUE failure for the caller (409).
func (s *updaterServer) insertRelease(rel Release) (Release, error) {
	if rel.Version != "" {
		return s.store.InsertRelease(rel)
	}
	const maxAttempts = 5
	for attempt := 1; ; attempt++ {
		prefix := versionPrefix(time.Now())
		serial, err := s.store.MaxDailySerial(prefix)
		if err != nil {
			return rel, err
		}
		if serial >= 9999 {
			return rel, fmt.Errorf("the daily version serial space is exhausted (%s9999 already published today)", prefix)
		}
		rel.Version = fmt.Sprintf("%s%04d", prefix, serial+1)
		stored, err := s.store.InsertRelease(rel)
		if err == nil {
			return stored, nil
		}
		if !IsUniqueVersionErr(err) || attempt >= maxAttempts {
			return rel, err
		}
		rel.Version = "" // lost the allocation race — re-derive and retry
	}
}

// handlePromote — POST /api/promote (Bearer publish-token): stamp one
// published version GA. Body: {"version": "vYYMMDD-NNNN"}. Idempotent — an
// already-GA version answers 200 with its original stamp (promote order is
// never rewritten); an unknown version is a 404. There is no demote: a bad GA
// is fixed FORWARD by promoting a newer version.
func (s *updaterServer) handlePromote(w http.ResponseWriter, r *http.Request) {
	if s.authenticate(w, r, kindPublish, bearerToken(r)) == nil {
		return
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, `the promote body must be JSON like {"version":"v260713-0002"}: `+err.Error())
		return
	}
	version := strings.TrimSpace(body.Version)
	if version == "" {
		writeError(w, http.StatusBadRequest, `the promote body needs a version, e.g. {"version":"v260713-0002"} (discover published versions with list-versions on the updater host)`)
		return
	}
	rel, err := s.store.PromoteRelease(version)
	if err != nil {
		internalError(w, err)
		return
	}
	if rel == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("version %q is not published here — publish it first, then promote", version))
		return
	}
	writeJSON(w, http.StatusOK, releaseDTO(*rel))
}

// channelParam reads ?channel= with GA as the deliberate default (owner
// decision D1, 2026-07-14): a client that says nothing — including every
// pre-channel ocserverd in the field — lands on the STABLE channel. ok=false
// means the 400 is already written.
func channelParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	switch r.URL.Query().Get("channel") {
	case "", channelGA:
		return channelGA, true
	case channelBeta:
		return channelBeta, true
	default:
		writeError(w, http.StatusBadRequest, `channel must be "ga" or "beta" (omit it for ga)`)
		return "", false
	}
}

// noLatestMessage is the honest empty-channel 404 body per channel.
func noLatestMessage(channel string) string {
	if channel == channelGA {
		return "no GA version exists yet — publish lands in beta; promote one to GA first (POST /api/promote)"
	}
	return "no version has been published yet"
}

// handleLatest — GET /api/latest?channel=ga|beta (Bearer invite-code): the
// newest version's metadata on that channel (default ga).
//
// Fleet monitoring (portal): the check is also a HEARTBEAT — the invite row
// records when this code last checked and what version/sha the client
// self-reported via the OPTIONAL ?current_version= / ?current_sha= params
// (ocserverd's update_check.go sends both; a client that sends neither still
// stamps the check time). Recording is best-effort: a failed stamp must never
// break the update check itself, so the error only reaches the process log.
func (s *updaterServer) handleLatest(w http.ResponseWriter, r *http.Request) {
	cred := s.authenticate(w, r, kindInvite, bearerToken(r))
	if cred == nil {
		return
	}
	channel, ok := channelParam(w, r)
	if !ok {
		return
	}
	currentSHA := strings.TrimSpace(r.URL.Query().Get("current_sha"))
	if err := s.store.RecordInviteCheck(cred.ID,
		strings.TrimSpace(r.URL.Query().Get("current_version")),
		currentSHA,
		channel); err != nil {
		log.Printf("[ocupdaterd] fleet record for invite %d failed: %v", cred.ID, err)
	}
	rel, err := s.store.LatestRelease(channel)
	if err != nil {
		internalError(w, err)
		return
	}
	if rel == nil {
		writeError(w, http.StatusNotFound, noLatestMessage(channel))
		return
	}
	dto := releaseDTO(*rel)
	// Self-lookup (T-e9d1): tell the caller what r-N its OWN running build is,
	// resolved from the self-reported current_sha. This is how a downstream
	// server names its running version in the cockpit ("r-N") without the
	// serial ever being knowable at its build time. Empty when the caller sent
	// no sha, or its build was never published here — the client then falls
	// back to showing the git sha alone (honest "unknown release").
	if currentSHA != "" {
		if mine, err := s.store.ReleaseBySHA(currentSHA); err != nil {
			internalError(w, err)
			return
		} else if mine != nil {
			dto["current_release_tag"] = releaseTag(mine.Serial)
			dto["current_serial"] = mine.Serial
		}
	}
	writeJSON(w, http.StatusOK, dto)
}

// handleBinary — GET /api/binary?version=<v> (Bearer invite-code): stream that
// version's bytes.
func (s *updaterServer) handleBinary(w http.ResponseWriter, r *http.Request) {
	if s.authenticate(w, r, kindInvite, bearerToken(r)) == nil {
		return
	}
	version := r.URL.Query().Get("version")
	if version == "" {
		writeError(w, http.StatusBadRequest, "the version query param is required, e.g. /api/binary?version=<v> (discover the newest via GET /api/latest)")
		return
	}
	rel, err := s.store.ReleaseByVersion(version)
	if err != nil {
		internalError(w, err)
		return
	}
	if rel == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("version %q is not published here", version))
		return
	}
	f, err := os.Open(rel.BlobPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the version's binary is missing from the blob store — the updater host needs attention: "+err.Error())
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", rel.Size))
	w.Header().Set("X-Checksum-Sha256", rel.SHA256)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

func releaseDTO(r Release) map[string]any {
	var gaAt any // null while beta-only — never a fabricated 0
	if r.GAAt != nil {
		gaAt = *r.GAAt
	}
	return map[string]any{
		"version": r.Version,
		// The pure monotonic serial (T-e9d1): serial is the raw number,
		// release_tag its human-facing "r-N" render. Additive alongside version
		// (which stays the immutable download key) — an older client that reads
		// only version is unaffected.
		"serial":       r.Serial,
		"release_tag":  releaseTag(r.Serial),
		"git_sha":      r.GitSHA,
		"sha256":       r.SHA256,
		"size":         r.Size,
		"notes":        r.Notes,
		"published_at": r.PublishedAt,
		// Dual-channel face: channel_ga answers "is this release GA?" in one
		// bool; ga_at carries the promote instant (null = beta-only).
		"channel_ga": r.GAAt != nil,
		"ga_at":      gaAt,
	}
}

func isLowerHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ── serve ────────────────────────────────────────────────────────────────────

// cmdServe is the zero-argument canonical start: read oc-updater.toml, open
// the store, assemble the handler, bind loopback:[server].port. Bind FIRST,
// announce second — "serving on" is only printed while actually holding the
// listener (the ocserverd lesson: a log line that claims success before the
// bind is a lie whenever the port is taken).
func cmdServe(env func(string) string, out io.Writer) int {
	cfg, err := loadConfig(configPath(env))
	if err != nil {
		fmt.Fprintf(out, "[ocupdaterd] FATAL: %v\n", err)
		return 1
	}
	store, err := openStore(cfg.DataDir)
	if err != nil {
		fmt.Fprintf(out, "[ocupdaterd] FATAL: %v\n", err)
		return 1
	}
	defer store.Close()
	handler := newHandler(store)
	addr := fmt.Sprintf("%s:%d", defaultHost, cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(out, "[ocupdaterd] FATAL: %s\n", bindErrorMessage(cfg.Port, err))
		return 1
	}
	fmt.Fprintf(out, "ocupdaterd serving on http://%s (data: %s)\n", addr, cfg.DataDir)
	// First-run portal claim (the ocserverd pattern): while no portal password
	// is set, mint/keep a one-shot claim token and print the setup URL to the
	// LOCAL log only — the set-password endpoint requires it, so a mere network
	// visitor can never claim an unclaimed portal.
	if setupURL, err := portalSetupURL(store, addr); err != nil {
		fmt.Fprintf(out, "[ocupdaterd] WARN: portal first-run claim: %v\n", err)
	} else if setupURL != "" {
		fmt.Fprintf(out, "portal password not set — claim it here (one-shot): %s\n", setupURL)
	}
	if err := http.Serve(ln, handler); err != nil {
		fmt.Fprintf(out, "[ocupdaterd] FATAL: %v\n", err)
		return 1
	}
	return 0
}

// bindErrorMessage turns a net.Listen failure into something the operator can
// act on (same posture as ocserverd: fail loud, never silently bind elsewhere
// — every installed one-liner hardwires this base URL).
func bindErrorMessage(port int, err error) string {
	if errors.Is(err, syscall.EADDRINUSE) {
		return fmt.Sprintf(
			"port %d already in use — another process holds it. Free it, or move this instance: set [server].port in oc-updater.toml. Find the holder with: lsof -nP -iTCP:%d -sTCP:LISTEN",
			port, port)
	}
	return fmt.Sprintf("cannot bind port %d: %v", port, err)
}
