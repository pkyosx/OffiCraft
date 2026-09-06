package main

// api_lore_switch.go — T-33. The one read that answers 「傳承功能開著嗎？」 and
// nothing else.
//
// 🔴 WHY IT IS A ROUTE OF ITS OWN RATHER THAN A FIELD SOMEBODY ALREADY HAS.
// `lore_enabled` is already on GET /api/settings — and that read is owner/admin
// gated, because the same response carries token TTLs, document caps, the push
// contact address and the onboarding report's raw install log. Owner ruling
// rc-2972dcd48782 (2026-09-06) was put to him as a choice and he took
// 「新一支 tool，只回這個開關（權限半徑就一格）」 over opening `get_settings` to
// ordinary members. So the widening is one field wide, not one document wide.
//
// 🔴 AND WHY IT IS NOT LORE-GATED. It exists to be callable while the feature is
// OFF; that is the only moment its answer is worth anything. Behind
// loreFeatureGate it would return 403 exactly then — and a caller cannot tell
// that 403 from 「you are not permitted」, so the tool would fail in the shape
// that sends its reader looking at the wrong thing. routes.go carries the same
// warning on the row itself, because that is where somebody adding a lore route
// next year will be looking.

import "net/http"

// GET /api/lore-switch — the live station-wide lore switch, alone.
//
// It reads loreEnabledSnapshot() per request like every other lore path, so the
// value it returns is the value at this instant. That is deliberately NOT the
// same guarantee a boot context gives: a boot document is assembled once at wake
// (lore_fold.go), so the 傳承 section an agent is holding can be older than the
// switch. This route is what the difference is for.
func (s *apiServer) HandleGetLoreSwitchApiLoreSwitchGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, loreSwitchDTO{LoreEnabled: s.loreEnabledSnapshot()})
}
