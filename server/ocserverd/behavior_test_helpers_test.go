package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

const bootSequenceH1 = "# 啟動步驟（Boot Sequence"

// resumeCtxServer seeds the named roster used by resume-context behavior tests.
func resumeCtxServer(t *testing.T) *apiServer {
	t.Helper()
	api := newTasksTestServer(t)
	for id, name := range map[string]string{
		"m-exec":  "阿執",
		"m-peer":  "小佩",
		"m-loud":  "大聲",
		"m-quiet": "安靜",
	} {
		if err := api.dal.PutMember(Member{
			ID: id, Name: name, Kind: "staff", RosterStatus: RosterStatusActive,
		}); err != nil {
			t.Fatalf("seed member %s: %v", id, err)
		}
	}
	return api
}

func setWorkerModelBody(t *testing.T, api *apiServer, workerID string, body map[string]any) {
	t.Helper()
	rec := postWorker(t, api, workerID, "model", body,
		api.HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("set model: %d %s", rec.Code, rec.Body.String())
	}
}

func workerBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

var (
	rfc3339Instant = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})`)
	timeShapeASCII = regexp.MustCompile(`(?i)\b\d+(\.\d+)?\s*(ms|msec|msecs|millisecond|milliseconds|s|sec|secs|second|seconds|m|min|mins|minute|minutes|h|hr|hrs|hour|hours|d|day|days)\b`)
	timeShapeCJK   = regexp.MustCompile(`[0-9０-９〇零一二三四五六七八九十兩两半幾几百千]+\s*(秒鐘|秒钟|秒|分鐘|分鍾|分钟|分|鐘|鍾|钟|小時|小时|時|时|刻鐘|刻钟|天)`)
	timeShapeClock = regexp.MustCompile(`\b\d{1,3}:\d{2}(:\d{2})?\b`)
	// Go's time.Duration.String() shape, such as 1m14s or 1h30m0s.
	timeShapeGoDuration = regexp.MustCompile(`(?i)\b\d+(\.\d+)?(ns|us|ms|s|m|h)(\d+(\.\d+)?(ns|us|ms|s|m|h))+\b`)
	deadlineWords       = regexp.MustCompile(`(?i)deadline|截止|死線|死线`)
)

func composedSentence(notice string) string {
	if i := strings.Index(notice, "\n"); i >= 0 {
		return notice[:i]
	}
	return notice
}

func quotesTimeOfAnyShape(notice string) (string, bool) {
	sentence := rfc3339Instant.ReplaceAllString(composedSentence(notice), "<instant>")
	for _, re := range []*regexp.Regexp{timeShapeASCII, timeShapeCJK, timeShapeClock, timeShapeGoDuration} {
		if m := re.FindString(sentence); m != "" {
			return m, true
		}
	}
	return "", false
}

func quotesADeadline(notice string) (string, bool) {
	if m := deadlineWords.FindString(composedSentence(notice)); m != "" {
		return m, true
	}
	return "", false
}

func assertQuotesNoTime(t *testing.T, arm, notice string) {
	t.Helper()
	if frag, yes := quotesADeadline(notice); yes {
		t.Fatalf("%s named a deadline (%q) nobody will honour:\n%s", arm, frag, notice)
	}
	if frag, yes := quotesTimeOfAnyShape(notice); yes {
		t.Fatalf("%s started a countdown nobody is counting (%q) — a span goes "+
			"stale on every replay and breaks the client's verbatim de-dupe:\n%s",
			arm, frag, notice)
	}
}

// parseSSEFrame splits one "id: N\ndata: {...}\n\n" wire text into the id
// line and the decoded JSON envelope.
func parseSSEFrame(t *testing.T, raw []byte) (string, map[string]any) {
	t.Helper()
	text := string(raw)
	if !strings.HasSuffix(text, "\n\n") {
		t.Fatalf("frame must end with a blank line: %q", text)
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "id: ") || !strings.HasPrefix(lines[1], "data: ") {
		t.Fatalf("frame shape: %q", text)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[1], "data: ")), &envelope); err != nil {
		t.Fatalf("frame data is not JSON: %v", err)
	}
	return strings.TrimPrefix(lines[0], "id: "), envelope
}

// drainListener empties a listener's queue and returns how many frames it held.
func drainListener(l *hubListener) int {
	n := 0
	for l.pop() != nil {
		n++
	}
	return n
}

func obsOf(id, desired string, online bool) memberObservation {
	return memberObservation{MemberID: id, Desired: desired, Online: online}
}

// failAfterWrites is a ResponseWriter+Flusher whose Write succeeds `ok` times
// and fails forever after.
type failAfterWrites struct {
	mu     sync.Mutex
	ok     int
	n      int
	hdr    http.Header
	frames [][]byte
}

func newFailAfterWrites(ok int) *failAfterWrites {
	return &failAfterWrites{ok: ok, hdr: http.Header{}}
}

func (c *failAfterWrites) Header() http.Header { return c.hdr }
func (c *failAfterWrites) WriteHeader(int)     {}
func (c *failAfterWrites) Flush()              {}

func (c *failAfterWrites) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	if c.n > c.ok {
		return 0, errors.New("connection reset by peer")
	}
	frame := make([]byte, len(p))
	copy(frame, p)
	c.frames = append(c.frames, frame)
	return len(p), nil
}

func (c *failAfterWrites) written() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]byte, len(c.frames))
	copy(out, c.frames)
	return out
}

// getTaskView reads the full task view. Artifact rows are intentionally not
// part of this response; callers that need rows use the artifact read face.
func getTaskView(t *testing.T, api *apiServer, taskID string) taskDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetTaskApiTasksTaskIdGet(rec,
		taskReq(t, "GET", "/api/tasks/"+taskID, nil, "owner", "owner"), taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("get task: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[taskDTO](t, rec)
}

// createdCardView follows the create receipt through the same read face used
// by the cockpit; create_reply_card deliberately returns only a receipt.
func createdCardView(t *testing.T, api *apiServer, rec *httptest.ResponseRecorder) replyCardDTO {
	t.Helper()
	receipt := decodeBody[replyCardCreateReceiptDTO](t, rec)
	if receipt.ID == "" {
		t.Fatalf("create receipt carried no card id: %s", rec.Body.String())
	}
	fresh := getReplyCardRaw(t, api, receipt.ID)
	if fresh.Code != http.StatusOK {
		t.Fatalf("get_reply_card %s: %d %s", receipt.ID, fresh.Code, fresh.Body.String())
	}
	return decodeBody[replyCardDTO](t, fresh)
}

// getReplyCardRaw fetches a card through the single-card endpoint.
func getReplyCardRaw(t *testing.T, api *apiServer, cardID string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetReplyCardApiReplyCardsCardIdGet(rec,
		taskReq(t, "GET", "/api/reply-cards/"+cardID, nil, "owner", "owner"), cardID)
	return rec
}

// errorMessageOf reads the unified error envelope's message so refusal tests
// assert the reason as well as the status code.
func errorMessageOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error envelope: %v (%s)", err, rec.Body.String())
	}
	return body.Error.Message
}

// strandLegacyOrphanCard recreates the pre-fix row shape that can still exist
// in a live database, so legacy-card guards remain executable.
func strandLegacyOrphanCard(t *testing.T, api *apiServer, cardID string) ReplyCard {
	t.Helper()
	c, err := api.dal.GetReplyCard(cardID)
	if err != nil || c == nil {
		t.Fatalf("card: %v %v", c, err)
	}
	c.Status = replyCardStatusWaiting
	c.ExpiredTS = 0
	if err := api.dal.PutReplyCard(*c); err != nil {
		t.Fatalf("strand card: %v", err)
	}
	return *c
}

// createTaskAs posts create_task as the supplied principal and scope.
func createTaskAs(t *testing.T, api *apiServer, body map[string]any, sub, scope string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleCreateTaskApiTasksPost(rec, taskReq(t, "POST", "/api/tasks", body, sub, scope))
	return rec
}

// mintDiffLink calls the mint route and returns the server-relative URL.
func mintDiffLink(t *testing.T, base, token, query string) string {
	t.Helper()
	status, body := doRaw(t, "GET", base+"/api/diff/share-link?"+query, token, "", nil)
	if status != 200 {
		t.Fatalf("mint failed: %d %s", status, body)
	}
	var minted DiffShareLinkDTO
	if err := json.Unmarshal([]byte(body), &minted); err != nil {
		t.Fatalf("mint answered unparseable JSON: %v (%s)", err, body)
	}
	return minted.Url
}

// interopSecret is the stable signing key used by the mainline wired fixtures.
// Keep it in the behavior-fixture layer: the canonical JWT tests do not need a
// package-wide secret, but the behavior files that were split from the old
// ticket tests still share this value.
const interopSecret = "interop-unit-test-signing-secret"

// newTestDAL is the direct-DAL fixture still used by canonical behavior tests
// that do not need the API read pool or HTTP route table.
func newTestDAL(t *testing.T) *DAL {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "behavior-test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewDAL(db)
}

// newWiredTestServer is the route/auth fixture used by behavior checks that
// must prove the real HTTP surface. It is deliberately kept here with the
// other compatibility fixtures rather than restoring the old server test
// file.
func newWiredTestServer(t *testing.T) (*httptest.Server, []byte, *Hub) {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "behavior-server.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	dal := NewDAL(db)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	secret := []byte(interopSecret)
	hub := NewHub()
	api := newAPIServer(dal, hub, singleKeyring(secret), 3600, "../..")
	passwordHash, err := hashPassword("test-password")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	api.passwordHash = passwordHash
	h, err := buildHandler(specsFor(api), api.keys, dal.GetMember, nil)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	api.loopback = h
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, secret, hub
}

// newDocCapTestServer is the wired document fixture with the DAL exposed so a
// test can seed a deterministic overlay before exercising the REST surface.
func newDocCapTestServer(t *testing.T) (*httptest.Server, *DAL, []byte) {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "behavior-doccap.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	dal := NewDAL(db)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	secret := []byte(interopSecret)
	api := newAPIServer(dal, NewHub(), singleKeyring(secret), 3600, "../..")
	passwordHash, err := hashPassword("test-password")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	api.passwordHash = passwordHash
	h, err := buildHandler(specsFor(api), api.keys, dal.GetMember, nil)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	api.loopback = h
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, dal, secret
}

func newReconcileTestServer(t *testing.T) *apiServer {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "behavior-reconcile.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	dal := NewDAL(db)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return newAPIServer(dal, NewHub(), singleKeyring([]byte("reconcile-test-secret")), 3600, "../..")
}

// putOutsourceManual stores the smallest typed manual needed by scheduler and
// worker-lifecycle behavior tests. Governance is intentionally outside this
// fixture; those tests exercise the resulting runtime behavior.
func putOutsourceManual(t *testing.T, api *apiServer, typeKey, model string, copies int) {
	t.Helper()
	if err := api.dal.PutTaskManual(TaskManual{
		TypeKey: typeKey,
		Fields:  "[]",
		Assignee: `{"kind":"outsource","model":"` + model + `",` +
			`"effort":"high","copies":` + strconv.Itoa(copies) + `}`,
	}); err != nil {
		t.Fatalf("put manual: %v", err)
	}
}

// createOutsourceTask creates one typed task through the real task handler so
// the fixture retains the same executor classification as production.
func createOutsourceTask(t *testing.T, api *apiServer, typeKey, title string) taskDTO {
	t.Helper()
	if m, _ := api.dal.GetMember("m-front"); m == nil {
		if err := api.dal.PutMember(Member{
			ID: "m-front", Name: "小前", Kind: "staff", RoleKey: adminRoleKey,
			RosterStatus: RosterStatusActive,
		}); err != nil {
			t.Fatalf("seed approver creator: %v", err)
		}
	}
	rec := httptest.NewRecorder()
	api.HandleCreateTaskApiTasksPost(rec, taskReq(t, "POST", "/api/tasks",
		map[string]any{"title": title, "type_key": typeKey}, "m-front", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create outsource task: %d %s", rec.Code, rec.Body.String())
	}
	return createdTaskView(t, api, rec)
}

// seedLiveWorkerEnv seeds an eligible online warden and the manual used by
// worker respawn behavior tests.
func seedLiveWorkerEnv(t *testing.T, api *apiServer) {
	t.Helper()
	seedMachine(t, api, ServerSelfHost)
	connectWarden(t, api, ServerSelfHost)
	putOutsourceManual(t, api, "review-pr", "claude-sonnet-4-5", 1)
}

// newActiveWorker builds an active worker bound to a live outsource task. The
// optional online session distinguishes an active worker with a live SSE from
// one that was claimed and then disconnected.
func newActiveWorker(t *testing.T, api *apiServer, online bool) string {
	t.Helper()
	seedLiveWorkerEnv(t, api)
	task := createOutsourceTask(t, api, "review-pr", "review")
	workerID := "ow-" + newHexID(6)
	w := OutsourceWorker{
		ID: workerID, Codename: "S-" + workerID, Model: "claude-sonnet-4-5",
		Effort: "medium", TaskID: task.ID, Status: WorkerStatusActive,
		DesiredState: DesiredStateOnline, DesiredMachineID: ServerSelfHost,
	}
	if err := api.dal.PutOutsourceWorker(w); err != nil {
		t.Fatalf("put worker: %v", err)
	}
	bound, err := api.dal.GetTask(task.ID)
	if err != nil || bound == nil {
		t.Fatalf("get task: %v", err)
	}
	bound.ExecutorID = workerID
	if err := api.dal.PutTask(*bound); err != nil {
		t.Fatalf("bind task: %v", err)
	}
	if online {
		if _, err := api.hub.Connect(workerID, ""); err != nil {
			t.Fatalf("connect worker SSE: %v", err)
		}
	}
	api.workerSpawnTarget[workerID] = ServerSelfHost
	return workerID
}

func newActiveOnlineWorker(t *testing.T, api *apiServer) string {
	t.Helper()
	return newActiveWorker(t, api, true)
}

func postWorker(t *testing.T, api *apiServer, workerID, op string, body map[string]any,
	h func(http.ResponseWriter, *http.Request, string)) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, taskReq(t, "POST", "/api/outsource-workers/"+workerID+"/"+op, body,
		wireOwnerID, "owner"), workerID)
	return rec
}

func loadForTest(t *testing.T, dal *DAL, cfg Config) (authSettings, []string) {
	t.Helper()
	var logs []string
	settings, err := loadAuthSettings(dal, cfg, func(message string) { logs = append(logs, message) })
	if err != nil {
		t.Fatalf("loadAuthSettings: %v", err)
	}
	return settings, logs
}

func fullMember(id string) Member {
	ok := true
	return Member{
		ID: id, Name: "Mira", Kind: KindStaff, RoleKey: "assistant",
		Runtime: RuntimeClaude, Model: "opus", Effort: "high",
		DesiredState: DesiredStateOnline, DesiredMachineID: "m-abc123",
		WakingSince: 1.5, StoppingSince: 2.5, StoppedSince: 3.5,
		RefocusSince: 4.5, BankedCost: 6.25, LastOp: "start", LastOpOK: &ok,
		LastOpLog:    "spawned",
		LastOpReason: "session_already_exists: tmux session \"member-m-1\" is already live",
		LastOpAt:     7.5, RosterStatus: RosterStatusActive,
	}
}

func putWarden(t *testing.T, s *apiServer, id string) {
	t.Helper()
	putTestMember(t, s, Member{
		ID: id, Name: id, Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOnline, RosterStatus: RosterStatusActive,
	})
}

func sameIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type drainedFrame struct {
	Topic string
	RPC   string
	Args  map[string]any
}

func drainFrames(t *testing.T, s *apiServer, wardenID string) []drainedFrame {
	t.Helper()
	var frames []drainedFrame
	for _, command := range s.hub.DrainWardenCommands(wardenID) {
		text := strings.TrimSpace(strings.TrimPrefix(string(command.Frame), "data: "))
		var envelope struct {
			Topic string `json:"topic"`
			Data  struct {
				RPC  string         `json:"rpc"`
				Args map[string]any `json:"args"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(text), &envelope); err != nil {
			t.Fatalf("frame decode: %v (%q)", err, text)
		}
		frames = append(frames, drainedFrame{
			Topic: envelope.Topic, RPC: envelope.Data.RPC, Args: envelope.Data.Args,
		})
	}
	return frames
}

