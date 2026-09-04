package main

// server.go — app assembly (the Go twin of the retired Python service/app.py create_app +
// serve): run the fail-closed boot assertions FIRST, then register every
// RouteSpec row onto the mux with its auth + RBAC dependencies attached, then
// bind loopback:[server].port (port from oc.toml; the host is hardwired).
//
// Two dependencies per gated row, in order (register_routes contract): the JWT
// gate (401 deny-by-default), then — when the row's Requires names a class
// ABOVE the "machine" floor — the single principal choke (403 below the
// declared minimum). Public rows attach neither.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// appVersion is the single app version string. A `var` (not const) so bin/build
// can stamp it at link time (-X main.appVersion=…), same mechanism as buildSHA;
// an unstamped build (plain `go build`, self-build, dev) keeps the honest
// "0.0.0" placeholder. An OFFICIAL package (bin/release) stamps this to the
// GitHub Release tag, so /api/version's `version` self-identifies the release
// — the single human-facing version identity (release_check.go compares it
// against the newest GitHub Release tag).
var appVersion = "0.0.0"

// ── build identity (handlers.git_sha / git_time; captured once at boot) ─────

// buildSHA / buildTime are the LINK-TIME build identity, stamped by bin/build
// (-ldflags "-X main.buildSHA=<short sha> -X main.buildTime=<%cI>") onto the
// single-file deploy artifact. When stamped they WIN over the CWD git probe:
// a repo-less standalone binary has no checkout to probe, and even inside a
// checkout the running code's identity is the binary's own build, not
// whatever HEAD the CWD happens to sit on. Empty (a plain `go build`, e.g.
// the committed prebuilt) falls back to the probe.
var (
	buildSHA  string
	buildTime string
)

// gitSHA returns the stamped build sha, else the current short (7-char) git
// sha of the CWD checkout, else "unknown". Best-effort; never fails the boot.
func gitSHA() string {
	if buildSHA != "" {
		return buildSHA
	}
	out, err := gitOutput("rev-parse", "--short", "HEAD")
	if err != nil || out == "" {
		return "unknown"
	}
	return out
}

// gitTime returns the stamped build commit time, else the committer date of
// HEAD (strict ISO-8601), or "" when unavailable — the caller serialises ""
// as null, never a fabricated time.
func gitTime() string {
	if buildTime != "" {
		return buildTime
	}
	out, err := gitOutput("show", "-s", "--format=%cI", "HEAD")
	if err != nil {
		return ""
	}
	return out
}

func gitOutput(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	return strings.TrimSpace(string(out)), err
}

// ── DTOs (field ORDER mirrors service/dto.py so the JSON bytes align) ────────
//
// These three are HAND-WRITTEN besides the generated ocapi_gen.go types on
// purpose: the generated structs marshal alphabetically-sorted optional fields
// with omitempty, while the probe contract is byte-level field ORDER parity
// with the Python DTOs (null serialised, never omitted). The generated types
// are the sub-batch B request/response vocabulary; these lock the probes.

type healthDTO struct {
	Status string `json:"status"`
}

type versionDTO struct {
	Version         string  `json:"version"`
	GitSHA          string  `json:"git_sha"`
	GitTime         *string `json:"git_time"` // null when unavailable
	CatalogHash     string  `json:"catalog_hash"`
	UpdateAvailable bool    `json:"update_available"`
	LatestVersion   *string `json:"latest_version"` // only meaningful when UpdateAvailable
	// UpdateCheckedOKAt is when the update check last SUCCEEDED (RFC3339,
	// UTC) — the freshness of UpdateAvailable, added because `false` alone
	// cannot distinguish "checked a minute ago, nothing newer" from "the
	// check has never once succeeded". OMITTED (not null) when no check has
	// ever succeeded, so a station that has never checked serves the exact
	// bytes it served before this field existed.
	UpdateCheckedOKAt *string `json:"update_checked_ok_at,omitempty"`
}

// probeVersionDTO is the bare `/version` deploy-probe shape (autodeploy reads
// `sha` to compare) — service/dto.py ProbeVersionDTO.
type probeVersionDTO struct {
	Version     string `json:"version"`
	SHA         string `json:"sha"`
	CatalogHash string `json:"catalog_hash"`
}

// ── response writers (unified error envelope; docs/design/api-error-envelope.md)

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

// errorCodeForStatus is the status → machine-readable code map
// (service.errors.CODE_BY_STATUS + the honest fallback buckets).
func errorCodeForStatus(status int) string {
	switch status {
	case 400, 422:
		return "validation_error"
	case 401:
		return "unauthorized"
	case 403:
		return "forbidden"
	case 404:
		return "not_found"
	case 405:
		return "method_not_allowed"
	case 409:
		return "conflict"
	case 503:
		return "service_unavailable"
	}
	if status >= 500 {
		return "internal_error"
	}
	return "client_error"
}

