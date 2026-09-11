package main

import (
	"encoding/base64"
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
	t.Run("one watermark fans a single chat_read delta keyed by owner, reader and peer, and only the owner sees it", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		reader := apiTestListen(t, api, "mira")
		bystander := apiTestListen(t, api, "kip")

		api.publishChatRead(ChatRead{ReaderID: "mira", PeerID: "owner", LastReadTS: 7.5}, "mira")

		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "chat_read",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat_read",
				"key":     "owner::mira::owner",
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{
					"reader":       "mira",
					"peer":         "owner",
					"last_read_ts": 7.5,
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "mira",
		})
		reader.wantFrames()
		bystander.wantFrames()
	})

	t.Run("each conversation gets its own key, and every publish carries the next epoch of the whole stream", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		api.publishChatRead(ChatRead{ReaderID: "owner", PeerID: "mira", LastReadTS: 1}, "owner")
		api.publishChatRead(ChatRead{ReaderID: "owner", PeerID: "kip", LastReadTS: 2}, "owner")
		api.publishChatRead(ChatRead{ReaderID: "owner", PeerID: "mira", LastReadTS: 3}, "owner")

		dashboard.wantFrames(
			apiTestChatReadFrame(1, "owner::owner::mira", 1, "owner", "mira", 1),
			apiTestChatReadFrame(2, "owner::owner::kip", 2, "owner", "kip", 2),
			apiTestChatReadFrame(3, "owner::owner::mira", 3, "owner", "mira", 3),
		)
	})
}

func apiTestChatReadFrame(seq int, key string, epoch int, reader, peer string, ts float64) map[string]any {
	return map[string]any{
		"seq":   seq,
		"topic": "chat_read",
		"op":    "patch",
		"data": map[string]any{
			"entity":  "chat_read",
			"key":     key,
			"epoch":   epoch,
			"deleted": false,
			"payload": map[string]any{
				"reader":       reader,
				"peer":         peer,
				"last_read_ts": ts,
			},
		},
		"ts":      apiAnyNumber,
		"trigger": "owner",
	}
}

func TestSniffAttachmentMime(t *testing.T) {
	t.Run("each image magic-byte prefix is named and everything else falls to the octet-stream", func(t *testing.T) {
		for name, c := range map[string]struct {
			raw  []byte
			want string
		}{
			"a png signature":                   {[]byte("\x89PNG\r\n\x1a\nIHDR"), "image/png"},
			"a jpeg signature":                  {[]byte{0xff, 0xd8, 0xff, 0xe0}, "image/jpeg"},
			"the 87a gif signature":             {[]byte("GIF87a\x01\x00"), "image/gif"},
			"the 89a gif signature":             {[]byte("GIF89a\x01\x00"), "image/gif"},
			"a webp riff container":             {[]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), "image/webp"},
			"a riff container that is not webp": {[]byte("RIFF\x00\x00\x00\x00WAVEfmt "), "application/octet-stream"},
			"a png signature cut short":         {[]byte("\x89PNG"), "application/octet-stream"},
			"plain text":                        {[]byte("zzz"), "application/octet-stream"},
			"no bytes at all":                   {nil, "application/octet-stream"},
		} {
			if got := sniffAttachmentMime(c.raw); got != c.want {
				t.Fatalf("sniffAttachmentMime(%s) = %q, want %q", name, got, c.want)
			}
		}
	})
}

func TestResolveChatRecipient(t *testing.T) {
	t.Run("the owner and every active AI member are durable addresses, whatever their presence", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		for _, id := range []string{"owner", "mira", "kip", "  mira  "} {
			got, err := api.resolveChatRecipient(id)
			if err != nil || got != strings.TrimSpace(id) {
				t.Fatalf("resolveChatRecipient(%q) = %q, %v", id, got, err)
			}
		}
	})

	t.Run("a warden, an invented id and a blank address are all refused as not found", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		for _, id := range []string{"m-server-self", "ghost", "", "   "} {
			got, err := api.resolveChatRecipient(id)
			if got != "" || err != errNotFound {
				t.Fatalf("resolveChatRecipient(%q) = %q, %v", id, got, err)
			}
		}
	})

	t.Run("a member the owner has dismissed stops being an address", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		if got, err := api.resolveChatRecipient("kip"); got != "kip" || err != nil {
			t.Fatalf("before the dismissal: %q, %v", got, err)
		}

		if status, data := apiJSON(t, h, "DELETE", "/api/members/kip", owner, ""); status != 200 {
			t.Fatalf("dismiss: %d %v", status, data)
		}

		got, err := api.resolveChatRecipient("kip")
		if got != "" || err != errNotFound {
			t.Fatalf("after the dismissal: %q, %v", got, err)
		}
	})
}

func TestDecodeChatAttachment(t *testing.T) {
	t.Run("bare base64 under a declared name and mime is stored as sent, under a fresh id", func(t *testing.T) {
		att, err := decodeChatAttachment("aGVsbG8=", "notes.txt", "text/plain")
		if err != nil {
			t.Fatalf("decodeChatAttachment: %v", err)
		}
		apiWantDecodedAttachment(t, att, "text/plain", "notes.txt", "hello")
	})

	t.Run("a data-URI carries its own mime, and the pasted-image name is defaulted from it", func(t *testing.T) {
		att, err := decodeChatAttachment("data:image/png;base64,iVBORw0KGgo=", "", "")
		if err != nil {
			t.Fatalf("decodeChatAttachment: %v", err)
		}
		apiWantDecodedAttachment(t, att, "image/png", "pasted-image.png", "\x89PNG\r\n\x1a\n")
	})

	t.Run("the caller's mime wins over the one the data-URI declares", func(t *testing.T) {
		att, err := decodeChatAttachment("data:image/png;base64,iVBORw0KGgo=", "shot.txt", "text/plain")
		if err != nil {
			t.Fatalf("decodeChatAttachment: %v", err)
		}
		apiWantDecodedAttachment(t, att, "text/plain", "shot.txt", "\x89PNG\r\n\x1a\n")
	})

	t.Run("with no mime declared anywhere the bytes are sniffed", func(t *testing.T) {
		att, err := decodeChatAttachment("iVBORw0KGgo=", "", "")
		if err != nil {
			t.Fatalf("decodeChatAttachment: %v", err)
		}
		apiWantDecodedAttachment(t, att, "image/png", "pasted-image.png", "\x89PNG\r\n\x1a\n")

		att, err = decodeChatAttachment("emJ6", "", "")
		if err != nil {
			t.Fatalf("decodeChatAttachment: %v", err)
		}
		apiWantDecodedAttachment(t, att, "application/octet-stream", "", "zbz")
	})

	t.Run("a client fault answers the message the caller is shown and no blob at all", func(t *testing.T) {
		for name, c := range map[string]struct{ data, filename, mime, want string }{
			"a data-URI that is not base64": {"data:image/png,iVBORw0KGgo=", "", "", "attachment must be base64-encoded"},
			"a data-URI with no comma":      {"data:image/png;base64", "", "", "attachment must be base64-encoded"},
			"bytes that are not base64":     {"!!!not base64!!!", "", "", "attachment is not valid base64"},
			"nothing sent at all":           {"", "", "", "attachment is empty"},
			"only whitespace":               {"   ", "", "", "attachment is empty"},
			"a filename over the cap":       {"aGVsbG8=", strings.Repeat("x", 129), "text/plain", "attachment filename is 129 chars, over the 128-char limit"},
		} {
			att, err := decodeChatAttachment(c.data, c.filename, c.mime)
			if att != nil {
				t.Fatalf("%s: want no attachment, got %#v", name, att)
			}
			if err == nil || err.Error() != c.want {
				t.Fatalf("%s: error = %v, want %q", name, err, c.want)
			}
		}
	})

	t.Run("a filename at the cap passes", func(t *testing.T) {
		name := strings.Repeat("字", 128)
		att, err := decodeChatAttachment("aGVsbG8=", name, "text/plain")
		if err != nil {
			t.Fatalf("decodeChatAttachment: %v", err)
		}
		apiWantDecodedAttachment(t, att, "text/plain", name, "hello")
	})
}

func apiWantDecodedAttachment(t *testing.T, att *ChatAttachment, mime, filename, data string) {
	t.Helper()
	if att == nil {
		t.Fatalf("want an attachment, got nil")
	}
	if !strings.HasPrefix(att.ID, "att-") || len(att.ID) != 16 {
		t.Fatalf("id: want a fresh att- id, got %q", att.ID)
	}
	if att.Mime != mime {
		t.Fatalf("mime: want %q, got %q", mime, att.Mime)
	}
	got := ""
	if att.Filename != nil {
		got = *att.Filename
	}
	if filename == "" {
		if att.Filename != nil {
			t.Fatalf("filename: want it absent, got %q", got)
		}
	} else if got != filename {
		t.Fatalf("filename: want %q, got %q", filename, got)
	}
	if string(att.Data) != data {
		t.Fatalf("data: want %q, got %q", data, string(att.Data))
	}
}

func TestResolveChatAttachment(t *testing.T) {
	t.Run("declared bytes keep their mime and name and get a fresh id", func(t *testing.T) {
		att, err := resolveChatAttachment([]byte("hello"), "  notes.txt  ", "  text/plain  ")
		if err != nil {
			t.Fatalf("resolveChatAttachment: %v", err)
		}
		apiWantDecodedAttachment(t, att, "text/plain", "notes.txt", "hello")
	})

	t.Run("an undeclared mime is sniffed, and only a sniffed image earns the pasted-image name", func(t *testing.T) {
		att, err := resolveChatAttachment([]byte("GIF89a\x01"), "", "")
		if err != nil {
			t.Fatalf("resolveChatAttachment: %v", err)
		}
		apiWantDecodedAttachment(t, att, "image/gif", "pasted-image.gif", "GIF89a\x01")

		att, err = resolveChatAttachment([]byte("zzz"), "   ", "")
		if err != nil {
			t.Fatalf("resolveChatAttachment: %v", err)
		}
		apiWantDecodedAttachment(t, att, "application/octet-stream", "", "zzz")
	})

	t.Run("an image mime with no extension of its own still defaults to a png name", func(t *testing.T) {
		att, err := resolveChatAttachment([]byte("<svg/>"), "", "image/svg+xml")
		if err != nil {
			t.Fatalf("resolveChatAttachment: %v", err)
		}
		apiWantDecodedAttachment(t, att, "image/svg+xml", "pasted-image.png", "<svg/>")
	})

	t.Run("the image cap bounds images only, and a non-image of the same size passes", func(t *testing.T) {
		raw := make([]byte, chatAttachmentImageMaxBytes+1)
		copy(raw, "\x89PNG\r\n\x1a\n")
		att, err := resolveChatAttachment(raw, "", "")
		if att != nil {
			t.Fatalf("want no attachment, got %q", att.ID)
		}
		if err == nil || err.Error() != "image exceeds the 20 MB size limit" {
			t.Fatalf("error = %v", err)
		}

		att, err = resolveChatAttachment(raw, "big.bin", "application/octet-stream")
		if err != nil {
			t.Fatalf("resolveChatAttachment of a same-sized non-image: %v", err)
		}
		if att.Mime != "application/octet-stream" || len(att.Data) != chatAttachmentImageMaxBytes+1 {
			t.Fatalf("got %q, %d bytes", att.Mime, len(att.Data))
		}
	})

	t.Run("no bytes and an over-long name are refused before anything is minted", func(t *testing.T) {
		for name, c := range map[string]struct {
			raw            []byte
			filename, want string
		}{
			"no bytes":          {nil, "notes.txt", "attachment is empty"},
			"an empty slice":    {[]byte{}, "notes.txt", "attachment is empty"},
			"an over-long name": {[]byte("hello"), strings.Repeat("字", 129), "attachment filename is 129 chars, over the 128-char limit"},
		} {
			att, err := resolveChatAttachment(c.raw, c.filename, "text/plain")
			if att != nil {
				t.Fatalf("%s: want no attachment, got %#v", name, att)
			}
			if err == nil || err.Error() != c.want {
				t.Fatalf("%s: error = %v, want %q", name, err, c.want)
			}
		}
	})
}

