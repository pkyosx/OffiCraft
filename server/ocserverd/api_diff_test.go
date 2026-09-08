// Skeleton generated from server/ocserverd/api_diff.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"net/http"
	"net/url"
	"testing"
)

func TestDiffPageQuery(t *testing.T) {
	t.Skip("TODO: diffPageQuery builds the page URL's query.")
}

func TestOptString(t *testing.T) {
	t.Skip("TODO: optString reads an optional query parameter WITHOUT trimming it.")
}

// apiDiffSeededSides files two stored blobs through the real upload route and
// answers the two addresses a comparison names them by.
func apiDiffSeededSides(t *testing.T, h http.Handler, credential string) (string, string) {
	t.Helper()
	return apiDiffUpload(t, h, credential, "yesterday.txt", "the old wording"),
		apiDiffUpload(t, h, credential, "today.txt", "the new wording")
}

func apiDiffUpload(t *testing.T, h http.Handler, credential, filename, body string) string {
	t.Helper()
	status, data := apiJSON(t, h, "POST",
		"/api/chat/attachments?filename="+filename+"&mime=text/plain", credential, body)
	if status != 200 {
		t.Fatalf("upload %s: %d %v", filename, status, data)
	}
	id, _ := data["id"].(string)
	if id == "" {
		t.Fatalf("upload %s minted no id: %v", filename, data)
	}
	return id
}

// apiDiffShareQuery mints an external link through the real route and answers
// the query half of it — what a credential-less reader would present.
func apiDiffShareQuery(t *testing.T, h http.Handler, credential, before, after, labelBefore, labelAfter string) string {
	t.Helper()
	target := "/api/diff/share-link?before=" + url.QueryEscape(before) + "&after=" + url.QueryEscape(after)
	if labelBefore != "" {
		target += "&label_before=" + url.QueryEscape(labelBefore)
	}
	if labelAfter != "" {
		target += "&label_after=" + url.QueryEscape(labelAfter)
	}
	status, data := apiJSON(t, h, "GET", target, credential, "")
	if status != 200 {
		t.Fatalf("mint a share link: %d %v", status, data)
	}
	link, _ := data["url"].(string)
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatalf("the minted url does not parse: %v", err)
	}
	return parsed.RawQuery
}

// apiWantDiffLink asserts the whole minted link: the page it points at, every
// query parameter it carries, and that the signature is the fixed-width one the
// verifier compares — its value is a function of a server-minted key and cannot
// be written down.
func apiWantDiffLink(t *testing.T, link string, wantQuery map[string]string) {
	t.Helper()
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatalf("the minted url does not parse: %v", err)
	}
	if parsed.Scheme != "" || parsed.Host != "" {
		t.Fatalf("the minted url must be server-relative, got %q", link)
	}
	if parsed.Path != "/diff" {
		t.Fatalf("want the /diff page, got %q", parsed.Path)
	}
	got := map[string]any{}
	for name, values := range parsed.Query() {
		if len(values) != 1 {
			t.Fatalf("%s is repeated in the minted url: %v", name, values)
		}
		got[name] = values[0]
	}
	want := map[string]any{}
	for name, value := range wantQuery {
		want[name] = value
	}
	want["sig"] = apiAnyString
	apiWantValue(t, "link-query", any(got), any(want))
	if sig, _ := got["sig"].(string); len(sig) != 32 {
		t.Fatalf("want a 32-character signature, got %q", got["sig"])
	}
}

