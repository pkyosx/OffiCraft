import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { useI18n } from "../i18n";
import { en } from "../i18n/locales/en";
import { zh } from "../i18n/locales/zh";
import { readDictMessage } from "../i18n/wording";
import { MESSAGE_KEYS } from "../i18n/messageKeys.generated";
import {
  MAX_CUSTOM_THEMES,
  AVATAR_KINDS,
  NAV_ICON_KEYS,
  isValidAvatarValue,
  validateThemeBundle,
  BACKGROUND_MODES,
  DEFAULT_BACKGROUND_MODE,
  type AvatarKind,
  type BackgroundMode,
  type NavIconKey,
  type ThemeBundle,
} from "../lib/themeBundle";
import { SAFE_FONT_FAMILIES } from "../styles/themeFonts.generated";
import { THEME_ALIAS_DEFAULT_TOKENS } from "../styles/themeTokens.generated";
import {
  bundleFilename,
  exportOfficeBaseTheme,
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

// How many skipped wording codes the import warning names before the count
// carries the rest — a pack can skip dozens, and the banner must stay one line.
const IMPORT_SKIPPED_SAMPLE = 3;

// ── 用詞 list windowing (T-8115) ──
// The 用詞 panel offers EVERY overridable message code — ~870 of them. It was
// mounting all of them at once, so opening the theme editor built ~870
// controlled <input>s.
//
// WHAT THAT ACTUALLY COST — measured, and the two environments are NOT the
// same story, so keep them apart:
//   * jsdom (ThemeSettings.test.tsx): 5.13s → 0.543s, ~9x. This is where the
//     pain was real enough to fail a build — the file was hitting the 5000ms
//     per-test timeout. The mechanism is quadratic and specific to jsdom:
//     dom-testing-library's getByLabelText reads `input.labels` on every
//     labelable element, and jsdom answers each of those by walking the whole
//     document for form controls, so N inputs cost N document walks per query
//     (--cpu-prof top-2 self time: NodeList-impl._update 1145ms +
//     form-controls.query 551ms; React's own render+commit was only 579ms).
//   * a real browser (Chromium CT, M5 Mac, 4 runs each): opening the editor
//     goes ~80ms → ~44ms. Real and in the right direction, but that is about
//     a frame and a half, not a visibly slow editor. Per-keystroke latency in
//     a 用詞 input actually got slightly WORSE, 6.5ms → 7.4ms (the measuring
//     effect below runs on every commit). Scrolling is unchanged, rAF-bound.
// So: do NOT repeat the ~9x number as if a user feels it — it is a jsdom
// number. The honest case for this change is (a) it unblocks a test file that
// was timing out, (b) ~1.8x off the editor's open cost, which matters more on
// a slower machine than the one those numbers came from, and (c) the DOM it
// builds is proportional to what is on screen.
//
// The list is — and always was — a SCROLL WINDOW (theme-settings.css
// .ts-wording-list: max-height 340px; overflow-y auto). So we render only the
// rows that window can actually show, plus an overscan margin, and reserve the
// exact pixel height of the rows above and below with two spacers. Nothing is
// hidden: the scrollbar still spans all ~870 codes, every code is still one
// scroll away, and a search still yields every match it ever did.
//
// The overscan is not just a repaint smoothness knob — it is what keeps
// keyboard traversal working. Tab moves focus to the next rendered input; the
// browser scrolls it into view, our onScroll advances the window, and a fresh
// overscan row appears below. As long as some rows are always rendered past
// the viewport edge, Tab walks the whole list exactly as it did before.
//
// A row that holds keyboard focus stays mounted even after it scrolls out of
// the window (see `focusedWordingCode` below) — otherwise unmounting it hands
// focus back to <body> mid-edit and the caret is simply gone.
//
// The two px numbers below are only the fallback for when layout is
// unavailable (jsdom, or the first paint before we have measured). In a real
// browser the effect below replaces them with what the stylesheet actually
// produced, so a font-size change in theme-settings.css cannot desync them.
// Both are MEASURED values, not derivations — 48 is what Chromium lays a row
// out at (6+6 row padding + a 36px content box: the two-line meta column is
// 36px and outgrows the 34px input next to it).
const WORDING_ROW_PITCH_PX = 48; // .ts-wording-row, measured in Chromium
const WORDING_VIEWPORT_PX = 340; // .ts-wording-list max-height
const WORDING_OVERSCAN_ROWS = 6;

// The two font tokens the editor offers a dropdown for (T-16a1 P4). Body =
// --font-sans (interface text), Title = --font-title (page headings). The
// options come from the safe-family allowlist; "" = keep the theme default.
const FONT_SLOTS = [
  { token: "--font-sans", labelKey: "themeFontBody" },
  { token: "--font-title", labelKey: "themeFontTitle" },
] as const;

// The four avatar slots the editor offers (T-16a1 P5; T-ea81): 正職 member /
// 外包 outsource / owner CEO / assistant 助理. Each accepts one uploaded image
// (validated client-side, embedded as a base64 data URI so it travels inside
// the bundle).
type AvatarLabelKey =
  | "themeAvatarMember"
  | "themeAvatarOutsource"
  | "themeAvatarOwner"
  | "themeAvatarAssistant";
const AVATAR_SLOTS: { kind: AvatarKind; labelKey: AvatarLabelKey }[] = [
  { kind: "member", labelKey: "themeAvatarMember" },
  { kind: "outsource", labelKey: "themeAvatarOutsource" },
  { kind: "owner", labelKey: "themeAvatarOwner" },
  { kind: "assistant", labelKey: "themeAvatarAssistant" },
];

// The five nav-tab icon slots the editor offers (T-ea81), keyed on the five
// main nav tabs. Same upload flow as avatars (shared image gate); stored on
// bundle.navIcons[key].
type NavIconLabelKey =
  | "themeNavOffice"
  | "themeNavReplies"
  | "themeNavTasks"
  | "themeNavMonitor"
  | "themeNavGuide";
const NAV_ICON_SLOTS: { key: NavIconKey; labelKey: NavIconLabelKey }[] = [
  { key: "office", labelKey: "themeNavOffice" },
  { key: "replies", labelKey: "themeNavReplies" },
  { key: "tasks", labelKey: "themeNavTasks" },
  { key: "monitor", labelKey: "themeNavMonitor" },
  { key: "guide", labelKey: "themeNavGuide" },
];

/**
 * 主題管理 — the SettingsPage 主題 sub-section (T-16a1 P3b). All theme MANAGEMENT
 * lives here (owner IA: 偏好=選擇, 設定=管理): add / import / export / edit
 * (friendly colours + 用詞 overlay) / delete. The ProfileDropdown keeps only the
 * theme SELECTOR + language.
 */
export function ThemeSettings({ crumbs }: { crumbs: Crumb[] }) {
  const { t, msg, theme, setTheme, language, customThemes, commitCustomThemes } =
    useI18n();

  const [view, setView] = useState<View>("list");

  // ── import state ──
  const [importText, setImportText] = useState("");
  const [importError, setImportError] = useState("");
  // The unrecognised wording codes the last import dropped. An import with such
  // codes SUCCEEDS (owner ruling 2026-07-27) — this is what makes the drop
  // visible instead of silent, and it is shown on the list the import lands on.
  const [importSkipped, setImportSkipped] = useState<string[]>([]);
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
    owner: useRef<HTMLInputElement>(null),
    assistant: useRef<HTMLInputElement>(null),
  };
  // Studio logo (T-ea81): a single embedded base64 data URI ("" = none, falls
  // back to the built-in mark). Per-nav-tab icons: key → embedded data URI.
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
    monitor: useRef<HTMLInputElement>(null),
    guide: useRef<HTMLInputElement>(null),
  };
  const [wordingLang, setWordingLang] = useState<"zh" | "en">("zh");
  const [wordingSearch, setWordingSearch] = useState("");
  // Where the 用詞 scroll window currently sits, and how big it is. See the
  // WORDING_* constants above for why the list is windowed at all.
  const wordingListRef = useRef<HTMLDivElement>(null);
  const [wordingScrollTop, setWordingScrollTop] = useState(0);
  // The code whose input currently holds focus, or "" when focus is elsewhere.
  // Unmounting a focused input drops focus to <body> — the caret vanishes and
  // typing goes nowhere — so this row is kept mounted no matter where the
  // window is (see `wordingPinned`).
  const [focusedWordingCode, setFocusedWordingCode] = useState("");
  const [wordingMetrics, setWordingMetrics] = useState({
    pitch: WORDING_ROW_PITCH_PX,
    viewport: WORDING_VIEWPORT_PX,
  });
  const [editError, setEditError] = useState("");

  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);
  const [addError, setAddError] = useState("");

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
  // straight into edit so the owner names + tweaks the fresh copy. The office
  // BASE palette is read via exportOfficeBaseTheme (which neutralises any active
  // custom theme's overrides), so the copy starts from office colours even when
  // the currently applied theme is a custom one. ──
  function handleAddNew() {
    if (customThemes.length >= MAX_CUSTOM_THEMES) {
      setAddError(t.profile.themeLimitReached);
      return;
    }
    const id = nextCustomThemeId(customThemes.map((b) => b.id));
    const bundle = exportOfficeBaseTheme(id, t.themeIdentity.newTheme);
    commitCustomThemes([...customThemes, bundle]);
    setAddError("");
    openEdit(bundle);
  }

  // ── import ──
  function openImport() {
    setImportText("");
    setImportError("");
    setImportSkipped([]);
    setAddError("");
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
    setImportSkipped(res.skippedWording);
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
  // server enforces, the same one avatars / logo / nav-icons all reuse; the
  // image safety gate is NOT relaxed for any of them). Returns the validated
  // data URI, or null when the file is unreadable or fails validation (never a
  // silent bad value in the bundle).
  async function readValidatedImage(file: File): Promise<string | null> {
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
    return isValidAvatarValue(dataUri) ? dataUri : null;
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
    const dataUri = await readValidatedImage(file);
    if (dataUri === null) {
      setBrandError(t.settings.themeAvatarInvalid);
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

  function handleSaveEdit() {
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

  // Replace the fallback px numbers with what the stylesheet actually laid out.
  // Runs on every commit because a font/zoom/width change moves both numbers,
  // and it only writes state when a number really moved. Where there is no
  // layout at all (jsdom, or before first paint) both measurements read 0 and
  // the stylesheet's own numbers stand — which is also what a browser produces.
  useLayoutEffect(() => {
    const list = wordingListRef.current;
    if (!list) return;
    // In-flow rows only: the pinned row is positioned absolutely, so its
    // offsetTop is an answer to a different question.
    const rows = list.querySelectorAll<HTMLElement>(
      ".ts-wording-row:not(.ts-wording-row--pinned)"
    );
    // Pitch from two adjacent rows, so any row gap is included by construction.
    const pitch =
      rows.length >= 2
        ? rows[1].offsetTop - rows[0].offsetTop
        : (rows[0]?.offsetHeight ?? 0);
    const viewport = list.clientHeight;
    const next = {
      pitch: pitch > 0 ? pitch : WORDING_ROW_PITCH_PX,
      viewport: viewport > 0 ? viewport : WORDING_VIEWPORT_PX,
    };
    setWordingMetrics((prev) =>
      prev.pitch === next.pitch && prev.viewport === next.viewport ? prev : next
    );
  });

  // The slice of `wordingRows` that is actually mounted. Everything outside it
  // is represented by the two spacers, so the scroll range covers all of them.
  const wordingWindow = useMemo(() => {
    const { pitch, viewport } = wordingMetrics;
    const total = wordingRows.length;
    const visible = Math.ceil(viewport / pitch) + WORDING_OVERSCAN_ROWS * 2;
    const wanted = Math.floor(wordingScrollTop / pitch) - WORDING_OVERSCAN_ROWS;
    // Clamp so a scrollTop left over from a longer list cannot empty the window.
    const first = Math.max(0, Math.min(wanted, total - visible));
    const last = Math.min(total, first + visible);
    return { first, last, padTop: first * pitch, padBottom: (total - last) * pitch };
  }, [wordingMetrics, wordingScrollTop, wordingRows.length]);

  // The focused row when the window has scrolled past it. Rendering it as well
  // — absolutely positioned at the offset it would have had — is what keeps
  // the caret alive: React would otherwise unmount the element focus lives in,
  // and the browser answers that by moving focus to <body>. It is also where
  // the row genuinely belongs, so scrolling back reveals it in place with no
  // duplicate (the `< first || >= last` test is what excludes the duplicate).
  const wordingPinned = useMemo(() => {
    if (focusedWordingCode === "") return null;
    const index = wordingRows.indexOf(focusedWordingCode);
    if (index < 0) return null; // filtered out by a search — nothing to pin
    if (index >= wordingWindow.first && index < wordingWindow.last) return null;
    return { code: focusedWordingCode, index };
  }, [focusedWordingCode, wordingRows, wordingWindow]);

  // A new result set starts at the top — both our window and the real element,
  // which would otherwise keep a scroll offset that no longer means anything.
  function resetWordingScroll() {
    setWordingScrollTop(0);
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

  /** One 用詞 row, at its absolute index in `wordingRows`. `pinned` rows are
   * the same row taken out of flow and placed at the offset the spacer above
   * is already reserving for them, so a focused row that has scrolled out of
   * the window stays in the document without displacing anything. */
  function wordingRow(code: string, index: number, pinned: boolean) {
    const enText = readDictMessage(en, code) ?? "";
    const curText = readDictMessage(DICTS_BY_LANG[wordingLang], code) ?? "";
    const override = editWording[wordingLang]?.[code] ?? "";
    return (
      <div
        key={code}
        className={`ts-wording-row${pinned ? " ts-wording-row--pinned" : ""}`}
        role="listitem"
        aria-setsize={wordingRows.length}
        aria-posinset={index + 1}
        data-wording-code={code}
        style={pinned ? { top: index * wordingMetrics.pitch } : undefined}
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
          onFocus={() => setFocusedWordingCode(code)}
          onBlur={() =>
            setFocusedWordingCode((prev) => (prev === code ? "" : prev))
          }
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
          {/* role/aria-setsize: only the windowed rows are in the a11y tree,
              so without the set size a screen reader would report the list as
              ~21 items long instead of ~870. The positions are 1-based and
              absolute, so "第 431 項,共 866 項" stays true while scrolling. */}
          <div
            className="ts-wording-list"
            role="list"
            ref={wordingListRef}
            data-wording-total={wordingRows.length}
            onScroll={(e) => setWordingScrollTop(e.currentTarget.scrollTop)}
          >
            {wordingWindow.padTop > 0 && (
              <div
                className="ts-wording-pad"
                aria-hidden="true"
                style={{ height: wordingWindow.padTop }}
              />
            )}
            {/* The pinned row must sit in the SAME keyed array as the windowed
                rows, not in a slot of its own. React reconciles children slot
                by slot, so a row that scrolls out of the window and into a
                separate slot is torn down and rebuilt — which loses exactly
                the focus the pin exists to keep. Inside one array its key
                carries it, and React only moves the node it already has. */}
            {[
              ...wordingRows
                .slice(wordingWindow.first, wordingWindow.last)
                .map((code, i) =>
                  wordingRow(code, wordingWindow.first + i, false)
                ),
              ...(wordingPinned
                ? [wordingRow(wordingPinned.code, wordingPinned.index, true)]
                : []),
            ]}
            {wordingWindow.padBottom > 0 && (
              <div
                className="ts-wording-pad"
                aria-hidden="true"
                style={{ height: wordingWindow.padBottom }}
              />
            )}
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
      {addError && <div className="set-error">{addError}</div>}
      {importSkipped.length > 0 && (
        <div className="set-warn" data-testid="theme-import-skipped">
          {msg.themeImportSkipped(
            importSkipped.length,
            importSkipped.slice(0, IMPORT_SKIPPED_SAMPLE)
          )}
        </div>
      )}

      {/* 內建 / 自訂 is carried by the GROUP STRUCTURE ALONE, the twin of the
       * quick picker's ordering (ProfileDropdown.tsx) and fed by the same
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
        {/* built-in: office is the only built-in — selectable and downloadable,
         * but not editable/deletable. Its download icon exports the office base
         * palette (owner: 辦公室主題不用擋下載); the edit/delete icons stay
         * inert-disabled so the built-in and custom rows still line up their right
         * edge at every width (owner: 內建列與自訂列對齊). */}
        <div className="ts-row">
          <button
            type="button"
            className={`ts-pick${theme === "office" ? " ts-pick--active" : ""}`}
            onClick={() => setTheme("office")}
          >
            {t.themeIdentity.office}
          </button>
          <button
            type="button"
            className="ts-icon-btn"
            aria-label={`${t.profile.themeExport} ${t.themeIdentity.office}`}
            title={t.profile.themeExport}
            onClick={() =>
              downloadBundle(
                exportOfficeBaseTheme("office-base", t.themeIdentity.office)
              )
            }
          >
            <DownloadIcon size={15} />
          </button>
          <button
            type="button"
            className="ts-icon-btn"
            disabled
            aria-disabled="true"
            aria-label={`${t.profile.themeEdit} ${t.themeIdentity.office}`}
            title={t.profile.themeEdit}
          >
            <PencilIcon size={15} />
          </button>
          <button
            type="button"
            className="ts-icon-btn ts-icon-btn--danger"
            disabled
            aria-disabled="true"
            aria-label={`${t.profile.themeDelete} ${t.themeIdentity.office}`}
            title={t.profile.themeDelete}
          >
            <TrashIcon size={15} />
          </button>
        </div>
      </div>

      {customThemes.length > 0 && (
        <div className="ts-list" role="group" aria-labelledby="ts-group-custom">
          <div className="ts-group-head" id="ts-group-custom" data-testid="ts-group-custom">
            {t.themeMarkers.customGroup}
          </div>
          {customThemes.map((b) => (
          <div key={b.id} className="ts-row">
            <button
              type="button"
              className={`ts-pick${theme === b.id ? " ts-pick--active" : ""}`}
              onClick={() => setTheme(b.id)}
            >
              {b.name}
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
      )}

      {(() => {
        const target = customThemes.find((b) => b.id === confirmDeleteId);
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