func TestAttachmentRef(t *testing.T) {
	t.Run("a named blob answers exactly the three light-ref fields", func(t *testing.T) {
		name := "notes.txt"
		got := attachmentRef(&ChatAttachment{
			ID: "att-000000000001", Mime: "text/plain", Filename: &name,
			Data: []byte("hello"),
		})
		apiWantValue(t, "ref", any(got), map[string]any{
			"id": "att-000000000001", "mime": "text/plain", "filename": "notes.txt",
		})
	})

	t.Run("a blob carrying no filename folds it to the empty string rather than dropping the field", func(t *testing.T) {
		got := attachmentRef(&ChatAttachment{
			ID: "att-000000000002", Mime: "application/octet-stream", Data: []byte("zzz"),
		})
		apiWantValue(t, "ref", any(got), map[string]any{
			"id": "att-000000000002", "mime": "application/octet-stream", "filename": "",
		})
	})
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
	t.Run("a ref is looked up and an inline item is decoded, and only the inline one is marked for writing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		_, uploaded := apiJSON(t, h, "POST",
			"/api/chat/attachments?filename=notes.txt&mime=text/plain", owner, "hello")
		storedID, _ := uploaded["id"].(string)

		resolved, status, problem := api.resolveChatAttachmentInputs([]ChatAttachmentInputDTO{
			{Id: apiTestStrPtr(storedID), Filename: apiTestStrPtr("ignored.txt"), Mime: apiTestStrPtr("image/png")},
			{DataB64: apiTestStrPtr("d29ybGQ="), Filename: apiTestStrPtr("inline.txt"), Mime: apiTestStrPtr("text/plain")},
		})
		if status != 0 || problem != "" {
			t.Fatalf("want no violation, got %d %q", status, problem)
		}
		if len(resolved) != 2 {
			t.Fatalf("resolved %d items", len(resolved))
		}
		if resolved[0].store || resolved[0].att.ID != storedID ||
			resolved[0].att.Mime != "text/plain" || *resolved[0].att.Filename != "notes.txt" ||
			string(resolved[0].att.Data) != "hello" {
			t.Fatalf("the stored blob is authoritative, got %#v", resolved[0].att)
		}
		if !resolved[1].store {
			t.Fatalf("the inline item must be marked for writing")
		}
		apiWantDecodedAttachment(t, resolved[1].att, "text/plain", "inline.txt", "world")
	})

	t.Run("a message carrying no attachment resolves to nothing at all", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		resolved, status, problem := api.resolveChatAttachmentInputs(nil)
		if resolved != nil || status != 0 || problem != "" {
			t.Fatalf("got %#v %d %q", resolved, status, problem)
		}
	})

	t.Run("every malformed item is refused by name, and nothing is resolved beside it", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		for name, c := range map[string]struct {
			in   ChatAttachmentInputDTO
			want string
		}{
			"both an id and bytes":     {ChatAttachmentInputDTO{Id: apiTestStrPtr("att-1"), DataB64: apiTestStrPtr("aGk=")}, "attachment carries both id and data_b64"},
			"neither id nor bytes":     {ChatAttachmentInputDTO{}, "attachment carries neither id nor data_b64"},
			"a name but no bytes":      {ChatAttachmentInputDTO{Filename: apiTestStrPtr("notes.txt")}, "attachment carries neither id nor data_b64"},
			"an id nothing carries":    {ChatAttachmentInputDTO{Id: apiTestStrPtr("att-nosuchblob")}, "attachment 'att-nosuchblob' not found"},
			"bytes that do not decode": {ChatAttachmentInputDTO{DataB64: apiTestStrPtr("!!!")}, "attachment is not valid base64"},
		} {
			resolved, status, problem := api.resolveChatAttachmentInputs([]ChatAttachmentInputDTO{c.in})
			if resolved != nil {
				t.Fatalf("%s: want nothing resolved, got %#v", name, resolved)
			}
			if status != 400 || problem != c.want {
				t.Fatalf("%s: got %d %q, want 400 %q", name, status, problem, c.want)
			}
		}
	})

	t.Run("a rejected item takes its good siblings down with it, so nothing is stored half-way", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/chat", owner,
			`{"to":"mira","body":"see these","attachments":[`+
				`{"data_b64":"aGVsbG8=","filename":"good.txt","mime":"text/plain"},`+
				`{"id":"att-nosuchblob"}]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "attachment 'att-nosuchblob' not found")
		apiWantStoredBlobs(t, d)
		apiWantBody(t, apiTestChatListing(t, h, owner), map[string]any{"messages": []any{}})
		dashboard.wantFrames()
	})
}

func apiTestStrPtr(s string) *string { return &s }

func apiTestChatListing(t *testing.T, h http.Handler, token string) map[string]any {
	t.Helper()
	status, data := apiJSON(t, h, "GET", "/api/chat", token, "")
	if status != 200 {
		t.Fatalf("read the stream: %d (%v)", status, data)
	}
	return data
}

func TestPendingAttachments(t *testing.T) {
	t.Run("every item is projected into a light ref, and only the inline ones are handed back to be written", func(t *testing.T) {
		stored := "notes.txt"
		fresh := "inline.txt"
		refs, blobs := pendingAttachments([]resolvedAttachment{
			{att: &ChatAttachment{ID: "att-000000000001", Mime: "text/plain", Filename: &stored, Data: []byte("hello")}},
			{att: &ChatAttachment{ID: "att-000000000002", Mime: "text/plain", Filename: &fresh, Data: []byte("world")}, store: true},
			{att: &ChatAttachment{ID: "att-000000000003", Mime: "application/octet-stream", Data: []byte("zzz")}, store: true},
		})
		apiWantValue(t, "refs", any(refs), []any{
			map[string]any{"id": "att-000000000001", "mime": "text/plain", "filename": "notes.txt"},
			map[string]any{"id": "att-000000000002", "mime": "text/plain", "filename": "inline.txt"},
			map[string]any{"id": "att-000000000003", "mime": "application/octet-stream", "filename": ""},
		})
		if len(blobs) != 2 || blobs[0].ID != "att-000000000002" || blobs[1].ID != "att-000000000003" {
			t.Fatalf("blobs to write: %#v", blobs)
		}
		if string(blobs[0].Data) != "world" || string(blobs[1].Data) != "zzz" {
			t.Fatalf("blob bytes: %q %q", blobs[0].Data, blobs[1].Data)
		}
	})

	t.Run("a message carrying nothing projects an empty ref list and no blob to write", func(t *testing.T) {
		refs, blobs := pendingAttachments(nil)
		if refs == nil || len(refs) != 0 {
			t.Fatalf("refs = %#v, want an empty non-nil list", refs)
		}
		if blobs != nil {
			t.Fatalf("blobs = %#v, want nil", blobs)
		}
	})

	t.Run("a message whose every item is an already-stored ref hands back no blob to write", func(t *testing.T) {
		refs, blobs := pendingAttachments([]resolvedAttachment{
			{att: &ChatAttachment{ID: "att-000000000001", Mime: "text/plain"}},
		})
		apiWantValue(t, "refs", any(refs), []any{
			map[string]any{"id": "att-000000000001", "mime": "text/plain", "filename": ""},
		})
		if blobs != nil {
			t.Fatalf("blobs = %#v, want nil", blobs)
		}
	})
}

func TestHandlePostChatApiChatPost(t *testing.T) {
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
	t.Run("the receipt carries the id, the recipient, the timestamp and the attachments that landed", func(t *testing.T) {
		got := chatPostReceiptOf(ChatMessage{
			ID: "c-000000000001", Sender: "owner", Recipient: "mira",
			Body: "see this", TS: 1700000000.5,
			Meta: map[string]any{"attachments": []any{
				map[string]any{"id": "att-000000000001", "mime": "text/plain", "filename": "notes.txt"},
				map[string]any{"id": "att-000000000002", "mime": "image/png", "filename": "shot.png"},
			}},
		})
		if got.ID != "c-000000000001" || got.To != "mira" || got.TS != 1700000000.5 {
			t.Fatalf("receipt head = %#v", got)
		}
		apiWantValue(t, "attachments", any(apiTestJSONOf(t, got.Attachments)), []any{
			map[string]any{
				"id": "att-000000000001", "url": "/api/chat/attachment/att-000000000001",
				"filename": "notes.txt", "mime": "text/plain", "is_image": false,
			},
			map[string]any{
				"id": "att-000000000002", "url": "/api/chat/attachment/att-000000000002",
				"filename": "shot.png", "mime": "image/png", "is_image": true,
			},
		})
	})

	t.Run("a message that carried no file answers an empty attachment list, never a missing one", func(t *testing.T) {
		got := chatPostReceiptOf(ChatMessage{
			ID: "c-000000000002", Sender: "mira", Recipient: "owner", Body: "hi", TS: 1,
		})
		if got.ID != "c-000000000002" || got.To != "owner" || got.TS != 1 {
			t.Fatalf("receipt head = %#v", got)
		}
		apiWantValue(t, "attachments", any(apiTestJSONOf(t, got.Attachments)), []any{})
	})

	t.Run("the receipt says nothing about the body, the sender or the meta the message carried", func(t *testing.T) {
		got := chatPostReceiptOf(ChatMessage{
			ID: "c-000000000003", Sender: "owner", Recipient: "mira", Body: "secret",
			TS: 2, Meta: map[string]any{"source": "cli"},
		})
		apiWantValue(t, "receipt", apiTestJSONOf(t, got), map[string]any{
			"id": "c-000000000003", "to": "mira", "ts": float64(2), "attachments": []any{},
		})
	})
}

func apiTestJSONOf(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestServedChatMessageDTO(t *testing.T) {
	t.Run("a plain message is projected with its ids and no read-time joins at all", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		_, posted := apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"hi","meta":{"source":"cli"}}`)

		dto, err := api.servedChatMessageDTO(apiTestChatRowOf(t, d, posted["id"].(string)))
		if err != nil {
			t.Fatalf("servedChatMessageDTO: %v", err)
		}
		apiWantValue(t, "dto", apiTestJSONOf(t, dto), map[string]any{
			"id": posted["id"], "from": "owner", "from_name": "", "to": "mira", "to_name": "",
			"body": "hi", "body_omitted_chars": 0, "ts": apiAnyNumber, "ts_display": "",
			"meta": map[string]any{"source": "cli"}, "reply_card_status": "",
			"attachments": []any{}, "reply_to": "",
		})
	})

	t.Run("a reply carries the quoted message beside the id it names", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		_, first := apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"the original"}`)
		_, second := apiJSON(t, h, "POST", "/api/chat", owner,
			`{"to":"mira","body":"quoting you","reply_to":"`+first["id"].(string)+`"}`)

		dto, err := api.servedChatMessageDTO(apiTestChatRowOf(t, d, second["id"].(string)))
		if err != nil {
			t.Fatalf("servedChatMessageDTO: %v", err)
		}
		apiWantValue(t, "dto", apiTestJSONOf(t, dto), map[string]any{
			"id": second["id"], "from": "owner", "from_name": "", "to": "mira", "to_name": "",
			"body": "quoting you", "body_omitted_chars": 0, "ts": apiAnyNumber, "ts_display": "",
			"meta": map[string]any{"reply_to": first["id"]}, "reply_card_status": "",
			"attachments": []any{}, "reply_to": first["id"],
			"reply_to_chat": map[string]any{
				"id": first["id"], "from": "owner", "from_name": "",
				"to": "mira", "to_name": "", "content": "the original",
			},
		})
	})

	t.Run("a card-bearing message joins the card's LIVE status, which moves when the card is answered", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		status, card := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要出貨","options":[{"text":"出"},{"text":"不出"}],"linked_task":null}`)
		if status != 200 {
			t.Fatalf("open the card: %d %v", status, card)
		}
		cardID, _ := card["id"].(string)
		messageID, _ := card["chat_message_id"].(string)

		dto, err := api.servedChatMessageDTO(apiTestChatRowOf(t, d, messageID))
		if err != nil {
			t.Fatalf("servedChatMessageDTO: %v", err)
		}
		if dto.ReplyCardStatus != "waiting" {
			t.Fatalf("reply_card_status while waiting = %q", dto.ReplyCardStatus)
		}

		if status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/answer", owner,
			`{"option_idxs":[0]}`); status != 200 {
			t.Fatalf("answer the card: %d %v", status, data)
		}

		dto, err = api.servedChatMessageDTO(apiTestChatRowOf(t, d, messageID))
		if err != nil {
			t.Fatalf("servedChatMessageDTO: %v", err)
		}
		if dto.ReplyCardStatus != "answered" {
			t.Fatalf("reply_card_status after the answer = %q", dto.ReplyCardStatus)
		}
		if dto.Card != nil {
			t.Fatalf("the served view folds no card content, got %#v", dto.Card)
		}
	})

	t.Run("a message naming a card that is no longer there is served with an empty status rather than refused", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		dto, err := api.servedChatMessageDTO(ChatMessage{
			ID: "c-000000000001", Sender: "mira", Recipient: "owner", Body: "請示", TS: 1,
			Meta: map[string]any{"reply_card_id": "rc-nosuchcard"},
		})
		if err != nil {
			t.Fatalf("servedChatMessageDTO: %v", err)
		}
		if dto.ReplyCardStatus != "" {
			t.Fatalf("reply_card_status = %q, want the empty string", dto.ReplyCardStatus)
		}
	})
}

