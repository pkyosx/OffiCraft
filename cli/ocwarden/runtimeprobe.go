package main

import "strings"

// collectRuntimeCapabilities reports launch readiness without exposing any
// credential material. Claude reuses its existing presence-only probe; Codex
// uses the CLI's non-interactive login status command.
func collectRuntimeCapabilities(env func(string) string, runner CmdRunner,
	claude map[string]any, logf func(string, ...any)) map[string]any {
	out := map[string]any{}
	claudeBin := resolveClaudeBin(env)
	claudeCap := map[string]any{"installed": claudeBin != ""}
	if version, ok := claude["version"].(string); ok && version != "" {
		claudeCap["version"] = version
	}
	// EVIDENCE ONLY, NEVER A GUESS. The two arms of this function are not
	// symmetric and must not be written as if they were. Codex below actually
	// runs `codex login status`: its false is a MEASUREMENT. Claude has no such
	// probe — it has two presence checks (a credential file, a keychain item),
	// and finding neither means only "we found no evidence here", which is not
	// the same claim as "this host is signed out".
	//
	// Emitting that non-finding as logged_in:false published a guess as a fact,
	// and the server then spent it: reconcile.go's runtimeCapabilityReady
	// rejects an explicit false, so a machine with claude installed but its
	// credential kept somewhere these two checks cannot see had its out-of-box
	// assistant resolved to codex and PERSISTED there — an irreversible choice
	// made on a guess. Omitting the key instead reports unknown (declared in
	// spec/openapi.json: "Absent = not probed, which placement reads as
	// unknown, not as false"), which every reader already handles: the server's
	// two readiness gates both spell it `LoggedIn == nil || *LoggedIn`, and the
	// cockpit's "signed out" badge is keyed on `loggedIn === false`, so unknown
	// stops claiming something we did not measure.
	//
	// The cost is accepted and named: a host that IS genuinely signed out now
	// gets picked for claude and fails at spawn instead. Be precise about WHICH
	// refusal that is — an installed-but-signed-out host resolves ClaudeBin, so
	// it can only ever land on claude_not_logged_in, never on
	// claude_bin_unresolved. When this comment was first written, that arm did
	// NOT name the Codex exit, so the sentence backing this trade-off was false
	// for the only path it describes. Both arms now lead with "set this
	// member's 執行環境 to Codex", and a test pins it on each.
	//
	// The harder half of the justification, which this comment used to omit:
	// the spawn-side gate accepts FOUR env-carried credential sources that this
	// probe never looks at (claudeCredEnvKeys — two direct keys plus the
	// Bedrock / Vertex managed-auth flags, where no local claude login exists at
	// all). So a host on Bedrock or Vertex reported logged_in:false and, before
	// this change, was PERSISTED as codex — a machine that could have run claude
	// perfectly well, pinned to the other runtime with no way back.
	// A visible failure beats an invisible irreversible guess.
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
		// KEEP THE ERROR (2026-09-05 codex-probe incident). Until now this line was the whole of it —
		// `logged_in = err == nil` — and the err itself reached nowhere at all.
		// That is not a cosmetic gap: `err` here collapses FOUR different
		// worlds into one false — the host is signed out, the probe TIMED OUT
		// (execRunner's 5s subprocessBudget), codex crashed, or codex is not
		// where we think it is. The server's placement gate
		// (machineSupportsRuntime) then fail-closes on that false, so every
		// codex member on the host stops being placeable, permanently and
		// with no field anywhere saying which of the four it was.
		//
		// MEASURED, 2026-09-05 on the server host: `codex login status` exits 0
		// in every context we could construct — the warden's own 14 env vars
		// replayed under `env -i`, cwd /, stdin closed, no TTY, five concurrent
		// runs, 0.02s each, well inside the 5s budget — and the heartbeat kept
		// reporting logged_in:false every round. Two agents reached that same
		// dead end independently. Nobody could get further because THE ONE
		// THING NOBODY HAD EVER SEEN was this err. Five members sat unstartable
		// while we argued about which hypothesis to test next.
		//
		// WHY THE TEXT GOES TO THE LOG AND NOT INTO THE REPORTED MAP: this
		// function's contract, first line of the doc comment, is that it
		// reports readiness "without exposing any credential material", and
		// this string is the subprocess's stderr — content we do not control
		// and cannot promise is credential-free. The warden's own log is a
		// local file on the host that already ran the command; the capability
		// map is published to the server and rendered in the cockpit. Only one
		// of those two is a safe home for unvetted text.
		if err != nil && logf != nil {
			logf("[ocwarden runtimeprobe] codex login status failed (bin=%s): %v",
				codexBin, err)
		}
	}
	out["codex"] = codexCap
	return out
}
