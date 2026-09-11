package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
)

func TestDiffPageQuery(t *testing.T) {
	tests := []struct {
		name                                        string
		before, after, labelBefore, labelAfter, sig string
		want                                        string
	}{
		{
			name:        "addresses labels and signature are URL encoded",
			before:      "att-0123456789ab",
			after:       "doc:global_context/global/current/text",
			labelBefore: "初始 版本",
			labelAfter:  "現在",
			sig:         "sig/+=",
			want:        "after=doc%3Aglobal_context%2Fglobal%2Fcurrent%2Ftext&before=att-0123456789ab&label_after=%E7%8F%BE%E5%9C%A8&label_before=%E5%88%9D%E5%A7%8B+%E7%89%88%E6%9C%AC&sig=sig%2F%2B%3D",
		},
		{
			name:   "empty optional values are left out",
			before: "left",
			after:  "right",
			want:   "after=right&before=left",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := diffPageQuery(tc.before, tc.after, tc.labelBefore, tc.labelAfter, tc.sig)
			if got != tc.want {
				t.Fatalf("diffPageQuery() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOptString(t *testing.T) {
	empty := ""
	padded := " att-0123456789ab "
	label := " 初始版本 "
	for _, tc := range []struct {
		name  string
		input *string
		want  string
	}{
		{name: "nil optional parameter is empty", input: nil, want: ""},
		{name: "present empty parameter is empty", input: &empty, want: ""},
		{name: "address padding is preserved", input: &padded, want: " att-0123456789ab "},
		{name: "label padding is preserved", input: &label, want: " 初始版本 "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := optString(tc.input); got != tc.want {
				t.Fatalf("optString() = %q, want %q", got, tc.want)
			}
		})
	}
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
	for _, tc := range []struct {
		name          string
		before, after string
		wantOK        bool
		wantStatus    int
		wantError     string
	}{
		{
			name:       "two sayable addresses leave a successful empty response",
			before:     "att-0123456789ab",
			after:      "doc:global_context/global/current/text",
			wantOK:     true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "an invalid before side names the before side",
			before:     "att-nope",
			after:      "att-0123456789ab",
			wantStatus: http.StatusUnprocessableEntity,
			wantError:  "the before side: 'att-nope' is neither a stored attachment id (att- plus 12 hex digits) nor a document address (doc:<kind>/<key>/<at>/<field>)",
		},
		{
			name:       "an invalid after side names the after side",
			before:     "att-0123456789ab",
			after:      "doc:global_context/global/current",
			wantStatus: http.StatusUnprocessableEntity,
			wantError:  "the after side: 'doc:global_context/global/current' is not a document address — it is doc:<kind>/<key>/<at>/<field>, where <at> is current, seed or a revision id",
		},
		{
			name:       "an empty before side names the missing comparison side",
			before:     "",
			after:      "att-0123456789ab",
			wantStatus: http.StatusUnprocessableEntity,
			wantError:  "the before side: a comparison side must name a stored attachment id (att-…) or a document (doc:<kind>/<key>/<at>/<field>)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			if got := diffSidesSayable(rec, tc.before, tc.after); got != tc.wantOK {
				t.Fatalf("diffSidesSayable() = %v, want %v", got, tc.wantOK)
			}
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantError == "" {
				if rec.Body.Len() != 0 {
					t.Fatalf("body = %q, want empty", rec.Body.String())
				}
				return
			}
			apiWantError(t, apiTestDecodeJSONBody(t, rec), "validation_error", tc.wantError)
		})
	}
}

func TestResolveDiffSide(t *testing.T) {
	t.Run("a stored attachment returns its bytes and media type", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		id := apiDiffUpload(t, h, owner, "report.txt", "the report")

		got := api.resolveDiffSide(id, "before")
		apiWantValue(t, "side", apiTestJSONOf(t, got), map[string]any{
			"address": id, "gone": false, "label": "before", "mime": "text/plain", "text": "the report",
		})
	})

	t.Run("a missing attachment is marked gone with its reason and heading", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		got := api.resolveDiffSide("att-000000000000", "before")
		apiWantValue(t, "side", apiTestJSONOf(t, got), map[string]any{
			"address": "att-000000000000", "gone": true, "label": "before",
			"gone_reason": "attachment 'att-000000000000' is no longer stored",
		})
	})

	t.Run("a live document returns its text and heading without a media type", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"current rules"}`); status != http.StatusOK {
			t.Fatalf("write global context: %d %v", status, data)
		}

		got := api.resolveDiffSide("doc:global_context/global/current/text", "current")
		apiWantValue(t, "side", apiTestJSONOf(t, got), map[string]any{
			"address": "doc:global_context/global/current/text", "gone": false,
			"label": "current", "text": "current rules",
		})
	})
}

func TestDiffGone(t *testing.T) {
	label := "before"
	mime := "text/plain"
	text := "old bytes"
	got := diffGone(DiffSideDTO{
		Address: "att-0123456789ab", Label: &label, Mime: &mime, Text: &text,
	}, "attachment was removed")

	apiWantValue(t, "side", apiTestJSONOf(t, got), map[string]any{
		"address": "att-0123456789ab", "gone": true, "label": "before",
		"gone_reason": "attachment was removed",
	})
}

func TestDiffDocContent(t *testing.T) {
	t.Run("the global context seed is an empty tombstone document", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		got, ok, err := api.diffDocContent(diffDocAddress{
			Kind: "global_context", Key: "global", At: diffAtSeed, Field: "text",
		})
		if err != nil {
			t.Fatalf("diffDocContent: %v", err)
		}
		want := map[string]string{"text": "", "tombstoned": "true"}
		if !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("content = %#v, present = %v, want %#v, true", got, ok, want)
		}
	})

	t.Run("the current address returns the live field map", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"current rules"}`); status != http.StatusOK {
			t.Fatalf("write global context: %d %v", status, data)
		}

		got, ok, err := api.diffDocContent(diffDocAddress{
			Kind: "global_context", Key: "global", At: diffAtCurrent, Field: "text",
		})
		if err != nil {
			t.Fatalf("diffDocContent: %v", err)
		}
		want := map[string]string{"text": "current rules"}
		if !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("content = %#v, present = %v, want %#v, true", got, ok, want)
		}
	})

	t.Run("a retained revision returns the complete field map it stored", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		for _, body := range []string{`{"text":"v1"}`, `{"text":"v2"}`} {
			if status, data := apiJSON(t, h, "POST", "/api/global-context", owner, body); status != http.StatusOK {
				t.Fatalf("write global context: %d %v", status, data)
			}
		}

		got, ok, err := api.diffDocContent(diffDocAddress{
			Kind: "global_context", Key: "global", At: "1", Field: "text",
		})
		if err != nil {
			t.Fatalf("diffDocContent: %v", err)
		}
		want := map[string]string{"text": "v1", "tombstoned": "false"}
		if !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("content = %#v, present = %v, want %#v, true", got, ok, want)
		}
	})

	t.Run("a revision that is not retained is absent without an error", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		got, ok, err := api.diffDocContent(diffDocAddress{
			Kind: "global_context", Key: "global", At: "99", Field: "text",
		})
		if err != nil {
			t.Fatalf("diffDocContent: %v", err)
		}
		if got != nil || ok {
			t.Fatalf("content = %#v, present = %v, want nil, false", got, ok)
		}
	})

	t.Run("an unknown document kind is absent without an error", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		got, ok, err := api.diffDocContent(diffDocAddress{
			Kind: "not_a_document", Key: "global", At: diffAtCurrent, Field: "text",
		})
		if err != nil {
			t.Fatalf("diffDocContent: %v", err)
		}
		if got != nil || ok {
			t.Fatalf("content = %#v, present = %v, want nil, false", got, ok)
		}
	})
}

