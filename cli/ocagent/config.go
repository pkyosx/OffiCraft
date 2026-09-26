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

const defaultBase = "http://127.0.0.1:7755"

type Config struct {
	Base string
	// Not derivable as "Base == defaultBase": an agent on the station's own host
	// legitimately sets OC_BASE to that loopback address.
	BaseConfigured bool
	Token          string
	MemberID       string
	AgentsRoot     string
	Role           string
	TaskType       string
}

// The message states the fact, never "refusing": diff/upload/download refuse on
// it, but context-report only prints it and carries on.
func warnMissingBase(cfg Config, subcommand string, errOut io.Writer) bool {
	if cfg.BaseConfigured {
		return false
	}
	fmt.Fprintf(errOut, "[ocagent] %s: no OC_BASE configured — nothing here knows which station to talk to, and the built-in default is this machine's loopback address.\n", subcommand)
	return true
}

func loadConfig(env func(string) string) Config {
	base := normalizeBase(env("OC_BASE"))
	// IT IS NOT A VALIDITY CHECK, and deliberately not: it records only whether
	// the fallback below was taken. normalizeBase hands back a value it cannot
	// re-scheme unchanged, so OC_BASE=http:// (TrimRight leaves "http:") counts
	// as CONFIGURED.
	//
	// ⚠️ So one malformed case is NOT covered. The claim that a malformed OC_BASE
	// "fails loudly on the first request" was measured false by the independent
	// review: true for upload, download and `diff --external`, but plain `diff`
	// makes no request, so OC_BASE=http:// prints "http:/diff?..." with exit 0 and
	// an empty stderr — the same silent shape T-86 exists to remove.
	//
	// Out of scope here rather than a hole to grow into: a shape check belongs to
	// normalizeBase, mirrored across three modules and pinned by
	// bin/tests/base-scheme-mirror-guard.sh. ocwarden's install asserts
	// ocBaseShape (its loadConfig does not re-check at spawn), so in practice
	// only a hand-set value gets here.
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
		MemberID:       id,
		AgentsRoot:     home,
		Role:           env("OC_ROLE"),
		TaskType:       env("OC_TASK_TYPE"),
	}
}

// envNamespaceKey / namespaceShape / fallbackAgentsHome are a HAND-TRANSCRIBED
// MIRROR of cli/ocwarden/namespace.go's envNamespaceKey / namespaceShape /
// officraftRootFor; the shared truth is bin/tests/fixtures/namespace-axes.tsv.
// ⚠️ Nothing checks THIS copy against that table: bin/tests/namespace-mirror-guard.sh
// does not grep cli/ocagent, and config_test.go pins literal cases only.
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
