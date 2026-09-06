import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { isHttpStatus, retryAfterSeconds } from "../api/errors";
import type { MfaEnrollView } from "../types";
import {
  adoptServerSettings,
  loadServerSettings,
} from "../hooks/sharedServerSettings";
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  GearIcon,
  BellIcon,
  LogOutIcon,
  UserIcon,
} from "./icons";
import { InlineEdit } from "./InlineEdit";
import { qrSvg } from "../lib/qrSvg";
import "./profile-dropdown.css";

interface ProfileDropdownProps {
  open: boolean;
  onClose: () => void;
  /** Real-mode logout hook (AuthGate): clears the owner token + returns to the
   * login wall. Undefined/no-op in mock mode. */
  onLogout?: () => void;
  /** Resolved owner nickname for the profile header (server-backed, T-0b41);
   * falls back to the localized default when unset. */
  userName: string;
  /** Commit an edited nickname to the server (PATCH /api/settings). */
  setOwnerName: (next: string) => void;
}

type View = "main" | "preferences" | "password" | "notifications" | "mfa";

/**
 * Profile menu that drops from the topbar profile pill.
 *  - main view: profile header (inline rename), Preferences row, Log out.
 *  - preferences view: Theme SELECTOR (辦公室 / custom) + Language
 *    (中文 / English) + Layout (窄版 / 寬版).
 *  - account rows in the main view: notification email and password.
 *  - password view: current / new / repeat → POST /api/auth/change-password.
 *
 * Scope (owner 2026-07-12): this menu holds APPEARANCE + ACCOUNT IDENTITY only.
 * T-16a1 P3b narrowed 外觀 further: this dropdown now only SELECTS a theme; all
 * theme MANAGEMENT (add / edit colours / 用詞 / import / export / delete)
 * moved to the 設定 page's 主題 sub-section (SettingsPage → ThemeSettings)
 * so selection stays a quick flip here and management lives in one place. The
 * server PARAMETER knobs likewise live in 設定/參數調整 — all of them, which
 * is why this line names none of them.
 *
 * Local preferences persist via the i18n/preferences provider. Click-outside +
 * toggling is owned by the parent (App) via a wrapping ref.
 */
