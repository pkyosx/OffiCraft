package main

// testsupport_test.go — the shared API-test fixture: one full handler stack
// (routing + auth gate + RBAC choke) over a fresh migrated DB, credentials
// exchanged at the real endpoints, and the in-memory JSON round-trip every API
// test speaks through.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// apiTestOwnerPassword is the password newAPITestServer claims the server with.
const apiTestOwnerPassword = "officraft-support-pass"

// apiTestPlainAgentID is a staff member whose role is NOT the admin role, so its
// token resolves to principalAgent — the identity an admin_agent-gated row must
// answer 403 to.
const apiTestPlainAgentID = "kip"

// newAPITestDAL opens a fresh migrated SQLite database for one test, over the
// two pools serve time uses: writes on one connection, reads on several.
func newAPITestDAL(t *testing.T) *DAL {
	t.Helper()
	path := filepath.Join(t.TempDir(), "api-test.db")
	wdb, err := openSQLite(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { wdb.Close() })
	if err := runMigrations(wdb); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	rdb, err := openSQLiteReadPool(path)
	if err != nil {
		t.Fatalf("open read pool: %v", err)
	}
	t.Cleanup(func() { rdb.Close() })
	return NewDALPools(wdb, rdb)
}

// newAPITestStack assembles the handler stack over a FIRST-RUN server: no
// password is set, so the returned claim token is the one-shot gate POST
// /api/auth/set-password demands. The roster is the out-of-box seed (the
// assistant "mira", the server-self warden) plus one plain-agent staff row.
func newAPITestStack(t *testing.T) (*apiServer, http.Handler, *DAL, string) {
	t.Helper()
	return apiTestStack(t, true)
}

// newAPITestStackWithoutSigningSecret is newAPITestStack over the install state
// in which no signing secret was ever configured: the key ring is empty, so
// nothing can be minted and nothing can be verified.
func newAPITestStackWithoutSigningSecret(t *testing.T) (*apiServer, http.Handler, *DAL, string) {
	t.Helper()
	return apiTestStack(t, false)
}

