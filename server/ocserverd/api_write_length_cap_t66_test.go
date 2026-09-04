package main

// api_write_length_cap_t66_test.go — T-66 ②: the two SHORT NAMING fields are
// capped at 128 CHARACTERS on the write side.
//
// Owner, verbatim: 「過去先不管 新的都要限制長度」 (c-5d058a53ef74),
// 「128字元」 (c-92c734ef561e), 「不用在錯誤訊息寫」 (c-b9bb4cfde26a).
// 執行者判斷 (T-66) picked the two fields the cap binds — a task artifact's
// label and a chat attachment's filename — and the shape of the guard (copied
// from the chatBodyMaxChars refusal in api_chat.go).
//
// 🔴 WHAT THIS FILE PINS, and why each part is load-bearing:
//
//   1. THE BOUNDARY ITSELF, on both sides. 128 passes, 129 is refused. A test
//      that only sends something enormous passes against a cap of 4000 too.
//   2. IT COUNTS CHARACTERS, NOT BYTES. 128 CJK characters are 384 bytes; if
//      any of these guards ever counted bytes, that case reddens and the ASCII
//      cases alone would not have noticed.
//   3. IT REFUSES, IT DOES NOT TRUNCATE. A silently shortened name is worse
//      than a refusal: it still names the wrong thing, and nobody is told.
//   4. THE PAST IS LEFT ALONE. An over-length row written before the cap
//      existed still reads back IN FULL — no migration, no backfill, no
//      truncation on read. That is the 「過去先不管」 half of the ruling, and it
//      is the half a later "let's just clean the data" change would break.
//   5. THE MESSAGE SAYS THE NUMBER AND NOTHING ELSE. It must not tell the
//      caller where to put the text instead — owner said not to.
//
// The chat BODY cap (4,000) is a different cap with a different message and
// lives in api_chat_test.go; nothing here should be read as being about it.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// t66Runes builds a string of exactly n copies of r — the corpus generator for
// both the ASCII and the CJK side, so the two cases differ ONLY in the rune.
func t66Runes(r rune, n int) string {
	return strings.Repeat(string(r), n)
}

// t66AssertCapCorpus is the anti-tautology floor: the fixtures this file feeds
// the guards must really be the lengths their names claim, and the CJK one must
// really be multi-byte, or "128 CJK characters pass" proves nothing about
// bytes-vs-runes.
func t66AssertCapCorpus(t *testing.T) {
	t.Helper()
	if shortLabelMaxChars != 128 {
		t.Fatalf("owner said 128 字元 (c-92c734ef561e); the constant is %d",
			shortLabelMaxChars)
	}
	cjk := t66Runes('字', shortLabelMaxChars)
	if utf8.RuneCountInString(cjk) != 128 {
		t.Fatalf("語料不合格:CJK 樣本不是 128 個字,而是 %d",
			utf8.RuneCountInString(cjk))
	}
	// 這一句就是「不是 byte」的證據:若守衛用 byte 判,這 384 bytes 會被擋。
	if len(cjk) <= shortLabelMaxChars {
		t.Fatalf("語料不合格:CJK 樣本只有 %d bytes,和 rune 數分不開,"+
			"這一跑證明不了守衛數的是字不是 byte", len(cjk))
	}
}

// ── the artifact label ──────────────────────────────────────────────────────

// t66AddLabel pins one link artifact carrying `label` and returns the recorder.
func t66AddLabel(t *testing.T, api *apiServer, taskID, label string) *httptest.ResponseRecorder {
	t.Helper()
	return addArtifact(t, api, taskID, map[string]any{
		"kind": "link", "url": "https://github.com/x/y/pull/1", "label": label,
	}, "m-exec", "agent")
}

