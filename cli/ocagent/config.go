package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// config (mirrors agent/oc_agent.py: AgentConfig / load_config / jwt_sub)
// ---------------------------------------------------------------------------
//
// SHARED-CODE NOTE: loadConfig + jwtSub are deliberately a faithful port of the
// SAME two helpers already proven in ocwarden/main.go (loadConfig / jwtSub). We
// COPY rather than import because ocwarden is `package main` — its helpers are
// unexported and cannot be imported without first refactoring ocwarden into a
// library package (churn + risk on a landed, working binary). The two ports are
// kept structurally identical so a future extraction into one shared `ocshared`
// module is a mechanical lift, not a rewrite. See the phase-0 report for the
// full rationale. The only intentional divergence from ocwarden's Config is
// this struct carries the agent's extra identity fields (Home/Role/TaskType)
// that agent/oc_agent.py's AgentConfig has and the warden does not.

const defaultBase = "http://127.0.0.1:7755"

// Config is the resolved ocagent identity. Base always has a value; Token/ID are
// empty when unset (a mis-wired launch must degrade, never crash — mirrors the
// Python AgentConfig contract).
type Config struct {
	Base string
	// BaseConfigured says whether Base came from OC_BASE or from the built-in
	// defaultBase fallback. It exists because Base ALONE CANNOT ANSWER THAT
	// QUESTION and two callers need the answer for opposite reasons.
	//
	// WHY A FIELD AND NOT A COMPARISON AGAINST defaultBase. "cfg.Base ==
	// defaultBase" is the obvious derivation and it is wrong: an agent running
	// on the station's own host legitimately sets OC_BASE to the loopback
	// address, so that test would refuse a correctly-wired agent. The two states
	// are genuinely distinct and only the resolver can tell them apart.
	//
	// WHY THE ZERO VALUE IS THE REFUSING SIDE. loadConfig is the only producer
	// in production code, and it always sets this. Everything else that builds a
	// Config is a test, where a literal that says nothing about OC_BASE gets the
	// strict answer rather than a silent pass — a caller that never declared its
	// intent must not inherit the permissive one.
	BaseConfigured bool
	// BaseMalformed says OC_BASE was set but is not an http(s)://host address
	// (`http://`, `ftp://x`, `notaurl`, `http://:9999`). normalizeBase hands such
	// a value back unchanged by design, so without this it would be carried into
	// every URL built from Base — `OC_BASE=http://` made plain `diff` print
	// "http:/diff?..." with exit 0 and nothing on stderr.
	//
	// It is only ever true alongside BaseConfigured, and only loadConfig sets it.
	BaseMalformed bool
	Token         string
	ID            string
	Home          string
	Role          string
	TaskType      string
}

// requireBase is the OC_BASE half of the mis-wire guard, the twin of the
// "no OC_TOKEN configured" refusals upload/download/diff already carry. It
// returns true when the caller must STOP.
//
// THE DEFECT IT CLOSES. loadConfig substitutes defaultBase for an unset
// OC_BASE, so every subcommand downstream holds a syntactically fine base that
// points at this machine. A subcommand that then makes a request does not fail
// loudly: on a machine with nothing on that port it looks like the operation
// simply did nothing, and on the station's OWN host it would reach the real
// station under an identity nobody meant to use. The old diff guard tried to
// catch this by testing Base for emptiness, which the fallback makes
// unreachable — the message existed and the path to it did not.
//
// It names the variable and prints NO VALUE: what is missing is knowable
// without echoing anything, and OC_* values are the one thing this binary must
// never put on someone's terminal.
//
// THE MESSAGE STATES THE FACT, NOT THE CONSEQUENCE, and that is deliberate:
// three callers refuse on it and context-report does not, so a message that
// said "refusing" would be a lie in the one place the fail-safe forbids
// refusing. What each caller does about it is carried by its exit code.
func requireBase(cfg Config, subcommand string, errOut io.Writer) bool {
	if !cfg.BaseConfigured {
		fmt.Fprintf(errOut, "[ocagent] %s: no OC_BASE configured — nothing here knows which station to talk to, and the built-in default is this machine's loopback address.\n", subcommand)
		return true
	}
	if cfg.BaseMalformed {
		fmt.Fprintf(errOut, "[ocagent] %s: OC_BASE is set but is not a usable station address — it must be http:// or https:// followed by a host.\n", subcommand)
		return true
	}
	return false
}

