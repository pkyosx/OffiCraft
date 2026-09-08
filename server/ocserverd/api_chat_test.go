// Skeleton generated from server/ocserverd/api_chat.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
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
	t.Run("an upload declaring its filename and mime answers the light ref and stores those bytes", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST",
			"/api/chat/attachments?filename=notes.txt&mime=text/plain", owner, "hello")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":       apiAnyString,
			"mime":     "text/plain",
			"filename": "notes.txt",
		})
		id, _ := data["id"].(string)
		apiWantStoredBlobs(t, d, id)
		served := apiRequest(t, h, "GET", "/api/chat/attachment/"+id, owner, "")
		if served.Code != 200 || served.Body.String() != "hello" {
			t.Fatalf("serving the stored blob: %d %q", served.Code, served.Body.String())
		}
		dashboard.wantFrames()
		bystander.wantFrames()
	})

	t.Run("an image uploaded without a mime or a name is stored under the sniffed mime and the pasted-image default", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/chat/attachments", owner,
			"\x89PNG\r\n\x1a\nrest")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":       apiAnyString,
			"mime":     "image/png",
			"filename": "pasted-image.png",
		})
		apiWantStoredBlobs(t, d, data["id"].(string))
	})

	t.Run("bytes that are not an image and carry no name are stored as an unnamed octet-stream blob", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/chat/attachments", owner, "zzz")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":       apiAnyString,
			"mime":     "application/octet-stream",
			"filename": "",
		})
		apiWantStoredBlobs(t, d, data["id"].(string))
	})

	t.Run("a filename over the character cap answers 400 and stores nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST",
			"/api/chat/attachments?filename="+strings.Repeat("x", 129), owner, "hello")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"attachment filename is 129 chars, over the 128-char limit")
		apiWantStoredBlobs(t, d)
		dashboard.wantFrames()
	})

	t.Run("an upload with no bytes at all answers 400 and stores nothing", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/chat/attachments?filename=empty.txt",
			owner, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "attachment is empty")
		apiWantStoredBlobs(t, d)
	})

	t.Run("a request without credentials answers 401 and stores nothing", func(t *testing.T) {
		_, h, d, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/chat/attachments?filename=notes.txt",
			"", "hello")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiWantStoredBlobs(t, d)
	})
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

	t.Run("a member posting to the owner fans one chat frame and hands push the owner's notification", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")
		sender := apiTestListen(t, api, "mira")
		bystander := apiTestListen(t, api, "kip")
		wantPushed := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/chat", agent,
			`{"to":"owner","body":"hi boss"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		id, _ := data["id"].(string)

		frame := map[string]any{
			"seq":   1,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     "owner::" + id,
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{"id": id, "from": "mira", "to": "owner"},
			},
			"ts":      apiAnyNumber,
			"trigger": "mira",
		}
		dashboard.wantFrames(frame)
		sender.wantFrames(frame)
		bystander.wantFrames()
		wantPushed(map[string]any{
			"kind":         "chat",
			"chat_id":      id,
			"chat_peer_id": "mira",
			"title":        "OffiCraft 有新訊息",
			"body":         "你有一則新訊息。",
		})
	})

	t.Run("a message the owner sends fans one chat frame and pushes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "mira")
		wantPushed := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/chat", owner,
			`{"to":"mira","body":"hi"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		id, _ := data["id"].(string)

		frame := map[string]any{
			"seq":   1,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     "owner::" + id,
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{"id": id, "from": "owner", "to": "mira"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(frame)
		recipient.wantFrames(frame)
		wantPushed()
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
	t.Run("an install with nothing said yet answers an empty page and no continuation", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/chat", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{}})
		dashboard.wantFrames()
	})

	t.Run("the stream comes back oldest first, each message in full", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, first := apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"one"}`)
		_, second := apiJSON(t, h, "POST", "/api/chat", agent, `{"to":"owner","body":"two"}`)

		status, data := apiJSON(t, h, "GET", "/api/chat", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{
			apiTestChatRow(first["id"].(string), "owner", "mira", "one"),
			apiTestChatRow(second["id"].(string), "kip", "owner", "two"),
		}})
	})

	t.Run("with narrows the listing to that member's line", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"one"}`)
		_, second := apiJSON(t, h, "POST", "/api/chat", agent, `{"to":"owner","body":"two"}`)

		status, data := apiJSON(t, h, "GET", "/api/chat?with=kip", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{
			apiTestChatRow(second["id"].(string), "kip", "owner", "two"),
		}})

		status, data = apiJSON(t, h, "GET", "/api/chat?with=nobody", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{}})
	})

	t.Run("a page shorter than the stream carries a cursor that walks to the older page", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		_, first := apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"one"}`)
		_, second := apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"two"}`)

		status, data := apiJSON(t, h, "GET", "/api/chat?limit=1", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"messages": []any{
				apiTestChatRow(second["id"].(string), "owner", "mira", "two"),
			},
			"next_cursor": apiAnyString,
		})

		status, data = apiJSON(t, h, "GET",
			"/api/chat?limit=1&cursor="+data["next_cursor"].(string), owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{
			apiTestChatRow(first["id"].(string), "owner", "mira", "one"),
		}})
	})

	t.Run("ids re-reads the named messages and consults nothing else", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		_, first := apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"one"}`)
		apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"two"}`)

		status, data := apiJSON(t, h, "GET",
			"/api/chat?ids="+first["id"].(string)+"&with=nobody", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{
			apiTestChatRow(first["id"].(string), "owner", "mira", "one"),
		}})
	})

	t.Run("a query parameter this route does not declare is refused, naming it and the accepted set", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/chat?zzz=2&nope=1", owner, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"unknown query parameter(s) on GET /api/chat: nope, zzz — this route "+
				"refuses parameters it does not declare rather than ignoring them, "+
				"because an ignored parameter silently answers a question you did "+
				"not ask; accepted here: before_id, before_ts, cursor, end_id, ids, "+
				"limit, recipient, sender, start_id, token, unread, with")
	})

	t.Run("unread combined with a stream position is refused", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET",
			"/api/chat?unread=true&before_ts=1&before_id=c-1", owner, "")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"unread cannot be combined with before_ts/before_id or start_id/end_id "+
				"— those name a position in the whole stream, unread names the set "+
				"your read watermarks define; page unread with cursor instead")
	})

	t.Run("reading the stream marks nothing read", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/chat", agent, `{"to":"owner","body":"two"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/chat?with=kip", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantChatReads(t, h, owner)
		dashboard.wantFrames()
	})

	t.Run("a request without credentials answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/chat", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
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
	t.Run("an image blob is served under its stored mime with no download disposition", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		_, uploaded := apiJSON(t, h, "POST", "/api/chat/attachments", owner,
			"\x89PNG\r\n\x1a\nrest")

		rec := apiRequest(t, h, "GET", "/api/chat/attachment/"+uploaded["id"].(string), owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		apiWantHeaders(t, rec, map[string]string{"Content-Type": "image/png"})
		if rec.Body.String() != "\x89PNG\r\n\x1a\nrest" {
			t.Fatalf("body: got %q", rec.Body.String())
		}
		dashboard.wantFrames()
	})

	t.Run("a previewable non-image is served inline under a sandbox so it cannot script on this origin", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		_, uploaded := apiJSON(t, h, "POST",
			"/api/chat/attachments?filename=notes.txt&mime=text/plain", owner, "hello")

		rec := apiRequest(t, h, "GET", "/api/chat/attachment/"+uploaded["id"].(string), owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		apiWantHeaders(t, rec, map[string]string{
			"Content-Type":            "text/plain",
			"Content-Disposition":     `inline; filename="notes.txt"; filename*=UTF-8''notes.txt`,
			"Content-Security-Policy": "sandbox",
		})
		if rec.Body.String() != "hello" {
			t.Fatalf("body: got %q", rec.Body.String())
		}
	})

	t.Run("a blob the browser cannot render downloads under its stored name", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		_, uploaded := apiJSON(t, h, "POST", "/api/chat/attachments?filename=blob.bin",
			owner, "zzz")

		rec := apiRequest(t, h, "GET", "/api/chat/attachment/"+uploaded["id"].(string), owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		apiWantHeaders(t, rec, map[string]string{
			"Content-Type":        "application/octet-stream",
			"Content-Disposition": `attachment; filename="blob.bin"; filename*=UTF-8''blob.bin`,
		})
		if rec.Body.String() != "zzz" {
			t.Fatalf("body: got %q", rec.Body.String())
		}
	})

	t.Run("an attachment id nothing was stored under answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/chat/attachment/att-nosuchblob", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "attachment 'att-nosuchblob' not found")
	})

	t.Run("a request without credentials answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		_, uploaded := apiJSON(t, h, "POST",
			"/api/chat/attachments?filename=notes.txt&mime=text/plain", owner, "hello")

		status, data := apiJSON(t, h, "GET",
			"/api/chat/attachment/"+uploaded["id"].(string), "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleGetChatAttachmentShareLinkApiChatAttachmentsAttachmentIdShareLinkGet(t *testing.T) {
	t.Run("minting a link for a stored blob answers that blob's serve path carrying a signature", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		_, uploaded := apiJSON(t, h, "POST",
			"/api/chat/attachments?filename=notes.txt&mime=text/plain", owner, "hello")
		id, _ := uploaded["id"].(string)

		status, data := apiJSON(t, h, "GET", "/api/chat/attachments/"+id+"/share-link", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"url": apiAnyString})
		url, _ := data["url"].(string)
		prefix := "/api/chat/attachment/" + id + "?sig="
		if !strings.HasPrefix(url, prefix) || len(url) == len(prefix) {
			t.Fatalf("url: want %q followed by a signature, got %q", prefix, url)
		}
		dashboard.wantFrames()
	})

	t.Run("the minted link serves that one blob to a caller holding no credential", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		_, uploaded := apiJSON(t, h, "POST",
			"/api/chat/attachments?filename=notes.txt&mime=text/plain", owner, "hello")
		_, minted := apiJSON(t, h, "GET",
			"/api/chat/attachments/"+uploaded["id"].(string)+"/share-link", owner, "")

		rec := apiRequest(t, h, "GET", minted["url"].(string), "", "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		if rec.Body.String() != "hello" {
			t.Fatalf("body: got %q", rec.Body.String())
		}
	})

	t.Run("a signature this server did not mint answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		_, uploaded := apiJSON(t, h, "POST",
			"/api/chat/attachments?filename=notes.txt&mime=text/plain", owner, "hello")

		status, data := apiJSON(t, h, "GET",
			"/api/chat/attachment/"+uploaded["id"].(string)+"?sig=forged", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "invalid signature")
	})

	t.Run("an attachment id nothing was stored under mints nothing and answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET",
			"/api/chat/attachments/att-nosuchblob/share-link", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "attachment 'att-nosuchblob' not found")
	})

	t.Run("a request without credentials answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		_, uploaded := apiJSON(t, h, "POST",
			"/api/chat/attachments?filename=notes.txt&mime=text/plain", owner, "hello")

		status, data := apiJSON(t, h, "GET",
			"/api/chat/attachments/"+uploaded["id"].(string)+"/share-link", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleListChatAttachmentsApiChatAttachmentsGet(t *testing.T) {
	t.Run("a member's line that carries no attachment answers an empty gallery", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"no file here"}`)
		dashboard := apiTestListen(t, api, "")

		status, body := apiChatDecoded(t, h, "GET", "/api/chat/attachments?with=mira", owner)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, body)
		}
		apiWantValue(t, "body", body, []any{})
		dashboard.wantFrames()
	})

	t.Run("every attachment of that member's line comes back sender-labelled", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		_, uploaded := apiJSON(t, h, "POST",
			"/api/chat/attachments?filename=notes.txt&mime=text/plain", owner, "hello")
		attachmentID, _ := uploaded["id"].(string)
		_, posted := apiJSON(t, h, "POST", "/api/chat", owner,
			`{"to":"mira","body":"see this","attachments":[{"id":"`+attachmentID+`"}]}`)
		messageID, _ := posted["id"].(string)

		status, body := apiChatDecoded(t, h, "GET", "/api/chat/attachments?with=mira", owner)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, body)
		}
		apiWantValue(t, "body", body, []any{map[string]any{
			"id":         attachmentID,
			"url":        "/api/chat/attachment/" + attachmentID,
			"filename":   "notes.txt",
			"mime":       "text/plain",
			"is_image":   false,
			"message_id": messageID,
			"from":       "owner",
			"from_name":  "",
			"to":         "mira",
			"ts":         apiAnyNumber,
		}})
	})

	t.Run("a gallery asked for without naming a member answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/chat/attachments", owner, "")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "with is required")
	})

	t.Run("a request without credentials answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/chat/attachments?with=mira", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleMarkChatReadApiChatMarkReadPost(t *testing.T) {
	t.Run("marking a conversation read answers the stored watermark and fans one owner-only chat_read frame", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		peer := apiTestListen(t, api, "mira")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/chat/mark-read", owner,
			`{"peer":"mira","last_read_ts":5}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"reader_id":    "owner",
			"peer_id":      "mira",
			"last_read_ts": float64(5),
		})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "chat_read",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat_read",
				"key":     "owner::owner::mira",
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{
					"reader":       "owner",
					"peer":         "mira",
					"last_read_ts": float64(5),
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		peer.wantFrames()
		bystander.wantFrames()
		apiWantChatReads(t, h, owner, map[string]any{
			"reader_id": "owner", "peer_id": "mira", "last_read_ts": float64(5),
		})
	})

	t.Run("a watermark behind the stored one answers the stored one and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/chat/mark-read", owner, `{"peer":"mira","last_read_ts":5}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/chat/mark-read", owner,
			`{"peer":"mira","last_read_ts":3}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"reader_id":    "owner",
			"peer_id":      "mira",
			"last_read_ts": float64(5),
		})
		dashboard.wantFrames()
		apiWantChatReads(t, h, owner, map[string]any{
			"reader_id": "owner", "peer_id": "mira", "last_read_ts": float64(5),
		})
	})

	t.Run("a peer that is only whitespace answers 422 and writes no receipt", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/chat/mark-read", owner, `{"peer":"  "}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "peer is required")
		dashboard.wantFrames()
		apiWantChatReads(t, h, owner)
	})

	t.Run("a body that names no peer at all answers 422 and writes no receipt", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/chat/mark-read", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: peer")
		apiWantChatReads(t, h, owner)
	})

	t.Run("a request without credentials answers 401 and writes no receipt", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/chat/mark-read", "",
			`{"peer":"mira","last_read_ts":5}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiWantChatReads(t, h, owner)
	})
}

func TestHandleListChatReadsApiChatReadsGet(t *testing.T) {
	t.Run("an install where nobody has marked anything read answers an empty array", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, body := apiChatDecoded(t, h, "GET", "/api/chat/reads", owner)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, body)
		}
		apiWantValue(t, "body", body, []any{})
		dashboard.wantFrames()
	})

	t.Run("every watermark written so far comes back", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		apiJSON(t, h, "POST", "/api/chat/mark-read", owner, `{"peer":"mira","last_read_ts":5}`)
		apiJSON(t, h, "POST", "/api/chat/mark-read", agent, `{"peer":"owner","last_read_ts":7}`)

		status, body := apiChatDecoded(t, h, "GET", "/api/chat/reads", owner)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, body)
		}
		apiWantValue(t, "body", body, []any{
			map[string]any{"reader_id": "owner", "peer_id": "mira", "last_read_ts": float64(5)},
			map[string]any{"reader_id": "mira", "peer_id": "owner", "last_read_ts": float64(7)},
		})
	})

	t.Run("with narrows the listing to that one conversation", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		apiJSON(t, h, "POST", "/api/chat/mark-read", owner, `{"peer":"mira","last_read_ts":5}`)
		apiJSON(t, h, "POST", "/api/chat/mark-read", agent, `{"peer":"owner","last_read_ts":7}`)

		status, body := apiChatDecoded(t, h, "GET", "/api/chat/reads?with=mira", owner)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, body)
		}
		apiWantValue(t, "body", body, []any{
			map[string]any{"reader_id": "owner", "peer_id": "mira", "last_read_ts": float64(5)},
		})

		status, body = apiChatDecoded(t, h, "GET", "/api/chat/reads?with=kip", owner)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, body)
		}
		apiWantValue(t, "body", body, []any{})
	})

	t.Run("a request without credentials answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/chat/reads", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleResumeSummaryApiResumeSummaryGet(t *testing.T) {
	t.Run("a caller with nothing in flight wakes to an empty chat and the studio floor", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/resume-summary", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"identity":             "owner",
			"generated_at":         apiAnyString,
			"chat":                 []any{},
			"chat_earlier_omitted": map[string]any{"omitted": false, "hint": ""},
			"tasks":                []any{},
			"roster":               apiTestSeedRoster(),
			"machines":             apiTestSeedMachines(),
			"overview":             apiTestResumeOverview(0, 26),
			"note":                 apiTestResumeNote,
		})
		apiWantWallClock(t, "generated_at", data["generated_at"].(string))
		dashboard.wantFrames()
	})

	t.Run("the caller's own line rides the snapshot with names and rendered times beside the ids", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		_, posted := apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"hi"}`)
		id, _ := posted["id"].(string)

		status, data := apiJSON(t, h, "GET", "/api/resume-summary", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"identity":     "owner",
			"generated_at": apiAnyString,
			"chat": []any{map[string]any{
				"id":                 id,
				"from":               "owner",
				"from_name":          "Owner",
				"to":                 "mira",
				"to_name":            "Mira",
				"body":               "hi",
				"body_omitted_chars": 0,
				"ts":                 apiAnyNumber,
				"ts_display":         apiAnyString,
				"meta":               map[string]any{},
				"reply_card_status":  "",
				"attachments":        []any{},
				"reply_to":           "",
			}},
			"chat_earlier_omitted": map[string]any{"omitted": false, "hint": ""},
			"tasks":                []any{},
			"roster":               apiTestSeedRoster(),
			"machines":             apiTestSeedMachines(),
			"overview":             apiTestResumeOverview(1, 64),
			"note":                 apiTestResumeNote,
		})
		apiWantWallClock(t, "generated_at", data["generated_at"].(string))
		apiWantWallClock(t, "chat[0].ts_display",
			data["chat"].([]any)[0].(map[string]any)["ts_display"].(string))
	})

	t.Run("an agent wakes to its own identity and only the line it is on", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"not for kip"}`)
		_, posted := apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"kip","body":"for kip"}`)
		id, _ := posted["id"].(string)

		status, data := apiJSON(t, h, "GET", "/api/resume-summary", agent, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"identity":     "kip",
			"generated_at": apiAnyString,
			"chat": []any{map[string]any{
				"id":                 id,
				"from":               "owner",
				"from_name":          "Owner",
				"to":                 "kip",
				"to_name":            "Kip",
				"body":               "for kip",
				"body_omitted_chars": 0,
				"ts":                 apiAnyNumber,
				"ts_display":         apiAnyString,
				"meta":               map[string]any{},
				"reply_card_status":  "",
				"attachments":        []any{},
				"reply_to":           "",
			}},
			"chat_earlier_omitted": map[string]any{"omitted": false, "hint": ""},
			"tasks":                []any{},
			"roster":               apiTestSeedRoster(),
			"machines":             apiTestSeedMachines(),
			"overview":             apiTestResumeOverview(1, 68),
			"note":                 apiTestResumeNote,
		})
	})

	t.Run("waking marks nothing read", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/chat", agent, `{"to":"owner","body":"boss"}`)

		if status, data := apiJSON(t, h, "GET", "/api/resume-summary", owner, ""); status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantChatReads(t, h, owner)
	})

	t.Run("a request without credentials answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/resume-summary", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
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
	t.Run("the peek reports the caller's overview and the sum of the blocks the snapshot carries", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/resume-summary-size", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"identity":              "owner",
			"overview":              apiTestResumeOverview(0, 26),
			"estimated_total_chars": 258,
			"note":                  apiTestPeekNote,
		})
		dashboard.wantFrames()
	})

	t.Run("a message on the caller's line grows the chat block the peek reports", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"hi"}`)

		status, data := apiJSON(t, h, "GET", "/api/resume-summary-size", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"identity":              "owner",
			"overview":              apiTestResumeOverview(1, 64),
			"estimated_total_chars": 296,
			"note":                  apiTestPeekNote,
		})
	})

	t.Run("a request without credentials answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/resume-summary-size", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleGetMemberResumeSummaryApiMembersMemberIdResumeSummaryGet(t *testing.T) {
	t.Run("the owner reads another member's snapshot from that member's vantage", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"kip","body":"not mira's"}`)
		_, posted := apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"hi"}`)
		id, _ := posted["id"].(string)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/members/mira/resume-summary", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"identity":     "mira",
			"generated_at": apiAnyString,
			"chat": []any{map[string]any{
				"id":                 id,
				"from":               "owner",
				"from_name":          "Owner",
				"to":                 "mira",
				"to_name":            "Mira",
				"body":               "hi",
				"body_omitted_chars": 0,
				"ts":                 apiAnyNumber,
				"ts_display":         apiAnyString,
				"meta":               map[string]any{},
				"reply_card_status":  "",
				"attachments":        []any{},
				"reply_to":           "",
			}},
			"chat_earlier_omitted": map[string]any{"omitted": false, "hint": ""},
			"tasks":                []any{},
			"roster":               apiTestSeedRoster(),
			"machines":             apiTestSeedMachines(),
			"overview":             apiTestResumeOverview(1, 64),
			"note":                 apiTestResumeNote,
		})
		apiWantWallClock(t, "generated_at", data["generated_at"].(string))
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "GET", "/api/members/mira/resume-summary", agent, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("a member the roster does not carry answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/members/ghost/resume-summary", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ghost' not found")
	})

	t.Run("a request without credentials answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/members/mira/resume-summary", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleChatUnreadCountApiChatUnreadCountGet(t *testing.T) {
	t.Run("an install where nothing has been said answers zero", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/chat/unread-count", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"unread": 0})
		dashboard.wantFrames()
	})

	t.Run("messages the caller has not marked read are counted, and marking them read clears the dot", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/chat", agent, `{"to":"owner","body":"one"}`)
		_, second := apiJSON(t, h, "POST", "/api/chat", agent, `{"to":"owner","body":"two"}`)

		status, data := apiJSON(t, h, "GET", "/api/chat/unread-count", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"unread": 2})

		apiJSON(t, h, "POST", "/api/chat/mark-read", owner,
			`{"peer":"kip","last_read_ts":`+strconv.FormatFloat(second["ts"].(float64), 'f', -1, 64)+`}`)

		status, data = apiJSON(t, h, "GET", "/api/chat/unread-count", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"unread": 0})
	})

	t.Run("a dismissed sender's leftover unread stops lighting the dot", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/chat", agent, `{"to":"owner","body":"one"}`)

		if status, data := apiJSON(t, h, "DELETE", "/api/members/kip", owner, ""); status != 200 {
			t.Fatalf("dismiss: %d %v", status, data)
		}

		status, data := apiJSON(t, h, "GET", "/api/chat/unread-count", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"unread": 0})
	})

	t.Run("a request without credentials answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/chat/unread-count", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

// ── the chat/wake-snapshot fixtures ───────────────────────────────────────

// apiChatDecoded drives one request whose answer is a bare JSON array rather
// than the object apiJSON decodes into.
func apiChatDecoded(t *testing.T, h http.Handler, method, target, token string) (int, any) {
	t.Helper()
	rec := apiRequest(t, h, method, target, token, "")
	var parsed any
	if raw := rec.Body.Bytes(); len(raw) > 0 {
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("non-JSON body (%d): %s", rec.Code, raw)
		}
	}
	return rec.Code, parsed
}

// apiWantHeaders asserts the response carries exactly these header values.
func apiWantHeaders(t *testing.T, rec *httptest.ResponseRecorder, want map[string]string) {
	t.Helper()
	for name, value := range want {
		if got := rec.Header().Get(name); got != value {
			t.Fatalf("%s: want %q, got %q", name, value, got)
		}
	}
	for _, name := range []string{"Content-Disposition", "Content-Security-Policy"} {
		if _, named := want[name]; !named && rec.Header().Get(name) != "" {
			t.Fatalf("%s: the response carries a header the expectation does not name (%q)",
				name, rec.Header().Get(name))
		}
	}
}

// apiWantStoredBlobs asserts the shared attachment store holds exactly these
// blob ids — the write side of an upload, and the "nothing landed" side of a
// refused one.
func apiWantStoredBlobs(t *testing.T, d *DAL, want ...string) {
	t.Helper()
	rows, err := d.rdb.Query(`SELECT id FROM chat_attachment ORDER BY id`)
	if err != nil {
		t.Fatalf("read the attachment store: %v", err)
	}
	defer rows.Close()
	got := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the attachment store: %v", err)
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, append([]string{}, want...)) {
		t.Fatalf("stored blobs: want %v, got %v", want, got)
	}
}

// apiWantChatReads asserts GET /api/chat/reads answers exactly these receipts
// — the write side of mark-read, and the "nothing was written" side of every
// route that promises to advance no watermark.
func apiWantChatReads(t *testing.T, h http.Handler, token string, want ...map[string]any) {
	t.Helper()
	status, body := apiChatDecoded(t, h, "GET", "/api/chat/reads", token)
	if status != 200 {
		t.Fatalf("read the receipts: %d (%v)", status, body)
	}
	expected := make([]any, len(want))
	for i := range want {
		expected[i] = want[i]
	}
	apiWantValue(t, "chat reads", body, any(expected))
}

// apiTestChatRow is one message as GET /api/chat renders it: the ids without
// the display names and rendered times the wake snapshot adds beside them.
func apiTestChatRow(id, from, to, body string) map[string]any {
	return map[string]any{
		"id":                 id,
		"from":               from,
		"from_name":          "",
		"to":                 to,
		"to_name":            "",
		"body":               body,
		"body_omitted_chars": 0,
		"ts":                 apiAnyNumber,
		"ts_display":         "",
		"meta":               map[string]any{},
		"reply_card_status":  "",
		"attachments":        []any{},
		"reply_to":           "",
	}
}

// apiTestSeedRoster is the studio floor of an out-of-box install nobody holds a
// connection to: the plain-agent staff row and the seeded assistant.
func apiTestSeedRoster() []any {
	return []any{
		map[string]any{
			"id":             "kip",
			"name":           "Kip",
			"kind":           "staff",
			"role_name":      "",
			"duty":           "",
			"current_task":   "",
			"task_status":    "",
			"waiting_reason": "",
			"progress_done":  0,
			"progress_total": 0,
			"machine":        "",
			"presence":       "offline",
		},
		map[string]any{
			"id":             "mira",
			"name":           "Mira",
			"kind":           "staff",
			"role_name":      "Assistant",
			"duty":           apiTestSeedAssistantDuty,
			"current_task":   "",
			"task_status":    "",
			"waiting_reason": "",
			"progress_done":  0,
			"progress_total": 0,
			"machine":        "",
			"presence":       "offline",
		},
	}
}

// apiTestSeedMachines is the machine block of an out-of-box install: the
// server's own machine, and no binding for the caller.
func apiTestSeedMachines() map[string]any {
	return map[string]any{
		"list": []any{map[string]any{
			"machine_id":   "m-server-self",
			"display_name": "伺服器這一台",
			"online":       false,
		}},
		"you_are_on": "",
	}
}

// apiTestResumeOverview is the overview block of an out-of-box install, whose
// only moving parts are the chat count and the size of the rendered chat.
func apiTestResumeOverview(chatCount, chatChars int) map[string]any {
	return map[string]any{
		"chat_count":                   chatCount,
		"chat_chars":                   chatChars,
		"tasks_returned":               0,
		"tasks_open_total":             0,
		"tasks_detail_chars":           0,
		"cards_waiting":                0,
		"cards_answered_recent":        0,
		"roster_chars":                 213,
		"machines_chars":               19,
		"steps_on_answered_card":       0,
		"steps_on_answered_card_chars": 0,
	}
}

// apiWantWallClock asserts a rendered time field carries the snapshot's
// date-time-offset shape; its value is whenever the test happened to run.
func apiWantWallClock(t *testing.T, path, got string) {
	t.Helper()
	if _, err := time.Parse("2006-01-02 15:04:05 -07:00", got); err != nil {
		t.Fatalf("%s: want a rendered wall-clock time, got %q (%v)", path, got, err)
	}
}

const apiTestResumeNote = "這是一份**開機快照**，不是完整資料。\n聊天：只帶最近的往來，而且是照**字數**（不是則數）收的，收到裝不下為止，由舊到新排。每則都附寄件與收件者的名字、以及帶時區的時間（請跟最上面的 `generated_at` 對照著看）；有回覆卡的會一併附上。\n有兩種「不完整」，意思不一樣，不要混：\n· `body_omitted_chars` > 0 ＝ **這一則就在這裡，只是被摺短了**，數字是被摺掉的字數，這是確定的事實（你自己寫給自己的交接、以及你跟 owner 之間的往來——他說的和你對他說的都算——一律不摺）。要看全文，把那一則的 `id` 放進 `get_chat` 的 `ids`。\n· `chat_earlier_omitted` ＝ **可能整則整則不見了**。這是「可能」不是「一定」：那條線在讀取或字數上限被切斷，而沒有人往切口後面看過，所以就算其實沒有更舊的也會標。它自己會附上怎麼去抓。\n任務：只給精簡列，沒有計畫細節；其中 `answered_card_steps` ＝ **這一步卡在一張 owner 已經回答、卻還沒有人接手的卡上**（`overview` 的 `steps_on_answered_card` ＝ **這份快照帶的這幾列裡**有幾步這樣卡著，不是你所有任務的總數：任務列只帶最近更新的前幾張，你手上票多的時候，更舊的那些就算卡著也不會出現在這裡，也不會被算進去——所以 0 不等於沒有，要確認請用 `list_tasks`／`list_reply_cards`）——那不表示那一步做完了，他的答覆也可能是不通過、要改做，先用 `get_reply_card` 把答案讀完再決定怎麼走。名冊：工作室裡每個人的狀態、所在機器與職責（過長會截斷，`…` 是切口）。機器：機器清單，以及你在哪一台。\n先看 `overview` 的數量與大小，再決定要拉什麼：單張任務用 `get_task`（`detail_chars` 很大的就交給分身去拉），你的卡片用 `list_reply_cards`（記得給 `limit`），要更多聊天或任務用 `list_chat`／`list_tasks`。"

const apiTestPeekNote = "Size-only preview of resume_summary — counts/sizes ONLY, no chat or task content. estimated_total_chars is exactly chat_chars + tasks_detail_chars + roster_chars + machines_chars + steps_on_answered_card_chars, all five reported in overview: the WHOLE chat block as the snapshot renders it (chat_chars is the rendered block's cost, NOT the sum of the message bodies), plus the plan text its task rows omit, the two studio-floor blocks, and the answered-card pointers its task rows carry. steps_on_answered_card > 0 means that many steps AMONG THE FEW MOST-RECENTLY-UPDATED TASKS the snapshot carries — not across all your tasks — are sitting on a reply card the owner ALREADY answered while the step is still in_progress, and nobody has acted on the answer yet; pull resume_summary (or the cards) and read it before anything else. It is a FLOOR, not a total: the task block is capped at the most recently updated tasks, so when you hold more tasks than that cap, an older task stuck on an answered card is not counted here and 0 does not prove there is none — use list_tasks / list_reply_cards to be sure. So it is what pulling the snapshot actually costs. Use it to decide: if small (rule of thumb < 20000 chars, ≈ 5k tokens) call resume_summary directly in your main session; if large, spawn a cheap sub-agent (e.g. haiku) to call resume_summary and return a compressed digest, so the full payload never burns your own context."

const apiTestSeedAssistantDuty = "Owner 的助理，工作室的預設對口。\n\n- **不知道該找誰**：先找我，我會判斷並安排後續。\n- **OffiCraft 怎麼運作**：怎麼使用、規則是什麼、某個操作在哪裡，都可以問我。\n- **你做不到的操作**：我的權限比一般成員大，權限之內的我可以代你執行；只有 Owner 能決定的，我整理好開一張卡送到他面前。"
