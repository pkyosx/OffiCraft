// LorePage — the 傳承 tab. `#lore` is a directory of boot-document targets
// (所有人, each member, each task manual) with their budget numbers; each target
// opens its own section page (`#lore/<kind>/<key>`) holding that section's cards
// and filters.
//
// 🔴 EVERY BUDGET NUMBER IS THE SERVER'S. The section headers read
// `listLoreBudgets`, a section page's sentence reads the
// `capChars`/`usedChars`/`budgetSegments` its own list page answered, and a
// card is folded (and greyed once opened) exactly when the server marked it
// `dropped`. Adding up title+body lengths, or greying by position, would be a
// second copy of the server's picking walk, wrong on every page after the
// first, after any filter, and for a member whose 所有人 entries interleave
// with its own.
//
// 🔴 THE FILTER TRAVELS WITH THE PAGE REQUEST, never as a `.filter()` over
// downloaded rows; a client-side filter is right only for the rows that
// happen to be downloaded.
//
// 🔴 狀態、類型、適用範圍 ARE STAGED, NOT SENT. The card shows the staged
// value in a warn-coloured frame; 儲存變更 sends them all, then the whole
// section is re-read in the server's order (as many rows as were loaded)
// together with the numbers. Until then a card stays greyed (or not) as the
// server last said; if that re-read fails, the cards keep the values that went
// through and the page asks for a reload. 編輯 and 還原 still save at once and
// re-read the section the same way; staged values outlive that re-read.

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useI18n } from "../i18n";
import { taskTypeLabel } from "../lib/taskTypeLabel";
import { api, viewerMaySetLoreScope } from "../api";
import type {
  ChatAttachmentInput,
  LoreBudgetTargetView,
  LoreEntryEdit,
  LoreEntryPageView,
  LoreEntryState,
  LoreEntryView,
  LoreListOptions,
  LoreScopeKind,
  LoreType,
} from "../api/adapter";
import { isHttpStatus, serverMessageOf } from "../api/errors";
import { LORE_CAP_CHARS_DEFAULTS } from "../api/loreCap";
import { LORE_TYPES } from "../api/loreType";
import { loadServerSettings } from "../hooks/sharedServerSettings";
import { useMembers } from "../hooks/useMembers";
import { useOutsourceWorkers } from "../hooks/useOutsourceWorkers";
import { useTaskManuals } from "../hooks/useTaskManuals";
import { useIsMobile } from "../hooks/useIsMobile";
import { useSuggestedRepliesLoreMessage } from "../hooks/useSuggestedReplies";
import {
  ATTACH_ACCEPT,
  STAGING_TARGET_PER_MOUNT,
  useAttachmentStaging,
} from "../hooks/useAttachmentStaging";
import {
  useWorkerAvatarUrls,
  useWorkerCodenames,
} from "../hooks/useWorkerCodenames";
import { avatarKindForMember } from "../lib/avatarKind";
import { isSyntheticSender } from "../lib/syntheticSender";
import type { AvatarKind } from "../lib/themeBundle";
import { formatAbsolute } from "../lib/dateFormat";
import { autosizeTextarea } from "../lib/autosize";
import { registerUnsavedWork } from "../lib/unsavedInput";
import { enterShouldSend } from "../lib/composerKeys";
import { isCardToggleClick, isCardToggleKey } from "../lib/cardToggle";
import {
  formatHash,
  navigateHash,
  setNavigationGuard,
  useHashRoute,
  type HashRoute,
  type LoreSectionKind,
} from "../lib/hashRoute";
import { Avatar } from "./Avatar";
import { HeadingBody } from "./ChatGroupHeading";
import { ComposerAttachmentPreview } from "./ComposerAttachmentPreview";
import { DocumentHistoryEntry } from "./DocumentHistoryEntry";
import { FilterField, FilterPanel } from "./FilterPanel";
import { IdFilterInput } from "./IdFilterInput";
import { InstantHint } from "./InstantHint";
import { Markdown } from "./Markdown";
import { MultiSelectFilter } from "./MultiSelectFilter";
import { SuggestedReplies, appendSuggestion } from "./SuggestedReplies";
import {
  BookIcon,
  CheckIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  ChevronsDownUpIcon,
  ChevronsUpDownIcon,
  CircleCheckBigIcon,
  CircleSlashIcon,
  PaperclipIcon,
  PencilIcon,
  PinIcon,
  RotateCcwIcon,
  SendIcon,
  UsersIcon,
} from "./icons";
import { CopyIdChip } from "./IdChips";
import "./lore.css";
// The message box wears the 任務卡 box's classes: .chat__input / .chat__attach
// and ComposerAttachmentPreview's .chat__preview-* from office.css, the row and
// 送出 from card-composer.css.
import "./office.css";
import "./card-composer.css";
// The body renders through <Markdown>'s `.doc-md` skin from settings.css;
// imported here rather than relied on transitively (css-layout-traps.md).
import "./settings.css";
import "./chat-groups.css";

/** 捲到底一次載這麼多. A page shorter than this is the last one. */
const PAGE = 30;

/** The server's largest `limit` on GET /api/lore. */
const MAX_LIST_LIMIT = 200;

/** A section's bottom is "reached" within this many px of the scrollport's
 * bottom (the same margin ChatArea uses). */
const NEAR_BOTTOM_PX = 120;

/** The state-change menu, in the order the owner asked for: 失效 / 生效 / 置頂. */
const STATE_ACTIONS: readonly LoreEntryState[] = ["retired", "active", "pinned"];

/** What the 狀態 filter starts on: retired entries stay hidden until ticked. */
const DEFAULT_STATES: readonly LoreEntryState[] = ["pinned", "active"];

type DirGroupKind = "everyone" | "staff" | "outsource" | "manual";
const DIR_GROUPS: readonly DirGroupKind[] = ["everyone", "staff", "outsource", "manual"];
/** A first visit opens only 所有人. */
const DIR_DEFAULT_COLLAPSED: readonly DirGroupKind[] = ["staff", "outsource", "manual"];
const DIR_COLLAPSED_KEY = "oc_lore_dir_collapsed";

/** 生效期: the start, and for a retired entry whose retire time is known, the
 * end as well. A retired entry bumped after it was retired starts after it
 * ended, so it shows the start alone. */
function loreEffectiveText(entry: LoreEntryView): string {
  const now = Date.now() / 1000;
  const start = formatAbsolute(entry.effectiveTs, now);
  if (entry.state !== "retired" || entry.retiredTs === undefined || entry.retiredTs < entry.effectiveTs) {
    return start;
  }
  return `${start} – ${formatAbsolute(entry.retiredTs, now)}`;
}

/** The folded directory groups, kept in this browser as the list of folded
 * kinds; nothing saved yet means the first-visit default. */
function readDirCollapsed(): ReadonlySet<DirGroupKind> {
  try {
    const raw = window.localStorage.getItem(DIR_COLLAPSED_KEY);
    if (raw !== null) {
      const parsed: unknown = JSON.parse(raw);
      if (Array.isArray(parsed)) {
        return new Set(DIR_GROUPS.filter((k) => parsed.includes(k)));
      }
    }
  } catch {
    /* storage unavailable or garbled — fall back to the default */
  }
  return new Set(DIR_DEFAULT_COLLAPSED);
}

function writeDirCollapsed(collapsed: ReadonlySet<DirGroupKind>): void {
  try {
    window.localStorage.setItem(DIR_COLLAPSED_KEY, JSON.stringify(DIR_GROUPS.filter((k) => collapsed.has(k))));
  } catch {
    /* storage unavailable — the folding still holds for this visit */
  }
}

/** How one author id resolves against the LIVE roster. `peerId === ""` means
 * "not reachable": the entry then has no message box. */
interface AuthorIdentity {
  /** The label lists and badges use (外包 · 代號). */
  text: string;
  /** The 撰寫人 chip's bare name (owner): no 外包 prefix. */
  name: string;
  peerId: string;
}

/** `roster` is set while the roster has not answered (or failed to): until
 * then a live author cannot be told from an absent one, so nobody is picked. */
interface MessageRecipient {
  id: string;
  name: string;
  roster?: "loading" | "error";
}

interface ScopeChoice {
  kind: LoreScopeKind;
  label: string;
}

interface ScopeMenu {
  choices: ScopeChoice[];
  defaultKind: LoreScopeKind;
}

type CapInfo = Pick<LoreEntryPageView, "capChars" | "usedChars" | "budgetSegments">;

const NO_CAP: CapInfo = {
  capChars: 0,
  usedChars: 0,
  budgetSegments: [],
};

function capOf(page: LoreEntryPageView): CapInfo {
  return {
    capChars: page.capChars,
    usedChars: page.usedChars,
    budgetSegments: page.budgetSegments,
  };
}

interface SectionData {
  entries: LoreEntryView[];
  /** The server's count for the current filter (all pages); null until page one lands or when the server omits it. */
  total: number | null;
  cap: CapInfo;
  loading: boolean;
  fetchingMore: boolean;
  error: boolean;
  exhausted: boolean;
  /** The last next-page read failed; the next scroll retries it. */
  moreFailed: boolean;
}

interface SectionDef {
  key: string;
  kind: LoreScopeKind;
  scopeKey: string;
  target: LoreBudgetTargetView;
}

function sectionKeyOf(kind: string, key: string): string {
  return `${kind}:${key}`;
}

/** The scope half of a section's request. 所有人 carries no key, so a member's
 * section asks for 所有人 too and gets the member's whole boot list, interleaved. */
function scopeOpts(def: { kind: LoreScopeKind; scopeKey: string }): LoreListOptions {
  if (def.kind === "everyone") return { scopeKinds: ["everyone"] };
  if (def.kind === "agent") return { scopeKinds: ["everyone", "agent"], scopeKeys: [def.scopeKey] };
  return { scopeKinds: [def.kind], scopeKeys: [def.scopeKey] };
}

/** Live entries across the directory, for a server without `total`. A member's
 * count already includes the 所有人 entries its boot document walks, so those
 * are counted once. */
function directoryEntryTotal(defs: SectionDef[]): number {
  const everyone = defs.find((d) => d.kind === "everyone")?.target.entryCount ?? 0;
  return defs.reduce(
    (n, d) => n + (d.kind === "agent" ? Math.max(0, d.target.entryCount - everyone) : d.target.entryCount),
    0,
  );
}

/** `212`, `L-212` and `l-212` all mean L-212; null when the text is not an id. */
function normalizeEntryId(raw: string): string | null {
  const v = raw.trim();
  if (/^\d+$/.test(v)) return `L-${v}`;
  const m = /^l-(\d+)$/i.exec(v);
  return m ? `L-${m[1]}` : null;
}

/** Mirrors the server's shrink-only cap: a field over its cap may be saved
 * only when it comes out shorter than what is stored. */
function editFieldBlocked(next: string, stored: string, capChars: number): boolean {
  const n = [...next].length;
  return next !== stored && n > capChars && n >= [...stored].length;
}

/** Auto-paging for a highlighted entry stops after this many extra pages. */
const HIGHLIGHT_PAGE_CAP = 10;

/** The row element for an entry inside the page. */
function rowElement(root: HTMLElement | null, id: string): HTMLElement | null {
  if (!root) return null;
  for (const el of root.querySelectorAll<HTMLElement>('[data-testid="lore-row"]')) {
    if (el.dataset.entryId === id) return el;
  }
  return null;
}

/** How long the 適用範圍 notice stays. */
const TOAST_MS = 5000;

/** What a card will change to once 儲存變更 sends it. */
interface StagedChange {
  state?: LoreEntryState;
  loreType?: LoreType;
  scope?: { kind: LoreScopeKind; key: string; label: string };
}

type StagedField = keyof StagedChange;

/** The stored value's label for each staged field of one card. */
type StagedFrom = Partial<Record<StagedField, string>>;

function withStaged(entry: LoreEntryView, ch: StagedChange | undefined): LoreEntryView {
  if (!ch) return entry;
  return {
    ...entry,
    ...(ch.state ? { state: ch.state } : {}),
    ...(ch.loreType ? { loreType: ch.loreType } : {}),
    ...(ch.scope ? { scopeKind: ch.scope.kind, scopeKey: ch.scope.key } : {}),
  };
}