func connectOnlineMachine(t *testing.T, s *apiServer, memberID, machineID string) *hubListener {
	t.Helper()
	listener, err := s.hub.Connect(memberID, machineID)
	if err != nil {
		t.Fatalf("connect %s@%s: %v", memberID, machineID, err)
	}
	t.Cleanup(func() { s.hub.Disconnect(listener) })
	return listener
}

func drainHubFrames(listener *hubListener) {
	for listener.pop() != nil {
	}
}

func assertNoFrame(t *testing.T, listener *hubListener, what string) {
	t.Helper()
	if raw := listener.pop(); raw != nil {
		_, envelope := parseSSEFrame(t, raw)
		t.Errorf("%s fanned an SSE frame (topic=%v): nothing was written", what, envelope["topic"])
	}
}

func newSettingsTestServer(t *testing.T, password string) (*apiServer, *httptest.Server, *DAL, string) {
	t.Helper()
	dal := newTestDAL(t)
	cfg := defaultConfig()
	cfg.Auth.Password = password
	auth, _ := loadForTest(t, dal, cfg)
	claim, err := ensureFirstRunClaimToken(dal, auth.passwordHash != "", func(string) {})
	if err != nil {
		t.Fatalf("ensureFirstRunClaimToken: %v", err)
	}
	api := newAPIServer(dal, NewHub(), singleKeyring(auth.secret), auth.ownerTokenTTL, "../..")
	api.agentTokenTTL = auth.agentTokenTTL
	api.passwordHash = auth.passwordHash
	api.passwordChangedAt = auth.passwordChangedAt
	api.ctxhigh = auth.ctxhigh
	h, err := buildHandler(specsFor(api), api.keys, dal.GetMember, api.authPasswordChangedAt)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return api, srv, dal, claim
}

