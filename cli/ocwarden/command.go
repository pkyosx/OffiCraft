// The warden does NOT report presence: the server observes this host's actual
// change from its own SSE-connection presence projection, so no warden-side
// reconcile follows a spawn or kill.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	commandTopic = "warden-command"
	rpcStart     = "start"
	rpcStop      = "stop"
	rpcUninstall = "uninstall"
	// Older wardens refuse a newer verb as unknown-rpc (logged + skipped, reader
	// loop unharmed), so new verbs are safe to send fleet-wide.
	rpcUpdate = "update"
	// renew CARRIES NO CREDENTIAL (owner ruling rc-0572eaeb0f4d, option A): the
	// station only says GO; the existing renewal path in renewapply.go does the rest.
	// Not redundant with credential exp: a credential signed by a retired key is
	// worthless at any age, and no clock can see that.
	rpcRenew = "renew"
	// Legacy alias only. worker_start is retired outright: a spawn
	// can wait for the fleet to converge, a kill must not.
	rpcWorkerStop = "worker_stop"

	// The server's fold (api_monitoring.go stopNoopReasonPrefix) matches the
	// "no_such_session" prefix — cross-module contract, do NOT drift the prefix.
	stopNoopReason = "no_such_session: stop was a no-op (no session, no member process on this warden)"

	// The server re-clamps to the same cap.
	commandResultLogMax = 4096
)

type Command struct {
	RPC  string
	Args map[string]any
}

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
	if env.Topic != commandTopic {
		return nil, nil
	}
	if len(env.Data) == 0 {
		return nil, fmt.Errorf("command: warden-command frame missing data")
	}
	var body struct {
		RPC  string         `json:"rpc"`
		Args map[string]any `json:"args"`
	}
	if err := json.Unmarshal(env.Data, &body); err != nil {
		return nil, fmt.Errorf("command: malformed data body: %w", err)
	}
	switch body.RPC {
	case rpcStart, rpcStop, rpcUninstall, rpcUpdate, rpcRenew, rpcWorkerStop,
		rpcLoginStart, rpcLoginCode, rpcLoginCancel:
	default:
		return nil, fmt.Errorf("command: unknown or missing rpc %q", body.RPC)
	}
	if body.Args == nil {
		return nil, fmt.Errorf("command: rpc %q missing args object", body.RPC)
	}
	return &Command{RPC: body.RPC, Args: body.Args}, nil
}

// Spawn → SpawnDeps.start; OK means the spawn was EXECUTED, not that boot
// confirmed (the server judges boot from its presence projection).
// Stop  → a closure over stop(): the robust stop ladder, self-discovering
// pane pid / process tree / workdir listeners.
type CommandDeps struct {
	Spawn    func(StartParams) SpawnOutcome
	Stop     func(session string) (ok, noop bool)
	Teardown func() (ok bool, log string)
	Exit     func(int)
	// No receipt: the swap announces via the telemetry self_update field, and
	// convergence is observed through heartbeat bin_status.
	Update func()
	// Takes and returns nothing: a warden's word about its own credential is worth
	// nothing — the station counts convergence from the key id it sees on the next
	// authenticated request.
	Renew func()
	// No receipt either: a login reports its own progress through
	// runtimeLoginPath, and a command_result would land on member.last_op*,
	// which the login must never touch.
	Login  LoginSeam
	Report func(CommandResult) error
}

type CommandResult struct {
	MemberID string
	WorkerID string
	RPC      string
	OK       bool
	// Reason is folded onto member.last_op_reason, which the cockpit renders even
	// beside a SUCCESSFUL op, so a plain success reports "" (boilerplate would bury
	// the notes that need eyes); Log is the full operation record.
	Reason string
	Log    string
	At     string
}

func truncLog(s string) string {
	if len(s) > commandResultLogMax {
		return s[:commandResultLogMax]
	}
	return s
}

// errReceiptUndelivered: the op RAN but its command_result did not reach the
// server. handlePayload (transport.go) branches on it to suppress the
// `dispatched %s OK` line, which would state the opposite of what happened.
// Logging alone is not the signal: the warden log has, measured, zero readers;
// the server-side receipt deadline is.
var errReceiptUndelivered = errors.New("command_result receipt undelivered")

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