func apiTestChatRowOf(t *testing.T, d *DAL, id string) ChatMessage {
	t.Helper()
	msgs, err := d.ListChatByIDs([]string{id})
	if err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	if len(msgs) != 1 {
		t.Fatalf("read %s: got %d rows", id, len(msgs))
	}
	return msgs[0]
}

func TestChatReplyQuote(t *testing.T) {
	t.Run("the quoted message comes back as who said it, who it was said to, and what it said", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		_, posted := apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"the original"}`)
		id, _ := posted["id"].(string)

		quote, err := api.chatReplyQuote(id, map[string]string{"mira": "Mira"})
		if err != nil {
			t.Fatalf("chatReplyQuote: %v", err)
		}
		if quote == nil {
			t.Fatalf("want a quote, got nil")
		}
		apiWantValue(t, "quote", apiTestJSONOf(t, *quote), map[string]any{
			"id": id, "from": "owner", "from_name": "Owner",
			"to": "mira", "to_name": "Mira", "content": "the original",
		})
	})

	t.Run("with no name table the ids are still carried and the names stay honestly empty", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		_, posted := apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"the original"}`)
		id, _ := posted["id"].(string)

		quote, err := api.chatReplyQuote(id, nil)
		if err != nil || quote == nil {
			t.Fatalf("chatReplyQuote = %#v, %v", quote, err)
		}
		apiWantValue(t, "quote", apiTestJSONOf(t, *quote), map[string]any{
			"id": id, "from": "owner", "from_name": "",
			"to": "mira", "to_name": "", "content": "the original",
		})
	})

	t.Run("a message that replies to nothing, and one naming an id nobody carries, both answer no quote and no error", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		for _, id := range []string{"", "c-nosuchmessage"} {
			quote, err := api.chatReplyQuote(id, nil)
			if quote != nil || err != nil {
				t.Fatalf("chatReplyQuote(%q) = %#v, %v", id, quote, err)
			}
		}
	})
}

func TestRequestedChatIDs(t *testing.T) {
	t.Run("blanks are dropped, duplicates collapse and the request order survives", func(t *testing.T) {
		ids := []string{" c-b ", "", "c-a", "   ", "c-b", "c-a", "c-c"}
		if got := requestedChatIDs(&ids); !reflect.DeepEqual(got, []string{"c-b", "c-a", "c-c"}) {
			t.Fatalf("requestedChatIDs = %#v", got)
		}
	})

	t.Run("an absent parameter answers nil and a parameter that carries only blanks answers the empty slice", func(t *testing.T) {
		if got := requestedChatIDs(nil); got != nil {
			t.Fatalf("requestedChatIDs(nil) = %#v, want nil", got)
		}
		blanks := []string{"", "   "}
		got := requestedChatIDs(&blanks)
		if got == nil || len(got) != 0 {
			t.Fatalf("requestedChatIDs(blanks) = %#v, want an empty non-nil slice", got)
		}
	})
}

func TestServeChatByIDs(t *testing.T) {
	t.Run("the named messages come back in full, oldest first whatever order they were asked in, and nothing else", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		_, first := apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"one"}`)
		_, second := apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"kip","body":"two"}`)
		apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"three"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET",
			"/api/chat?ids="+second["id"].(string)+"&ids="+first["id"].(string), owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{
			apiTestChatRow(first["id"].(string), "owner", "mira", "one"),
			apiTestChatRow(second["id"].(string), "owner", "kip", "two"),
		}})
		dashboard.wantFrames()
	})

	t.Run("a message between two other members is readable by id, the same rows the listing already serves", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, posted := apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"not kip's"}`)

		status, data := apiJSON(t, h, "GET", "/api/chat?ids="+posted["id"].(string), agent, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{
			apiTestChatRow(posted["id"].(string), "owner", "mira", "not kip's"),
		}})
	})

	t.Run("one id nobody carries refuses the whole call rather than answering it short", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		_, first := apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"one"}`)

		status, data := apiJSON(t, h, "GET",
			"/api/chat?ids="+first["id"].(string)+"&ids=c-nosuchmessage", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found",
			"no message carries id c-nosuchmessage — the whole call is refused rather "+
				"than answered short, because a shortened answer is indistinguishable "+
				"from the folded message you are trying to read back; drop that id and "+
				"ask again")
	})

	t.Run("more ids than one call may name is refused before any row is read, and the refusal states the limit", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		asked := make([]string, 0, 21)
		for i := 0; i < 21; i++ {
			asked = append(asked, "ids=c-"+strconv.Itoa(i))
		}
		status, data := apiJSON(t, h, "GET", "/api/chat?"+strings.Join(asked, "&"), owner, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"get_chat accepts at most 20 ids per call (asked for 21) — messages come "+
				"back with their bodies whole, so the count is what bounds the response; "+
				"name the ones you actually need and call again for the rest")
	})

	t.Run("re-reading a message by id advances no watermark, and the ones already written stay put", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, posted := apiJSON(t, h, "POST", "/api/chat", agent, `{"to":"owner","body":"one"}`)

		if status, data := apiJSON(t, h, "GET", "/api/chat?ids="+posted["id"].(string), owner, ""); status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantChatReads(t, h, owner)

		apiJSON(t, h, "POST", "/api/chat/mark-read", owner, `{"peer":"kip","last_read_ts":5}`)
		if status, data := apiJSON(t, h, "GET", "/api/chat?ids="+posted["id"].(string), owner, ""); status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantChatReads(t, h, owner, map[string]any{
			"reader_id": "owner", "peer_id": "kip", "last_read_ts": float64(5),
		})
	})
}

func TestChatDeclaredQueryParams(t *testing.T) {
	t.Run("the accepted set is every parameter the route declares plus the transport token, sorted", func(t *testing.T) {
		if got := chatDeclaredQueryParams(); !reflect.DeepEqual(got, []string{
			"before_id", "before_ts", "cursor", "end_id", "ids", "limit",
			"recipient", "sender", "start_id", "token", "unread", "with",
		}) {
			t.Fatalf("chatDeclaredQueryParams = %#v", got)
		}
	})
}

func TestUnknownChatQueryParams(t *testing.T) {
	t.Run("a request whose parameters are all declared answers nothing unknown", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/chat?with=mira&limit=5&token=x&ids=c-1&ids=c-2", nil)
		if got := unknownChatQueryParams(req); got != nil {
			t.Fatalf("unknownChatQueryParams = %#v, want nil", got)
		}
	})

	t.Run("several undeclared parameters come back sorted, so one request is refused the same way every time", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/chat?zzz=1&with=mira&nope=2&Limit=3", nil)
		if got := unknownChatQueryParams(req); !reflect.DeepEqual(got, []string{"Limit", "nope", "zzz"}) {
			t.Fatalf("unknownChatQueryParams = %#v", got)
		}
	})
}

func TestEncodeChatCursor(t *testing.T) {
	t.Run("the token is base64url of the direction, the shortest exact ts and the id", func(t *testing.T) {
		if got := encodeChatCursor(chatCursorOlder, chatAnchor{TS: 1.5, ID: "c-1"}); got != "bwAxLjUAYy0x" {
			t.Fatalf("older cursor = %q", got)
		}
		if got := encodeChatCursor(chatCursorNewer, chatAnchor{TS: 1700000000, ID: "c-2"}); got != "bgAxLjdlKzA5AGMtMg" {
			t.Fatalf("newer cursor = %q", got)
		}
	})

	t.Run("the same position minted for the two directions produces two different tokens", func(t *testing.T) {
		a := chatAnchor{TS: 1.5, ID: "c-1"}
		if encodeChatCursor(chatCursorOlder, a) == encodeChatCursor(chatCursorNewer, a) {
			t.Fatalf("both directions minted the same token")
		}
	})
}

