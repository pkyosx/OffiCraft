package main

import "strings"

// collectRuntimeCapabilities reports launch readiness without exposing any
// credential material. logged_in comes from the interval login check; a nil
// verdict leaves the key absent, which the server reads as unknown.
func collectRuntimeCapabilities(env func(string) string, runner CmdRunner,
	claude map[string]any, login loginState) map[string]any {
	out := map[string]any{}
	claudeBin := resolveClaudeBin(env)
	claudeCap := map[string]any{"installed": claudeBin != ""}
	if version, ok := claude["version"].(string); ok && version != "" {
		claudeCap["version"] = version
		if below, known := claudeBelowNotifyMinimum(version); known {
			claudeCap["below_notify_minimum"] = below
		}
	}
	if claudeBin != "" && login.Claude != nil {
		claudeCap["logged_in"] = *login.Claude
	}
	out["claude"] = claudeCap

	codexBin := resolveCodexBin(env)
	codexCap := map[string]any{"installed": codexBin != ""}
	if codexBin != "" {
		// Placement reads this before sending a member whose model is a family word
		// (sol, luna, …) here; a warden without it would pass the word to Codex verbatim.
		codexCap["model_families"] = true
		if version, err := runner.Run(codexBin, "--version"); err == nil {
			fields := strings.Fields(version)
			if len(fields) > 0 {
				codexCap["version"] = fields[len(fields)-1]
			}
		}
		if login.Codex != nil {
			codexCap["logged_in"] = *login.Codex
		}
	}
	out["codex"] = codexCap
	return out
}
