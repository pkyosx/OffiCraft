package main

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/SherClockHolmes/webpush-go"
)

const settingPushVAPIDPrivateKey = "push.vapid_private_key"

// The VAPID subject (owner-set push.contact_email). Its domain must be real:
// Apple rejects the whole VAPID JWT (BadJwtToken) for an unreachable one such
// as .local, and every iPhone silently loses push. It must stay a bare
// address: webpush-go prefixes "mailto:" itself.
func (s *apiServer) pushContactAddress() string {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.pushContactEmail
}

const webPushDeliveryTimeout = 10 * time.Second

func validatePushEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return errors.New("push endpoint must be an absolute HTTPS URL")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return errors.New("push endpoint host is not public")
	}
	if ip, err := netip.ParseAddr(host); err == nil && !isPublicUnicastIP(ip) {
		return errors.New("push endpoint host is not public")
	}
	return nil
}

func isPublicUnicastIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	// netip does not count RFC 6598 shared space (carrier/VPN) as private.
	return !netip.MustParsePrefix("100.64.0.0/10").Contains(ip)
}

func safePushHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: webPushDeliveryTimeout}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// No proxy: through a CONNECT tunnel the proxy's address, not the
	// endpoint's, would be what passes the public-IP check below.
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("push endpoint host could not be resolved")
		}
		for _, ip := range ips {
			if !isPublicUnicastIP(ip) {
				return nil, errors.New("push endpoint resolved to a non-public address")
			}
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
	return &http.Client{
		Timeout:   webPushDeliveryTimeout,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("push endpoint redirects are not allowed")
		},
	}
}

func (s *apiServer) webPushClient() webpush.HTTPClient {
	if s.pushHTTPClient != nil {
		return s.pushHTTPClient
	}
	return safePushHTTPClient()
}

func (s *apiServer) ensurePushVAPIDKey() (string, error) {
	stored, err := s.dal.GetSetting(settingPushVAPIDPrivateKey)
	if err != nil {
		return "", err
	}
	var raw []byte
	if stored == nil {
		key, err := ecdh.P256().GenerateKey(rand.Reader)
		if err != nil {
			return "", err
		}
		raw = key.Bytes()
		if err := s.dal.PutSetting(settingPushVAPIDPrivateKey, base64.RawURLEncoding.EncodeToString(raw)); err != nil {
			return "", err
		}
	} else {
		raw, err = base64.RawURLEncoding.DecodeString(*stored)
		if err != nil {
			return "", err
		}
	}
	key, err := ecdh.P256().NewPrivateKey(raw)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}

func (s *apiServer) pushVAPIDKeys() (publicKey, privateKey string, err error) {
	private, err := s.dal.GetSetting(settingPushVAPIDPrivateKey)
	if err != nil {
		return "", "", err
	}
	if private == nil {
		if _, err := s.ensurePushVAPIDKey(); err != nil {
			return "", "", err
		}
		private, err = s.dal.GetSetting(settingPushVAPIDPrivateKey)
		if err != nil || private == nil {
			return "", "", err
		}
	}
	raw, err := base64.RawURLEncoding.DecodeString(*private)
	if err != nil {
		return "", "", err
	}
	key, err := ecdh.P256().NewPrivateKey(raw)
	if err != nil {
		return "", "", err
	}
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), *private, nil
}

// Minimal on purpose: the service worker renders the notification, so no
// message body or card detail is handed to the push provider.
type webPushPayload struct {
	Kind          string `json:"kind"`
	ChatMessageID string `json:"chat_id,omitempty"`
	ChatPeerID    string `json:"chat_peer_id,omitempty"`
	ReplyCardID   string `json:"reply_card_id,omitempty"`
	Title         string `json:"title"`
	Body          string `json:"body"`
	NeedsDecision bool   `json:"needs_decision,omitempty"`
}

