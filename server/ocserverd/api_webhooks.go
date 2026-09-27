package main

// Webhooks: owner-facing CRUD plus the PUBLIC /in inlet. Delivery reuses the
// chat channel: an accepted /in POST synthesises ONE ordinary chat_message from
// `hook:<endpoint_id>` and fans the same "chat" delta, so it inherits chat's
// offline queue, SSE push and on-wake catch-up, and never auto-wakes the member
// (SPEC §2). meta.webhook.purpose is what the member's 用途守衛 (seed §3)
// judges the untrusted payload against.

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// A public unauthenticated inlet must not be an amplification / memory sink.
// 🔴 Over-cap is a REFUSAL, not a truncation (owner rc-a40ef8c66781): a cut body
// also fed truncated bytes to the HMAC gates, so legitimate signed calls were
// classified sig_failed.
const webhookPayloadMaxBytes = 1 << 20

const (
	webhookLogHeadersMaxBytes = 4 << 10
	webhookLogBodyMaxBytes    = 16 << 10
)

const webhookInternalErrorMessage = "internal server error"

func writeWebhookInternalError(w http.ResponseWriter, err error) {
	log.Printf("[webhook] internal error: %v", err)
	writeError(w, http.StatusInternalServerError, webhookInternalErrorMessage)
}

const (
	webhookOutcomeDelivered     = "delivered"
	webhookOutcomeChallenge     = "challenge"
	webhookOutcomePing          = "ping"
	webhookOutcomeDroppedPrefix = "dropped:"
)

// logWebhookRequest is STRICTLY best-effort: the inlet's byte-identical silent
// face must never be perturbed by observability, so every error is swallowed.
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

func (s *apiServer) recordWebhookOversizeRejection(token string, r *http.Request, payload []byte) {
	e, err := s.dal.GetWebhookByToken(token)
	if err != nil || e == nil {
		return
	}
	ts := nowSecs()
	_ = s.dal.MarkWebhookDropped(e.Token, WebhookDropReasonOversize, ts)
	s.logWebhookRequest(e.Token, r, payload,
		webhookOutcomeDroppedPrefix+WebhookDropReasonOversize, ts)
}

