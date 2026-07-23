import { useEffect, useState, type FormEvent } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { isHttpStatus } from "../api/errors";
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  GearIcon,
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

type View = "main" | "preferences" | "password";

/**
 * Profile menu that drops from the topbar profile pill.
 *  - main view: profile header (inline rename), Preferences row, Log out.
 *  - preferences view: Theme SELECTOR (辦公室 / 修仙 / custom) + Language
 *    (中文 / English), then a 修改密碼 row.
 *  - password view: current / new / repeat → POST /api/auth/change-password.
 *
 * Scope (owner 2026-07-12): this menu holds APPEARANCE + ACCOUNT IDENTITY only.
 * T-16a1 P3b narrowed 外觀 further: this dropdown now only SELECTS a theme; all
 * theme MANAGEMENT (add / edit colours / 用詞 / import / export / delete + 修仙
 * dogfood) moved to the 設定 page's 主題 sub-section (SettingsPage → ThemeSettings)
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
    customThemes,
    language,
    setLanguage,
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
            onClick={() => setView("preferences")}
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

            <ul className="profile-dd__theme-list">
              <li className="profile-dd__theme-row">
                <button
                  type="button"
                  className={`profile-dd__theme-pick${
                    theme === "office"
                      ? " profile-dd__theme-pick--active"
                      : ""
                  }`}
                  onClick={() => setTheme("office")}
                >
                  {t.profile.themeOffice}
                </button>
              </li>
              <li className="profile-dd__theme-row">
                <button
                  type="button"
                  className={`profile-dd__theme-pick${
                    theme === "xian" ? " profile-dd__theme-pick--active" : ""
                  }`}
                  onClick={() => setTheme("xian")}
                >
                  {t.profile.themeXian}
                </button>
              </li>

              {customThemes.map((b) => (
                <li key={b.id} className="profile-dd__theme-row">
                  <button
                    type="button"
                    className={`profile-dd__theme-pick${
                      theme === b.id ? " profile-dd__theme-pick--active" : ""
                    }`}
                    onClick={() => setTheme(b.id)}
                  >
                    {b.name}
                  </button>
                </li>
              ))}
            </ul>
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

          <div className="profile-dd__divider" />

          <button
            type="button"
            className="profile-dd__row"
            onClick={openPasswordView}
          >
            <span className="profile-dd__row-body">
              <span className="profile-dd__row-title">
                {t.profile.changePassword}
              </span>
              <span className="profile-dd__row-sub">
                {t.profile.changePasswordSub}
              </span>
            </span>
            <ChevronRightIcon
              size={16}
              className="profile-dd__row-chevron"
            />
          </button>
        </>
      )}

      {view === "password" && (
        <>
          <button
            type="button"
            className="profile-dd__back"
            onClick={() => setView("preferences")}
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
    </div>
  );
}
