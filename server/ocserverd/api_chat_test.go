// Skeleton generated from server/ocserverd/api_chat.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"strings"
	"testing"
)

func TestPublishChatRead(t *testing.T) {
	t.Skip("TODO: publishChatRead fans one chat_read delta for an EFFECTIVE watermark (repository.put_chat_read parity: key {owner}::{reader}::{peer}, payload {reader, peer, last_read_ts} — spec/sse.md §2.2).")
}

func TestSniffAttachmentMime(t *testing.T) {
	t.Skip("TODO: sniffAttachmentMime is the best-effort image magic-byte sniff; a non-image is application/octet-stream (handlers._sniff_attachment_mime).")
}

func TestResolveChatRecipient(t *testing.T) {
	t.Skip("TODO: resolveChatRecipient accepts only a durable chat address: the single owner or an active AI member (staff or outsource).")
}

func TestDecodeChatAttachment(t *testing.T) {
	t.Skip("TODO: decodeChatAttachment decodes one posted attachment (data-URI or bare base64), resolves the mime (caller → data-URI → sniff), enforces the size caps, and defaults a pasted image's filename (handlers._decode_chat_attachment).")
}

func TestResolveChatAttachment(t *testing.T) {
	t.Skip("TODO: resolveChatAttachment builds a storable blob from RAW bytes: mime (declared → sniff), the size caps, the pasted-image filename default, and a fresh id.")
}

func TestAttachmentRef(t *testing.T) {
	t.Skip("TODO: attachmentRef is the ONE light-ref shape a record stamps for a stored blob ({id, mime, filename} — meta[\"attachments\"] / reply-card answer_attachments / the upload response); filename folds nil → \"\".")
}

func TestHandleUploadChatAttachmentApiChatAttachmentsPost(t *testing.T) {
	t.Run("a well-formed POST /api/chat/attachments answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/chat/attachments request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/chat/attachments reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/chat/attachments request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestResolveChatAttachmentInputs(t *testing.T) {
	t.Skip("TODO: resolveChatAttachmentInputs resolves EVERY item (refs looked up, inline items decoded) BEFORE any new blob is stored — all-or-nothing, so a rejected item never leaves earlier siblings orphaned.")
}

func TestPendingAttachments(t *testing.T) {
	t.Skip("TODO: pendingAttachments projects the resolved items into (a) the light [{id, mime, filename}] refs the record's meta carries and (b) the fresh blobs that still have to be written.")
}

func TestPostChat(t *testing.T) {
	t.Run("a message with a recipient and a body answers 200 and a receipt naming the recipient", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/chat", owner,
			`{"to":"mira","body":"hi"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":          apiAnyString,
			"to":          "mira",
			"ts":          apiAnyNumber,
			"attachments": []any{},
		})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/chat", "",
			`{"to":"mira","body":"hi"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("a request missing `to` answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/chat", owner, `{"body":"hi"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: to")
	})

	t.Run("a body over the character cap answers 400 telling the caller to move the content to an attachment", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "POST", "/api/chat", agent,
			`{"to":"owner","body":"`+strings.Repeat("x", 4001)+`"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"message body is 4001 chars, over the 4000-char limit. "+
				"Put long content in an attachment (ocagent upload) and keep the message to a short pointer.")
	})

	t.Run("the owner may post a body over the character cap and gets 200", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/chat", owner,
			`{"to":"mira","body":"`+strings.Repeat("x", 4001)+`"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":          apiAnyString,
			"to":          "mira",
			"ts":          apiAnyNumber,
			"attachments": []any{},
		})
	})

	t.Run("more than ten attachments answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		items := strings.TrimSuffix(strings.Repeat(`{"id":"att-nope"},`, 11), ",")
		status, data := apiJSON(t, h, "POST", "/api/chat", owner,
			`{"to":"mira","body":"hi","attachments":[`+items+`]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "a message may carry at most 10 attachments")
	})

	t.Run("an empty body with no attachment answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/chat", owner,
			`{"to":"mira","body":""}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "message must carry text or an attachment")
	})

	t.Run("a message carrying meta answers 200 and a receipt naming the recipient", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/chat", owner,
			`{"to":"mira","body":"hi","meta":{"source":"cli"}}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":          apiAnyString,
			"to":          "mira",
			"ts":          apiAnyNumber,
			"attachments": []any{},
		})
	})

	t.Run("a reply to a message that exists answers 200", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		firstStatus, first := apiJSON(t, h, "POST", "/api/chat", owner,
			`{"to":"mira","body":"the original"}`)
		if firstStatus != 200 {
			t.Fatalf("want 200, got %d (%v)", firstStatus, first)
		}
		apiWantBody(t, first, map[string]any{
			"id":          apiAnyString,
			"to":          "mira",
			"ts":          apiAnyNumber,
			"attachments": []any{},
		})
		quoted, _ := first["id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/chat", owner,
			`{"to":"mira","body":"quoting you","reply_to":"`+quoted+`"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":          apiAnyString,
			"to":          "mira",
			"ts":          apiAnyNumber,
			"attachments": []any{},
		})
	})

	t.Run("a reply to a message that does not exist answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/chat", owner,
			`{"to":"mira","body":"quoting nothing","reply_to":"c-nosuchmessage"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"reply_to names no message (c-nosuchmessage) — you can only reply to a "+
				"message that exists; re-read the conversation and use the id it carries")
	})

	t.Run("a message addressed to a member that is not on the roster answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/chat", owner,
			`{"to":"ghost","body":"hi"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "chat recipient 'ghost' not found")
	})

	t.Run("a message carrying an inline attachment answers 200 and a receipt listing the attachment that landed", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/chat", owner,
			`{"to":"mira","body":"see this","attachments":[`+
				`{"data_b64":"aGVsbG8=","filename":"notes.txt","mime":"text/plain"}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": apiAnyString,
			"to": "mira",
			"ts": apiAnyNumber,
			"attachments": []any{map[string]any{
				"id":       apiAnyString,
				"url":      apiAnyString,
				"filename": "notes.txt",
				"mime":     "text/plain",
				"is_image": false,
			}},
		})
		att, _ := data["attachments"].([]any)[0].(map[string]any)
		if url, _ := att["url"].(string); url != "/api/chat/attachment/"+att["id"].(string) {
			t.Fatalf("attachment url: want the serve path for %v, got %v", att["id"], att["url"])
		}
	})

	t.Run("an attachment referencing an id that does not exist answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/chat", owner,
			`{"to":"mira","body":"hi","attachments":[{"id":"att-nosuchblob"}]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "attachment 'att-nosuchblob' not found")
	})

	t.Run("a member posting to the owner answers 200 and a receipt naming the owner", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "POST", "/api/chat", agent,
			`{"to":"owner","body":"hi boss"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":          apiAnyString,
			"to":          "owner",
			"ts":          apiAnyNumber,
			"attachments": []any{},
		})
	})
}