// loadConfig resolves OC_* env into a Config (mirrors agent/oc_agent.py
// load_config). Base is stripped of a trailing slash; ID defaults to the JWT
// `sub` claim of the token, so a launch needs only OC_TOKEN + OC_BASE.
func loadConfig(env func(string) string) Config {
	base := normalizeBase(env("OC_BASE")) // T-78: keep the host, re-decide the scheme
	// baseConfigured records only whether the fallback below was taken; whether
	// the value is usable is baseMalformed's question. The two stay separate
	// because listen and context-report must not refuse either case, and each
	// case gets its own wording.
	baseConfigured := base != ""
	if base == "" {
		base = defaultBase
	}
	base = strings.TrimRight(base, "/")
	baseMalformed := baseConfigured && !baseShapeOK(base)

	token := env("OC_TOKEN")
	id := env("OC_ID")
	if id == "" && token != "" {
		id = jwtSub(token)
	}

	home := env("OC_AGENT_HOME")
	if home == "" {
		home = fallbackAgentsHome(env, os.UserHomeDir)
	}

	return Config{
		Base:           base,
		BaseConfigured: baseConfigured,
		BaseMalformed:  baseMalformed,
		Token:          token,
		ID:             id,
		Home:           home,
		Role:           env("OC_ROLE"),
		TaskType:       env("OC_TASK_TYPE"),
	}
}

// ---------------------------------------------------------------------------
// agent-home fallback — a namespace derivation point, and it was missing (T-5047)
// ---------------------------------------------------------------------------
//
// envNamespaceKey / namespaceShape / fallbackAgentsHome are a HAND-TRANSCRIBED
// MIRROR of cli/ocwarden/namespace.go's envNamespaceKey / namespaceShape /
// officraftRootFor. ocagent and ocwarden are separate Go modules with no import
// path between them (same reason loadConfig/jwtSub above are copies), so this copy
// is confronted against the SHARED TABLE bin/tests/fixtures/namespace-axes.tsv.
// ⚠️ THE CONFRONTATION IS NOT IN THIS PACKAGE. Nothing under cli/ocagent reads
// that table; the only thing that does is a shell check in bin/tests, so a drift
// here is caught at build time or not at all — and never by this module's own
// suite, however green it runs.
//
// THE DEFECT THIS CLOSES
// ----------------------
// The fallback used to be a hard-wired filepath.Join(home, ".officraft", "agents")
// with no namespace in it at all. It was described as an axis that only exists in
// the Go copy; it is not — it is a namespace derivation point that was simply
// MISSING one. A namespaced ocagent that loses OC_AGENT_HOME (spawn only exports it
// when the namespace is non-empty, so a hand-started or re-exec'd listener is one
// unset env away) would resolve its state directory into the MAIN instance's
// ~/.officraft/agents and, keyed only by lowercased agent id, collide with the main
// instance's agent of the same id. What is at stake is bounded — the files under
// there are the SSE cursor, the chat unread cursor, the reply-card seen set and
// the context-report stamp, i.e. pure dedup/optimisation ("losing it costs a full
// refetch or one silent re-baseline, never truth", cursorPath) — but "instance A
// silently writes into instance B's tree" is the exact shape this ticket exists to
// remove, and a derivation point that is right by accident is the thing being
// removed.
const envNamespaceKey = "OC_NAMESPACE"

// namespaceShape is the locked charset, byte-identical to the other copies. It is
// load-bearing here and not decoration: an unvalidated value is joined into a
// PATH, so `OC_NAMESPACE=../x` would escape the instance root entirely.
var namespaceShape = regexp.MustCompile(`^[a-z0-9-]{1,16}$`)

// fallbackAgentsHome derives THIS INSTANCE's agents root: <root>/agents where root
// is ~/.officraft for the main instance and ~/.officraft-<ns> otherwise (path
// syntax uses a dash — see the shared table).
//
// A non-empty OC_NAMESPACE that fails the charset yields "" — the same
// already-tolerated degraded state as an unresolvable home directory (the agent
// then keeps no cross-run dedup state, which costs a refetch and nothing else).
// It deliberately does NOT fold back to the main instance: namespace.go's own
// warning is that "a malformed namespace silently folding back to the main
// instance's paths would be far worse than a hard error", and unlike ocwarden this
// call site has no error channel to refuse through. Loud refusal for a malformed
// namespace stays where it belongs, in ocwarden's namespaceFromEnv at install and
// at run.
func fallbackAgentsHome(env func(string) string, userHomeDir func() (string, error)) string {
	h, err := userHomeDir()
	if err != nil {
		return ""
	}
	ns := env(envNamespaceKey)
	if ns == "" {
		return filepath.Join(h, ".officraft", "agents")
	}
	if !namespaceShape.MatchString(ns) {
		return ""
	}
	return filepath.Join(h, ".officraft-"+ns, "agents")
}

// baseShapeOK is the shape check normalizeBase deliberately does not make: that
// function hands back what it cannot re-scheme, and it is a canonical block
// mirrored across three modules, so the check lives here instead.
func baseShapeOK(base string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return false
	}
	return u.Hostname() != ""
}

// jwtSub reads the `sub` claim of a JWT WITHOUT verifying (the agent holds no
// secret — it only decodes its OWN token to learn its identity; the server
// re-verifies every gated call). A malformed token yields "". Never panics.
// This is a byte-for-byte behavioural twin of ocwarden/main.go's jwtSub and
// agent/oc_agent.py's jwt_sub.
func jwtSub(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		return ""
	}
	if sub, ok := claims["sub"].(string); ok {
		return sub
	}
	return ""
}
