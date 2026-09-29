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

const defaultBase = "http://127.0.0.1:7755"

type Config struct {
	Base string
	// Not derivable as "Base == defaultBase": an agent on the station's own host
	// legitimately sets OC_BASE to that loopback address.
	BaseConfigured bool
	// Set but not http(s)://host. normalizeBase does not check shape, and plain
	// `diff` would turn such a value into a dead link with exit 0.
	BaseMalformed bool
	Token         string
	MemberID      string
	AgentsRoot    string
	Role          string
	TaskType      string
}

// The message states the fact, never "refusing": diff/upload/download refuse on
// it, but context-report only prints it and carries on.
func warnMissingBase(cfg Config, subcommand string, errOut io.Writer) bool {
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

// The shape check lives here, not in normalizeBase: that function is mirrored
// verbatim across three modules and deliberately returns what it cannot re-scheme.
func baseShapeOK(base string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "http", "https":
		return u.Hostname() != ""
	}
	return false
}

func loadConfig(env func(string) string) Config {
	base := normalizeBase(env(baseEnv))
	baseConfigured := base != ""
	if base == "" {
		base = defaultBase
	}
	base = strings.TrimRight(base, "/")
	baseMalformed := baseConfigured && !baseShapeOK(base)

	token := env(tokenEnv)
	id := env(idEnv)
	if id == "" && token != "" {
		id = jwtSub(token)
	}

	home := env(agentHomeEnv)
	if home == "" {
		home = fallbackAgentsHome(env, os.UserHomeDir)
	}

	return Config{
		Base:           base,
		BaseConfigured: baseConfigured,
		BaseMalformed:  baseMalformed,
		Token:          token,
		MemberID:       id,
		AgentsRoot:     home,
		Role:           env("OC_ROLE"),
		TaskType:       env("OC_TASK_TYPE"),
	}
}

// envNamespaceKey / namespaceShape / fallbackAgentsHome are a HAND-TRANSCRIBED
// MIRROR of cli/ocwarden/namespace.go's envNamespaceKey / namespaceShape /
// officraftRootFor. ⚠️ Nothing compares this copy with ocwarden's or with
// bin/tests/fixtures/namespace-axes.tsv: bin/tests/namespace-mirror-guard.sh does
// not grep cli/ocagent, and config_test.go pins literal cases only.
//
// The namespace must be in the fallback: spawn exports OC_AGENT_HOME only for a
// non-empty namespace, so a namespaced ocagent that loses it (hand-started or
// re-exec'd) would otherwise land in the MAIN instance's ~/.officraft/agents and
// collide with the main instance's agent of the same id.
const envNamespaceKey = "OC_NAMESPACE"

var namespaceShape = regexp.MustCompile(`^[a-z0-9-]{1,16}$`)

// A malformed OC_NAMESPACE yields "" and deliberately does NOT fold back to the
// main instance (namespace.go: silent fold-back is far worse than a hard error);
// the loud refusal lives in ocwarden's namespaceFromEnv. "" does not disable
// state: callers filepath.Join it, so state files land relative to the cwd.
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
