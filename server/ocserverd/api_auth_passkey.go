package main

// Owner passkeys (WebAuthn). The RP ID is the request's Host without its port,
// so a passkey registered on one site signs in on that site only.
//
// 🔴 A passkey sign-in asks for neither the password nor the TOTP code: user
// verification is required at registration and at sign-in, which already makes
// it two factors (owner ruling). Do not add a TOTP step here.

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"ocserverd/txguard"
)

const (
	passkeyCeremonyTTL      = 300 * time.Second
	passkeyLoginCeremonyCap = 64
	passkeyMaxPerOwner      = 20
	passkeyUserHandleBytes  = 32
	passkeyUserName         = "owner"
)

// One message for every refusal cause on the public route: naming the cause
// would say whether a passkey exists or which check it failed.
const passkeyLoginFailedMsg = "passkey sign-in failed"

const passkeyCeremonyDeniedMsg = "ceremony_id is unknown, expired, or already used"

const passkeyCredentialRejectedMsg = "credential failed verification"

type passkeyPurpose int

const (
	passkeyPurposeRegister passkeyPurpose = iota + 1
	passkeyPurposeLogin
)

type passkeyCeremony struct {
	id        string
	purpose   passkeyPurpose
	rpID      string
	origin    string
	session   webauthn.SessionData
	expiresAt time.Time
}

// In memory on purpose: begin-login is public, and keeping its state here means
// an anonymous caller can never make the server write SQLite. The price is that
// begin and finish must reach the same process.
type passkeyCeremonyStore struct {
	mu txguard.Mutex
	// insertion order, so the oldest login ceremony is the first one dropped.
	entries []passkeyCeremony
}

func newPasskeyCeremonyStore() *passkeyCeremonyStore {
	return &passkeyCeremonyStore{}
}

func (st *passkeyCeremonyStore) put(c passkeyCeremony, now time.Time) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	c.id = base64.RawURLEncoding.EncodeToString(raw)
	c.expiresAt = now.Add(passkeyCeremonyTTL)

	st.mu.Lock()
	defer st.mu.Unlock()
	kept := st.entries[:0]
	logins := 0
	for _, e := range st.entries {
		if now.After(e.expiresAt) {
			continue
		}
		// One owner, one registration in flight: a new begin replaces the old.
		if c.purpose == passkeyPurposeRegister && e.purpose == passkeyPurposeRegister {
			continue
		}
		if e.purpose == passkeyPurposeLogin {
			logins++
		}
		kept = append(kept, e)
	}
	if c.purpose == passkeyPurposeLogin && logins >= passkeyLoginCeremonyCap {
		drop := logins - passkeyLoginCeremonyCap + 1
		trimmed := kept[:0]
		for _, e := range kept {
			if drop > 0 && e.purpose == passkeyPurposeLogin {
				drop--
				continue
			}
			trimmed = append(trimmed, e)
		}
		kept = trimmed
	}
	st.entries = append(kept, c)
	return c.id, nil
}

// Single use: a matching id is removed whatever the outcome. Constant-time scan
// on purpose, as machineClaimStore.take.
func (st *passkeyCeremonyStore) take(id string, purpose passkeyPurpose, now time.Time) (passkeyCeremony, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i, e := range st.entries {
		if subtle.ConstantTimeCompare([]byte(e.id), []byte(id)) == 1 {
			st.entries = append(st.entries[:i], st.entries[i+1:]...)
			if e.purpose != purpose || now.After(e.expiresAt) {
				return passkeyCeremony{}, false
			}
			return e, true
		}
	}
	return passkeyCeremony{}, false
}

// passkeyRPIDOf answers the RP ID a request on host would use, or the reason
// the host cannot carry a passkey.
func (s *apiServer) passkeyRPIDOf(host string) (rpID, refusal string) {
	h, port := host, ""
	if hostOnly, p, err := net.SplitHostPort(host); err == nil {
		h, port = hostOnly, p
	}
	h = strings.ToLower(strings.TrimSuffix(h, "."))
	if h == "" || net.ParseIP(h) != nil || protocol.ValidateRPID(h) != nil {
		return "", "passkeys cannot be used on an IP address; open this site at " + s.passkeyLocalhostURL(port) + " instead"
	}
	if h == "localhost" {
		if port == "" {
			port = "80"
		}
		// Every port on localhost shares the RP ID "localhost", so without this
		// any other local service could run a ceremony against our passkeys
		// under its own Host and origin and sign in here.
		if s.listenPort == "" || port != s.listenPort {
			return "", "passkeys on localhost work only at " + s.passkeyLocalhostURL("") +
				", the port this server listens on"
		}
	}
	return h, ""
}

