import { useEffect, useMemo, useRef, useState, type RefObject } from "react";
import { useI18n } from "../i18n";
import { en } from "../i18n/locales/en";
import { zh } from "../i18n/locales/zh";
import { readDictMessage } from "../i18n/wording";
import { MESSAGE_KEYS } from "../i18n/messageKeys.generated";
import {
  MAX_CUSTOM_THEMES,
  AVATAR_KINDS,
  BUILTIN_THEMES,
  RESERVED_THEME_IDS,
  NAV_ICON_KEYS,
  isValidAvatarValue,
  isValidBackgroundValue,
  validateThemeBundle,
  BACKGROUND_MODES,
  DEFAULT_BACKGROUND_MODE,
  type AvatarKind,
  type BackgroundMode,
  type NavIconKey,
  type ThemeBundle,
} from "../lib/themeBundle";
import {
  MAX_SCHEDULE_ENTRIES,
  MIN_SCHEDULE_ENTRIES,
  themeScheduleProblem,
  type ThemeScheduleEntry,
} from "../lib/themeSchedule";
import { SAFE_FONT_FAMILIES } from "../styles/themeFonts.generated";
import { THEME_ALIAS_DEFAULT_TOKENS } from "../styles/themeTokens.generated";
import {
  bundleFilename,
  exportBuiltinTheme,
  nextCustomThemeId,
  parseImportedBundle,
  serializeBundle,
} from "../lib/themeExport";
import {
  GROUP_ORDER,
  groupLabel,
  tokenMeta,
  toHex6,
  alphaPercent,
  withAlphaPercent,
  type TokenGroup,
} from "../lib/themeTokenMeta";
import { api } from "../api";
import { ApiError, serverMessageOf } from "../api/errors";
import { Breadcrumbs, type Crumb } from "./Breadcrumbs";
import {
  ChevronLeftIcon,
  DownloadIcon,
  PencilIcon,
  TrashIcon,
  UserIcon,
} from "./icons";
import { ConfirmModal } from "./ConfirmModal";
import officeTheme from "../../../themes/office.theme.json" with { type: "json" };
import officeLightTheme from "../../../themes/office-light.theme.json" with { type: "json" };
import "./settings.css";
import "./theme-settings.css";

type View = "list" | "import" | "edit";

type PageTab = "appearance" | "library";
const PAGE_TABS = [
  { id: "appearance", labelKey: "themeTabAppearance" },
  { id: "library", labelKey: "themeTabLibrary" },
] as const;

const DICTS_BY_LANG = { zh, en } as const;

// How many skipped wording codes the import warning names before the count
// carries the rest — a pack can skip dozens, and the banner must stay one line.
const IMPORT_SKIPPED_SAMPLE = 3;

// ── 用詞 list: every code stays in the DOM, deliberately (T-8115 reverted) ──
// The 用詞 panel offers EVERY overridable message code — 866 of them — and it
// renders all of them at once. That is a decision, not an oversight.
//
// It used to be a virtualised scroll window (only the visible rows mounted, the
// rest reserved by two spacer blocks). That bought a real, measured saving:
// opening the theme editor cost ~7.6ms mounted-window vs ~64ms fully mounted,
// and ~34ms vs ~165ms on a 4x-throttled CPU (20 rows / 240 inputs vs 866 rows /
// 1086 inputs). The owner weighed that against what it cost and chose to pay it
// (2026-08-02): "這設定根本不常進去 只要不是秒等級 根本沒差 而且通常都是直接匯入"
// — this screen is opened rarely, themes usually arrive by import rather than by
// hand-editing wording, and nothing here is anywhere near a second.
//
// What the windowing cost, and what came back by dropping it (each measured):
//   * Tab / screen-reader order. Windowing has to keep the FOCUSED row mounted
//     (unmounting the element focus lives in hands focus to <body> and the caret
//     vanishes), and that pinned row broke sequential order: Tab out of it left
//     the list entirely and landed on 取消, and the reading order ran
//     …865, 866, 1. With every row mounted there is no pin and no reordering.
//   * The browser's own find (Cmd+F), whole-page select-all, and print only ever
//     saw the mounted handful. Selectable text measured 1,736 → 32,189 chars.
// So: do NOT reintroduce virtualisation, windowing, an overscan window, or any
// "only render N rows" cap here without a fresh owner ruling. Three separate
// capabilities ride on every row being in the document, and two of them are
// browser features we do not implement and cannot re-expose any other way.

// The two font tokens the editor offers a dropdown for (T-16a1 P4). Body =
// --font-sans (interface text), Title = --font-title (page headings). The
// options come from the safe-family allowlist; "" = keep the theme default.
const FONT_SLOTS = [
  { token: "--font-sans", labelKey: "themeFontBody" },
  { token: "--font-title", labelKey: "themeFontTitle" },
] as const;

// The four avatar slots the editor offers: 正職 staff /
// 外包 outsource / owner CEO / assistant 助理. Each accepts one uploaded image
// (validated client-side, embedded as a base64 data URI so it travels inside
// the bundle).
type AvatarLabelKey =
  | "themeAvatarMember"
  | "themeAvatarOutsource"
  | "themeAvatarOwner"
  | "themeAvatarAssistant";
const AVATAR_SLOTS: { kind: AvatarKind; labelKey: AvatarLabelKey }[] = [
  { kind: "staff", labelKey: "themeAvatarMember" },
  { kind: "outsource", labelKey: "themeAvatarOutsource" },
  { kind: "owner", labelKey: "themeAvatarOwner" },
  { kind: "assistant", labelKey: "themeAvatarAssistant" },
];

// The nav-tab icon slots the editor offers, keyed by tab. Same upload
// flow as avatars (shared image gate); stored on
// bundle.navIcons[key].
type NavIconLabelKey =
  | "themeNavOffice"
  | "themeNavReplies"
  | "themeNavTasks"
  | "themeNavLore"
  | "themeNavSecrets"
  | "themeNavMonitor"
  | "themeNavGuide";
const NAV_ICON_LABEL_KEYS: Record<NavIconKey, NavIconLabelKey> = {
  office: "themeNavOffice",
  replies: "themeNavReplies",
  tasks: "themeNavTasks",
  lore: "themeNavLore",
  secrets: "themeNavSecrets",
  monitor: "themeNavMonitor",
  guide: "themeNavGuide",
};
const NAV_ICON_SLOTS = NAV_ICON_KEYS.map((key) => ({
  key,
  labelKey: NAV_ICON_LABEL_KEYS[key],
}));

/**
 * 主題 — the SettingsPage 主題 sub-section. 外觀 chooses what is shown (one theme,
 * or a time-of-day schedule, plus the seasonal switch); 主題庫 manages the set:
 * add / import / export / edit (friendly colours + 用詞 overlay) / delete. The
 * ProfileDropdown only says which theme is showing (owner 2026-11-05).
 */