export function ProfileDropdown({
  open,
  onClose,
  onLogout,
  userName,
  setOwnerName,
}: ProfileDropdownProps) {
  const {
    t,
    theme,
    setTheme,
    themeList,
    language,
    setLanguage,
    wide,
    setWide,
    resetPreferences, msg } = useI18n();

  const [view, setView] = useState<View>("main");

  // ── change-password form state ────────────────────────────────────────────
  const [currentPwd, setCurrentPwd] = useState("");
  const [newPwd, setNewPwd] = useState("");
  const [confirmPwd, setConfirmPwd] = useState("");
  const [pwdBusy, setPwdBusy] = useState(false);
  const [pwdDone, setPwdDone] = useState(false);
  const [pwdError, setPwdError] = useState<
    "" | "current" | "short" | "mismatch" | "throttled"
  >("");
  /** Seconds from a 429 on change-password (the shared credential brake). */
  const [pwdRetryAfter, setPwdRetryAfter] = useState(0);
  // ── second-factor (TOTP) state ────────────────────────────────────────────
  // `mfaEnrolled` is the ARMED bit, read from the public probe when this view
  // opens rather than cached at mount: the owner may have armed the factor from
  // another device, and a stale "off" here would offer to enrol a second time
  // (which the server refuses with 409 — an error the owner did nothing to
  // deserve).
  // null = NOT READ YET, which is a third state and not a quieter "off". The
  // row's subtitle is a security claim: rendering "Off — your password is the
  // only key" from an unread default tells an owner whose factor IS armed the
  // exact opposite of the truth, on every fresh mount of the menu.
  const [mfaEnrolled, setMfaEnrolled] = useState<boolean | null>(null);
  // The ship-dark rollout flag. null = not read yet (same "never fabricate a
  // default" rule as mfaEnrolled): claiming the feature is unavailable when we
  // simply have not asked would hide a button the owner does have.
  const [mfaOffered, setMfaOffered] = useState<boolean | null>(null);
  const [mfaLoaded, setMfaLoaded] = useState(false);
  // The PENDING enrolment, held in component state ONLY — never persisted. It
  // is the one moment a secret exists on this client, and it dies with the view.
  const [mfaPending, setMfaPending] = useState<MfaEnrollView | null>(null);
  const [mfaCode, setMfaCode] = useState("");
  // ONE password field serves both the activate and the disable forms — they are
  // never on screen at the same time (the panels are mutually exclusive on
  // mfaEnrolled), and openMfaView clears it on every entry.
  const [mfaPwd, setMfaPwd] = useState("");
  const [mfaBusy, setMfaBusy] = useState(false);
  const [mfaNotice, setMfaNotice] = useState<"" | "activated" | "disabled">("");
  const [mfaError, setMfaError] = useState<
    "" | "code" | "disable" | "load" | "throttled" | "session" | "offer"
  >("");
  /** Seconds from a 429's Retry-After, for the throttled message. */
  const [mfaRetryAfter, setMfaRetryAfter] = useState(0);
  const [pushContactEmail, setPushContactEmail] = useState("");
  const [savedPushContactEmail, setSavedPushContactEmail] = useState("");
  const [pushEmailLoaded, setPushEmailLoaded] = useState(false);
  const [pushEmailSaving, setPushEmailSaving] = useState(false);
  const [pushEmailError, setPushEmailError] = useState(false);

  // The armed bit for the main-menu subtitle. Read from the PUBLIC probe, which
  // needs no token and is the same source the login wall branches on, so the
  // row cannot disagree with the wall. Left null on failure — the subtitle then
  // says nothing rather than guessing.
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    // The owner-gated state route, not the public probe: it carries the rollout
    // flag too, and the flag is not something to publish to unauthenticated
    // callers.
    api
      .getMfaState()
      .then((state) => {
        if (cancelled) return;
        setMfaEnrolled(state.enrolled);
        setMfaOffered(state.offered);
      })
      .catch(() => {
        if (cancelled) return;
        setMfaEnrolled(null);
        setMfaOffered(null);
      });
    return () => {
      cancelled = true;
    };
  }, [open]);

  // Reset transient view state whenever the menu is (re)opened.
  useEffect(() => {
    if (open) setView("main");
  }, [open]);

  // 🔴 THE QR IS AN ENHANCEMENT, AND MUST FAIL LIKE ONE. It is computed here
  // rather than inline in the JSX because a throw during render unmounts the
  // whole dropdown — taking the setup key, the otpauth link AND the disable
  // button with it, which is the opposite of what an owner needs at that moment.
  //
  // It can genuinely throw: the issuer/account baked into the URI come from the
  // owner-settable org name and nickname, so a long enough studio name pushes
  // the payload past what a QR symbol can hold. When that happens the primary
  // path — the copyable key and the otpauth link — is still right there.
  //
  // It MUST stay above the `if (!open)` gate below: a hook after an early
  // return changes the hook COUNT between the closed and open renders, which
  // React rejects outright ("Rendered more hooks than during the previous
  // render") and which crashes the very dropdown this memo exists to protect.
  const mfaQr = useMemo(() => {
    if (!mfaPending?.otpauthUri) return null;
    try {
      return qrSvg(mfaPending.otpauthUri, { size: 176, title: t.profile.mfaQrAlt });
    } catch {
      return null;
    }
  }, [mfaPending?.otpauthUri, t.profile.mfaQrAlt]);

  if (!open) return null;

  function handleLogout() {
    // Resets local preferences to their initial state (theme/language). The
    // owner nickname is server-backed now (T-0b41) and is deliberately left in
    // place — logout is not a place to silently wipe server-side identity.
    // In real-backend mode onLogout (AuthGate) also clears the owner token and
    // returns to the login wall — an honest sign-out. In mock mode there is no
    // token/session, so onLogout keeps the app mounted (pref-reset only).
    resetPreferences();
    onClose();
    onLogout?.();
  }

  function openPasswordView() {
    setCurrentPwd("");
    setNewPwd("");
    setConfirmPwd("");
    setPwdError("");
    setPwdDone(false);
    setPwdBusy(false);
    setView("password");
  }

  /**
   * Turn a rejection from a credentialPost seam into the right error key.
   *
   * 🔴 THE 401 IS AMBIGUOUS ON THESE TWO ENDPOINTS, and that ambiguity was a
   * trap. activateMfa / disableMfa deliberately ride `credentialPost`, which
   * skips the auth-expiry middleware so a WRONG CREDENTIAL stays an inline error
   * instead of bouncing the whole app to the login wall. The cost is that a 401
   * from the auth GATE — an owner token that expired while the form sat open —
   * looks identical, and used to render as "that code is wrong", forever, for a
   * code that was perfectly correct. Only a page reload escaped, and nothing on
   * screen suggested it.
   *
   * Rather than sniff the server's message text (which the wire deliberately
   * keeps identical for both credential halves, and which is localisable), it
   * asks a cheap GATED question through the TYPED client. If the session really
   * is dead that call 401s and the client's own middleware fires
   * `oc-auth-expired`, which bounces the app — so the honest outcome happens by
   * itself and we only need a message for the moment before it lands.
   */
  async function classifyCredentialError(
    err: unknown,
    credentialKey: "code" | "disable",
  ): Promise<"code" | "disable" | "throttled" | "session"> {
    // The brake, not a credential: it says nothing about what was submitted.
    if (isHttpStatus(err, 429)) {
      setMfaRetryAfter(retryAfterSeconds(err));
      return "throttled";
    }
    if (isHttpStatus(err, 401)) {
      try {
        await loadServerSettings(); // typed client: 401 here bounces the app
      } catch (probe) {
        if (isHttpStatus(probe, 401)) return "session";
      }
    }
    return credentialKey;
  }

  async function handleMfaOffer(next: boolean) {
    if (mfaBusy) return;
    setMfaBusy(true);
    setMfaError("");
    try {
      const state = await api.setMfaOffered(next);
      setMfaOffered(state.offered);
      setMfaEnrolled(state.enrolled);
    } catch {
      setMfaError("offer");
    } finally {
      setMfaBusy(false);
    }
  }

  async function openMfaView() {
    setMfaPending(null);
    setMfaCode("");
    setMfaPwd("");
    setMfaRetryAfter(0);
    setMfaBusy(false);
    setMfaNotice("");
    setMfaError("");
    setMfaLoaded(false);
    setView("mfa");
    try {
      const state = await api.getMfaState();
      setMfaEnrolled(state.enrolled);
      setMfaOffered(state.offered);
      setMfaLoaded(true);
    } catch {
      // HONEST: no guess. Without knowing the current state this view cannot
      // offer the right action, so it says so instead of showing a wrong one.
      setMfaLoaded(false);
      setMfaError("load");
    }
  }

  async function handleMfaEnroll() {
    if (mfaBusy) return;
    setMfaBusy(true);
    setMfaError("");
    try {
      setMfaPending(await api.enrollMfa());
      setMfaCode("");
    } catch {
      setMfaError("load");
    } finally {
      setMfaBusy(false);
    }
  }

  async function handleMfaActivate(e: FormEvent) {
    e.preventDefault();
    if (mfaBusy || !mfaCode || !mfaPwd) return;
    setMfaBusy(true);
    setMfaError("");
    try {
      await api.activateMfa(mfaPwd, mfaCode);
      // The secret and the password have served their only purpose — drop both
      // from memory the instant the factor is armed.
      setMfaPending(null);
      setMfaCode("");
      setMfaPwd("");
      setMfaEnrolled(true);
      setMfaNotice("activated");
    } catch (err) {
      setMfaError(await classifyCredentialError(err, "code"));
    } finally {
      setMfaBusy(false);
    }
  }

  async function handleMfaDisable(e: FormEvent) {
    e.preventDefault();
    if (mfaBusy || !mfaPwd || !mfaCode) return;
    setMfaBusy(true);
    setMfaError("");
    try {
      await api.disableMfa(mfaPwd, mfaCode);
      setMfaEnrolled(false);
      setMfaNotice("disabled");
      setMfaPwd("");
      setMfaCode("");
    } catch (err) {
      setMfaError(await classifyCredentialError(err, "disable"));
    } finally {
      setMfaBusy(false);
    }
  }

  async function loadPushContactEmail() {
    setPushEmailError(false);
    setPushEmailLoaded(false);
    setPushEmailSaving(false);
    try {
      const settings = await loadServerSettings();
      setPushContactEmail(settings.pushContactEmail);
      setSavedPushContactEmail(settings.pushContactEmail);
      setPushEmailLoaded(true);
    } catch {
      setPushEmailLoaded(false);
      setPushEmailError(true);
    }
  }

  function openPreferences() {
    setView("preferences");
  }

  function openNotifications() {
    setView("notifications");
    void loadPushContactEmail();
  }

  async function commitPushContactEmail() {
    if (!pushEmailLoaded || pushEmailSaving || pushContactEmail === savedPushContactEmail) return;
    setPushEmailError(false);
    setPushEmailSaving(true);
    try {
      const settings = await api.patchServerSettings({ pushContactEmail });
      adoptServerSettings(settings); // shared snapshot invalidation point (T-8115)
      setPushContactEmail(settings.pushContactEmail);
      setSavedPushContactEmail(settings.pushContactEmail);
    } catch {
      setPushEmailError(true);
    } finally {
      setPushEmailSaving(false);
    }
  }

  async function handleChangePassword(e: FormEvent) {
    e.preventDefault();
    if (pwdBusy || !currentPwd || !newPwd || !confirmPwd) return;
    if (newPwd.length < 8) {
      setPwdError("short");
      return;
    }
    if (newPwd !== confirmPwd) {
      setPwdError("mismatch");
      return;
    }
    setPwdBusy(true);
    setPwdError("");
    try {
      await api.changePassword(currentPwd, newPwd);
      setPwdDone(true);
      setCurrentPwd("");
      setNewPwd("");
      setConfirmPwd("");
    } catch (err) {
      // 429 is the shared in-flight cap — this endpoint and login draw on the
      // same pool of concurrent credential verifications. It is NOT a failure
      // count: there is no such counter any more, so fumbling a few logins can
      // never produce this. Without this branch it renders as "that current
      // password is wrong", which would tell an owner their CORRECT password is
      // wrong. The wait is a moment (Retry-After is 1s), not minutes.
      if (isHttpStatus(err, 429)) {
        setPwdRetryAfter(retryAfterSeconds(err));
        setPwdError("throttled");
      } else {
        setPwdError(isHttpStatus(err, 422) ? "short" : "current");
      }
    } finally {
      setPwdBusy(false);
    }
  }

  return (
    <div className="profile-dd" role="menu">
      {view === "main" && (
        <>
          {/* profile header — inline rename (Enter save / Esc cancel) */}
          <div className="profile-dd__head">
            <span className="profile-dd__avatar">
              <UserIcon size={18} />
            </span>
            <div className="profile-dd__ident">
              <span className="profile-dd__label">{t.profile.title}</span>
              <InlineEdit
                value={userName}
                onCommit={setOwnerName}
                placeholder={t.profile.renamePlaceholder}
                ariaLabel={t.profile.rename}
                displayClassName="profile-dd__name"
              />
            </div>
          </div>

          {/* preferences row → sub-view */}
          <button
            type="button"
            className="profile-dd__row"
            onClick={openPreferences}
          >
            <span className="profile-dd__row-icon">
              <GearIcon size={16} />
            </span>
            <span className="profile-dd__row-body">
              <span className="profile-dd__row-title">
                {t.profile.preferences}
              </span>
              <span className="profile-dd__row-sub">
                {t.profile.preferencesSub}
              </span>
            </span>
            <ChevronRightIcon size={16} className="profile-dd__row-chevron" />
          </button>

          <button type="button" className="profile-dd__row" onClick={openNotifications}>
            <span className="profile-dd__row-icon"><BellIcon size={16} /></span>
            <span className="profile-dd__row-body"><span className="profile-dd__row-title">{t.profile.pushContactEmail}</span><span className="profile-dd__row-sub">{t.profile.pushContactEmailSub}</span></span>
            <ChevronRightIcon size={16} className="profile-dd__row-chevron" />
          </button>

          <button type="button" className="profile-dd__row" onClick={openPasswordView}>
            <span className="profile-dd__row-icon"><GearIcon size={16} /></span>
            <span className="profile-dd__row-body"><span className="profile-dd__row-title">{t.profile.changePassword}</span><span className="profile-dd__row-sub">{t.profile.changePasswordSub}</span></span>
            <ChevronRightIcon size={16} className="profile-dd__row-chevron" />
          </button>

          <button type="button" className="profile-dd__row" onClick={openMfaView}>
            <span className="profile-dd__row-icon"><GearIcon size={16} /></span>
            <span className="profile-dd__row-body"><span className="profile-dd__row-title">{t.profile.mfa}</span><span className="profile-dd__row-sub">{mfaEnrolled === null || mfaOffered === null ? "" : mfaEnrolled ? t.profile.mfaSubOn : mfaOffered ? t.profile.mfaSubOff : t.profile.mfaSubUnavailable}</span></span>
            <ChevronRightIcon size={16} className="profile-dd__row-chevron" />
          </button>

          <div className="profile-dd__divider" />

          {/* logout (honest: local-only reset in M1) */}
          <button
            type="button"
            className="profile-dd__row profile-dd__row--danger"
            onClick={handleLogout}
          >
            <span className="profile-dd__row-icon">
              <LogOutIcon size={16} />
            </span>
            <span className="profile-dd__row-title">{t.profile.logout}</span>
          </button>
        </>
      )}

      {view === "preferences" && (
        <>
          {/* preferences sub-view */}
          <button
            type="button"
            className="profile-dd__back"
            onClick={() => setView("main")}
          >
            <ChevronLeftIcon size={16} />
            <span>{t.profile.back}</span>
          </button>

          <div className="profile-dd__section">
            <div className="profile-dd__section-head">
              <div className="profile-dd__section-label">{t.profile.theme}</div>
              {/* Manage lives in 設定/主題 now — this hint points there rather
               * than carrying import/export/edit chips in the quick menu. */}
              <div className="profile-dd__section-hint">
                {t.profile.themeManageHint}
              </div>
            </div>

            {/* A flat list, built-ins first (owner 2026-07-27: 「下拉式選單不用
              * 使用分區」/「就算真的沒有顯示內建或自訂也沒關係,只要設定有標示出來
              * 就好」). 內建 / 自訂 is shown in 設定 › 主題 only — this quick picker
              * is a plain ordered list.
              *
              * What must NOT come back is a TEXT marker on the option itself: a
              * pack naming itself 「辦公室(內建)」 then puts a second,
              * byte-identical built-in-looking row here (T-081b review round 3,
              * BLOCKER-2). Each option's text is the theme's own name and
              * nothing else; the only thing this picker asserts is ORDER, and
              * order comes from the rendering below — the built-in is written
              * out first, the packs follow — so no field of a bundle can move a
              * row ahead of the built-in. */}
            <select className="profile-dd__input" aria-label={t.profile.theme} value={theme} onChange={(e) => setTheme(e.target.value)}>
              <option value="office">{t.themeIdentity.office}</option>
              {themeList.map((b) => <option key={b.id} value={b.id}>{b.name}</option>)}
            </select>
          </div>

          <div className="profile-dd__section">
            <div className="profile-dd__section-label">
              {t.profile.language}
            </div>
            <div className="profile-dd__seg">
              <button
                type="button"
                className={`profile-dd__seg-btn${
                  language === "zh" ? " profile-dd__seg-btn--active" : ""
                }`}
                onClick={() => setLanguage("zh")}
              >
                {t.profile.langZh}
              </button>
              <button
                type="button"
                className={`profile-dd__seg-btn${
                  language === "en" ? " profile-dd__seg-btn--active" : ""
                }`}
                onClick={() => setLanguage("en")}
              >
                {t.profile.langEn}
              </button>
            </div>
          </div>

          <div className="profile-dd__section">
            <div className="profile-dd__section-label">{t.profile.layout}</div>
            <div className="profile-dd__seg">
              <button
                type="button"
                className={`profile-dd__seg-btn${
                  !wide ? " profile-dd__seg-btn--active" : ""
                }`}
                onClick={() => setWide(false)}
              >
                {t.profile.layoutNarrow}
              </button>
              <button
                type="button"
                className={`profile-dd__seg-btn${
                  wide ? " profile-dd__seg-btn--active" : ""
                }`}
                onClick={() => setWide(true)}
              >
                {t.profile.layoutWide}
              </button>
            </div>
          </div>

        </>
      )}

      {view === "mfa" && (
        <>
          <button
            type="button"
            className="profile-dd__back"
            onClick={() => setView("main")}
          >
            <ChevronLeftIcon size={16} />
            <span>{t.profile.mfa}</span>
          </button>

          {/* HONEST: the probe failed, so we do not know the current state and
              will not guess at an action that might be the wrong one. Its own
              sentence, not the wrong-code one — no code was ever submitted on
              this path — plus the retry that is otherwise missing (the only way
              out was Back-then-reopen, which nothing on screen suggested). */}
          {mfaError === "load" && !mfaPending && (
            <div className="profile-dd__form">
              <div className="profile-dd__error">{t.profile.mfaErrorLoad}</div>
              <button
                type="button"
                className="profile-dd__submit"
                disabled={mfaBusy}
                onClick={openMfaView}
              >
                {t.profile.mfaRetry}
              </button>
            </div>
          )}
          {mfaError === "throttled" && (
            <div className="profile-dd__error">
              {msg.loginThrottled(mfaRetryAfter)}
            </div>
          )}

          {mfaNotice === "activated" && (
            <div className="profile-dd__success">{t.profile.mfaActivated}</div>
          )}
          {mfaNotice === "disabled" && (
            <div className="profile-dd__success">{t.profile.mfaDisabled}</div>
          )}

          {/* ── OFF, nothing pending: offer to start ── */}
          {/* Feature withdrawn AND nothing armed: the only thing on offer is the
              rollout switch itself. */}
          {mfaLoaded && mfaOffered === false && mfaEnrolled === false && (
            <div className="profile-dd__form">
              <div className="profile-dd__hint">{t.profile.mfaOfferIntro}</div>
              {mfaError === "offer" && (
                <div className="profile-dd__error">{t.profile.mfaErrorOffer}</div>
              )}
              <button
                type="button"
                className="profile-dd__submit"
                disabled={mfaBusy}
                onClick={() => handleMfaOffer(true)}
              >
                {mfaBusy ? t.profile.mfaEnrollStarting : t.profile.mfaOfferOn}
              </button>
            </div>
          )}

          {mfaLoaded && mfaOffered === true && mfaEnrolled === false && !mfaPending && (
            <div className="profile-dd__form">
              <div className="profile-dd__hint">{t.profile.mfaIntro}</div>
              <button
                type="button"
                className="profile-dd__submit"
                disabled={mfaBusy}
                onClick={handleMfaEnroll}
              >
                {mfaBusy ? t.profile.mfaEnrollStarting : t.profile.mfaEnrollStart}
              </button>
              <button
                type="button"
                className="profile-dd__submit profile-dd__submit--danger"
                disabled={mfaBusy}
                onClick={() => handleMfaOffer(false)}
              >
                {t.profile.mfaOfferOff}
              </button>
              <div className="profile-dd__hint">{t.profile.mfaOfferOffHint}</div>
            </div>
          )}

          {/* ── PENDING: show the secret once, take a code to prove it ── */}
          {mfaPending && (
            <form className="profile-dd__form" onSubmit={handleMfaActivate}>
              {/* The QR is built from the SAME otpauth URI shown below, in the
                  browser — nothing is fetched and the secret never leaves the
                  page. dangerouslySetInnerHTML is safe here because qrSvg emits
                  a fixed shape from a fixed template and escapes its one
                  interpolated attribute; there is no caller-supplied markup.
                  When it could not be built, the key and link below carry the
                  whole flow on their own — so the hint changes with it rather
                  than pointing at a picture that is not there. */}
              {mfaQr && (
                <>
                  <div className="profile-dd__hint">
                    {t.profile.mfaScanQrHint}
                  </div>
                  <div
                    className="profile-dd__qr"
                    dangerouslySetInnerHTML={{ __html: mfaQr }}
                  />
                </>
              )}
              <div className="profile-dd__hint">{t.profile.mfaScanHint}</div>
              {/* The secret is ALSO shown as selectable text, beside the QR
                  above. Three routes into an authenticator — scan, tap the
                  otpauth link, or type the key — because each fails in a
                  different place: the QR needs a second camera-equipped device,
                  the link needs the cockpit to be open ON that device, and the
                  key needs neither but is the most error-prone to transcribe.
                  The key is also the fallback when the QR cannot be built at
                  all (see mfaQr). */}
              <div className="profile-dd__hint">
                {t.profile.mfaSecretLabel}
              </div>
              <code className="profile-dd__code">{mfaPending.secret}</code>
              <a
                className="profile-dd__link"
                href={mfaPending.otpauthUri}
                rel="noreferrer"
              >
                {t.profile.mfaOpenInApp}
              </a>
              <div className="profile-dd__hint">{t.profile.mfaActivateHint}</div>
              <input
                className="profile-dd__input"
                type="password"
                autoComplete="current-password"
                placeholder={t.profile.currentPasswordPlaceholder}
                aria-label={t.profile.currentPasswordPlaceholder}
                value={mfaPwd}
                disabled={mfaBusy}
                onChange={(e) => {
                  setMfaPwd(e.target.value);
                  setMfaError("");
                }}
              />
              <input
                className="profile-dd__input"
                type="text"
                autoComplete="one-time-code"
                inputMode="numeric"
                placeholder={t.profile.mfaCodePlaceholder}
                aria-label={t.profile.mfaCodePlaceholder}
                value={mfaCode}
                disabled={mfaBusy}
                onChange={(e) => {
                  setMfaCode(e.target.value);
                  setMfaError("");
                }}
              />
              {mfaError === "code" && (
                <div className="profile-dd__error">
                  {t.profile.mfaErrorActivate}
                </div>
              )}
              {mfaError === "throttled" && (
                <div className="profile-dd__error">
                  {msg.loginThrottled(mfaRetryAfter)}
                </div>
              )}
              {mfaError === "session" && (
                <div className="profile-dd__error">
                  {t.profile.mfaErrorSession}
                </div>
              )}
              <button
                type="submit"
                className="profile-dd__submit"
                disabled={mfaBusy || !mfaCode || !mfaPwd}
              >
                {mfaBusy ? t.profile.mfaActivating : t.profile.mfaActivate}
              </button>
            </form>
          )}

          {/* ── ON: disarming needs BOTH the password and a live code ── */}
          {mfaLoaded && mfaEnrolled === true && !mfaPending && (
            <form className="profile-dd__form" onSubmit={handleMfaDisable}>
              <div className="profile-dd__hint">{t.profile.mfaDisableHint}</div>
              <input
                className="profile-dd__input"
                type="password"
                autoComplete="current-password"
                placeholder={t.profile.currentPasswordPlaceholder}
                aria-label={t.profile.currentPasswordPlaceholder}
                value={mfaPwd}
                disabled={mfaBusy}
                onChange={(e) => {
                  setMfaPwd(e.target.value);
                  setMfaError("");
                }}
              />
              <input
                className="profile-dd__input"
                type="text"
                autoComplete="one-time-code"
                inputMode="numeric"
                placeholder={t.profile.mfaCodePlaceholder}
                aria-label={t.profile.mfaCodePlaceholder}
                value={mfaCode}
                disabled={mfaBusy}
                onChange={(e) => {
                  setMfaCode(e.target.value);
                  setMfaError("");
                }}
              />
              {mfaError === "disable" && (
                <div className="profile-dd__error">
                  {t.profile.mfaErrorDisable}
                </div>
              )}
              {mfaError === "session" && (
                <div className="profile-dd__error">
                  {t.profile.mfaErrorSession}
                </div>
              )}
              <button
                type="submit"
                className="profile-dd__submit profile-dd__submit--danger"
                disabled={mfaBusy || !mfaPwd || !mfaCode}
              >
                {mfaBusy ? t.profile.mfaDisabling : t.profile.mfaDisable}
              </button>
            </form>
          )}
        </>
      )}

      {view === "password" && (
        <>
          <button
            type="button"
            className="profile-dd__back"
            onClick={() => setView("main")}
          >
            <ChevronLeftIcon size={16} />
            <span>{t.profile.changePassword}</span>
          </button>

          <form className="profile-dd__form" onSubmit={handleChangePassword}>
            <input
              className="profile-dd__input"
              type="password"
              autoComplete="current-password"
              placeholder={t.profile.currentPasswordPlaceholder}
              aria-label={t.profile.currentPasswordPlaceholder}
              value={currentPwd}
              disabled={pwdBusy}
              onChange={(e) => {
                setCurrentPwd(e.target.value);
                setPwdError("");
                setPwdDone(false);
              }}
            />
            <input
              className="profile-dd__input"
              type="password"
              autoComplete="new-password"
              placeholder={t.profile.newPasswordPlaceholder}
              aria-label={t.profile.newPasswordPlaceholder}
              value={newPwd}
              disabled={pwdBusy}
              onChange={(e) => {
                setNewPwd(e.target.value);
                setPwdError("");
                setPwdDone(false);
              }}
            />
            <input
              className="profile-dd__input"
              type="password"
              autoComplete="new-password"
              placeholder={t.profile.confirmPasswordPlaceholder}
              aria-label={t.profile.confirmPasswordPlaceholder}
              value={confirmPwd}
              disabled={pwdBusy}
              onChange={(e) => {
                setConfirmPwd(e.target.value);
                setPwdError("");
                setPwdDone(false);
              }}
            />

            {pwdError && (
              <div className="profile-dd__error">
                {
                  {
                    current: t.profile.pwdErrorCurrent,
                    short: t.profile.pwdErrorTooShort,
                    mismatch: t.profile.pwdErrorMismatch,
                    throttled: msg.loginThrottled(pwdRetryAfter),
                  }[pwdError]
                }
              </div>
            )}
            {pwdDone && (
              <div className="profile-dd__success">{t.profile.pwdChanged}</div>
            )}

            <button
              type="submit"
              className="profile-dd__submit"
              disabled={pwdBusy || !currentPwd || !newPwd || !confirmPwd}
            >
              {pwdBusy ? t.profile.saving : t.profile.save}
            </button>
          </form>
        </>
      )}

      {view === "notifications" && (
        <>
          <button type="button" className="profile-dd__back" onClick={() => setView("main")}><ChevronLeftIcon size={16} /><span>{t.profile.pushContactEmail}</span></button>
          <form className="profile-dd__form" onSubmit={(e) => { e.preventDefault(); void commitPushContactEmail(); }}>
            <label className="profile-dd__field-label" htmlFor="push-contact-email">{t.profile.pushContactEmail}</label>
            <div className="profile-dd__section-hint">{t.profile.pushContactEmailSub}</div>
            <input id="push-contact-email" className="profile-dd__input" type="email" inputMode="email" autoComplete="email" placeholder={t.profile.pushContactEmailPlaceholder} value={pushContactEmail} disabled={!pushEmailLoaded} onChange={(e) => setPushContactEmail(e.target.value)} />
            {pushEmailError && <div className="profile-dd__error">{t.profile.pushContactEmailError}</div>}
            <button type="submit" className="profile-dd__submit" disabled={!pushEmailLoaded || pushEmailSaving || pushContactEmail === savedPushContactEmail}>{pushEmailSaving ? t.profile.saving : t.profile.save}</button>
          </form>
        </>
      )}
    </div>
  );
}
