package main

// account_display.go — the ONE readable-name fold for raw Claude account keys,
// shared by the monitoring fold (api_monitoring.go) and the outsource worker
// projection (api_outsource.go / wire.go), so neither serves a raw credential
// hash.

import "net/http"

// accountRuntimeKey is an internal provenance stamp, deliberately in no API
// DTO: account identity is runtime-specific.
const accountRuntimeKey = "account_runtime"

// accountLabelOverlay: the label is PII and OWNER-FACING ONLY — non-owner
// callers get an empty overlay.
func accountLabelOverlay(telemetry map[string]map[string]any, isOwner bool) map[string]string {
	labels := map[string]string{}
	if !isOwner {
		return labels
	}
	labelTS := map[string]float64{}
	for _, entry := range telemetry {
		account, _ := entry["account"].(string)
		label, _ := entry["account_label"].(string)
		if account == "" || label == "" {
			continue
		}
		ts, _ := entry["ts"].(float64)
		if prior, seen := labelTS[account]; !seen || ts > prior {
			labelTS[account] = ts
			labels[account] = label
		}
	}
	return labels
}

// telemetryAccount: the provenance stamp is the ONLY admissible proof. No
// fallback to the entry's ordinary `runtime` field — every later heartbeat
// mutates it, so an unstamped account would inherit whichever runtime reported
// last. Unstamped ⇒ empty for both runtimes.
func telemetryAccount(entry map[string]any, actorRuntime string) string {
	account, _ := entry["account"].(string)
	if account == "" {
		return ""
	}
	reported, _ := entry[accountRuntimeKey].(string)
	if reported == "" || NormalizeRuntime(reported) != NormalizeRuntime(actorRuntime) {
		return ""
	}
	return account
}

func clearAccountPairing(entry map[string]any) {
	delete(entry, "account")
	delete(entry, accountRuntimeKey)
	delete(entry, "account_label")
}

// applyAccountReport is the single writer of the account unit:
//  1. a runtime that disagrees with the stored stamp retires the old pairing
//     first (the member row's runtime can lag a live switch by a reconcile);
//  2. an account reported WITHOUT a runtime is unprovable: not stored, and it
//     clears the standing pairing (leaving it let a later heartbeat be served
//     an older runtime's key);
//  3. only an account WITH a runtime writes a new pairing.
//
// Every shipped reporter sends `runtime` with `account` (cli/ocagent
// contextreport.go, cli/ocwarden codex_session.go), so rule 2 costs nothing.
func applyAccountReport(entry map[string]any, rawAccount, rawLabel any, runtime *string) {
	account, _ := rawAccount.(string)
	label, _ := rawLabel.(string)
	if runtime != nil {
		if stamped, _ := entry[accountRuntimeKey].(string); stamped != "" &&
			NormalizeRuntime(stamped) != NormalizeRuntime(*runtime) {
			clearAccountPairing(entry)
		}
	}
	if account != "" && runtime == nil {
		clearAccountPairing(entry)
		return
	}
	if runtime == nil {
		return
	}
	if account != "" {
		entry["account"] = account
		entry[accountRuntimeKey] = NormalizeRuntime(*runtime)
	}
	if label != "" {
		entry["account_label"] = label
	}
}

// resolveAccountDisplay: ① the owner's alias (accounts table), visible to
// every caller; ② the reported label overlay; ③ "" — the caller picks its
// fallback. The worker projection and the monitoring session row serve "" (a
// dash, NEVER the raw hash); only the monitoring ACCOUNTS row falls back to the
// raw key, because that row is where aliases are set.
func resolveAccountDisplay(aliases, labels map[string]string, raw string) string {
	if name := aliases[raw]; name != "" {
		return name
	}
	if label := labels[raw]; label != "" {
		return label
	}
	return ""
}

// accountDisplayFold: pass the SAME telemetry snapshot the handler already took,
// so the overlay and the fold read one consistent view.
func (s *apiServer) accountDisplayFold(
	r *http.Request, telemetry map[string]map[string]any,
) (func(string) string, error) {
	aliases, err := s.dal.AccountDisplayNames()
	if err != nil {
		return nil, err
	}
	labels := accountLabelOverlay(telemetry, s.principalOfRequest(r) == principalOwner)
	return func(raw string) string {
		return resolveAccountDisplay(aliases, labels, raw)
	}, nil
}