func TestHandleGetDiffShareLinkApiDiffShareLinkGet(t *testing.T) {
	t.Run("minting answers the server-relative /diff page carrying both addresses and a signature that opens the pair with no credential at all", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		before, after := apiDiffSeededSides(t, h, owner)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET",
			"/api/diff/share-link?before="+before+"&after="+after, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"url": apiAnyString})
		dashboard.wantFrames()

		link, _ := data["url"].(string)
		apiWantDiffLink(t, link, map[string]string{"before": before, "after": after})

		query, _ := url.Parse(link)
		status, pair := apiJSON(t, h, "GET", "/api/diff?"+query.RawQuery, "", "")
		if status != 200 {
			t.Fatalf("the minted link must open the pair with no credential: %d %v", status, pair)
		}
		apiWantBody(t, pair, map[string]any{
			"before": map[string]any{"address": before, "gone": false, "mime": "text/plain", "text": "the old wording"},
			"after":  map[string]any{"address": after, "gone": false, "mime": "text/plain", "text": "the new wording"},
		})
	})

	t.Run("both column headings ride in the link, and relabelling a column voids the signature", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		before, after := apiDiffSeededSides(t, h, owner)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET",
			"/api/diff/share-link?before="+before+"&after="+after+
				"&label_before="+url.QueryEscape("初始版本")+"&label_after=now", agent, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"url": apiAnyString})
		dashboard.wantFrames()

		link, _ := data["url"].(string)
		apiWantDiffLink(t, link, map[string]string{
			"before": before, "after": after, "label_before": "初始版本", "label_after": "now",
		})

		parsed, _ := url.Parse(link)
		status, pair := apiJSON(t, h, "GET", "/api/diff?"+parsed.RawQuery, "", "")
		if status != 200 {
			t.Fatalf("the minted link must open the pair: %d %v", status, pair)
		}
		apiWantBody(t, pair, map[string]any{
			"before": map[string]any{"address": before, "gone": false, "label": "初始版本", "mime": "text/plain", "text": "the old wording"},
			"after":  map[string]any{"address": after, "gone": false, "label": "now", "mime": "text/plain", "text": "the new wording"},
		})

		tampered := parsed.Query()
		tampered.Set("label_after", "初始版本")
		status, refused := apiJSON(t, h, "GET", "/api/diff?"+tampered.Encode(), "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, refused)
		}
		apiWantError(t, refused, "unauthorized", "invalid signature")
	})

	t.Run("minting the same comparison twice answers the same link, and a different comparison a different one", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		before, after := apiDiffSeededSides(t, h, owner)

		first := apiDiffShareQuery(t, h, owner, before, after, "", "")
		if again := apiDiffShareQuery(t, h, owner, before, after, "", ""); again != first {
			t.Fatalf("the same comparison must mint the same link:\n%s\n%s", first, again)
		}
		if swapped := apiDiffShareQuery(t, h, owner, after, before, "", ""); swapped == first {
			t.Fatalf("swapping the two sides must not mint the same link: %s", swapped)
		}
		if labelled := apiDiffShareQuery(t, h, owner, before, after, "was", ""); labelled == first {
			t.Fatalf("adding a column heading must not mint the same link: %s", labelled)
		}
	})

	t.Run("a side that is not a sayable address is refused 422 naming which side, and mints nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		before, _ := apiDiffSeededSides(t, h, owner)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/diff/share-link?before="+before+"&after=att-nope", owner, "")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"the after side: 'att-nope' is neither a stored attachment id (att- plus 12 hex digits) "+
				"nor a document address (doc:<kind>/<key>/<at>/<field>)")
		dashboard.wantFrames()
	})

	t.Run("a request carrying no credentials answers 401", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		before, after := apiDiffSeededSides(t, h, owner)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/diff/share-link?before="+before+"&after="+after, "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
	})

	t.Run("a GET /api/diff/share-link request the wire layer rejects (a request with no before parameter) answers 422 without reaching the domain", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		_, after := apiDiffSeededSides(t, h, owner)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/diff/share-link?after="+after, owner, "")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "Query argument before is required, but not found")
		dashboard.wantFrames()
	})
}