func apiTestStack(t *testing.T, withSigningSecret bool) (*apiServer, http.Handler, *DAL, string) {
	t.Helper()
	// The first-run onboarding kick installs a launchd warden on whoever runs
	// `go test`; the product's own escape hatch keeps it out of the fixture.
	t.Setenv("OC_NO_ONBOARDING", "1")
	d := newAPITestDAL(t)
	if err := seedOutOfBox(d); err != nil {
		t.Fatalf("seedOutOfBox: %v", err)
	}
	if err := d.PutMember(Member{
		ID:           apiTestPlainAgentID,
		Name:         "Kip",
		Kind:         KindStaff,
		RoleKey:      "engineer",
		RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("PutMember: %v", err)
	}
	auth, err := loadAuthSettings(d, defaultConfig(), func(string) {})
	if err != nil {
		t.Fatalf("loadAuthSettings: %v", err)
	}
	claim, err := ensureFirstRunClaimToken(d, auth.passwordHash != "", func(string) {})
	if err != nil {
		t.Fatalf("ensureFirstRunClaimToken: %v", err)
	}
	secret := auth.secret
	if !withSigningSecret {
		secret = nil
	}
	api := newAPIServer(d, NewHub(), singleKeyring(secret), auth.ownerTokenTTL, "../..")
	api.agentTokenTTL = auth.agentTokenTTL
	api.passwordHash = auth.passwordHash
	api.passwordChangedAt = auth.passwordChangedAt
	api.mfaOffered = auth.mfaOffered
	api.totpSecret = auth.totpSecret
	api.totpLastStep = auth.totpLastStep
	api.ctxhigh = auth.ctxhigh
	api.codexCompactionThreshold = auth.codexCompactionThreshold
	api.codexNoticeRound = auth.codexNoticeRound
	api.monitoringRefreshSeconds = auth.monitoringRefreshSeconds
	api.acceleratedGraceSecs = auth.acceleratedGraceSecs
	api.wardenCredLifetimeSecs = auth.wardenCredLifetimeSecs
	api.outsourceMaxParallel = auth.outsourceMaxParallel
	api.docCapCharsDuty = auth.docCapCharsDuty
	api.docCapCharsInsight = auth.docCapCharsInsight
	api.docCapCharsManualSop = auth.docCapCharsManualSop
	api.docCapCharsSystemInteraction = auth.docCapCharsSystemInteraction
	api.docCapCharsBootSequence = auth.docCapCharsBootSequence
	api.docCapCharsOffboard = auth.docCapCharsOffboard
	api.chatBudgetChars = auth.chatBudgetChars
	api.stepNoteCapChars = auth.stepNoteCapChars
	api.backupRetain = auth.backupRetain
	api.updaterReceiveBeta = auth.updaterReceiveBeta
	api.updaterAutoUpdate = auth.updaterAutoUpdate
	api.orgName = auth.orgName
	api.ownerName = auth.ownerName
	api.pushContactEmail = auth.pushContactEmail
	api.displayTheme = auth.displayTheme
	api.displayLanguage = auth.displayLanguage
	api.displayWide = auth.displayWide
	api.suggestedRepliesReplyCard = auth.suggestedRepliesReplyCard
	api.suggestedRepliesTaskMessage = auth.suggestedRepliesTaskMessage
	// $OC_RELEASE_API_BASE's harness seam, pointed at a dead port: nothing in
	// the fixture may reach the real api.github.com.
	api.releaseAPIBase = "http://127.0.0.1:1"
	// The production floor is three seconds per refused credential attempt; a
	// test that is not about the floor should not pay it (throttle.go).
	api.credentialFailureFloor = time.Millisecond

	h, err := buildHandler(specsFor(api), api.keys, d.GetMember, api.authPasswordChangedAt)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	return api, h, d, claim
}

// newAPITestServer is newAPITestStack with the server already claimed: the
// owner token it answers with is exchanged at POST /api/auth/set-password, so
// the credential is one the product minted rather than one the test assembled.
func newAPITestServer(t *testing.T) (*apiServer, http.Handler, *DAL, string) {
	t.Helper()
	api, h, d, claim := newAPITestStack(t)
	status, data := apiJSON(t, h, "POST", "/api/auth/set-password", "",
		`{"password":"`+apiTestOwnerPassword+`","claim_token":"`+claim+`"}`)
	if status != 200 {
		t.Fatalf("set-password: %d %v", status, data)
	}
	owner, _ := data["token"].(string)
	if owner == "" {
		t.Fatalf("set-password must mint an owner token: %v", data)
	}
	return api, h, d, owner
}

// apiTestAgentToken is the session credential the spawn path hands a member —
// minted through the production mint, not hand-assembled. machineID is the
// boot-host placement claim the token carries; "" is the claim-less shape.
func apiTestAgentToken(t *testing.T, api *apiServer, sub, machineID string) string {
	t.Helper()
	tok, err := api.mintAgentToken(sub, machineID, 3600)
	if err != nil {
		t.Fatalf("mintAgentToken: %v", err)
	}
	return tok
}

// apiTestPrincipalToken seeds a roster row that classifies as class and answers
// the session credential for it, so a test can speak as any rung of the ladder
// without hand-assembling claims. The owner has no roster row and no lookup
// path — mint an owner token through the product instead (newAPITestServer).
func apiTestPrincipalToken(t *testing.T, api *apiServer, d *DAL, class principalClass, id string) string {
	t.Helper()
	member := Member{ID: id, Name: id, Kind: KindStaff, RosterStatus: RosterStatusActive}
	switch class {
	case principalMachine:
		member.Kind = KindWarden
	case principalAdminAgent:
		member.RoleKey = adminRoleKey
	case principalAgent:
		member.RoleKey = "conf-plain-role"
	default:
		t.Fatalf("apiTestPrincipalToken cannot seed %v", class)
	}
	if err := d.PutMember(member); err != nil {
		t.Fatalf("PutMember(%q): %v", id, err)
	}
	if got := classifyMember(&member); got != class {
		t.Fatalf("the seeded row classifies as %v, not %v — the fixture is lying about who is calling", got, class)
	}
	return apiTestAgentToken(t, api, id, "")
}

// apiTestArmMFA puts the server in the state a boot with an already-enrolled
// second factor leaves it in (server.go copies exactly these two fields off
// loadAuthSettings), and answers with the shared secret an authenticator app
// would be holding.
func apiTestArmMFA(t *testing.T, api *apiServer, d *DAL) string {
	t.Helper()
	secret, err := newTOTPSecret()
	if err != nil {
		t.Fatalf("newTOTPSecret: %v", err)
	}
	if err := d.PutSetting(settingTOTPSecret, secret); err != nil {
		t.Fatalf("PutSetting: %v", err)
	}
	api.totpSecret = secret
	api.totpLastStep = 0
	return secret
}

// apiTestTOTPCode is the six digits an authenticator app holding secret shows
// right now.
func apiTestTOTPCode(t *testing.T, secret string) string {
	t.Helper()
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		t.Fatalf("decodeTOTPSecret: %v", err)
	}
	return totpCodeAt(key, time.Now().Unix()/totpStepSecs)
}