// writeError answers the ONE non-2xx wire shape every Python route already
// speaks: {"error":{"code":"...","message":"..."}}.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]map[string]string{
		"error": {"code": errorCodeForStatus(status), "message": message},
	})
}

// ── auth middleware (service/auth.py require_auth: the single verify path) ──

type contextKey string

const claimsContextKey contextKey = "ocserverd.claims"

// verifyingKeyContextKey carries WHICH ring key verified this request's
// credential (T-80). It is put here rather than acted on in the middleware
// because the two questions are answered in different places: only requireAuth
// can know which key matched, and only a handler can know whether this request
// is evidence of anything.
//
// 🔴 WHY THAT SEPARATION IS THE WHOLE POINT. The first shape of this feature
// recorded the key in the middleware, on every gated route — which is wrong in
// the one direction that matters, because a request is not proof that the
// machine is RUNNING on the credential it just presented. A warden renewing
// itself presents its CANDIDATE credential to /api/machines before writing it
// to disk (cli/ocwarden/renewapply.go: probe, then write, then exec), so the
// middleware recorded "this machine has moved to the new key" about a
// credential the machine might never adopt — and if the write then failed, the
// station said converged while the machine kept running on the old key. That is
// the number reading SAFE when it is not, on the one screen whose whole purpose
// is to gate an irreversible removal.
const verifyingKeyContextKey contextKey = "ocserverd.verifying_key_id"

func claimsFromContext(ctx context.Context) map[string]any {
	claims, _ := ctx.Value(claimsContextKey).(map[string]any)
	return claims
}

// verifyingKeyFromContext returns the id of the ring key that verified this
// request, or "" when there is none (an unauthenticated route, or a test that
// built the context by hand).
func verifyingKeyFromContext(ctx context.Context) string {
	id, _ := ctx.Value(verifyingKeyContextKey).(string)
	return id
}

// extractToken pulls the bearer token from the request — the byte-faithful
// twin of service/auth.py extract_token. `Authorization: Bearer <jwt>` is the
// canonical form (scheme case-insensitive; a bare scheme-less value is
// tolerated too); when NO Authorization header is present the identical token
// is also accepted as a `?token=` query param, because the SPA's SSE downlink
// (EventSource) and inline <img>/<a href> blob loads cannot set a header. A
// present-but-invalid header never falls through to the query param.
func extractToken(r *http.Request) string {
	if header := strings.TrimSpace(r.Header.Get("Authorization")); header != "" {
		scheme, rest, found := strings.Cut(header, " ")
		if found && strings.EqualFold(scheme, "bearer") {
			if token := strings.TrimSpace(rest); token != "" {
				return token
			}
		}
		return header
	}
	return r.URL.Query().Get(authTokenQueryParam)
}

// authTokenQueryParam is the header-less credential extractToken accepts above.
// Named here rather than typed at each use because it is not only read: GET
// /api/chat refuses query parameters it does not declare (unknownChatQueryParams)
// and has to know that this one is a credential rather than a typo — a second
// spelling of it there would deny every EventSource and <img src> that carries
// its token this way, and would deny them only in production.
const authTokenQueryParam = "token"