func TestHandleGetDiffApiDiffGet(t *testing.T) {
	t.Run("both stored sides come back in one answer, each with its text and its media type", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		before, after := apiDiffSeededSides(t, h, owner)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/diff?before="+before+"&after="+after, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"before": map[string]any{"address": before, "gone": false, "mime": "text/plain", "text": "the old wording"},
			"after":  map[string]any{"address": after, "gone": false, "mime": "text/plain", "text": "the new wording"},
		})
		dashboard.wantFrames()
	})

	t.Run("a document side answers the live text beside the shipped default, and a heading given for one column appears only on that column", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"office rules v2"}`); status != 200 {
			t.Fatalf("write the global context: %d %v", status, data)
		}
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET",
			"/api/diff?before="+url.QueryEscape("doc:global_context/global/seed/text")+
				"&after="+url.QueryEscape("doc:global_context/global/current/text")+
				"&label_before="+url.QueryEscape("初始版本"), agent, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"before": map[string]any{
				"address": "doc:global_context/global/seed/text", "gone": false,
				"label": "初始版本", "text": "",
			},
			"after": map[string]any{
				"address": "doc:global_context/global/current/text", "gone": false,
				"text": "office rules v2",
			},
		})
		dashboard.wantFrames()
	})

	t.Run("an address that resolves to nothing marks only its own side gone and says why, while the other still draws", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		_, after := apiDiffSeededSides(t, h, owner)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/diff?before=att-000000000000&after="+after, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"before": map[string]any{
				"address": "att-000000000000", "gone": true,
				"gone_reason": "attachment 'att-000000000000' is no longer stored",
			},
			"after": map[string]any{"address": after, "gone": false, "mime": "text/plain", "text": "the new wording"},
		})
		dashboard.wantFrames()

		status, data = apiJSON(t, h, "GET",
			"/api/diff?before="+url.QueryEscape("doc:no_such_kind/x/current/text")+"&after="+after, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"before": map[string]any{
				"address": "doc:no_such_kind/x/current/text", "gone": true,
				"gone_reason": "'doc:no_such_kind/x/current/text' names no document this station holds — " +
					"a revision that has been pruned, a document with no shipped default, or a kind/key that does not exist",
			},
			"after": map[string]any{"address": after, "gone": false, "mime": "text/plain", "text": "the new wording"},
		})

		status, data = apiJSON(t, h, "GET",
			"/api/diff?before="+url.QueryEscape("doc:global_context/global/current/heading")+"&after="+after, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"before": map[string]any{
				"address": "doc:global_context/global/current/heading", "gone": true,
				"gone_reason": "'global_context/global' has no field 'heading' at current",
			},
			"after": map[string]any{"address": after, "gone": false, "mime": "text/plain", "text": "the new wording"},
		})
	})

	t.Run("a revision id larger than the ids this station can hold is that side's gone marker rather than an error", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		_, after := apiDiffSeededSides(t, h, owner)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET",
			"/api/diff?before="+url.QueryEscape("doc:global_context/global/9999999999999999999/text")+
				"&after="+after, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"before": map[string]any{
				"address": "doc:global_context/global/9999999999999999999/text", "gone": true,
				"gone_reason": "'doc:global_context/global/9999999999999999999/text' names no document this station holds — " +
					"a revision that has been pruned, a document with no shipped default, or a kind/key that does not exist",
			},
			"after": map[string]any{"address": after, "gone": false, "mime": "text/plain", "text": "the new wording"},
		})
		dashboard.wantFrames()
	})

	t.Run("an unsayable side is refused 422 naming which of the two it is, before either side is read", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		before, after := apiDiffSeededSides(t, h, owner)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/diff?before=att-nope&after="+after, owner, "")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"the before side: 'att-nope' is neither a stored attachment id (att- plus 12 hex digits) "+
				"nor a document address (doc:<kind>/<key>/<at>/<field>)")

		status, data = apiJSON(t, h, "GET",
			"/api/diff?before="+before+"&after="+url.QueryEscape("doc:global_context/global/current"), owner, "")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"the after side: 'doc:global_context/global/current' is not a document address — "+
				"it is doc:<kind>/<key>/<at>/<field>, where <at> is current, seed or a revision id")

		status, data = apiJSON(t, h, "GET", "/api/diff?before=&after="+after, owner, "")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"the before side: a comparison side must name a stored attachment id (att-…) "+
				"or a document (doc:<kind>/<key>/<at>/<field>)")
		dashboard.wantFrames()
	})

	t.Run("an address padded with a space is refused rather than trimmed into a different one", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		before, after := apiDiffSeededSides(t, h, owner)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET",
			"/api/diff?before="+url.QueryEscape(" "+before)+"&after="+after, owner, "")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"the before side: ' "+before+"' is neither a stored attachment id (att- plus 12 hex digits) "+
				"nor a document address (doc:<kind>/<key>/<at>/<field>)")
		dashboard.wantFrames()
	})

	t.Run("a credential-less request carrying a signature for a different comparison answers 401", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		before, after := apiDiffSeededSides(t, h, owner)
		other := apiDiffUpload(t, h, owner, "third.txt", "a third wording")
		minted := apiDiffShareQuery(t, h, owner, before, after, "", "")
		dashboard := apiTestListen(t, api, "")

		signature := mustParseQuery(t, minted).Get("sig")
		status, data := apiJSON(t, h, "GET",
			"/api/diff?before="+before+"&after="+other+"&sig="+url.QueryEscape(signature), "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "invalid signature")

		status, data = apiJSON(t, h, "GET", "/api/diff?before="+before+"&after="+after+"&sig=notasignature", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "invalid signature")
		dashboard.wantFrames()
	})

	t.Run("a signed-in reader is answered whether or not the request carries a signature", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		before, after := apiDiffSeededSides(t, h, owner)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET",
			"/api/diff?before="+before+"&after="+after+"&sig=notasignature", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"before": map[string]any{"address": before, "gone": false, "mime": "text/plain", "text": "the old wording"},
			"after":  map[string]any{"address": after, "gone": false, "mime": "text/plain", "text": "the new wording"},
		})
		dashboard.wantFrames()
	})

	t.Run("a request carrying neither credentials nor a signature answers 401", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		before, after := apiDiffSeededSides(t, h, owner)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/diff?before="+before+"&after="+after, "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
	})

	t.Run("a GET /api/diff request the wire layer rejects (a request with no after parameter) answers 422 without reaching the domain", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		before, _ := apiDiffSeededSides(t, h, owner)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/diff?before="+before, owner, "")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "Query argument after is required, but not found")
		dashboard.wantFrames()
	})
}

func mustParseQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("parse query %q: %v", raw, err)
	}
	return values
}

func TestDiffSidesSayable(t *testing.T) {
	t.Skip("TODO: diffSidesSayable judges the SHAPE of both sides before anything is read, and writes the 422 itself.")
}

func TestResolveDiffSide(t *testing.T) {
	t.Skip("TODO: resolveDiffSide turns one address into the column the reader draws.")
}

func TestDiffGone(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestDiffDocContent(t *testing.T) {
	t.Skip("TODO: diffDocContent answers one document address as the SAME field map a retained revision carries — which is what lets one reader compare any two of the three points in time against each other.")
}

func TestCurrentDocumentContent(t *testing.T) {
	t.Skip("TODO: currentDocumentContent reads the LIVE content of one editable document in the field names its retained revisions carry.")
}
