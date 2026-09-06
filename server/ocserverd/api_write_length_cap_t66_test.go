package main

// api_write_length_cap_t66_test.go — T-66 ②: the SHORT NAMING fields are capped
// on the write side.
//
// Owner, verbatim: 「過去先不管 新的都要限制長度」 (c-5d058a53ef74),
// 「128字元」 (c-92c734ef561e), 「不用在錯誤訊息寫」 (c-b9bb4cfde26a).
// 執行者判斷 (T-66) picked the fields the cap binds — a task artifact's text and
// a chat attachment's filename — and the shape of the guard (copied from the
// chatBodyMaxChars refusal in api_chat.go).
//
// ⚠️ THE ARTIFACT SIDE IS NO LONGER ONE FIELD AT 128. T-92 split the single
// `label` into a display `name` and a prose `description`, and the owner set
// them their own caps in the same breath (c-0d0a576f68af: 48 / 256,
// 「舊資料不截斷」). The 128 belongs to the attachment filename now and to
// nothing else. Everything else on this list survived the split unchanged,
// which is why the cases below are the same five assertions against two caps
// instead of one.
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
	// The artifact side has its own two numbers since T-92 (owner
	// c-0d0a576f68af: 48 / 256). They are asserted here rather than inlined
	// below so a constant that quietly moves cannot take the boundary case with
	// it and stay green.
	if artifactNameMaxChars != 48 || artifactDescriptionMaxChars != 256 {
		t.Fatalf("owner said 48 / 256 (c-0d0a576f68af); the constants are %d / %d",
			artifactNameMaxChars, artifactDescriptionMaxChars)
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

// ── the artifact name and description ───────────────────────────────────────

// t66AddArtifactText pins one link artifact carrying the given name and
// description and returns the recorder. Both are always sent, because since
// T-92 the name is REQUIRED and a request that omits it is refused for that
// rather than for the length this file is about.
func t66AddArtifactText(t *testing.T, api *apiServer, taskID, name, description string) *httptest.ResponseRecorder {
	t.Helper()
	return addArtifact(t, api, taskID, map[string]any{
		"kind": "link", "url": "https://github.com/x/y/pull/1",
		"name": name, "description": description,
	}, "m-exec", "agent")
}

// TestArtifactNameAndDescriptionCapsArePinnedAtFortyEightAndTwoFiftySix pins the
// boundary on BOTH halves of the split text: 48 in / 49 out for the name, 256 in
// / 257 out for the description, in ASCII and in CJK. Two caps in one case
// because a mutant that applies one constant to both fields passes either half
// alone.
func TestArtifactNameAndDescriptionCapsArePinnedAtFortyEightAndTwoFiftySix(t *testing.T) {
	t66AssertCapCorpus(t)
	for _, tc := range []struct {
		name string
		r    rune
	}{{"ascii", 'a'}, {"cjk", '字'}} {
		t.Run(tc.name, func(t *testing.T) {
			for _, f := range []struct {
				field string
				cap   int
				// send builds a request whose FIELD is n runes long and whose
				// other field is comfortably short, so a refusal can only be
				// about the field under test.
				send func(api *apiServer, taskID string, n int) *httptest.ResponseRecorder
				// got reads the stored value of the field under test.
				got func(a taskArtifactDTO) string
			}{
				{"name", artifactNameMaxChars,
					func(api *apiServer, taskID string, n int) *httptest.ResponseRecorder {
						return t66AddArtifactText(t, api, taskID, t66Runes(tc.r, n), "短說明")
					},
					func(a taskArtifactDTO) string { return a.Name }},
				{"description", artifactDescriptionMaxChars,
					func(api *apiServer, taskID string, n int) *httptest.ResponseRecorder {
						return t66AddArtifactText(t, api, taskID, "短名字", t66Runes(tc.r, n))
					},
					func(a taskArtifactDTO) string { return a.Description }},
			} {
				t.Run(f.field, func(t *testing.T) {
					api := newTasksTestServer(t)
					task := createAdHocTask(t, api, "m-exec")

					atCap := t66Runes(tc.r, f.cap)
					if rec := f.send(api, task.ID, f.cap); rec.Code != http.StatusOK {
						t.Fatalf("%d 個字的 %s 應該過,得到 %d %s",
							f.cap, f.field, rec.Code, rec.Body.String())
					}
					// 而且是原樣存下,沒有被偷偷截短。
					arts := getTaskArtifacts(t, api, task.ID).Artifacts
					if len(arts) != 1 || f.got(arts[0]) != atCap {
						t.Fatalf("剛好在上限的 %s 應原樣存下(%d 字),得到 %d 字",
							f.field, utf8.RuneCountInString(atCap),
							utf8.RuneCountInString(f.got(arts[0])))
					}

					rec := f.send(api, task.ID, f.cap+1)
					if rec.Code != http.StatusBadRequest {
						t.Fatalf("%d 個字的 %s 應該被擋(400),得到 %d %s",
							f.cap+1, f.field, rec.Code, rec.Body.String())
					}
					// 拒絕就是拒絕:不得靜默截斷後照樣寫進去。
					if got := getTaskArtifacts(t, api, task.ID).Artifacts; len(got) != 1 {
						t.Fatalf("被拒的 %s 不該留下任何一列:artifacts=%d", f.field, len(got))
					}
					// 訊息要說長度與上限 —— 而且只說這個(owner c-b9bb4cfde26a
					// 「不用在錯誤訊息寫」該把字放哪),並且要說是哪一個欄位,
					// 因為現在有兩個上限,只印數字的訊息指不出是哪一個超了。
					body := rec.Body.String()
					for _, want := range []string{
						strconv.Itoa(f.cap + 1), strconv.Itoa(f.cap), f.field,
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
		})
	}
}

// TestArtifactTextCapsLeaveExistingRowsAlone pins 「過去先不管」/「舊資料不截斷」:
// a row written before the caps existed (here: straight through the DAL, which
// they do not bind) still reads back IN FULL. Nothing migrates, backfills or
// truncates it — and on the live store 313 such rows really do carry a
// description longer than 256, so this is not a hypothetical.
//
// The legacy row also has an EMPTY name, which is what nearly every migrated
// row looks like, so it doubles as the acceptance for the read-time derivation:
// the wire's `name` is never empty even when the column is.
func TestArtifactTextCapsLeaveExistingRowsAlone(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	legacy := t66Runes('舊', artifactDescriptionMaxChars*3)
	if err := api.dal.PutTaskArtifact(TaskArtifact{
		ID: "ta-legacy0001", TaskID: task.ID, Kind: ArtifactKindLink,
		Description: legacy, CreatedTS: 1000, CreatedBy: "m-exec",
	}); err != nil {
		t.Fatal(err)
	}
	arts := getTaskArtifacts(t, api, task.ID).Artifacts
	if len(arts) != 1 {
		t.Fatalf("語料不合格:舊列沒有種進去,artifacts=%d", len(arts))
	}
	if got := arts[0].Description; got != legacy {
		t.Fatalf("既有資料不得被動到:讀回 %d 字,原本 %d 字",
			utf8.RuneCountInString(got), utf8.RuneCountInString(legacy))
	}
	// 名字欄是空的,但 wire 上不會是空的 —— 這一列連 blob 都沒有,所以掉到
	// 最後一段 fallback:「#」+ 去掉 ta- 前綴的 id。
	if got := arts[0].Name; got != "#legacy0001" {
		t.Fatalf("沒有名字的舊列要在讀取時衍生出一個,得到 %q", got)
	}
	// 反恆真:同一台 server 上,新的寫入確實還是被擋著 —— 否則「舊的沒動」
	// 也可能只是守衛整條沒生效。
	if rec := t66AddArtifactText(t, api, task.ID, "短名字",
		t66Runes('新', artifactDescriptionMaxChars+1)); rec.Code != http.StatusBadRequest {
		t.Fatalf("對照組壞了:新的超長 description 也沒被擋(%d)— 這一跑什麼都沒證明",
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
