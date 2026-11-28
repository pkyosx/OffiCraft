package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func apiShareLinkUpload(t *testing.T, h http.Handler, credential string) string {
	t.Helper()
	return apiDiffUpload(t, h, credential, "notes.txt", "hello")
}

// apiMintShareLink mints through the real route and answers the decoded body.
func apiMintShareLink(t *testing.T, h http.Handler, target, credential, body string) map[string]any {
	t.Helper()
	status, data := apiJSON(t, h, "POST", target, credential, body)
	if status != 200 {
		t.Fatalf("mint %s: %d %v", target, status, data)
	}
	return data
}

func apiAttachmentLinkTarget(id string) string {
	return "/api/chat/attachments/" + id + "/share-link"
}

func apiShareLinkID(data map[string]any) string {
	link, _ := data["share_link"].(map[string]any)
	id, _ := link["id"].(string)
	return id
}

func apiExpireShareLinkNow(t *testing.T, d *DAL, id string) {
	t.Helper()
	if _, err := d.wdb.Exec(`UPDATE share_link SET expires_ts = ? WHERE id = ?`, nowSecs()-1, id); err != nil {
		t.Fatalf("expire %s: %v", id, err)
	}
}

func apiShareLinkIDs(t *testing.T, h http.Handler, query, credential string) []string {
	t.Helper()
	status, data := apiJSON(t, h, "GET", "/api/share-links"+query, credential, "")
	if status != 200 {
		t.Fatalf("list %s: %d %v", query, status, data)
	}
	ids := []string{}
	rows, _ := data["share_links"].([]any)
	for _, row := range rows {
		ids = append(ids, row.(map[string]any)["id"].(string))
	}
	return ids
}

func apiWantIDs(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s: got %v, want %v", what, got, want)
	}
}

