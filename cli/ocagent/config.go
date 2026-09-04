package main

import (
	"encoding/base64"
	"encoding/json"
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
	Base     string
	Token    string
	ID       string
	Home     string
	Role     string
	TaskType string
}

// loadConfig resolves OC_* env into a Config (mirrors agent/oc_agent.py
// load_config). Base is stripped of a trailing slash; ID defaults to the JWT
// `sub` claim of the token, so a launch needs only OC_TOKEN + OC_BASE.
func loadConfig(env func(string) string) Config {
	base := normalizeBase(env("OC_BASE")) // T-78: keep the host, re-decide the scheme
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
		Base:     base,
		Token:    token,
		ID:       id,
		Home:     home,
		Role:     env("OC_ROLE"),
		TaskType: env("OC_TASK_TYPE"),
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