// requireAuth wraps a GATED handler with the JWT gate: the extracted token
// (header first, then the `?token=` query fallback — see extractToken)
// verified against the LIVE signing-key ring, claims stashed on the request
// context, 401 deny-by-default on anything else.
//
// 🔴 keys is the ring itself, not a key copied out of it (T-62). Every gated
// route closes over this same pointer, which is what makes a rotation take
// effect on the next request instead of at the next restart. A token verifies
// if ANY key still in the ring signed it; REMOVING a key is what revokes the
// tokens it signed.
//
// ownerIatFloor (nil = no cut) is the change-password revocation seam
// (lifecycle.md §1.3): an owner-scope token whose iat is EARLIER than the
// floor was minted before the last password change and is refused — the one
// stateful exception to stateless verification. Agent/warden tokens never
// consult it — they have their OWN floor, which is a different shape and lives
// on the member row (agentIatFloorRefusal below): T-14 項目 4B refuses an
// agent-scope token minted for a generation its member has already replaced.
// Warden credentials consult neither.
//
// lookup (nil = no cut) is the SECOND revocation seam, added here for T-9cf8
// and deliberately in the same place: a credential belonging to a machine the
// roster has deleted is refused (authz.go revocationRefusal). It sits AFTER
// signature verification — a forged token is still just "invalid token" and
// never reaches a roster read. It also binds every exp-less credential to an
// active warden row; no other signed JWT may become permanent.
//
// T-80: the id of the ring key that verified this request is put on the context
// (verifyingKeyContextKey) and NOT acted on here. requireAuth is the only place
// that can know which key matched; it is emphatically NOT the place that can
// know whether the request means the machine is RUNNING on that credential.
// See verifyingKeyContextKey for what recording it here got wrong, and
// HandleEventsApiEventsGet for where the answer is actually taken.
func requireAuth(keys *keyring, ownerIatFloor func() int64, lookup func(id string) (*Member, error), next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if keys == nil || len(keys.verifySecrets()) == 0 {
			writeError(w, http.StatusUnauthorized, "auth not configured")
			return
		}
		token := extractToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "missing credentials")
			return
		}
		claims, keyID, err := verifyJWTAnyKey(keys, token, time.Now().Unix())
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		if ownerIatFloor != nil {
			if scope, _ := claims["scope"].(string); scope == "owner" {
				iat, ok := claims["iat"].(float64) // encoding/json numbers land as float64
				if !ok || int64(iat) < ownerIatFloor() {
					writeError(w, http.StatusUnauthorized, "invalid token")
					return
				}
			}
		}
		// The AGENT-side twin of the floor above, and deliberately next to it —
		// same mechanism, different shape: ownerIatFloor is ONE global number
		// (the owner has no roster row), while an agent's floor is per member
		// and is read off that member's row through `lookup`. Warden rows are
		// exempt by name inside; see agentIatFloorRefusal for why that is a
		// safety property and not a shortcut.
		if agentIatFloorRefusal(claims, lookup) {
			// The refusal is AUTHORITATIVE and permanent: the floor only ever
			// rises, so this credential can never come back. Name it on the
			// response so the process still holding the old session's socket
			// can stop retrying and shut itself down, instead of reconnecting
			// every ≤15s forever while the cockpit shows the SUCCESSOR as the
			// live one. Body text is unchanged on purpose — see
			// authRefusalHeader in authz.go.
			w.Header().Set(authRefusalHeader, refusalAgentSuperseded)
			// And SAY SO in the log. This refusal ends a live session: the
			// process reading the marker kills its own tmux and the model
			// session under it. Every other way a member's session dies leaves
			// a trace on the station; without this line the owner sees a
			// member's tmux simply vanish with NOTHING in the server log, and
			// the only remaining evidence is a 401 status code indistinguishable
			// from the ordinary ones above. Nothing secret is written: the
			// subject is a member id and the iat is a timestamp — no token, no
			// signature, no claim body.
			sub, _ := claims["sub"].(string)
			iat, _ := claims["iat"].(float64)
			log.Printf("[auth] REFUSED %s: agent credential iat=%d is below its "+
				"member's agent_iat_floor — a newer session of this member has "+
				"reported waking, so this one is superseded. Marked %s: %s; the "+
				"process holding it should stop retrying and shut itself down.",
				sub, int64(iat), authRefusalHeader, refusalAgentSuperseded)
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		if permanentCredentialRefusal(claims, lookup) {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		if refusal := revocationRefusal(claims, lookup); refusal != "" {
			writeError(w, http.StatusUnauthorized, refusal)
			return
		}
		// T-80: hand the verifying key id down, do not act on it. Everything
		// above can still refuse, so a key id only reaches a handler on a
		// credential that was actually accepted — but "accepted" is still not
		// "the machine is running on it", which is why nothing is recorded here.
		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		ctx = context.WithValue(ctx, verifyingKeyContextKey, keyID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// shareSigGate is the third auth path on a ShareSig-flagged route: a bearer
// credential of any kind (header or ?token=) always takes the normal authed
// chain — a present-but-invalid token stays a 401 and NEVER falls through to
// the sig. Only a credential-less request may present ?sig=; a valid sig serves
// the RAW handler; a bad sig is 401; no sig at all falls to the authed chain's
// "missing credentials" 401.
//
// WHAT a valid sig means is the ROW's business, not this gate's: the row hands
// in the verifier (RouteSpec.ShareSig), which reads its subject out of the
// request and checks it against its OWN domain-separated key — the attachment
// blob GET signs its path's attachment_id, GET /api/diff signs both addresses
// and both labels. One gate, one precedence ladder, per-row subjects; every
// other row never consults sigs at all.
//
// The verifier is handed the RING, not a key: which key signed a given sig is
// not knowable from the request, so every verifier tries all of them
// (sharesig.go) and a removed key ends its sigs on both rows alike.
func shareSigGate(keys *keyring, verify shareSigVerifier, raw, authed http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if extractToken(r) != "" {
			authed.ServeHTTP(w, r)
			return
		}
		sig := r.URL.Query().Get("sig")
		if sig == "" {
			authed.ServeHTTP(w, r)
			return
		}
		if keys == nil || !verify(keys, r, sig) {
			writeError(w, http.StatusUnauthorized, "invalid signature")
			return
		}
		raw.ServeHTTP(w, r)
	})
}

// ── app assembly ─────────────────────────────────────────────────────────────

// buildAPIHandler is the PRODUCTION assembly seam: it is the only place that
// decides which key ring the gate gets, and it always answers api.keys.
//
// 🔴 IT EXISTS SO THAT DECISION IS TESTABLE. buildHandler takes a ring as a
// parameter because a handful of tests must hand it a DIFFERENT one (a nil ring
// is how auth_refusal_exits_t14_test.go reaches the "auth not configured"
// exit). That parameter is also how the gate and the mint could silently drift
// apart: hand buildHandler a ring that is not api.keys and every rotation moves
// the minting half while the verifying half stays behind — signed tokens the
// server itself refuses, and nothing in the tree would have gone red. Wrapping
// the one production call in a named function puts that line under test
// (keyring_rotation_t62_test.go) instead of leaving it as an argument nobody
// guards.
func buildAPIHandler(api *apiServer, lookup func(id string) (*Member, error)) (http.Handler, error) {
	return buildHandler(specsFor(api), api.keys, lookup, api.authPasswordChangedAt)
}

// buildHandler assembles the mux from the route table: boot assertions FIRST
// (fail closed — a bad table is an error, never a served app), then each row
// registered with its auth + RBAC chokes. Mirrors create_app + register_routes.
// lookup is the roster read the principal resolver classifies agent-scoped
// callers through (nil = token-only classification, the plumbing-test face).
func buildHandler(specs []RouteSpec, keys *keyring, lookup func(id string) (*Member, error), ownerIatFloor func() int64) (http.Handler, error) {
	if err := assertAllRoutesLabelled(specs); err != nil {
		return nil, err
	}
	// RBAC twin of the auth-label assertion: every row must also declare the
	// MINIMUM principal class it admits and agree with its auth label.
	if err := assertAllRoutesDeclareRequires(specs); err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	for _, spec := range specs {
		var h http.Handler = spec.Handler
		if spec.Auth == authGated {
			if spec.Requires != principalMachine {
				h = requirePrincipalClass(spec.Requires, lookup, h)
			}
			h = requireAuth(keys, ownerIatFloor, lookup, h)
			if spec.ShareSig != nil {
				h = shareSigGate(keys, spec.ShareSig, spec.Handler, h)
			}
		}
		mux.Handle(spec.Method+" "+spec.Path, h)
	}
	// The SPA / error fallback holds the bare "/" pattern — every table row
	// above is a more specific mux pattern and always wins (static.py
	// precedence, structural). See spa.go for the decision ladder.
	mux.Handle("/", newFallbackHandler(specs, webdistFS()))
	return mux, nil
}

// specsFor builds the route table over one apiServer through the generated
// ServerInterfaceWrapper (param binding; a param the wrapper cannot bind is
// the wire-frozen 422 through the unified envelope) and stamps the
// derived catalog hash back onto the server (the hash is over the table's own
// non-mcp_exclude rows).
func specsFor(s *apiServer) []RouteSpec {
	wrapper := &ServerInterfaceWrapper{
		Handler: s,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
		},
	}
	specs := routeSpecs(wrapper)
	s.catalogHash = catalogHashOf(specs)
	s.mcpTools = mcpToolIndex(specs)
	return specs
}

