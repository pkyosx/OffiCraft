package main

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

// T-59 (owner 2026-09-03, c-95717eb161b3): a diff attachment is a POINTER PAIR
// naming the two blobs to compare, not a copy of their contents.
//
// The refusal lives in resolveChatAttachment — the shared tail of the streaming
// upload and the inline-base64 decode — so every face that takes an attachment
// inherits it without wiring anything up. That is the property worth a test:
// "it works on the face I happened to try" is how this repo previously ended up
// with one mechanism answering 400 on the question side and 200 on the answer
// side of the same card. So the malformed case is driven through the REAL
// entry points, and through more than one of them.

const wellFormedDiff = `{"before":{"attachment_id":"att-0123456789ab","label":"9/2 21:12"},` +
	`"after":{"attachment_id":"att-fedcba987654","label":"目前存檔內容"}}`

func uploadAttachment(t *testing.T, srvURL, tok, mime string, body []byte) (int, string) {
	t.Helper()
	return doRaw(t, "POST",
		srvURL+"/api/chat/attachments?mime="+mime+"&filename=change.diff",
		tok, "application/octet-stream", body)
}

func TestDiffAttachmentRefusedOnEveryFaceWhenItCouldNotBeDrawn(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	agentTok, _ := mintJWT("mira", "agent", 300, secret, now, "")

	for _, tc := range []struct{ name, payload, want string }{
		{
			"not JSON at all",
			`--- a/x\n+++ b/x`,
			"a diff attachment must be a JSON object naming its before and after sides",
		},
		{
			"after side never named",
			`{"before":{"attachment_id":"att-0123456789ab"}}`,
			"a diff attachment's after side must name either an attachment_id or a doc",
		},
		{
			"before side blank",
			`{"before":{"attachment_id":"  "},"after":{"attachment_id":"att-fedcba987654"}}`,
			"a diff attachment's before side must name either an attachment_id or a doc",
		},
		{
			// Both shapes on one side does not say which one to draw, and
			// preferring one silently would make the other a value nobody sees.
			"side names a blob AND a document",
			`{"before":{"attachment_id":"att-0123456789ab","doc":{"kind":"role_lessons",` +
				`"key":"mira","at":"current","field":"lessons"}},` +
				`"after":{"attachment_id":"att-fedcba987654"}}`,
			"a diff attachment's before side names both an attachment_id and a doc — it must name exactly one",
		},
		{
			// A path, a URL or an avatar id are all things a caller might reach
			// for; none of them resolves to a blob this server can serve.
			"side names something that is not a stored blob",
			`{"before":{"attachment_id":"/Users/eva/before.txt"},"after":{"attachment_id":"att-fedcba987654"}}`,
			"a diff attachment's before attachment_id must be a stored blob id (att-…), got /Users/eva/before.txt",
		},
		{
			// The prefix alone used to pass this, and it is the likeliest typo
			// of all: copy an id, lose the tail. It would be accepted here and
			// then 404 at read time — exactly the "accepted but will not draw"
			// split this guard exists to prevent.
			"side carries the prefix and nothing else",
			`{"before":{"attachment_id":"att-"},"after":{"attachment_id":"att-fedcba987654"}}`,
			"a diff attachment's before attachment_id must be a stored blob id (att-…), got att-",
		},
		{
			// The prefix version accepted this too, and the FE builds the side
			// URL by concatenation: the browser normalises it to a DIFFERENT
			// endpoint and the compare screen draws that response as "before".
			"side smuggles a path out of the blob route",
			`{"before":{"attachment_id":"att-/../../api/version"},"after":{"attachment_id":"att-fedcba987654"}}`,
			"a diff attachment's before attachment_id must be a stored blob id (att-…), got att-/../../api/version",
		},
		{
			// A revision stores a MAP of fields, not one text, so the reader
			// cannot pick one for you. The server deliberately holds no
			// kind→field table to default it from.
			"document side does not say which field",
			`{"before":{"doc":{"kind":"role_lessons","key":"mira","at":"12"}},` +
				`"after":{"attachment_id":"att-fedcba987654"}}`,
			"a diff attachment's before doc must name its field",
		},
		{
			"document side does not say which document",
			`{"before":{"doc":{"kind":"role_lessons","at":"12","field":"lessons"}},` +
				`"after":{"attachment_id":"att-fedcba987654"}}`,
			"a diff attachment's before doc must name its key",
		},
		{
			// The blob side's traversal hole again, except a document address
			// has THREE places to try it instead of one.
			"document key traverses out of the document route",
			`{"before":{"doc":{"kind":"role_lessons","key":"../../api/version","at":"current",` +
				`"field":"lessons"}},"after":{"attachment_id":"att-fedcba987654"}}`,
			"a diff attachment's before doc key is not a usable address segment, got ../../api/version",
		},
		{
			// ".." contains no excluded character, so it is refused by name.
			"document kind is the parent directory",
			`{"before":{"doc":{"kind":"..","key":"mira","at":"current","field":"lessons"}},` +
				`"after":{"attachment_id":"att-fedcba987654"}}`,
			"a diff attachment's before doc kind is not a usable address segment, got ..",
		},
		{
			// "latest" reads like it would work and does not: the live content
			// is spelled "current", and anything else must be a revision id.
			"document side asks for a revision that has no address",
			`{"before":{"doc":{"kind":"role_lessons","key":"mira","at":"latest","field":"lessons"}},` +
				`"after":{"attachment_id":"att-fedcba987654"}}`,
			`a diff attachment's before doc at must be "current", "seed" or a retained revision id, got latest`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Face 1 — the streaming upload, which is what `ocagent upload` uses.
			status, resp := uploadAttachment(t, srv.URL, agentTok,
				chatAttachmentDiffMime, []byte(tc.payload))
			if status != 400 || rawErrorMessage(t, resp) != tc.want {
				t.Errorf("upload: want 400 %q, got %d %s", tc.want, status, resp)
			}

			// Face 2 — inline base64 on a chat post. A separate call site, and
			// the one that would silently keep working if the refusal had been
			// wired into the upload handler instead of the shared tail.
			inline := `{"to":"owner","body":"see the diff","attachments":[{"filename":"change.diff",` +
				`"mime":"` + chatAttachmentDiffMime + `","data_b64":"` +
				base64.StdEncoding.EncodeToString([]byte(tc.payload)) + `"}]}`
			status, resp = doRaw(t, "POST", srv.URL+"/api/chat", agentTok,
				"application/json", []byte(inline))
			if status != 400 || rawErrorMessage(t, resp) != tc.want {
				t.Errorf("chat inline: want 400 %q, got %d %s", tc.want, status, resp)
			}

			// Face 3 — the reply card's question side.
			card := `{"kind":"decision","summary":"look at this?","options":[{"text":"ok"}],` +
				`"linked_task":null,"attachments":[{"filename":"change.diff","mime":"` +
				chatAttachmentDiffMime + `","data_b64":"` +
				base64.StdEncoding.EncodeToString([]byte(tc.payload)) + `"}]}`
			status, resp = doRaw(t, "POST", srv.URL+"/api/reply-cards", agentTok,
				"application/json", []byte(card))
			if status != 400 || rawErrorMessage(t, resp) != tc.want {
				t.Errorf("reply card: want 400 %q, got %d %s", tc.want, status, resp)
			}
		})
	}

	// Nothing was stored: a refusal must not be a post that quietly lost its
	// attachment.
	status, resp := doRaw(t, "GET", srv.URL+"/api/chat?with=owner", agentTok, "", nil)
	if status != 200 {
		t.Fatalf("read back the stream: %d %s", status, resp)
	}
	var stream []map[string]any
	if err := json.Unmarshal([]byte(resp), &stream); err != nil {
		t.Fatalf("chat stream is not a list: %v (%s)", err, resp)
	}
	if len(stream) != 0 {
		t.Fatalf("a refused post must leave no message, got %v", stream)
	}
}

