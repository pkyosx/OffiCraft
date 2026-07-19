// Phase 4a-②a "ears (logic half)": the COMMAND DISPATCH core of the stateless
// warden — it turns a server-downpushed DIRECTED command frame into a call on the
// already-built spawn (Phase 2) / kill (Phase 3) execution surfaces. The warden does
// NOT report presence: the server observes this host's ACTUAL change from its own
// SSE-connection presence projection, so no warden-side reconcile is fired.
//
// This file is the PURE-LOGIC half of the "SSE nudge reader": it consumes ONE
// frame payload (a []byte lifted from an SSE `data:` line) → parses → dispatches.
// It does NO network I/O. The real SSE transport (the long-lived /api/events GET,
// reconnect/backoff, line framing) is Phase 4a-②b, wired separately once the server
// side is frozen; that transport will call parseCommandFrame + dispatchCommand for
// every frame it reads. The side-effecting actions (spawn / kill) are injected
// through the CommandDeps seam so tests drive the whole path with fakes and NO real
// tmux / spawn / kill / network.
//
// CONTRACT (closed-loop with the server BE — do NOT drift):
// the SSE data line carries a TOPIC ENVELOPE:
//
//		{"topic":"warden-command","data":{"rpc":"start"|"stop","args":{...}}}
//
//	  - topic != "warden-command"  → SKIP (context-high / chat / keepalive / heartbeat
//	    are NOT this reader's business): parseCommandFrame returns (nil, nil), no error.
//	  - rpc=start → args carry the StartParams fields (member_id / persona_context /
//	    member_token / role / task_type / model / effort / session_name).
//	  - rpc=stop  → args address the member to kill (member_id + session_name/session_id).
//	  - correlation: NONE. The warden does not parse a command_id; the server reconciles
//	    the outcome ASYNC via its own presence projection, never a synchronous reply.
//
// GUARDRAILS (hard):
//   - A MALFORMED frame NEVER crashes the reader loop. Every JSON shape fault
//     (truncated / wrong type / missing rpc / missing args / empty payload) returns
//     an error the caller logs+skips — never a panic. discovered via typed unmarshal
//   - guarded map lookups (no blind type assertions on untrusted input).
//   - A start missing any of the 3 executor-hard-required fields (member_id /
//     persona_context / member_token) is REFUSED (no spawn) — a half-formed spawn
//     (e.g. a token-less or persona-less agent) is MORE dangerous than none. The other
//     4 fields are optional (the executor defaults them); the dispatcher is never
//     stricter than the executor.
//   - dispatch makes ZERO kill/spawn DECISIONS and NEVER pattern-matches. It only
//     EXECUTES the exact directed command; stop only ever targets the command's own
//     member session (the kill mechanism's own isMemberSession guard is the backstop).
//   - the command payload is NOT trusted for SEMANTICS: it drives WHICH action to run,
//     never any reported state. The warden reports no presence; the server observes the
//     host's true state from its own presence projection, never from command args.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	// commandTopic is the ONLY topic this reader acts on. Every other envelope topic
	// (context-high / chat / keepalive / heartbeat) is skipped as a non-error.
	commandTopic = "warden-command"
	rpcStart     = "start"
	rpcStop      = "stop"
	rpcUninstall = "uninstall"
	// rpcUpdate (T-5f01) is the owner's one-click "kick your self-update NOW":
	// dispatch calls the injected Update seam (main.go wires it to the T-c93d
	// updater Kick), which wakes the EXISTING download+verify+swap reconcile
	// immediately instead of waiting out the poll backstop. An older warden
	// build that predates this verb refuses the frame as unknown-rpc below —
	// logged + skipped, the reader loop unharmed (safe to send fleet-wide).
	rpcUpdate = "update"
	// rpcWorkerStop is the RETIRED worker kill verb (A案 P5b: outsource workers
	// now ride the member start/stop verbs — one vocabulary, session
	// member-<ow-id>). Kept accepted ONE transition window as a LEGACY alias so
	// an old server build can still reclaim a worker-<ow-id> session through a
	// new warden; it maps onto the same Stop closure (the kill ladder's guard
	// admits the worker-* namespace for exactly this residual-kill path).
	// rpcWorkerStart is retired OUTRIGHT — an old server's worker_start is
	// refused as unknown-rpc (logged + skipped, reader loop unharmed): a spawn
	// can wait for the fleet to converge, a kill must not.
	rpcWorkerStop = "worker_stop"

	// commandResultLogMax bounds the merged stdout+stderr / reason log carried in a
	// CommandResult (bytes). The server re-clamps to the same cap defensively. Kept
	// small enough that an occasional command_result POST is never a fat payload.
	commandResultLogMax = 4096
)