func TestDecodeChatCursor(t *testing.T) {
	t.Run("a token this API minted decodes back to the exact position it was minted from", func(t *testing.T) {
		for _, ts := range []float64{1.5, 1700000000, 1788845011.1138828} {
			a, refusal, ok := decodeChatCursor(
				encodeChatCursor(chatCursorOlder, chatAnchor{TS: ts, ID: "c-1"}), chatCursorOlder)
			if !ok || refusal != "" || a.TS != ts || a.ID != "c-1" {
				t.Fatalf("round trip of %v = %+v %q %v", ts, a, refusal, ok)
			}
		}
	})

	t.Run("a token minted by the other walk is refused with both directions spelled out and no position", func(t *testing.T) {
		a, refusal, ok := decodeChatCursor(
			encodeChatCursor(chatCursorNewer, chatAnchor{TS: 1.5, ID: "c-1"}), chatCursorOlder)
		if ok || a != (chatAnchor{}) {
			t.Fatalf("want a refusal and no position, got %+v %v", a, ok)
		}
		if refusal != "this cursor continues towards newer messages and cannot be used on a "+
			"path that continues towards older messages — a cursor belongs to the query "+
			"that minted it; start that walk again without one" {
			t.Fatalf("refusal = %q", refusal)
		}
	})

	t.Run("anything this API did not mint is refused as unreadable", func(t *testing.T) {
		for name, token := range map[string]string{
			"not base64":            "!!!",
			"empty":                 "",
			"an unknown direction":  base64.RawURLEncoding.EncodeToString([]byte("x\x001\x00c-1")),
			"a ts that is no float": base64.RawURLEncoding.EncodeToString([]byte("o\x00zz\x00c-1")),
			"an empty id":           base64.RawURLEncoding.EncodeToString([]byte("o\x001\x00")),
			"too few parts":         base64.RawURLEncoding.EncodeToString([]byte("o\x001")),
		} {
			a, refusal, ok := decodeChatCursor(token, chatCursorOlder)
			if ok || a != (chatAnchor{}) {
				t.Fatalf("%s: want a refusal and no position, got %+v %v", name, a, ok)
			}
			if refusal != "cursor is not a cursor this API issued — copy the previous "+
				"response's next_cursor back verbatim; it is opaque, and constructing "+
				"or editing one is not supported" {
				t.Fatalf("%s: refusal = %q", name, refusal)
			}
		}
	})
}

func TestChatCursorDirName(t *testing.T) {
	t.Run("each direction tag reads as words, and anything else reads as the older walk", func(t *testing.T) {
		if got := chatCursorDirName(chatCursorNewer); got != "towards newer messages" {
			t.Fatalf("newer = %q", got)
		}
		for _, dir := range []string{chatCursorOlder, "", "x"} {
			if got := chatCursorDirName(dir); got != "towards older messages" {
				t.Fatalf("chatCursorDirName(%q) = %q", dir, got)
			}
		}
	})
}

func TestChatPageWindow(t *testing.T) {
	t.Run("a capped page asks for one row more than it will return", func(t *testing.T) {
		for limit, want := range map[int]int{1: 2, 30: 31, 200: 201} {
			if got := chatPageWindow(limit); got != want {
				t.Fatalf("chatPageWindow(%d) = %d, want %d", limit, got, want)
			}
		}
	})

	t.Run("an uncapped or empty read has no next page to detect and asks for no extra row", func(t *testing.T) {
		for _, limit := range []int{0, -1, -30} {
			if got := chatPageWindow(limit); got != limit {
				t.Fatalf("chatPageWindow(%d) = %d, want it unchanged", limit, got)
			}
		}
	})
}

func TestWriteChatPage(t *testing.T) {
	t.Run("every path answers the same object, and the continuation appears only where there is another page", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		_, first := apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"one"}`)
		_, second := apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"two"}`)

		for name, target := range map[string]string{
			"the newest page with nothing older": "/api/chat",
			"the named messages":                 "/api/chat?ids=" + first["id"].(string) + "&ids=" + second["id"].(string),
			"a window naming both its ends":      "/api/chat?start_id=" + first["id"].(string) + "&end_id=" + second["id"].(string),
			"an older walk that reaches the end": "/api/chat?before_ts=99999999999&before_id=c-zzz",
		} {
			status, data := apiJSON(t, h, "GET", target, owner, "")
			if status != 200 {
				t.Fatalf("%s: want 200, got %d (%v)", name, status, data)
			}
			if _, named := data["next_cursor"]; named {
				t.Fatalf("%s: a page with nothing more carries %v", name, data["next_cursor"])
			}
			if _, named := data["messages"]; !named {
				t.Fatalf("%s: the answer is not the page envelope (%v)", name, data)
			}
		}

		status, data := apiJSON(t, h, "GET", "/api/chat?limit=1", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"messages":    []any{apiTestChatRow(second["id"].(string), "owner", "mira", "two")},
			"next_cursor": apiAnyString,
		})
	})

	t.Run("each rendered message carries the read-time joins, so a page and a by-id read agree field for field", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		_, card := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要出貨","options":[{"text":"出"}],"linked_task":null}`)
		messageID, _ := card["chat_message_id"].(string)
		_, reply := apiJSON(t, h, "POST", "/api/chat", owner,
			`{"to":"mira","body":"quoting","reply_to":"`+messageID+`"}`)

		status, page := apiJSON(t, h, "GET", "/api/chat?with=mira", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, page)
		}
		status, byID := apiJSON(t, h, "GET",
			"/api/chat?ids="+messageID+"&ids="+reply["id"].(string), owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, byID)
		}
		apiWantValue(t, "by-id page", byID["messages"], page["messages"])

		rows, _ := page["messages"].([]any)
		if len(rows) != 2 {
			t.Fatalf("want two rows, got %v", rows)
		}
		asked, _ := rows[0].(map[string]any)
		if asked["reply_card_status"] != "waiting" {
			t.Fatalf("reply_card_status = %v", asked["reply_card_status"])
		}
		quoting, _ := rows[1].(map[string]any)
		apiWantValue(t, "reply_to_chat", quoting["reply_to_chat"], map[string]any{
			"id": messageID, "from": "mira", "from_name": "", "to": "owner",
			"to_name": "", "content": "要不要出貨",
		})
	})
}

func TestRequestedChatWindow(t *testing.T) {
	t.Run("an anchor that was sent is reported as sent, whatever it carries", func(t *testing.T) {
		start, blank := "c-1", ""
		startID, hasStart, endID, hasEnd := requestedChatWindow(HandleListChatApiChatGetParams{
			StartId: &start, EndId: &blank,
		})
		if startID != "c-1" || !hasStart || endID != "" || !hasEnd {
			t.Fatalf("got %q %v %q %v", startID, hasStart, endID, hasEnd)
		}
	})

	t.Run("an anchor that was not sent at all is reported absent, so a blank one cannot be mistaken for it", func(t *testing.T) {
		startID, hasStart, endID, hasEnd := requestedChatWindow(HandleListChatApiChatGetParams{})
		if startID != "" || hasStart || endID != "" || hasEnd {
			t.Fatalf("got %q %v %q %v", startID, hasStart, endID, hasEnd)
		}
	})

	t.Run("one end alone is a window with only that end", func(t *testing.T) {
		end := "c-9"
		startID, hasStart, endID, hasEnd := requestedChatWindow(HandleListChatApiChatGetParams{EndId: &end})
		if startID != "" || hasStart || endID != "c-9" || !hasEnd {
			t.Fatalf("got %q %v %q %v", startID, hasStart, endID, hasEnd)
		}
	})
}

func TestServeChatWindow(t *testing.T) {
	t.Run("both ends are inside the window and the page runs oldest to newest", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		ids := apiTestChatStream(t, h, owner, "one", "two", "three", "four")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET",
			"/api/chat?start_id="+ids[1]+"&end_id="+ids[2], owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{
			apiTestChatRow(ids[1], "owner", "mira", "two"),
			apiTestChatRow(ids[2], "owner", "mira", "three"),
		}})
		apiWantChatReads(t, h, owner)
		dashboard.wantFrames()
	})

	t.Run("one anchor alone walks to that end of the stream", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		ids := apiTestChatStream(t, h, owner, "one", "two", "three")

		status, data := apiJSON(t, h, "GET", "/api/chat?start_id="+ids[1], owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{
			apiTestChatRow(ids[1], "owner", "mira", "two"),
			apiTestChatRow(ids[2], "owner", "mira", "three"),
		}})

		status, data = apiJSON(t, h, "GET", "/api/chat?end_id="+ids[1], owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{
			apiTestChatRow(ids[0], "owner", "mira", "one"),
			apiTestChatRow(ids[1], "owner", "mira", "two"),
		}})
	})

	t.Run("a window whose anchors sit outside the filter answers empty, where the same window unfiltered does not", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		ids := apiTestChatStream(t, h, owner, "one", "two")

		status, data := apiJSON(t, h, "GET",
			"/api/chat?with=kip&start_id="+ids[0]+"&end_id="+ids[1], owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{}})

		status, data = apiJSON(t, h, "GET",
			"/api/chat?with=mira&start_id="+ids[0]+"&end_id="+ids[1], owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{
			apiTestChatRow(ids[0], "owner", "mira", "one"),
			apiTestChatRow(ids[1], "owner", "mira", "two"),
		}})
	})

	t.Run("a request that is wrong in more than one way is refused in a fixed order: cursor family, then limit, then the anchors", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		ids := apiTestChatStream(t, h, owner, "one", "two")

		status, data := apiJSON(t, h, "GET",
			"/api/chat?start_id=c-nope&limit=0&before_ts=1&before_id=c-1", owner, "")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"start_id/end_id cannot be combined with the deprecated before_ts/before_id "+
				"cursor — the two families disagree about direction; send one family or "+
				"the other")

		status, data = apiJSON(t, h, "GET", "/api/chat?start_id=c-nope&limit=0&cursor=x", owner, "")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"cursor cannot be combined with before_ts/before_id or start_id/end_id — "+
				"one keyset walk per request; send the cursor alone")

		for _, limit := range []int{0, -1, 201} {
			status, data = apiJSON(t, h, "GET",
				"/api/chat?start_id=c-nope&limit="+strconv.Itoa(limit), owner, "")
			if status != 422 {
				t.Fatalf("limit %d: want 422, got %d (%v)", limit, status, data)
			}
			apiWantError(t, data, "validation_error",
				"limit must be between 1 and 200 when start_id or end_id is given (got "+
					strconv.Itoa(limit)+") — the legacy anchorless listing keeps its own "+
					"semantics, where a negative limit is uncapped and 0 is an empty page")
		}

		status, data = apiJSON(t, h, "GET", "/api/chat?start_id=c-nope&end_id="+ids[1], owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found",
			"no message carries id c-nope — a window anchor must name a real message, "+
				"because an empty page is what a real window at the edge of the stream "+
				"returns and the two must not be indistinguishable")

		status, data = apiJSON(t, h, "GET",
			"/api/chat?start_id="+ids[1]+"&end_id="+ids[0], owner, "")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"start_id "+ids[1]+" is newer than end_id "+ids[0]+" — the window is empty "+
				"by construction; refused rather than answered with an empty array, "+
				"which is what a real empty window returns")
	})

	t.Run("a window names both its edges, so it never hands back a continuation", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		ids := apiTestChatStream(t, h, owner, "one", "two", "three")

		status, data := apiJSON(t, h, "GET", "/api/chat?start_id="+ids[0]+"&limit=1", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{
			apiTestChatRow(ids[0], "owner", "mira", "one"),
		}})
	})
}

