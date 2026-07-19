package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
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

const defaultBase = "http://127.0.0.1:8770"

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
	base := env("OC_BASE")
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
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".officraft", "agents")
		}
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