func TestChatPostReceiptOf(t *testing.T) {
	t.Skip("TODO: chatPostReceiptOf projects a just-written chat message onto the T-91 write receipt.")
}

func TestServedChatMessageDTO(t *testing.T) {
	t.Skip("TODO: servedChatMessageDTO builds the chat-message view AND joins the live reply card status (reply_card_status) for a card-bearing message — the read-time field the inline ChatReplyCard reads to lazy-load answered cards (waiting → load the composer eagerly; answered → collapse, fetch only on expand).")
}

func TestChatReplyQuote(t *testing.T) {
	t.Skip("TODO: chatReplyQuote reads the message an id names and projects it into the quote line's view.")
}

func TestRequestedChatIDs(t *testing.T) {
	t.Skip("TODO: requestedChatIDs normalises the repeatable ?ids= parameter: blanks dropped, duplicates collapsed, request order preserved.")
}

func TestServeChatByIDs(t *testing.T) {
	t.Skip("TODO: serveChatByIDs answers `?ids=` — the named messages IN FULL, oldest→newest.")
}

func TestChatDeclaredQueryParams(t *testing.T) {
	t.Skip("TODO: ── unknown query parameters (T-48, owner ruling) ──────────────────────────── Owner, verbatim: 「如果送了 server 不認得的參數應該是要 error 告訴他這個參數不 存在才對」, scoped to THIS ROUTE ONLY (rc-84f98080af16).")
}

func TestUnknownChatQueryParams(t *testing.T) {
	t.Skip("TODO: unknownChatQueryParams returns the query parameter names this route does not accept, sorted so a request that gets several wrong is refused with the same message every time.")
}

func TestEncodeChatCursor(t *testing.T) {
	t.Skip("TODO: ── the T-48 continuation cursor ───────────────────────────────────────────── One opaque string standing in for the composite (ts, id) keyset position the deprecated before_ts/before_id pair spelled out, PLUS the direction the walk that minted it was going.")
}