// TestArtifactLabelCapIsOneHundredTwentyEightRunes pins the boundary on the
// artifact label: 128 in, 129 out, in ASCII and in CJK.
func TestArtifactLabelCapIsOneHundredTwentyEightRunes(t *testing.T) {
	t66AssertCapCorpus(t)
	for _, tc := range []struct {
		name string
		r    rune
	}{{"ascii", 'a'}, {"cjk", '字'}} {
		t.Run(tc.name, func(t *testing.T) {
			api := newTasksTestServer(t)
			task := createAdHocTask(t, api, "m-exec")

			atCap := t66Runes(tc.r, shortLabelMaxChars)
			rec := t66AddLabel(t, api, task.ID, atCap)
			if rec.Code != http.StatusOK {
				t.Fatalf("%d 個字的 label 應該過,得到 %d %s",
					shortLabelMaxChars, rec.Code, rec.Body.String())
			}
			// 而且是原樣存下,沒有被偷偷截短。
			view := getTaskView(t, api, task.ID)
			if len(view.Artifacts) != 1 || view.Artifacts[0].Label != atCap {
				t.Fatalf("剛好在上限的 label 應原樣存下(%d 字),得到 %d 字",
					utf8.RuneCountInString(atCap),
					utf8.RuneCountInString(view.Artifacts[0].Label))
			}

			over := t66Runes(tc.r, shortLabelMaxChars+1)
			rec = t66AddLabel(t, api, task.ID, over)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%d 個字的 label 應該被擋(400),得到 %d %s",
					shortLabelMaxChars+1, rec.Code, rec.Body.String())
			}
			// 拒絕就是拒絕:不得靜默截斷後照樣寫進去。
			if got := getTaskView(t, api, task.ID); len(got.Artifacts) != 1 {
				t.Fatalf("被拒的 label 不該留下任何一列:artifacts=%d",
					len(got.Artifacts))
			}
			// 訊息要說長度與上限 —— 而且只說這個(owner c-b9bb4cfde26a
			// 「不用在錯誤訊息寫」該把字放哪)。
			body := rec.Body.String()
			for _, want := range []string{
				strconv.Itoa(shortLabelMaxChars + 1), strconv.Itoa(shortLabelMaxChars),
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("拒絕訊息該說出 %q,得到 %s", want, body)
				}
			}
			for _, forbidden := range []string{"attachment (", "ocagent", "instead"} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("拒絕訊息不該教人把字寫去哪(出現 %q):%s", forbidden, body)
				}
			}
		})
	}
}

// TestArtifactLabelCapLeavesExistingRowsAlone pins 「過去先不管」: a row written
// before the cap existed (here: straight through the DAL, which the cap does not
// bind) still reads back IN FULL. Nothing migrates, backfills or truncates it.
func TestArtifactLabelCapLeavesExistingRowsAlone(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	legacy := t66Runes('舊', shortLabelMaxChars*3)
	if err := api.dal.PutTaskArtifact(TaskArtifact{
		ID: "ta-legacy0001", TaskID: task.ID, Kind: ArtifactKindLink,
		URL: "https://example.invalid/old", Label: legacy,
		CreatedTS: 1000, CreatedBy: "m-exec",
	}); err != nil {
		t.Fatal(err)
	}
	view := getTaskView(t, api, task.ID)
	if len(view.Artifacts) != 1 {
		t.Fatalf("語料不合格:舊列沒有種進去,artifacts=%d", len(view.Artifacts))
	}
	if got := view.Artifacts[0].Label; got != legacy {
		t.Fatalf("既有資料不得被動到:讀回 %d 字,原本 %d 字",
			utf8.RuneCountInString(got), utf8.RuneCountInString(legacy))
	}
	// 反恆真:同一台 server 上,新的寫入確實還是被擋著 —— 否則「舊的沒動」
	// 也可能只是守衛整條沒生效。
	if rec := t66AddLabel(t, api, task.ID,
		t66Runes('新', shortLabelMaxChars+1)); rec.Code != http.StatusBadRequest {
		t.Fatalf("對照組壞了:新的超長 label 也沒被擋(%d)— 這一跑什麼都沒證明",
			rec.Code)
	}
}

// ── the attachment filename ─────────────────────────────────────────────────