func apiTestChatStream(t *testing.T, h http.Handler, owner string, bodies ...string) []string {
	t.Helper()
	ids := make([]string, 0, len(bodies))
	for _, body := range bodies {
		status, data := apiJSON(t, h, "POST", "/api/chat", owner,
			`{"to":"mira","body":"`+body+`"}`)
		if status != 200 {
			t.Fatalf("post %q: %d %v", body, status, data)
		}
		ids = append(ids, data["id"].(string))
	}
	return ids
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
	t.Run("the deprecated pair and the cursor that replaces it answer the same page byte for byte", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		ids := apiTestChatStream(t, h, owner, "one", "two", "three")
		dashboard := apiTestListen(t, api, "")

		status, page := apiJSON(t, h, "GET", "/api/chat?limit=1", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, page)
		}
		cursor, _ := page["next_cursor"].(string)

		status, byCursor := apiJSON(t, h, "GET", "/api/chat?limit=1&cursor="+cursor, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, byCursor)
		}
		newest, _ := page["messages"].([]any)[0].(map[string]any)
		status, byPair := apiJSON(t, h, "GET", "/api/chat?limit=1&before_ts="+
			strconv.FormatFloat(newest["ts"].(float64), 'f', -1, 64)+
			"&before_id="+ids[2], owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, byPair)
		}
		apiWantValue(t, "the pair's page", any(byPair), any(byCursor))
		apiWantBody(t, byCursor, map[string]any{
			"messages":    []any{apiTestChatRow(ids[1], "owner", "mira", "two")},
			"next_cursor": apiAnyString,
		})
		apiWantChatReads(t, h, owner)
		dashboard.wantFrames()
	})

	t.Run("the anchor is excluded and the walk ends by handing back no continuation", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		ids := apiTestChatStream(t, h, owner, "one", "two")

		status, data := apiJSON(t, h, "GET", "/api/chat?limit=1", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		status, data = apiJSON(t, h, "GET",
			"/api/chat?limit=1&cursor="+data["next_cursor"].(string), owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{
			apiTestChatRow(ids[0], "owner", "mira", "one"),
		}})
	})

	t.Run("half of the deprecated pair, and a cursor pointing the other way, are both refused", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiTestChatStream(t, h, owner, "one")

		for _, target := range []string{
			"/api/chat?before_ts=1", "/api/chat?before_id=c-1",
		} {
			status, data := apiJSON(t, h, "GET", target, owner, "")
			if status != 422 {
				t.Fatalf("%s: want 422, got %d (%v)", target, status, data)
			}
			apiWantError(t, data, "validation_error",
				"before_ts and before_id must be supplied together")
		}

		status, data := apiJSON(t, h, "GET",
			"/api/chat?cursor="+encodeChatCursor(chatCursorNewer, chatAnchor{TS: 1, ID: "c-1"}),
			owner, "")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"this cursor continues towards newer messages and cannot be used on a path "+
				"that continues towards older messages — a cursor belongs to the query "+
				"that minted it; start that walk again without one")
	})

	t.Run("the older walk keeps the participant filter it was started with", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		ids := apiTestChatStream(t, h, owner, "one", "two")
		apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"kip","body":"elsewhere"}`)

		status, data := apiJSON(t, h, "GET", "/api/chat?with=mira&limit=1", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		status, data = apiJSON(t, h, "GET",
			"/api/chat?with=mira&limit=1&cursor="+data["next_cursor"].(string), owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{
			apiTestChatRow(ids[0], "owner", "mira", "one"),
		}})
	})
}

func TestTrimChatPageOlder(t *testing.T) {
	t.Run("the surplus row comes off the front and the cursor names the oldest row still in the page", func(t *testing.T) {
		msgs, next := trimChatPageOlder([]ChatMessage{
			{ID: "c-1", TS: 1}, {ID: "c-2", TS: 2}, {ID: "c-3", TS: 3},
		}, 2)
		apiWantChatIDs(t, msgs, "c-2", "c-3")
		if next != "bwAyAGMtMg" {
			t.Fatalf("next cursor = %q", next)
		}
	})

	t.Run("a page with no surplus row mints no cursor, which is how the walk ends", func(t *testing.T) {
		for name, n := range map[string]int{"exactly the limit": 2, "short of it": 1} {
			msgs, next := trimChatPageOlder([]ChatMessage{{ID: "c-1", TS: 1}, {ID: "c-2", TS: 2}}[:n], 2)
			if len(msgs) != n || next != "" {
				t.Fatalf("%s: got %d rows and cursor %q", name, len(msgs), next)
			}
		}
	})

	t.Run("an uncapped or empty read returns what it was given and names no position", func(t *testing.T) {
		all := []ChatMessage{{ID: "c-1", TS: 1}, {ID: "c-2", TS: 2}}
		for _, limit := range []int{0, -1} {
			msgs, next := trimChatPageOlder(all, limit)
			apiWantChatIDs(t, msgs, "c-1", "c-2")
			if next != "" {
				t.Fatalf("limit %d minted %q", limit, next)
			}
		}
	})
}

func apiWantChatIDs(t *testing.T, msgs []ChatMessage, want ...string) {
	t.Helper()
	got := []string{}
	for _, m := range msgs {
		got = append(got, m.ID)
	}
	if !reflect.DeepEqual(got, append([]string{}, want...)) {
		t.Fatalf("message ids: want %v, got %v", want, got)
	}
}

func TestServeChatUnread(t *testing.T) {
	t.Run("the caller's own unread comes back oldest first, and what the caller sent is not unread", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, first := apiJSON(t, h, "POST", "/api/chat", agent, `{"to":"owner","body":"one"}`)
		_, second := apiJSON(t, h, "POST", "/api/chat", agent, `{"to":"owner","body":"two"}`)
		apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"kip","body":"mine"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/chat?unread=true", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{
			apiTestChatRow(first["id"].(string), "kip", "owner", "one"),
			apiTestChatRow(second["id"].(string), "kip", "owner", "two"),
		}})
		apiWantChatReads(t, h, owner)
		dashboard.wantFrames()
	})

	t.Run("limit takes the OLDEST batch and the cursor it mints walks towards the newer", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, first := apiJSON(t, h, "POST", "/api/chat", agent, `{"to":"owner","body":"one"}`)
		_, second := apiJSON(t, h, "POST", "/api/chat", agent, `{"to":"owner","body":"two"}`)

		status, data := apiJSON(t, h, "GET", "/api/chat?unread=true&limit=1", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"messages":    []any{apiTestChatRow(first["id"].(string), "kip", "owner", "one")},
			"next_cursor": apiAnyString,
		})

		status, data = apiJSON(t, h, "GET",
			"/api/chat?unread=true&limit=1&cursor="+data["next_cursor"].(string), owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{
			apiTestChatRow(second["id"].(string), "kip", "owner", "two"),
		}})
	})

	t.Run("marking the conversation read empties the backfill that was not empty a moment ago", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, posted := apiJSON(t, h, "POST", "/api/chat", agent, `{"to":"owner","body":"one"}`)

		status, data := apiJSON(t, h, "GET", "/api/chat?unread=true", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{
			apiTestChatRow(posted["id"].(string), "kip", "owner", "one"),
		}})

		apiJSON(t, h, "POST", "/api/chat/mark-read", owner,
			`{"peer":"kip","last_read_ts":`+
				strconv.FormatFloat(posted["ts"].(float64), 'f', -1, 64)+`}`)

		status, data = apiJSON(t, h, "GET", "/api/chat?unread=true", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{}})
	})

	t.Run("this route keeps the legacy limit semantics: zero is an empty page and a negative one is uncapped", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, first := apiJSON(t, h, "POST", "/api/chat", agent, `{"to":"owner","body":"one"}`)
		_, second := apiJSON(t, h, "POST", "/api/chat", agent, `{"to":"owner","body":"two"}`)

		status, data := apiJSON(t, h, "GET", "/api/chat?unread=true&limit=0", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{}})

		status, data = apiJSON(t, h, "GET", "/api/chat?unread=true&limit=-1", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"messages": []any{
			apiTestChatRow(first["id"].(string), "kip", "owner", "one"),
			apiTestChatRow(second["id"].(string), "kip", "owner", "two"),
		}})
	})

	t.Run("a cursor minted by the older walk cannot be replayed here", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/chat?unread=true&cursor="+
			encodeChatCursor(chatCursorOlder, chatAnchor{TS: 1, ID: "c-1"}), owner, "")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"this cursor continues towards older messages and cannot be used on a path "+
				"that continues towards newer messages — a cursor belongs to the query "+
				"that minted it; start that walk again without one")
	})
}

func TestTrimChatPageNewer(t *testing.T) {
	t.Run("the surplus row comes off the back and the cursor names the newest row still in the page", func(t *testing.T) {
		msgs, next := trimChatPageNewer([]ChatMessage{
			{ID: "c-1", TS: 1}, {ID: "c-2", TS: 2}, {ID: "c-3", TS: 3},
		}, 2)
		apiWantChatIDs(t, msgs, "c-1", "c-2")
		if next != "bgAyAGMtMg" {
			t.Fatalf("next cursor = %q", next)
		}
	})

	t.Run("a page with no surplus row mints no cursor, which is how the backfill ends", func(t *testing.T) {
		for name, n := range map[string]int{"exactly the limit": 2, "short of it": 1} {
			msgs, next := trimChatPageNewer([]ChatMessage{{ID: "c-1", TS: 1}, {ID: "c-2", TS: 2}}[:n], 2)
			if len(msgs) != n || next != "" {
				t.Fatalf("%s: got %d rows and cursor %q", name, len(msgs), next)
			}
		}
	})

	t.Run("an uncapped or empty read returns what it was given and names no position", func(t *testing.T) {
		all := []ChatMessage{{ID: "c-1", TS: 1}, {ID: "c-2", TS: 2}}
		for _, limit := range []int{0, -1} {
			msgs, next := trimChatPageNewer(all, limit)
			apiWantChatIDs(t, msgs, "c-1", "c-2")
			if next != "" {
				t.Fatalf("limit %d minted %q", limit, next)
			}
		}
	})

	t.Run("the two directions mint different tokens from the same trimmed page", func(t *testing.T) {
		page := []ChatMessage{{ID: "c-1", TS: 1}, {ID: "c-2", TS: 2}, {ID: "c-3", TS: 3}}
		_, older := trimChatPageOlder(append([]ChatMessage{}, page...), 2)
		_, newer := trimChatPageNewer(append([]ChatMessage{}, page...), 2)
		if older == newer {
			t.Fatalf("both walks minted %q", older)
		}
	})
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
	t.Run("marking a conversation read answers a receipt saying it advanced and fans one owner-only chat_read frame", func(t *testing.T) {
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
			"peer_id":      "mira",
			"last_read_ts": float64(5),
			"advanced":     true,
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

	t.Run("a watermark behind the stored one answers the standing watermark with advanced false and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/chat/mark-read", owner, `{"peer":"mira","last_read_ts":5}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/chat/mark-read", owner,
			`{"peer":"mira","last_read_ts":3}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"peer_id":      "mira",
			"last_read_ts": float64(5),
			"advanced":     false,
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
	t.Run("an out-of-box caller wakes to an empty chat, the studio floor, and an overview whose only cost is the header", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		snap, err := api.resumeSnapshotParts("owner")
		if err != nil {
			t.Fatalf("resumeSnapshotParts: %v", err)
		}
		apiWantWallClock(t, "generated_at", snap.GeneratedAt)
		apiWantValue(t, "chat", apiTestJSONOf(t, snap.Chat), []any{})
		apiWantValue(t, "chat_earlier_omitted", apiTestJSONOf(t, snap.ChatCut),
			map[string]any{"omitted": false, "hint": ""})
		apiWantValue(t, "tasks", apiTestJSONOf(t, snap.Tasks), []any{})
		apiWantValue(t, "roster", apiTestJSONOf(t, snap.Roster), any(apiTestSeedRoster()))
		apiWantValue(t, "machines", apiTestJSONOf(t, snap.Machines), any(apiTestSeedMachines()))
		apiWantValue(t, "overview", apiTestJSONOf(t, snap.Overview), any(apiTestResumeOverview(0, 26)))
	})

	t.Run("the caller's own line rides the snapshot and its cost lands in the overview", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		_, posted := apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"hi"}`)

		snap, err := api.resumeSnapshotParts("owner")
		if err != nil {
			t.Fatalf("resumeSnapshotParts: %v", err)
		}
		apiWantChatDTOIDs(t, snap.Chat, posted["id"].(string))
		apiWantValue(t, "overview", apiTestJSONOf(t, snap.Overview), any(apiTestResumeOverview(1, 64)))
		if snap.Chat[0].FromName != "Owner" || snap.Chat[0].ToName != "Mira" ||
			snap.Chat[0].TSDisplay == "" {
			t.Fatalf("the row carries no names or no rendered time: %#v", snap.Chat[0])
		}
	})

	t.Run("only the subject's own line is assembled, and a line between two others is not", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"kip","body":"not mira's"}`)
		_, posted := apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"hi"}`)

		snap, err := api.resumeSnapshotParts("mira")
		if err != nil {
			t.Fatalf("resumeSnapshotParts: %v", err)
		}
		apiWantChatDTOIDs(t, snap.Chat, posted["id"].(string))
		apiWantValue(t, "overview", apiTestJSONOf(t, snap.Overview), any(apiTestResumeOverview(1, 64)))
	})

	t.Run("a waiting card the subject asked is counted, and one somebody else asked is not", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		mira := apiTestAgentToken(t, api, "mira", "")
		kip := apiTestAgentToken(t, api, "kip", "")
		if status, data := apiJSON(t, h, "POST", "/api/reply-cards", mira,
			`{"kind":"decision","summary":"要不要出貨","options":[{"text":"出"}],"linked_task":null}`); status != 200 {
			t.Fatalf("open mira's card: %d %v", status, data)
		}
		if status, data := apiJSON(t, h, "POST", "/api/reply-cards", kip,
			`{"kind":"decision","summary":"要不要漲價","options":[{"text":"漲"}],"linked_task":null}`); status != 200 {
			t.Fatalf("open kip's card: %d %v", status, data)
		}

		snap, err := api.resumeSnapshotParts("mira")
		if err != nil {
			t.Fatalf("resumeSnapshotParts: %v", err)
		}
		if snap.Overview.CardsWaiting != 1 || snap.Overview.CardsAnsweredRecent != 0 {
			t.Fatalf("card counts = %d waiting, %d answered", snap.Overview.CardsWaiting,
				snap.Overview.CardsAnsweredRecent)
		}
	})

	t.Run("a caller with no identity still lands on the floor, with no chat and no cards read at all", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"hi"}`)

		snap, err := api.resumeSnapshotParts("")
		if err != nil {
			t.Fatalf("resumeSnapshotParts: %v", err)
		}
		apiWantValue(t, "chat", apiTestJSONOf(t, snap.Chat), []any{})
		apiWantValue(t, "roster", apiTestJSONOf(t, snap.Roster), any(apiTestSeedRoster()))
		apiWantValue(t, "overview", apiTestJSONOf(t, snap.Overview), any(apiTestResumeOverview(0, 26)))
	})
}

func TestResumeDisplayTime(t *testing.T) {
	t.Run("an epoch second renders as a full date, time and zone offset the server can be read back from", func(t *testing.T) {
		got := resumeDisplayTime(1700000000)
		parsed, err := time.Parse("2006-01-02 15:04:05 -07:00", got)
		if err != nil {
			t.Fatalf("resumeDisplayTime(1700000000) = %q: %v", got, err)
		}
		if parsed.Unix() != 1700000000 {
			t.Fatalf("resumeDisplayTime(1700000000) = %q, which is %d", got, parsed.Unix())
		}
		if want := time.Unix(1700000000, 0).Local().
			Format("2006-01-02 15:04:05 -07:00"); got != want {
			t.Fatalf("resumeDisplayTime(1700000000) = %q, want %q", got, want)
		}
		if n := len([]rune(got)); n != 26 {
			t.Fatalf("resumeDisplayTime(1700000000) = %q, %d runes", got, n)
		}
	})

	t.Run("a fractional second is rendered on the second it falls in", func(t *testing.T) {
		if got := resumeDisplayTime(1700000000.75); got != resumeDisplayTime(1700000000) {
			t.Fatalf("resumeDisplayTime(1700000000.75) = %q", got)
		}
	})

	t.Run("an absent timestamp renders as nothing rather than as the epoch", func(t *testing.T) {
		for _, ts := range []float64{0, -1} {
			if got := resumeDisplayTime(ts); got != "" {
				t.Fatalf("resumeDisplayTime(%v) = %q, want the empty string", ts, got)
			}
		}
	})
}

func TestResumeDisplayName(t *testing.T) {
	t.Run("the owner has a name of its own, a roster id resolves, and an id nobody carries stays empty", func(t *testing.T) {
		names := map[string]string{"mira": "Mira", "kip": "Kip"}
		for id, want := range map[string]string{
			"owner": "Owner",
			"mira":  "Mira",
			"kip":   "Kip",
			"ghost": "",
			"":      "",
		} {
			if got := resumeDisplayName(id, names); got != want {
				t.Fatalf("resumeDisplayName(%q) = %q, want %q", id, got, want)
			}
		}
	})

	t.Run("the owner keeps its name even when no name table was handed in", func(t *testing.T) {
		if got := resumeDisplayName("owner", nil); got != "Owner" {
			t.Fatalf("resumeDisplayName(owner, nil) = %q", got)
		}
		if got := resumeDisplayName("mira", nil); got != "" {
			t.Fatalf("resumeDisplayName(mira, nil) = %q, want the empty string", got)
		}
	})
}

func TestResumeChatCarriesFullBody(t *testing.T) {
	t.Run("the subject's hand-off to itself and every line touching the owner are carried whole", func(t *testing.T) {
		for name, m := range map[string]ChatMessage{
			"the subject's own baton": {Sender: "mira", Recipient: "mira"},
			"the owner writing in":    {Sender: "owner", Recipient: "kip"},
			"an answer to the owner":  {Sender: "kip", Recipient: "owner"},
		} {
			if !resumeChatCarriesFullBody("mira", m) {
				t.Fatalf("%s: want it exempt from collapsing", name)
			}
		}
	})

	t.Run("third-party traffic and another agent's hand-off to itself are collapsible", func(t *testing.T) {
		for name, m := range map[string]ChatMessage{
			"two other agents talking":      {Sender: "kip", Recipient: "zed"},
			"another agent's own baton":     {Sender: "kip", Recipient: "kip"},
			"a message the subject sent on": {Sender: "mira", Recipient: "kip"},
		} {
			if resumeChatCarriesFullBody("mira", m) {
				t.Fatalf("%s: want it collapsible", name)
			}
		}
	})

	t.Run("with no subject named, a self hand-off is no longer exempt on that ground alone", func(t *testing.T) {
		if resumeChatCarriesFullBody("", ChatMessage{Sender: "kip", Recipient: "kip"}) {
			t.Fatalf("want it collapsible when no subject is named")
		}
		if !resumeChatCarriesFullBody("", ChatMessage{Sender: "owner", Recipient: "owner"}) {
			t.Fatalf("the owner's own line stays exempt whatever the subject is")
		}
	})
}

func TestResumeChatMessageDTO(t *testing.T) {
	t.Run("names and a rendered time ride beside the ids, and a body the subject is entitled to is carried whole", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		names := map[string]string{"mira": "Mira", "kip": "Kip"}

		got, err := api.resumeChatMessageDTO("mira", ChatMessage{
			ID: "c-000000000001", Sender: "owner", Recipient: "mira",
			Body: strings.Repeat("字", 300), TS: 1700000000,
		}, names, nil)
		if err != nil {
			t.Fatalf("resumeChatMessageDTO: %v", err)
		}
		apiWantValue(t, "dto", apiTestJSONOf(t, got), map[string]any{
			"id": "c-000000000001", "from": "owner", "from_name": "Owner",
			"to": "mira", "to_name": "Mira", "body": strings.Repeat("字", 300),
			"body_omitted_chars": 0, "ts": float64(1700000000),
			"ts_display": resumeDisplayTime(1700000000), "meta": map[string]any{},
			"reply_card_status": "", "attachments": []any{}, "reply_to": "",
		})
	})

	t.Run("a third party's long body is folded and the marker says how much was taken", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		got, err := api.resumeChatMessageDTO("mira", ChatMessage{
			ID: "c-000000000002", Sender: "kip", Recipient: "zed",
			Body: strings.Repeat("字", 300), TS: 1700000000,
		}, map[string]string{"kip": "Kip"}, nil)
		if err != nil {
			t.Fatalf("resumeChatMessageDTO: %v", err)
		}
		if got.Body != strings.Repeat("字", 120)+"…" || got.BodyOmittedChars != 180 {
			t.Fatalf("body = %q, omitted %d", got.Body, got.BodyOmittedChars)
		}
		if got.ToName != "" {
			t.Fatalf("to_name = %q, want it honestly empty for an id nobody carries", got.ToName)
		}
	})

	t.Run("a third party's body that is barely over the preview is left whole, because folding it would save nothing", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		got, err := api.resumeChatMessageDTO("mira", ChatMessage{
			ID: "c-000000000003", Sender: "kip", Recipient: "zed",
			Body: strings.Repeat("字", 122), TS: 1700000000,
		}, nil, nil)
		if err != nil {
			t.Fatalf("resumeChatMessageDTO: %v", err)
		}
		if got.Body != strings.Repeat("字", 122) || got.BodyOmittedChars != 0 {
			t.Fatalf("body = %q, omitted %d", got.Body, got.BodyOmittedChars)
		}

		got, err = api.resumeChatMessageDTO("mira", ChatMessage{
			ID: "c-000000000004", Sender: "kip", Recipient: "zed",
			Body: strings.Repeat("字", 123), TS: 1700000000,
		}, nil, nil)
		if err != nil {
			t.Fatalf("resumeChatMessageDTO: %v", err)
		}
		if got.Body != strings.Repeat("字", 120)+"…" || got.BodyOmittedChars != 3 {
			t.Fatalf("body = %q, omitted %d", got.Body, got.BodyOmittedChars)
		}
	})

	t.Run("a card the subject asked is folded in place; one it merely answered leaves only the status", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		cards := map[string]ReplyCard{"rc-1": {
			ID: "rc-1", FromMember: "mira", Status: "answered",
			Options:          []ReplyCardOption{{Text: "出"}, {Text: "不出"}},
			AnswerOptionIdxs: []int{0}, AnswerText: "就出", AnsweredTS: 1700000000,
		}}
		msg := ChatMessage{
			ID: "c-000000000005", Sender: "mira", Recipient: "owner", Body: "請示",
			TS: 1700000000, Meta: map[string]any{"reply_card_id": "rc-1"},
		}

		got, err := api.resumeChatMessageDTO("mira", msg, nil, cards)
		if err != nil {
			t.Fatalf("resumeChatMessageDTO: %v", err)
		}
		if got.ReplyCardStatus != "answered" || got.Card == nil {
			t.Fatalf("status %q, card %#v", got.ReplyCardStatus, got.Card)
		}
		apiWantValue(t, "card", apiTestJSONOf(t, *got.Card), map[string]any{
			"options": []any{
				map[string]any{"text": "出", "ai_pick": false},
				map[string]any{"text": "不出", "ai_pick": false},
			},
			"answer_option_idxs":  []any{float64(0)},
			"answer_text":         "就出",
			"answered_ts":         float64(1700000000),
			"answered_at_display": resumeDisplayTime(1700000000),
		})

		got, err = api.resumeChatMessageDTO("kip", msg, nil, cards)
		if err != nil {
			t.Fatalf("resumeChatMessageDTO: %v", err)
		}
		if got.ReplyCardStatus != "answered" || got.Card != nil {
			t.Fatalf("someone else's card: status %q, card %#v", got.ReplyCardStatus, got.Card)
		}
	})

	t.Run("a reply carries its quote, resolved through the snapshot's own name table", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		_, first := apiJSON(t, h, "POST", "/api/chat", owner, `{"to":"mira","body":"the original"}`)
		_, second := apiJSON(t, h, "POST", "/api/chat", owner,
			`{"to":"mira","body":"quoting","reply_to":"`+first["id"].(string)+`"}`)

		got, err := api.resumeChatMessageDTO("mira", apiTestChatRowOf(t, d, second["id"].(string)),
			map[string]string{"mira": "Mira"}, nil)
		if err != nil {
			t.Fatalf("resumeChatMessageDTO: %v", err)
		}
		if got.ReplyToChat == nil {
			t.Fatalf("want a quote, got nil")
		}
		apiWantValue(t, "quote", apiTestJSONOf(t, *got.ReplyToChat), map[string]any{
			"id": first["id"], "from": "owner", "from_name": "Owner",
			"to": "mira", "to_name": "Mira", "content": "the original",
		})
	})
}

func TestResumeChatCollapseIsWorthIt(t *testing.T) {
	t.Run("a fold is worth it only when it saves strictly more than the marker it costs", func(t *testing.T) {
		for omitted, want := range map[int]bool{
			0: false, 1: false, 2: false, 3: true, 9: true,
			10: true, 100: true, 1000: true,
		} {
			if got := resumeChatCollapseIsWorthIt(omitted); got != want {
				t.Fatalf("resumeChatCollapseIsWorthIt(%d) = %v, want %v", omitted, got, want)
			}
		}
	})
}

func TestResumeChatMessageChars(t *testing.T) {
	t.Run("the carried body, both display names and the rendered time are billed in runes", func(t *testing.T) {
		got := resumeChatMessageChars(chatMessageDTO{
			ID: "c-000000000001", From: "owner", To: "mira", ReplyTo: "c-000000000002",
			FromName: "Owner", ToName: "Mira", Body: "你好世界",
			TSDisplay: "2026-01-02 03:04:05 +08:00",
		})
		if got != 40 {
			t.Fatalf("chars = %d, want 40", got)
		}
	})

	t.Run("a collapsed body is billed as carried, plus the digits of the marker beside it", func(t *testing.T) {
		got := resumeChatMessageChars(chatMessageDTO{Body: "abc…", BodyOmittedChars: 120})
		if got != 7 {
			t.Fatalf("chars = %d, want 7", got)
		}
	})

	t.Run("the quote line is prose and is billed, but no id-shaped field ever is", func(t *testing.T) {
		got := resumeChatMessageChars(chatMessageDTO{
			ID: "c-000000000001", ReplyTo: "c-000000000002",
			ReplyToChat: &chatReplyQuoteDTO{
				ID: "c-000000000002", From: "owner", To: "mira",
				FromName: "Owner", ToName: "Mira", Content: "the original",
			},
		})
		if got != 22 {
			t.Fatalf("chars = %d, want 22", got)
		}
	})

	t.Run("a folded card bills its option texts, the answer and the rendered answer time", func(t *testing.T) {
		got := resumeChatMessageChars(chatMessageDTO{
			Card: &chatInlineReplyCardDTO{
				Options:           []ReplyCardOption{{Text: "出"}, {Text: "不出"}},
				AnswerOptionIdxs:  []int{0, 10},
				AnswerText:        "就出",
				AnsweredTS:        1700000000,
				AnsweredAtDisplay: "2026-01-02 03:04:05 +08:00",
			},
		})
		if got != 35 {
			t.Fatalf("chars = %d, want 35", got)
		}
	})

	t.Run("an empty projection still bills the one digit its zero marker costs", func(t *testing.T) {
		if got := resumeChatMessageChars(chatMessageDTO{}); got != 1 {
			t.Fatalf("chars = %d, want 1", got)
		}
	})
}

func TestResumeChatBlock(t *testing.T) {
	t.Run("a stream that fits comes back whole, oldest first, and reports what it cost", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		msgs := []ChatMessage{
			{ID: "c-1", Sender: "owner", Recipient: "mira", Body: "one", TS: 1},
			{ID: "c-2", Sender: "mira", Recipient: "owner", Body: "two", TS: 2},
		}

		chat, cut, used, err := api.resumeChatBlock("mira", msgs, map[string]string{"mira": "Mira"}, nil, 13000)
		if err != nil {
			t.Fatalf("resumeChatBlock: %v", err)
		}
		apiWantChatDTOIDs(t, chat, "c-1", "c-2")
		if cut != (resumeChatCutDTO{}) {
			t.Fatalf("cut = %#v, want nothing left out", cut)
		}
		if want := resumeChatMessageChars(chat[0]) + resumeChatMessageChars(chat[1]); used != want {
			t.Fatalf("used = %d, want %d", used, want)
		}
	})

	t.Run("the budget stops the walk at the last message that fits, and everything older is left out with a hint", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		msgs := []ChatMessage{
			{ID: "c-1", Sender: "owner", Recipient: "mira", Body: strings.Repeat("字", 50), TS: 1},
			{ID: "c-2", Sender: "owner", Recipient: "mira", Body: strings.Repeat("字", 50), TS: 2},
			{ID: "c-3", Sender: "owner", Recipient: "mira", Body: strings.Repeat("字", 50), TS: 3},
		}

		chat, cut, used, err := api.resumeChatBlock("mira", msgs, nil, nil, 170)
		if err != nil {
			t.Fatalf("resumeChatBlock: %v", err)
		}
		apiWantChatDTOIDs(t, chat, "c-2", "c-3")
		if !cut.Omitted || cut.Hint == "" {
			t.Fatalf("cut = %#v, want the omission reported", cut)
		}
		if used != 164 {
			t.Fatalf("used = %d, want 164", used)
		}
	})

	t.Run("a budget that fits nothing empties the block rather than overspending on one message", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		msgs := []ChatMessage{{ID: "c-1", Sender: "owner", Recipient: "mira", Body: "one", TS: 1}}

		chat, cut, used, err := api.resumeChatBlock("mira", msgs, nil, nil, 0)
		if err != nil {
			t.Fatalf("resumeChatBlock: %v", err)
		}
		if chat == nil || len(chat) != 0 || used != 0 || !cut.Omitted {
			t.Fatalf("chat = %#v, used = %d, cut = %#v", chat, used, cut)
		}
	})

	t.Run("a caller with nothing said yet gets an empty block and no claim that anything was left out", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		chat, cut, used, err := api.resumeChatBlock("mira", nil, nil, nil, 13000)
		if err != nil {
			t.Fatalf("resumeChatBlock: %v", err)
		}
		if chat == nil || len(chat) != 0 || used != 0 || cut != (resumeChatCutDTO{}) {
			t.Fatalf("chat = %#v, used = %d, cut = %#v", chat, used, cut)
		}
	})

	t.Run("a read that filled its own window reports the cut even when the budget dropped nothing", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		msgs := make([]ChatMessage, resumeChatFetch)
		for i := range msgs {
			msgs[i] = ChatMessage{ID: "c-" + strconv.Itoa(i), Sender: "owner", Recipient: "mira", TS: 0}
		}

		chat, cut, used, err := api.resumeChatBlock("mira", msgs, nil, nil, 13000)
		if err != nil {
			t.Fatalf("resumeChatBlock: %v", err)
		}
		if len(chat) != resumeChatFetch || used != 3000 {
			t.Fatalf("kept %d messages costing %d", len(chat), used)
		}
		if !cut.Omitted || cut.Hint == "" {
			t.Fatalf("cut = %#v, want the read cap reported", cut)
		}
	})
}

func apiWantChatDTOIDs(t *testing.T, chat []chatMessageDTO, want ...string) {
	t.Helper()
	got := []string{}
	for _, d := range chat {
		got = append(got, d.ID)
	}
	if !reflect.DeepEqual(got, append([]string{}, want...)) {
		t.Fatalf("chat ids: want %v, got %v", want, got)
	}
}

func TestResumeChatPackBudget(t *testing.T) {
	t.Run("the header and the cut hint are set aside before the packer is given anything to spend", func(t *testing.T) {
		header := "2026-01-02 03:04:05 +08:00"
		if got := resumeChatPackBudget(13000, header); got != 12526 {
			t.Fatalf("resumeChatPackBudget(13000) = %d, want 12526", got)
		}
		if got := resumeChatPackBudget(13000, ""); got != 12552 {
			t.Fatalf("resumeChatPackBudget with no header = %d, want 12552", got)
		}
	})

	t.Run("a budget the reservations swallow comes out at zero rather than negative", func(t *testing.T) {
		for _, budget := range []int{0, 100, 473} {
			if got := resumeChatPackBudget(budget, "2026-01-02 03:04:05 +08:00"); got != 0 {
				t.Fatalf("resumeChatPackBudget(%d) = %d, want 0", budget, got)
			}
		}
		if got := resumeChatPackBudget(475, "2026-01-02 03:04:05 +08:00"); got != 1 {
			t.Fatalf("resumeChatPackBudget(475) = %d, want 1", got)
		}
	})
}

func TestResumeFloorParts(t *testing.T) {
	t.Run("an out-of-box floor is the two staff rows, the server's own machine, and the sizes the peek reports", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		roster, machines, rosterSize, machinesSize, names, err := api.resumeFloorParts("owner")
		if err != nil {
			t.Fatalf("resumeFloorParts: %v", err)
		}
		apiWantValue(t, "roster", apiTestJSONOf(t, roster), any(apiTestSeedRoster()))
		apiWantValue(t, "machines", apiTestJSONOf(t, machines), any(apiTestSeedMachines()))
		if rosterSize != 213 || machinesSize != 19 {
			t.Fatalf("sizes = %d, %d, want 213, 19", rosterSize, machinesSize)
		}
		if !reflect.DeepEqual(names, map[string]string{
			"kip": "Kip", "mira": "Mira", "m-server-self": "伺服器這一台",
		}) {
			t.Fatalf("names = %#v", names)
		}
	})

	t.Run("a dismissed colleague leaves the roster but keeps its name, so an old hand-off still reads", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "DELETE", "/api/members/kip", owner, ""); status != 200 {
			t.Fatalf("dismiss: %d %v", status, data)
		}

		roster, _, rosterSize, _, names, err := api.resumeFloorParts("owner")
		if err != nil {
			t.Fatalf("resumeFloorParts: %v", err)
		}
		apiWantValue(t, "roster", apiTestJSONOf(t, roster), []any{apiTestSeedRoster()[1]})
		if rosterSize != 193 {
			t.Fatalf("roster_chars = %d, want 193", rosterSize)
		}
		if names["kip"] != "Kip" {
			t.Fatalf("names = %#v, want the dismissed colleague still named", names)
		}
	})
}

func TestContractorTaskFields(t *testing.T) {
	t.Run("a contractor's duty is the title of the one task it is bound to, truncated, beside that task's status and step progress", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		title := strings.Repeat("字", 45)
		if status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"`+title+`","executor_member_id":"kip"}`); status != 200 {
			t.Fatalf("create task: %d %v", status, data)
		}
		if err := d.PutOutsourceWorker(OutsourceWorker{
			ID: "ow-000000000001", Codename: "Contractor", TaskID: "T-1",
			Status: "assigned", Runtime: "claude", Model: "sonnet", Effort: "medium",
		}); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}

		gotTitle, gotStatus, gotReason, done, total := api.contractorTaskFields("ow-000000000001",
			map[string]TaskStepProgress{"T-1": {Done: 3, Total: 12}})
		if gotTitle != strings.Repeat("字", 40)+"…" {
			t.Fatalf("title = %q", gotTitle)
		}
		if gotStatus != "not_started" || gotReason != "" || done != 3 || total != 12 {
			t.Fatalf("got %q %q %d/%d", gotStatus, gotReason, done, total)
		}
	})

	t.Run("a task with no step of its own is simply absent from the progress map and reads 0 of 0", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship the crate","executor_member_id":"kip"}`); status != 200 {
			t.Fatalf("create task: %d %v", status, data)
		}
		if err := d.PutOutsourceWorker(OutsourceWorker{
			ID: "ow-000000000001", Codename: "Contractor", TaskID: "T-1",
			Status: "assigned", Runtime: "claude", Model: "sonnet", Effort: "medium",
		}); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}

		gotTitle, gotStatus, gotReason, done, total := api.contractorTaskFields("ow-000000000001", nil)
		if gotTitle != "Ship the crate" || gotStatus != "not_started" || gotReason != "" ||
			done != 0 || total != 0 {
			t.Fatalf("got %q %q %q %d/%d", gotTitle, gotStatus, gotReason, done, total)
		}
	})

	t.Run("a contractor nobody minted, and one bound to a task that is gone, both degrade to blanks rather than dropping off the floor", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		if err := d.PutOutsourceWorker(OutsourceWorker{
			ID: "ow-000000000002", Codename: "Contractor", TaskID: "T-404",
			Status: "assigned", Runtime: "claude", Model: "sonnet", Effort: "medium",
		}); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}

		for _, id := range []string{"ow-nosuchworker", "ow-000000000002"} {
			title, status, reason, done, total := api.contractorTaskFields(id, nil)
			if title != "" || status != "" || reason != "" || done != 0 || total != 0 {
				t.Fatalf("%s: got %q %q %q %d/%d", id, title, status, reason, done, total)
			}
		}
	})
}