func (s *apiServer) passkeyLocalhostURL(requestPort string) string {
	port := s.listenPort
	if port == "" {
		port = requestPort
	}
	if port == "" {
		return "http://localhost"
	}
	return "http://localhost:" + port
}

// A fresh verifier per request: the RP ID and origin come from the request.
func (s *apiServer) passkeyRP(rpID, origin string) (*webauthn.WebAuthn, error) {
	return webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: s.mfaIssuer(),
		RPOrigins:     []string{origin},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			RequireResidentKey: protocol.ResidentKeyRequired(),
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			UserVerification:   protocol.VerificationRequired,
		},
		AttestationPreference: protocol.PreferNoAttestation,
	})
}

type passkeyOwner struct {
	handle      []byte
	credentials []webauthn.Credential
}

func (u passkeyOwner) WebAuthnID() []byte                         { return u.handle }
func (u passkeyOwner) WebAuthnName() string                       { return passkeyUserName }
func (u passkeyOwner) WebAuthnDisplayName() string                { return passkeyUserName }
func (u passkeyOwner) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

func decodePasskeyUserHandle(stored *string) []byte {
	if stored == nil {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(*stored)
	if err != nil || len(raw) != passkeyUserHandleBytes {
		return nil
	}
	return raw
}

func ensurePasskeyUserHandleOn(tx *writeTx) ([]byte, error) {
	stored, err := getSettingOn(tx, settingPasskeyUserHandle)
	if err != nil {
		return nil, err
	}
	if handle := decodePasskeyUserHandle(stored); handle != nil {
		return handle, nil
	}
	handle := make([]byte, passkeyUserHandleBytes)
	if _, err := rand.Read(handle); err != nil {
		return nil, err
	}
	if err := putSettingOn(tx, settingPasskeyUserHandle, base64.RawURLEncoding.EncodeToString(handle)); err != nil {
		return nil, err
	}
	return handle, nil
}

func passkeyTransports(p Passkey) []protocol.AuthenticatorTransport {
	var names []string
	_ = json.Unmarshal([]byte(p.Transports), &names)
	out := make([]protocol.AuthenticatorTransport, 0, len(names))
	for _, n := range names {
		out = append(out, protocol.AuthenticatorTransport(n))
	}
	return out
}

func passkeyCredentialOf(p Passkey) webauthn.Credential {
	flags := protocol.FlagUserPresent | protocol.FlagUserVerified
	if p.BackupEligible {
		flags |= protocol.FlagBackupEligible
	}
	if p.BackupState {
		flags |= protocol.FlagBackupState
	}
	return webauthn.Credential{
		ID:              p.CredentialID,
		PublicKey:       p.PublicKey,
		AttestationType: p.AttestationType,
		Transport:       passkeyTransports(p),
		Flags:           webauthn.NewCredentialFlags(flags),
		Authenticator: webauthn.Authenticator{
			AAGUID:     p.AAGUID,
			SignCount:  p.SignCount,
			Attachment: protocol.AuthenticatorAttachment(p.Attachment),
		},
	}
}

func newPasskeyDTO(p Passkey, requestRPID string) passkeyDTO {
	return passkeyDTO{
		ID:             p.ID,
		Name:           p.Name,
		RPID:           p.RPID,
		Origin:         p.Origin,
		CreatedTS:      p.CreatedTS,
		LastUsedTS:     p.LastUsedTS,
		UsableHere:     requestRPID != "" && p.RPID == requestRPID,
		CloneWarningTS: p.CloneWarningTS,
	}
}

func (s *apiServer) HandleListPasskeysApiAuthPasskeysGet(w http.ResponseWriter, r *http.Request) {
	rows, err := s.dal.ListPasskeys()
	if err != nil {
		internalError(w, err)
		return
	}
	rpID, _ := s.passkeyRPIDOf(r.Host)
	out := passkeysDTO{Passkeys: make([]passkeyDTO, 0, len(rows))}
	for _, p := range rows {
		out.Passkeys = append(out.Passkeys, newPasskeyDTO(p, rpID))
	}
	writeJSON(w, http.StatusOK, out)
}

// 🔴 IT DEMANDS THE PASSWORD, not just the owner token: a passkey outlives the
// session that planted it, so a stolen session must not be able to add one.
func (s *apiServer) HandleBeginPasskeyRegistrationApiAuthPasskeysRegisterBeginPost(w http.ResponseWriter, r *http.Request) {
	var body PasskeyRegistrationBeginDTO
	if !decodeJSONBodyRequired(w, r, &body, "current_password") {
		return
	}
	rpID, refusal := s.passkeyRPIDOf(r.Host)
	if refusal != "" {
		writeError(w, http.StatusBadRequest, refusal)
		return
	}
	origin := baseURLForHost(r.Host)
	rp, err := s.passkeyRP(rpID, origin)
	if err != nil {
		internalError(w, err)
		return
	}

	s.settingsWriteMu.Lock()
	defer s.settingsWriteMu.Unlock()
	if s.passwordHash == "" || !verifyPassword(strOrEmpty(body.CurrentPassword), s.passwordHash) {
		writeError(w, http.StatusUnauthorized, "invalid password")
		return
	}
	rows, err := s.dal.ListPasskeys()
	if err != nil {
		internalError(w, err)
		return
	}
	if len(rows) >= passkeyMaxPerOwner {
		writeError(w, http.StatusConflict, passkeyLimitMsg)
		return
	}
	var handle []byte
	if err := s.dal.inTx(func(tx *writeTx) error {
		var err error
		handle, err = ensurePasskeyUserHandleOn(tx)
		return err
	}); err != nil {
		internalError(w, err)
		return
	}
	var exclude []protocol.CredentialDescriptor
	for _, p := range rows {
		if p.RPID == rpID {
			cred := passkeyCredentialOf(p)
			exclude = append(exclude, cred.Descriptor())
		}
	}
	creation, session, err := rp.BeginRegistration(passkeyOwner{handle: handle},
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
		webauthn.WithExclusions(exclude))
	if err != nil {
		internalError(w, err)
		return
	}
	id, err := s.passkeyCeremonies.put(passkeyCeremony{
		purpose: passkeyPurposeRegister, rpID: rpID, origin: origin, session: *session,
	}, time.Now())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, passkeyCeremonyDTO{
		CeremonyID: id,
		ExpiresIn:  int(passkeyCeremonyTTL.Seconds()),
		Options:    creation.Response,
	})
}