func TestDiffAttachmentAcceptedAndTypedByItsMime(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	agentTok, _ := mintJWT("mira", "agent", 300, secret, now, "")

	status, resp := uploadAttachment(t, srv.URL, agentTok,
		chatAttachmentDiffMime, []byte(wellFormedDiff))
	if status != 200 {
		t.Fatalf("a well-formed pointer pair must be accepted: %d %s", status, resp)
	}
	var ref struct {
		ID   string `json:"id"`
		Mime string `json:"mime"`
	}
	if err := json.Unmarshal([]byte(resp), &ref); err != nil {
		t.Fatalf("upload response is not a light ref: %v (%s)", err, resp)
	}
	// The MIME is what carries the type onward — to the attachment strip, to a
	// task artifact built from this blob, and to the compare screen. A stored
	// blob that came back typed as something else would be undiscoverable.
	if ref.Mime != chatAttachmentDiffMime {
		t.Errorf("stored mime = %q, want %q", ref.Mime, chatAttachmentDiffMime)
	}

	// Read the bytes back verbatim: the server stores a pointer pair, it does
	// not rewrite or normalise it.
	status, body := doRaw(t, "GET", srv.URL+"/api/chat/attachment/"+ref.ID, agentTok, "", nil)
	if status != 200 || body != wellFormedDiff {
		t.Fatalf("served bytes = %d %q, want the pair as posted", status, body)
	}

	// A side may instead name a DOCUMENT (T-59 second round). The blob-pair
	// spelling above keeps working unchanged — every diff attachment PR #392
	// already minted is of that shape — and the three ways to address a
	// document are accepted alongside it.
	for _, tc := range []struct{ name, payload string }{
		{
			"a retained revision against the live content",
			`{"before":{"doc":{"kind":"role_lessons","key":"mira","at":"12","field":"lessons"},` +
				`"label":"8/28"},"after":{"doc":{"kind":"role_lessons","key":"mira",` +
				`"at":"current","field":"lessons"},"label":"目前存檔內容"}}`,
		},
		{
			"the shipped default against the live content",
			`{"before":{"doc":{"kind":"global_context","key":"global","at":"seed","field":"content"}},` +
				`"after":{"doc":{"kind":"global_context","key":"global","at":"current","field":"content"}}}`,
		},
		{
			// One side each: the two shapes are exclusive per SIDE, not per
			// attachment, so an uploaded file compares against a document.
			"an uploaded file against a document",
			`{"before":{"attachment_id":"att-0123456789ab"},` +
				`"after":{"doc":{"kind":"task_manual_sop","key":"tm-05f7c776d6ff","at":"current",` +
				`"field":"sop_md"}}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, resp := uploadAttachment(t, srv.URL, agentTok,
				chatAttachmentDiffMime, []byte(tc.payload))
			if status != 200 {
				t.Fatalf("want accepted, got %d %s", status, resp)
			}
			var ref struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal([]byte(resp), &ref); err != nil {
				t.Fatalf("upload response is not a light ref: %v (%s)", err, resp)
			}
			status, body := doRaw(t, "GET", srv.URL+"/api/chat/attachment/"+ref.ID, agentTok, "", nil)
			if status != 200 || body != tc.payload {
				t.Fatalf("served bytes = %d %q, want the pair as posted", status, body)
			}
		})
	}
}

// The refusal is keyed on the DECLARED type, not on what the bytes look like.
// Without this, "diff validation" could quietly become "reject any JSON that
// does not look like a diff", which would refuse ordinary .json attachments.
func TestNonDiffAttachmentIsNotHeldToTheDiffShape(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	agentTok, _ := mintJWT("mira", "agent", 300, secret, now, "")

	// The exact bytes rejected above, under an ordinary type.
	status, resp := uploadAttachment(t, srv.URL, agentTok,
		"application/json", []byte(`{"before":{"attachment_id":"/not/a/blob"}}`))
	if status != 200 {
		t.Fatalf("a plain JSON attachment must not be judged as a diff: %d %s", status, resp)
	}
}
