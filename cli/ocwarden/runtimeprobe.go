package main

import "strings"

// collectRuntimeCapabilities reports launch readiness without exposing any
// credential material.
func collectRuntimeCapabilities(env func(string) string, runner CmdRunner,
	claude map[string]any, logf func(string, ...any)) map[string]any {
	out := map[string]any{}
	claudeBin := resolveClaudeBin(env)
	claudeCap := map[string]any{"installed": claudeBin != ""}
	if version, ok := claude["version"].(string); ok && version != "" {
		claudeCap["version"] = version
	}
	// Claude has only two presence checks (credential file, keychain item), so
	// finding neither is NOT "signed out" — never emit logged_in:false (Codex's
	// false below is a measurement; this would be a guess). The server's
	// runtimeCapabilityReady (reconcile.go) rejects an explicit false, and such
	// hosts got their assistant PERSISTED to codex irreversibly — including
	// Bedrock / Vertex hosts whose credentials ride env keys this probe never
	// sees (claudeCredEnvKeys). Absent reads as unknown (spec/openapi.json).
	// Accepted cost: a genuinely signed-out host fails at spawn with
	// claude_not_logged_in instead.
	if claudeBin != "" {
		if value, ok := claude["cred_file"].(bool); ok && value {
			claudeCap["logged_in"] = true
		} else if value, ok := claude["keychain"].(bool); ok && value {
			claudeCap["logged_in"] = true
		}
	}
	out["claude"] = claudeCap

	codexBin := resolveCodexBin(env)
	codexCap := map[string]any{"installed": codexBin != ""}
	if codexBin != "" {
		if version, err := runner.Run(codexBin, "--version"); err == nil {
			fields := strings.Fields(version)
			if len(fields) > 0 {
				codexCap["version"] = fields[len(fields)-1]
			}
		}
		_, err := runner.Run(codexBin, "login", "status")
		codexCap["logged_in"] = err == nil
		// Keep the error: a false here conflates signed out, probe timeout (5s
		// subprocessBudget), crash and wrong binary, and the placement gate
		// (machineSupportsRuntime) fail-closes every codex member on the host on
		// it. It goes to the local log, NOT the published map: it is subprocess
		// stderr we cannot promise is credential-free.
		if err != nil && logf != nil {
			logf("[ocwarden runtimeprobe] codex login status failed (bin=%s): %v",
				codexBin, err)
		}
	}
	out["codex"] = codexCap
	return out
}