const (
	capDropAnchor = "«DROP-THIS-SECTION»"
	capKeepAnchor = "«KEEP-THIS-SECTION»"
)

func capDoc(t *testing.T, n int) string {
	t.Helper()
	body := n - utf8.RuneCountInString(capDropAnchor)
	if body < 2 {
		t.Fatalf("capDoc: n=%d too small", n)
	}
	head := body * 9 / 10
	doc := strings.Repeat("a", head) + capDropAnchor + strings.Repeat("b", body-head)
	if got := utf8.RuneCountInString(doc); got != n {
		t.Fatalf("capDoc built %d runes, want %d", got, n)
	}
	return doc
}

func seedInsightOverlay(t *testing.T, dal *DAL, roleKey, text string) {
	t.Helper()
	if err := dal.PutInsight(Insight{RoleKey: roleKey, Text: text, Tombstoned: false}); err != nil {
		t.Fatalf("PutInsight: %v", err)
	}
}

func getInsightText(t *testing.T, url, token, roleKey string) string {
	t.Helper()
	status, data := doJSON(t, http.MethodGet, url+"/api/insight/"+roleKey, token, "")
	if status != http.StatusOK {
		t.Fatalf("get insight: status %d", status)
	}
	text, _ := data["text"].(string)
	return text
}