func TestResolveShareLinkExpiry(t *testing.T) {
	now := 1_800_000_000.0
	at := func(v float64) *float64 { return &v }
	for _, tc := range []struct {
		name     string
		sent     bool
		value    *float64
		want     *float64
		wantOkay bool
	}{
		{name: "an omitted expiry is seven days from now", want: at(now + 7*24*3600), wantOkay: true},
		{name: "an explicit null never expires", sent: true, wantOkay: true},
		{name: "a time in the next year is kept as given", sent: true, value: at(now + 3600), want: at(now + 3600), wantOkay: true},
		{name: "exactly 365 days ahead is accepted", sent: true, value: at(now + 365*24*3600), want: at(now + 365*24*3600), wantOkay: true},
		{name: "a second past 365 days is refused", sent: true, value: at(now + 365*24*3600 + 1)},
		{name: "now itself is refused", sent: true, value: at(now)},
		{name: "a past time is refused", sent: true, value: at(now - 60)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := resolveShareLinkExpiry(map[string]bool{"expires_ts": tc.sent}, tc.value, now)
			if ok != tc.wantOkay {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOkay)
			}
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Fatalf("expires = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHandleCreateChatAttachmentShareLinkApiChatAttachmentsAttachmentIdShareLinkPost(t *testing.T) {
	t.Run("minting with no body answers a one-time url and a record that expires in seven days", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		id := apiShareLinkUpload(t, h, owner)
		dashboard := apiTestListen(t, api, "")

		before := nowSecs()
		data := apiMintShareLink(t, h, apiAttachmentLinkTarget(id), owner, "")
		apiWantBody(t, data, map[string]any{
			"url": apiAnyString,
			"share_link": map[string]any{
				"id": apiAnyString, "kind": "attachment",
				"attachment_id": id, "attachment_filename": "notes.txt",
				"before": nil, "after": nil, "label_before": nil, "label_after": nil,
				"created_by": "owner", "created_ts": apiAnyNumber, "expires_ts": apiAnyNumber,
			},
		})
		dashboard.wantFrames()

		link := data["share_link"].(map[string]any)
		if got := link["expires_ts"].(float64) - link["created_ts"].(float64); got != shareLinkDefaultTTLSecs {
			t.Fatalf("expires - created = %v, want %v", got, shareLinkDefaultTTLSecs)
		}
		if created := link["created_ts"].(float64); created < before {
			t.Fatalf("created_ts %v is before the request started (%v)", created, before)
		}
		if !strings.HasPrefix(link["id"].(string), "sl-") || len(link["id"].(string)) != 15 {
			t.Fatalf("id: want sl- + 12 hex, got %q", link["id"])
		}
		minted, err := url.Parse(data["url"].(string))
		if err != nil || minted.Path != "/api/chat/attachment/"+id || len(minted.Query()) != 1 || minted.Query().Get("link") == "" {
			t.Fatalf("url: want the blob path carrying only ?link=, got %q", data["url"])
		}
	})

	t.Run("the url opens that one blob with no credential, and the database keeps only a hash of its credential", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		id := apiShareLinkUpload(t, h, owner)
		data := apiMintShareLink(t, h, apiAttachmentLinkTarget(id), owner, "")
		token := strings.SplitN(data["url"].(string), "?link=", 2)[1]

		rec := apiRequest(t, h, "GET", data["url"].(string), "", "")
		if rec.Code != 200 || rec.Body.String() != "hello" {
			t.Fatalf("want 200 hello, got %d %q", rec.Code, rec.Body.String())
		}
		var stored string
		if err := d.rdb.QueryRow(`SELECT token_hash FROM share_link WHERE id = ?`, apiShareLinkID(data)).Scan(&stored); err != nil {
			t.Fatalf("read row: %v", err)
		}
		if stored != shareLinkTokenHash(token) || strings.Contains(stored, token) {
			t.Fatalf("token_hash = %q, want the SHA-256 of the credential and never the credential", stored)
		}
	})

	t.Run("every call mints a separate link with its own expiry", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiShareLinkUpload(t, h, owner)
		in := strconv.FormatFloat(nowSecs()+3600, 'f', 3, 64)

		first := apiMintShareLink(t, h, apiAttachmentLinkTarget(id), owner, `{"expires_ts":`+in+`}`)
		forever := apiMintShareLink(t, h, apiAttachmentLinkTarget(id), owner, `{"expires_ts":null}`)
		if first["url"] == forever["url"] || apiShareLinkID(first) == apiShareLinkID(forever) {
			t.Fatalf("two mints answered the same link: %v / %v", first, forever)
		}
		want, _ := strconv.ParseFloat(in, 64)
		if got := first["share_link"].(map[string]any)["expires_ts"]; got != want {
			t.Fatalf("first expires_ts = %v, want %v", got, want)
		}
		if got := forever["share_link"].(map[string]any)["expires_ts"]; got != nil {
			t.Fatalf("null expires_ts must mint a link that never expires, got %v", got)
		}
	})

	t.Run("an AI member mints under its own id", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		id := apiShareLinkUpload(t, h, owner)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		data := apiMintShareLink(t, h, apiAttachmentLinkTarget(id), agent, "{}")
		if got := data["share_link"].(map[string]any)["created_by"]; got != apiTestPlainAgentID {
			t.Fatalf("created_by = %v, want %s", got, apiTestPlainAgentID)
		}
	})

	t.Run("an expiry that is not later than now or is more than 365 days ahead is refused 422 and mints nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiShareLinkUpload(t, h, owner)
		for _, ts := range []float64{nowSecs() - 1, nowSecs() + shareLinkMaxAheadSecs + 3600} {
			status, data := apiJSON(t, h, "POST", apiAttachmentLinkTarget(id), owner,
				`{"expires_ts":`+strconv.FormatFloat(ts, 'f', 0, 64)+`}`)
			if status != 422 {
				t.Fatalf("%v: want 422, got %d (%v)", ts, status, data)
			}
			apiWantError(t, data, "validation_error", shareLinkExpiryWindowMsg)
		}
		apiWantIDs(t, "links after refusals", apiShareLinkIDs(t, h, "", owner))
	})

	t.Run("an attachment id nothing was stored under mints nothing and answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", apiAttachmentLinkTarget("att-nosuchblob"), owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "attachment 'att-nosuchblob' not found")
	})

	t.Run("a request without credentials answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiShareLinkUpload(t, h, owner)

		status, data := apiJSON(t, h, "POST", apiAttachmentLinkTarget(id), "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleCreateDiffShareLinkApiDiffShareLinkPost(t *testing.T) {
	t.Run("minting answers the /diff page carrying both addresses and a link that opens the pair with no credential", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		before, after := apiDiffSeededSides(t, h, owner)
		dashboard := apiTestListen(t, api, "")

		data := apiMintShareLink(t, h, "/api/diff/share-link", owner, `{"before":"`+before+`","after":"`+after+`"}`)
		apiWantBody(t, data, map[string]any{
			"url": apiAnyString,
			"share_link": map[string]any{
				"id": apiAnyString, "kind": "diff",
				"attachment_id": nil, "attachment_filename": nil,
				"before": before, "after": after, "label_before": "", "label_after": "",
				"created_by": "owner", "created_ts": apiAnyNumber, "expires_ts": apiAnyNumber,
			},
		})
		dashboard.wantFrames()

		parsed, _ := url.Parse(data["url"].(string))
		if parsed.Path != "/diff" || parsed.Query().Get("before") != before || parsed.Query().Get("after") != after ||
			parsed.Query().Get("link") == "" || len(parsed.Query()) != 3 {
			t.Fatalf("url: want /diff carrying before, after and link only, got %q", data["url"])
		}
		status, pair := apiJSON(t, h, "GET", "/api/diff?"+parsed.RawQuery, "", "")
		if status != 200 {
			t.Fatalf("the minted link must open the pair with no credential: %d %v", status, pair)
		}
		apiWantBody(t, pair, map[string]any{
			"before": map[string]any{"address": before, "gone": false, "mime": "text/plain", "text": "the old wording"},
			"after":  map[string]any{"address": after, "gone": false, "mime": "text/plain", "text": "the new wording"},
		})
	})

	t.Run("both column headings ride in the link, and relabelling or swapping a side answers 410", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		before, after := apiDiffSeededSides(t, h, owner)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		data := apiMintShareLink(t, h, "/api/diff/share-link", agent,
			`{"before":"`+before+`","after":"`+after+`","label_before":"預設內容","label_after":"now","expires_ts":null}`)
		parsed, _ := url.Parse(data["url"].(string))
		if parsed.Query().Get("label_before") != "預設內容" || parsed.Query().Get("label_after") != "now" {
			t.Fatalf("url must carry both labels, got %q", data["url"])
		}
		if status, pair := apiJSON(t, h, "GET", "/api/diff?"+parsed.RawQuery, "", ""); status != 200 {
			t.Fatalf("the minted link must open the pair: %d %v", status, pair)
		}

		relabelled := parsed.Query()
		relabelled.Set("label_after", "預設內容")
		swapped := parsed.Query()
		swapped.Set("before", after)
		swapped.Set("after", before)
		for name, q := range map[string]url.Values{"relabelled": relabelled, "swapped": swapped} {
			status, refused := apiJSON(t, h, "GET", "/api/diff?"+q.Encode(), "", "")
			if status != 410 {
				t.Fatalf("%s: want 410, got %d (%v)", name, status, refused)
			}
			apiWantError(t, refused, "client_error", shareLinkGoneMsg)
		}
	})

	t.Run("a side that is not a sayable address is refused 422 naming which side, and mints nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		before, _ := apiDiffSeededSides(t, h, owner)

		status, data := apiJSON(t, h, "POST", "/api/diff/share-link", owner, `{"before":"`+before+`","after":"att-nope"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"the after side: 'att-nope' is neither a stored attachment id (att- plus 12 hex digits) "+
				"nor a document address (doc:<kind>/<key>/<at>/<field>)")
		apiWantIDs(t, "links after the refusal", apiShareLinkIDs(t, h, "", owner))
	})

	t.Run("an expiry past 365 days is refused 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		before, after := apiDiffSeededSides(t, h, owner)
		ts := strconv.FormatFloat(nowSecs()+shareLinkMaxAheadSecs+3600, 'f', 0, 64)

		status, data := apiJSON(t, h, "POST", "/api/diff/share-link", owner,
			`{"before":"`+before+`","after":"`+after+`","expires_ts":`+ts+`}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", shareLinkExpiryWindowMsg)
	})

	t.Run("a body without before is refused 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		_, after := apiDiffSeededSides(t, h, owner)

		status, data := apiJSON(t, h, "POST", "/api/diff/share-link", owner, `{"after":"`+after+`"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: before")
	})
}

func TestHandleListShareLinksApiShareLinksGet(t *testing.T) {
	t.Run("the list holds live links in mint order and filters by one attachment or one comparison", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		first := apiShareLinkUpload(t, h, owner)
		second := apiDiffUpload(t, h, owner, "other.txt", "other")
		before, after := apiDiffSeededSides(t, h, owner)

		a1 := apiShareLinkID(apiMintShareLink(t, h, apiAttachmentLinkTarget(first), owner, ""))
		b1 := apiShareLinkID(apiMintShareLink(t, h, apiAttachmentLinkTarget(second), owner, ""))
		diff := apiShareLinkID(apiMintShareLink(t, h, "/api/diff/share-link", owner,
			`{"before":"`+before+`","after":"`+after+`","label_before":"old"}`))
		reversed := apiShareLinkID(apiMintShareLink(t, h, "/api/diff/share-link", owner,
			`{"before":"`+after+`","after":"`+before+`"}`))
		expired := apiShareLinkID(apiMintShareLink(t, h, apiAttachmentLinkTarget(first), owner, ""))
		apiExpireShareLinkNow(t, d, expired)

		apiWantIDs(t, "every live link", apiShareLinkIDs(t, h, "", owner), a1, b1, diff, reversed)
		apiWantIDs(t, "one attachment", apiShareLinkIDs(t, h, "?attachment_id="+first, owner), a1)
		apiWantIDs(t, "one comparison whatever its labels",
			apiShareLinkIDs(t, h, "?before="+url.QueryEscape(before)+"&after="+url.QueryEscape(after), owner), diff)
		apiWantIDs(t, "an empty filter value counts as not given", apiShareLinkIDs(t, h, "?attachment_id=", owner),
			a1, b1, diff, reversed)
		apiWantIDs(t, "a filter matching nothing", apiShareLinkIDs(t, h, "?attachment_id=att-000000000000", owner))
	})

	t.Run("one comparison side without the other, or an attachment with a comparison, is refused 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		for target, msg := range map[string]string{
			"/api/share-links?before=att-000000000000":                         "before and after filter one comparison together; give both or neither",
			"/api/share-links?attachment_id=att-000000000000&before=a&after=b": "filter by attachment_id or by before + after, not both",
		} {
			status, data := apiJSON(t, h, "GET", target, owner, "")
			if status != 422 {
				t.Fatalf("%s: want 422, got %d (%v)", target, status, data)
			}
			apiWantError(t, data, "validation_error", msg)
		}
	})

	t.Run("an admin AI member may list, a plain AI member may not", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		id := apiShareLinkUpload(t, h, owner)
		minted := apiShareLinkID(apiMintShareLink(t, h, apiAttachmentLinkTarget(id), owner, ""))
		admin := apiTestPrincipalToken(t, api, d, principalAdminAgent, "m-admin")
		plain := apiTestPrincipalToken(t, api, d, principalAgent, "m-plain")

		apiWantIDs(t, "admin agent", apiShareLinkIDs(t, h, "", admin), minted)
		status, data := apiJSON(t, h, "GET", "/api/share-links", plain, "")
		if status != 403 {
			t.Fatalf("plain agent: want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})
}

func TestHandleRevokeShareLinkApiShareLinksShareLinkIdDelete(t *testing.T) {
	t.Run("revoking answers the link, and only that link stops opening", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		id := apiShareLinkUpload(t, h, owner)
		revoked := apiMintShareLink(t, h, apiAttachmentLinkTarget(id), owner, "")
		kept := apiMintShareLink(t, h, apiAttachmentLinkTarget(id), owner, "")
		admin := apiTestPrincipalToken(t, api, d, principalAdminAgent, "m-admin")

		status, data := apiJSON(t, h, "DELETE", "/api/share-links/"+apiShareLinkID(revoked), admin, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "revoked", any(data), any(revoked["share_link"]))

		if rec := apiRequest(t, h, "GET", revoked["url"].(string), "", ""); rec.Code != http.StatusGone {
			t.Fatalf("revoked link: want 410, got %d (%s)", rec.Code, rec.Body.String())
		}
		if rec := apiRequest(t, h, "GET", kept["url"].(string), "", ""); rec.Code != 200 || rec.Body.String() != "hello" {
			t.Fatalf("the other link to the same blob must keep opening, got %d %q", rec.Code, rec.Body.String())
		}
		if rec := apiRequest(t, h, "GET", "/api/chat/attachment/"+id, owner, ""); rec.Code != 200 {
			t.Fatalf("the owner's login must keep reading the blob, got %d", rec.Code)
		}
		apiWantIDs(t, "remaining links", apiShareLinkIDs(t, h, "", owner), apiShareLinkID(kept))
	})

	t.Run("an id that is unknown, already revoked or expired answers 404", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		id := apiShareLinkUpload(t, h, owner)
		revoked := apiShareLinkID(apiMintShareLink(t, h, apiAttachmentLinkTarget(id), owner, ""))
		apiJSON(t, h, "DELETE", "/api/share-links/"+revoked, owner, "")
		expired := apiShareLinkID(apiMintShareLink(t, h, apiAttachmentLinkTarget(id), owner, ""))
		apiExpireShareLinkNow(t, d, expired)

		for _, target := range []string{"sl-000000000000", revoked, expired} {
			status, data := apiJSON(t, h, "DELETE", "/api/share-links/"+target, owner, "")
			if status != 404 {
				t.Fatalf("%s: want 404, got %d (%v)", target, status, data)
			}
			apiWantError(t, data, "not_found", "share link '"+target+"' not found")
		}
	})

	t.Run("a plain AI member may not revoke", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		id := apiShareLinkUpload(t, h, owner)
		minted := apiShareLinkID(apiMintShareLink(t, h, apiAttachmentLinkTarget(id), owner, ""))
		plain := apiTestPrincipalToken(t, api, d, principalAgent, "m-plain")

		status, data := apiJSON(t, h, "DELETE", "/api/share-links/"+minted, plain, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantIDs(t, "links after the refusal", apiShareLinkIDs(t, h, "", owner), minted)
	})
}

func TestHandleGetChatAttachmentPreviewLinkApiChatAttachmentsAttachmentIdPreviewLinkGet(t *testing.T) {
	t.Run("the preview url carries a sig valid one hour, opens the blob without a credential and is not listed", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiShareLinkUpload(t, h, owner)

		status, data := apiJSON(t, h, "GET", "/api/chat/attachments/"+id+"/preview-link", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"url": apiAnyString})
		parsed, _ := url.Parse(data["url"].(string))
		q := parsed.Query()
		exp, err := strconv.ParseInt(q.Get("exp"), 10, 64)
		if parsed.Path != "/api/chat/attachment/"+id || len(q) != 2 || len(q.Get("sig")) != shareSigLen || err != nil {
			t.Fatalf("url: want the blob path carrying sig and exp only, got %q", data["url"])
		}
		if ahead := exp - time.Now().Unix(); ahead < previewLinkTTLSecs-5 || ahead > previewLinkTTLSecs {
			t.Fatalf("exp is %ds ahead, want one hour", ahead)
		}

		if rec := apiRequest(t, h, "GET", data["url"].(string), "", ""); rec.Code != 200 || rec.Body.String() != "hello" {
			t.Fatalf("preview url: want 200 hello, got %d %q", rec.Code, rec.Body.String())
		}
		apiWantIDs(t, "share links", apiShareLinkIDs(t, h, "", owner))
	})

	t.Run("a past, altered or missing-signature exp answers 401", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		id := apiShareLinkUpload(t, h, owner)
		base := "/api/chat/attachment/" + id + "?"
		live := time.Now().Unix() + 600
		past := time.Now().Unix() - 1
		liveSig := previewSigForRing(api.keys, id, live)

		for name, query := range map[string]string{
			"past exp":    previewLinkQuery(api.keys, id, past),
			"altered exp": "sig=" + liveSig + "&exp=" + strconv.FormatInt(live+3600, 10),
			"other blob":  "sig=" + previewSigForRing(api.keys, "att-000000000000", live) + "&exp=" + strconv.FormatInt(live, 10),
			"bad exp":     "sig=" + liveSig + "&exp=soon",
		} {
			status, data := apiJSON(t, h, "GET", base+query, "", "")
			if status != 401 {
				t.Fatalf("%s: want 401, got %d (%v)", name, status, data)
			}
			apiWantError(t, data, "unauthorized", "invalid signature")
		}
	})

	t.Run("an attachment id nothing was stored under answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/chat/attachments/att-nosuchblob/preview-link", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "attachment 'att-nosuchblob' not found")
	})
}

func TestWriteShareLinkGone(t *testing.T) {
	t.Run("a browser opening a dead link gets the page in the station's language; any other client the envelope", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		id := apiShareLinkUpload(t, h, owner)
		data := apiMintShareLink(t, h, apiAttachmentLinkTarget(id), owner, "")
		apiExpireShareLinkNow(t, d, apiShareLinkID(data))

		status, body := apiJSON(t, h, "GET", data["url"].(string), "", "")
		if status != http.StatusGone {
			t.Fatalf("json client: want 410, got %d (%v)", status, body)
		}
		apiWantError(t, body, "client_error", shareLinkGoneMsg)

		for lang, title := range map[string]string{"en": shareLinkGoneTitle, "zh": shareLinkGoneTitleZh} {
			api.settingsMu.Lock()
			api.displayLanguage = lang
			api.settingsMu.Unlock()
			req := httptest.NewRequest("GET", data["url"].(string), nil)
			req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusGone || !strings.Contains(rec.Body.String(), "<title>"+title+"</title>") {
				t.Fatalf("%s browser: want 410 page titled %q, got %d %q", lang, title, rec.Code, rec.Body.String())
			}
			apiWantHeaders(t, rec, map[string]string{
				"Content-Type":            "text/html; charset=utf-8",
				"Content-Security-Policy": "default-src 'none'; style-src 'unsafe-inline'",
				"Cache-Control":           "no-store",
				"Referrer-Policy":         "no-referrer",
			})
		}
	})

	t.Run("a credential nobody minted answers 410 like an expired one", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiShareLinkUpload(t, h, owner)

		status, data := apiJSON(t, h, "GET", "/api/chat/attachment/"+id+"?link=nobody-minted-this", "", "")
		if status != http.StatusGone {
			t.Fatalf("want 410, got %d (%v)", status, data)
		}
		apiWantError(t, data, "client_error", shareLinkGoneMsg)
	})

	t.Run("a link minted for one blob does not open another", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiShareLinkUpload(t, h, owner)
		other := apiDiffUpload(t, h, owner, "other.txt", "other")
		data := apiMintShareLink(t, h, apiAttachmentLinkTarget(id), owner, "")
		token := strings.SplitN(data["url"].(string), "?link=", 2)[1]

		if rec := apiRequest(t, h, "GET", "/api/chat/attachment/"+other+"?link="+token, "", ""); rec.Code != http.StatusGone {
			t.Fatalf("want 410, got %d (%s)", rec.Code, rec.Body.String())
		}
	})
}

func TestPruneExpiredShareLinks(t *testing.T) {
	_, h, d, owner := newAPITestServer(t)
	id := apiShareLinkUpload(t, h, owner)
	expired := apiShareLinkID(apiMintShareLink(t, h, apiAttachmentLinkTarget(id), owner, ""))
	live := apiShareLinkID(apiMintShareLink(t, h, apiAttachmentLinkTarget(id), owner, ""))
	forever := apiShareLinkID(apiMintShareLink(t, h, apiAttachmentLinkTarget(id), owner, `{"expires_ts":null}`))
	apiExpireShareLinkNow(t, d, expired)

	if n, err := d.PruneExpiredShareLinks(nowSecs()); err != nil || n != 1 {
		t.Fatalf("PruneExpiredShareLinks = (%d, %v), want (1, nil)", n, err)
	}
	rows, err := d.rdb.Query(`SELECT id FROM share_link ORDER BY created_ts, id`)
	if err != nil {
		t.Fatalf("read rows: %v", err)
	}
	defer rows.Close()
	var left []string
	for rows.Next() {
		var rowID string
		_ = rows.Scan(&rowID)
		left = append(left, rowID)
	}
	apiWantIDs(t, "rows left", left, live, forever)
}