func newWebhookToken() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func (s *apiServer) HandleListWebhooksApiMembersMemberIdWebhooksGet(w http.ResponseWriter, r *http.Request, memberId string) {
	m, err := s.resolveMember(memberId, anyMember)
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

func (s *apiServer) HandleCreateWebhookApiMembersMemberIdWebhooksPost(w http.ResponseWriter, r *http.Request, memberId string) {
	var body WebhookCreateDTO
	if !decodeJSONBodyRequired(w, r, &body, "endpoint_id") {
		return
	}
	m, err := s.resolveMember(memberId, anyMember)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	endpointID := trimString(body.EndpointId)
	if err := ValidateWebhookEndpointID(endpointID); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
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

func (s *apiServer) HandleUpdateWebhookApiMembersMemberIdWebhooksEndpointIdPatch(w http.ResponseWriter, r *http.Request, memberId, endpointId string) {
	var body WebhookUpdateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	e, err := s.resolveWebhook(memberId, endpointId, anyMember)
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
	if body.SigningSecret != nil {
		e.SigningSecret = *body.SigningSecret
	}
	if err := s.dal.PutWebhookEndpoint(*e); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newWebhookEndpointDTO(*e))
}

func (s *apiServer) HandleDeleteWebhookApiMembersMemberIdWebhooksEndpointIdDelete(w http.ResponseWriter, r *http.Request, memberId, endpointId string) {
	e, err := s.resolveWebhook(memberId, endpointId, anyMember)
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

// 🔴 The member lookup scope is a PARAMETER: this serves PATCH, DELETE and the
// delivery-log read, and a hard-wired scope here would decide the contractor
// question for all three out of sight of each.
func (s *apiServer) resolveWebhook(memberID, endpointID string, scope memberScope) (*WebhookEndpoint, error) {
	m, err := s.resolveMember(memberID, scope)
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

// POST /in — the PUBLIC inlet. Identity comes SOLELY from ?t=; an unknown or
// disabled token, an absent member and a missing token all answer the SAME
// silent 200 — never reveal whether an endpoint exists. A repeated ?t= is
// refused 422 by the generated parameter binding before this handler runs.
func (s *apiServer) HandleReceiveWebhookInPost(w http.ResponseWriter, r *http.Request, params HandleReceiveWebhookInPostParams) {
	// Read the body regardless of token validity (no timing difference); cap+1
	// proves over-cap without buffering an unbounded body.
	payload, err := io.ReadAll(io.LimitReader(r.Body, webhookPayloadMaxBytes+1))
	// 🔴 A CUT BODY IS NOT A SHORT BODY: gates below would treat it as the whole
	// request and answer "ok", so the sender never resends. Refuse it before the
	// size verdict too.
	if err != nil {
		writeWebhookInternalError(w, err)
		return
	}

	token := ""
	if params.T != nil {
		token = *params.T
	}
	// 🔴 The size verdict precedes any identity or signature work, and the 413 is
	// byte-identical whatever the token resolves to (asserted by test); the
	// recording call only finds somewhere to write. It must also precede the
	// platform gates: an HMAC over a cut body is a lie about the sender.
	if len(payload) > webhookPayloadMaxBytes {
		s.recordWebhookOversizeRejection(token, r, payload)
		writeError(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("webhook payload is too large (max %d bytes)", webhookPayloadMaxBytes))
		return
	}
	e, err := s.dal.GetWebhookByToken(token)
	if err != nil {
		writeWebhookInternalError(w, err)
		return
	}
	if e == nil {
		s.writeWebhookAccepted(w)
		return
	}
	receivedTS := nowSecs()
	if e.Status != WebhookStatusEnabled {
		_ = s.dal.MarkWebhookDropped(e.Token, WebhookDropReasonDisabled, receivedTS)
		s.logWebhookRequest(e.Token, r, payload, webhookOutcomeDroppedPrefix+WebhookDropReasonDisabled, receivedTS)
		s.writeWebhookAccepted(w)
		return
	}
	// Verification runs on the SAME raw bytes read above, never after a JSON
	// decode. Only Slack's url_verification handshake answers a distinct body —
	// Slack needs the challenge echoed to activate the subscription.
	switch e.Platform {
	case WebhookPlatformSlack:
		if challenge, ok := slackURLVerificationChallenge(payload); ok {
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
		if r.Header.Get("X-GitHub-Event") == "ping" {
			_ = s.dal.TouchWebhookReceived(e.Token, receivedTS)
			s.logWebhookRequest(e.Token, r, payload, webhookOutcomePing, receivedTS)
			s.writeWebhookAccepted(w)
			return
		}
	}
	// 🔴 THE INLET'S DOOR IS THE CHAT DOOR: resolveChatRecipient already decides
	// who may receive (ACTIVE, staff or outsource), so no separate rule belongs
	// here — 「請不要再製造分岔」(owner). wireOwnerID is refused first: it is a
	// legal chat address but never a member row, so it can only come from corrupt
	// data on the one UNAUTHENTICATED surface.
	if e.MemberID == wireOwnerID {
		_ = s.dal.MarkWebhookDropped(e.Token, WebhookDropReasonMemberGone, receivedTS)
		s.logWebhookRequest(e.Token, r, payload, webhookOutcomeDroppedPrefix+WebhookDropReasonMemberGone, receivedTS)
		s.writeWebhookAccepted(w)
		return
	}
	recipientID, err := s.resolveChatRecipient(e.MemberID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			_ = s.dal.MarkWebhookDropped(e.Token, WebhookDropReasonMemberGone, receivedTS)
			s.logWebhookRequest(e.Token, r, payload, webhookOutcomeDroppedPrefix+WebhookDropReasonMemberGone, receivedTS)
			s.writeWebhookAccepted(w)
			return
		}
		writeWebhookInternalError(w, err)
		return
	}
	msg := ChatMessage{
		ID:        "c-" + newHexID(12),
		Sender:    "hook:" + e.EndpointID,
		Recipient: recipientID,
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
		writeWebhookInternalError(w, err)
		return
	}
	// The payload every chat delta carries (spec/sse.md §2.2).
	s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+msg.ID,
		map[string]any{"id": msg.ID, "from": msg.Sender, "to": msg.Recipient},
		audienceMembers(msg.Sender, msg.Recipient), triggerServer)
	_ = s.dal.MarkWebhookDelivered(e.Token, receivedTS)
	s.logWebhookRequest(e.Token, r, payload, webhookOutcomeDelivered, receivedTS)
	s.writeWebhookAccepted(w)
}

// requires=admin_agent: raw UNVERIFIED external payloads never reach a plain
// agent; kept off WebhookEndpointDTO so the list wire stays light.
func (s *apiServer) HandleListWebhookRequestsApiMembersMemberIdWebhooksEndpointIdRequestsGet(w http.ResponseWriter, r *http.Request, memberId, endpointId string) {
	e, err := s.resolveWebhook(memberId, endpointId, anyMember)
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

func (s *apiServer) writeWebhookAccepted(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
