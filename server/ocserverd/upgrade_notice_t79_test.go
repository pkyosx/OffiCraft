package main

// upgrade_notice_t79_test.go — guards for the post-upgrade migration notice
// (T-79). The properties under test are the ones whose failure would be
// SILENT in production: a message that reassures when it knows nothing, a
// notice delivered by the process that never actually upgraded, and a FROM
// side lost when two upgrades land back to back.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func t79Server(t *testing.T) *apiServer {
	t.Helper()
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	if err := s.dal.PutMember(Member{
		ID: seedMiraID, Name: "銀月", Kind: KindStaff, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("seed assistant: %v", err)
	}
	return s
}

// t79CompareServer stands in for GitHub's compare endpoint.
func t79CompareServer(t *testing.T, files []string) *httptest.Server {
	t.Helper()
	type file struct {
		Filename string `json:"filename"`
		Status   string `json:"status"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := struct {
			Files []file `json:"files"`
		}{}
		for _, f := range files {
			body.Files = append(body.Files, file{Filename: f, Status: "modified"})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// ─── the message ───────────────────────────────────────────────────────────

// A failed lookup must not be reported in a way that reads like "nothing
// important changed". The two states are opposite and the reader acts
// differently on each, so the wording is load-bearing, not cosmetic.
func TestUpgradeNoticeBody_ALookupFailureNeverReadsLikeAllClear(t *testing.T) {
	n := pendingUpgradeNotice{FromVersion: "v0.5.310", FromSHA: "ad414a8c5be1", ToVersion: "v0.5.312", ToSHA: "e0920b5b57ba"}
	body := upgradeNoticeBody(n, nil, false, errors.New("github answered 502"))

	if !strings.Contains(body, "github answered 502") {
		t.Errorf("the reason the list is missing is not in the message:\n%s", body)
	}
	if strings.Contains(body, "未動到") {
		t.Errorf("a failed lookup claimed the shared layer was untouched:\n%s", body)
	}
	if !strings.Contains(body, "v0.5.310") || !strings.Contains(body, "v0.5.312") {
		t.Errorf("both versions must survive a failed lookup:\n%s", body)
	}
	if !strings.Contains(body, upgradeCompareURL(n.FromSHA, n.ToSHA)) {
		t.Errorf("the reader has no link to do the judging by hand:\n%s", body)
	}
}

// The boring upgrade is the common case (six of eight measured in one day).
// It has to stay short, or the reader learns to skip the whole channel.
func TestUpgradeNoticeBody_AnUpgradeBelowTheSharedLayerStaysOneLine(t *testing.T) {
	n := pendingUpgradeNotice{FromVersion: "v0.5.311", FromSHA: "b4253267", ToVersion: "v0.5.312", ToSHA: "e0920b5b"}
	body := upgradeNoticeBody(n, []string{"server/ocserverd/upgrade.go", "frontend/src/App.tsx"}, false, nil)

	if !strings.HasPrefix(body, "⚪") {
		t.Errorf("the quiet case must be marked as quiet:\n%s", body)
	}
	if strings.Contains(body, "\n") {
		t.Errorf("the quiet case grew past one line:\n%s", body)
	}
	if !strings.Contains(body, "未動到") || !strings.Contains(body, "共 2 個檔案") {
		t.Errorf("the quiet line must still say what it checked:\n%s", body)
	}
}

// When the shared layer moves, the files that caused it lead — and they are
// never elided, whatever else is in the diff.
func TestUpgradeNoticeBody_ASharedLayerChangeLeadsWithTheFilesThatCausedIt(t *testing.T) {
	files := []string{"server/ocserverd/api_chat.go", "seeds/system_interaction.md", "spec/mcp-catalog.json"}
	n := pendingUpgradeNotice{FromVersion: "v0.5.309", FromSHA: "272d80b2", ToVersion: "v0.5.310", ToSHA: "ad414a8c"}
	body := upgradeNoticeBody(n, files, false, nil)

	if !strings.HasPrefix(body, "🔴") {
		t.Errorf("a shared-layer change must be marked loudly:\n%s", body)
	}
	for _, want := range []string{"seeds/system_interaction.md", "spec/mcp-catalog.json"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing the shared-layer file %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "server/ocserverd/api_chat.go") {
		t.Errorf("the rest of the diff is material too:\n%s", body)
	}
	if !strings.Contains(body, "請你判斷並執行") {
		t.Errorf("the message must ask for a judgement, not state a conclusion:\n%s", body)
	}
}

// A list that sits on GitHub's ceiling is a floor, not the whole set. Both
// shapes must say so — the loud one AND the quiet one, because the quiet one
// is the one that would otherwise assert "untouched" on partial evidence.
func TestUpgradeNoticeBody_ATruncatedListIsDeclaredInBothShapes(t *testing.T) {
	n := pendingUpgradeNotice{FromVersion: "v0.5.300", FromSHA: "aaaaaaaa", ToVersion: "v0.5.312", ToSHA: "bbbbbbbb"}

	quiet := upgradeNoticeBody(n, []string{"server/ocserverd/x.go"}, true, nil)
	if !strings.Contains(quiet, "上限") {
		t.Errorf("the quiet shape asserted 未動到 on a truncated list without saying so:\n%s", quiet)
	}

	loud := upgradeNoticeBody(n, []string{"seeds/system_interaction.md"}, true, nil)
	if !strings.Contains(loud, "這份清單不完整") {
		t.Errorf("the loud shape hid the truncation:\n%s", loud)
	}
}

// The long tail is capped, and the cap announces itself rather than quietly
// ending the list.
func TestUpgradeNoticeBody_TheLongTailIsCappedAndSaysHowManyItDropped(t *testing.T) {
	files := []string{"seeds/system_interaction.md"}
	for i := 0; i < upgradeNoticeMaxListed+7; i++ {
		files = append(files, fmt.Sprintf("server/ocserverd/f%02d.go", i))
	}
	body := upgradeNoticeBody(pendingUpgradeNotice{FromSHA: "aaaaaaaa", ToSHA: "bbbbbbbb"}, files, false, nil)

	if !strings.Contains(body, "另外 7 個") {
		t.Errorf("the elided remainder was not counted out loud:\n%s", body)
	}
}

// ─── which paths count as the shared layer ─────────────────────────────────

func TestSharedLayerFiles_MatchesTheDirectoriesAndNotTheirLookalikes(t *testing.T) {
	got := sharedLayerFiles([]string{
		"seeds/system_interaction.md",
		"spec/openapi.json",
		"myseeds/thing.md",    // not the shared layer
		"docs/seeds.md",       // documentation ABOUT it, not it
		"server/spec_test.go", // substring, not a prefix
	})
	want := []string{"seeds/system_interaction.md", "spec/openapi.json"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("shared layer = %v, want %v", got, want)
	}
}

// ─── the compare fetch ─────────────────────────────────────────────────────

func TestFetchUpgradeChangedFiles_ReadsTheFilenamesAndReportsNoTruncation(t *testing.T) {
	srv := t79CompareServer(t, []string{"a.go", "seeds/b.md"})
	files, truncated, err := fetchUpgradeChangedFiles(srv.URL, "aaaa", "bbbb")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if strings.Join(files, ",") != "a.go,seeds/b.md" {
		t.Errorf("files = %v", files)
	}
	if truncated {
		t.Error("a two-file compare was reported as truncated")
	}
}

func TestFetchUpgradeChangedFiles_AFullPageIsReportedAsPossiblyIncomplete(t *testing.T) {
	var many []string
	for i := 0; i < githubCompareFileCeiling; i++ {
		many = append(many, fmt.Sprintf("f%03d.go", i))
	}
	srv := t79CompareServer(t, many)
	_, truncated, err := fetchUpgradeChangedFiles(srv.URL, "aaaa", "bbbb")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !truncated {
		t.Error("a response sitting exactly on GitHub's ceiling was reported as complete")
	}
}

func TestFetchUpgradeChangedFiles_RefusesRatherThanGuessWhenACommitIsMissing(t *testing.T) {
	if _, _, err := fetchUpgradeChangedFiles("http://127.0.0.1:1", "", "bbbb"); err == nil {
		t.Error("an empty base commit produced no error")
	}
}

func TestFetchUpgradeChangedFiles_ANonOKAnswerIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	if _, _, err := fetchUpgradeChangedFiles(srv.URL, "aaaa", "bbbb"); err == nil {
		t.Error("a 403 was treated as an empty file list")
	}
}

// ─── the durable marker ────────────────────────────────────────────────────

// Two upgrades before a delivery must still describe the whole journey. If
// the second record overwrote the FROM side, the surviving message would
// compare the new build against the intermediate one nobody ever heard about
// — and the file list would silently lose everything in the first hop.
//
// The setup is the case that actually produces it: the station upgrades A→B,
// comes up on B, and its delivery FAILS (the chat write is the one step that
// can), so the A→B marker is deliberately kept. It then upgrades B→C, and by
// now the running build is B, not A. Reading the FROM side off the running
// process at that moment is what loses the first hop.
func TestRecordPendingUpgradeNotice_KeepsTheOriginalFromAcrossASecondUpgrade(t *testing.T) {
	s := t79Server(t)
	s.processSHA = "aaaaaaaaaaaa"
	s.recordPendingUpgradeNotice("v0.5.311", "bbbbbbbbbbbb")
	// It came up on B; the notice is still pending because delivery failed.
	s.processSHA = "bbbbbbbbbbbb"
	s.recordPendingUpgradeNotice("v0.5.312", "cccccccccccc")

	got, err := s.readPendingUpgradeNotice()
	if err != nil || got == nil {
		t.Fatalf("read back: %v (notice=%v)", err, got)
	}
	if got.FromSHA != "aaaaaaaaaaaa" {
		t.Errorf("FromSHA = %q, want the ORIGINAL station build", got.FromSHA)
	}
	if got.ToVersion != "v0.5.312" || got.ToSHA != "cccccccccccc" {
		t.Errorf("TO side = %s/%s, want the latest", got.ToVersion, got.ToSHA)
	}
}

// ─── delivery ──────────────────────────────────────────────────────────────

func TestDeliverPendingUpgradeNotice_WithNothingPendingItSendsNothing(t *testing.T) {
	s := t79Server(t)
	if s.deliverPendingUpgradeNotice() {
		t.Error("a station with no pending notice sent a message")
	}
}

// 🔴 The property that makes the past tense honest. syscall.Exec can fail;
// the OLD build then keeps serving with the marker still on disk. Announcing
// "this station is now on X" from a process that is not on X is a lie, and
// deleting the marker there would lose the story for the boot that really
// does come up on X.
func TestDeliverPendingUpgradeNotice_TheOldBuildDoesNotAnnounceAnUpgradeItDidNotTake(t *testing.T) {
	s := t79Server(t)
	s.processSHA = "aaaaaaaaaaaa"
	s.recordPendingUpgradeNotice("v0.5.312", "cccccccccccc")

	if s.deliverPendingUpgradeNotice() {
		t.Fatal("the process still running the OLD build announced the upgrade")
	}
	got, err := s.readPendingUpgradeNotice()
	if err != nil || got == nil {
		t.Fatalf("the notice was dropped instead of left pending: %v (notice=%v)", err, got)
	}
}

func TestDeliverPendingUpgradeNotice_TheNewBuildTellsTheAssistantAndClearsTheMarker(t *testing.T) {
	srv := t79CompareServer(t, []string{"seeds/system_interaction.md", "server/ocserverd/upgrade.go"})
	s := t79Server(t)
	s.releaseAPIBase = srv.URL
	s.processSHA = "aaaaaaaaaaaa"
	s.recordPendingUpgradeNotice("v0.5.312", "cccccccccccc")
	// The station is now the NEW build: same marker, different running sha.
	s.processSHA = "cccccccccccc"

	if !s.deliverPendingUpgradeNotice() {
		t.Fatal("the upgraded build sent nothing")
	}
	msgs, err := s.dal.ListChatInvolving(seedMiraID, 10)
	if err != nil {
		t.Fatalf("list chat: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("assistant received %d messages, want 1", len(msgs))
	}
	if msgs[0].Sender != wireSystemSender || msgs[0].Recipient != seedMiraID {
		t.Errorf("message routed as %s → %s", msgs[0].Sender, msgs[0].Recipient)
	}
	if !strings.Contains(msgs[0].Body, "seeds/system_interaction.md") {
		t.Errorf("the material never reached the message:\n%s", msgs[0].Body)
	}
	got, err := s.readPendingUpgradeNotice()
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != nil {
		t.Error("the marker survived delivery — the next boot would repeat the message")
	}
}

// GitHub being unreachable must cost the file list, not the message: the
// versions and the link alone still let the reader do the judging.
func TestDeliverPendingUpgradeNotice_AnUnreachableGitHubStillGetsAMessageOut(t *testing.T) {
	s := t79Server(t)
	s.releaseAPIBase = "http://127.0.0.1:1"
	s.processSHA = "aaaaaaaaaaaa"
	s.recordPendingUpgradeNotice("v0.5.312", "cccccccccccc")
	s.processSHA = "cccccccccccc"

	if !s.deliverPendingUpgradeNotice() {
		t.Fatal("an unreachable GitHub silenced the notice entirely")
	}
	msgs, err := s.dal.ListChatInvolving(seedMiraID, 10)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("assistant received %d messages (err=%v), want 1", len(msgs), err)
	}
	if strings.Contains(msgs[0].Body, "未動到") {
		t.Errorf("a message with no file list claimed the shared layer was untouched:\n%s", msgs[0].Body)
	}
}

// ─── the wiring ────────────────────────────────────────────────────────────
//
// 🔴 These two exist because the independent review PROVED they were missing:
// with both call sites deleted the whole package still went green, so the
// feature could be unplugged entirely and CI would call it fine. Everything
// above tests the machinery; these two test that anything calls it.

// The upgrade path must LEAVE the marker behind. Delete the one line in
// runUpgrade and this is the test that notices.
func TestUpgradeRecordsAPendingNoticeForTheNextBoot(t *testing.T) {
	api, srvURL, token, _, restarted := newUpgradeTestServer(t)
	gh := githubWithRelease(t, "v0.9.0", smokePassingBinary, "", true)
	pointAtGitHub(t, api, gh)

	if before, err := api.readPendingUpgradeNotice(); err != nil || before != nil {
		t.Fatalf("precondition: a marker exists before any upgrade (%v, %v)", before, err)
	}
	status, data := doJSON(t, "POST", srvURL+"/api/update/upgrade", token, "")
	if status != 200 {
		t.Fatalf("valid upgrade must 200: %d %v", status, data)
	}
	select {
	case <-restarted:
	case <-time.After(3 * time.Second):
		t.Fatal("restart never fired — the upgrade did not complete")
	}

	got, err := api.readPendingUpgradeNotice()
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if got == nil {
		t.Fatal("a completed upgrade left NO pending notice — nothing will ever tell the assistant it happened")
	}
	if got.ToVersion != "v0.9.0" {
		t.Errorf("marker names %q as the new version, want v0.9.0", got.ToVersion)
	}
}

// And boot must LOOK for it and say so. Delete the one line in cmdServe and
// this notices.
//
// It asserts the boot LINE rather than the delivered message on purpose: the
// held-port pattern makes serve exit right after the bind fails, which closes
// the pools out from under any background work, so an assertion on the
// message would be a race. The line is written synchronously, before the
// goroutine exists — that is exactly why the look was made synchronous.
func TestServeAnnouncesAPendingUpgradeNoticeOnBoot(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "oc.db")

	seedDB, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	if err := runMigrations(seedDB); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	seed := NewDAL(seedDB)
	if err := seed.PutMember(Member{
		ID: seedMiraID, Name: "\u9280\u6708", Kind: KindStaff, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("seed assistant: %v", err)
	}
	blob, _ := json.Marshal(pendingUpgradeNotice{
		FromVersion: "v0.5.311", FromSHA: "aaaaaaaaaaaa",
		ToVersion: "v0.5.312", ToSHA: "cccccccccccc", RecordedTS: 1,
	})
	if err := seed.PutSetting(pendingUpgradeNoticeKey, string(blob)); err != nil {
		t.Fatalf("seed marker: %v", err)
	}
	if err := seedDB.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	// Hold the port so the boot runs and the bind is what ends it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	cfgPath := filepath.Join(dir, "oc.toml")
	if err := os.WriteFile(cfgPath,
		[]byte(fmt.Sprintf("[server]\nport = %d\n", ln.Addr().(*net.TCPAddr).Port)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var out strings.Builder
	if rc := cmdServe(envOf(map[string]string{
		"OC_CONFIG":       cfgPath,
		"OC_DATABASE_URL": "sqlite:///" + dbPath,
	}), true, true, &out); rc != 1 {
		t.Fatalf("the held port must make serve exit 1 (boot ran, bind failed), got %d\n%s", rc, out.String())
	}
	if !strings.Contains(out.String(), "already in use") {
		t.Fatalf("serve exited for some earlier reason, so the boot under test never happened:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "[upgrade-notice] pending notice for v0.5.312") {
		t.Fatalf("serve booted with a pending upgrade notice and never looked at it — "+
			"the boot mount is missing, so no upgrade will ever be announced:\n%s", out.String())
	}
}

// A boot with nothing pending must stay quiet: this channel only works while
// its lines mean something.
func TestServeSaysNothingWhenNoUpgradeIsPending(t *testing.T) {
	s := t79Server(t)
	var out strings.Builder
	s.startUpgradeNoticeDelivery(&out)
	if out.String() != "" {
		t.Errorf("an ordinary boot printed an upgrade notice line: %q", out.String())
	}
}

// A marker that cannot be decoded would otherwise be invisible forever —
// unreadable every boot, delivered never, cleared never.
func TestServeSaysSoWhenThePendingNoticeCannotBeRead(t *testing.T) {
	s := t79Server(t)
	if err := s.dal.PutSetting(pendingUpgradeNoticeKey, "{not json"); err != nil {
		t.Fatalf("seed corrupt marker: %v", err)
	}
	var out strings.Builder
	s.startUpgradeNoticeDelivery(&out)
	if !strings.Contains(out.String(), "cannot be read") {
		t.Errorf("a corrupt marker booted silently: %q", out.String())
	}
	if s.deliverPendingUpgradeNotice() {
		t.Error("a corrupt marker produced a message anyway")
	}
}

// ─── what the independent review found ─────────────────────────────────────

// 🔴 A bare DeleteSetting here loses an upgrade silently. Delivery can sit on
// GitHub for upgradeCompareTimeout, and the owner's explicit upgrade takes no
// part in this file's locking — so a swap can land, record its marker, and be
// wiped by the delete belonging to the notice that was still being delivered.
func TestClearDeliveredUpgradeNotice_ANewerUpgradeRecordedMidDeliverySurvives(t *testing.T) {
	s := t79Server(t)
	s.processSHA = "aaaaaaaaaaaa"
	s.recordPendingUpgradeNotice("v0.5.311", "bbbbbbbbbbbb")
	delivered, err := s.readPendingUpgradeNotice()
	if err != nil || delivered == nil {
		t.Fatalf("seed marker: %v (%v)", err, delivered)
	}
	// While that one is "being delivered", a newer upgrade lands.
	s.recordPendingUpgradeNotice("v0.5.312", "cccccccccccc")

	s.clearDeliveredUpgradeNotice(*delivered)

	got, err := s.readPendingUpgradeNotice()
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got == nil {
		t.Fatal("the newer upgrade's marker was wiped by the older notice's delete — " +
			"that upgrade will never be announced, and nothing records that it was lost")
	}
	if got.ToVersion != "v0.5.312" {
		t.Errorf("surviving marker names %q, want the newer v0.5.312", got.ToVersion)
	}
}

// An unstamped build records the literal "unknown" as its sha. A compare link
// built from it 404s — in a message whose entire value IS that link.
func TestUpgradeNoticeBody_AnUnstampedBuildIsNotHandedADeadLink(t *testing.T) {
	n := pendingUpgradeNotice{FromVersion: "v0.5.311", FromSHA: "unknown", ToVersion: "v0.5.312", ToSHA: "cccccccccccc"}
	for _, body := range []string{
		upgradeNoticeBody(n, nil, false, errors.New("both commits must be known")),
		upgradeNoticeBody(n, []string{"seeds/system_interaction.md"}, false, nil),
		upgradeNoticeBody(n, []string{"server/x.go"}, false, nil),
	} {
		if strings.Contains(body, "compare/unknown") {
			t.Errorf("handed out a link that 404s:\n%s", body)
		}
		if !strings.Contains(body, "commit 不明") {
			t.Errorf("the reader is not told the starting point is unknown:\n%s", body)
		}
	}
}

// ...and the station must still SAY something. The "did the swap take" guard
// compares two shas; on an unstamped build both are the same literal, and a
// naive comparison would silence this station's notices forever.
func TestDeliverPendingUpgradeNotice_AnUnstampedBuildIsNotSilencedForever(t *testing.T) {
	s := t79Server(t)
	s.processSHA = "unknown"
	s.recordPendingUpgradeNotice("v0.5.312", "unknown")

	if !s.deliverPendingUpgradeNotice() {
		t.Fatal("an unstamped build never announces any upgrade at all")
	}
	msgs, err := s.dal.ListChatInvolving(seedMiraID, 10)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("assistant received %d messages (err=%v), want 1", len(msgs), err)
	}
}

// No assistant on the roster is not an error — but it must not consume the
// notice either, or the upgrade is lost the moment she is re-hired.
func TestDeliverPendingUpgradeNotice_WithNoAssistantTheNoticeIsKept(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()} // deliberately no mira
	s.releaseAPIBase = "http://127.0.0.1:1"
	s.processSHA = "aaaaaaaaaaaa"
	s.recordPendingUpgradeNotice("v0.5.312", "cccccccccccc")
	s.processSHA = "cccccccccccc"

	if s.deliverPendingUpgradeNotice() {
		t.Fatal("claimed to have delivered a message with nobody to deliver it to")
	}
	got, err := s.readPendingUpgradeNotice()
	if err != nil || got == nil {
		t.Fatalf("the notice was consumed even though nobody received it: %v (%v)", err, got)
	}
}

// A compare that reports no differing files at all is unusual for an upgrade.
// Printing a bare "0 files" reads like "nothing happened"; it must be named.
func TestUpgradeNoticeBody_AnEmptyDiffIsCalledOutRatherThanPrintedAsZero(t *testing.T) {
	n := pendingUpgradeNotice{FromVersion: "v0.5.311", FromSHA: "aaaaaaaa", ToVersion: "v0.5.312", ToSHA: "bbbbbbbb"}
	body := upgradeNoticeBody(n, nil, false, nil)
	if strings.Contains(body, "共 0 個檔案") {
		t.Errorf("an empty diff was printed as a bare zero:\n%s", body)
	}
	if !strings.Contains(body, "不尋常") {
		t.Errorf("an empty diff was not flagged as unusual:\n%s", body)
	}
}

// 🔴 The half of the wiring the first review round left uncovered: the two
// call sites were pinned, but the `go deliver()` INSIDE the boot look could be
// deleted and everything stayed green — while the boot line went on announcing
// a delivery that never happened. A boot that lies is worse than a silent one.
func TestServeActuallyDispatchesTheDeliveryItAnnounces(t *testing.T) {
	s := t79Server(t)
	s.processSHA = "aaaaaaaaaaaa"
	s.recordPendingUpgradeNotice("v0.5.312", "cccccccccccc")

	called := make(chan struct{}, 1)
	s.upgradeNoticeDeliver = func() bool {
		called <- struct{}{}
		return true
	}
	var out strings.Builder
	s.startUpgradeNoticeDelivery(&out)

	if !strings.Contains(out.String(), "delivering to the assistant") {
		t.Fatalf("precondition: the boot did not announce a delivery: %q", out.String())
	}
	select {
	case <-called:
	case <-time.After(3 * time.Second):
		t.Fatal("the boot announced a delivery it never dispatched — the message is never sent, " +
			"and the boot line says otherwise")
	}
}

// The quiet line must read as a sentence even when there is no link to offer.
func TestUpgradeNoticeBody_TheQuietLineIsNotAMalformedSentenceWithoutALink(t *testing.T) {
	n := pendingUpgradeNotice{FromVersion: "v0.5.311", FromSHA: "unknown", ToVersion: "v0.5.312", ToSHA: "unknown"}
	body := upgradeNoticeBody(n, []string{"server/x.go"}, false, nil)
	if strings.Contains(body, "點 （") {
		t.Errorf("the quiet line tells the reader to click something that is not a link:\n%s", body)
	}
}
