package main

// account_display.go — the ONE readable-name fold for raw Claude account keys
// (credential hash / oauth uid-org), shared by the monitoring fold
// (api_monitoring.go) and the outsource worker projection (api_outsource.go /
// wire.go). T-ba6b: the worker DTO used to serve the raw telemetry key
// verbatim, so the 外包 detail panel showed credential hashes; both surfaces
// now resolve through the same precedence chain.

import "net/http"

// accountLabelOverlay folds the freshest reporter-supplied `account_label`
// (oauthAccount email/org — T-260e) per account key across EVERY telemetry
// entry — members and outsource workers alike (the pre-T-ba6b fold scanned
// only roster members, so an account reported by a worker-only session never
// picked up its label — recon §6-4/§6-6). PRIVACY GATE: the label is PII and
// OWNER-FACING ONLY — callers must pass isOwner=false for any non-owner
// caller, and the overlay then stays empty so every fold degrades honestly.
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

// resolveAccountDisplay maps a raw account key to its human-readable name:
// ① the owner's hand-set alias (accounts table) — highest precedence, never
//
//	overwritten by a reported label, visible to every caller rank;
//
// ② the reported account_label overlay (empty for non-owner callers);
// ③ nothing readable → "" — the caller picks its own honest fallback. The
//
//	worker projection and the monitoring session row serve the empty string
//	(the panel renders a bare dash — NEVER the raw credential hash); only the
//	monitoring ACCOUNTS row falls back to the raw stable key, because that
//	row is the aliasing surface where the key itself is the information.
func resolveAccountDisplay(aliases, labels map[string]string, raw string) string {
	if name := aliases[raw]; name != "" {
		return name
	}
	if label := labels[raw]; label != "" {
		return label
	}
	return ""
}

// accountDisplayFold builds the per-request raw→readable resolver over the
// given telemetry snapshot (pass the SAME snapshot the handler already took,
// so the overlay and the fold read one consistent view). The label overlay is
// owner-gated by the caller's verified principal.
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
