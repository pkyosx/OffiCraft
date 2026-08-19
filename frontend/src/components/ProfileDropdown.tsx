import { useEffect, useState, type FormEvent } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { isHttpStatus } from "../api/errors";
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

type View = "main" | "preferences" | "password" | "notifications";

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
 * server PARAMETER knobs (登入有效期 / 自動換手門檻) likewise live in 設定/參數調整.
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
    resetPreferences,
  } = useI18n();

  const [view, setView] = useState<View>("main");

  // ── change-password form state ────────────────────────────────────────────
  const [currentPwd, setCurrentPwd] = useState("");
  const [newPwd, setNewPwd] = useState("");
  const [confirmPwd, setConfirmPwd] = useState("");
  const [pwdBusy, setPwdBusy] = useState(false);
  const [pwdDone, setPwdDone] = useState(false);
  const [pwdError, setPwdError] = useState<
    "" | "current" | "short" | "mismatch"
  >("");
  const [pushContactEmail, setPushContactEmail] = useState("");
  const [savedPushContactEmail, setSavedPushContactEmail] = useState("");
  const [pushEmailLoaded, setPushEmailLoaded] = useState(false);
  const [pushEmailSaving, setPushEmailSaving] = useState(false);
  const [pushEmailError, setPushEmailError] = useState(false);

  // Reset transient view state whenever the menu is (re)opened.
  useEffect(() => {
    if (open) setView("main");
  }, [open]);

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
      setPwdError(isHttpStatus(err, 422) ? "short" : "current");
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