export function ThemeSettings({ crumbs }: { crumbs: Crumb[] }) {
  // [T-83ef] The set arrives as ONE LINE EACH (id + name) and a bundle is
  // fetched per theme. That is the whole shape of this screen now: everything
  // the LIST draws comes from `themeList`; 匯出, 編輯 and the 外觀 thumbnails
  // pay for a request, one theme at a time.
  const {
    t,
    msg,
    theme,
    language,
    themeList,
    activeThemeBundle,
    loadTheme,
    saveTheme,
    removeTheme,
  } = useI18n();

  const [view, setView] = useState<View>("list");
  const [tab, setTab] = useState<PageTab>("appearance");
  const schedule = useThemeScheduleDraft();
  const swatches = useThemeSwatches(
    view === "list" && tab === "appearance"
      ? (schedule.draft?.map((r) => r.theme) ?? [theme])
      : []
  );

  // ── import state ──
  const [importText, setImportText] = useState("");
  const [importError, setImportError] = useState("");
  // The unrecognised wording codes the last import dropped. An import with such
  // codes SUCCEEDS (owner ruling 2026-07-27) — this is what makes the drop
  // visible instead of silent, and it is shown on the list the import lands on.
  const [importSkipped, setImportSkipped] = useState<string[]>([]);
  const fileInputRef = useRef<HTMLInputElement>(null);
  // ── link import (T-29c7) ──
  // A theme with a background image runs to hundreds of thousands of
  // characters while a chat message is capped at 4000, so a link is the only
  // channel through which a finished theme can be handed over at all.
  const [importUrl, setImportUrl] = useState("");
  const [importUrlBusy, setImportUrlBusy] = useState(false);

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
  // Avatar choices (T-16a1 P5): staff/outsource → embedded base64 data URI. An
  // absent entry means "no avatar for this kind" (falls back to the built-in
  // glyph). Per-kind upload error surfaced inline.
  const [editAvatars, setEditAvatars] = useState<
    Partial<Record<AvatarKind, string>>
  >({});
  const [avatarError, setAvatarError] = useState("");
  const avatarInputRefs = {
    staff: useRef<HTMLInputElement>(null),
    outsource: useRef<HTMLInputElement>(null),
    owner: useRef<HTMLInputElement>(null),
    assistant: useRef<HTMLInputElement>(null),
  };
  // Studio logo: a single embedded base64 data URI ("" = none, falls back to
  // the built-in mark). Per-nav-tab icons: key → embedded data URI.
  // Both go through the SAME image gate as avatars; upload errors surfaced
  // inline in their own section.
  const [editLogo, setEditLogo] = useState<string>("");
  const [editNavIcons, setEditNavIcons] = useState<
    Partial<Record<NavIconKey, string>>
  >({});
  // Outer-canvas background tile (T-081b): a single embedded base64 data URI
  // ("" = none, the canvas stays the plain --color-bg colour). SAME image gate
  // as the logo. Only this ONE zone is offered — the topbar / nav / main zones
  // sit under text and are deliberately colour-only.
  const [editCanvasBg, setEditCanvasBg] = useState<string>("");
  // How that tile is laid down (T-081b) — tile (repeat both axes, the historical
  // and default behaviour) or sides (one copy against each viewport edge).
  const [editCanvasBgMode, setEditCanvasBgMode] = useState<BackgroundMode>(
    DEFAULT_BACKGROUND_MODE
  );
  const [brandError, setBrandError] = useState("");
  const logoInputRef = useRef<HTMLInputElement>(null);
  const canvasBgInputRef = useRef<HTMLInputElement>(null);
  const navIconInputRefs = {
    office: useRef<HTMLInputElement>(null),
    replies: useRef<HTMLInputElement>(null),
    tasks: useRef<HTMLInputElement>(null),
    lore: useRef<HTMLInputElement>(null),
    secrets: useRef<HTMLInputElement>(null),
    monitor: useRef<HTMLInputElement>(null),
    guide: useRef<HTMLInputElement>(null),
  } satisfies Record<NavIconKey, RefObject<HTMLInputElement>>;
  const [wordingLang, setWordingLang] = useState<"zh" | "en">("zh");
  const [wordingSearch, setWordingSearch] = useState("");
  // Only so a new search result set can be scrolled back to the top; the list
  // itself is a plain scroll box with every row in it (see the note above).
  const wordingListRef = useRef<HTMLDivElement>(null);
  const [editError, setEditError] = useState("");

  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);
  // The LIST view's one error line. It is not only "add" any more (T-83ef):
  // every list action now talks to the server on its own — fetching one bundle
  // to edit/export, writing one theme, deleting one — and each of them can fail
  // on its own. They share this line because they share the screen: at most one
  // list action is in flight at a time (see `busyId`), so two of them can never
  // be competing to say something different.
  const [listError, setListError] = useState("");
  // The theme id whose bundle is being fetched right now, or null. It is BOTH
  // the "loading…" signal the row buttons disable on AND the re-entrancy guard:
  // a bundle is hundreds of KB, so a second click during the flight would cost
  // a second full download and could land its result out of order.
  const [busyId, setBusyId] = useState<string | null>(null);

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

  // ── add (req4): create a new custom theme with office as its base, then jump
  // straight into edit so the owner names + tweaks the fresh copy. A custom
  // theme paints over the office base, so the copy starts from office colours
  // whichever theme is applied right now. ──
  async function handleAddNew() {
    if (themeList.length >= MAX_CUSTOM_THEMES) {
      setListError(t.profile.themeLimitReached);
      return;
    }
    const id = nextCustomThemeId(themeList.map((b) => b.id));
    const bundle = exportBuiltinTheme("office", id, t.themeIdentity.newTheme);
    try {
      await saveTheme(bundle);
    } catch (e) {
      // The row must not appear if the server did not take it — jumping into
      // the editor for a theme that was never created would let the owner spend
      // real work on something the next reload does not have.
      setListError(failureText(e));
      return;
    }
    setListError("");
    // No fetch: this bundle is the one we just wrote, so it IS the server's
    // content by definition.
    openEdit(bundle);
  }

  /** Server's own words when it gave any, else the shared "動作失敗" line the
   * doc editor already uses for exactly this case (settings.docActionFailed).
   * A failure that only reached console.warn is a failure the owner cannot see,
   * and every caller below can fail for a reason worth reading. */
  function failureText(e: unknown): string {
    return serverMessageOf(e) || t.settings.docActionFailed;
  }

  /** The full bundle for one listed theme — what 匯出 and 編輯 need and what the
   * list no longer carries (T-83ef). Returns null when it could not be had, and
   * has already put the reason on screen in that case.
   *
   * The ACTIVE theme is served from the bundle the provider already holds. That
   * is a whole request saved on the most likely row to be edited, and it is
   * sound ONLY under the id check: `activeThemeBundle` is the active theme, so
   * handing it back for any other id would silently edit/export the wrong
   * theme's colours under the right theme's name. */
  async function bundleFor(id: string): Promise<ThemeBundle | null> {
    if (activeThemeBundle && activeThemeBundle.id === id) return activeThemeBundle;
    if (busyId !== null) return null; // one bundle in flight at a time
    setBusyId(id);
    setListError("");
    try {
      return await loadTheme(id);
    } catch (e) {
      setListError(failureText(e));
      return null;
    } finally {
      setBusyId(null);
    }
  }

  async function handleExportTheme(id: string) {
    const bundle = await bundleFor(id);
    if (bundle) downloadBundle(bundle);
  }

  async function handleEditTheme(id: string) {
    const bundle = await bundleFor(id);
    if (bundle) openEdit(bundle);
  }

  // ── import ──
  function openImport() {
    setImportText("");
    setImportUrl("");
    setImportUrlBusy(false);
    setImportError("");
    setImportSkipped([]);
    setListError("");
    setView("import");
  }

  // Returns the message to show, or null on success. The duplicate/limit checks
  // read `themeList` — id and name is all they ever needed.
  async function addBundle(bundle: ThemeBundle): Promise<string | null> {
    // Only a NEW theme is held to this: one the server already stores under a
    // seasonal id (written over the API) stays editable.
    if (RESERVED_THEME_IDS.includes(bundle.id)) return t.profile.themeImportSeasonalId;
    if (themeList.some((b) => b.id === bundle.id))
      return t.profile.themeImportDup;
    if (themeList.length >= MAX_CUSTOM_THEMES)
      return t.profile.themeLimitReached;
    try {
      await saveTheme(bundle);
      swatches.remember(bundle);
    } catch (e) {
      // The write is the import. A rejected write that reported success would
      // send the owner back to a list without the theme they just imported and
      // no reason why.
      return failureText(e);
    }
    return null;
  }

  // The ONE import path. Pasted text, a picked file and a fetched link all end
  // up here (T-29c7): a link-imported theme is validated by exactly the same
  // parseImportedBundle as a pasted one, so the two can never start accepting
  // different things.
  async function importBundleText(text: string) {
    const res = parseImportedBundle(text);
    if ("error" in res) {
      setImportError(res.error);
      return;
    }
    const err = await addBundle(res.bundle);
    if (err) {
      setImportError(err);
      return;
    }
    setImportSkipped(res.skippedWording);
    setView("list");
  }

  async function handleConfirmImport() {
    await importBundleText(importText);
  }

  // Paste a link → the server reads it back → the bundle goes through the same
  // import path, in ONE click. Stopping at "the JSON is now in the box" would
  // leave the owner a second button to press for no reason.
  async function handleImportFromLink() {
    const url = importUrl.trim();
    if (!url || importUrlBusy) return;
    setImportUrlBusy(true);
    setImportError("");
    try {
      const content = await api.fetchThemeFromLink(url);
      // Show what came back BEFORE importing: if the bundle is then rejected
      // (duplicate id, theme limit) the owner can see and edit the thing that
      // was rejected instead of an empty box and a message about nothing.
      setImportText(content);
      // Awaited so the busy flag below still covers the WRITE — the import is
      // not over when the fetch returns, it is over when the theme is stored.
      await importBundleText(content);
    } catch (e) {
      setImportError(
        e instanceof ApiError && e.serverMessage
          ? e.serverMessage
          : t.profile.themeImportLinkFailed
      );
    } finally {
      setImportUrlBusy(false);
    }
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
  async function handleDeleteTheme(id: string) {
    // Re-entrancy: the confirm modal stays up until the delete settles (so a
    // failure can be reported against the theme it happened to), which means a
    // second click on 刪除 is possible in that window.
    if (busyId !== null) return;
    setBusyId(id);
    setListError("");
    try {
      // Deleting the ACTIVE theme is no longer this screen's arithmetic: the
      // provider is told by the server that it reset the stored display theme
      // and switches back to the built-in itself. Recomputing it here would be
      // a second opinion on the same fact, free to disagree.
      await removeTheme(id);
      setConfirmDeleteId(null);
    } catch (e) {
      setListError(failureText(e));
      // The dialog closes on a failure too: it asks a question that has been
      // answered, and the row is still there for a second attempt.
      setConfirmDeleteId(null);
    } finally {
      setBusyId(null);
    }
  }

  // ── edit ──
  function openEdit(bundle: ThemeBundle) {
    setEditId(bundle.id);
    setEditName(bundle.name);
    // Rows = what the bundle sets, PLUS every token whose default merely
    // follows another token (the zone backgrounds, the T-081b split tokens).
    // Those never appear in an exported bundle — that is deliberate, it is what
    // lets them keep following — so without this they were unreachable in the
    // product's own editor while being exactly the ones a theme needs to set
    // apart (e.g. a translucent top bar, or a light theme's switch knob).
    // An empty value means "leave it following"; only non-empty rows are saved.
    setEditColors([
      ...Object.entries(bundle.colors),
      ...Object.keys(THEME_ALIAS_DEFAULT_TOKENS)
        .filter((tok) => !(tok in bundle.colors))
        .map((tok) => [tok, ""] as [string, string]),
    ]);
    setEditWording(
      bundle.wording
        ? JSON.parse(JSON.stringify(bundle.wording))
        : { zh: {}, en: {} }
    );
    setEditFonts({ ...(bundle.fonts ?? {}) });
    setEditAvatars({ ...(bundle.avatars ?? {}) });
    setAvatarError("");
    setEditLogo(bundle.logo ?? "");
    setEditNavIcons({ ...(bundle.navIcons ?? {}) });
    setEditCanvasBg(bundle.backgrounds?.canvas ?? "");
    setEditCanvasBgMode(bundle.backgroundModes?.canvas ?? DEFAULT_BACKGROUND_MODE);
    setBrandError("");
    setWordingLang(language);
    setWordingSearch("");
    setEditError("");
    setView("edit");
  }

  // Read one picked file as a base64 data URI and VALIDATE it through the shared
  // client validator (mime whitelist + size + magic bytes — the same gate the
  // server enforces). `validate` selects WHICH size caps apply and defaults to
  // the avatar caps (64 KiB), which is what avatars / logo / nav-icons want; the
  // canvas background passes isValidBackgroundValue instead (512 KiB, T-72da).
  // The SAFETY half of the gate — raster mime allowlist, SVG refusal, magic
  // bytes — is identical either way and is never relaxed. If a caller forgets to
  // pass its validator the cockpit would reject an image the server accepts, and
  // the owner would see "invalid image" for a perfectly good file.
  // Returns the validated data URI, or null when the file is unreadable or fails
  // validation (never a silent bad value in the bundle).
  async function readValidatedImage(
    file: File,
    validate: (v: string) => boolean = isValidAvatarValue
  ): Promise<string | null> {
    let dataUri: string;
    try {
      dataUri = await new Promise<string>((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve(String(reader.result ?? ""));
        reader.onerror = () => reject(new Error("read failed"));
        reader.readAsDataURL(file);
      });
    } catch {
      return null;
    }
    return validate(dataUri) ? dataUri : null;
  }

  async function handleAvatarPicked(
    kind: AvatarKind,
    e: React.ChangeEvent<HTMLInputElement>
  ) {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    setAvatarError("");
    const dataUri = await readValidatedImage(file);
    if (dataUri === null) {
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

  async function handleLogoPicked(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    setBrandError("");
    const dataUri = await readValidatedImage(file);
    if (dataUri === null) {
      setBrandError(t.settings.themeAvatarInvalid);
      return;
    }
    setEditLogo(dataUri);
    setEditError("");
  }

  async function handleCanvasBgPicked(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    setBrandError("");
    // The canvas background is the ONE picker on the background caps.
    const dataUri = await readValidatedImage(file, isValidBackgroundValue);
    if (dataUri === null) {
      setBrandError(t.settings.themeCanvasBgInvalid);
      return;
    }
    setEditCanvasBg(dataUri);
    setEditError("");
  }

  function clearCanvasBg() {
    setEditCanvasBg("");
    setBrandError("");
    setEditError("");
  }

  function clearLogo() {
    setEditLogo("");
    setBrandError("");
    setEditError("");
  }

  async function handleNavIconPicked(
    key: NavIconKey,
    e: React.ChangeEvent<HTMLInputElement>
  ) {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    setBrandError("");
    const dataUri = await readValidatedImage(file);
    if (dataUri === null) {
      setBrandError(t.settings.themeAvatarInvalid);
      return;
    }
    setEditNavIcons((prev) => ({ ...prev, [key]: dataUri }));
    setEditError("");
  }

  function clearNavIcon(key: NavIconKey) {
    setEditNavIcons((prev) => {
      const next = { ...prev };
      delete next[key];
      return next;
    });
    setBrandError("");
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

  async function handleSaveEdit() {
    if (editId == null) return;
    const colors: Record<string, string> = {};
    // An empty row is "still following its default" — writing it would bake a
    // literal value in and break exactly the following it is there to preserve.
    for (const [tok, val] of editColors) if (val.trim() !== "") colors[tok] = val;

    // Prune empty overrides — the validator rejects an empty-after-trim value,
    // and an empty override just means "no override". The VALUE is stored
    // verbatim, never trimmed: several overridable leaves are sentence FRAGMENTS
    // whose boundary space is load-bearing (machine.uninstallWarnBody2 is
    // `"” still has "`, …Body3 is `" member(s) online…"`), so trimming what the
    // owner typed would render "still has3member(s)" / 「上還有3位成員」 — the
    // product's own editor corrupting the very strings it just made overridable.
    // Only the emptiness TEST trims, which is all it was ever protecting.
    const wording: Record<string, Record<string, string>> = {};
    for (const lang of ["zh", "en"] as const) {
      const entries = editWording[lang] ?? {};
      const kept: Record<string, string> = {};
      for (const [code, val] of Object.entries(entries)) {
        if (typeof val === "string" && val.trim() !== "") kept[code] = val;
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
    const avatars: Partial<Record<AvatarKind, string>> = {};
    for (const kind of AVATAR_KINDS) {
      const v = editAvatars[kind];
      if (typeof v === "string" && v !== "") avatars[kind] = v;
    }

    // Same for nav-tab icons — keep only the tabs that hold an image (an absent
    // tab keeps its built-in icon). The logo is a single value (absent = built-in
    // mark). Both already passed the shared image gate at upload time.
    const navIcons: Partial<Record<NavIconKey, string>> = {};
    for (const key of NAV_ICON_KEYS) {
      const v = editNavIcons[key];
      if (typeof v === "string" && v !== "") navIcons[key] = v;
    }

    const bundle: ThemeBundle = { id: editId, name: editName, colors };
    if (Object.keys(wording).length > 0) bundle.wording = wording;
    if (Object.keys(fonts).length > 0) bundle.fonts = fonts;
    if (Object.keys(avatars).length > 0) bundle.avatars = avatars;
    if (editLogo !== "") bundle.logo = editLogo;
    if (Object.keys(navIcons).length > 0) bundle.navIcons = navIcons;
    // The outer-canvas tile (T-081b) — absent means "no image", i.e. the plain
    // --color-bg canvas an older bundle has always had.
    if (editCanvasBg !== "") {
      bundle.backgrounds = { canvas: editCanvasBg };
      // Only a NON-default mode is written: a bundle that tiles stays byte-
      // identical to one authored before the field existed.
      if (editCanvasBgMode !== DEFAULT_BACKGROUND_MODE) {
        bundle.backgroundModes = { canvas: editCanvasBgMode };
      }
    }

    const err = validateThemeBundle(bundle);
    if (err) {
      setEditError(err);
      return;
    }
    // ONE theme is written, not the whole set — that is the point of T-83ef.
    try {
      await saveTheme(bundle);
      swatches.remember(bundle);
    } catch (e) {
      // Stay in the editor on a refusal. Returning to the list would throw away
      // everything typed here while showing a list that still holds the OLD
      // colours, i.e. it would look like the save worked.
      setEditError(failureText(e));
      return;
    }
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

  // A new result set starts at the top: the old scroll offset pointed into a
  // list that no longer exists.
  function resetWordingScroll() {
    if (wordingListRef.current) wordingListRef.current.scrollTop = 0;
  }

  const wordingOverrideCount = useMemo(() => {
    let n = 0;
    for (const lang of ["zh", "en"] as const) {
      for (const v of Object.values(editWording[lang] ?? {})) {
        if (typeof v === "string" && v.trim() !== "") n++;
      }
    }
    return n;
  }, [editWording]);

  /** One 用詞 row, at its index in `wordingRows`. */
  function wordingRow(code: string, index: number) {
    const enText = readDictMessage(en, code) ?? "";
    const curText = readDictMessage(DICTS_BY_LANG[wordingLang], code) ?? "";
    const override = editWording[wordingLang]?.[code] ?? "";
    return (
      <div
        key={code}
        className="ts-wording-row"
        role="listitem"
        aria-setsize={wordingRows.length}
        aria-posinset={index + 1}
        data-wording-code={code}
      >
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
  }

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
          {/* ── import from a link (T-29c7) ── */}
          <label className="ts-link-label" htmlFor="ts-import-url">
            {t.profile.themeImportLinkLabel}
          </label>
          <div className="ts-form-actions">
            <input
              id="ts-import-url"
              type="url"
              className="ts-link-input"
              placeholder={t.profile.themeImportLinkPlaceholder}
              aria-label={t.profile.themeImportLinkLabel}
              value={importUrl}
              onChange={(e) => {
                setImportUrl(e.target.value);
                setImportError("");
              }}
            />
            <button
              type="button"
              className="doc-btn"
              disabled={!importUrl.trim() || importUrlBusy}
              onClick={handleImportFromLink}
            >
              {importUrlBusy
                ? t.profile.themeImportLinkWorking
                : t.profile.themeImportFromLink}
            </button>
          </div>
          {/* The share links this box is meant to eat are identity-less and
              never expire. They are no longer beyond recall, though: since
              T-62 a sig is derived from a key in the signing-key RING, so
              removing that key voids every link it signed at once — coarse,
              and never per-link. Whoever pastes one here is also the person
              who can decide whether the theme should be readable by anyone
              holding the URL — so the warning belongs on this screen, not only
              in the docs. */}
          <div className="ts-link-note">{t.profile.themeImportLinkShareNote}</div>
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
                // An empty row is a token still FOLLOWING its default (see
                // openEdit). Picking a colour is what starts it leading.
                const follows = value.trim() === "";
                const alpha = follows ? null : alphaPercent(value);
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
                      placeholder={
                        follows
                          ? `${t.settings.themeColorFollows} ${
                              tokenMeta(
                                THEME_ALIAS_DEFAULT_TOKENS[token],
                                language
                              ).label
                            }`
                          : undefined
                      }
                      aria-label={meta.label}
                      onChange={(e) => setColorAt(i, e.target.value)}
                    />
                    {/* Opacity is a first-class control, not something only a
                        hand-written #RRGGBBAA can reach: a theme that floats the
                        cockpit on a canvas image does it by making these layers
                        translucent (owner 2026-07-27). Hidden for rgb()/hsl()
                        values, whose alpha this editor does not rewrite. */}
                    {alpha !== null && (
                      <label className="ts-color-alpha">
                        <input
                          type="range"
                          min={0}
                          max={100}
                          step={1}
                          value={alpha}
                          aria-label={`${meta.label} ${t.settings.themeColorOpacity}`}
                          onChange={(e) => {
                            const next = withAlphaPercent(
                              value,
                              Number(e.target.value)
                            );
                            if (next !== null) setColorAt(i, next);
                          }}
                        />
                        <span className="ts-color-alpha__value">{alpha}%</span>
                      </label>
                    )}
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

          {/* ── studio logo (工作室 logo) — single top-bar mark image ── */}
          <div className="ts-section-label">{t.settings.themeLogoSection}</div>
          <div className="ts-wording-sub">{t.settings.themeLogoHint}</div>
          <div className="ts-avatar-slots">
            <div className="ts-avatar-slot">
              <div className="ts-avatar-label">{t.settings.themeLogo}</div>
              <div className="ts-avatar-row">
                <span
                  className="avatar ts-avatar-preview"
                  style={{ width: 48, height: 48 }}
                >
                  {editLogo ? (
                    <img
                      className="avatar__img"
                      src={editLogo}
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
                  ref={logoInputRef}
                  type="file"
                  accept="image/png,image/jpeg,image/webp"
                  className="ts-file"
                  aria-label={t.settings.themeLogo}
                  onChange={handleLogoPicked}
                />
                <button
                  type="button"
                  className="doc-btn"
                  onClick={() => logoInputRef.current?.click()}
                >
                  {t.settings.themeAvatarChoose}
                </button>
                {editLogo && (
                  <button type="button" className="doc-btn" onClick={clearLogo}>
                    {t.settings.themeAvatarClear}
                  </button>
                )}
              </div>
            </div>
          </div>

          {/* ── nav-tab icons (導覽圖示) — per-tab icon image upload ── */}
          <div className="ts-section-label">{t.settings.themeNavIconsSection}</div>
          <div className="ts-wording-sub">{t.settings.themeNavIconsHint}</div>
          <div className="ts-avatar-slots">
            {NAV_ICON_SLOTS.map(({ key, labelKey }) => {
              const src = editNavIcons[key];
              return (
                <div key={key} className="ts-avatar-slot">
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
                      ref={navIconInputRefs[key]}
                      type="file"
                      accept="image/png,image/jpeg,image/webp"
                      className="ts-file"
                      aria-label={t.settings[labelKey]}
                      onChange={(e) => handleNavIconPicked(key, e)}
                    />
                    <button
                      type="button"
                      className="doc-btn"
                      onClick={() => navIconInputRefs[key].current?.click()}
                    >
                      {t.settings.themeAvatarChoose}
                    </button>
                    {src && (
                      <button
                        type="button"
                        className="doc-btn"
                        onClick={() => clearNavIcon(key)}
                      >
                        {t.settings.themeAvatarClear}
                      </button>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
          {/* ── outer-canvas background (外框背景) — the ONE zone that takes an
              image; the topbar/nav/main zones stay colour-only (readability) ── */}
          <div className="ts-section-label">{t.settings.themeCanvasBgSection}</div>
          <div className="ts-wording-sub">{t.settings.themeCanvasBgHint}</div>
          <div className="ts-avatar-slots">
            <div className="ts-avatar-slot">
              <div className="ts-avatar-label">{t.settings.themeCanvasBg}</div>
              <div className="ts-avatar-row">
                <span
                  className="avatar ts-avatar-preview"
                  style={{ width: 48, height: 48 }}
                >
                  {editCanvasBg ? (
                    <img
                      className="avatar__img"
                      src={editCanvasBg}
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
                  ref={canvasBgInputRef}
                  type="file"
                  accept="image/png,image/jpeg,image/webp"
                  className="ts-file"
                  aria-label={t.settings.themeCanvasBg}
                  onChange={handleCanvasBgPicked}
                />
                <button
                  type="button"
                  className="doc-btn"
                  onClick={() => canvasBgInputRef.current?.click()}
                >
                  {t.settings.themeAvatarChoose}
                </button>
                {editCanvasBg && (
                  <button type="button" className="doc-btn" onClick={clearCanvasBg}>
                    {t.settings.themeAvatarClear}
                  </button>
                )}
              </div>
              {/* The mode only means anything once there IS an image, so it
                  appears with one rather than sitting there inert (T-081b). */}
              {editCanvasBg && (
                <div className="ts-avatar-row">
                  <label className="ts-avatar-label" htmlFor="ts-canvas-bg-mode">
                    {t.settings.themeCanvasBgMode}
                  </label>
                  <select
                    id="ts-canvas-bg-mode"
                    className="ts-input ts-font-select"
                    value={editCanvasBgMode}
                    onChange={(e) =>
                      setEditCanvasBgMode(e.target.value as BackgroundMode)
                    }
                  >
                    {BACKGROUND_MODES.map((mode) => (
                      <option key={mode} value={mode}>
                        {
                          {
                            tile: t.settings.themeCanvasBgModeTile,
                            sides: t.settings.themeCanvasBgModeSides,
                            cover: t.settings.themeCanvasBgModeCover,
                          }[mode]
                        }
                      </option>
                    ))}
                  </select>
                </div>
              )}
              {editCanvasBg && editCanvasBgMode !== "tile" && (
                <div className="ts-wording-sub">
                  {editCanvasBgMode === "sides"
                    ? t.settings.themeCanvasBgModeHint
                    : t.settings.themeCanvasBgModeCoverHint}
                </div>
              )}
            </div>
          </div>
          {brandError && <div className="set-error">{brandError}</div>}

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
                onClick={() => {
                  setWordingLang(lang);
                  resetWordingScroll();
                }}
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
            onChange={(e) => {
              setWordingSearch(e.target.value);
              resetWordingScroll();
            }}
          />
          {/* Every row is here. `aria-setsize`/`aria-posinset` are still
              explicit rather than left to the AT to count: they are what make
              "第 431 項,共 866 項" true, and they keep the announced position
              tied to the row's index in the CURRENT (possibly searched) set. */}
          <div
            className="ts-wording-list"
            role="list"
            ref={wordingListRef}
            data-wording-total={wordingRows.length}
          >
            {wordingRows.map((code, i) => wordingRow(code, i))}
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

      <div
        className="ts-wording-tabs ts-page-tabs"
        role="tablist"
        aria-label={t.settings.themeManage}
        onKeyDown={(e) => {
          if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
          const next = tab === "appearance" ? "library" : "appearance";
          setTab(next);
          document.getElementById(`ts-tab-${next}`)?.focus();
        }}
      >
        {PAGE_TABS.map(({ id, labelKey }) => (
          <button
            key={id}
            type="button"
            role="tab"
            id={`ts-tab-${id}`}
            aria-selected={tab === id}
            aria-controls={`ts-panel-${id}`}
            tabIndex={tab === id ? 0 : -1}
            className={`ts-tab${tab === id ? " ts-tab--active" : ""}`}
            onClick={() => setTab(id)}
          >
            {t.settings[labelKey]}
          </button>
        ))}
      </div>

      {tab === "appearance" ? (
        <div role="tabpanel" id="ts-panel-appearance" aria-labelledby="ts-tab-appearance">
          <ThemeAppearance {...schedule} swatchOf={swatches.swatchOf} />
        </div>
      ) : (
        <div role="tabpanel" id="ts-panel-library" aria-labelledby="ts-tab-library">
          <div className="ts-toolbar">
            <button
              type="button"
              className="doc-btn doc-btn--accent"
              onClick={handleAddNew}
            >
              {t.profile.themeAdd}
            </button>
            <button type="button" className="doc-btn" onClick={openImport}>
              {t.profile.themeImport}
            </button>
          </div>
          {listError && <div className="set-error">{listError}</div>}
          {importSkipped.length > 0 && (
            <div className="set-warn" data-testid="theme-import-skipped">
              {msg.themeImportSkipped(
                importSkipped.length,
                importSkipped.slice(0, IMPORT_SKIPPED_SAMPLE)
              )}
            </div>
          )}

          {/* 內建 / 自訂 is carried by the GROUP STRUCTURE ALONE, the twin of the
           * 外觀 select's built-ins-first ordering (ThemeOptions) and fed by the same
           * themeMarkers wording. The rows carry no per-row chip:
           * a chip was forgeable on all three of its signals (its TEXT, its COLOUR
           * and the row's own NAME), and once the heading says it, a chip repeating
           * it adds nothing a theme could not already imitate. Which group a row
           * lands in is decided by the code that renders it, not by anything the
           * pack ships (T-081b review round 4, BLOCKER-A). */}
          <div className="ts-list" role="group" aria-labelledby="ts-group-builtin">
            <div className="ts-group-head" id="ts-group-builtin" data-testid="ts-group-builtin">
              {t.themeMarkers.builtinGroup}
            </div>
            {/* built-in rows are downloadable, but not editable/deletable. 下載 exports that row's own palette whatever is
             * applied right now (owner: 辦公室主題不用擋下載); the edit/delete icons
             * stay inert-disabled so built-in and custom rows line up their right
             * edge at every width (owner: 內建列與自訂列對齊). */}
            {BUILTIN_THEMES.map((b) => {
              const name = t.themeIdentity[b.nameKey];
              return (
                <div key={b.id} className="ts-row">
                  <span className="ts-name">{name}</span>
                  <button
                    type="button"
                    className="ts-icon-btn"
                    aria-label={`${t.profile.themeExport} ${name}`}
                    title={t.profile.themeExport}
                    onClick={() =>
                      downloadBundle(exportBuiltinTheme(b.id, `${b.id}-base`, name))
                    }
                  >
                    <DownloadIcon size={15} />
                  </button>
                  <button
                    type="button"
                    className="ts-icon-btn"
                    disabled
                    aria-disabled="true"
                    aria-label={`${t.profile.themeEdit} ${name}`}
                    title={t.profile.themeEdit}
                  >
                    <PencilIcon size={15} />
                  </button>
                  <button
                    type="button"
                    className="ts-icon-btn ts-icon-btn--danger"
                    disabled
                    aria-disabled="true"
                    aria-label={`${t.profile.themeDelete} ${name}`}
                    title={t.profile.themeDelete}
                  >
                    <TrashIcon size={15} />
                  </button>
                </div>
              );
            })}
          </div>

          {themeList.length > 0 && (
            <div className="ts-list" role="group" aria-labelledby="ts-group-custom">
              <div className="ts-group-head" id="ts-group-custom" data-testid="ts-group-custom">
                {t.themeMarkers.customGroup}
              </div>
              {/* A row is one LINE now (id + name) — everything drawn here was
               * always just the name, so nothing on screen changed; what left is
               * the megabytes of embedded images the old set dragged in behind it.
               * 匯出 and 編輯 fetch the bundle on click instead, and are disabled
               * while one is in flight — this is a whole theme pack on the wire, so
               * "nothing happened yet" must be visible rather than guessable. */}
              {themeList.map((b) => (
              <div key={b.id} className="ts-row">
                <span className="ts-name">{b.name}</span>
                <button
                  type="button"
                  className="ts-icon-btn"
                  disabled={busyId !== null}
                  aria-label={`${t.profile.themeExport} ${b.name}`}
                  title={t.profile.themeExport}
                  onClick={() => handleExportTheme(b.id)}
                >
                  <DownloadIcon size={15} />
                </button>
                <button
                  type="button"
                  className="ts-icon-btn"
                  disabled={busyId !== null}
                  aria-label={`${t.profile.themeEdit} ${b.name}`}
                  title={t.profile.themeEdit}
                  onClick={() => handleEditTheme(b.id)}
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
          )}
        </div>
      )}

      {(() => {
        // The confirm only ever needed the NAME, which the list line carries.
        const target = themeList.find((b) => b.id === confirmDeleteId);
        if (!target) return null;
        return (
          <ConfirmModal
            testId="theme-delete-confirm"
            confirmTestId="theme-delete-confirm-btn"
            danger
            body={msg.themeDeleteConfirm(target.name)}
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

const DEFAULT_SCHEDULE_DRAFT: ThemeScheduleEntry[] = [
  { start: "08:00", theme: "office-light" },
  { start: "18:00", theme: "office" },
];

function useThemeScheduleDraft() {
  const { themeSchedule } = useI18n();
  // null = the switch is off. A draft with rows is on but maybe unsaved; the
  // server only accepts it once there are 2..6 rows, so turning on saves nothing.
  const [draft, setDraft] = useState<ThemeScheduleEntry[] | null>(() =>
    themeSchedule.length ? themeSchedule : null
  );
  const savedKey = JSON.stringify(themeSchedule);
  useEffect(() => {
    setDraft(themeSchedule.length ? themeSchedule : null);
    // Keyed on the content: a reconcile re-delivering the same schedule must
    // not throw away rows being edited.
  }, [savedKey]);
  return { draft, setDraft };
}

interface ThemeSwatch {
  bg?: string;
  card?: string;
  text?: string;
  accent?: string;
  image?: string;
}

function swatchOfColors(colors: Record<string, string>): ThemeSwatch {
  return {
    bg: colors["--color-bg"],
    card: colors["--color-card"],
    text: colors["--color-text"],
    accent: colors["--color-accent"],
  };
}

const BUILTIN_SWATCHES: Record<string, ThemeSwatch> = {
  office: swatchOfColors(officeTheme.colors),
  "office-light": swatchOfColors(officeLightTheme.colors),
};

// A custom theme paints over the office base, so whatever it leaves out is office's.
function swatchOfBundle(bundle: ThemeBundle): ThemeSwatch {
  const own = swatchOfColors(bundle.colors);
  const canvas = bundle.backgrounds?.canvas;
  return {
    bg: own.bg ?? BUILTIN_SWATCHES.office.bg,
    card: own.card ?? BUILTIN_SWATCHES.office.card,
    text: own.text ?? BUILTIN_SWATCHES.office.text,
    accent: own.accent ?? BUILTIN_SWATCHES.office.accent,
    image: canvas && isValidBackgroundValue(canvas) ? canvas : undefined,
  };
}

/** Thumbnail colours per theme id. Built-ins are known up front; a custom theme
 * is fetched once per page when one of `wanted` names it, and a failed fetch
 * stays a blank placeholder rather than being retried. */
function useThemeSwatches(wanted: string[]) {
  const { themeList, loadTheme, activeThemeBundle } = useI18n();
  const [loaded, setLoaded] = useState<Record<string, ThemeSwatch | null>>({});
  const inFlight = useRef(new Set<string>());
  const loadRef = useRef(loadTheme);
  loadRef.current = loadTheme;
  const activeRef = useRef(activeThemeBundle);
  activeRef.current = activeThemeBundle;

  const customIds = new Set(themeList.map((b) => b.id));
  const missing = [...new Set(wanted)].filter(
    (id) => customIds.has(id) && !(id in loaded) && !inFlight.current.has(id)
  );
  const missingKey = missing.join("\n");

  useEffect(() => {
    for (const id of missing) {
      const active = activeRef.current;
      if (active && active.id === id) {
        setLoaded((m) => ({ ...m, [id]: swatchOfBundle(active) }));
        continue;
      }
      inFlight.current.add(id);
      loadRef
        .current(id)
        .then(
          (b) => setLoaded((m) => ({ ...m, [id]: swatchOfBundle(b) })),
          (e) => {
            console.warn("theme thumbnail: load failed", id, e);
            setLoaded((m) => ({ ...m, [id]: null }));
          }
        )
        .finally(() => inFlight.current.delete(id));
    }
  }, [missingKey]);

  return {
    swatchOf: (id: string): ThemeSwatch | null =>
      BUILTIN_SWATCHES[id] ?? loaded[id] ?? null,
    remember: (bundle: ThemeBundle) =>
      setLoaded((m) => ({ ...m, [bundle.id]: swatchOfBundle(bundle) })),
  };
}

type ThemeSwatches = ReturnType<typeof useThemeSwatches>;

function ThemeThumb({ swatch }: { swatch: ThemeSwatch | null }) {
  if (!swatch) {
    return <span className="ts-thumb ts-thumb--blank" aria-hidden="true" data-testid="ts-thumb" />;
  }
  return (
    <span
      className="ts-thumb"
      aria-hidden="true"
      data-testid="ts-thumb"
      style={{
        backgroundColor: swatch.bg,
        backgroundImage: swatch.image ? `url("${swatch.image}")` : undefined,
      }}
    >
      <span className="ts-thumb__card" style={{ backgroundColor: swatch.card }}>
        <span className="ts-thumb__line" style={{ backgroundColor: swatch.text }} />
        <span className="ts-thumb__line ts-thumb__line--short" style={{ backgroundColor: swatch.accent }} />
      </span>
    </span>
  );
}

function ThemeOptions() {
  const { t, themeList } = useI18n();
  return (
    <>
      {BUILTIN_THEMES.map((b) => (
        <option key={b.id} value={b.id}>
          {t.themeIdentity[b.nameKey]}
        </option>
      ))}
      {themeList.map((b) => (
        <option key={b.id} value={b.id}>
          {b.name}
        </option>
      ))}
    </>
  );
}

const minutesOf = (start: string) => {
  const m = /^(\d{2}):(\d{2})$/.exec(start);
  return m ? Number(m[1]) * 60 + Number(m[2]) : null;
};

function ScheduleTimeline({
  rows,
  swatchOf,
}: {
  rows: ThemeScheduleEntry[];
  swatchOf: ThemeSwatches["swatchOf"];
}) {
  const marks = rows
    .map((r) => ({ at: minutesOf(r.start), start: r.start, theme: r.theme }))
    .filter((m): m is { at: number; start: string; theme: string } => m.at !== null)
    .sort((a, b) => a.at - b.at);
  if (!marks.length) return null;
  const DAY = 24 * 60;
  const last = marks[marks.length - 1];
  const segments = [
    ...(marks[0].at > 0 ? [{ from: 0, to: marks[0].at, theme: last.theme }] : []),
    ...marks.map((m, i) => ({ from: m.at, to: marks[i + 1]?.at ?? DAY, theme: m.theme })),
  ].filter((seg) => seg.to > seg.from);
  return (
    <div className="ts-timeline" aria-hidden="true" data-testid="ts-timeline">
      <div className="ts-timeline__bar">
        {segments.map((seg, i) => (
          <span
            key={i}
            className="ts-timeline__seg"
            style={{
              width: `${((seg.to - seg.from) / DAY) * 100}%`,
              backgroundColor: swatchOf(seg.theme)?.bg,
            }}
          />
        ))}
      </div>
      <div className="ts-timeline__labels">
        {marks.map((m, i) => (
          <span
            key={i}
            className="ts-timeline__label"
            style={{
              left: `${(m.at / DAY) * 100}%`,
              transform: `translateX(${m.at < DAY * 0.05 ? 0 : m.at > DAY * 0.95 ? -100 : -50}%)`,
            }}
          >
            {m.start}
          </span>
        ))}
      </div>
    </div>
  );
}

function ThemeAppearance({
  draft,
  setDraft,
  swatchOf,
}: ReturnType<typeof useThemeScheduleDraft> & { swatchOf: ThemeSwatches["swatchOf"] }) {
  const { t, theme, setTheme, themeSchedule, setThemeSchedule, seasonalTheme, setSeasonalTheme } =
    useI18n();
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState<"" | "saved" | "start" | "duplicate" | "failed">("");

  const save = async (next: ThemeScheduleEntry[]) => {
    const problem = themeScheduleProblem(next);
    if (problem === "start" || problem === "duplicate") {
      setStatus(problem);
      return;
    }
    setBusy(true);
    setStatus("");
    try {
      await setThemeSchedule(next);
      setStatus(next.length ? "saved" : "");
    } catch (e) {
      console.warn("theme schedule: save failed", e);
      setStatus("failed");
    } finally {
      setBusy(false);
    }
  };

  // A saved schedule switches off when the server has cleared it (the draft
  // hook's effect); a failed clear leaves the switch on.
  const toggleSchedule = () => {
    setStatus("");
    if (draft === null) {
      setDraft(DEFAULT_SCHEDULE_DRAFT);
    } else if (themeSchedule.length) {
      void save([]);
    } else {
      setDraft(null);
    }
  };

  const editRow = (i: number, patch: Partial<ThemeScheduleEntry>) => {
    setStatus("");
    setDraft((rows) => rows && rows.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  };

  return (
    <div className="param-card ts-appearance">
      <div className="param-row">
        <div className="param-row__body">
          <span className="param-row__name">{t.settings.themeSchedule}</span>
          <div className="param-row__sub">{t.settings.themeScheduleHint}</div>
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={draft !== null}
          aria-label={t.settings.themeSchedule}
          className={`set-toggle${draft !== null ? " set-toggle--on" : ""}`}
          disabled={busy}
          onClick={toggleSchedule}
        >
          <span className="set-toggle__knob" />
        </button>
      </div>

      {draft ? (
        <div className="param-row param-row--stacked ts-schedule" data-testid="theme-schedule">
          <ScheduleTimeline rows={draft} swatchOf={swatchOf} />
          <div className="ts-list">
            {draft.map((row, i) => (
              <div key={i} className="ts-row ts-schedule-row">
                <input
                  type="time"
                  className="ts-input ts-schedule-start"
                  aria-label={`${t.settings.themeScheduleStart} ${i + 1}`}
                  value={row.start}
                  onChange={(e) => editRow(i, { start: e.target.value })}
                />
                <ThemeThumb swatch={swatchOf(row.theme)} />
                <select
                  className="ts-input"
                  aria-label={`${t.settings.themeScheduleTheme} ${i + 1}`}
                  value={row.theme}
                  onChange={(e) => editRow(i, { theme: e.target.value })}
                >
                  <ThemeOptions />
                </select>
                <button
                  type="button"
                  className="ts-icon-btn ts-icon-btn--danger"
                  aria-label={`${t.settings.themeScheduleRemove} ${i + 1}`}
                  title={t.settings.themeScheduleRemove}
                  disabled={draft.length <= MIN_SCHEDULE_ENTRIES}
                  onClick={() => {
                    setStatus("");
                    setDraft(draft.filter((_, j) => j !== i));
                  }}
                >
                  <TrashIcon size={15} />
                </button>
              </div>
            ))}
          </div>
          <div className="ts-toolbar ts-schedule-actions">
            <button
              type="button"
              className="doc-btn"
              disabled={draft.length >= MAX_SCHEDULE_ENTRIES}
              onClick={() => {
                setStatus("");
                setDraft([...draft, { start: "", theme: "office" }]);
              }}
            >
              {t.settings.themeScheduleAdd}
            </button>
            <button
              type="button"
              className="doc-btn doc-btn--accent"
              disabled={busy}
              onClick={() => void save(draft)}
            >
              {t.settings.themeScheduleSave}
            </button>
          </div>
          {status === "saved" && <div className="ts-link-note">{t.settings.themeScheduleSaved}</div>}
          {status === "start" && <div className="set-error">{t.settings.themeScheduleErrorStart}</div>}
          {status === "duplicate" && (
            <div className="set-error">{t.settings.themeScheduleErrorDuplicate}</div>
          )}
          {status === "failed" && <div className="set-error">{t.settings.themeScheduleSaveFailed}</div>}
        </div>
      ) : (
        <div className="param-row">
          <div className="param-row__body">
            <span className="param-row__name">{t.settings.themePick}</span>
            <div className="param-row__sub">{t.settings.themePickHint}</div>
          </div>
          <ThemeThumb swatch={swatchOf(theme)} />
          {/* Built-ins are written out first and each option is the theme's own
           * name only, so no pack name can pass itself off as a built-in or
           * move ahead of one (T-081b review round 3, BLOCKER-2). */}
          <select
            className="ts-input ts-theme-select"
            aria-label={t.settings.themePick}
            value={theme}
            onChange={(e) => setTheme(e.target.value)}
          >
            <ThemeOptions />
          </select>
        </div>
      )}

      <div className="param-row">
        <div className="param-row__body">
          <span className="param-row__name">{t.settings.themeSeasonal}</span>
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={seasonalTheme}
          aria-label={t.settings.themeSeasonal}
          className={`set-toggle${seasonalTheme ? " set-toggle--on" : ""}`}
          onClick={() => setSeasonalTheme(!seasonalTheme)}
        >
          <span className="set-toggle__knob" />
        </button>
      </div>
    </div>
  );
}
