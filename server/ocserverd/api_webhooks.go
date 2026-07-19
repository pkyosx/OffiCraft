package main

// api_webhooks.go — M4 回呼端點 (webhook) handlers: the owner-facing CRUD over a
// member's webhook_endpoint rows + the PUBLIC /in inlet.
//
// Delivery mechanism (投遞方式 A — chat channel reuse): an accepted /in POST
// does NOT mint a new event type or SSE topic. It synthesises ONE ordinary
// chat_message from a synthetic sender (`hook:<endpoint_id>`) to the member and
// fans the same "chat" delta everyone else rides — so it inherits chat's
// durable offline queue, online SSE push, and on-wake catch-up for free, and
// never auto-wakes the member (SPEC §2 投遞). meta.webhook={endpoint_id,purpose}
// carries the per-endpoint purpose the member's 用途守衛 (seed §3) judges the
// untrusted payload against.

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// webhookPayloadMaxBytes caps a single /in payload. Webhook payloads are small
// control/event blobs, so this sits FAR below chat's 100 MB attachment ceiling
// — a public unauthenticated inlet must not be an amplification / memory sink.
const webhookPayloadMaxBytes = 1 << 20 // 1 MiB

// Request-log caps (migrations/00014 webhook_request_log): the ring buffer
// stores the raw request for debugging, cut at sane ceilings — headers as a
// JSON map ≤4 KiB, body text ≤16 KiB — with a truncated marker when either
// was cut. The 1 MiB payload cap above still bounds what we ever read.
const (
	webhookLogHeadersMaxBytes = 4 << 10  // 4 KiB
	webhookLogBodyMaxBytes    = 16 << 10 // 16 KiB
)

// Webhook request-log outcome labels — the closed classification a resolved
// /in request lands as. Drops carry their coarse reason as
// "dropped:<WebhookDropReason>"; an unknown token has no endpoint to log
// against, by construction.
const (
	webhookOutcomeDelivered     = "delivered"
	webhookOutcomeChallenge     = "challenge"
	webhookOutcomePing          = "ping"
	webhookOutcomeDroppedPrefix = "dropped:"
)

// logWebhookRequest records one resolved /in request into the endpoint's
// debug ring buffer (newest 5 kept). STRICTLY best-effort: the public inlet's
// byte-identical silent face must never be perturbed by observability, so
// every error here is swallowed.
func (s *apiServer) logWebhookRequest(token string, r *http.Request, payload []byte, outcome string, ts float64) {
	truncated := false
	headers, err := json.Marshal(r.Header)
	if err != nil {
		headers = []byte("{}")
	}
	if len(headers) > webhookLogHeadersMaxBytes {
		headers = headers[:webhookLogHeadersMaxBytes]
		truncated = true
	}
	body := payload
	if len(body) > webhookLogBodyMaxBytes {
		body = body[:webhookLogBodyMaxBytes]
		truncated = true
	}
	_ = s.dal.InsertWebhookRequestLog(token, WebhookRequestLog{
		TS:        ts,
		Outcome:   outcome,
		Headers:   string(headers),
		Body:      string(body),
		Truncated: truncated,
	})
}