// Command is one parsed, VALID directed command: an accepted rpc plus its raw args
// map. Args is decoded lazily by the dispatcher (start needs its StartParams fields, stop needs
// addressing) — Command itself asserts only that rpc is a known verb and args is an
// object, so a malformed frame never reaches dispatch.
type Command struct {
	RPC  string
	Args map[string]any
}

// ---------------------------------------------------------------------------
// parse — topic envelope → *Command. ROBUST: any JSON shape fault is an error,
// never a panic; a non-warden-command topic is a benign skip (nil, nil).
// ---------------------------------------------------------------------------

// parseCommandFrame parses ONE SSE frame payload (a raw JSON []byte) into a
// *Command. Three outcomes:
//
//	(nil, nil)   → SKIP: valid envelope whose topic is not "warden-command"
//	              (context-high / chat / keepalive / heartbeat). NOT an error.
//	(nil, err)   → MALFORMED or unactionable: truncated/!object payload, missing or
//	              non-object data, missing/non-string rpc, unknown rpc, or missing/
//	              non-object args. The caller LOGS + SKIPS; the loop keeps living.
//	(cmd, nil)   → a valid, dispatchable directed command.
//
// It NEVER panics on adversarial input: the envelope + data are decoded through
// typed structs (a json.RawMessage for the untrusted inner body), and args lands in
// a map[string]any whose fields the dispatcher reads through guarded lookups.
func parseCommandFrame(payload []byte) (*Command, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("command: empty frame payload")
	}
	var env struct {
		Topic string          `json:"topic"`
		Data  json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, fmt.Errorf("command: malformed envelope: %w", err)
	}
	// Not our topic → skip (non-error). This is the common case on the shared event
	// stream; the reader must ignore everything that is not a directed command.
	if env.Topic != commandTopic {
		return nil, nil
	}
	// A warden-command envelope with an absent/empty data field is malformed: a
	// nil/empty RawMessage would panic nothing but decode to a shape error below.
	if len(env.Data) == 0 {
		return nil, fmt.Errorf("command: warden-command frame missing data")
	}
	var body struct {
		RPC  string         `json:"rpc"`
		Args map[string]any `json:"args"`
	}
	if err := json.Unmarshal(env.Data, &body); err != nil {
		// data is not an object, or rpc/args are the wrong JSON type → malformed.
		return nil, fmt.Errorf("command: malformed data body: %w", err)
	}
	switch body.RPC {
	case rpcStart, rpcStop, rpcUninstall, rpcUpdate, rpcWorkerStop:
		// known verb (worker_stop is the legacy transition alias — see the const)
	default:
		return nil, fmt.Errorf("command: unknown or missing rpc %q", body.RPC)
	}
	if body.Args == nil {
		// missing args, or args JSON-null / non-object (unmarshal leaves it nil).
		return nil, fmt.Errorf("command: rpc %q missing args object", body.RPC)
	}
	return &Command{RPC: body.RPC, Args: body.Args}, nil
}

// ---------------------------------------------------------------------------
// dispatch — *Command → the Phase 2/3 execution surface (spawn / kill).
// ---------------------------------------------------------------------------