// newAPIServer assembles the handler carrier: build identity captured ONCE
// (at process start) so the probes report the sha of the RUNNING code — an
// autodeploy that pulls a new sha but fails to restart keeps reporting the
// OLD sha (handlers._PROCESS_SHA contract).
func newAPIServer(dal *DAL, hub *Hub, keys *keyring, tokenTTL int64, root assetRoot) *apiServer {
	// T-66a2 L3: bind the durable warden-command queue and rehydrate the FIFO
	// before anything can connect. Assembly is the RIGHT seam: an upgrade
	// re-execs this process, so "the queue survives a restart" is exactly "a
	// freshly assembled apiServer over the same store finds the frames again".
	// A DAL-less assembly (defaultRouteSpecs' route-shape probe) simply keeps
	// the pre-T-66a2 in-memory-only behaviour.
	//
	// KNOWN: this makes assembly a WRITE against the store (expiry sweep) and a
	// read that populates the new hub. Building a second apiServer over a DAL a
	// live one is already using therefore lands the same pending command in two
	// hubs. cmdServe assembles exactly once, so this is a constraint on future
	// callers rather than a live defect — see hub.BindWardenCommandStore.
	if dal != nil && hub != nil {
		hub.BindWardenCommandStore(dal)
	}
	return &apiServer{
		processSHA:                   gitSHA(),
		processTime:                  gitTime(),
		dal:                          dal,
		hub:                          hub,
		telemetry:                    newMemStore(),
		gauge:                        newMemStore(),
		machineClaims:                newMachineClaimStore(),
		keys:                         keys,
		ownerTokenTTL:                tokenTTL,
		agentTokenTTL:                defaultAgentTokenTTL,
		acceleratedGraceSecs:         acceleratedGraceSecsDefault,
		outsourceMaxParallel:         defaultOutsourceMaxParallel,
		docCapCharsDuty:              dutyCapCharsDefault,
		docCapCharsInsight:           contextDocMaxCharsDefault,
		docCapCharsLearning:          contextDocMaxCharsDefault,
		docCapCharsManualSop:         contextDocMaxCharsDefault,
		docCapCharsManualLearnings:   contextDocMaxCharsDefault,
		docCapCharsSystemInteraction: systemInteractionCapCharsDefault,
		docCapCharsBootSequence:      bootSequenceCapCharsDefault,
		docCapCharsOffboard:          offboardCapCharsDefault,
		chatBudgetChars:              chatBudgetCharsDefault,
		backupRetain:                 backupRetainDefault,
		ctxhigh:                      defaultSseContextHigh(),
		root:                         root,
		binHashes:                    bindistBinaryHashesFrom(bindistFS()),
		reconcileStates:              map[string]reconcileState{},
		reconcileCfg:                 defaultReconcileConfig(),
		identitySweepAt:              map[string]float64{},
		receiptPending:               map[string]pendingReceipt{},
		workerSpawnAt:                map[string]float64{},
		workerSpawnTarget:            map[string]string{},
		workerSpawnAttempts:          map[string]int{},
		workerReclaimed:              map[string]bool{},
		workerStopPending:            map[string]string{},
		workerStopLanded:             map[string]workerStopDispatch{},
		workerMachinePref:            map[string]string{},
		workerReconcileStates:        map[string]reconcileState{},
		workerMachineCooldown:        map[string]float64{},
		workerOfflineSince:           map[string]float64{},
	}
}

