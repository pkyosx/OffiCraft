import { useMemo, useRef, useState } from "react";
import { useI18n } from "../i18n";
import { en } from "../i18n/locales/en";
import { zh } from "../i18n/locales/zh";
import { readDictMessage } from "../i18n/wording";
import { MESSAGE_KEYS } from "../i18n/messageKeys.generated";
import {
  MAX_CUSTOM_THEMES,
  AVATAR_KINDS,
  isValidAvatarValue,
  validateThemeBundle,
  type AvatarKind,
  type ThemeBundle,
} from "../lib/themeBundle";
import { SAFE_FONT_FAMILIES } from "../styles/themeFonts.generated";
import {
  bundleFilename,
  exportComputedTheme,
  parseImportedBundle,
  serializeBundle,
} from "../lib/themeExport";
import {
  GROUP_ORDER,
  groupLabel,
  tokenMeta,
  toHex6,
  type TokenGroup,
} from "../lib/themeTokenMeta";
import { Breadcrumbs, type Crumb } from "./Breadcrumbs";
import {
  ChevronLeftIcon,
  DownloadIcon,
  PencilIcon,
  TrashIcon,
  UserIcon,
} from "./icons";
import { ConfirmModal } from "./ConfirmModal";
import "./theme-settings.css";

type View = "list" | "import" | "edit";

const DICTS_BY_LANG = { zh, en } as const;

// The two font tokens the editor offers a dropdown for (T-16a1 P4). Body =
// --font-sans (interface text), Title = --font-title (page headings). The
// options come from the safe-family allowlist; "" = keep the theme default.
const FONT_SLOTS = [
  { token: "--font-sans", labelKey: "themeFontBody" },
  { token: "--font-title", labelKey: "themeFontTitle" },
] as const;

// The two avatar slots the editor offers (T-16a1 P5): 正職 member / 外包
// outsource. Each accepts one uploaded image (validated client-side, embedded
// as a base64 data URI so it travels inside the bundle).
const AVATAR_SLOTS: { kind: AvatarKind; labelKey: "themeAvatarMember" | "themeAvatarOutsource" }[] = [
  { kind: "member", labelKey: "themeAvatarMember" },
  { kind: "outsource", labelKey: "themeAvatarOutsource" },
];

/**
 * 主題管理 — the SettingsPage 主題 sub-section (T-16a1 P3b). All theme MANAGEMENT
 * lives here (owner IA: 偏好=選擇, 設定=管理): add / import / export / edit
 * (friendly colours + 用詞 overlay) / delete. The ProfileDropdown keeps only the
 * theme SELECTOR + language.
 */