// CommandDeps is the injectable side-effect seam for the dispatcher. Phase 4a-②b
// (the SSE transport) wires the REAL closures:
//
//	Spawn → SpawnDeps.start (Phase 2): assembles workdir + .mcp.json + persona, boots
//	        the tmux session. Returns SpawnOutcome (OK means the spawn was EXECUTED,
//	        not that boot confirmed — the server judges boot from its own presence
//	        projection, never the warden).
//	Stop  → a closure over stop(runner, socket, session, kill, getpgid, sweepSeams)
//	        (Phase 3): the single robust stop with the snapshot→SIGHUP→re-assert→
//	        killpg-escalate→re-assert→exact-pid-sweep ladder. It self-discovers the
//	        pane pid / process tree / workdir listeners, so the command need NOT
//	        carry any.
//
// Tests inject fakes for both — no real spawn/kill/tmux/network is ever touched.
//
// Presence is DELIBERATELY absent: the warden does not report presence at all. The
// server projects each host's presence from its own SSE-connection view, so a spawn
// or kill needs no warden-side presence reconcile — the change is observed server-side.
type CommandDeps struct {
	Spawn func(StartParams) SpawnOutcome
	// Stop kills ONE session by name. Since the A案 P5b naming convergence the
	// ONE closure serves both namespaces: member-<id> (members AND outsource
	// workers — a worker session is member-<ow-id> now) and the LEGACY
	// worker-<ow-id> residuals (the transition sweep + the legacy worker_stop
	// alias). The closure resolves the sweep workdir per namespace
	// (transport.go); the kill ladder's own guard is the outermost gate.
	Stop func(session string) bool
	// Teardown removes THIS warden's own install from the machine (launchd bootout +
	// delete the tokfile + plist), returning (ok, log). It is the injected doTeardown
	// closure (bound in buildCommandDeps over the resolved teardown paths). It does the
	// bootout that will make launchd stop this process — but NOT immediately: launchd
	// lets the running process finish, so after Teardown returns the warden is still
	// alive and MUST self-exit. A nil Teardown makes the uninstall case a no-op refusal.
	Teardown func() (ok bool, log string)
	// Exit is the process-exit seam (os.Exit in production; a fake in tests so the
	// uninstall self-exit is asserted without killing the test binary). ONLY the
	// uninstall case calls it — start/stop never exit. A nil Exit falls back to os.Exit.
	Exit func(int)
	// Update is the self-update kick seam (T-5f01): the `update` verb calls it
	// to wake the running self-update reconcile NOW (main.go wires it to the
	// T-c93d updater.Kick — non-blocking, coalesced, sha-gated cheap). No
	// receipt rides it: the swap itself already announces via the telemetry
	// self_update field, and convergence is observed through the heartbeat
	// fingerprints (bin_status), never a synchronous reply. nil ⇒ the verb is
	// refused as unwired (a --once / test build degrades loudly in the log).
	Update func()
	// Report is the SYNCHRONOUS command_result reporter (fleet remote-ops stage 1).
	// After a start/stop/uninstall EXECUTES, dispatchCommand hands it the computed
	// outcome so the server can fold "this member's last op + result/log" into the
	// durable member (an OBSERVATION channel — NOT presence, NOT a reconcile input).
	// It returns an error: nil == the server durably accepted the receipt (a 2xx),
	// non-nil == a transport fault or non-2xx (the receipt did NOT land).
	//
	// For start/stop the return is IGNORED (best-effort, mirrors the prior toothless
	// contract): the reporter still swallows a nil poster / empty field / any non-2xx
	// / any transport error into a benign nil-or-error, and dispatchCommand calls it
	// only AFTER the kill/spawn is done. A nil Report field is a silent no-op there.
	//
	// For UNINSTALL the return is LOAD-BEARING: uninstall is the warden dismantling
	// itself, so its receipt MUST reach the server BEFORE the warden os.Exit()s. The
	// uninstall case blocks on this synchronous POST and self-exits ONLY on a nil
	// return; a non-nil (undelivered) receipt keeps the warden alive so the server's
	// reconcile can re-issue the uninstall rather than losing the final state.
	Report func(CommandResult) error
}

// CommandResult is the receipt for ONE executed directed command — the payload the
// warden best-effort POSTs so the server can fold the last-op result onto the durable
// member. It is the "this execution's log/result", never presence: the server still
// projects presence from its own SSE-connection view.
type CommandResult struct {
	MemberID string // the addressed roster member (from the command args)
	// WorkerID is the addressed outsource worker (ow-… id) for the worker verbs
	// (worker_start / worker_stop). A worker is NOT a roster member, so its
	// receipt keys on this id instead of MemberID — the server folds it onto the
	// durable worker row's last-op fields (T-9ccf), the worker twin of the
	// member last_op* fold. Exactly one of MemberID / WorkerID is set per receipt.
	WorkerID string
	RPC      string // "start" | "stop" | "uninstall" | "worker_start" | "worker_stop"
	OK       bool   // start: SpawnOutcome.OK; stop: the robust-stop verdict
	// Reason is the short structured cause (start: SpawnOutcome.Reason's
	// "<code>: <detail>" one-liner). The server folds it onto the member's
	// last_op_reason so the owner sees WHY an op failed, not just that it did.
	Reason string
	Log    string // merged stdout+stderr / outcome text, truncated to 4 KB
	At     string // RFC3339 timestamp of when the op executed
}