// apiJSON drives one request through the whole handler stack in memory and
// decodes the JSON answer.
func apiJSON(t *testing.T, h http.Handler, method, target, token, body string) (int, map[string]any) {
	t.Helper()
	rec := apiRequest(t, h, method, target, token, body)
	var parsed any
	if raw := rec.Body.Bytes(); len(raw) > 0 {
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("non-JSON body (%d): %s", rec.Code, raw)
		}
	}
	data, _ := parsed.(map[string]any)
	return rec.Code, data
}

// apiRequest is apiJSON without the decode, for a route whose answer is not
// JSON or whose response headers are the subject.
func apiRequest(t *testing.T, h http.Handler, method, target, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// apiAnyString / apiAnyNumber stand in, inside an expected body, for a field
// whose VALUE cannot be written down in advance — a minted id, an ingest
// timestamp. The field must still be THERE and carry the right shape; nothing
// else in the body may be waved through this way.
const (
	apiAnyString = "\x00any-nonempty-string"
	apiAnyNumber = "\x00any-positive-number"
)

// apiWantBody asserts the decoded response carries EXACTLY the fields in want,
// each equal to the literal beside it. A field the response grows and this
// table does not name is a failure, so nothing can change unnoticed.
func apiWantBody(t *testing.T, data map[string]any, want map[string]any) {
	t.Helper()
	apiWantValue(t, "body", any(data), any(want))
}

// apiWantError asserts the WHOLE error envelope: the code and the complete
// message text.
func apiWantError(t *testing.T, data map[string]any, code, message string) {
	t.Helper()
	apiWantBody(t, data, map[string]any{
		"error": map[string]any{"code": code, "message": message},
	})
}

func apiWantValue(t *testing.T, path string, got, want any) {
	t.Helper()
	switch w := want.(type) {
	case string:
		switch w {
		case apiAnyString:
			if text, ok := got.(string); !ok || text == "" {
				t.Fatalf("%s: want a non-empty string, got %#v", path, got)
			}
			return
		case apiAnyNumber:
			if n, ok := got.(float64); !ok || n <= 0 {
				t.Fatalf("%s: want a positive number, got %#v", path, got)
			}
			return
		}
	case int:
		want = float64(w)
	case float64:
		want = w
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("%s: want an object, got %#v", path, got)
		}
		for key := range g {
			if _, named := w[key]; !named {
				t.Fatalf("%s.%s: the response carries a field the expectation does not name (%#v)", path, key, g[key])
			}
		}
		for key, wv := range w {
			gv, present := g[key]
			if !present {
				t.Fatalf("%s.%s: missing from the response (%v)", path, key, g)
			}
			apiWantValue(t, path+"."+key, gv, wv)
		}
		return
	case []any:
		g, ok := got.([]any)
		if !ok {
			t.Fatalf("%s: want an array, got %#v", path, got)
		}
		if len(g) != len(w) {
			t.Fatalf("%s: want %d entries, got %d (%v)", path, len(w), len(g), g)
		}
		for i := range w {
			apiWantValue(t, fmt.Sprintf("%s[%d]", path, i), g[i], w[i])
		}
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: want %#v, got %#v", path, want, got)
	}
}