// defaultRouteSpecs is the dependency-free table view (route-shape tests +
// the SPA fallback's template list): the probes work, the business handlers
// would need the full newAPIServer wiring.
func defaultRouteSpecs() []RouteSpec {
	return specsFor(newAPIServer(nil, NewHub(), nil, defaultOwnerTokenTTL, "."))
}

// sseKeepAlive is the TCP keep-alive config applied to every accepted
// connection (T-7e07). WHY at the socket level: a long-lived SSE downlink whose
// peer silently vanishes (machine off, NAT/LB/CDN drops the flow — no FIN/RST)
// leaves the server writing 15-byte heartbeats that just land in the kernel
// send buffer and return success, so neither r.Context() nor a write deadline
// notices for a very long time (the old ~15 min OS-retransmit wedge that pinned
// a member permanently online / 409-on-reconnect). Keep-alive probes ride on
// TCP ACKs, independent of app reads/writes: a healthy peer's kernel ACKs the
// probe (never falsely reaped, even on an idle stream), while a vanished peer
// fails to ACK and the connection is closed after ~Idle+Interval*Count ≈ 30 s →
// r.Context() cancels → the handler returns → Disconnect → the stale listener's
// online projection drops → the member can reconnect. Cross-platform: Go 1.23+
// net.KeepAliveConfig maps to TCP_KEEPALIVE/KEEPINTVL/KEEPCNT on macOS (the
// prod fleet) and the matching options on Linux. Values are deliberately not
// aggressive — a shorter window risks reaping a healthy connection over
// transient jitter.
var sseKeepAlive = net.KeepAliveConfig{
	Enable:   true,
	Idle:     15 * time.Second,
	Interval: 5 * time.Second,
	Count:    3,
}

// keepAliveConn is the minimal surface applyKeepAlive needs — *net.TCPConn
// satisfies it; a test fake captures the config without touching a real socket.
type keepAliveConn interface {
	SetKeepAliveConfig(net.KeepAliveConfig) error
}

// applyKeepAlive arms sseKeepAlive on one accepted connection. Best-effort: a
// non-TCP conn (or one predating the API) simply skips it, never fails Accept.
func applyKeepAlive(c net.Conn) {
	if kc, ok := c.(keepAliveConn); ok {
		_ = kc.SetKeepAliveConfig(sseKeepAlive)
	}
}

// keepAliveListener wraps a net.Listener so every accepted connection gets
// sseKeepAlive. http.Serve(ln, …) (unlike ListenAndServe) sets no keep-alive of
// its own, so without this wrap the accepted SSE sockets have none. Keep-alive
// is invisible to short-lived request/response connections (they close before
// any probe fires), so no other endpoint's observable behaviour changes.
type keepAliveListener struct {
	net.Listener
}

func (l keepAliveListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	applyKeepAlive(c)
	return c, nil
}

const stationShutdownTimeout = 5 * time.Second

