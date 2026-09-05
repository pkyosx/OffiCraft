package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
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
	Token          string
	ID             string
	Home           string
	Role           string
	TaskType       string
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
	if cfg.BaseConfigured {
		return false
	}
	fmt.Fprintf(errOut, "[ocagent] %s: no OC_BASE configured — nothing here knows which station to talk to, and the built-in default is this machine's loopback address.\n", subcommand)
	return true
}

// loadConfig resolves OC_* env into a Config (mirrors agent/oc_agent.py
// load_config). Base is stripped of a trailing slash; ID defaults to the JWT
// `sub` claim of the token, so a launch needs only OC_TOKEN + OC_BASE.
func loadConfig(env func(string) string) Config {
	base := normalizeBase(env("OC_BASE")) // T-78: keep the host, re-decide the scheme
	// baseConfigured records exactly one thing: whether the fallback below was
	// taken. That is the state this guard exists for — an address the operator
	// never chose, substituted in silence.
	//
	// IT IS NOT A VALIDITY CHECK, and deliberately not. normalizeBase returns
	// its input unchanged for a value it cannot re-scheme (`OC_BASE=http://`
	// survives as "http://", and the TrimRight below leaves "http:"), so such a
	// value counts as CONFIGURED here even though no request will ever succeed
	// against it.
	//
	// ⚠️ THAT LEAVES ONE CASE THIS GUARD DOES NOT COVER, and it must not be
	// described as if it did. An earlier version of this comment claimed a
	// malformed OC_BASE "fails loudly on the first request"; the independent
	// review measured that false. It holds for upload, download and
	// `diff --external`, which do make a request — but plain `diff` makes none
	// by design, so OC_BASE=http:// prints "http:/diff?..." with exit 0 and an
	// empty stderr. That is the SAME failure shape T-86 exists to remove, from a
	// different input, and arguably a worse one: at least
	// "http://127.0.0.1:7755/diff?..." is recognisably loopback.
	//
	// It is still out of this guard's scope rather than a hole it should grow to
	// cover: closing it means a shape check, and the shape check belongs to
	// normalizeBase, a canonical block mirrored across three modules and pinned
	// by bin/tests/base-scheme-mirror-guard.sh. A warden-supplied base cannot
	// reach this state either (ocwarden install.go asserts ocBaseShape) — it
	// takes a hand-set value. The split is between "an address was invented in
	// silence", which is this field, and "an address was given wrong", which is
	// not.
	baseConfigured := base != ""
	if base == "" {
		base = defaultBase
	}
	base = strings.TrimRight(base, "/")

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
// is confronted against the SHARED TABLE bin/tests/fixtures/namespace-axes.tsv by
// namespace_mirror_test.go in this package — the same discipline the other copies
// already had, so a drift here reddens THIS copy by name.
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