// apiErrorMessage pulls the message text out of the error envelope, for a
// caller comparing only that half. apiWantError is the fuller assertion.
func apiErrorMessage(t *testing.T, data map[string]any) string {
	t.Helper()
	body, ok := data["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error object in the response: %v", data)
	}
	msg, _ := body["message"].(string)
	return msg
}

// apiTestListener is one live SSE connection subscribed to the server's hub —
// the observation surface for the deltas a handler fans out. It is the same
// registration /api/events makes, so what it collects is what another client
// would actually have received. memberID "" is the owner/dashboard connection,
// which every publish is addressed to (spec/sse.md §4).
type apiTestListener struct {
	t   *testing.T
	hub *Hub
	l   *hubListener
}

func apiTestListen(t *testing.T, api *apiServer, memberID string) *apiTestListener {
	t.Helper()
	l, err := api.hub.Connect(memberID, "")
	if err != nil {
		t.Fatalf("hub.Connect(%q): %v", memberID, err)
	}
	t.Cleanup(func() { api.hub.Disconnect(l) })
	return &apiTestListener{t: t, hub: api.hub, l: l}
}

// wantFrames asserts this connection received EXACTLY the frames in want, in
// publish order, each compared field by field the way apiWantBody compares a
// response body — apiAnyString / apiAnyNumber for the values that cannot be
// written down in advance. Passing no frame asserts the connection was fanned
// nothing at all.
func (c *apiTestListener) wantFrames(want ...map[string]any) {
	c.t.Helper()
	got := []any{}
	for {
		raw := c.l.pop()
		if raw == nil {
			break
		}
		got = append(got, apiDecodeSSEFrame(c.t, raw))
	}
	expected := make([]any, len(want))
	for i := range want {
		expected[i] = want[i]
	}
	apiWantValue(c.t, "frames", any(got), any(expected))
}

// apiDecodeSSEFrame parses one buffered wire-text frame ("id: N\ndata: {…}\n\n")
// back into the decoded envelope.
func apiDecodeSSEFrame(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	_, data, ok := strings.Cut(strings.TrimSuffix(string(raw), "\n\n"), "\ndata: ")
	if !ok {
		t.Fatalf("SSE frame carries no data line: %q", raw)
	}
	var frame map[string]any
	if err := json.Unmarshal([]byte(data), &frame); err != nil {
		t.Fatalf("SSE frame data is not JSON (%q): %v", data, err)
	}
	return frame
}

// apiTestWebPushSink installs the collector apiServer.webPushSink names, and
// answers with the assertion over what enqueueWebPush handed it.
func apiTestWebPushSink(t *testing.T, api *apiServer) func(want ...map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var sent []any
	api.webPushSink = func(payload webPushPayload) {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Errorf("marshal web-push payload: %v", err)
			return
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Errorf("decode web-push payload: %v", err)
			return
		}
		mu.Lock()
		sent = append(sent, decoded)
		mu.Unlock()
	}
	return func(want ...map[string]any) {
		t.Helper()
		expected := make([]any, len(want))
		for i := range want {
			expected[i] = want[i]
		}
		mu.Lock()
		got := append([]any{}, sent...)
		mu.Unlock()
		apiWantValue(t, "web-push", any(got), any(expected))
	}
}

// apiMarkDone drives mark_task_done through the whole handler stack for a
// caller that only needs the task closed. Since T-182 reporting the last step
// done lands the task in ready_for_done and closes nothing, so every test that
// used to reach `done` by finishing the plan needs this one extra call.
func apiMarkDone(t *testing.T, h http.Handler, taskID, token string) {
	t.Helper()
	if status, data := apiJSON(t, h, "POST", "/api/tasks/"+taskID+"/mark-done", token, ""); status != 200 {
		t.Fatalf("mark-done %s: want 200, got %d (%v)", taskID, status, data)
	}
}