func (s *apiServer) enqueueWebPush(payload webPushPayload) {
	if s.webPushSink != nil {
		s.webPushSink(payload)
		return
	}
	go func() {
		subscriber := s.pushContactAddress()
		if subscriber == "" {
			log.Printf("[push] no contact address configured; delivery skipped")
			return
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			log.Printf("[push] encode payload: %v", err)
			return
		}
		publicKey, privateKey, err := s.pushVAPIDKeys()
		if err != nil {
			log.Printf("[push] load VAPID keys: %v", err)
			return
		}
		subscriptions, err := s.dal.ListPushSubscriptions()
		if err != nil {
			log.Printf("[push] list subscriptions: %v", err)
			return
		}
		for _, subscription := range subscriptions {
			ctx, cancel := context.WithTimeout(context.Background(), webPushDeliveryTimeout)
			response, err := webpush.SendNotificationWithContext(ctx, encoded, &webpush.Subscription{
				Endpoint: subscription.Endpoint,
				Keys:     webpush.Keys{P256dh: subscription.P256dh, Auth: subscription.Auth},
			}, &webpush.Options{
				Subscriber: subscriber,
				TTL:        60, Urgency: webpush.UrgencyHigh,
				VAPIDPublicKey: publicKey, VAPIDPrivateKey: privateKey,
				VapidExpiration: time.Now().Add(12 * time.Hour),
				HTTPClient:      s.webPushClient(),
			})
			cancel()
			if response != nil {
				statusClass := pushDeliveryStatusClass(response.StatusCode)
				log.Printf("[push] delivery status=%d class=%s", response.StatusCode, statusClass)
				response.Body.Close()
				if response.StatusCode == http.StatusGone || response.StatusCode == http.StatusNotFound {
					if err := s.dal.DeletePushSubscription(subscription.Endpoint); err != nil {
						log.Printf("[push] prune expired subscription: %v", err)
					}
					continue
				}
			}
			if err != nil {
				// The error text can contain the endpoint; log only its class.
				log.Printf("[push] delivery error_class=%s", pushDeliveryErrorClass(err))
			}
		}
	}()
}

func pushDeliveryStatusClass(status int) string {
	switch {
	case status >= 200 && status < 300:
		return "accepted"
	case status == http.StatusNotFound || status == http.StatusGone:
		return "expired"
	case status >= 400 && status < 500:
		return "rejected"
	case status >= 500 && status < 600:
		return "gateway_error"
	default:
		return "unexpected"
	}
}

func pushDeliveryErrorClass(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return "network"
	}
	return "send_error"
}

func (s *apiServer) HandleGetPushPublicKeyApiPushPublicKeyGet(w http.ResponseWriter, r *http.Request) {
	key, err := s.ensurePushVAPIDKey()
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, PushPublicKeyDTO{PublicKey: key})
}

func (s *apiServer) HandleCreatePushSubscriptionApiPushSubscriptionPost(w http.ResponseWriter, r *http.Request) {
	var body PushSubscriptionCreateDTO
	if !decodeJSONBodyRequired(w, r, &body, "endpoint", "keys") {
		return
	}
	body.Endpoint = strings.TrimSpace(body.Endpoint)
	body.Keys.P256dh = strings.TrimSpace(body.Keys.P256dh)
	body.Keys.Auth = strings.TrimSpace(body.Keys.Auth)
	if body.Endpoint == "" || body.Keys.P256dh == "" || body.Keys.Auth == "" {
		writeError(w, http.StatusBadRequest, "endpoint and subscription keys must not be blank")
		return
	}
	if err := validatePushEndpoint(body.Endpoint); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.dal.PutPushSubscription(PushSubscription{Endpoint: body.Endpoint, P256dh: body.Keys.P256dh, Auth: body.Keys.Auth, ExpirationTime: body.ExpirationTime}); err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *apiServer) HandleDeletePushSubscriptionApiPushSubscriptionDelete(w http.ResponseWriter, r *http.Request) {
	var body PushSubscriptionDeleteDTO
	if !decodeJSONBodyRequired(w, r, &body, "endpoint") {
		return
	}
	if err := s.dal.DeletePushSubscription(strings.TrimSpace(body.Endpoint)); err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