// truncLog clamps s to the command-result log cap (bytes). Defensive against a
// pathological multi-MB log ballooning the POST body.
func truncLog(s string) string {
	if len(s) > commandResultLogMax {
		return s[:commandResultLogMax]
	}
	return s
}

// report is the nil-safe emit of ONE CommandResult. It normalises the log cap + the
// timestamp, then forwards to deps.Report and RETURNS its delivery verdict (nil when
// deps.Report is nil — a silent skip is not a failure). The start/stop callers discard
// this return (best-effort, unchanged); the uninstall caller REQUIRES a nil before it
// self-exits, so the receipt is proven delivered before the warden dies.
func (deps CommandDeps) report(cr CommandResult) error {
	if deps.Report == nil {
		return nil
	}
	cr.Log = truncLog(cr.Log)
	if cr.At == "" {
		cr.At = time.Now().UTC().Format(time.RFC3339)
	}
	return deps.Report(cr)
}

// dispatchCommand executes ONE parsed command. It makes NO placement/grace decisions
// (the server already decided) and NEVER pattern-matches a target — it runs exactly
// the directed action.
//
// A validation refusal (start missing a field, unknown rpc) returns the error WITHOUT
// touching spawn/kill — nothing changed on this host. The command payload is not
// trusted for semantics: it drives WHICH action to run, never any reported state (the
// warden reports none; the server observes the host's true state via its presence
// projection).
func dispatchCommand(cmd *Command, deps CommandDeps) error {
	if cmd == nil {
		return nil // a skipped frame (parse returned nil,nil) — nothing to do.
	}
	switch cmd.RPC {
	case rpcStart:
		params, err := startParamsFromArgs(cmd.Args)
		if err != nil {
			return err // half-formed start REFUSED — no spawn (no op executed → no report).
		}
		if deps.Spawn != nil {
			// Surface a REFUSED spawn as a dispatch error so it is visible in the
			// warden log — never silently mistaken for success (the Phase-4 boot-death
			// masked exactly this: OK=false was dropped and logged as "dispatched OK").
			// Every OK=false path now carries a STRUCTURED Reason ("<code>: <detail>",
			// closed code set — see SpawnOutcome.Reason); the fallback below only fires
			// for an out-of-tree Spawn seam that reports a bare OK=false.
			out := deps.Spawn(params)
			// Best-effort receipt of the EXECUTED start (toothless: reported AFTER the
			// spawn ran, never gating the return). The reason doubles as the log so a
			// refusal cause is visible server-side.
			deps.report(CommandResult{
				MemberID: params.MemberID,
				RPC:      rpcStart,
				OK:       out.OK,
				Reason:   out.Reason,
				Log:      out.Reason,
			})
			if !out.OK {
				reason := out.Reason
				if reason == "" {
					reason = "spawn refused (warden reported no reason)"
				}
				return fmt.Errorf("command: start for %q did not spawn: %s", params.MemberID, reason)
			}
		}
		return nil
	case rpcWorkerStop:
		// LEGACY transition alias (P5b): an old server reclaiming a worker still
		// addresses the retired worker-<ow-id> session by worker_id. Same robust
		// stop ladder through the ONE Stop closure (its workdir resolution keys
		// on the worker- prefix); receipt stays keyed on worker_id so an old
		// server's fold keeps working.
		session, err := workerStopSessionFromArgs(cmd.Args)
		if err != nil {
			return err
		}
		if deps.Stop == nil {
			return fmt.Errorf("command: worker_stop for %q refused: stop seam not wired", session)
		}
		ok := deps.Stop(session)
		workerID, _ := argString(cmd.Args, "worker_id")
		stopReason := "stopped"
		if !ok {
			stopReason = "stop incomplete (session still present / sweep survivor)"
		}
		deps.report(CommandResult{
			WorkerID: workerID,
			RPC:      rpcWorkerStop,
			OK:       ok,
			Reason:   stopReason,
			Log:      fmt.Sprintf("session=%s: %s", session, stopReason),
		})
		if !ok {
			return fmt.Errorf("command: worker_stop incomplete for %q (session still present / sweep survivor)", session)
		}
		return nil
	case rpcUpdate:
		// One-shot self-update kick. No target to resolve (the frame addresses
		// THIS warden by connection), no receipt (see the Update seam comment)
		// — the kick is fire-and-forget and the reconcile it wakes is
		// idempotent by the content-hash swap oracle.
		if deps.Update == nil {
			return fmt.Errorf("command: update refused: self-update kick seam not wired")
		}
		deps.Update()
		return nil
	case rpcStop:
		session, err := stopSessionFromArgs(cmd.Args)
		if err != nil {
			return err
		}
		if deps.Stop != nil {
			ok := deps.Stop(session)
			// P5b TRANSITION SWEEP: a stop addressed by member_id also reaps the
			// LEGACY worker-<id> session the pre-convergence naming would have
			// spawned — an old residual must never become unkillable once the
			// server only speaks member verbs. Exact derived name only (never a
			// pattern); stopping an absent session is a clean no-op, and the
			// verdict/receipt stay the PRIMARY session's (the legacy reap is
			// best-effort hygiene, logged by the ladder itself).
			if legacy := legacyWorkerSessionFromArgs(cmd.Args); legacy != "" {
				deps.Stop(legacy)
			}
			// Best-effort receipt of the EXECUTED stop — the ladder's FINAL verdict
			// (ok==true ⇒ ¬has_session AND sweep-clean confirmed). member_id is resolved
			// from the args (the stop shortcut on the server keys off it); the session
			// name is the log so "which session this stop targeted" is visible.
			memberID, _ := argString(cmd.Args, "member_id")
			reason := "stopped"
			if !ok {
				reason = "stop incomplete (session still present / broken probe / member process survived the sweep)"
			}
			deps.report(CommandResult{
				MemberID: memberID,
				RPC:      rpcStop,
				OK:       ok,
				Reason:   reason,
				Log:      fmt.Sprintf("session=%s: %s", session, reason),
			})
		}
		return nil
	case rpcUninstall:
		// UNINSTALL is the warden dismantling ITSELF: kill the addressed agent session,
		// tear down this warden's own install, then SYNCHRONOUSLY report the receipt and
		// (only on a delivered receipt) self-exit. member_id addresses the receipt; the
		// session (session_name/session_id/member_id-derived) is the agent to kill first.
		memberID, _ := argString(cmd.Args, "member_id")
		// ① Stop the addressed agent session first — a doomed machine should not leave a
		// live agent orphaned once its warden is gone. A missing target is tolerated: an
		// uninstall may target a host whose agent is already dead, and the teardown of the
		// warden itself is the load-bearing step. stop()'s isMemberSession gate is the
		// backstop against ever killing the warden's own / a non-member session.
		if deps.Stop != nil {
			if session, err := stopSessionFromArgs(cmd.Args); err == nil {
				deps.Stop(session)
			}
		}
		// ② Tear down THIS warden's own install (bootout + delete tokfile/plist). This
		// issues the launchd bootout that will eventually stop this process — but launchd
		// lets the running process finish, so we are STILL ALIVE here and must self-exit
		// in ④. Because the plist is now deleted, launchd will NOT relaunch us on exit.
		ok := true
		log := "uninstall: teardown seam not wired"
		if deps.Teardown != nil {
			ok, log = deps.Teardown()
		}
		reason := "uninstalled"
		if !ok {
			reason = "teardown incomplete (a required artifact could not be removed)"
		}
		// ③ SYNCHRONOUS report — the timing barrier. The warden is about to os.Exit, so
		// its final receipt MUST be proven delivered (a 2xx) BEFORE we die: a dropped
		// fire-and-forget POST would leave the server unable to reconcile this member's
		// end state. deps.report returns the delivery verdict; we gate the exit on it.
		reportErr := deps.report(CommandResult{
			MemberID: memberID,
			RPC:      rpcUninstall,
			OK:       ok,
			Reason:   reason,
			Log:      log,
		})
		if reportErr != nil {
			// The receipt did NOT land. Do NOT exit: staying alive lets the server's
			// reconcile re-issue the uninstall (and re-attempt the receipt) rather than
			// losing the final state to a silent death. Surface it in the warden log.
			return fmt.Errorf("command: uninstall receipt undelivered, NOT self-exiting: %w", reportErr)
		}
		// ④ Receipt delivered. Self-exit. ok→0 (clean uninstall; plist gone so launchd
		// will not relaunch). A teardown that did NOT fully complete (ok=false) still
		// reported successfully, but we DO NOT exit — the warden stays up so a retry can
		// finish the removal (a half-torn-down warden that exited would be unmanageable).
		if !ok {
			return fmt.Errorf("command: uninstall teardown incomplete for %q (receipt delivered); staying alive for retry", memberID)
		}
		exit := deps.Exit
		if exit == nil {
			exit = os.Exit
		}
		exit(0)
		return nil // unreachable in production (exit terminates); reached only with a fake Exit in tests.
	default:
		// Unreachable in practice: parseCommandFrame already rejected unknown rpcs.
		// Kept as a defense-in-depth refusal so a hand-built *Command can't slip past.
		return fmt.Errorf("command: unhandled rpc %q", cmd.RPC)
	}
}