function withEdit(entry: LoreEntryView, patch: LoreEntryEdit): LoreEntryView {
  return {
    ...entry,
    ...(patch.title !== undefined ? { title: patch.title } : {}),
    ...(patch.body !== undefined ? { body: patch.body } : {}),
  };
}

/** The scope key the server gives an entry moved to `kind`. */
function scopeKeyFor(entry: LoreEntryView, kind: LoreScopeKind): string {
  return kind === "everyone" ? "" : kind === "manual" ? entry.taskTypeKey : entry.authorId;
}

type EntrySeed = { key: string; entryId: string; state: LoreEntryState };

/** What every section visit reads and nothing in a visit changes. Held by the
 * outer LorePage so a section change does not re-fetch the roster (expensive)
 * and the cards do not flicker to raw ids and plain-text badges meanwhile. */
interface LoreShared {
  members: ReturnType<typeof useMembers>["members"];
  membersLoading: boolean;
  membersError: boolean;
  workers: ReturnType<typeof useOutsourceWorkers>["workers"];
  manuals: ReturnType<typeof useTaskManuals>["manuals"];
  entryCaps: { title: number; body: number };
}

export function LorePage({ canSetScope }: { canSetScope?: boolean } = {}) {
  const { members, loading: membersLoading, error: membersError } = useMembers();
  const { workers } = useOutsourceWorkers();
  const { manuals } = useTaskManuals();
  const [entryCaps, setEntryCaps] = useState({
    title: LORE_CAP_CHARS_DEFAULTS.title,
    body: LORE_CAP_CHARS_DEFAULTS.body,
  });
  useEffect(() => {
    let alive = true;
    loadServerSettings()
      .then((s) => {
        if (alive) setEntryCaps({ title: s.loreCapCharsTitle, body: s.loreCapCharsBody });
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, []);
  const shared = useMemo<LoreShared>(
    () => ({ members, membersLoading, membersError, workers, manuals, entryCaps }),
    [members, membersLoading, membersError, workers, manuals, entryCaps]
  );
  const [route] = useHashRoute();
  const sec = route.page === "lore" ? route.loreSection : undefined;
  // The directory's id search knows the entry's state; the section page it
  // opens ticks that state so the entry is listed (a 已失效 one included).
  const seedRef = useRef<EntrySeed | null>(null);
  // The budgets outlive a section visit: the directory and every section page
  // read the same numbers, asked once and re-asked after a change.
  const [budgets, setBudgets] = useState<LoreBudgetTargetView[] | null>(null);
  const [budgetsError, setBudgetsError] = useState(false);
  // Only the newest budgets read lands: an older one answering last would put
  // stale numbers back in the section headers.
  const budgetsSeqRef = useRef(0);
  const refreshBudgets = useCallback(async () => {
    const seq = ++budgetsSeqRef.current;
    try {
      const next = await api.listLoreBudgets();
      if (seq !== budgetsSeqRef.current) return;
      setBudgets(next);
      setBudgetsError(false);
    } catch (e) {
      console.warn("LorePage: budgets load failed", e);
      if (seq === budgetsSeqRef.current) setBudgetsError(true);
    }
  }, []);
  useEffect(() => {
    void refreshBudgets();
  }, [refreshBudgets]);
  // One mount per page: filters, the toast and the highlight paging belong to
  // the section they were set on, so a section change starts them over by
  // remounting rather than by resetting each.
  const sectionKey = sec ? sectionKeyOf(sec.kind, sec.key) : null;
  // Held above the per-section remount so a trip to a section page and back
  // keeps the folding even when storage refuses it.
  const [dirCollapsed, setDirCollapsed] = useState(readDirCollapsed);
  const changeDirCollapsed = useCallback((next: ReadonlySet<DirGroupKind>) => {
    setDirCollapsed(next);
    writeDirCollapsed(next);
  }, []);
  // 🔴 The view gets its route from here and never reads the hash itself: a
  // second subscriber could see a new section before this key changes, load
  // it into the old mount, and have those rows thrown away by the remount.
  return (
    <LoreView
      key={sectionKey ?? ""}
      sectionKey={sectionKey}
      highlightId={(route.page === "lore" && sec && route.loreEntryId) || ""}
      canSetScope={canSetScope}
      shared={shared}
      seedRef={seedRef}
      budgets={budgets}
      budgetsError={budgetsError}
      refreshBudgets={refreshBudgets}
      dirCollapsed={dirCollapsed}
      onDirCollapsedChange={changeDirCollapsed}
    />
  );
}

function LoreView({
  sectionKey,
  highlightId,
  canSetScope: canSetScopeProp,
  shared,
  seedRef,
  budgets,
  budgetsError,
  refreshBudgets,
  dirCollapsed,
  onDirCollapsedChange,
}: {
  sectionKey: string | null;
  highlightId: string;
  canSetScope?: boolean;
  shared: LoreShared;
  seedRef: { current: EntrySeed | null };
  budgets: LoreBudgetTargetView[] | null;
  budgetsError: boolean;
  refreshBudgets: () => Promise<void>;
  dirCollapsed: ReadonlySet<DirGroupKind>;
  onDirCollapsedChange: (next: ReadonlySet<DirGroupKind>) => void;
}) {
  const { t, msg } = useI18n();
  const { members, membersLoading, membersError, workers, manuals, entryCaps } = shared;
  const canSetScope = canSetScopeProp ?? viewerMaySetLoreScope(members);

  // undefined while asked, null when the server gave no total.
  const [officeTotal, setOfficeTotal] = useState<number | null | undefined>(undefined);
  useEffect(() => {
    if (sectionKey !== null) return;
    let alive = true;
    api
      .countLoreEntries()
      .then((n) => {
        if (alive) setOfficeTotal(n ?? null);
      })
      .catch((e) => {
        console.warn("LorePage: lore total load failed", e);
        if (alive) setOfficeTotal(null);
      });
    return () => {
      alive = false;
    };
  }, [sectionKey]);

  // ── 篩選 ────────────────────────────────────────────────────────────────
  const [searchDraft, setSearchDraft] = useState("");
  const [appliedSearch, setAppliedSearch] = useState("");
  const [states, setStates] = useState<Set<LoreEntryState>>(() => {
    const seeded = new Set<LoreEntryState>(DEFAULT_STATES);
    const seed = seedRef.current;
    if (seed && seed.key === sectionKey && seed.entryId === highlightId) seeded.add(seed.state);
    return seeded;
  });
  const [loreTypes, setLoreTypes] = useState<Set<LoreType>>(new Set());

  const baseOpts = useMemo<LoreListOptions>(() => {
    const next: LoreListOptions = {};
    if (states.size > 0) next.states = [...states];
    if (loreTypes.size > 0) next.loreTypes = [...loreTypes];
    if (appliedSearch !== "") {
      const id = normalizeEntryId(appliedSearch);
      if (id !== null) next.entryIds = [id];
      else next.q = appliedSearch;
    }
    return next;
  }, [states, loreTypes, appliedSearch]);
  const filterKey = JSON.stringify(baseOpts);

  // Ticking 已失效 only ADDS rows to a section, so it does not count as a
  // search; dropping 置頂 or 生效中 narrows like any other filter.
  const statesNarrow =
    states.size > 0 && DEFAULT_STATES.some((s) => !states.has(s));
  const narrowing =
    appliedSearch !== "" || loreTypes.size > 0 || statesNarrow;
  const statesAtDefault =
    states.size === DEFAULT_STATES.length && DEFAULT_STATES.every((s) => states.has(s));
  const activeFilterCount = [appliedSearch !== "", loreTypes.size > 0, !statesAtDefault].filter(
    Boolean,
  ).length;

  function clearFilters() {
    setSearchDraft("");
    setAppliedSearch("");
    setStates(new Set(DEFAULT_STATES));
    setLoreTypes(new Set());
  }

  // Auto-paging for a highlight; a new section visit is a new mount.
  const hlRef = useRef<{ id: string; filterKey: string; pages: number } | null>(null);
  const [hlGaveUp, setHlGaveUp] = useState("");

  // ── sections ───────────────────────────────────────────────────────────
  const [open, setOpen] = useState<Set<string>>(new Set());
  const [data, setData] = useState<Record<string, SectionData>>({});
  const [actionError, setActionError] = useState<string | null>(null);

  // ── 暫存的變更 ─────────────────────────────────────────────────────────
  const [staged, setStaged] = useState<Map<string, StagedChange>>(new Map());
  const stagedRef = useRef(staged);
  stagedRef.current = staged;
  const [saving, setSaving] = useState(false);
  const savingRef = useRef(false);
  const [saveFailed, setSaveFailed] = useState<{ id: string; reason: string }[]>([]);
  // A browser Back / tab click refused because 儲存變更 is running.
  const [leaveBlocked, setLeaveBlocked] = useState(false);

  const defs = useMemo<SectionDef[]>(() => {
    if (!budgets) return [];
    const rank = (k: string) => (k === "everyone" ? 0 : k === "agent" ? 1 : k === "manual" ? 2 : 3);
    return budgets
      .filter((b) => b.scopeKind !== "unknown")
      .map((b) => ({
        key: sectionKeyOf(b.scopeKind, b.scopeKey),
        kind: b.scopeKind as LoreScopeKind,
        scopeKey: b.scopeKey,
        target: b,
      }))
      .sort((a, b) => rank(a.kind) - rank(b.kind));
  }, [budgets]);
  const defsRef = useRef(defs);
  defsRef.current = defs;
  const baseOptsRef = useRef(baseOpts);
  baseOptsRef.current = baseOpts;
  const dataRef = useRef(data);
  dataRef.current = data;
  const openRef = useRef(open);
  openRef.current = open;
  // 🔴 openRef moves with every setOpen, not only on render: the filter effect
  // can run between a click that opens a section and the re-render after it,
  // and reading a stale ref there wipes the new section's data and never
  // re-asks it, leaving it on 載入中 for good.
  const applyOpen = (next: Set<string>) => {
    openRef.current = next;
    setOpen(next);
  };

  // Per-section generation: a reload bumps it, and an answer to an older
  // question (first page, next page or a refresh) is dropped.
  const genRef = useRef(new Map<string, number>());
  const flightRef = useRef(new Set<string>());
  const bumpGen = (key: string) => {
    const g = (genRef.current.get(key) ?? 0) + 1;
    genRef.current.set(key, g);
    return g;
  };
  // Per-section count of reads that answer a cap. Rows from a slower read may
  // still merge, but only the newest read's cap is drawn, or an older answer
  // landing last would bring back stale numbers.
  const capSeqRef = useRef(new Map<string, number>());
  const nextCapSeq = (key: string) => {
    const n = (capSeqRef.current.get(key) ?? 0) + 1;
    capSeqRef.current.set(key, n);
    return n;
  };
  const isLatestCap = (key: string, seq: number) => capSeqRef.current.get(key) === seq;

  const patchSection = useCallback(
    (key: string, fn: (prev: SectionData) => SectionData) =>
      setData((prev) => (prev[key] ? { ...prev, [key]: fn(prev[key]) } : prev)),
    []
  );

  const loadSection = useCallback(
    (def: SectionDef): Promise<number> => {
      const gen = bumpGen(def.key);
      const capSeq = nextCapSeq(def.key);
      flightRef.current.add(def.key);
      setData((prev) => ({
        ...prev,
        [def.key]: {
          entries: [],
          total: null,
          cap: NO_CAP,
          loading: true,
          fetchingMore: false,
          error: false,
          exhausted: false,
          moreFailed: false,
        },
      }));
      return api
        .listLoreEntries({ ...baseOptsRef.current, ...scopeOpts(def), limit: PAGE, offset: 0 })
        .then((page) => {
          if (genRef.current.get(def.key) !== gen) return -1;
          // Out of flight before the rows land: an effect that runs on them
          // (the highlight's auto-paging) may ask for the next page at once.
          flightRef.current.delete(def.key);
          patchSection(def.key, (p) => ({
            entries: page.entries,
            total: page.total ?? null,
            cap: isLatestCap(def.key, capSeq) ? capOf(page) : p.cap,
            loading: false,
            fetchingMore: false,
            error: false,
            exhausted: page.entries.length < PAGE,
            moreFailed: false,
          }));
          return page.entries.length;
        })
        .catch((e) => {
          console.warn("LorePage: section load failed", e);
          if (genRef.current.get(def.key) === gen) {
            patchSection(def.key, (p) => ({ ...p, loading: false, error: true }));
          }
          return -1;
        })
        .finally(() => {
          if (genRef.current.get(def.key) === gen) flightRef.current.delete(def.key);
        });
    },
    [patchSection]
  );

  const loadMore = useCallback(
    (def: SectionDef): boolean => {
      const held = dataRef.current[def.key];
      if (!held || held.exhausted || held.error || flightRef.current.has(def.key)) return false;
      const gen = genRef.current.get(def.key) ?? 0;
      const capSeq = nextCapSeq(def.key);
      flightRef.current.add(def.key);
      patchSection(def.key, (p) => ({ ...p, fetchingMore: true }));
      api
        .listLoreEntries({
          ...baseOptsRef.current,
          ...scopeOpts(def),
          limit: PAGE,
          offset: held.entries.length,
        })
        .then((page) => {
          if (genRef.current.get(def.key) !== gen) return;
          flightRef.current.delete(def.key);
          patchSection(def.key, (p) => {
            // An entry that changed between two reads can slide across the page
            // boundary and arrive twice; the first copy keeps its place.
            const ids = new Set(p.entries.map((e) => e.id));
            return {
              ...p,
              entries: p.entries.concat(page.entries.filter((e) => !ids.has(e.id))),
              cap: isLatestCap(def.key, capSeq) ? capOf(page) : p.cap,
              exhausted: page.entries.length < PAGE,
              moreFailed: false,
            };
          });
        })
        .catch((e) => {
          // The rows already shown stay; the next scroll retries.
          console.warn("LorePage: page load failed", e);
          if (genRef.current.get(def.key) === gen) patchSection(def.key, (p) => ({ ...p, moreFailed: true }));
        })
        .finally(() => {
          if (genRef.current.get(def.key) !== gen) return;
          patchSection(def.key, (p) => ({ ...p, fetchingMore: false }));
          flightRef.current.delete(def.key);
        });
      return true;
    },
    [patchSection]
  );

  const budgetsReady = budgets !== null;
  // The section page loads its one section, again on every filter change.
  // defs are read through a ref: a budgets refresh after a mutation must not
  // re-ask the section from page one.
  useEffect(() => {
    if (!budgetsReady) return;
    for (const key of openRef.current) {
      bumpGen(key);
      flightRef.current.delete(key);
    }
    setData({});
    const def = sectionKey ? defsRef.current.find((d) => d.key === sectionKey) : undefined;
    applyOpen(def ? new Set([def.key]) : new Set());
    if (def) void loadSection(def);
  }, [filterKey, budgetsReady, sectionKey, loadSection]);

  // ── 捲到底: the section pages on when its bottom nears the scrollport's
  // bottom. jsdom has no geometry, so there any scroll asks for more.
  const scrollRef = useRef<HTMLDivElement>(null);
  const sentinelRefs = useRef(new Map<string, HTMLElement>());
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    function onScroll() {
      if (!el) return;
      const bottom = el.getBoundingClientRect().bottom;
      for (const def of defsRef.current) {
        if (!openRef.current.has(def.key)) continue;
        const s = sentinelRefs.current.get(def.key);
        if (!s) continue;
        if (s.getBoundingClientRect().top - bottom < NEAR_BOTTOM_PX) loadMore(def);
      }
    }
    el.addEventListener("scroll", onScroll);
    return () => el.removeEventListener("scroll", onScroll);
  }, [loadMore]);

  // ── identity resolution (the SAME shape as TaskCard's `creator`) ────────
  const allEntries = useMemo(
    () => Object.values(data).flatMap((d) => d.entries),
    [data]
  );
  const workerIds = useMemo(
    () => allEntries.map((e) => e.authorId).filter((id) => id.startsWith("ow-")),
    [allEntries]
  );
  // A writer who has left — released worker or departed staff — is in neither
  // list; the per-id read still serves the name. A staff id is only judged once
  // the roster has loaded: before that every live colleague looks absent and
  // would cost a read each.
  const rosterLoaded = !membersLoading;
  const unlistedAuthorIds = useMemo(
    () =>
      [...allEntries.map((e) => e.authorId), ...defs.filter((d) => d.kind === "agent").map((d) => d.scopeKey)]
        .filter(
          (id) =>
            id !== "" &&
            !isSyntheticSender(id) &&
            !workers.some((w) => w.id === id) &&
            (id.startsWith("ow-") ||
              (rosterLoaded && !members.some((m) => m.id === id)))
        ),
    [allEntries, defs, members, workers, rosterLoaded]
  );
  const unlistedNames = useWorkerCodenames(unlistedAuthorIds);
  const releasedAvatarUrls = useWorkerAvatarUrls(workerIds);

  const resolveAuthor = useCallback(
    (id: string): AuthorIdentity => {
      if (id === "") return { text: t.lore.authorUnknown, name: t.lore.authorUnknown, peerId: "" };
      const m = members.find((x) => x.id === id);
      if (m) return { text: m.name, name: m.name, peerId: id };
      if (id.startsWith("ow-")) {
        // 🔴 The codename and the reachability come from different lists: a
        // released worker still wrote this entry and keeps his name, but only
        // the LIVE roster may offer a chat with him — the server refuses a
        // message to someone no longer on it.
        const live = workers.find((x) => x.id === id);
        const cn = live?.codename ?? unlistedNames.get(id);
        return {
          text: cn ? msg.outsourceLabel(cn) : id,
          name: cn || id,
          peerId: live ? id : "",
        };
      }
      // Not on the roster: the entry stays, only the live affordance goes.
      const name = unlistedNames.get(id) ?? id;
      return { text: name, name, peerId: "" };
    },
    [members, workers, unlistedNames, msg, t.lore.authorUnknown]
  );

  /** Which scope this ONE entry rides, for the badge. An unresolved key falls
   * back to the raw key, never a blank; `unknown` (the retired `role` kind the
   * migration could not place) names no kind and gets no glyph. */
  const resolveScope = useCallback(
    (entry: LoreEntryView): {
      kind: "" | LoreScopeKind;
      label: string;
      manualKey: string;
    } => {
      if (entry.scopeKind === "agent") {
        return { kind: "agent", label: resolveAuthor(entry.scopeKey).text, manualKey: "" };
      }
      if (entry.scopeKind === "manual") {
        const m = manuals.find((x) => x.typeKey === entry.scopeKey);
        return {
          kind: "manual",
          label: taskTypeLabel(t, entry.scopeKey, m?.displayName ?? ""),
          manualKey: entry.scopeKey,
        };
      }
      if (entry.scopeKind === "everyone") {
        return { kind: "everyone", label: t.lore.scopeEveryone, manualKey: "" };
      }
      return { kind: "", label: entry.scopeKey || t.lore.scopeUnknown, manualKey: "" };
    },
    [manuals, resolveAuthor, t]
  );

  const resolveScopeMenu = useCallback(
    (entry: LoreEntryView): ScopeMenu => {
      const manualName = taskTypeLabel(
        t,
        entry.taskTypeKey,
        manuals.find((x) => x.typeKey === entry.taskTypeKey)?.displayName ?? ""
      );
      const authorName = resolveAuthor(entry.authorId).text;
      const choices = entry.scopeOptions.map(
        (kind): ScopeChoice => ({
          kind,
          label:
            kind === "manual"
              ? msg.loreScopeManual(manualName)
              : kind === "agent"
                ? msg.loreScopeAgent(authorName)
                : t.lore.scopeEveryone,
        })
      );
      const defaultKind: LoreScopeKind =
        entry.sourceTaskId !== "" && entry.taskTypeKey !== "" ? "manual" : "agent";
      return { choices, defaultKind };
    },
    [manuals, resolveAuthor, msg, t]
  );

  const authorAvatar = useCallback(
    (id: string) => {
      const m = members.find((x) => x.id === id);
      if (m) return { src: m.avatarUrl, kind: avatarKindForMember(m) } as const;
      const w = workers.find((x) => x.id === id);
      if (w) return { src: w.avatarUrl, kind: "outsource" } as const;
      if (id.startsWith("ow-")) {
        return { src: releasedAvatarUrls.get(id), kind: "outsource" } as const;
      }
      return null;
    },
    [members, workers, releasedAvatarUrls]
  );

  const sectionName = useCallback(
    (def: { kind: LoreScopeKind; scopeKey: string }): string =>
      def.kind === "everyone"
        ? t.lore.kindEveryone
        : def.kind === "manual"
          ? taskTypeLabel(
              t,
              def.scopeKey,
              manuals.find((x) => x.typeKey === def.scopeKey)?.displayName ?? ""
            )
          : resolveAuthor(def.scopeKey).text,
    [manuals, resolveAuthor, t]
  );

  // ── after a change: the whole section again, in the server's order, as many
  // rows as were loaded, and the numbers. The rows stay on screen meanwhile.
  // Resolves false only when the re-read itself failed and no newer one has
  // taken over.
  const rereadSection = useCallback(
    async (def: SectionDef): Promise<boolean> => {
      const gen = bumpGen(def.key);
      const capSeq = nextCapSeq(def.key);
      flightRef.current.add(def.key);
      const loaded = dataRef.current[def.key]?.entries.length ?? 0;
      const want = Math.max(PAGE, Math.ceil(loaded / PAGE) * PAGE);
      try {
        const rows: LoreEntryView[] = [];
        const seen = new Set<string>();
        let offset = 0;
        let last: LoreEntryPageView | null = null;
        let total: number | undefined;
        let exhausted = false;
        while (offset < want) {
          const limit = Math.min(MAX_LIST_LIMIT, want - offset);
          const p = await api.listLoreEntries({ ...baseOptsRef.current, ...scopeOpts(def), limit, offset });
          if (genRef.current.get(def.key) !== gen) return true;
          last = p;
          total ??= p.total;
          offset += p.entries.length;
          for (const e of p.entries) {
            if (seen.has(e.id)) continue;
            seen.add(e.id);
            rows.push(e);
          }
          if (p.entries.length < limit) {
            exhausted = true;
            break;
          }
        }
        flightRef.current.delete(def.key);
        const fresh = last;
        patchSection(def.key, (p) => ({
          entries: rows,
          total: total ?? null,
          cap: fresh && isLatestCap(def.key, capSeq) ? capOf(fresh) : p.cap,
          loading: false,
          fetchingMore: false,
          error: false,
          exhausted,
          moreFailed: false,
        }));
        return true;
      } catch (e) {
        console.warn("LorePage: section re-read failed", e);
        if (genRef.current.get(def.key) !== gen) return true;
        patchSection(def.key, (p) => ({ ...p, fetchingMore: false }));
        return false;
      } finally {
        if (genRef.current.get(def.key) === gen) flightRef.current.delete(def.key);
      }
    },
    [patchSection]
  );

  /** Resolves false when the open section could not be re-read. */
  const rereadAll = useCallback(async (): Promise<boolean> => {
    const def = sectionKey ? defsRef.current.find((d) => d.key === sectionKey) : undefined;
    const [, sectionOk] = await Promise.all([
      refreshBudgets(),
      def && openRef.current.has(def.key) ? rereadSection(def) : Promise.resolve(true),
    ]);
    return sectionOk;
  }, [sectionKey, refreshBudgets, rereadSection]);

  const failText = useCallback(
    (e: unknown) =>
      serverMessageOf(e) || (isHttpStatus(e, 403) ? t.lore.forbidden : t.lore.actionFailed),
    [t.lore.forbidden, t.lore.actionFailed]
  );

  const findEntry = useCallback((id: string): LoreEntryView | undefined => {
    for (const d of Object.values(dataRef.current)) {
      const e = d.entries.find((x) => x.id === id);
      if (e) return e;
    }
    return undefined;
  }, []);

  const stage = useCallback(
    (id: string, patch: StagedChange) => {
      const orig = findEntry(id);
      if (!orig || savingRef.current) return;
      setSaveFailed([]);
      setStaged((prev) => {
        const next = new Map(prev);
        const ch: StagedChange = { ...(next.get(id) ?? {}), ...patch };
        // Choosing the stored value again un-stages that field.
        if (ch.state === orig.state) delete ch.state;
        if (ch.loreType === orig.loreType) delete ch.loreType;
        if (ch.scope?.kind === orig.scopeKind) delete ch.scope;
        if (Object.keys(ch).length === 0) next.delete(id);
        else next.set(id, ch);
        return next;
      });
    },
    [findEntry]
  );

  const setEntryState = useCallback(
    (id: string, state: LoreEntryState) => stage(id, { state }),
    [stage]
  );
  const setEntryLoreType = useCallback(
    (id: string, loreType: LoreType) => stage(id, { loreType }),
    [stage]
  );
  const setEntryScope = useCallback(
    (id: string, kind: LoreScopeKind, label: string) => {
      const orig = findEntry(id);
      if (orig) stage(id, { scope: { kind, key: scopeKeyFor(orig, kind), label } });
    },
    [findEntry, stage]
  );

  // The 適用範圍 notice: an entry whose new scope is another section's leaves
  // this page on the re-read, and the notice says where it went.
  const [toast, setToast] = useState<{ id: string; scopeLabel: string }[]>([]);
  useEffect(() => {
    if (toast.length === 0) return;
    const timer = window.setTimeout(() => setToast([]), TOAST_MS);
    return () => window.clearTimeout(timer);
  }, [toast]);

  /** A write landed but its re-read failed: the cards show what was sent. */
  const showSentAfterFailedReread = useCallback(
    (sent: (e: LoreEntryView) => LoreEntryView) => {
      setData((prev) => {
        const next: Record<string, SectionData> = {};
        for (const [key, sd] of Object.entries(prev)) {
          next[key] = { ...sd, entries: sd.entries.map(sent) };
        }
        return next;
      });
      setActionError(t.lore.stagedSavedRereadFailed);
    },
    [t.lore.stagedSavedRereadFailed]
  );

  // One entry at a time, type → scope → state; a failed field keeps its staged
  // value with the server's reason, and the section is re-read once at the end.
  async function saveStaged() {
    savingRef.current = true;
    setSaving(true);
    setActionError(null);
    setSaveFailed([]);
    const failed: { id: string; reason: string }[] = [];
    const sentOk = new Map<string, StagedChange>();
    const moved: { id: string; scopeLabel: string }[] = [];
    for (const [id, ch] of [...stagedRef.current]) {
      const ok: StagedChange = {};
      let reason = "";
      let anyFailed = false;
      const attempt = async (fn: () => Promise<unknown>, onOk: () => void) => {
        try {
          await fn();
          onOk();
        } catch (e) {
          anyFailed = true;
          if (reason === "") reason = failText(e);
        }
      };
      const { loreType, scope, state } = ch;
      if (loreType) await attempt(() => api.editLoreEntry(id, { loreType }), () => (ok.loreType = loreType));
      if (scope) {
        await attempt(
          () => api.setLoreEntryScope(id, scope.kind),
          () => {
            ok.scope = scope;
            moved.push({ id, scopeLabel: scope.label });
          }
        );
      }
      if (state) await attempt(() => api.setLoreEntryState(id, state), () => (ok.state = state));
      if (Object.keys(ok).length > 0) sentOk.set(id, ok);
      if (anyFailed) failed.push({ id, reason });
    }
    const rereadOk = await rereadAll();
    if (!rereadOk && sentOk.size > 0) {
      showSentAfterFailedReread((e) => withStaged(e, sentOk.get(e.id)));
    }
    // Clear only what went through with the value that was sent; a change
    // staged on the same card meanwhile stays.
    setStaged((prev) => {
      const next = new Map(prev);
      for (const [id, ok] of sentOk) {
        const cur = next.get(id);
        if (!cur) continue;
        const left: StagedChange = { ...cur };
        if (ok.loreType !== undefined && left.loreType === ok.loreType) delete left.loreType;
        if (ok.scope !== undefined && left.scope?.kind === ok.scope.kind) delete left.scope;
        if (ok.state !== undefined && left.state === ok.state) delete left.state;
        if (Object.keys(left).length === 0) next.delete(id);
        else next.set(id, left);
      }
      return next;
    });
    savingRef.current = false;
    setSaving(false);
    setLeaveBlocked(false);
    setSaveFailed(failed);
    if (moved.length > 0) setToast(moved);
  }

  function discardStaged() {
    setStaged(new Map());
    setSaveFailed([]);
  }

  /** Puts one staged field of a card back, or every field on it without `field`. */
  const revertStaged = useCallback((id: string, field?: StagedField) => {
    if (savingRef.current) return;
    const cur = stagedRef.current.get(id);
    if (!cur) return;
    const left: StagedChange = { ...cur };
    if (field) delete left[field];
    const cardCleared = !field || Object.keys(left).length === 0;
    setStaged((prev) => {
      const next = new Map(prev);
      if (cardCleared) next.delete(id);
      else next.set(id, left);
      return next;
    });
    if (cardCleared) {
      setSaveFailed((prev) => (prev.some((f) => f.id === id) ? prev.filter((f) => f.id !== id) : prev));
    }
  }, []);

  const hasStaged = staged.size > 0;
  let stagedFields = 0;
  for (const ch of staged.values()) stagedFields += Object.keys(ch).length;
  // The bar is fixed to the bottom of the screen, over the end of `.lore`;
  // `.lore` pads its end by the part the bar covers so the last card can be
  // scrolled clear of it.
  const saveBarRef = useRef<HTMLDivElement>(null);
  const saveBarShown = hasStaged || saving;
  useLayoutEffect(() => {
    const bar = saveBarRef.current;
    const lore = scrollRef.current;
    if (!saveBarShown || !bar || !lore) return;
    const reserve = () => {
      const covered = Math.max(0, lore.getBoundingClientRect().bottom - bar.getBoundingClientRect().top);
      lore.style.setProperty("--lore-savebar-reserve", `${covered}px`);
    };
    reserve();
    const ro = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(reserve);
    ro?.observe(bar);
    ro?.observe(lore);
    window.addEventListener("resize", reserve);
    return () => {
      ro?.disconnect();
      window.removeEventListener("resize", reserve);
      lore.style.removeProperty("--lore-savebar-reserve");
    };
  }, [saveBarShown]);

  const leaveText = t.lore.stagedLeaveConfirm;
  // Leaving the section (another tab, the directory, Back) or closing the page
  // with staged changes asks first; a highlight inside the same section is not
  // leaving. While 儲存變更 runs, leaving is refused outright: its results
  // would land on a page that no longer shows those cards.
  const holdsWork = hasStaged || saving;
  useEffect(() => {
    if (!holdsWork) return;
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = "";
    };
    window.addEventListener("beforeunload", onBeforeUnload);
    const releaseWork = registerUnsavedWork(() => true);
    const release = setNavigationGuard((next: HashRoute) => {
      const same =
        next.page === "lore" &&
        next.loreSection !== undefined &&
        sectionKeyOf(next.loreSection.kind, next.loreSection.key) === sectionKey;
      if (same) return true;
      if (savingRef.current) {
        setLeaveBlocked(true);
        return false;
      }
      return window.confirm(leaveText);
    });
    return () => {
      window.removeEventListener("beforeunload", onBeforeUnload);
      releaseWork();
      release();
    };
  }, [holdsWork, sectionKey, leaveText]);

  /** Rejects with the reason so the editor can keep the draft and say why. */
  const editEntry = useCallback(
    async (id: string, patch: LoreEntryEdit): Promise<void> => {
      setActionError(null);
      try {
        await api.editLoreEntry(id, patch);
      } catch (e) {
        throw new Error(failText(e));
      }
      if (!(await rereadAll())) {
        showSentAfterFailedReread((e) => (e.id === id ? withEdit(e, patch) : e));
      }
    },
    [failText, rereadAll, showSentAfterFailedReread]
  );

  /** A restore already landed; this re-reads the section. */
  const restoredEntry = useCallback(
    async (id: string, restored: Record<string, string> | undefined) => {
      if (await rereadAll()) return;
      showSentAfterFailedReread((e) =>
        e.id === id && restored ? withEdit(e, { title: restored.title, body: restored.body }) : e
      );
    },
    [rereadAll, showSentAfterFailedReread]
  );

  // ── 目錄的編號搜尋 ──────────────────────────────────────────────────────
  const [dirSearch, setDirSearch] = useState("");
  const [dirApplied, setDirApplied] = useState("");
  const [dirNote, setDirNote] = useState<string | null>(null);
  async function jumpToEntry(raw: string) {
    setDirNote(null);
    const v = raw.trim();
    setDirApplied(v);
    if (v === "") return;
    const id = normalizeEntryId(v);
    if (id === null) {
      setDirNote(t.lore.directoryIdOnly);
      return;
    }
    try {
      const page = await api.listLoreEntries({
        entryIds: [id],
        states: ["pinned", "active", "retired"],
        limit: 1,
        offset: 0,
      });
      const e = page.entries[0];
      if (!e) {
        setDirNote(msg.loreDirectoryNotFound(id));
        return;
      }
      const key = e.scopeKind === "everyone" ? "" : e.scopeKey;
      // A removed member or a deleted manual has no section page to open.
      if (e.scopeKind === "unknown" || !defsRef.current.some((d) => d.key === sectionKeyOf(e.scopeKind, key))) {
        setDirNote(msg.loreDirectorySectionGone(id));
        return;
      }
      seedRef.current = { key: sectionKeyOf(e.scopeKind, key), entryId: id, state: e.state };
      navigateHash({ page: "lore", loreSection: { kind: e.scopeKind, key }, loreEntryId: id });
    } catch (e) {
      setDirNote(failText(e));
    }
  }

  const currentDef = sectionKey ? defs.find((d) => d.key === sectionKey) ?? null : null;
  // A highlighted entry beyond the loaded pages: page on until it shows, the
  // section runs out, the cap is reached, or the reader changes a filter (then
  // they are looking for something else).
  const currentData = currentDef ? data[currentDef.key] : undefined;
  const highlightLoaded =
    highlightId !== "" && !!currentData?.entries.some((e) => e.id === highlightId);
  const hlCapped = hlGaveUp !== "" && hlGaveUp === highlightId;
  useEffect(() => {
    if (!currentDef || !currentData || highlightId === "" || highlightLoaded) return;
    let r = hlRef.current;
    if (!r || r.id !== highlightId) r = hlRef.current = { id: highlightId, filterKey, pages: 0 };
    if (r.filterKey !== filterKey) return;
    if (
      currentData.loading ||
      currentData.fetchingMore ||
      currentData.exhausted ||
      currentData.error ||
      currentData.moreFailed
    )
      return;
    if (r.pages >= HIGHLIGHT_PAGE_CAP) {
      setHlGaveUp(highlightId);
      return;
    }
    if (loadMore(currentDef)) r.pages += 1;
  }, [currentDef, currentData, highlightId, highlightLoaded, filterKey, loadMore]);

  // A dropped entry starts folded to one row, every other entry open; a fold or
  // open is remembered per entry for the life of the page, so a re-read that
  // keeps it dropped keeps it as the reader left it. A highlighted entry opens
  // itself.
  const [openOverride, setOpenOverride] = useState<Map<string, boolean>>(() => new Map());
  const focusAfterToggle = useRef<string | null>(null);
  function setEntryOpen(id: string, open: boolean) {
    if (openOverride.get(id) === open) return;
    focusAfterToggle.current = id;
    setOpenOverride((prev) => (prev.get(id) === open ? prev : new Map(prev).set(id, open)));
  }
  useEffect(() => {
    if (highlightId === "") return;
    setOpenOverride((prev) => (prev.get(highlightId) === true ? prev : new Map(prev).set(highlightId, true)));
  }, [highlightId]);
  const highlightExpanded = openOverride.get(highlightId) === true;
  const scrolledToRef = useRef("");
  useEffect(() => {
    if (!highlightLoaded) {
      scrolledToRef.current = "";
      return;
    }
    // Focusing the folded row would lose focus to <body> once the open card
    // replaces it on the next render.
    if (!highlightExpanded) return;
    if (scrolledToRef.current === highlightId) return;
    scrolledToRef.current = highlightId;
    const el = rowElement(scrollRef.current, highlightId);
    el?.scrollIntoView?.({ block: "center" });
    el?.focus({ preventScroll: true });
  }, [highlightLoaded, highlightExpanded, highlightId]);
  useEffect(() => {
    const f = focusAfterToggle.current;
    if (!f) return;
    focusAfterToggle.current = null;
    rowElement(scrollRef.current, f)?.focus({ preventScroll: true });
  }, [openOverride]);

  // ── render helpers ──────────────────────────────────────────────────────
  // The section's own boot document as one bar. A member's 所有人 and own
  // entries interleave in boot order; the bar only stacks the two shares.
  function renderSummary(def: SectionDef, sd: SectionData | undefined) {
    const cap = sd?.cap;
    // 所有人 has no boot document of its own (the cap the server reports is a
    // member's), so its page reads like its directory card.
    if (def.kind === "everyone" || !sd || !cap || cap.capChars <= 0) return dirUsage(def);
    const own =
      [...cap.budgetSegments].reverse().find((s) => s.scopeKind === def.kind) ?? null;
    const everyoneSeg =
      def.kind === "agent"
        ? cap.budgetSegments.find((s) => s.scopeKind === "everyone") ?? null
        : null;
    const pct = (chars: number) => `${Math.min(100, (chars / cap.capChars) * 100)}%`;
    return (
      <>
        <div className="lore-dir-card__seg" data-testid="lore-section-bar">
          {everyoneSeg && (
            <span
              className="lore-dir-card__seg-everyone"
              data-testid="lore-bar-everyone"
              style={{ width: pct(everyoneSeg.fittedChars) }}
            />
          )}
          <span
            className="lore-dir-card__bar-fill"
            data-testid="lore-bar-own"
            style={{ width: pct(own?.fittedChars ?? 0) }}
          />
        </div>
        <div className="lore-section__usage" data-testid="lore-section-usage-row">
          <span data-testid="lore-section-used">
            <b className="lore-section__used-num">{msg.loreLegendNumber(cap.usedChars)}</b>
            {t.lore.charsTail}
          </span>
          <span data-testid="lore-section-cap">{msg.loreLegendCap(cap.capChars)}</span>
        </div>
        <div className="lore-section__legend" data-testid="lore-section-summary">
          {everyoneSeg && (
            <>
              <span className="lore-section__legend-item" data-testid="lore-legend-everyone">
                <i className="lore-dir-card__swatch lore-dir-card__swatch--everyone" aria-hidden="true" />
                <span className="lore-section__legend-name">{t.lore.kindEveryone}</span>{" "}
                {msg.loreLegendFit(everyoneSeg.fittedCount, everyoneSeg.entryCount)}
              </span>
              <span className="lore-section__legend-sep" aria-hidden="true">
                {" · "}
              </span>
            </>
          )}
          <span className="lore-section__legend-item" data-testid="lore-legend-own">
            <i className="lore-dir-card__swatch" aria-hidden="true" />
            <span className="lore-section__legend-name">{sectionName(def)}</span>{" "}
            {msg.loreLegendFit(own?.fittedCount ?? 0, own?.entryCount ?? 0)}
          </span>
        </div>
      </>
    );
  }

  function stagedFromOf(stored: LoreEntryView, ch: StagedChange): StagedFrom {
    const from: StagedFrom = {};
    if (ch.loreType) from.loreType = loreTypeLabel(t, stored.loreType);
    if (ch.state) from.state = stateLabelOf(t, stored.state);
    if (ch.scope) from.scope = resolveScope(stored).label;
    return from;
  }

  function renderSectionBody(def: SectionDef, sd: SectionData | undefined) {
    if (!sd || (sd.loading && sd.entries.length === 0)) {
      return <div className="lore__more">{t.lore.loadingMore}</div>;
    }
    if (sd.error) {
      return (
        <div className="lore__error" data-testid="lore-section-error">
          {t.lore.sectionLoadError}
        </div>
      );
    }
    return (
      <>
        {sd.entries.length === 0 && (
          <div className="lore__empty" data-testid="lore-section-empty">
            {t.lore.sectionEmpty}
          </div>
        )}
        {sd.entries.map((stored) => {
          const ch = staged.get(stored.id);
          const entry = withStaged(stored, ch);
          const dropped = stored.dropped === true;
          const foldable = !ch;
          const expanded = !foldable || (openOverride.get(entry.id) ?? !dropped);
          return (
            <LoreRow
                  key={entry.id}
                  entry={entry}
                  dimmed={dropped}
                  expanded={expanded}
                  onToggle={foldable ? () => setEntryOpen(entry.id, !expanded) : null}
                  author={resolveAuthor(entry.authorId)}
                  avatar={authorAvatar(entry.authorId)}
                  scope={resolveScope(entry)}
                  scopeMenu={
                    canSetScope && entry.scopeOptions.length > 0 ? resolveScopeMenu(entry) : null
                  }
                  highlighted={entry.id === highlightId}
                  stagedFrom={ch ? stagedFromOf(stored, ch) : null}
                  chipsLocked={saving}
                  onRevert={revertStaged}
                  entryCaps={entryCaps}
                  recipient={messageRecipient(stored)}
                  onOpenManual={openManual}
                  onSetState={setEntryState}
                  onSetScope={setEntryScope}
                  onSetLoreType={setEntryLoreType}
                  onSave={editEntry}
                  onRestored={restoredEntry}
                />
          );
        })}
        {!sd.exhausted && (
          <div
            className="lore__more"
            data-testid="lore-more"
            ref={(el) => {
              if (el) sentinelRefs.current.set(def.key, el);
              else sentinelRefs.current.delete(def.key);
            }}
          >
            {sd.fetchingMore ? t.lore.loadingMore : ""}
          </div>
        )}
      </>
    );
  }

  function messageRecipient(entry: LoreEntryView): MessageRecipient | null {
    // These ids are never on the roster, so the box must not wait on it to go.
    if (entry.authorId === "" || isSyntheticSender(entry.authorId)) return null;
    if (membersLoading) return { id: "", name: "", roster: "loading" };
    if (membersError) return { id: "", name: "", roster: "error" };
    const author = resolveAuthor(entry.authorId);
    if (author.peerId === "") return null;
    const codename = workers.find((w) => w.id === author.peerId)?.codename;
    return { id: author.peerId, name: codename ?? author.text };
  }

  function openManual(typeKey: string) {
    navigateHash({ page: "settings", manualKey: typeKey });
  }

  const sectionRoute = (def: SectionDef): HashRoute => ({
    page: "lore",
    loreSection: { kind: def.kind as LoreSectionKind, key: def.kind === "everyone" ? "" : def.scopeKey },
  });

  function dirBadges(def: SectionDef) {
    return (
      <span className="lore-dir-card__badges">
        <span className="lore-badge" data-testid="lore-section-count">
          {msg.loreEntryCount(def.target.entryCount)}
        </span>
        {def.target.droppedCount > 0 ? (
          <span className="lore-badge lore-badge--dropped" data-testid="lore-section-dropped">
            {msg.loreDropped(def.target.droppedCount)}
          </span>
        ) : (
          <span className="lore-badge lore-badge--fit">{t.lore.allFit}</span>
        )}
      </span>
    );
  }

  function dirLead(def: SectionDef) {
    if (def.kind === "agent") {
      const a = authorAvatar(def.scopeKey);
      return <Avatar size={28} kind={a?.kind ?? "staff"} src={a?.src} />;
    }
    return (
      <span className="lore-dir-card__glyph" aria-hidden="true">
        {def.kind === "everyone" ? <UsersIcon size={16} /> : <BookIcon size={16} />}
      </span>
    );
  }

  function sectionLead(def: SectionDef) {
    if (def.kind === "agent") {
      const a = authorAvatar(def.scopeKey);
      return <Avatar size={20} kind={a?.kind ?? "staff"} src={a?.src} />;
    }
    return (
      <span className="lore-section__glyph" aria-hidden="true">
        {def.kind === "everyone" ? <UsersIcon size={14} /> : <BookIcon size={14} />}
      </span>
    );
  }

  // 所有人 has no cap of its own (it is read inside every member's), so it shows
  // only its size; a bar there would compare against nothing.
  function dirUsage(def: SectionDef) {
    const b = def.target;
    if (def.kind === "everyone" || b.capChars <= 0) {
      return (
        <div className="lore-dir-card__usage" data-testid="lore-section-usage">
          <span className="lore-dir-card__usage-text">
            {def.kind === "everyone" ? msg.loreEveryoneUsed(b.usedChars) : msg.loreChars(b.usedChars)}
          </span>
        </div>
      );
    }
    const pct = Math.min(100, Math.round((b.usedChars / b.capChars) * 100));
    return (
      <div className="lore-dir-card__usage" data-testid="lore-section-usage">
        <span className="lore-dir-card__bar">
          <span className="lore-dir-card__bar-fill" style={{ width: `${pct}%` }} />
        </span>
        <span className="lore-dir-card__usage-text">
          {msg.loreDirUsage(b.usedChars, b.capChars)}
        </span>
      </div>
    );
  }

  const errors = (
    <>
      {budgetsError && (
        <div className="lore__error" data-testid="lore-load-error">
          {t.lore.loadError}
        </div>
      )}
      {actionError && (
        <div className="lore__error" data-testid="lore-action-error">
          {actionError}
        </div>
      )}
    </>
  );

  if (sectionKey === null) {
    const groups: { kind: DirGroupKind; label: string; defs: SectionDef[] }[] = [
      { kind: "everyone", label: t.lore.kindEveryone, defs: [] },
      { kind: "staff", label: t.lore.kindStaff, defs: [] },
      { kind: "outsource", label: t.lore.kindOutsource, defs: [] },
      { kind: "manual", label: t.lore.kindManual, defs: [] },
    ];
    const groupOf = (d: SectionDef): DirGroupKind => {
      if (d.kind !== "agent") return d.kind === "everyone" ? "everyone" : "manual";
      return avatarKindForMember({ id: d.scopeKey }) === "outsource" ? "outsource" : "staff";
    };
    for (const d of defs) groups.find((g) => g.kind === groupOf(d))?.defs.push(d);
    const shown = groups.filter((g) => g.defs.length > 0);
    const anyExpanded = shown.some((g) => !dirCollapsed.has(g.kind));
    const collapseLabel = anyExpanded ? t.office.groups.collapseAll : t.office.groups.expandAll;
    const toggleGroup = (kind: DirGroupKind) => {
      const next = new Set(dirCollapsed);
      if (next.has(kind)) next.delete(kind);
      else next.add(kind);
      onDirCollapsedChange(next);
    };
    return (
      <div className="lore" ref={scrollRef} data-testid="lore-page">
        {errors}
        <FilterPanel
          testId="lore-directory-filter"
          clearLabel={t.lore.clearFilters}
          onClear={
            dirSearch !== "" || dirApplied !== "" || dirNote
              ? () => {
                  setDirSearch("");
                  setDirApplied("");
                  setDirNote(null);
                }
              : undefined
          }
          collapse={{ title: t.lore.filterDialogTitle, activeCount: dirApplied !== "" ? 1 : 0 }}
          summary={
            typeof officeTotal === "number"
              ? msg.loreListedCount(officeTotal)
              : officeTotal === null && budgetsReady
                ? msg.loreListedCount(directoryEntryTotal(defs))
                : undefined
          }
          actions={
            <button
              type="button"
              className="filter-panel__trigger lore__collapse-all"
              aria-label={collapseLabel}
              title={collapseLabel}
              disabled={shown.length === 0}
              data-testid="lore-collapse-all"
              onClick={() => {
                const next = new Set(dirCollapsed);
                for (const g of shown) {
                  if (anyExpanded) next.add(g.kind);
                  else next.delete(g.kind);
                }
                onDirCollapsedChange(next);
              }}
            >
              {anyExpanded ? <ChevronsDownUpIcon size={14} /> : <ChevronsUpDownIcon size={14} />}
            </button>
          }
        >
          <FilterField label={t.lore.directoryIdLabel} wide>
            <IdFilterInput
              value={dirSearch}
              onChange={setDirSearch}
              onCommit={(v) => void jumpToEntry(v)}
              label={t.lore.directoryIdLabel}
              testId="lore-directory-id"
              widthCh={7}
            />
            {dirNote && (
              <span className="lore__dir-note" data-testid="lore-directory-panel-note">
                {dirNote}
              </span>
            )}
          </FilterField>
        </FilterPanel>
        {dirNote && (
          <div className="lore__dir-note" role="status" data-testid="lore-directory-note">
            {dirNote}
          </div>
        )}
        {shown.map((g) => {
          const collapsed = dirCollapsed.has(g.kind);
          return (
            <div className="lore__group" key={g.kind} data-testid={`lore-group-${g.kind}`}>
              <button
                type="button"
                className="chat-groups__heading"
                aria-expanded={!collapsed}
                data-testid={`lore-group-heading-${g.kind}`}
                onClick={() => toggleGroup(g.kind)}
              >
                <HeadingBody label={msg.groupHeading(g.label, g.defs.length)} collapsed={collapsed} unread={0} />
              </button>
              {!collapsed && g.defs.map((def) => (
                <a
                  key={def.key}
                  className="lore-dir-card lore-target"
                  data-testid="lore-target"
                  data-section={def.key}
                  href={formatHash(sectionRoute(def))}
                  onClick={(e) => {
                    e.preventDefault();
                    navigateHash(sectionRoute(def));
                  }}
                >
                  <div className="lore-dir-card__head">
                    {dirLead(def)}
                    <span className="lore-dir-card__main">
                      <span className="lore-dir-card__name" data-testid="lore-section-name">
                        {sectionName(def)}
                      </span>
                      {dirBadges(def)}
                    </span>
                    <span className="lore-dir-card__mark" aria-hidden="true">
                      <ChevronRightIcon size={14} />
                    </span>
                  </div>
                  {dirUsage(def)}
                </a>
              ))}
            </div>
          );
        })}
      </div>
    );
  }

  const sd = currentDef ? data[currentDef.key] : undefined;
  const listedCount = sd?.total ?? (sd?.exhausted ? sd.entries.length : null);
  const highlightMissing =
    highlightId !== "" &&
    !!sd &&
    !sd.loading &&
    !sd.fetchingMore &&
    !sd.moreFailed &&
    (sd.exhausted || hlCapped) &&
    !highlightLoaded;
  const highlightFailed =
    highlightId !== "" && !!sd && !sd.fetchingMore && sd.moreFailed && !highlightLoaded;

  return (
    <div className="lore" ref={scrollRef} data-testid="lore-page">
      <a
        className="lore__back"
        data-testid="lore-back"
        href="#lore"
        onClick={(e) => {
          e.preventDefault();
          navigateHash({ page: "lore" });
        }}
      >
        <ChevronLeftIcon size={14} />
        {t.lore.backToDirectory}
      </a>
      {errors}

      {budgetsReady && !currentDef && (
        <div className="lore__empty" data-testid="lore-section-missing">
          {t.lore.sectionMissing}
        </div>
      )}

      {currentDef && (
        <>
          <div className="lore-section__head" data-testid="lore-section-head">
            <div className="lore__group-title lore-section__title">
              {sectionLead(currentDef)}
              {currentDef.kind === "manual" ? (
                // The way back to the manual for every viewer; the card scope tag offers it only to those who may move entries.
                <button
                  type="button"
                  className="lore-section__name lore-section__name--link"
                  data-testid="lore-section-name"
                  title={msg.loreOpenManual(sectionName(currentDef))}
                  aria-label={msg.loreOpenManual(sectionName(currentDef))}
                  onClick={() => openManual(currentDef.scopeKey)}
                >
                  {sectionName(currentDef)}
                </button>
              ) : (
                <span className="lore-section__name" data-testid="lore-section-name">
                  {sectionName(currentDef)}
                </span>
              )}
              {currentDef.target.droppedCount > 0 && (
                <span className="lore-section__dropped" data-testid="lore-section-dropped">
                  {msg.loreDropped(currentDef.target.droppedCount)}
                </span>
              )}
            </div>
            {renderSummary(currentDef, sd)}
          </div>

          <FilterPanel
            testId="lore-filter"
            clearLabel={t.lore.clearFilters}
            onClear={activeFilterCount > 0 ? clearFilters : undefined}
            collapse={{ title: t.lore.filterDialogTitle, activeCount: activeFilterCount }}
            summary={listedCount == null ? undefined : msg.loreListedCount(listedCount)}
          >
            <FilterField label={t.lore.idSearchLabel} wide>
              <IdFilterInput
                value={searchDraft}
                onChange={setSearchDraft}
                onCommit={(v) => setAppliedSearch(v.trim())}
                label={t.lore.idSearchLabel}
                testId="lore-filter-id"
                widthCh={7}
              />
            </FilterField>
            <FilterField label={t.lore.filterTypeNoun}>
              <MultiSelectFilter
                noun={t.lore.filterTypeNoun}
                allLabel={t.lore.filterTypeAll}
                testId="lore-filter-type"
                options={LORE_TYPES.map((lt) => ({ value: lt, label: loreTypeLabel(t, lt) }))}
                selected={loreTypes}
                onChange={(next) => setLoreTypes(next as Set<LoreType>)}
              />
            </FilterField>
            <FilterField label={t.lore.filterStateNoun}>
              <MultiSelectFilter
                noun={t.lore.filterStateNoun}
                allLabel={t.lore.filterStateAll}
                testId="lore-filter-state"
                options={[
                  { value: "pinned", label: t.lore.statePinned },
                  { value: "active", label: t.lore.stateActive },
                  { value: "retired", label: t.lore.stateRetired },
                ]}
                selected={states}
                onChange={(next) => setStates(next as Set<LoreEntryState>)}
              />
            </FilterField>
          </FilterPanel>

          {highlightMissing && (
            <div className="lore__empty" data-testid="lore-highlight-missing">
              {msg.loreHighlightMissing(highlightId)}
            </div>
          )}
          {highlightFailed && (
            <div className="lore__error" data-testid="lore-highlight-failed">
              {msg.loreHighlightLoadFailed(highlightId)}
            </div>
          )}

          <section
            className="lore-section"
            data-testid="lore-section"
            data-section={currentDef.key}
          >
            <div className="lore-section__body" data-testid="lore-section-body">
              {narrowing && sd && !sd.loading && !sd.error && sd.entries.length === 0 ? (
                <div className="lore__empty" data-testid="lore-empty">
                  {t.lore.emptyFiltered}
                </div>
              ) : (
                renderSectionBody(currentDef, sd)
              )}
            </div>
          </section>
        </>
      )}

      {toast.length > 0 && (
        <div className="lore__dock">
          <div className="lore-toast" role="status" data-testid="lore-toast">
            {toast.map((m) => (
              <span key={m.id}>{msg.loreScopeChanged(m.id, m.scopeLabel)}</span>
            ))}
          </div>
        </div>
      )}

      {(hasStaged || saving) && (
        <div
          className="lore-savebar"
          ref={saveBarRef}
          role="region"
          aria-label={t.lore.stagedBarLabel}
          data-testid="lore-staged-bar"
        >
          <span className="lore-savebar__text">
            <span className="lore-savebar__count" data-testid="lore-staged-count">
              {msg.loreStagedChanges(stagedFields)}
            </span>
            {saving && leaveBlocked && (
              <span className="lore-savebar__note" role="status" data-testid="lore-staged-leave-blocked">
                {t.lore.stagedLeaveBlocked}
              </span>
            )}
            {saveFailed.length > 0 && (
              <span
                className="lore-savebar__note lore-savebar__note--failed"
                role="status"
                data-testid="lore-staged-failed"
              >
                {msg.loreStagedFailed(saveFailed)}
              </span>
            )}
          </span>
          <span className="lore-savebar__actions">
            <button
              type="button"
              className="doc-btn"
              data-testid="lore-staged-discard"
              disabled={saving}
              onClick={discardStaged}
            >
              {t.lore.stagedDiscard}
            </button>
            <button
              type="button"
              className="doc-btn doc-btn--accent"
              data-testid="lore-staged-save"
              disabled={saving || !hasStaged}
              onClick={() => void saveStaged()}
            >
              {saving ? t.lore.stagedSaving : t.lore.stagedSave}
            </button>
          </span>
        </div>
      )}
    </div>
  );
}