func TestStripLeadingTitle(t *testing.T) {
	t.Run("the one opening title line goes and the rest of the doc survives", func(t *testing.T) {
		for name, c := range map[string]struct{ in, want string }{
			"a title over a body":            {"# 助理\n\nbody here\n", "body here"},
			"a title over an outline":        {"# 助理\n## 負責\n第一件事", "## 負責\n第一件事"},
			"blank lines before the title":   {"\n\n# 助理\nbody", "body"},
			"a title with no space after it": {"#助理\nbody", "#助理\nbody"},
			"a six-hash title":               {"###### 助理\nbody", "body"},
			"a seven-hash line":              {"####### 助理\nbody", "####### 助理\nbody"},
		} {
			if got := stripLeadingTitle(c.in); got != c.want {
				t.Fatalf("%s: stripLeadingTitle(%q) = %q, want %q", name, c.in, got, c.want)
			}
		}
	})

	t.Run("a doc that is only a title comes back whole, so an empty duty keeps meaning the member has no role", func(t *testing.T) {
		for _, in := range []string{"# 助理", "# 助理\n", "# 助理\n\n\n"} {
			if got := stripLeadingTitle(in); got != "# 助理" {
				t.Fatalf("stripLeadingTitle(%q) = %q, want it kept whole", in, got)
			}
		}
	})

	t.Run("body text that merely starts with a hash is not a title and is kept", func(t *testing.T) {
		for _, in := range []string{"#1 順位：先看 X\nbody", "#hashtag\nbody", "第一行\n# 助理\nbody"} {
			if got := stripLeadingTitle(in); got != strings.TrimSpace(in) {
				t.Fatalf("stripLeadingTitle(%q) = %q, want it kept", in, got)
			}
		}
	})

	t.Run("a four-space indent makes the line a code block rather than a title, and the surviving text is de-indented", func(t *testing.T) {
		if got := stripLeadingTitle("    # 助理\nbody"); got != "# 助理\nbody" {
			t.Fatalf("stripLeadingTitle of an indented title = %q", got)
		}
		if got := stripLeadingTitle("   # 助理\nbody"); got != "body" {
			t.Fatalf("stripLeadingTitle of a three-space indent = %q", got)
		}
	})
}