// cmdServe is the zero-argument canonical start (service.app.serve): read
// oc.toml, open + migrate + seed the store, load the DB settings snapshot
// (running the one-shot oc.toml → DB auth migration — settings.go), assemble
// the app (boot assertions fail closed), mount the reconcile producer cadence
// (unless --no-reconcile) and the outsource-assignment scheduler cadence
// (unless --no-outsource), bind host:port.
func cmdServe(env func(string) string, noReconcile, noOutsource bool, out io.Writer) int {
	cfg, dsn, rc := announceResolution("serve", env, out)
	if rc != 0 {
		return rc
	}
	dbPath, ok := sqliteFilePath(dsn)
	if !ok {
		fmt.Fprintf(out, "[ocserverd] FATAL: serve supports sqlite DSNs only for now (got %q)\n", dsn)
		return 1
	}
	db, err := openSQLite(dbPath)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: open %s: %v\n", dbPath, err)
		return 1
	}
	defer db.Close()
	// Backup trigger ③ (backup.go): snapshot BEFORE goose touches the schema.
	// Swapping the binary cannot hurt the data; a migration can, so this — not
	// the upgrade step — is the moment that needs a retreat point. Never fatal:
	// refusing to boot because a backup failed would trade an unlikely data
	// risk for a certain outage, and the log says so plainly.
	backupBeforeMigrations(db, dbPath, time.Now())
	if err := runMigrations(db); err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: goose up: %v\n", err)
		return 1
	}
	// T-dd7a: ask the FILE which journal mode it is actually in, now that
	// migrations have run against it. A malformed pragma is silently ignored by
	// SQLite, so the only symptom of a typo in openSQLite's DSN would be the
	// request queueing this ticket removed: correct data, no error, nothing to
	// notice. Deliberately NOT fatal — a slow studio beats one that will not boot
	// (the same trade the pre-migration backup hook makes).
	if mode, err := assertJournalMode(db, sqliteJournalMode); err != nil {
		fmt.Fprintf(out, "[ocserverd] WARNING: %v — every request will serialise at the database again, and that is this regression's ONLY symptom (T-dd7a)\n", err)
	} else {
		fmt.Fprintf(out, "[ocserverd] journal_mode=%s (reads do not queue behind each other)\n", mode)
	}
	// The read pool is opened AFTER the migration: `mode=ro` never creates a file
	// and a read-only connection cannot recover a WAL, so the write pool has to
	// have been here first.
	rdb, err := openSQLiteReadPool(dbPath)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: open read pool %s: %v\n", dbPath, err)
		return 1
	}
	defer rdb.Close()
	dal := NewDALPools(db, rdb)
	if err := seedOutOfBox(dal); err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: seed: %v\n", err)
		return 1
	}
	auth, err := loadAuthSettings(dal, cfg, func(msg string) {
		fmt.Fprintf(out, "[ocserverd] settings: %s\n", msg)
	})
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: load settings: %v\n", err)
		return 1
	}
	// The signing-key ring, loaded ONCE and then shared BY POINTER with the
	// handler tree below (T-62). auth.secret is this install's pre-ring key: it
	// seeds the ring the first time this binary boots and is thereafter just
	// the oldest key in it. Nothing downstream copies the key bytes out, which
	// is why a rotation reaches every signer and verifier without a restart.
	keys, err := loadKeyring(dal, auth.secret)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: load signing keys: %v\n", err)
		return 1
	}
	api := newAPIServer(dal, NewHub(), keys, auth.ownerTokenTTL, ".")
	api.agentTokenTTL = auth.agentTokenTTL
	api.passwordHash = auth.passwordHash
	api.passwordChangedAt = auth.passwordChangedAt
	api.mfaOffered = auth.mfaOffered
	api.totpSecret = auth.totpSecret
	api.totpLastStep = auth.totpLastStep
	api.ctxhigh = auth.ctxhigh
	api.codexCompactionThreshold = auth.codexCompactionThreshold
	api.codexNoticeRound = auth.codexNoticeRound
	api.monitoringRefreshSeconds = auth.monitoringRefreshSeconds
	api.acceleratedGraceSecs = auth.acceleratedGraceSecs
	api.outsourceMaxParallel = auth.outsourceMaxParallel
	api.docCapCharsDuty = auth.docCapCharsDuty
	api.docCapCharsInsight = auth.docCapCharsInsight
	api.docCapCharsLearning = auth.docCapCharsLearning
	api.docCapCharsManualSop = auth.docCapCharsManualSop
	api.docCapCharsManualLearnings = auth.docCapCharsManualLearnings
	api.docCapCharsSystemInteraction = auth.docCapCharsSystemInteraction
	api.docCapCharsBootSequence = auth.docCapCharsBootSequence
	api.docCapCharsOffboard = auth.docCapCharsOffboard
	api.chatBudgetChars = auth.chatBudgetChars
	api.backupRetain = auth.backupRetain
	api.updaterReceiveBeta = auth.updaterReceiveBeta
	api.updaterAutoUpdate = auth.updaterAutoUpdate
	// $OC_RELEASE_API_BASE is a HARNESS seam (conformance/e2e): it re-points
	// the GitHub Releases API base so a black-box run never reaches the real
	// api.github.com (hermeticity + the anonymous rate limit). "" = the real
	// GitHub — normal deployments never set it.
	api.releaseAPIBase = env("OC_RELEASE_API_BASE")
	api.orgName = auth.orgName
	api.ownerName = auth.ownerName
	api.pushContactEmail = auth.pushContactEmail
	api.displayTheme = auth.displayTheme
	api.displayLanguage = auth.displayLanguage
	api.displayWide = auth.displayWide
	api.namespace = cfg.Server.Namespace
	// The embed-fallback binary cache rides beside the SQLite data file — a
	// stable per-instance location that follows the configured DSN (never the
	// CWD): bootstrap/teardown-here can exec the embedded ocwarden repo-less.
	api.binCacheDir = filepath.Join(filepath.Dir(dbPath), "bin")
	// T-9ca5 ⑤: one-shot alignment of any pre-derivation task whose stored status
	// drifts from what its steps derive to. Non-fatal — a hiccup logs, boot goes on.
	if n, err := api.reconcileTaskStatusesOnBoot(); err != nil {
		fmt.Fprintf(out, "[ocserverd] WARN: task status boot reconcile: %v\n", err)
	} else if n > 0 {
		fmt.Fprintf(out, "[ocserverd] task status boot reconcile: aligned %d task(s) to derived status\n", n)
	}
	// T-4166 存量: retire waiting cards left orphaned on already-terminal (or
	// vanished) tasks by the pre-fix lifecycle — unanswerable (409) and
	// un-clearable, so they pinned the cockpit red dot forever. Runs AFTER the
	// task reconcile above, so a task that reconcile just closed is seen closed.
	// Non-fatal — a hiccup logs, boot goes on.
	if n, err := api.reconcileOrphanReplyCardsOnBoot(); err != nil {
		fmt.Fprintf(out, "[ocserverd] WARN: orphan reply-card boot reconcile: %v\n", err)
	} else if n > 0 {
		fmt.Fprintf(out, "[ocserverd] orphan reply-card boot reconcile: retired %d card(s) stranded on closed tasks\n", n)
	}
	claimToken, err := ensureFirstRunClaimToken(dal, auth.passwordHash != "", func(msg string) {
		fmt.Fprintf(out, "[ocserverd] settings: %s\n", msg)
	})
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: claim token: %v\n", err)
		return 1
	}
	handler, err := buildAPIHandler(api, dal.GetMember)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: %v\n", err)
		return 1
	}
	// The MCP tools/call loopback re-enters this very mux (auth gate + RBAC
	// choke + param binding included) — wire it back onto the server.
	api.loopback = handler
	// The lifecycle producer cadence (lifecycle_tick.go): ONE 30s loop running
	// the reconcile half (reconcile.go) then the outsource half
	// (outsource_sched.go), each under its own lock. Both kill switches survive
	// the merge unchanged in meaning — each one skips ITS half inside the tick,
	// and still disables that producer's event-driven seams (api.noReconcile /
	// api.noOutsource). The loop is mounted either way; with both flags set its
	// body is two boolean tests.
	api.noReconcile = noReconcile
	api.noOutsource = noOutsource
	if noReconcile {
		fmt.Fprintln(out, "[ocserverd] --no-reconcile: reconcile producer disabled (no cadence half, no warden-command dispatch)")
	}
	if noOutsource {
		fmt.Fprintln(out, "[ocserverd] --no-outsource: outsource-assignment scheduler disabled (no cadence half, no event-driven assignment)")
	}
	api.startLifecycleCadence(time.Duration(lifecycleCadenceSecs * float64(time.Second)))
	// Auto-update cadence (auto_update.go): ALWAYS mounted — the OFF-default
	// `updater.auto_update` setting gates action, so an owner arming it via
	// PATCH /api/settings needs no restart. An unarmed tick is two mutex reads.
	api.startAutoUpdateCadence(autoUpdateCadence)
	// Post-upgrade migration notice (upgrade_notice.go, T-79): ALWAYS mounted,
	// no toggle. With nothing pending its whole body is one indexed read, and
	// an upgrade that lands while the toggle is off is exactly the upgrade
	// nobody would be told about.
	api.startUpgradeNoticeDelivery()
	// Scheduled messages (scheduled_message.go, T-f059): ALWAYS mounted, no
	// toggle. A tick with no armed schedules is one indexed read; the fire/skip
	// test is slot identity, so nothing here depends on the process having been
	// up when a slot came due.
	api.startScheduledMessageCadence(scheduledMessageCadence)
	// Backup trigger ② (backup.go): ALWAYS mounted, no toggle. A backup that
	// has to be armed is a backup nobody has — and until T-ada9 this studio had
	// none at all. The tick only wakes; runDatabaseBackup decides whether one
	// is actually due.
	// T-da06: the cockpit-visible half. Armed SYNCHRONOUSLY (the first pass and
	// the durable baseline are written before serve continues, which is what
	// makes the wiring provable), then its own watchdog goroutine — deliberately
	// NOT hung off the backup cadence, because the failure it exists to catch is
	// "the cadence never ran at all".
	api.backupHealth = armBackupHealth(dal, dbPath, time.Now())
	startBackupHealthWatchdog(api.backupHealth, backupWatchdogCadence)
	startBackupCadence(db, dbPath, backupCadence, api.backupHealth)
	// The bind host is hardwired loopback (B2): expose via a tunnel, never a
	// direct non-loopback bind.
	addr := fmt.Sprintf("%s:%d", defaultHost, cfg.Server.Port)
	// A `running` onboarding report can only belong to a goroutine that died with
	// a previous process — close it out so it cannot wedge the studio in a state
	// that is invisible to BOTH the re-run check and the cockpit banner.
	api.recoverStaleOnboarding()
	// Bind FIRST, announce second. The old order printed "serving on ..." before
	// ListenAndServe had bound anything, so a port clash produced a log that
	// claimed success and then immediately contradicted itself with a FATAL —
	// and any reader (human or installer) that trusted the first line was simply
	// lied to. Holding the listener is the only honest moment to say we serve.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: %s\n", bindErrorMessage(cfg.Server.Port, err))
		return 1
	}
	// cfg.Server.Port may be 0 for an isolated test run. Only the listener knows
	// the kernel-assigned address, so use it for both the announcement and
	// in-process URLs rather than retaining the requested :0 placeholder.
	boundAddr := ln.Addr().String()
	// The scheme comes from schemeForHost (base_scheme_t78.go), not from a
	// literal. It resolves to http today and every day: boundAddr is this
	// listener's own address and defaultHost is hardwired to 127.0.0.1. So this
	// is not a behaviour change — it is the FIFTH place that decided a scheme on
	// its own, and the most load-bearing of them: selfBase is written into a real
	// machine's OC_BASE by runWardenInstallHere (api_machines.go) and by the
	// first-run onboarding path, i.e. into a launchd plist that nothing ever
	// re-derives. Found by the independent reviewer, who noted it is the same
	// "correct by coincidence" this ticket already fixed once in browser.go and
	// asked why that reasoning stopped one file short.
	api.selfBase = schemeForHost(boundAddr) + "://" + boundAddr
	fmt.Fprintf(out, "ocserverd serving on http://%s\n", boundAddr)
	if claimToken != "" {
		setupURL := firstRunSetupURL(boundAddr, claimToken)
		fmt.Fprintf(out, "[ocserverd] FIRST RUN: no owner password is set — finish setup in a browser by choosing a password (the link carries the one-shot claim code):\n")
		if shouldAutoOpenBrowser(env, stdoutIsTerminal()) {
			go func() {
				time.Sleep(firstRunBrowserDelay)
				popFirstRunBrowser(browserOpener{goos: runtime.GOOS, run: runBrowserCommand}, setupURL, out)
			}()
		} else {
			fmt.Fprintf(out, "[ocserverd]   %s\n", setupURL)
		}
	}
	// Root every request in a station context. A signal shutdown cancels that
	// context after marking the cause, so active SSE handlers run their defer and
	// record station-shutdown instead of looking like peer disconnects. An
	// upgrade marks the same cause before re-exec but deliberately leaves the
	// listener alive until syscall.Exec (or its failure) so the existing
	// restart contract is preserved.
	stationCtx, stationCancel := context.WithCancel(context.Background())
	api.stationCancel = stationCancel
	httpServer := &http.Server{
		Handler: handler,
		BaseContext: func(net.Listener) context.Context {
			return stationCtx
		},
	}
	signalCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	shutdownDone := make(chan struct{})
	go func() {
		<-signalCtx.Done()
		api.markStationShutdown()
		api.cancelStationContext()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), stationShutdownTimeout)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		close(shutdownDone)
	}()
	// Wrap the listener so every accepted connection carries the keep-alive
	// half-open reaper (T-7e07; sseKeepAlive above).
	if err := httpServer.Serve(keepAliveListener{ln}); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(out, "[ocserverd] FATAL: %v\n", err)
		return 1
	}
	if signalCtx.Err() != nil {
		<-shutdownDone
	}
	return 0
}