func TestDecodeChatCursor(t *testing.T) {
	t.Skip("TODO: decodeChatCursor reads a token back, refusing anything this API did not mint.")
}

func TestChatCursorDirName(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestChatPageWindow(t *testing.T) {
	t.Skip("TODO: chatPageWindow is the row budget one page asks the DAL for: `limit` + 1.")
}

func TestWriteChatPage(t *testing.T) {
	t.Skip("TODO: writeChatPage renders one page into the T-48 envelope.")
}

func TestRequestedChatWindow(t *testing.T) {
	t.Skip("TODO: requestedChatWindow reads the T-48 window anchors off the GENERATED params.")
}

func TestServeChatWindow(t *testing.T) {
	t.Skip("TODO: serveChatWindow answers ?start_id= / ?end_id= — the T-48 window, both ends INCLUSIVE, still oldest→newest.")
}

func TestHandleListChatApiChatGet(t *testing.T) {
	t.Run("a well-formed GET /api/chat answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/chat request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/chat reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/chat request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestServeChatOlder(t *testing.T) {
	t.Skip("TODO: serveChatOlder answers the two walks that go TOWARDS THE OLDER — the deprecated before_ts/before_id pair and the ?cursor= token that replaces it — so the two cannot drift into answering the same request differently.")
}

func TestTrimChatPageOlder(t *testing.T) {
	t.Skip("TODO: trimChatPageOlder cuts the one extra row chatPageWindow asked for off an OLDER-walking page and mints the cursor for the next one.")
}

func TestServeChatUnread(t *testing.T) {
	t.Skip("TODO: serveChatUnread answers ?unread=true — the caller's OWN unread, OLDEST FIRST, `limit` taking the OLDEST batch and ?cursor= walking TOWARDS THE NEWER.")
}

func TestTrimChatPageNewer(t *testing.T) {
	t.Skip("TODO: trimChatPageNewer is trimChatPageOlder's mirror for a walk going TOWARDS THE NEWER: the page arrives oldest→newest, so the surplus row is the NEWEST one — drop the back — and the next cursor names the newest row STILL IN THE PAGE, the exclusive lower bound of the next batch.")
}

func TestIsPreviewableMime(t *testing.T) {
	t.Skip("TODO: isPreviewableMime: a mime the browser renders in a new tab (image/*, text/*, application/pdf) — the preview/download split (handlers._is_previewable_mime).")
}

func TestHandleGetChatAttachmentApiChatAttachmentAttachmentIdGet(t *testing.T) {
	t.Run("a well-formed GET /api/chat/attachment/{attachment_id} answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/chat/attachment/{attachment_id} request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/chat/attachment/{attachment_id} reaches this handler with attachment_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/chat/attachment/{attachment_id} request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleGetChatAttachmentShareLinkApiChatAttachmentsAttachmentIdShareLinkGet(t *testing.T) {
	t.Run("a well-formed GET /api/chat/attachments/{attachment_id}/share-link answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/chat/attachments/{attachment_id}/share-link request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/chat/attachments/{attachment_id}/share-link reaches this handler with attachment_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/chat/attachments/{attachment_id}/share-link request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleListChatAttachmentsApiChatAttachmentsGet(t *testing.T) {
	t.Run("a well-formed GET /api/chat/attachments answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/chat/attachments request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/chat/attachments reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/chat/attachments request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleMarkChatReadApiChatMarkReadPost(t *testing.T) {
	t.Run("a well-formed POST /api/chat/mark-read answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/chat/mark-read request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/chat/mark-read reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/chat/mark-read request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleListChatReadsApiChatReadsGet(t *testing.T) {
	t.Run("a well-formed GET /api/chat/reads answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/chat/reads request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/chat/reads reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/chat/reads request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleResumeSummaryApiResumeSummaryGet(t *testing.T) {
	t.Run("a well-formed GET /api/resume-summary answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/resume-summary request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/resume-summary reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/resume-summary request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestResumeSnapshotParts(t *testing.T) {
	t.Skip("TODO: resumeSnapshotParts assembles the caller's wake snapshot: the recent chat (budget-packed per conversation line, other agents' bodies collapsed, reply cards folded in place), the caller's open tasks as LIGHT rows, the studio floor, and the overview size/概要 block.")
}

func TestResumeDisplayTime(t *testing.T) {
	t.Skip("TODO: resumeDisplayTime renders an epoch second as resumeTimeLayout in the SERVER's local zone.")
}

func TestResumeDisplayName(t *testing.T) {
	t.Skip("TODO: resumeDisplayName resolves an id to a display name for the wake snapshot.")
}

func TestResumeChatCarriesFullBody(t *testing.T) {
	t.Skip("TODO: resumeChatCarriesFullBody decides, PER MESSAGE, whether this one is exempt from collapsing.")
}

func TestResumeChatMessageDTO(t *testing.T) {
	t.Skip("TODO: resumeChatMessageDTO projects ONE message for the wake snapshot: names beside ids, a rendered timestamp beside the epoch one, the body collapsed unless exempt, and the reply card folded in place.")
}

func TestResumeChatCollapseIsWorthIt(t *testing.T) {
	t.Skip("TODO: resumeChatCollapseIsWorthIt answers whether folding a body actually SAVES anything, and it exists because for a while it did not have to.")
}

func TestResumeChatMessageChars(t *testing.T) {
	t.Skip("TODO: resumeChatMessageChars is the rune cost ONE projected message puts on the wire.")
}

func TestResumeChatBlock(t *testing.T) {
	t.Skip("TODO: resumeChatBlock packs the wake snapshot's chat and reports what it left out.")
}

func TestResumeChatPackBudget(t *testing.T) {
	t.Skip("TODO: resumeChatPackBudget is what resumeChatBlock may spend on MESSAGES, once the runes that ride outside the array but inside overview.chat_chars are set aside: the snapshot header (generated_at) and the cut hint.")
}

func TestResumeFloorParts(t *testing.T) {
	t.Skip("TODO: resumeFloorParts assembles the studio floor a waking agent lands on: the roster (T-1b09, owner ruling rc-4e98c0481852 — \"All members and contractors and their online / offline status\") and the machine block (rc-09476f535b59 — the machine list plus which one you are on).")
}

func TestContractorTaskFields(t *testing.T) {
	t.Skip("TODO: contractorTaskFields returns the TRUNCATED title of the one task a contractor is bound to (owner ruling rc-a02d8bc7fe23: 正職給職責、外包給任務 標題 — a contractor id is minted per task, so its task title IS its duty), plus that task's status, waiting_reason, and step progress (T-925f, owner ruling rc-6935feeb293a 選①).")
}

func TestStripLeadingTitle(t *testing.T) {
	t.Skip("TODO: stripLeadingTitle drops the ONE markdown title line a role doc opens with — 「# 助理」 — before the cap is applied, so the budget is not spent restating the role name the row already carries in RoleName.")
}

func TestIsATXHeading(t *testing.T) {
	t.Skip("TODO: isATXHeading reports whether line is a markdown ATX heading, by the syntax rule rather than by \"starts with #\": 0–3 spaces of indent, then 1–6 '#', then a space or end of line.")
}

func TestTruncateRunes(t *testing.T) {
	t.Skip("TODO: truncateRunes caps s at max RUNES (not bytes — one CJK character is one rune, three bytes) and marks the cut with an ellipsis so a reader can tell a short duty from a truncated one.")
}

func TestRosterChars(t *testing.T) {
	t.Skip("TODO: rosterChars / machinesChars size the two blocks the way the peek reports them: the TEXT this payload actually carries.")
}

func TestAnsweredCardStepChars(t *testing.T) {
	t.Skip("TODO: answeredCardStepChars sizes the answered-card pointers ONE task row carries, the way rosterChars sizes the roster: the text this payload actually carries, ids included.")
}

func TestMachinesChars(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestHandlePeekResumeSummarySizeApiResumeSummarySizeGet(t *testing.T) {
	t.Run("a well-formed GET /api/resume-summary-size answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/resume-summary-size request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/resume-summary-size reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/resume-summary-size request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleGetMemberResumeSummaryApiMembersMemberIdResumeSummaryGet(t *testing.T) {
	t.Run("a well-formed GET /api/members/{member_id}/resume-summary answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/members/{member_id}/resume-summary request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/members/{member_id}/resume-summary reaches this handler with member_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/members/{member_id}/resume-summary request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleChatUnreadCountApiChatUnreadCountGet(t *testing.T) {
	t.Run("a well-formed GET /api/chat/unread-count answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/chat/unread-count request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/chat/unread-count reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/chat/unread-count request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}