func TestIsATXHeading(t *testing.T) {
	t.Run("one to six hashes at up to three spaces of indent, closed by a space or the end of the line", func(t *testing.T) {
		for _, line := range []string{
			"# 助理", "###### 助理", "   # 助理", "#", "###", "#\ttabbed",
			"# 助理   ", "#   ",
		} {
			if !isATXHeading(line) {
				t.Fatalf("isATXHeading(%q) = false, want true", line)
			}
		}
	})

	t.Run("everything a bare hash prefix would have eaten is not a heading", func(t *testing.T) {
		for _, line := range []string{
			"#1 順位：先看 X", "#hashtag", "####### 七個", "    # 助理", "", "body",
			" 　# 助理", "a # 助理",
		} {
			if isATXHeading(line) {
				t.Fatalf("isATXHeading(%q) = true, want false", line)
			}
		}
	})
}

func TestTruncateRunes(t *testing.T) {
	t.Run("a string over the cap is cut at RUNES and the cut is marked", func(t *testing.T) {
		if got := truncateRunes("你好世界", 2); got != "你好…" {
			t.Fatalf("truncateRunes(你好世界, 2) = %q", got)
		}
		if got := truncateRunes("abcd", 3); got != "abc…" {
			t.Fatalf("truncateRunes(abcd, 3) = %q", got)
		}
	})

	t.Run("a string at or under the cap comes back byte for byte with no marker", func(t *testing.T) {
		for _, c := range []struct {
			in  string
			max int
		}{{"你好", 2}, {"你好", 3}, {"", 0}, {"abc", 3}} {
			if got := truncateRunes(c.in, c.max); got != c.in {
				t.Fatalf("truncateRunes(%q, %d) = %q, want it unchanged", c.in, c.max, got)
			}
		}
	})

	t.Run("a cap of zero leaves only the marker", func(t *testing.T) {
		if got := truncateRunes("你好", 0); got != "…" {
			t.Fatalf("truncateRunes(你好, 0) = %q", got)
		}
	})
}