func TestCurrentDocumentContent(t *testing.T) {
	api, _, d, _ := newAPITestServer(t)
	if err := d.PutUserContext(UserContext{Text: "context text"}); err != nil {
		t.Fatalf("PutUserContext: %v", err)
	}
	if err := d.PutRoleDef(RoleDef{
		RoleKey: "r-current", Name: "Current", DefinitionMD: "# current",
	}); err != nil {
		t.Fatalf("PutRoleDef: %v", err)
	}
	if err := d.PutInsight(Insight{RoleKey: "r-current", Text: "insight text"}); err != nil {
		t.Fatalf("PutInsight: %v", err)
	}
	if err := d.PutBootDocument(BootDocument{Kind: "offboard", Key: "global", Text: "offboard text"}); err != nil {
		t.Fatalf("PutBootDocument: %v", err)
	}
	if err := d.PutTaskManual(TaskManual{
		TypeKey: "tm-current", SopMD: "SOP text",
	}); err != nil {
		t.Fatalf("PutTaskManual: %v", err)
	}
	if err := d.PutTask(Task{
		ID: "T-current", Title: "task title", Description: "task description",
		Inputs: map[string]any{}, Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: KindStaff, ExecutorID: "kip", CreatorID: "owner",
	}); err != nil {
		t.Fatalf("PutTask: %v", err)
	}

	for _, tc := range []struct {
		name       string
		kind, key  string
		want       map[string]string
		wantExists bool
	}{
		{name: "global context returns its live text", kind: "global_context", key: "global", want: map[string]string{"text": "context text"}, wantExists: true},
		{name: "a role definition returns its live definition field", kind: "role_definition", key: "r-current", want: map[string]string{"definition_md": "# current"}, wantExists: true},
		{name: "insight returns its live text field", kind: "insight", key: "r-current", want: map[string]string{"text": "insight text"}, wantExists: true},
		{name: "a boot document returns its whole live text", kind: "offboard", key: "global", want: map[string]string{"text": "offboard text"}, wantExists: true},
		{name: "a task manual returns its live SOP", kind: docKindTaskManualSop, key: "tm-current", want: map[string]string{"sop_md": "SOP text"}, wantExists: true},
		{name: "a task returns its live title", kind: docKindTaskTitle, key: "T-current", want: map[string]string{"title": "task title"}, wantExists: true},
		{name: "a task returns its live description", kind: docKindTaskDescription, key: "T-current", want: map[string]string{"description": "task description"}, wantExists: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, err := api.currentDocumentContent(tc.kind, tc.key)
			if err != nil {
				t.Fatalf("currentDocumentContent: %v", err)
			}
			if ok != tc.wantExists || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("content = %#v, present = %v, want %#v, %v", got, ok, tc.want, tc.wantExists)
			}
		})
	}

	for _, tc := range []struct {
		name      string
		kind, key string
	}{
		{name: "a wrong global context key is absent", kind: "global_context", key: "other"},
		{name: "an unknown role definition is absent", kind: "role_definition", key: "r-missing"},
		{name: "a missing task manual is absent", kind: docKindTaskManualSop, key: "tm-missing"},
		{name: "a missing task is absent", kind: docKindTaskDescription, key: "T-missing"},
		{name: "an unknown kind is absent", kind: "not_a_document", key: "global"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, err := api.currentDocumentContent(tc.kind, tc.key)
			if err != nil {
				t.Fatalf("currentDocumentContent: %v", err)
			}
			if got != nil || ok {
				t.Fatalf("content = %#v, present = %v, want nil, false", got, ok)
			}
		})
	}
}