// TestAttachmentFilenameCapIsOneHundredTwentyEightRunes pins the boundary on the
// SHARED seam (resolveChatAttachment), which is what both the inline base64 path
// and the `ocagent upload` streaming path call — so one assertion binds both.
func TestAttachmentFilenameCapIsOneHundredTwentyEightRunes(t *testing.T) {
	t66AssertCapCorpus(t)
	for _, tc := range []struct {
		name string
		r    rune
	}{{"ascii", 'a'}, {"cjk", '檔'}} {
		t.Run(tc.name, func(t *testing.T) {
			atCap := t66Runes(tc.r, shortLabelMaxChars-4) + ".txt" // 128 runes total
			if utf8.RuneCountInString(atCap) != shortLabelMaxChars {
				t.Fatalf("語料不合格:剛好在上限的檔名是 %d 字",
					utf8.RuneCountInString(atCap))
			}
			att, err := resolveChatAttachment([]byte("hello"), atCap, "text/plain")
			if err != nil {
				t.Fatalf("%d 個字的檔名應該過,得到 %v", shortLabelMaxChars, err)
			}
			if att.Filename == nil || *att.Filename != atCap {
				t.Fatalf("剛好在上限的檔名應原樣存下,得到 %v", att.Filename)
			}

			over := t66Runes(tc.r, shortLabelMaxChars-3) + ".txt" // 129 runes
			att, err = resolveChatAttachment([]byte("hello"), over, "text/plain")
			if err == nil {
				t.Fatalf("%d 個字的檔名應該被擋,卻收下了 %v",
					shortLabelMaxChars+1, att.Filename)
			}
			if _, ok := err.(chatBadRequest); !ok {
				t.Fatalf("超長檔名該是 client fault(chatBadRequest → 400),得到 %T", err)
			}
			if att != nil {
				t.Fatalf("被拒時不該回傳一顆 blob(會被寫進 store):%+v", att)
			}
			msg := err.Error()
			for _, want := range []string{
				strconv.Itoa(shortLabelMaxChars + 1), strconv.Itoa(shortLabelMaxChars),
			} {
				if !strings.Contains(msg, want) {
					t.Fatalf("拒絕訊息該說出 %q,得到 %s", want, msg)
				}
			}
			for _, forbidden := range []string{"ocagent", "instead"} {
				if strings.Contains(msg, forbidden) {
					t.Fatalf("拒絕訊息不該教人把字寫去哪(出現 %q):%s", forbidden, msg)
				}
			}
		})
	}
}

// TestAttachmentFilenameCapBindsTheStreamingUploadSeam proves the cap really is
// reached through the ENDPOINT `ocagent upload` speaks, not merely through the
// helper — and that a refused upload stores NO blob (the guard runs before
// PutChatAttachment, so nothing is orphaned).
func TestAttachmentFilenameCapBindsTheStreamingUploadSeam(t *testing.T) {
	api := newTasksTestServer(t)
	mime := "text/plain"
	upload := func(name string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		// The raw body IS the bytes on this seam (never JSON), so the request is
		// built by hand rather than through taskReq's encoder.
		req := httptest.NewRequest("POST", "/api/chat/attachments",
			strings.NewReader("hello bytes"))
		req = req.WithContext(context.WithValue(req.Context(), claimsContextKey,
			map[string]any{"sub": "m-exec", "scope": "agent"}))
		api.HandleUploadChatAttachmentApiChatAttachmentsPost(rec, req,
			HandleUploadChatAttachmentApiChatAttachmentsPostParams{
				Filename: &name, Mime: &mime,
			})
		return rec
	}
	ok := upload(t66Runes('檔', shortLabelMaxChars))
	if ok.Code != http.StatusOK {
		t.Fatalf("128 個中文字的檔名走上傳端點應該過,得到 %d %s",
			ok.Code, ok.Body.String())
	}
	stored := decodeBody[chatAttachmentUploadDTO](t, ok)
	if utf8.RuneCountInString(stored.Filename) != shortLabelMaxChars {
		t.Fatalf("上傳端點回的檔名長度不對:%d",
			utf8.RuneCountInString(stored.Filename))
	}
	bad := upload(t66Runes('檔', shortLabelMaxChars+1))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("129 個字的檔名走上傳端點應該被擋(400),得到 %d %s",
			bad.Code, bad.Body.String())
	}
}