func TestRosterChars(t *testing.T) {
	t.Run("every text field a roster row carries is counted in runes, ids and progress digits included", func(t *testing.T) {
		rows := []resumeRosterMemberDTO{{
			ID: "mira", Name: "Mira", Kind: "staff", RoleName: "Assistant",
			Duty: "助理", CurrentTask: "", Machine: "m-server-self",
			Presence: "offline", TaskStatus: "", WaitingReason: "",
			ProgressDone: 0, ProgressTotal: 0,
		}, {
			ID: "ow-1", Name: "Contractor", Kind: "outsource", CurrentTask: "出貨",
			TaskStatus: "in_progress", WaitingReason: "等回覆", Presence: "online",
			ProgressDone: 3, ProgressTotal: 12,
		}}
		if got := rosterChars(rows); got != 94 {
			t.Fatalf("rosterChars = %d, want 94", got)
		}
	})

	t.Run("an empty roster carries nothing", func(t *testing.T) {
		if got := rosterChars(nil); got != 0 {
			t.Fatalf("rosterChars(nil) = %d", got)
		}
	})
}

func TestAnsweredCardStepChars(t *testing.T) {
	t.Run("the step id, the step name and the card id are all counted in runes", func(t *testing.T) {
		rows := []resumeAnsweredCardStepDTO{
			{StepID: "s-1", StepName: "驗證", CardID: "rc-1"},
			{StepID: "s-2", StepName: "出貨", CardID: "rc-2"},
		}
		if got := answeredCardStepChars(rows); got != 18 {
			t.Fatalf("answeredCardStepChars = %d, want 18", got)
		}
	})

	t.Run("a task row with no answered-card pointer carries nothing", func(t *testing.T) {
		if got := answeredCardStepChars(nil); got != 0 {
			t.Fatalf("answeredCardStepChars(nil) = %d", got)
		}
	})
}

func TestMachinesChars(t *testing.T) {
	t.Run("every machine id and display name plus the caller's own binding are counted in runes", func(t *testing.T) {
		block := resumeMachinesDTO{
			List: []resumeMachineDTO{
				{MachineID: "m-server-self", DisplayName: "伺服器這一台", Online: false},
				{MachineID: "m-abc123", DisplayName: "Laptop", Online: true},
			},
			YouAreOn: "m-abc123",
		}
		if got := machinesChars(block); got != 41 {
			t.Fatalf("machinesChars = %d, want 41", got)
		}
	})

	t.Run("an empty machine block carries nothing", func(t *testing.T) {
		if got := machinesChars(resumeMachinesDTO{}); got != 0 {
			t.Fatalf("machinesChars(empty) = %d", got)
		}
	})
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

	t.Run("a live outsource line counts while a released worker's leftovers do not", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		live := Member{
			ID: "worker-live", Name: "Live worker", Kind: KindOutsource,
			Codename: "O-live", RosterStatus: RosterStatusActive,
		}
		released := Member{
			ID: "worker-released", Name: "Released worker", Kind: KindOutsource,
			Codename: "O-released", RosterStatus: RosterStatusRemoved,
		}
		for _, member := range []Member{live, released} {
			if err := d.PutMember(member); err != nil {
				t.Fatalf("PutMember(%q): %v", member.ID, err)
			}
		}
		dalPutChats(t, d,
			dalChat("live-1", live.ID, "owner", 100),
			dalChat("live-2", live.ID, "owner", 200),
			dalChat("released-1", released.ID, "owner", 300),
		)

		status, data := apiJSON(t, h, "GET", "/api/chat/unread-count", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"unread": 2})
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
