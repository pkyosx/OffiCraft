package main

// api_tasks_artifact_url_guard_t95_test.go — T-95: a link artifact's url is
// checked, not merely non-blank. Two rules, both binding NEW writes only:
//
//	scheme  ∈ {https://, http://}   (case-insensitive)
//	length  ≤ artifactURLMaxChars   (2048 Unicode RUNES, not bytes)
//
// 🔴 WHAT THIS FILE PINS, and why each part is load-bearing:
//
//  1. BOTH DOORS, FROM ONE TABLE. The field has two front doors — add and
//     replace — and a guard on one of two is not a guard. Every case below is
//     driven through BOTH by the same loop, so a half-fix (add guarded,
//     replace not) reddens here and cannot ship. The replace door is the one
//     already known to be walked: T-92 measured 429 stored labels over the
//     old 128-char cap, nearly all on replaced rows. (T-92 has since split
//     `label` into `name`/`description` and moved a link's target into a
//     minted text/uri-list blob; the guard stayed on the handler, ahead of
//     mintLinkTargetBlob, so a refused url never reaches the store.)
//  2. THE BOUNDARY, ON BOTH SIDES. 2048 passes, 2049 is refused. A test that
//     only sends something enormous passes against a cap of 100000 too.
//  3. IT COUNTS RUNES, NOT BYTES. A url of 2048 percent-free CJK runes is
//     6144 bytes; if this guard ever counted bytes that case reddens while the
//     ASCII case alone would not have noticed.
//  4. `http` IS IN THE WHITELIST ON PURPOSE. Production carries 6 live http
//     link artifacts (2026-09-06 03:4x, read-only: 706 live links = https 700 /
//     http 6 / other 0, max length 118). Dropping http would make those 6 rows
//     UNEDITABLE, because replace re-validates the url on every call and has no
//     carry-forward branch — see TestArtifactURLReplaceRevalidatesEvenWhenUnchanged.
//  5. THE PAST IS LEFT ALONE. A row written before the guard existed still
//     reads back IN FULL — no migration, no backfill, no truncation. Paired
//     with a live control so that "the old row survived" cannot be produced by
//     the guard simply not running (copied from
//     TestArtifactLabelCapLeavesExistingRowsAlone).
//  6. IT REFUSES, IT DOES NOT REWRITE. A url the server quietly "fixed" points
//     somewhere the caller never asked for.
//
// 🔴 DO NOT DELETE THIS FILE WHILE RESOLVING A MERGE CONFLICT. T-92 (#432)
// removes the `url` COLUMN and moves a link's target into a text/uri-list blob,
// deleting the very two lines this guard sits on (`art.URL = url`,
// `next.URL = url`). All six files those two packages share conflict, so the
// conflict itself is loud — but the RESOLUTION is silent: taking one side
// wholesale drops the guard while both branches were green. This file does not
// participate in that conflict, which is exactly why it can look unrelated and
// be swept away with it. It is the only thing that reddens when the guard is
// gone. Whoever resolves that merge: re-attach artifactLinkURLRefusal to
// whatever now reads body.Url on BOTH doors, then run
// `go test -run TestArtifactURL` and read the DENOMINATOR (5 tests / 31
// subtests), not the word `ok`.

import (
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"
)

// t95URL builds a url of exactly n runes total, padding the path after scheme
// with copies of r — so the ASCII and CJK cases differ ONLY in the rune.
func t95URL(scheme string, r rune, n int) string {
	pad := n - utf8.RuneCountInString(scheme)
	if pad < 0 {
		panic("t95URL: n shorter than the scheme itself")
	}
	return scheme + strings.Repeat(string(r), pad)
}

// t95AssertCorpus is the anti-tautology floor: the fixtures must really be the
// lengths their names claim, and the CJK one must really be multi-byte, or
// "2048 CJK runes pass" proves nothing about bytes-vs-runes.
func t95AssertCorpus(t *testing.T) {
	t.Helper()
	if artifactURLMaxChars != 2048 {
		t.Fatalf("設計定的是 2048（T-95 步驟④），常數是 %d", artifactURLMaxChars)
	}
	cjk := t95URL("https://", '字', artifactURLMaxChars)
	if got := utf8.RuneCountInString(cjk); got != artifactURLMaxChars {
		t.Fatalf("語料不合格：剛好在上限的 CJK url 是 %d 個字", got)
	}
	if len(cjk) <= artifactURLMaxChars {
		t.Fatalf("語料不合格：CJK 樣本只有 %d bytes，不是多位元組，"+
			"這一跑對 bytes-vs-runes 零鑑別力", len(cjk))
	}
	if want := []string{"https://", "http://"}; len(artifactLinkURLSchemes) != len(want) ||
		artifactLinkURLSchemes[0] != want[0] || artifactLinkURLSchemes[1] != want[1] {
		t.Fatalf("白名單被改過：%v —— http 是硬需求（正式站 6 筆），"+
			"要改必須先重跑 T-95 步驟④ 那兩條 SQL", artifactLinkURLSchemes)
	}
}