// newWebhookToken mints a high-entropy, unguessable, URL-safe opaque token
// (32 bytes of crypto/rand → base64url, ~43 chars). Distinct from newHexID's
// 12-hex member/chat ids: a webhook token is a bearer credential, not a
// display id, so it takes full 256-bit entropy.
func newWebhookToken() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err) // the OS entropy source failing is not a servable state
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// GET /api/members/{member_id}/webhooks — the member's endpoints, oldest→newest.
func (s *apiServer) HandleListWebhooksApiMembersMemberIdWebhooksGet(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	rows, err := s.dal.ListWebhooksByMember(m.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	out := []webhookEndpointDTO{}
	for _, e := range rows {
		out = append(out, newWebhookEndpointDTO(e))
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/members/{member_id}/webhooks — create. endpoint_id is required,
// closed-charset, and per-member unique (409 on a duplicate); the server mints
// the opaque token and returns it once.
func (s *apiServer) HandleCreateWebhookApiMembersMemberIdWebhooksPost(w http.ResponseWriter, r *http.Request, memberId string) {
	var body WebhookCreateDTO
	if !decodeJSONBodyRequired(w, r, &body, "endpoint_id") {
		return
	}
	m, err := s.resolveMember(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	endpointID := trimString(body.EndpointId)
	if err := ValidateWebhookEndpointID(endpointID); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	// platform is the fixed-at-creation verification preset; default generic.
	platform := WebhookPlatformGeneric
	if body.Platform != nil && string(*body.Platform) != "" {
		platform = string(*body.Platform)
	}
	if !ValidWebhookPlatform(platform) {
		writeError(w, http.StatusUnprocessableEntity,
			"platform must be one of ['generic' 'slack' 'github']; got '"+platform+"'")
		return
	}
	signingSecret := strOrEmpty(body.SigningSecret)
	// slack/github verification is impossible without a shared secret — reject at
	// the create seam rather than silently accept an endpoint that can never
	// verify a single call.
	if platform != WebhookPlatformGeneric && signingSecret == "" {
		writeError(w, http.StatusUnprocessableEntity,
			"signing_secret is required when platform is '"+platform+"'")
		return
	}
	existing, err := s.dal.GetWebhookByMemberEndpoint(m.ID, endpointID)
	if err != nil {
		internalError(w, err)
		return
	}
	if existing != nil {
		writeError(w, http.StatusConflict,
			"a webhook endpoint '"+endpointID+"' already exists for this member")
		return
	}
	e := WebhookEndpoint{
		Token:         newWebhookToken(),
		MemberID:      m.ID,
		EndpointID:    endpointID,
		Purpose:       strOrEmpty(body.Purpose),
		Status:        WebhookStatusEnabled,
		CreatedTS:     nowSecs(),
		Platform:      platform,
		SigningSecret: signingSecret,
	}
	if err := s.dal.PutWebhookEndpoint(e); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newWebhookEndpointDTO(e))
}

// PATCH /api/members/{member_id}/webhooks/{endpoint_id} — flip status and/or
// edit purpose. endpoint_id is immutable (address key, not editable here).
func (s *apiServer) HandleUpdateWebhookApiMembersMemberIdWebhooksEndpointIdPatch(w http.ResponseWriter, r *http.Request, memberId, endpointId string) {
	var body WebhookUpdateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	e, err := s.resolveWebhook(memberId, endpointId)
	if err != nil {
		writeResolveError(w, err, "webhook endpoint", endpointId)
		return
	}
	if body.Status != nil {
		if !ValidWebhookStatus(*body.Status) {
			writeError(w, http.StatusUnprocessableEntity,
				"status must be one of ['enabled' 'disabled']; got '"+*body.Status+"'")
			return
		}
		e.Status = *body.Status
	}
	if body.Purpose != nil {
		e.Purpose = *body.Purpose
	}
	// signing_secret rotation: platform is immutable here, but the shared secret
	// can be re-supplied to rotate it. An explicit "" clears it (though that
	// leaves a slack/github endpoint unable to verify — the owner's choice).
	if body.SigningSecret != nil {
		e.SigningSecret = *body.SigningSecret
	}
	if err := s.dal.PutWebhookEndpoint(*e); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newWebhookEndpointDTO(*e))
}

// DELETE /api/members/{member_id}/webhooks/{endpoint_id} — permanent revocation
// (the token can never deliver again).
func (s *apiServer) HandleDeleteWebhookApiMembersMemberIdWebhooksEndpointIdDelete(w http.ResponseWriter, r *http.Request, memberId, endpointId string) {
	e, err := s.resolveWebhook(memberId, endpointId)
	if err != nil {
		writeResolveError(w, err, "webhook endpoint", endpointId)
		return
	}
	if err := s.dal.DeleteWebhookEndpoint(e.Token); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newWebhookEndpointDTO(*e))
}

// resolveWebhook returns the endpoint addressed by (member, endpoint_id),
// folding an absent member OR an absent endpoint to errNotFound (the 404 face).
func (s *apiServer) resolveWebhook(memberID, endpointID string) (*WebhookEndpoint, error) {
	m, err := s.resolveMember(memberID)
	if err != nil {
		return nil, err
	}
	e, err := s.dal.GetWebhookByMemberEndpoint(m.ID, endpointID)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, errNotFound
	}
	return e, nil
}

