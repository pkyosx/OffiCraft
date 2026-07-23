import { useEffect, useRef, useState, type FormEvent } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { isHttpStatus } from "../api/errors";
import {
  MAX_CUSTOM_THEMES,
  validateThemeBundle,
  type ThemeBundle,
} from "../lib/themeBundle";
import {
  bundleFilename,
  exportBuiltinTheme,
  exportComputedTheme,
  parseImportedBundle,
  serializeBundle,
} from "../lib/themeExport";
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

type View = "main" | "preferences" | "password" | "themeImport" | "themeEdit";

/**
 * Profile menu that drops from the topbar profile pill.
 *  - main view: profile header (inline rename), Preferences row, Log out.
 *  - preferences view: Theme (辦公室 / 修仙) + Language (中文 / English) segmented,
 *    then a 修改密碼 row.
 *  - password view: current / new / repeat → POST /api/auth/change-password.
 *
 * Scope (owner 2026-07-12): this menu holds APPEARANCE + ACCOUNT IDENTITY only.
 * The server PARAMETER knobs (登入有效期 / 自動換手門檻) that used to render here
 * now live in the 設定 page's 參數調整 entry (SettingsPage + useServerSettings),
 * so every parameter is tuned in one place. Nothing about the wire changed —
 * /api/settings is the same endpoint, just called from the page instead.
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
    commitCustomThemes,
    language,
    setLanguage,
    resetPreferences,
  } = useI18n();

  const [view, setView] = useState<View>("main");

  // ── theme import / edit state ──────────────────────────────────────────────
  const [importText, setImportText] = useState("");
  const [importError, setImportError] = useState("");
  const [editId, setEditId] = useState<string | null>(null);
  const [editName, setEditName] = useState("");
  const [editColors, setEditColors] = useState<[string, string][]>([]);
  const [editError, setEditError] = useState("");
  const [copied, setCopied] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

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

  // ── theme picker helpers ───────────────────────────────────────────────────

  // id + display name to stamp on an exported bundle. A built-in cannot reuse
  // its reserved id ("office"/"xian"), so it exports under a "-copy" id that
  // re-imports as an editable custom theme.
  function currentThemeMeta(): { id: string; name: string } {
    if (theme === "office")
      return { id: "office-copy", name: t.profile.themeOffice };
    if (theme === "xian")
      return { id: "xian-copy", name: t.profile.themeXian };
    const b = customThemes.find((x) => x.id === theme);
    return { id: b?.id ?? "theme", name: b?.name ?? theme };
  }

  function downloadBundle(bundle: ThemeBundle) {
    const blob = new Blob([serializeBundle(bundle)], {
      type: "application/json",
    });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = bundleFilename(bundle);
    a.click();
    URL.revokeObjectURL(url);
  }

  function handleExportCurrent() {
    const { id, name } = currentThemeMeta();
    downloadBundle(exportComputedTheme(id, name));
  }

  async function handleCopyCurrent() {
    const { id, name } = currentThemeMeta();
    try {
      await navigator.clipboard.writeText(
        serializeBundle(exportComputedTheme(id, name))
      );
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // clipboard blocked — the download button remains the reliable path
    }
  }

  function openImport() {
    setImportText("");
    setImportError("");
    setView("themeImport");
  }

  function addBundle(bundle: ThemeBundle): string | null {
    if (customThemes.some((b) => b.id === bundle.id)) {
      return t.profile.themeImportDup;
    }
    if (customThemes.length >= MAX_CUSTOM_THEMES) {
      return t.profile.themeLimitReached;
    }
    commitCustomThemes([...customThemes, bundle]);
    return null;
  }

  function handleConfirmImport() {
    const res = parseImportedBundle(importText);
    if ("error" in res) {
      setImportError(res.error);
      return;
    }
    const err = addBundle(res.bundle);
    if (err) {
      setImportError(err);
      return;
    }
    setView("preferences");
  }

  async function handleFilePicked(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    try {
      setImportText(await file.text());
      setImportError("");
    } catch {
      setImportError(t.profile.themeImportReadFailed);
    }
  }

  // 修仙 dogfood: build the example by EXPORTING the shipped 修仙 built-in, then
  // running it back through the import path — proving the export→import loop
  // against a real theme. The example is an editable custom copy; the built-in
  // 修仙 entry stays as-is.
  function handleImportXianExample() {
    const bundle = exportBuiltinTheme(
      "xian",
      "xian-example",
      t.profile.themeExampleName
    );
    const parsed = parseImportedBundle(serializeBundle(bundle));
    if ("error" in parsed) {
      setImportError(parsed.error);
      setView("themeImport");
      return;
    }
    const err = addBundle(parsed.bundle);
    if (err) {
      setImportError(err);
      setView("themeImport");
    }
  }

  function handleDeleteTheme(id: string) {
    const next = customThemes.filter((b) => b.id !== id);
    // Deleting the active theme drops back to the office base; the same PATCH
    // carries the reset so the server's dangling-guard agrees.
    commitCustomThemes(next, theme === id ? "office" : undefined);
  }

  function openEdit(bundle: ThemeBundle) {
    setEditId(bundle.id);
    setEditName(bundle.name);
    setEditColors(Object.entries(bundle.colors));
    setEditError("");
    setView("themeEdit");
  }

  function handleSaveEdit() {
    if (editId == null) return;
    const colors: Record<string, string> = {};
    for (const [tok, val] of editColors) colors[tok] = val;
    const bundle: ThemeBundle = { id: editId, name: editName, colors };
    const err = validateThemeBundle(bundle);
    if (err) {
      setEditError(err);
      return;
    }
    commitCustomThemes(
      customThemes.map((b) => (b.id === editId ? bundle : b))
    );
    setView("preferences");
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
              <div className="profile-dd__theme-actions">
                <button
                  type="button"
                  className="profile-dd__chip"
                  onClick={openImport}
                >
                  {t.profile.themeImport}
                </button>
                <button
                  type="button"
                  className="profile-dd__chip"
                  onClick={handleExportCurrent}
                >
                  {t.profile.themeExport}
                </button>
                <button
                  type="button"
                  className="profile-dd__chip"
                  onClick={handleCopyCurrent}
                >
                  {copied ? t.profile.themeCopied : t.profile.themeCopy}
                </button>
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
                  <button
                    type="button"
                    className="profile-dd__theme-mini"
                    aria-label={t.profile.themeEdit}
                    title={t.profile.themeEdit}
                    onClick={() => openEdit(b)}
                  >
                    {t.profile.themeEdit}
                  </button>
                  <button
                    type="button"
                    className="profile-dd__theme-mini profile-dd__theme-mini--danger"
                    aria-label={t.profile.themeDelete}
                    title={t.profile.themeDelete}
                    onClick={() => handleDeleteTheme(b.id)}
                  >
                    {t.profile.themeDelete}
                  </button>
                </li>
              ))}
            </ul>

            {!customThemes.some((b) => b.id === "xian-example") && (
              <button
                type="button"
                className="profile-dd__theme-example"
                onClick={handleImportXianExample}
              >
                {t.profile.themeExampleImport}
              </button>
            )}
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

      {view === "themeImport" && (
        <>
          <button
            type="button"
            className="profile-dd__back"
            onClick={() => setView("preferences")}
          >
            <ChevronLeftIcon size={16} />
            <span>{t.profile.themeImportTitle}</span>
          </button>

          <div className="profile-dd__form">
            <textarea
              className="profile-dd__textarea"
              placeholder={t.profile.themeImportPlaceholder}
              aria-label={t.profile.themeImportTitle}
              value={importText}
              onChange={(e) => {
                setImportText(e.target.value);
                setImportError("");
              }}
            />

            <input
              ref={fileInputRef}
              type="file"
              accept="application/json,.json"
              className="profile-dd__file"
              onChange={handleFilePicked}
            />
            <button
              type="button"
              className="profile-dd__chip profile-dd__chip--block"
              onClick={() => fileInputRef.current?.click()}
            >
              {t.profile.themeChooseFile}
            </button>

            {importError && (
              <div className="profile-dd__error">{importError}</div>
            )}

            <button
              type="button"
              className="profile-dd__submit"
              disabled={!importText.trim()}
              onClick={handleConfirmImport}
            >
              {t.profile.themeConfirmImport}
            </button>
          </div>
        </>
      )}

      {view === "themeEdit" && (
        <>
          <button
            type="button"
            className="profile-dd__back"
            onClick={() => setView("preferences")}
          >
            <ChevronLeftIcon size={16} />
            <span>{t.profile.themeEditTitle}</span>
          </button>

          <div className="profile-dd__form">
            <label className="profile-dd__field-label">
              {t.profile.themeNameLabel}
            </label>
            <input
              className="profile-dd__input"
              value={editName}
              aria-label={t.profile.themeNameLabel}
              onChange={(e) => {
                setEditName(e.target.value);
                setEditError("");
              }}
            />

            <div className="profile-dd__color-list">
              {editColors.map(([token, value], i) => (
                <div key={token} className="profile-dd__color-row">
                  <span className="profile-dd__color-token" title={token}>
                    {token}
                  </span>
                  <input
                    className="profile-dd__input profile-dd__color-input"
                    value={value}
                    aria-label={token}
                    onChange={(e) => {
                      const next = editColors.slice();
                      next[i] = [token, e.target.value];
                      setEditColors(next);
                      setEditError("");
                    }}
                  />
                </div>
              ))}
            </div>

            {editError && (
              <div className="profile-dd__error">{editError}</div>
            )}

            <button
              type="button"
              className="profile-dd__submit"
              disabled={!editName.trim()}
              onClick={handleSaveEdit}
            >
              {t.profile.save}
            </button>
          </div>
        </>
      )}
    </div>
  );
}