func dispatchCommand(cmd *Command, deps CommandDeps) error {
	if cmd == nil {
		return nil
	}
	switch cmd.RPC {
	case rpcStart:
		params, err := startParamsFromArgs(cmd.Args)
		if err != nil {
			return err
		}
		if deps.Spawn != nil {
			out := deps.Spawn(params)
			// On OK the receipt carries SpawnOutcome.Note (advisory; older wardens used it for
			// pre-trust warnings), folded onto member.last_op_reason like a refusal cause.
			text := out.Reason
			if out.OK {
				text = out.Note
			}
			receiptErr := deps.report(CommandResult{
				MemberID: params.MemberID,
				RPC:      rpcStart,
				OK:       out.OK,
				Reason:   text,
				Log:      text,
			})
			if !out.OK {
				reason := out.Reason
				if reason == "" {
					reason = "spawn refused (warden reported no reason)"
				}
				// Ranked before the receipt fault: the spawn refusal is the actionable fact.
				return fmt.Errorf("command: start for %q did not spawn: %s", params.MemberID, reason)
			}
			if receiptErr != nil {
				return fmt.Errorf("command: start for %q ran but %w: %v",
					params.MemberID, errReceiptUndelivered, receiptErr)
			}
		}
		return nil
	case rpcWorkerStop:
		session, err := workerStopSessionFromArgs(cmd.Args)
		if err != nil {
			return err
		}
		if deps.Stop == nil {
			return fmt.Errorf("command: worker_stop for %q refused: stop seam not wired", session)
		}
		ok, noop := deps.Stop(session)
		workerID, _ := argString(cmd.Args, "worker_id")
		stopReason := "stopped"
		if noop {
			stopReason = stopNoopReason
		}
		if !ok {
			stopReason = "stop incomplete (session still present / sweep survivor)"
		}
		receiptReason := stopReason
		if ok && !noop {
			receiptReason = ""
		}
		// Keyed on worker_id so an old server's fold keeps working.
		receiptErr := deps.report(CommandResult{
			WorkerID: workerID,
			RPC:      rpcWorkerStop,
			OK:       ok,
			Reason:   receiptReason,
			Log:      fmt.Sprintf("session=%s: %s", session, stopReason),
		})
		if !ok {
			return fmt.Errorf("command: worker_stop incomplete for %q (session still present / sweep survivor)", session)
		}
		if receiptErr != nil {
			return fmt.Errorf("command: worker_stop for %q ran but %w: %v",
				session, errReceiptUndelivered, receiptErr)
		}
		return nil
	case rpcUpdate:
		if deps.Update == nil {
			return fmt.Errorf("command: update refused: self-update kick seam not wired")
		}
		deps.Update()
		return nil
	case rpcRenew:
		if deps.Renew == nil {
			return fmt.Errorf("command: renew refused: credential-renewal seam not wired")
		}
		deps.Renew()
		return nil
	case rpcLoginStart, rpcLoginCode, rpcLoginCancel:
		return dispatchLogin(cmd, deps.Login)
	case rpcStop:
		session, err := stopSessionFromArgs(cmd.Args)
		if err != nil {
			return err
		}
		if deps.Stop != nil {
			ok, noop := deps.Stop(session)
			if legacy := legacyWorkerSessionFromArgs(cmd.Args); legacy != "" {
				deps.Stop(legacy)
			}
			memberID, _ := argString(cmd.Args, "member_id")
			reason := "stopped"
			if noop {
				reason = stopNoopReason
			}
			if !ok {
				reason = "stop incomplete (session still present / broken probe / member process survived the sweep)"
			}
			receiptReason := reason
			if ok && !noop {
				receiptReason = ""
			}
			receiptErr := deps.report(CommandResult{
				MemberID: memberID,
				RPC:      rpcStop,
				OK:       ok,
				Reason:   receiptReason,
				Log:      fmt.Sprintf("session=%s: %s", session, reason),
			})
			if receiptErr != nil {
				return fmt.Errorf("command: stop for session %q ran (%s) but %w: %v",
					session, reason, errReceiptUndelivered, receiptErr)
			}
		}
		return nil
	case rpcUninstall:
		memberID, _ := argString(cmd.Args, "member_id")
		// Stop the agent first: a doomed machine must not leave a live agent orphaned.
		if deps.Stop != nil {
			if session, err := stopSessionFromArgs(cmd.Args); err == nil {
				deps.Stop(session)
			}
		}
		// launchd lets the running process finish after bootout, so we are STILL ALIVE
		// and must self-exit; with the plist deleted launchd will NOT relaunch us.
		ok := true
		log := "uninstall: teardown seam not wired"
		if deps.Teardown != nil {
			ok, log = deps.Teardown()
		}
		reason := ""
		if !ok {
			reason = "teardown incomplete (a required artifact could not be removed)"
		}
		// SYNCHRONOUS: the final receipt must be proven delivered before os.Exit;
		// undelivered → stay alive so the server's reconcile can re-issue the uninstall.
		reportErr := deps.report(CommandResult{
			MemberID: memberID,
			RPC:      rpcUninstall,
			OK:       ok,
			Reason:   reason,
			Log:      log,
		})
		if reportErr != nil {
			return fmt.Errorf("command: uninstall receipt undelivered, NOT self-exiting: %w", reportErr)
		}
		// Incomplete teardown: stay up for a retry (a half-torn-down warden that exited
		// would be unmanageable).
		if !ok {
			return fmt.Errorf("command: uninstall teardown incomplete for %q (receipt delivered); staying alive for retry", memberID)
		}
		exit := deps.Exit
		if exit == nil {
			exit = os.Exit
		}
		exit(0)
		return nil
	default:
		return fmt.Errorf("command: unhandled rpc %q", cmd.RPC)
	}
}

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

// Only the three fields the executor CANNOT default are required (a persona-less
// or token-less agent is worse than none); the dispatcher MUST NOT be stricter than
// the executor (e.g. a legitimate model:"").
func startParamsFromArgs(args map[string]any) (StartParams, error) {
	required := func(key string) (string, error) {
		s, ok := argString(args, key)
		if !ok || strings.TrimSpace(s) == "" {
			return "", fmt.Errorf("command: start missing/blank required field %q", key)
		}
		return s, nil
	}
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
	p.Runtime = optional("runtime")
	p.Model = optional("model")
	p.Effort = optional("effort")
	p.SessionName = optional("session_name")
	return p, nil
}

// stop()'s own isMemberSession gate is the authoritative backstop.
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

// The error never quotes args: login_code carries the code.
func dispatchLogin(cmd *Command, login LoginSeam) error {
	if login == nil {
		return fmt.Errorf("command: %s refused: login seam not wired", cmd.RPC)
	}
	loginID, err := loginIDFromArgs(cmd.Args)
	if err != nil {
		return err
	}
	switch cmd.RPC {
	case rpcLoginStart:
		runtime, _ := argString(cmd.Args, "runtime")
		login.Start(loginID, runtime)
	case rpcLoginCode:
		code, _ := argString(cmd.Args, "code")
		if strings.TrimSpace(code) == "" {
			return fmt.Errorf("command: login_code for %s carries no code", loginID)
		}
		login.Code(loginID, code)
	case rpcLoginCancel:
		login.Cancel(loginID)
	}
	return nil
}