// t95Doors runs one url through BOTH write doors. `pinned` is the url the
// replace door's artifact starts life with — it must itself be acceptable, so
// the replacement is the only variable.
func t95Doors(t *testing.T, name, url string, wantCode int) {
	t.Helper()
	t.Run(name+"/add", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createAdHocTask(t, api, "m-exec")
		rec := addArtifact(t, api, task.ID,
			map[string]any{"kind": "link", "url": url, "name": "T-95"},
			"m-exec", "agent")
		if rec.Code != wantCode {
			t.Fatalf("add：想要 %d，拿到 %d — %s", wantCode, rec.Code, rec.Body.String())
		}
		t95AssertRefusalIsAboutURL(t, "add", rec.Code, rec.Body.String())
	})
	t.Run(name+"/replace", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createAdHocTask(t, api, "m-exec")
		add := addArtifact(t, api, task.ID,
			map[string]any{"kind": "link", "url": "https://example.invalid/pinned",
				"name": "T-95"},
			"m-exec", "agent")
		if add.Code != http.StatusOK {
			t.Fatalf("語料不合格：連合法的 url 都釘不上去（%d %s）— "+
				"replace 這一格什麼都沒測到", add.Code, add.Body.String())
		}
		artID := getTaskArtifacts(t, api, task.ID).Artifacts[0].ID
		rec := replaceArtifact(t, api, task.ID, artID,
			map[string]any{"url": url}, "m-exec", "agent")
		if rec.Code != wantCode {
			t.Fatalf("replace：想要 %d，拿到 %d — %s", wantCode, rec.Code, rec.Body.String())
		}
		t95AssertRefusalIsAboutURL(t, "replace", rec.Code, rec.Body.String())
	})
}

// t95AssertRefusalIsAboutURL is the anti-tautology guard on the 400s. T-92 put
// a REQUIRED `name` in front of the url check on the add door, so a payload
// that forgot it also 400s — identical status, wholly unrelated reason, and
// every refused case would have passed with the url guard deleted.
func t95AssertRefusalIsAboutURL(t *testing.T, door string, code int, body string) {
	t.Helper()
	if code != http.StatusBadRequest {
		return
	}
	if !strings.Contains(body, "url ") {
		t.Fatalf("%s 這一格拿到 400，但理由不是 url：%s", door, body)
	}
}

// TestArtifactURLSchemeWhitelistBindsBothDoors pins WHAT IS REFUSED and WHAT IS
// NOT. The refused set is not a blacklist of javascript:/data: — it is
// everything outside the whitelist — so the accepted cases are as load-bearing
// as the refused ones.
func TestArtifactURLSchemeWhitelistBindsBothDoors(t *testing.T) {
	t95AssertCorpus(t)
	for _, tc := range []struct {
		name string
		url  string
		want int
	}{
		{"https", "https://github.com/x/y/pull/1", http.StatusOK},
		{"http_is_kept_on_purpose", "http://internal.example/report", http.StatusOK},
		{"scheme_is_case_insensitive", "HTTPS://Example.COM/x", http.StatusOK},
		{"javascript", "javascript:alert(document.cookie)", http.StatusBadRequest},
		{"data", "data:text/html,<script>x()</script>", http.StatusBadRequest},
		{"file", "file:///etc/passwd", http.StatusBadRequest},
		{"scheme_relative", "//evil.example/x", http.StatusBadRequest},
		{"bare_host", "example.com/x", http.StatusBadRequest},
		{"https_without_slashes", "https:evil", http.StatusBadRequest},
	} {
		t95Doors(t, tc.name, tc.url, tc.want)
	}
}

// TestArtifactURLLengthCapIsTwoThousandFortyEightRunes pins the boundary on
// both sides and in both encodings, through both doors.
func TestArtifactURLLengthCapIsTwoThousandFortyEightRunes(t *testing.T) {
	t95AssertCorpus(t)
	for _, tc := range []struct {
		name string
		r    rune
	}{{"ascii", 'a'}, {"cjk", '字'}} {
		t95Doors(t, tc.name+"/at_cap",
			t95URL("https://", tc.r, artifactURLMaxChars), http.StatusOK)
		t95Doors(t, tc.name+"/one_over",
			t95URL("https://", tc.r, artifactURLMaxChars+1), http.StatusBadRequest)
	}
}