// ---------------------------------------------------------------------------
// arg extraction — GUARDED map reads (never a blind type assertion on untrusted
// input). A missing OR wrong-typed field reads as absent, never a panic.
// ---------------------------------------------------------------------------

// argString returns args[key] as a string, or ("", false) when absent OR not a
// JSON string. Guarded so a malformed frame (e.g. member_id: 42) is a clean refusal.
func argString(args map[string]any, key string) (string, bool) {
	v, ok := args[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	return s, true
}

// startParamsFromArgs builds StartParams from the start args. Only the three fields
// the executor CANNOT default are HARD-REQUIRED: member_id (workdir + session naming),
// persona_context (the boot content written to disk), member_token (the .mcp.json
// auth). A start missing/blank any of these is REFUSED — a persona-less or token-less
// agent is worse than none.
//
// The other five (role / task_type / model / effort / session_name) are OPTIONAL:
// SpawnDeps.start already supplies its own defaults (role→"agent", session→member-<id>,
// model→omitted from the launch flags, effort→"medium", task_type→parity-only). The dispatcher passes them through when
// present and leaves them empty otherwise — it MUST NOT be stricter than the executor
// and refuse a start the executor would happily default (e.g. a legitimate model:"").
func startParamsFromArgs(args map[string]any) (StartParams, error) {
	required := func(key string) (string, error) {
		s, ok := argString(args, key)
		// TrimSpace-judge blank: a whitespace-only load-bearing field (e.g. a
		// member_token of " ") would produce a broken spawn (a "Bearer  " auth
		// header / an empty persona) — the same half-formed hazard as an absent
		// field. The value passed downstream is the ORIGINAL s (never trimmed).
		if !ok || strings.TrimSpace(s) == "" {
			return "", fmt.Errorf("command: start missing/blank required field %q", key)
		}
		return s, nil
	}
	// optional: absent OR non-string reads as empty; the executor defaults it. Being
	// lenient here (vs refusing a wrong-typed optional) keeps the dispatcher from being
	// stricter than the executor — the member still spawns with a sane default.
	optional := func(key string) string {
		s, _ := argString(args, key)
		return s
	}
	var p StartParams
	var err error
	if p.MemberID, err = required("member_id"); err != nil {
		return StartParams{}, err
	}
	if p.PersonaContext, err = required("persona_context"); err != nil {
		return StartParams{}, err
	}
	if p.MemberToken, err = required("member_token"); err != nil {
		return StartParams{}, err
	}
	p.Role = optional("role")
	p.TaskType = optional("task_type")
	p.Model = optional("model")
	// effort (M2-2): the owner-set reasoning-effort launch intent. Optional like
	// model — absent/blank keeps the executor's historic "--effort medium".
	p.Effort = optional("effort")
	p.SessionName = optional("session_name")
	return p, nil
}

// stopSessionFromArgs resolves the tmux session name the robust stop must target.
// Addressing priority: an explicit session_name, else session_id, else derive
// member-<id> from member_id. At least ONE of these must be present, else refuse.
//
// The dispatcher does NOT re-validate that the resolved name is a member session —
// stop()'s own isMemberSession gate is the authoritative backstop (it refuses the
// telemetry warden's own session, a bare "member-", or any non-member name). We only
// ensure we hand stop a concrete target; we never guess or pattern-match one.
func stopSessionFromArgs(args map[string]any) (string, error) {
	if s, ok := argString(args, "session_name"); ok && s != "" {
		return s, nil
	}
	if s, ok := argString(args, "session_id"); ok && s != "" {
		return s, nil
	}
	if id, ok := argString(args, "member_id"); ok && id != "" {
		return memberSessionName(id), nil
	}
	return "", fmt.Errorf("command: stop missing target (need session_name/session_id/member_id)")
}