// POST /in — the PUBLIC webhook inlet. Identity is resolved SOLELY from ?t=;
// an unknown/disabled token, an absent member, or a missing token all answer
// the SAME silent 200 (never reveal whether an endpoint exists). An accepted
// call synthesises exactly ONE chat_message to the member (投遞方式 A).
func (s *apiServer) HandleReceiveWebhookInPost(w http.ResponseWriter, r *http.Request, params HandleReceiveWebhookInPostParams) {
	// Read + cap the untrusted body regardless of token validity so a client
	// never learns anything from timing/short-circuit differences.
	payload, _ := io.ReadAll(io.LimitReader(r.Body, webhookPayloadMaxBytes))

	token := ""
	if params.T != nil {
		token = *params.T
	}
	e, err := s.dal.GetWebhookByToken(token)
	if err != nil {
		internalError(w, err)
		return
	}
	// Silent acceptance for an unknown token (無效 沉默回應) — no endpoint row
	// exists, so there is nothing to count against either.
	if e == nil {
		s.writeWebhookAccepted(w)
		return
	}
	// Observability (migrations/00014): every path below records best-effort
	// against the endpoint row — counters on webhook_endpoint plus one raw
	// request row in the webhook_request_log ring buffer (newest 5). A write
	// failure must never perturb the byte-identical silent face, so errors are
	// deliberately swallowed. The HTTP response stays EXACTLY as before.
	receivedTS := nowSecs()
	if e.Status != WebhookStatusEnabled {
		_ = s.dal.MarkWebhookDropped(e.Token, WebhookDropReasonDisabled, receivedTS)
		s.logWebhookRequest(e.Token, r, payload, webhookOutcomeDroppedPrefix+WebhookDropReasonDisabled, receivedTS)
		s.writeWebhookAccepted(w)
		return
	}
	// Platform verification gate — applied to the SAME raw bytes read above (never
	// after a JSON decode). A failed verification falls through to the identical
	// silent 200 as every other non-deliverable case (沉默丟棄, no leak). Only the
	// Slack url_verification handshake answers with a distinct (challenge) body,
	// by design — Slack needs it echoed to activate the subscription.
	switch e.Platform {
	case WebhookPlatformSlack:
		if challenge, ok := slackURLVerificationChallenge(payload); ok {
			// The handshake proves the caller reached us but neither delivers
			// nor drops — stamp last_received_ts only.
			_ = s.dal.TouchWebhookReceived(e.Token, receivedTS)
			s.logWebhookRequest(e.Token, r, payload, webhookOutcomeChallenge, receivedTS)
			writeJSON(w, http.StatusOK, map[string]any{"challenge": challenge})
			return
		}
		if !verifySlackSignature(e.SigningSecret,
			r.Header.Get("X-Slack-Signature"),
			r.Header.Get("X-Slack-Request-Timestamp"),
			payload, time.Now().Unix()) {
			_ = s.dal.MarkWebhookDropped(e.Token, WebhookDropReasonSigFailed, receivedTS)
			s.logWebhookRequest(e.Token, r, payload, webhookOutcomeDroppedPrefix+WebhookDropReasonSigFailed, receivedTS)
			s.writeWebhookAccepted(w)
			return
		}
	case WebhookPlatformGithub:
		if !verifyGithubSignature(e.SigningSecret,
			r.Header.Get("X-Hub-Signature-256"), payload) {
			_ = s.dal.MarkWebhookDropped(e.Token, WebhookDropReasonSigFailed, receivedTS)
			s.logWebhookRequest(e.Token, r, payload, webhookOutcomeDroppedPrefix+WebhookDropReasonSigFailed, receivedTS)
			s.writeWebhookAccepted(w)
			return
		}
		// A verified GitHub `ping` (sent once when the webhook is created) proves
		// the wiring but carries no member-facing content — ack it with the same
		// silent 200 and DON'T synthesise a chat (avoids a noise event on setup).
		// Verified-but-undelivered: received-only, like the Slack handshake.
		if r.Header.Get("X-GitHub-Event") == "ping" {
			_ = s.dal.TouchWebhookReceived(e.Token, receivedTS)
			s.logWebhookRequest(e.Token, r, payload, webhookOutcomePing, receivedTS)
			s.writeWebhookAccepted(w)
			return
		}
	}
	m, err := s.resolveMember(e.MemberID)
	if err != nil {
		if err == errNotFound {
			_ = s.dal.MarkWebhookDropped(e.Token, WebhookDropReasonMemberGone, receivedTS)
			s.logWebhookRequest(e.Token, r, payload, webhookOutcomeDroppedPrefix+WebhookDropReasonMemberGone, receivedTS)
			s.writeWebhookAccepted(w)
			return
		}
		internalError(w, err)
		return
	}
	// ONE POST → at most ONE chat event (防放大). Synthetic sender + the
	// per-endpoint purpose the member's 用途守衛 (seed §3) reads from meta.
	msg := ChatMessage{
		ID:        "c-" + newHexID(12),
		Sender:    "hook:" + e.EndpointID,
		Recipient: m.ID,
		Body:      string(payload),
		TS:        nowSecs(),
		Meta: map[string]any{
			"webhook": map[string]any{
				"endpoint_id": e.EndpointID,
				"purpose":     e.Purpose,
			},
		},
	}
	if err := s.dal.PutChat(msg); err != nil {
		internalError(w, err)
		return
	}
	// The same convenience payload every chat delta carries (spec/sse.md §2.2)
	// — the member's online SSE stream and offline unread both key off it.
	// Addressed to both participants + owner (spec §4).
	s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+msg.ID,
		map[string]any{"id": msg.ID, "from": msg.Sender, "to": msg.Recipient},
		audienceMembers(msg.Sender, msg.Recipient), triggerServer)
	_ = s.dal.MarkWebhookDelivered(e.Token, receivedTS)
	s.logWebhookRequest(e.Token, r, payload, webhookOutcomeDelivered, receivedTS)
	s.writeWebhookAccepted(w)
}

// GET /api/members/{member_id}/webhooks/{endpoint_id}/requests — the debug
// ring buffer: the last 5 raw requests /in resolved to this endpoint, newest
// first (owner-only; kept OFF WebhookEndpointDTO so the list wire stays light).
func (s *apiServer) HandleListWebhookRequestsApiMembersMemberIdWebhooksEndpointIdRequestsGet(w http.ResponseWriter, r *http.Request, memberId, endpointId string) {
	e, err := s.resolveWebhook(memberId, endpointId)
	if err != nil {
		writeResolveError(w, err, "webhook endpoint", endpointId)
		return
	}
	rows, err := s.dal.ListWebhookRequestLogs(e.Token)
	if err != nil {
		internalError(w, err)
		return
	}
	out := []webhookRequestLogDTO{}
	for _, l := range rows {
		out = append(out, newWebhookRequestLogDTO(l))
	}
	writeJSON(w, http.StatusOK, out)
}

// writeWebhookAccepted is the single silent acknowledgement — byte-identical for
// an accepted and an ignored call so the response never leaks endpoint existence.
func (s *apiServer) writeWebhookAccepted(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