function stateLabelOf(t: ReturnType<typeof useI18n>["t"], s: LoreEntryState): string {
  return s === "pinned" ? t.lore.statePinned : s === "active" ? t.lore.stateActive : t.lore.stateRetired;
}

function loreTypeLabel(t: ReturnType<typeof useI18n>["t"], lt: LoreType): string {
  switch (lt) {
    case "instruction_conflict":
      return t.lore.loreTypeInstructionConflict;
    case "instruction_supplement":
      return t.lore.loreTypeInstructionSupplement;
    case "owner_decision":
      return t.lore.loreTypeOwnerDecision;
    case "owner_preference":
      return t.lore.loreTypeOwnerPreference;
    case "other":
      return t.lore.loreTypeOther;
  }
}

/** ONE 傳承 card. Folded, its body is cut to two lines and it has neither the
 * 編輯 icon nor the message box; open, the body is in full. A click on the card
 * (not on one of its controls, not ending a text selection), Enter or Space
 * folds or opens it, as on 任務卡; the chevron only marks that state. */
function LoreRow({
  entry,
  dimmed,
  expanded,
  onToggle,
  author,
  avatar,
  scope,
  scopeMenu,
  recipient,
  onOpenManual,
  onSetState,
  onSetScope,
  onSetLoreType,
  highlighted,
  stagedFrom,
  chipsLocked,
  onRevert,
  entryCaps,
  onSave,
  onRestored,
}: {
  entry: LoreEntryView;
  /** The server left it out of what members read: content greyed, 讀不到 tag. */
  dimmed: boolean;
  expanded: boolean;
  /** Folds or opens the card; null ⇒ this card stays open (it carries a staged
   * change). It does not fold either while its editor is open or its message
   * box holds unsent input. */
  onToggle: (() => void) | null;
  author: AuthorIdentity;
  avatar: { src?: string; kind: AvatarKind } | null;
  /** 屬於, already resolved (resolveScope). `manualKey` non-empty offers the
   * manual jump inside the 適用範圍 menu. */
  scope: { kind: "" | LoreScopeKind; label: string; manualKey: string };
  /** null ⇒ the author cannot be messaged (the owner's entry, or gone from
   * the roster): the card has no message box. */
  recipient: MessageRecipient | null;
  onOpenManual: (typeKey: string) => void;
  onSetState: (id: string, next: LoreEntryState) => void;
  /** null ⇒ the viewer may not switch this entry's scope; the scope tag is plain text. */
  scopeMenu: ScopeMenu | null;
  onSetScope: (id: string, kind: LoreScopeKind, label: string) => void;
  onSetLoreType: (id: string, loreType: LoreType) => void;
  highlighted: boolean;
  /** The stored value's label of each staged field; `entry` already shows the
   * staged values. null ⇒ nothing staged on this card. */
  stagedFrom: StagedFrom | null;
  /** 儲存變更 is running: the chips cannot change what is staged. */
  chipsLocked: boolean;
  onRevert: (id: string, field: StagedField) => void;
  entryCaps: { title: number; body: number };
  /** Rejects with the reason to show; the draft stays. */
  onSave: (id: string, patch: LoreEntryEdit) => Promise<void>;
  onRestored: (id: string, restored: Record<string, string> | undefined) => Promise<void>;
}) {
  const { t, msg } = useI18n();
  const staged = stagedFrom !== null;
  const [stateOpen, setStateOpen] = useState(false);
  const statusRef = useRef<HTMLDivElement>(null);
  const [typeOpen, setTypeOpen] = useState(false);
  const typeRef = useRef<HTMLDivElement>(null);
  const [scopeOpen, setScopeOpen] = useState(false);
  const scopeRef = useRef<HTMLSpanElement>(null);
  const [editing, setEditing] = useState(false);
  const [messageUnsent, setMessageUnsent] = useState(false);
  const toggle = expanded && (editing || (recipient !== null && messageUnsent)) ? null : onToggle;

  useEffect(() => {
    if (!scopeOpen) return;
    function onDown(e: MouseEvent) {
      if (!scopeRef.current?.contains(e.target as Node)) setScopeOpen(false);
    }
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [scopeOpen]);

  useEffect(() => {
    if (!stateOpen) return;
    function onDown(e: MouseEvent) {
      if (!statusRef.current?.contains(e.target as Node)) setStateOpen(false);
    }
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [stateOpen]);

  useEffect(() => {
    if (!typeOpen) return;
    function onDown(e: MouseEvent) {
      if (!typeRef.current?.contains(e.target as Node)) setTypeOpen(false);
    }
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [typeOpen]);

  const stateLabel = stateLabelOf(t, entry.state);

  function actionLabel(s: LoreEntryState): string {
    return s === "retired"
      ? t.lore.actionRetire
      : s === "active"
        ? t.lore.actionActivate
        : t.lore.actionPin;
  }

  return (
    <article
      className={`lore-row lore-row--${entry.state}${
        dimmed ? " lore-row--dimmed" : ""
      }${highlighted ? " lore-row--highlight" : ""}${staged ? " lore-row--staged" : ""}`}
      data-testid="lore-row"
      data-entry-id={entry.id}
      data-dimmed={dimmed ? "true" : "false"}
      data-staged={staged ? "true" : "false"}
      data-folded={expanded ? undefined : "true"}
      {...(toggle
        ? {
            role: "button",
            tabIndex: 0,
            "aria-expanded": expanded,
            "aria-label": expanded ? t.lore.collapseEntry : t.lore.expandEntry,
            // The message box's padding around its textarea is still the box.
            onClick: (e: React.MouseEvent<HTMLElement>) => {
              if (isCardToggleClick(e, ".lore-row__message")) toggle();
            },
            onKeyDown: (e: React.KeyboardEvent<HTMLElement>) => {
              if (isCardToggleKey(e)) toggle();
            },
          }
        : // Focusable from code only: a highlighted entry lands here.
          { tabIndex: -1 })}
    >
      <div className="lore-row__head">
        <div className="lore-row__badges">
          {/* Order: 狀態 icon → 編號 → 類型 → 適用範圍, the 任務卡's head. The
              status icon is also the 變更狀態 control; its menu hangs from its
              LEFT edge (.lore-row__status-pop pins left:0) because a
              right-anchored pop would open off the card. */}
          <div className="lore-row__status" ref={statusRef}>
            <button
              type="button"
              className={`lore-row__status-icon lore-row__status-icon--${entry.state}${
                stagedFrom?.state !== undefined ? " lore-row__status-icon--staged" : ""
              }`}
              title={stateLabel}
              aria-label={stateLabel}
              aria-haspopup="menu"
              aria-expanded={stateOpen}
              data-testid="lore-state"
              data-state={entry.state}
              disabled={chipsLocked}
              onClick={() => setStateOpen((o) => !o)}
            >
              <LoreStateIcon state={entry.state} />
            </button>
            {stagedFrom?.state !== undefined && (
              <LoreRevertMark
                field="state"
                from={stagedFrom.state}
                onRevert={() => onRevert(entry.id, "state")}
                disabled={chipsLocked}
              />
            )}
            {stateOpen && (
              <div
                className="lore-row__menu-pop lore-row__status-pop"
                role="menu"
                aria-label={t.lore.stateMenuLabel}
                data-testid="lore-state-options"
              >
                {STATE_ACTIONS.map((s) => (
                  <button
                    key={s}
                    type="button"
                    role="menuitem"
                    // 「當前值用強調色＋700」 IS this modifier — the same one the
                    // 任務卡's 優先權/狀態 menus use (tasks.css
                    // .task-card__menu-item--active), copied into lore.css.
                    className={`lore-row__menu-item${
                      entry.state === s ? " lore-row__menu-item--active" : ""
                    }`}
                    data-testid={`lore-state-${s}`}
                    onClick={() => {
                      setStateOpen(false);
                      if (entry.state !== s) onSetState(entry.id, s);
                    }}
                  >
                    {actionLabel(s)}
                  </button>
                ))}
              </div>
            )}
          </div>

          <CopyIdChip
            value={entry.id}
            text={`#${entry.id}`}
            label={msg.loreCopyEntryId(entry.id)}
            copiedLabel={t.lore.entryIdCopied}
            className="lore-row__id-badge"
            copiedClassName="lore-row__id-badge-copied"
            testId="lore-entry-id"
            copiedTestId="lore-entry-id-copied"
          />

          <div className="lore-row__status" ref={typeRef}>
            <button
              type="button"
              className={`lore-badge lore-badge--lore-type lore-row__status-chip${
                stagedFrom?.loreType !== undefined ? " lore-row__status-chip--staged" : ""
              }`}
              title={t.lore.typeMenuLabel}
              aria-label={t.lore.typeMenuLabel}
              aria-haspopup="menu"
              aria-expanded={typeOpen}
              data-testid="lore-type"
              data-lore-type={entry.loreType}
              disabled={chipsLocked}
              onClick={() => setTypeOpen((o) => !o)}
            >
              {loreTypeLabel(t, entry.loreType)}
            </button>
            {stagedFrom?.loreType !== undefined && (
              <LoreRevertMark
                field="loreType"
                from={stagedFrom.loreType}
                onRevert={() => onRevert(entry.id, "loreType")}
                disabled={chipsLocked}
              />
            )}
            {typeOpen && (
              <div
                className="lore-row__menu-pop lore-row__status-pop"
                role="menu"
                aria-label={t.lore.typeMenuLabel}
                data-testid="lore-type-options"
              >
                {LORE_TYPES.map((lt) => (
                  <button
                    key={lt}
                    type="button"
                    role="menuitem"
                    className={`lore-row__menu-item${
                      entry.loreType === lt ? " lore-row__menu-item--active" : ""
                    }`}
                    data-testid={`lore-type-${lt}`}
                    onClick={() => {
                      setTypeOpen(false);
                      if (entry.loreType !== lt) onSetLoreType(entry.id, lt);
                    }}
                  >
                    {loreTypeLabel(t, lt)}
                  </button>
                ))}
              </div>
            )}
          </div>

          {/* 適用範圍 rides the tag row as the last tag. A viewer who may move
              the entry gets the same badge-is-the-control pattern as 類型;
              anyone else reads it as plain text. The menu hangs from
              .lore-row__head (this wrapper is not positioned) so a tag that
              wrapped to the row's right edge cannot push the menu off a phone. */}
          {scopeMenu ? (
            <span className="lore-row__scope" data-testid="lore-scope" ref={scopeRef}>
              <span className="lore-row__scope-anchor">
              <button
                type="button"
                className={`lore-badge lore-badge--scope lore-row__status-chip${
                  stagedFrom?.scope !== undefined ? " lore-row__status-chip--staged" : ""
                }`}
                data-testid="lore-scope-name"
                data-scope-kind={scope.kind}
                title={t.lore.scopeMenuLabel}
                aria-label={`${t.lore.scopeMenuLabel}：${scope.label}`}
                aria-haspopup="menu"
                aria-expanded={scopeOpen}
                disabled={chipsLocked}
                onClick={() => setScopeOpen((o) => !o)}
              >
                <span className="lore-badge__text">{scope.label}</span>
              </button>
              {stagedFrom?.scope !== undefined && (
                <LoreRevertMark
                  field="scope"
                  from={stagedFrom.scope}
                  onRevert={() => onRevert(entry.id, "scope")}
                  disabled={chipsLocked}
                />
              )}
              </span>
              {scopeOpen && (
                <div
                  className="lore-row__menu-pop lore-row__scope-pop"
                  role="menu"
                  aria-label={t.lore.scopeMenuLabel}
                  data-testid="lore-scope-options"
                >
                  <div className="lore-row__scope-pop-title">{t.lore.scopeMenuLabel}</div>
                  {scopeMenu.choices.map((c) => {
                    const current = c.kind === entry.scopeKind;
                    const tag =
                      c.kind === scopeMenu.defaultKind
                        ? t.lore.scopeTagDefault
                        : current
                          ? t.lore.scopeTagCurrent
                          : "";
                    return (
                      <button
                        key={c.kind}
                        type="button"
                        role="menuitemradio"
                        aria-checked={current}
                        className={`lore-row__menu-item lore-row__scope-option${
                          current ? " lore-row__menu-item--active" : ""
                        }`}
                        data-testid={`lore-scope-${c.kind}`}
                        onClick={() => {
                          setScopeOpen(false);
                          if (!current) onSetScope(entry.id, c.kind, c.label);
                        }}
                      >
                        <span className="lore-row__scope-check" aria-hidden="true">
                          {current && <CheckIcon size={14} />}
                        </span>
                        <span className="lore-row__scope-option-head">
                          <span data-testid="lore-scope-option-label">{c.label}</span>
                          {tag !== "" && (
                            <span
                              className="lore-row__scope-tag"
                              data-testid="lore-scope-option-tag"
                            >
                              {tag}
                            </span>
                          )}
                        </span>
                      </button>
                    );
                  })}
                  {scope.manualKey !== "" && (
                    <button
                      type="button"
                      role="menuitem"
                      className="lore-row__menu-item lore-row__scope-open-manual"
                      data-testid="lore-scope-open-manual"
                      onClick={() => {
                        setScopeOpen(false);
                        onOpenManual(scope.manualKey);
                      }}
                    >
                      {msg.loreOpenManual(scope.label)}
                    </button>
                  )}
                </div>
              )}
            </span>
          ) : (
            <span
              className="lore-badge lore-badge--scope"
              data-testid="lore-scope-name"
              data-scope-kind={scope.kind}
              title={t.lore.scopeMenuLabel}
            >
              <span className="lore-badge__text">{scope.label}</span>
            </span>
          )}
          {dimmed && (
            <span className="lore-badge lore-badge--dropped" data-testid="lore-dropped-tag">
              {t.lore.droppedTag}
            </span>
          )}
        </div>

        <div className="lore-row__actions">
          {expanded && (
            <button
              type="button"
              className="lore-row__icon-btn"
              data-testid="lore-edit"
              aria-label={t.lore.actionEdit}
              title={t.lore.actionEdit}
              disabled={editing}
              onClick={() => setEditing(true)}
            >
              <PencilIcon size={15} />
            </button>
          )}
          {toggle ? (
            <span
              className={`lore-row__fold${expanded ? " lore-row__fold--open" : ""}`}
              data-testid="lore-fold-mark"
              aria-hidden="true"
              title={expanded ? t.lore.collapseEntry : t.lore.expandEntry}
            >
              <ChevronRightIcon size={18} />
            </span>
          ) : (
            // Holds the chevron's width: staging a change must not rewrap the
            // tags and change the card's height.
            <span className="lore-row__fold lore-row__fold--held" aria-hidden="true" />
          )}
        </div>
      </div>

      {expanded && editing ? (
        <LoreEntryEditor
          entry={entry}
          caps={entryCaps}
          onRestored={(restored) => onRestored(entry.id, restored)}
          onCancel={() => setEditing(false)}
          onSave={async (patch) => {
            await onSave(entry.id, patch);
            setEditing(false);
          }}
        />
      ) : (
        <>
          <h3 className="lore-row__title">{entry.title}</h3>

          {/* 🔴 MARKDOWN GOES THROUGH THE SHARED, XSS-SAFE RENDERER, exactly as the
              chat bubble does. Never dangerouslySetInnerHTML: a 傳承 body is
              agent-authored free text. `breaks` for the same reason chat turns it
              on — this text was WRITTEN as plain text, where Enter meant "new
              line", and standard markdown's soft-wrap would silently join every
              such line into one run-on.

              ⚠️ Stored bodies were written under a plain-text contract, so
              characters that are now syntax are read as syntax. This follows chat
              (one behaviour for one kind of text). The renderer's inline set is
              only `**bold**`, `` `code` `` and links; `_` is not syntax, so a body
              carrying `note_size_chars` is unaffected. */}
          <div
            className={`lore-row__body${expanded ? "" : " lore-row__body--clamped"}`}
            data-testid="lore-body"
          >
            <Markdown source={entry.body} className="lore-row__body-md doc-md" breaks />
          </div>
        </>
      )}

      <div className="lore-row__meta">
        <span className="lore-row__meta-label">{t.lore.authorLabel}</span>
        <LoreAuthorChip author={author} avatar={avatar} />
        <span className="lore-row__meta-label">{t.lore.effectiveLabel}</span>
        <span className="lore-row__effective-ts" data-testid="lore-effective">
          {loreEffectiveText(entry)}
        </span>
        {/* No reason, no row: an empty one would teach that retirements come unexplained. */}
        {entry.retireReason !== "" && (
          <div className="lore-row__meta-row" data-testid="lore-retire-reason">
            <span className="lore-row__meta-label">{t.lore.retireReasonLabel}</span>
            <span className="lore-row__retire">{entry.retireReason}</span>
          </div>
        )}
      </div>
      {expanded && recipient !== null && (
        <LoreMessageBox
          entryId={entry.id}
          recipient={recipient}
          onUnsentChange={setMessageUnsent}
        />
      )}
    </article>
  );
}

/** The 狀態 icon: 置頂 a hollow pin, 生效中 a big check in a circle, 已失效 a
 * slashed circle. */
function LoreStateIcon({ state }: { state: LoreEntryState }) {
  return state === "pinned" ? (
    <PinIcon size={18} />
  ) : state === "active" ? (
    <CircleCheckBigIcon size={18} />
  ) : (
    <CircleSlashIcon size={18} />
  );
}

/** ↺ on a staged tag's top-right corner: hover names the value it goes back
 * to, and any click or tap reverts that one field at once. */
function LoreRevertMark({
  field,
  from,
  onRevert,
  disabled,
}: {
  field: StagedField;
  from: string;
  onRevert: () => void;
  disabled: boolean;
}) {
  const { msg } = useI18n();
  const label = msg.loreStagedRevertTo(from);
  return (
    <InstantHint
      hint={label}
      onActivate={onRevert}
      disabled={disabled}
      className="lore-row__revert"
      aria-label={label}
      data-testid={`lore-revert-${field}`}
    >
      <RotateCcwIcon size={9} />
    </InstantHint>
  );
}

/** 編輯 turns the card's title and body into this, in place. */
function LoreEntryEditor({
  entry,
  caps,
  onRestored,
  onCancel,
  onSave,
}: {
  entry: LoreEntryView;
  caps: { title: number; body: number };
  /** Re-reads the card after a 版本紀錄 restore of `restored` has landed. */
  onRestored: (restored: Record<string, string> | undefined) => Promise<void>;
  onCancel: () => void;
  onSave: (patch: LoreEntryEdit) => Promise<void>;
}): ReactNode {
  const { t } = useI18n();
  const [title, setTitle] = useState(entry.title);
  const [body, setBody] = useState(entry.body);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const bodyRef = useRef<HTMLTextAreaElement>(null);
  useLayoutEffect(() => {
    if (bodyRef.current) autosizeTextarea(bodyRef.current);
  }, [body]);

  const titleBlank = title.trim() === "";
  const nextTitle = title.trim();
  const nextBody = body.trim();
  const over =
    editFieldBlocked(nextTitle, entry.title, caps.title) ||
    editFieldBlocked(nextBody, entry.body, caps.body);
  const changed = nextTitle !== entry.title || nextBody !== entry.body;
  const canSave = !saving && changed && !titleBlank && !over;

  async function save() {
    if (!canSave) return;
    const patch: LoreEntryEdit = {};
    if (nextTitle !== entry.title) patch.title = nextTitle;
    if (nextBody !== entry.body) patch.body = nextBody;
    setSaving(true);
    setError(null);
    try {
      await onSave(patch);
    } catch (e) {
      setError(e instanceof Error && e.message ? e.message : t.lore.actionFailed);
      setSaving(false);
    }
  }

  return (
    <div className="lore-row__editor" data-testid="lore-editor">
      <label className="lore-row__editor-field">
        <span className="lore-row__editor-label">{t.lore.editTitleLabel}</span>
        <input
          type="text"
          className="lore-row__editor-input"
          data-testid="lore-edit-title"
          value={title}
          disabled={saving}
          onChange={(e) => setTitle(e.target.value)}
        />
      </label>
      <label className="lore-row__editor-field">
        <span className="lore-row__editor-label">{t.lore.editBodyLabel}</span>
        <textarea
          ref={bodyRef}
          className="lore-row__editor-input lore-row__editor-body"
          data-testid="lore-edit-body"
          rows={3}
          value={body}
          disabled={saving}
          onChange={(e) => setBody(e.target.value)}
        />
      </label>
      {(titleBlank || over || error) && (
        <div className="lore-row__editor-error" role="status" data-testid="lore-edit-error">
          {error ?? (titleBlank ? t.lore.editTitleBlank : t.lore.editOverCap)}
        </div>
      )}
      <div className="lore-row__editor-buttons">
        {/* 版本紀錄 lives in edit mode only, left of 取消/儲存, as on DocCard. */}
        <DocumentHistoryEntry
          kind="lore_entry"
          docKey={entry.id}
          title={`#${entry.id} ${entry.title}`}
          currentContent={{ title: entry.title, body: entry.body }}
          disabled={saving}
          onRestored={async (restored) => {
            // The restore already landed, so the draft is stale whatever the
            // re-read does: leave edit mode either way, as DocCard does.
            try {
              await onRestored(restored);
            } finally {
              onCancel();
            }
          }}
        />
        <button
          type="button"
          className="lore-row__action"
          data-testid="lore-edit-cancel"
          disabled={saving}
          onClick={onCancel}
        >
          {t.lore.editCancel}
        </button>
        <button
          type="button"
          className="lore-row__editor-save"
          data-testid="lore-edit-save"
          disabled={!canSave}
          onClick={() => void save()}
        >
          {saving ? t.lore.editSaving : t.lore.editSave}
        </button>
      </div>
    </div>
  );
}

/** The 撰寫人 pill: who wrote the entry, never a way to reach them — the
 * message box under the card is that. */
function LoreAuthorChip({
  author,
  avatar,
}: {
  author: AuthorIdentity;
  avatar: { src?: string; kind: AvatarKind } | null;
}): ReactNode {
  return (
    <span className="lore-row__chip" data-testid="lore-author-row">
      {author.peerId !== "" && avatar ? (
        <Avatar size={18} kind={avatar.kind} src={avatar.src} />
      ) : null}
      <span className="lore-row__author" data-testid="lore-author">
        {author.name}
      </span>
    </span>
  );
}

/** The 任務卡 message box on a 傳承 card: one message to `recipient`, sent
 * without leaving the page, with the entry id leading it.
 *
 * `[LoreID=…]` is a literal, not an i18n key: the reader is an agent, and a
 * prefix that changed with the cockpit's language could not be matched on.
 * `meta.lore_entry_id` carries the same id for a program, so nothing has to
 * parse the human-facing prefix. */
function LoreMessageBox({
  entryId,
  recipient,
  onUnsentChange,
}: {
  entryId: string;
  recipient: MessageRecipient;
  /** Whether typed text or staged files are waiting to be sent. */
  onUnsentChange: (unsent: boolean) => void;
}): ReactNode {
  const { t } = useI18n();
  const isMobile = useIsMobile();
  const suggestedReplies = useSuggestedRepliesLoreMessage();
  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);
  const [failed, setFailed] = useState(false);
  const boxRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const isComposingRef = useRef(false);
  const {
    pendingAttachments,
    attachError,
    onPaste,
    onPickFile,
    removeAttachment,
    clearAttachments,
  } = useAttachmentStaging(STAGING_TARGET_PER_MOUNT);
  const canSend =
    !sending &&
    recipient.roster === undefined &&
    (draft.trim().length > 0 || pendingAttachments.length > 0);
  const placeholder =
    recipient.roster === "loading"
      ? t.lore.loadingMore
      : recipient.roster === "error"
        ? t.office.loadError
        : t.tasks.messagePlaceholder(recipient.name);

  useLayoutEffect(() => {
    if (boxRef.current) autosizeTextarea(boxRef.current);
  }, [draft]);
  const unsent = draft !== "" || pendingAttachments.length > 0;
  useEffect(() => {
    onUnsentChange(unsent);
  }, [unsent, onUnsentChange]);

  async function send() {
    if (!canSend) return;
    const attachments: ChatAttachmentInput[] = pendingAttachments.map((a) => ({
      dataB64: a.dataUri,
      ...(a.filename ? { filename: a.filename } : {}),
    }));
    setSending(true);
    try {
      await api.postChat({
        to: recipient.id,
        body: `[LoreID=${entryId}] ${draft.trim()}`,
        meta: { lore_entry_id: entryId },
        ...(attachments.length > 0 ? { attachments } : {}),
      });
      setDraft("");
      clearAttachments();
      setFailed(false);
    } catch (e) {
      console.warn("LorePage: lore message failed", e);
      setFailed(true);
    } finally {
      setSending(false);
    }
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (enterShouldSend(e, { isMobile, composing: isComposingRef.current })) {
      e.preventDefault();
      void send();
    }
  }

  return (
    <div className="lore-row__message" data-testid="lore-message-box">
      {(pendingAttachments.length > 0 || attachError) && (
        <ComposerAttachmentPreview
          pendingAttachments={pendingAttachments}
          attachError={attachError}
          onRemove={removeAttachment}
        />
      )}
      <div className="task-card__composer">
        <input
          ref={fileInputRef}
          className="chat__file-input"
          type="file"
          accept={ATTACH_ACCEPT}
          multiple
          onChange={onPickFile}
          hidden
        />
        <button
          type="button"
          className="chat__attach"
          aria-label={t.chat.attachLabel}
          title={t.chat.attachLabel}
          disabled={sending}
          data-testid="lore-msg-attach"
          onClick={() => fileInputRef.current?.click()}
        >
          <PaperclipIcon size={18} />
        </button>
        <textarea
          ref={boxRef}
          className="chat__input"
          rows={1}
          value={draft}
          disabled={sending}
          placeholder={placeholder}
          aria-label={placeholder}
          data-testid="lore-msg-input"
          data-recipient={recipient.id}
          onChange={(e) => setDraft(e.target.value)}
          onCompositionStart={() => {
            isComposingRef.current = true;
          }}
          onCompositionEnd={(e) => {
            isComposingRef.current = false;
            setDraft(e.currentTarget.value);
          }}
          onKeyDown={onKeyDown}
          onPaste={onPaste}
        ></textarea>
        <button
          type="button"
          className="task-card__send"
          disabled={!canSend}
          data-testid="lore-msg-send"
          onClick={() => void send()}
        >
          <SendIcon size={14} />
          {t.tasks.send}
        </button>
      </div>
      <SuggestedReplies
        replies={suggestedReplies}
        testId="lore-suggested-replies"
        onPick={(suggestion) => {
          setDraft((cur) => appendSuggestion(cur, suggestion));
          boxRef.current?.focus();
        }}
      />
      {failed && (
        <div className="lore-row__message-error" role="status" data-testid="lore-msg-failed">
          {t.tasks.messageError}
        </div>
      )}
    </div>
  );
}