// bindErrorMessage turns a net.Listen failure into something the operator can
// ACT on. The bare Go error ("listen tcp 127.0.0.1:8770: bind: address already
// in use") states the fact but not the fix; the overwhelmingly common cause is
// a second officraft instance (a stale serve job, or a re-install racing the
// old one), and the fix is to free the port or move this instance off it.
//
// A loud failure here is the DESIGNED behaviour, not a gap waiting for a port
// self-heal. Do not make the server silently bind elsewhere when its preferred
// port is taken: the base URL is hardwired at both ends (launchd plists,
// install.sh's OC_BASE), so an instance that moved its own port would strand
// every warden already installed against the old one. A settings field
// advertising exactly that self-heal once existed on the wire with no writer
// behind it anywhere; it was removed rather than implemented.
func bindErrorMessage(port int, err error) string {
	if errors.Is(err, syscall.EADDRINUSE) {
		return fmt.Sprintf(
			"port %d already in use — another process (very likely another officraft server) holds it. "+
				"Free it, or move this instance: set [server].port in oc.toml, or OC_SERVE_PORT=<other>. "+
				"Find the holder with: lsof -nP -iTCP:%d -sTCP:LISTEN",
			port, port)
	}
	return fmt.Sprintf("cannot bind port %d: %v", port, err)
}