var passkeyLimitMsg = "the owner already has " + strconv.Itoa(passkeyMaxPerOwner) +
	" passkeys, the limit; delete one first"

const passkeyDuplicateMsg = "this authenticator already holds a passkey for this server"

type passkeyRegistrationFinishBody struct {
	CeremonyID string          `json:"ceremony_id"`
	Credential json.RawMessage `json:"credential"`
	Name       string          `json:"name"`
}

func (s *apiServer) HandleFinishPasskeyRegistrationApiAuthPasskeysRegisterFinishPost(w http.ResponseWriter, r *http.Request) {
	var body passkeyRegistrationFinishBody
	if !decodeJSONBodyRequired(w, r, &body, "ceremony_id", "credential", "name") {
		return
	}
	c, ok := s.passkeyCeremonies.take(body.CeremonyID, passkeyPurposeRegister, time.Now())
	if !ok {
		writeError(w, http.StatusBadRequest, passkeyCeremonyDeniedMsg)
		return
	}
	name := trimString(body.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name must not be blank")
		return
	}
	if n := utf8.RuneCountInString(name); n > shortLabelMaxChars {
		writeError(w, http.StatusBadRequest, "name is "+strconv.Itoa(n)+" chars, over the "+
			strconv.Itoa(shortLabelMaxChars)+"-char limit")
		return
	}
	rpID, refusal := s.passkeyRPIDOf(r.Host)
	if refusal != "" || rpID != c.rpID || baseURLForHost(r.Host) != c.origin {
		writeError(w, http.StatusBadRequest, "this registration was begun at "+c.origin+"; finish it there")
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(body.Credential)
	if err != nil {
		writeError(w, http.StatusBadRequest, passkeyCredentialRejectedMsg)
		return
	}
	rp, err := s.passkeyRP(c.rpID, c.origin)
	if err != nil {
		internalError(w, err)
		return
	}
	stored, err := s.dal.GetSetting(settingPasskeyUserHandle)
	if err != nil {
		internalError(w, err)
		return
	}
	cred, err := rp.CreateCredential(passkeyOwner{handle: decodePasskeyUserHandle(stored)}, c.session, parsed)
	if err != nil {
		writeError(w, http.StatusBadRequest, passkeyCredentialRejectedMsg)
		return
	}
	transports := make([]string, 0, len(cred.Transport))
	for _, t := range cred.Transport {
		transports = append(transports, string(t))
	}
	transportsJSON, err := json.Marshal(transports)
	if err != nil {
		internalError(w, err)
		return
	}
	row := Passkey{
		ID:              "pk-" + newHexID(12),
		CredentialID:    cred.ID,
		RPID:            c.rpID,
		Origin:          c.origin,
		PublicKey:       cred.PublicKey,
		AttestationType: cred.AttestationType,
		AAGUID:          cred.Authenticator.AAGUID,
		SignCount:       cred.Authenticator.SignCount,
		Transports:      string(transportsJSON),
		Attachment:      string(cred.Authenticator.Attachment),
		BackupEligible:  cred.Flags.BackupEligible,
		BackupState:     cred.Flags.BackupState,
		Name:            name,
		CreatedTS:       nowSecs(),
	}
	if row.AttestationType == "" {
		row.AttestationType = "none"
	}
	err = s.dal.inTx(func(tx *writeTx) error {
		// Read again inside the transaction: a reset between begin and finish
		// would otherwise let this passkey land under a handle that is gone.
		current, err := getSettingOn(tx, settingPasskeyUserHandle)
		if err != nil {
			return err
		}
		if !bytes.Equal(decodePasskeyUserHandle(current), c.session.UserID) {
			return refuseInTx(http.StatusBadRequest, passkeyCeremonyDeniedMsg)
		}
		dup, err := passkeyCredentialExistsOn(tx, row.CredentialID)
		if err != nil {
			return err
		}
		if dup {
			return refuseInTx(http.StatusConflict, passkeyDuplicateMsg)
		}
		n, err := countPasskeysOn(tx)
		if err != nil {
			return err
		}
		if n >= passkeyMaxPerOwner {
			return refuseInTx(http.StatusConflict, passkeyLimitMsg)
		}
		return insertPasskeyOn(tx, row)
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newPasskeyDTO(row, rpID))
}

// Owner tokens already issued through the passkey are not revoked (spec).
func (s *apiServer) HandleDeletePasskeyApiAuthPasskeysPasskeyIdDelete(w http.ResponseWriter, r *http.Request, passkeyId string) {
	var deleted Passkey
	err := s.dal.inTx(func(tx *writeTx) error {
		cur, err := getPasskeyOn(tx, passkeyId)
		if err != nil {
			return err
		}
		if cur == nil {
			return refuseInTx(http.StatusNotFound, "passkey '"+passkeyId+"' not found")
		}
		deleted = *cur
		return deletePasskeyOn(tx, passkeyId)
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	rpID, _ := s.passkeyRPIDOf(r.Host)
	writeJSON(w, http.StatusOK, newPasskeyDTO(deleted, rpID))
}

// No throttle slot: it does no credential work. allowCredentials stays empty,
// so the answer is the same whether or not a passkey is registered.
func (s *apiServer) HandleBeginPasskeyLoginApiAuthPasskeyLoginBeginPost(w http.ResponseWriter, r *http.Request) {
	if len(s.keys.signingSecret()) == 0 {
		writeError(w, http.StatusUnauthorized, "auth not configured")
		return
	}
	rpID, refusal := s.passkeyRPIDOf(r.Host)
	if refusal != "" {
		writeError(w, http.StatusBadRequest, refusal)
		return
	}
	origin := baseURLForHost(r.Host)
	rp, err := s.passkeyRP(rpID, origin)
	if err != nil {
		internalError(w, err)
		return
	}
	assertion, session, err := rp.BeginDiscoverableLogin(
		webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		internalError(w, err)
		return
	}
	id, err := s.passkeyCeremonies.put(passkeyCeremony{
		purpose: passkeyPurposeLogin, rpID: rpID, origin: origin, session: *session,
	}, time.Now())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, passkeyCeremonyDTO{
		CeremonyID: id,
		ExpiresIn:  int(passkeyCeremonyTTL.Seconds()),
		Options:    assertion.Response,
	})
}

type passkeyLoginFinishBody struct {
	CeremonyID string          `json:"ceremony_id"`
	Credential json.RawMessage `json:"credential"`
}

var errPasskeyUnknown = errors.New("no such passkey")

// 🔴 Every refusal is the same 401 after the same floor as /api/login, and
// shares its throttle. It must NOT call noteFactorRefusedAfterCorrectPassword:
// a failed passkey says nothing about the password, and the alert would be a
// false alarm an anonymous caller can trigger at will.
func (s *apiServer) HandleFinishPasskeyLoginApiAuthPasskeyLoginFinishPost(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	var body passkeyLoginFinishBody
	if !decodeJSONBodyRequired(w, r, &body, "ceremony_id", "credential") {
		return
	}
	if len(s.keys.signingSecret()) == 0 {
		writeError(w, http.StatusUnauthorized, "auth not configured")
		return
	}
	release, wait, blocked := s.loginThrottle.begin()
	if blocked {
		writeThrottled(w, wait)
		return
	}
	defer release()

	if !s.verifyPasskeyAssertion(r, body) {
		s.holdFailureFloor(started)
		writeError(w, http.StatusUnauthorized, passkeyLoginFailedMsg)
		return
	}
	s.writeOwnerToken(w, s.ownerTokenTTLValue(), time.Now().Unix())
}

func (s *apiServer) verifyPasskeyAssertion(r *http.Request, body passkeyLoginFinishBody) bool {
	c, ok := s.passkeyCeremonies.take(body.CeremonyID, passkeyPurposeLogin, time.Now())
	if !ok {
		return false
	}
	rpID, refusal := s.passkeyRPIDOf(r.Host)
	if refusal != "" || rpID != c.rpID || baseURLForHost(r.Host) != c.origin {
		return false
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(body.Credential)
	if err != nil {
		return false
	}
	rp, err := s.passkeyRP(c.rpID, c.origin)
	if err != nil {
		return false
	}
	stored, err := s.dal.GetSetting(settingPasskeyUserHandle)
	if err != nil {
		return false
	}
	handle := decodePasskeyUserHandle(stored)
	if handle == nil {
		return false
	}
	var row *Passkey
	lookup := func(rawID, userHandle []byte) (webauthn.User, error) {
		if subtle.ConstantTimeCompare(userHandle, handle) != 1 {
			return nil, errPasskeyUnknown
		}
		p, err := s.dal.GetPasskeyByCredential(rawID, c.rpID)
		if err != nil {
			return nil, err
		}
		if p == nil {
			return nil, errPasskeyUnknown
		}
		row = p
		return passkeyOwner{handle: handle, credentials: []webauthn.Credential{passkeyCredentialOf(*p)}}, nil
	}
	_, cred, err := rp.ValidatePasskeyLogin(lookup, c.session, parsed)
	if err != nil || row == nil {
		return false
	}
	now := nowSecs()
	if cred.Authenticator.CloneWarning {
		if err := s.dal.MarkPasskeyCloneWarning(row.ID, now); err != nil {
			log.Printf("[passkey] recording the clone warning on %s failed: %v", row.ID, err)
		}
		return false
	}
	still, err := s.dal.MarkPasskeySignedIn(row.ID, cred.Authenticator.SignCount, cred.Flags.BackupState, now)
	if err != nil || !still {
		return false
	}
	return true
}