func replaceInsight(t *testing.T, srv *httptest.Server, token, bodyText string, allowShrink bool) (int, map[string]any) {
	t.Helper()
	body := map[string]any{"text": bodyText}
	if allowShrink {
		body["allow_shrink"] = true
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return doJSON(t, http.MethodPost, srv.URL+"/api/insight/assistant", token, string(raw))
}

func capDocServer(t *testing.T) (*httptest.Server, *DAL, string) {
	t.Helper()
	srv, dal, secret := newDocCapTestServer(t)
	ownerToken, err := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	if err != nil {
		t.Fatalf("mint owner token: %v", err)
	}
	return srv, dal, ownerToken
}

func capErrMessage(data map[string]any) string {
	env, _ := data["error"].(map[string]any)
	message, _ := env["message"].(string)
	return message
}

// seedManual mints a manual through the real create face and returns its
// server-minted type_key.
func seedManual(t *testing.T, api *apiServer) string {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleCreateTaskManualApiTaskManualsPost(rec, taskReq(t, http.MethodPost,
		"/api/task-manuals", map[string]any{"display_name": "behavior manual"},
		"m-exec", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create manual: %d %s", rec.Code, rec.Body.String())
	}
	var dto struct {
		TypeKey string `json:"type_key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("create response: %v", err)
	}
	return dto.TypeKey
}

func edit(old, newValue string) map[string]any {
	return map[string]any{"old": old, "new": newValue}
}

func updateManual(t *testing.T, api *apiServer, typeKey string, body any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskManualApiTaskManualsTypeKeyPost(rec, taskReq(t, http.MethodPost,
		"/api/task-manuals/"+typeKey, body, "m-exec", "agent"), typeKey)
	return rec
}

func perfReq(sub, scope string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	claims := map[string]any{"sub": sub, "scope": scope}
	return req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))
}

func listManuals(t *testing.T, s *apiServer) []taskManualListItemDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleListTaskManualsApiTaskManualsGet(rec, perfReq("owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("list manuals: %d %s", rec.Code, rec.Body.String())
	}
	var manuals []taskManualListItemDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &manuals); err != nil {
		t.Fatalf("decode manuals: %v", err)
	}
	return manuals
}

func listManualRows(t *testing.T, s *apiServer) []map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleListTaskManualsApiTaskManualsGet(rec, perfReq("owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("list manuals: %d %s", rec.Code, rec.Body.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode manual rows: %v", err)
	}
	return rows
}

// These fixtures are shared by the mainline behavior checks that were moved
// out of ticket-named test files during the canonical test split.
func testAgent(id string) Member {
	return Member{
		ID: id, Name: id, Kind: KindStaff, Effort: "medium",
		DesiredState:     DesiredStateOnline,
		DesiredMachineID: ServerSelfHost,
		RosterStatus:     RosterStatusActive,
	}
}

func putTestMember(t *testing.T, s *apiServer, m Member) {
	t.Helper()
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("put member %s: %v", m.ID, err)
	}
	if err := s.dal.SetMemberWindDownAnchors(m.ID, m.StoppingSince, m.StoppedSince,
		m.RefocusSince, m.RefocusOp); err != nil {
		t.Fatalf("seed wind-down anchors for %s: %v", m.ID, err)
	}
}

func seedMemberAnchors(t *testing.T, s *apiServer, m Member) {
	t.Helper()
	if err := s.dal.SetMemberWindDownAnchors(m.ID, m.StoppingSince, m.StoppedSince,
		m.RefocusSince, m.RefocusOp); err != nil {
		t.Fatalf("seed wind-down anchors for %s: %v", m.ID, err)
	}
}

func seedWorkerAnchors(t *testing.T, s *apiServer, w OutsourceWorker) {
	t.Helper()
	if err := s.dal.SetMemberWindDownAnchors(w.ID, w.StoppingSince, w.StoppedSince,
		w.RefocusSince, w.RefocusOp); err != nil {
		t.Fatalf("seed wind-down anchors for %s: %v", w.ID, err)
	}
}

func connectOnline(t *testing.T, s *apiServer, memberID string) *hubListener {
	t.Helper()
	l, err := s.hub.Connect(memberID, "")
	if err != nil {
		t.Fatalf("connect %s: %v", memberID, err)
	}
	t.Cleanup(func() { s.hub.Disconnect(l) })
	return l
}

// putActiveMember inserts one active roster member for behavior tests that
// exercise a handler directly. It intentionally mirrors only the old fixture's
// seed shape; it does not restore any ticket-named test file.
func putActiveMember(t *testing.T, api *apiServer, id, name, kind string) {
	t.Helper()
	if err := api.dal.PutMember(Member{
		ID: id, Name: name, Kind: kind, Effort: "medium",
		RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("put member %s: %v", id, err)
	}
}

// reassign posts the reassign action with the claims supplied by the test.
func reassign(t *testing.T, api *apiServer, taskID string, body map[string]any, sub, scope string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleReassignTaskApiTasksTaskIdReassignPost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/reassign", body, sub, scope), taskID)
	return rec
}

func memberTarget(memberID string) map[string]any {
	return map[string]any{"target": map[string]any{"kind": "staff", "member_id": memberID}}
}

// recordedOcwardenRun captures the child process seam without launching a
// real warden. The recorder is used by canonical behavior tests for both
// bootstrap-here and teardown-here.
type recordedOcwardenRun struct {
	bin  string
	args []string
	env  []string
}

func withRecordedOcwarden(t *testing.T, exit int) *[]recordedOcwardenRun {
	t.Helper()
	runs := []recordedOcwardenRun{}
	previous := runOcwarden
	runOcwarden = func(bin string, args []string, env []string) (int, string, bool) {
		runs = append(runs, recordedOcwardenRun{bin: bin, args: args, env: env})
		return exit, "fake-ocwarden", false
	}
	t.Cleanup(func() { runOcwarden = previous })
	return &runs
}

// lockedLogBuffer is the smallest log sink needed by the agent iat-floor
// behavior test. The mutex keeps reads race-safe while requireAuth writes.
type lockedLogBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (b *lockedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// bootDocFixture is the wired HTTP fixture used by the document-history
// behavior checks. Only the route-driving fields and method are retained; the
// larger legacy fixture methods are not needed by these canonical files.
type bootDocFixture struct {
	url   string
	owner string
	admin string
}

func newBootDocFixture(t *testing.T) bootDocFixture {
	t.Helper()
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	mint := func(sub, scope string) string {
		tok, err := mintJWT(sub, scope, 300, secret, now, "")
		if err != nil {
			t.Fatalf("mint %s/%s: %v", sub, scope, err)
		}
		return tok
	}
	f := bootDocFixture{
		url:   srv.URL,
		owner: mint("owner", "owner"),
		admin: mint("mira", "agent"),
	}
	if got := classifyMember(&Member{ID: "mira", RoleKey: adminRoleKey}); got != principalAdminAgent {
		t.Fatalf("fixture premise: seeded mira must classify as %q, got %q", principalAdminAgent, got)
	}
	if got := classifyMember(nil); got != principalAgent {
		t.Fatalf("fixture premise: an unknown sub must classify as %q, got %q", principalAgent, got)
	}
	return f
}

func (f bootDocFixture) do(t *testing.T, method, path, token string, body any) (int, string) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = strings.NewReader(string(raw))
	}
	req, err := http.NewRequest(method, f.url+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func get(t *testing.T, url, token string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func doJSON(t *testing.T, method, url, token, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var parsed any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("non-JSON body (%d): %s", resp.StatusCode, raw)
		}
	}
	data, _ := parsed.(map[string]any)
	return resp.StatusCode, data
}

func postIn(t *testing.T, baseURL, token, body string) (int, string) {
	t.Helper()
	return postInWithHeaders(t, baseURL, token, body, nil)
}

func postInWithHeaders(t *testing.T, baseURL, token, body string, headers map[string]string) (int, string) {
	t.Helper()
	url := baseURL + "/in"
	if token != "" {
		url += "?t=" + token
	}
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/plain")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func miraChatBodies(t *testing.T, baseURL, token string) []map[string]any {
	t.Helper()
	status, body := get(t, baseURL+"/api/chat?with=mira", token)
	if status != http.StatusOK {
		t.Fatalf("list chat: want 200, got %d %s", status, body)
	}
	var msgs []map[string]any
	if err := json.Unmarshal(chatEnvelopeMessages(t, []byte(body)), &msgs); err != nil {
		t.Fatalf("chat list not JSON array: %v %s", err, body)
	}
	return msgs
}

func chatEnvelopeMessages(t *testing.T, raw []byte) []byte {
	t.Helper()
	var env struct {
		Messages json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || env.Messages == nil {
		t.Fatalf("GET /api/chat must answer the envelope {messages,...}: %v (%s)", err, raw)
	}
	return env.Messages
}