// TestArtifactURLRefusalNeverRewrites pins that a refused write changes
// NOTHING: the pinned artifact still carries the url it had. A guard that
// "corrected" the url instead of refusing would leave a 200 and a deliverable
// pointing somewhere the caller never named.
func TestArtifactURLRefusalNeverRewrites(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	const pinned = "https://example.invalid/pinned"
	if rec := addArtifact(t, api, task.ID,
		map[string]any{"kind": "link", "url": pinned, "name": "T-95"},
		"m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("語料不合格：合法 url 沒釘上去（%d）", rec.Code)
	}
	artID := getTaskArtifacts(t, api, task.ID).Artifacts[0].ID
	if rec := replaceArtifact(t, api, task.ID, artID,
		map[string]any{"url": "javascript:alert(1)"}, "m-exec", "agent"); rec.Code != http.StatusBadRequest {
		t.Fatalf("對照組壞了：javascript: 沒被擋（%d）", rec.Code)
	}
	arts := getTaskArtifacts(t, api, task.ID).Artifacts
	if len(arts) != 1 || arts[0].URL != pinned {
		t.Fatalf("被拒絕的寫入動到了資料：url=%q（原本 %q）", arts[0].URL, pinned)
	}
}

// TestArtifactURLReplaceRevalidatesEvenWhenUnchanged is the reason `http` must
// stay in the whitelist, pinned as a test rather than as a comment. Unlike
// name/description, url has NO carry-forward branch: a caller that only means to rename
// the deliverable must send the pinned url back, and it is re-validated. So
// whatever the whitelist refuses, it also makes UNEDITABLE for every row
// already carrying it.
func TestArtifactURLReplaceRevalidatesEvenWhenUnchanged(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	const httpURL = "http://internal.example/report" // one of production's 6
	if rec := addArtifact(t, api, task.ID,
		map[string]any{"kind": "link", "url": httpURL, "name": "舊名"},
		"m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("http 應該是收的（白名單），卻拿到 %d %s", rec.Code, rec.Body.String())
	}
	artID := getTaskArtifacts(t, api, task.ID).Artifacts[0].ID
	// 只想改名字，url 原樣送回 —— 這是那 6 筆唯一的編輯路徑。
	if rec := replaceArtifact(t, api, task.ID, artID,
		map[string]any{"url": httpURL, "name": "新名"},
		"m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("把 http 從白名單拿掉會讓正式站那 6 筆變成改不動：改名被擋（%d %s）",
			rec.Code, rec.Body.String())
	}
	if got := getTaskArtifacts(t, api, task.ID).Artifacts[0].Name; got != "新名" {
		t.Fatalf("改名沒生效：name=%q", got)
	}
}

// TestArtifactURLGuardLeavesExistingRowsAlone pins 「過去先不管」: a row seeded
// straight through the DAL — which the handler-level guard does not bind, and
// deliberately so, because task_artifact_history carries current.URL DB→DB —
// still reads back IN FULL.
//
// 🔴 The live control at the bottom is what stops this test from passing for
// the wrong reason: "the old row survived" is also what you see when the guard
// never ran at all.
func TestArtifactURLGuardLeavesExistingRowsAlone(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	legacy := "ftp://legacy.example/" + strings.Repeat("x", artifactURLMaxChars*2)
	// T-92 moved a link's target out of the `url` COLUMN and into a minted
	// text/uri-list blob, so seeding a pre-guard row means minting that blob
	// exactly as the handler does — PutTaskArtifactMintingBlob is the same DAL
	// door the API uses, and it is BELOW the handler guard, which is the point.
	legacyAtt, legacyBlob := mintLinkTargetBlob(legacy)
	if err := api.dal.PutTaskArtifactMintingBlob(TaskArtifact{
		ID: "ta-legacyurl01", TaskID: task.ID, Kind: ArtifactKindLink,
		AttachmentID: legacyAtt, Name: "舊列", CreatedTS: 1000, CreatedBy: "m-exec",
	}, legacyBlob); err != nil {
		t.Fatal(err)
	}
	arts := getTaskArtifacts(t, api, task.ID).Artifacts
	if len(arts) != 1 {
		t.Fatalf("語料不合格：舊列沒種進去，artifacts=%d", len(arts))
	}
	if arts[0].URL != legacy {
		t.Fatalf("既有資料不得被動到：讀回 %d 字，原本 %d 字",
			utf8.RuneCountInString(arts[0].URL), utf8.RuneCountInString(legacy))
	}
	// 反恆真：同一台 server 上，新的違規寫入確實還是被擋著。
	if rec := addArtifact(t, api, task.ID,
		map[string]any{"kind": "link", "url": "ftp://new.example/x", "name": "T-95"},
		"m-exec", "agent"); rec.Code != http.StatusBadRequest {
		t.Fatalf("對照組壞了：新的違規 url 也沒被擋（%d）— 這一跑什麼都沒證明", rec.Code)
	}
}