export function ThemeSettings({ crumbs }: { crumbs: Crumb[] }) {
  const { t, theme, setTheme, language, customThemes, commitCustomThemes } =
    useI18n();

  const [view, setView] = useState<View>("list");

  // ── import state ──
  const [importText, setImportText] = useState("");
  const [importError, setImportError] = useState("");
  const fileInputRef = useRef<HTMLInputElement>(null);

  // ── edit state ──
  const [editId, setEditId] = useState<string | null>(null);
  const [editName, setEditName] = useState("");
  const [editColors, setEditColors] = useState<[string, string][]>([]);
  const [editWording, setEditWording] = useState<
    Record<string, Record<string, string>>
  >({});
  // Font choices (T-16a1 P4): token → chosen family stack. An absent/"" entry
  // means "keep the theme default".
  const [editFonts, setEditFonts] = useState<Record<string, string>>({});
  // Avatar choices (T-16a1 P5): member/outsource → embedded base64 data URI. An
  // absent entry means "no avatar for this kind" (falls back to the built-in
  // glyph). Per-kind upload error surfaced inline.
  const [editAvatars, setEditAvatars] = useState<
    Partial<Record<AvatarKind, string>>
  >({});
  const [avatarError, setAvatarError] = useState("");
  const avatarInputRefs = {
    member: useRef<HTMLInputElement>(null),
    outsource: useRef<HTMLInputElement>(null),
  };
  const [wordingLang, setWordingLang] = useState<"zh" | "en">("zh");
  const [wordingSearch, setWordingSearch] = useState("");
  const [editError, setEditError] = useState("");

  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);

  // ── theme meta for export ──
  function currentThemeMeta(): { id: string; name: string } {
    if (theme === "office")
      return { id: "office-copy", name: t.profile.themeOffice };
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

  // ── import ──
  function openImport() {
    setImportText("");
    setImportError("");
    setView("import");
  }

  function addBundle(bundle: ThemeBundle): string | null {
    if (customThemes.some((b) => b.id === bundle.id))
      return t.profile.themeImportDup;
    if (customThemes.length >= MAX_CUSTOM_THEMES)
      return t.profile.themeLimitReached;
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
    setView("list");
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

  // ── delete ──
  function handleDeleteTheme(id: string) {
    const next = customThemes.filter((b) => b.id !== id);
    // Deleting the active theme drops back to the office base; the same PATCH
    // carries the reset so the server's dangling-guard agrees.
    commitCustomThemes(next, theme === id ? "office" : undefined);
    setConfirmDeleteId(null);
  }

  // ── edit ──
  function openEdit(bundle: ThemeBundle) {
    setEditId(bundle.id);
    setEditName(bundle.name);
    setEditColors(Object.entries(bundle.colors));
    setEditWording(
      bundle.wording
        ? JSON.parse(JSON.stringify(bundle.wording))
        : { zh: {}, en: {} }
    );
    setEditFonts({ ...(bundle.fonts ?? {}) });
    setEditAvatars({ ...(bundle.avatars ?? {}) });
    setAvatarError("");
    setWordingLang(language);
    setWordingSearch("");
    setEditError("");
    setView("edit");
  }

  // Read one picked file as a base64 data URI, VALIDATE it through the shared
  // client validator (mime whitelist + size + magic bytes — the same gate the
  // server enforces), and stash it on the given kind. An invalid file surfaces
  // an inline error and is NOT stored (never a silent bad value in the bundle).
  async function handleAvatarPicked(
    kind: AvatarKind,
    e: React.ChangeEvent<HTMLInputElement>
  ) {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    setAvatarError("");
    let dataUri: string;
    try {
      dataUri = await new Promise<string>((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve(String(reader.result ?? ""));
        reader.onerror = () => reject(new Error("read failed"));
        reader.readAsDataURL(file);
      });
    } catch {
      setAvatarError(t.settings.themeAvatarInvalid);
      return;
    }
    if (!isValidAvatarValue(dataUri)) {
      setAvatarError(t.settings.themeAvatarInvalid);
      return;
    }
    setEditAvatars((prev) => ({ ...prev, [kind]: dataUri }));
    setEditError("");
  }

  function clearAvatar(kind: AvatarKind) {
    setEditAvatars((prev) => {
      const next = { ...prev };
      delete next[kind];
      return next;
    });
    setAvatarError("");
    setEditError("");
  }

  function setColorAt(i: number, value: string) {
    const next = editColors.slice();
    next[i] = [next[i][0], value];
    setEditColors(next);
    setEditError("");
  }

  function setWordingAt(code: string, value: string) {
    setEditWording((prev) => ({
      ...prev,
      [wordingLang]: { ...(prev[wordingLang] ?? {}), [code]: value },
    }));
    setEditError("");
  }

  function handleSaveEdit() {
    if (editId == null) return;
    const colors: Record<string, string> = {};
    for (const [tok, val] of editColors) colors[tok] = val;

    // Prune empty overrides — the validator rejects an empty-after-trim value,
    // and an empty override just means "no override".
    const wording: Record<string, Record<string, string>> = {};
    for (const lang of ["zh", "en"] as const) {
      const entries = editWording[lang] ?? {};
      const kept: Record<string, string> = {};
      for (const [code, val] of Object.entries(entries)) {
        if (typeof val === "string" && val.trim() !== "") kept[code] = val.trim();
      }
      if (Object.keys(kept).length > 0) wording[lang] = kept;
    }

    // Prune "" (keep-default) picks — an absent font token means "theme
    // default", so we never store an empty value (the validator would reject it).
    const fonts: Record<string, string> = {};
    for (const { token } of FONT_SLOTS) {
      const v = editFonts[token];
      if (typeof v === "string" && v !== "") fonts[token] = v;
    }

    // Keep only the avatar kinds that actually hold an image — an absent kind
    // means "no avatar" (falls back to the built-in glyph). Each value already
    // passed isValidAvatarValue at upload time; the bundle validator re-checks.
    const avatars: { member?: string; outsource?: string } = {};
    for (const kind of AVATAR_KINDS) {
      const v = editAvatars[kind];
      if (typeof v === "string" && v !== "") avatars[kind] = v;
    }

    const bundle: ThemeBundle = { id: editId, name: editName, colors };
    if (Object.keys(wording).length > 0) bundle.wording = wording;
    if (Object.keys(fonts).length > 0) bundle.fonts = fonts;
    if (Object.keys(avatars).length > 0) bundle.avatars = avatars;

    const err = validateThemeBundle(bundle);
    if (err) {
      setEditError(err);
      return;
    }
    commitCustomThemes(customThemes.map((b) => (b.id === editId ? bundle : b)));
    setView("list");
  }

  // Colours grouped by purpose (owner: no raw --color-* in the editor).
  const groupedColors = useMemo(() => {
    const byGroup = new Map<TokenGroup, number[]>();
    editColors.forEach(([tok], i) => {
      const g = tokenMeta(tok, language).group;
      const arr = byGroup.get(g) ?? [];
      arr.push(i);
      byGroup.set(g, arr);
    });
    return GROUP_ORDER.filter((g) => byGroup.has(g)).map((g) => ({
      group: g,
      indices: byGroup.get(g)!,
    }));
  }, [editColors, language]);

  // The wording rows: every overridable message code, filtered by the search
  // (matches the code, its English original, or its text in the edited lang).
  const wordingRows = useMemo(() => {
    const q = wordingSearch.trim().toLowerCase();
    const dict = DICTS_BY_LANG[wordingLang];
    return MESSAGE_KEYS.filter((code) => {
      if (!q) return true;
      const enText = readDictMessage(en, code) ?? "";
      const curText = readDictMessage(dict, code) ?? "";
      return (
        code.toLowerCase().includes(q) ||
        enText.toLowerCase().includes(q) ||
        curText.toLowerCase().includes(q)
      );
    });
  }, [wordingSearch, wordingLang]);

  const wordingOverrideCount = useMemo(() => {
    let n = 0;
    for (const lang of ["zh", "en"] as const) {
      for (const v of Object.values(editWording[lang] ?? {})) {
        if (typeof v === "string" && v.trim() !== "") n++;
      }
    }
    return n;
  }, [editWording]);

  // ── render: import ──
  if (view === "import") {
    return (
      <div className="settings">
        <Breadcrumbs items={crumbs} />
        <button
          type="button"
          className="ts-back"
          onClick={() => setView("list")}
        >
          <ChevronLeftIcon size={16} />
          <span>{t.profile.themeImportTitle}</span>
        </button>
        <h1 className="settings__title settings__title--doc">
          {t.profile.themeImportTitle}
        </h1>

        <div className="ts-form">
          <textarea
            className="ts-textarea"
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
            className="ts-file"
            onChange={handleFilePicked}
          />
          <div className="ts-form-actions">
            <button
              type="button"
              className="doc-btn"
              onClick={() => fileInputRef.current?.click()}
            >
              {t.profile.themeChooseFile}
            </button>
            <button
              type="button"
              className="doc-btn doc-btn--accent"
              disabled={!importText.trim()}
              onClick={handleConfirmImport}
            >
              {t.profile.themeConfirmImport}
            </button>
          </div>
          {importError && <div className="set-error">{importError}</div>}
        </div>
      </div>
    );
  }

  // ── render: edit ──
  if (view === "edit") {
    return (
      <div className="settings">
        <Breadcrumbs items={crumbs} />
        <button
          type="button"
          className="ts-back"
          onClick={() => setView("list")}
        >
          <ChevronLeftIcon size={16} />
          <span>{t.profile.themeEditTitle}</span>
        </button>
        <h1 className="settings__title settings__title--doc">
          {t.profile.themeEditTitle}
        </h1>

        <div className="ts-card">
          <label className="ts-field-label" htmlFor="ts-edit-name">
            {t.profile.themeNameLabel}
          </label>
          <input
            id="ts-edit-name"
            className="ts-input"
            value={editName}
            aria-label={t.profile.themeNameLabel}
            onChange={(e) => {
              setEditName(e.target.value);
              setEditError("");
            }}
          />

          {/* ── colours, grouped by purpose, with a visual picker ── */}
          <div className="ts-section-label">{t.settings.themeColorsSection}</div>
          {groupedColors.map(({ group, indices }) => (
            <div key={group} className="ts-color-group">
              <div className="ts-color-group__label">
                {groupLabel(group, language)}
              </div>
              {indices.map((i) => {
                const [token, value] = editColors[i];
                const hex = toHex6(value);
                const meta = tokenMeta(token, language);
                return (
                  <div key={token} className="ts-color-row">
                    <span className="ts-color-name" title={token}>
                      {meta.label}
                    </span>
                    <input
                      type="color"
                      className="ts-swatch"
                      aria-label={`${meta.label} ${t.settings.themeColorPicker}`}
                      value={hex ?? "#000000"}
                      onChange={(e) => setColorAt(i, e.target.value)}
                    />
                    <input
                      className="ts-input ts-color-value"
                      value={value}
                      aria-label={meta.label}
                      onChange={(e) => setColorAt(i, e.target.value)}
                    />
                  </div>
                );
              })}
            </div>
          ))}

          {/* ── fonts (字型) — pick body / title font from a safe allowlist ── */}
          <div className="ts-section-label">{t.settings.themeFontsSection}</div>
          <div className="ts-wording-sub">{t.settings.themeFontsHint}</div>
          {FONT_SLOTS.map(({ token, labelKey }) => (
            <div key={token} className="ts-font-row">
              <label className="ts-font-label" htmlFor={`ts-font-${token}`}>
                {t.settings[labelKey]}
              </label>
              <select
                id={`ts-font-${token}`}
                className="ts-input ts-font-select"
                aria-label={t.settings[labelKey]}
                value={editFonts[token] ?? ""}
                style={{ fontFamily: editFonts[token] || undefined }}
                onChange={(e) => {
                  const val = e.target.value;
                  setEditFonts((prev) => ({ ...prev, [token]: val }));
                  setEditError("");
                }}
              >
                <option value="">{t.settings.themeFontDefault}</option>
                {SAFE_FONT_FAMILIES.map((f) => (
                  <option key={f.id} value={f.stack} style={{ fontFamily: f.stack }}>
                    {f.label}
                  </option>
                ))}
              </select>
            </div>
          ))}

          {/* ── avatars (頭像) — per-member-type avatar image upload ── */}
          <div className="ts-section-label">{t.settings.themeAvatarsSection}</div>
          <div className="ts-wording-sub">{t.settings.themeAvatarsHint}</div>
          <div className="ts-avatar-slots">
            {AVATAR_SLOTS.map(({ kind, labelKey }) => {
              const src = editAvatars[kind];
              return (
                <div key={kind} className="ts-avatar-slot">
                  <div className="ts-avatar-label">{t.settings[labelKey]}</div>
                  <div className="ts-avatar-row">
                    <span
                      className="avatar ts-avatar-preview"
                      style={{ width: 48, height: 48 }}
                    >
                      {src ? (
                        <img
                          className="avatar__img"
                          src={src}
                          alt=""
                          width={48}
                          height={48}
                          draggable={false}
                        />
                      ) : (
                        <UserIcon size={24} className="avatar__glyph" />
                      )}
                    </span>
                    <input
                      ref={avatarInputRefs[kind]}
                      type="file"
                      accept="image/png,image/jpeg,image/webp"
                      className="ts-file"
                      aria-label={t.settings[labelKey]}
                      onChange={(e) => handleAvatarPicked(kind, e)}
                    />
                    <button
                      type="button"
                      className="doc-btn"
                      onClick={() => avatarInputRefs[kind].current?.click()}
                    >
                      {t.settings.themeAvatarChoose}
                    </button>
                    {src && (
                      <button
                        type="button"
                        className="doc-btn"
                        onClick={() => clearAvatar(kind)}
                      >
                        {t.settings.themeAvatarClear}
                      </button>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
          {avatarError && <div className="set-error">{avatarError}</div>}

          {/* ── wording overlay (用詞) ── */}
          <div className="ts-section-label">
            {t.settings.themeWordingSection}
            {wordingOverrideCount > 0 && (
              <span className="ts-badge">{wordingOverrideCount}</span>
            )}
          </div>
          <div className="ts-wording-sub">{t.settings.themeWordingHint}</div>
          <div className="ts-wording-tabs" role="tablist">
            {(["zh", "en"] as const).map((lang) => (
              <button
                key={lang}
                type="button"
                role="tab"
                aria-selected={wordingLang === lang}
                className={`ts-tab${wordingLang === lang ? " ts-tab--active" : ""}`}
                onClick={() => setWordingLang(lang)}
              >
                {lang === "zh" ? t.profile.langZh : t.profile.langEn}
              </button>
            ))}
          </div>
          <input
            className="ts-input ts-wording-search"
            type="search"
            placeholder={t.settings.themeWordingSearch}
            aria-label={t.settings.themeWordingSearch}
            value={wordingSearch}
            onChange={(e) => setWordingSearch(e.target.value)}
          />
          <div className="ts-wording-list">
            {wordingRows.map((code) => {
              const enText = readDictMessage(en, code) ?? "";
              const curText =
                readDictMessage(DICTS_BY_LANG[wordingLang], code) ?? "";
              const override = editWording[wordingLang]?.[code] ?? "";
              return (
                <div key={code} className="ts-wording-row">
                  <div className="ts-wording-meta">
                    <span className="ts-wording-en">{enText}</span>
                    <span className="ts-wording-cur">{curText}</span>
                  </div>
                  <input
                    className="ts-input ts-wording-input"
                    value={override}
                    placeholder={curText}
                    aria-label={`${enText} — ${t.settings.themeWordingOverride}`}
                    onChange={(e) => setWordingAt(code, e.target.value)}
                  />
                </div>
              );
            })}
          </div>

          {editError && <div className="set-error">{editError}</div>}

          <div className="ts-form-actions">
            <button
              type="button"
              className="doc-btn"
              onClick={() => setView("list")}
            >
              {t.settings.cancel}
            </button>
            <button
              type="button"
              className="doc-btn doc-btn--accent"
              disabled={!editName.trim()}
              onClick={handleSaveEdit}
            >
              {t.profile.save}
            </button>
          </div>
        </div>
      </div>
    );
  }

  // ── render: list ──
  return (
    <div className="settings">
      <Breadcrumbs items={crumbs} />
      <h1 className="settings__title settings__title--doc">
        {t.settings.themeManage}
      </h1>

      <div className="ts-toolbar">
        <button type="button" className="doc-btn" onClick={openImport}>
          {t.profile.themeImport}
        </button>
        <button type="button" className="doc-btn" onClick={handleExportCurrent}>
          {t.profile.themeExport}
        </button>
      </div>

      <div className="ts-list">
        {/* built-in: office is the only built-in — selectable, not
         * editable/deletable. It still shows the SAME trailing action column as a
         * custom row, but disabled: the three icon buttons are inert placeholders
         * so the built-in and custom rows line up their right edge at every width
         * (owner: 內建列與自訂列對齊). */}
        <div className="ts-row">
          <button
            type="button"
            className={`ts-pick${theme === "office" ? " ts-pick--active" : ""}`}
            onClick={() => setTheme("office")}
          >
            {t.profile.themeOffice}
            <span className="ts-tag">{t.settings.themeBuiltinTag}</span>
          </button>
          <button
            type="button"
            className="ts-icon-btn"
            disabled
            aria-disabled="true"
            aria-label={`${t.profile.themeExport} ${t.profile.themeOffice}`}
            title={t.profile.themeExport}
          >
            <DownloadIcon size={15} />
          </button>
          <button
            type="button"
            className="ts-icon-btn"
            disabled
            aria-disabled="true"
            aria-label={`${t.profile.themeEdit} ${t.profile.themeOffice}`}
            title={t.profile.themeEdit}
          >
            <PencilIcon size={15} />
          </button>
          <button
            type="button"
            className="ts-icon-btn ts-icon-btn--danger"
            disabled
            aria-disabled="true"
            aria-label={`${t.profile.themeDelete} ${t.profile.themeOffice}`}
            title={t.profile.themeDelete}
          >
            <TrashIcon size={15} />
          </button>
        </div>

        {customThemes.map((b) => (
          <div key={b.id} className="ts-row">
            <button
              type="button"
              className={`ts-pick${theme === b.id ? " ts-pick--active" : ""}`}
              onClick={() => setTheme(b.id)}
            >
              {b.name}
              {b.wording && (
                <span className="ts-tag ts-tag--wording">
                  {t.settings.themeWordingTag}
                </span>
              )}
            </button>
            <button
              type="button"
              className="ts-icon-btn"
              aria-label={`${t.profile.themeExport} ${b.name}`}
              title={t.profile.themeExport}
              onClick={() => downloadBundle(b)}
            >
              <DownloadIcon size={15} />
            </button>
            <button
              type="button"
              className="ts-icon-btn"
              aria-label={`${t.profile.themeEdit} ${b.name}`}
              title={t.profile.themeEdit}
              onClick={() => openEdit(b)}
            >
              <PencilIcon size={15} />
            </button>
            <button
              type="button"
              className="ts-icon-btn ts-icon-btn--danger"
              aria-label={`${t.profile.themeDelete} ${b.name}`}
              title={t.profile.themeDelete}
              onClick={() => setConfirmDeleteId(b.id)}
            >
              <TrashIcon size={15} />
            </button>
          </div>
        ))}
      </div>

      {(() => {
        const target = customThemes.find((b) => b.id === confirmDeleteId);
        if (!target) return null;
        return (
          <ConfirmModal
            testId="theme-delete-confirm"
            confirmTestId="theme-delete-confirm-btn"
            danger
            body={t.settings.themeDeleteConfirm(target.name)}
            cancelLabel={t.settings.cancel}
            confirmLabel={t.profile.themeDelete}
            onCancel={() => setConfirmDeleteId(null)}
            onConfirm={() => handleDeleteTheme(target.id)}
          />
        );
      })()}
    </div>
  );
}
