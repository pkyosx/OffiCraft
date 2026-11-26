// LorePage — the 傳承 tab. `#lore` is a directory of boot-document targets
// (所有人, each member, each task manual) with their budget numbers; each target
// opens its own section page (`#lore/<kind>/<key>`) holding that section's cards
// and filters.
//
// 🔴 EVERY BUDGET NUMBER IS THE SERVER'S. The section headers read
// `listLoreBudgets`, a section page's sentence and 上限線 read the
// `capChars`/`usedChars`/`firstDroppedId`/`budgetSegments` its own list page
// answered. Adding up title+body lengths here would be a second copy of the
// server's picking walk, wrong on every page after the first and wrong for a
// member whose 所有人 entries come first; a line in the wrong place looks
// exactly like one in the right place.
//
// 🔴 THE FILTER TRAVELS WITH THE PAGE REQUEST, never as a `.filter()` over
// downloaded rows; a client-side filter is right only for the rows that
// happen to be downloaded.
//
// 🔴 A MUTATION NEVER RELOADS A SECTION FROM PAGE ONE (owner: 編輯完不要自動
// refresh). The changed row is re-read by id and replaced where it stands, only
// the numbers are re-asked, and rows re-sort only when a section is reopened
// or the filter changes.

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
import {
  useWorkerAvatarUrls,
  useWorkerCodenames,
} from "../hooks/useWorkerCodenames";
import { avatarKindForMember } from "../lib/avatarKind";
import { isSyntheticSender } from "../lib/syntheticSender";
import type { AvatarKind } from "../lib/themeBundle";
import { copyText } from "../lib/clipboard";
import { formatAbsolute } from "../lib/dateFormat";
import { autosizeTextarea } from "../lib/autosize";
import {
  formatHash,
  navigateHash,
  useHashRoute,
  type HashRoute,
  type LoreSectionKind,
} from "../lib/hashRoute";
import { Avatar } from "./Avatar";
import { DocumentHistoryEntry } from "./DocumentHistoryEntry";
import { FilterPanel } from "./FilterPanel";
import { IdFilterInput } from "./IdFilterInput";
import { Markdown } from "./Markdown";
import { MultiSelectFilter } from "./MultiSelectFilter";
import {
  BookIcon,
  ChatBubbleIcon,
  CheckIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  PencilIcon,
  UsersIcon,
} from "./icons";
import "./lore.css";
// The body renders through <Markdown>'s `.doc-md` skin from settings.css;
// imported here rather than relied on transitively (css-layout-traps.md).
import "./settings.css";

/** 捲到底一次載這麼多. A page shorter than this is the last one. */
const PAGE = 30;

/** A section's bottom is "reached" within this many px of the scrollport's
 * bottom (the same margin ChatArea uses). */
const NEAR_BOTTOM_PX = 120;

/** The state-change menu, in the order the owner asked for: 失效 / 生效 / 置頂. */
const STATE_ACTIONS: readonly LoreEntryState[] = ["retired", "active", "pinned"];

/** What the 狀態 filter starts on: retired entries stay hidden until ticked. */
const DEFAULT_STATES: readonly LoreEntryState[] = ["pinned", "active"];

/** How one author id resolves against the LIVE roster. `peerId === ""` means
 * "not reachable" and is what drops the 傳訊息 icon — see LoreAuthorChip. */
interface AuthorIdentity {
  text: string;
  peerId: string;
}

interface ScopeChoice {
  kind: LoreScopeKind;
  label: string;
}

interface ScopeMenu {
  choices: ScopeChoice[];
  defaultKind: LoreScopeKind;
}

type CapInfo = Omit<LoreEntryPageView, "entries" | "limit" | "offset">;

const NO_CAP: CapInfo = {
  capChars: 0,
  firstDroppedId: "",
  usedChars: 0,
  firstDroppedChars: 0,
  budgetSegments: [],
};

function capOf(page: LoreEntryPageView): CapInfo {
  return {
    capChars: page.capChars,
    firstDroppedId: page.firstDroppedId,
    usedChars: page.usedChars,
    firstDroppedChars: page.firstDroppedChars,
    budgetSegments: page.budgetSegments,
  };
}

interface SectionData {
  entries: LoreEntryView[];
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

/** The scope half of a section's request. 所有人 carries no key. */
function scopeOpts(def: { kind: LoreScopeKind; scopeKey: string }): LoreListOptions {
  return def.kind === "everyone"
    ? { scopeKinds: ["everyone"] }
    : { scopeKinds: [def.kind], scopeKeys: [def.scopeKey] };
}

/** Characters as the cap counts them: Unicode code points of title + body. */
function entryChars(title: string, body: string): number {
  return [...title].length + [...body].length;
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

/** A changed card keeps its place until the section reloads, even when its
 * new state would sort it elsewhere or the filter no longer matches it: the
 * owner changes several cards in one pass and must not lose the spot they were
 * working at. */
function placeChangedRows(
  entries: LoreEntryView[],
  fresh: Map<string, LoreEntryView>
): LoreEntryView[] {
  return entries.map((e) => fresh.get(e.id) ?? e);
}

/** Where the 上限線 falls among the rows on screen, or -1 when the first
 * dropped entry is not among them. */
function cutIndexOf(data: SectionData): number {
  if (data.cap.capChars <= 0 || data.cap.firstDroppedId === "") return -1;
  return data.entries.findIndex((e) => e.id === data.cap.firstDroppedId);
}

type EntrySeed = { key: string; entryId: string; state: LoreEntryState };

/** What every section visit reads and nothing in a visit changes. Held by the
 * outer LorePage so a section change does not re-fetch the roster (expensive)
 * and the cards do not flicker to raw ids and plain-text badges meanwhile. */
interface LoreShared {
  members: ReturnType<typeof useMembers>["members"];
  membersLoading: boolean;
  workers: ReturnType<typeof useOutsourceWorkers>["workers"];
  manuals: ReturnType<typeof useTaskManuals>["manuals"];
  entryCaps: { title: number; body: number };
}

export function LorePage({ canSetScope }: { canSetScope?: boolean } = {}) {
  const { members, loading: membersLoading } = useMembers();
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
    () => ({ members, membersLoading, workers, manuals, entryCaps }),
    [members, membersLoading, workers, manuals, entryCaps]
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
}: {
  sectionKey: string | null;
  highlightId: string;
  canSetScope?: boolean;
  shared: LoreShared;
  seedRef: { current: EntrySeed | null };
  budgets: LoreBudgetTargetView[] | null;
  budgetsError: boolean;
  refreshBudgets: () => Promise<void>;
}) {
  const { t, msg } = useI18n();
  const { members, membersLoading, workers, manuals, entryCaps } = shared;
  const canSetScope = canSetScopeProp ?? viewerMaySetLoreScope(members);

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
  // landing last would move the 上限線 back.
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
      if (id === "") return { text: t.lore.authorUnknown, peerId: "" };
      const m = members.find((x) => x.id === id);
      if (m) return { text: m.name, peerId: id };
      if (id.startsWith("ow-")) {
        // 🔴 The codename and the reachability come from different lists: a
        // released worker still wrote this entry and keeps his name, but only
        // the LIVE roster may offer a chat with him — the server refuses a
        // message to someone no longer on it.
        const live = workers.find((x) => x.id === id);
        const cn = live?.codename ?? unlistedNames.get(id);
        return {
          text: cn ? msg.outsourceLabel(cn) : id,
          peerId: live ? id : "",
        };
      }
      // Not on the roster: the entry stays, only the live affordance goes.
      return { text: unlistedNames.get(id) ?? id, peerId: "" };
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

  // ── after a mutation: re-read the changed rows by id and the numbers, in
  // place. Never page one again.
  const refreshInPlace = useCallback(
    async (changed: Map<string, string[]>, everyoneTouched: boolean) => {
      const byKey = new Map(defsRef.current.map((d) => [d.key, d]));
      const work: Promise<void>[] = [];
      for (const key of openRef.current) {
        const def = byKey.get(key);
        if (!def) continue;
        const ids = changed.get(key) ?? [];
        if (ids.length === 0 && !everyoneTouched) continue;
        const gen = genRef.current.get(key) ?? 0;
        const capSeq = nextCapSeq(key);
        work.push(
          (async () => {
            // The other filters are left off on purpose: a row the reader just
            // retired must come back so its card can show it.
            const page = await api.listLoreEntries({
              ...scopeOpts(def),
              ...(ids.length > 0 ? { entryIds: ids, limit: ids.length } : { limit: 1 }),
              offset: 0,
            });
            const fresh = new Map(page.entries.map((e) => [e.id, e]));
            const moved = ids.filter((id) => !fresh.has(id));
            if (moved.length > 0) {
              const elsewhere = await api.listLoreEntries({
                entryIds: moved,
                limit: moved.length,
                offset: 0,
              });
              for (const e of elsewhere.entries) fresh.set(e.id, e);
            }
            if (genRef.current.get(key) !== gen) return;
            patchSection(key, (p) => ({
              ...p,
              entries: placeChangedRows(p.entries, fresh),
              cap: isLatestCap(key, capSeq) ? capOf(page) : p.cap,
            }));
          })().catch((e) => console.warn("LorePage: refresh failed", e))
        );
      }
      work.push(refreshBudgets());
      await Promise.all(work);
    },
    [patchSection, refreshBudgets]
  );

  const sectionOfEntry = useCallback((id: string): string | null => {
    for (const [key, d] of Object.entries(dataRef.current)) {
      if (d.entries.some((e) => e.id === id)) return key;
    }
    return null;
  }, []);

  const failText = useCallback(
    (e: unknown) =>
      serverMessageOf(e) || (isHttpStatus(e, 403) ? t.lore.forbidden : t.lore.actionFailed),
    [t.lore.forbidden, t.lore.actionFailed]
  );

  const runOne = useCallback(
    async (id: string, fn: () => Promise<unknown>, scopeMoves = false) => {
      setActionError(null);
      const key = sectionOfEntry(id);
      const entry = key ? dataRef.current[key]?.entries.find((e) => e.id === id) : undefined;
      try {
        await fn();
      } catch (e) {
        // A 403 is a reachable answer (置頂 is admin-only, the rest is
        // author-or-admin); the server's own sentence says which rule refused.
        setActionError(failText(e));
        throw e;
      }
      const changed = new Map<string, string[]>();
      if (key) changed.set(key, [id]);
      await refreshInPlace(changed, scopeMoves || entry?.scopeKind === "everyone");
    },
    [failText, refreshInPlace, sectionOfEntry]
  );

  const setEntryState = useCallback(
    (id: string, next: LoreEntryState) =>
      void runOne(id, () => api.setLoreEntryState(id, next)).catch(() => {}),
    [runOne]
  );
  // The 適用範圍 notice. A changed card keeps its place on this section page even
  // when its new scope belongs to another section, so the notice is what says
  // the move landed and where the entry now lives.
  const [toast, setToast] = useState<{ id: string; scopeLabel: string } | null>(null);
  useEffect(() => {
    if (!toast) return;
    const timer = window.setTimeout(() => setToast(null), TOAST_MS);
    return () => window.clearTimeout(timer);
  }, [toast]);

  const setEntryScope = useCallback(
    (id: string, kind: LoreScopeKind, label: string) =>
      void runOne(id, () => api.setLoreEntryScope(id, kind), true)
        .then(() => setToast({ id, scopeLabel: label }))
        .catch(() => {}),
    [runOne]
  );
  const setEntryLoreType = useCallback(
    (id: string, loreType: LoreType) =>
      void runOne(id, () => api.editLoreEntry(id, { loreType })).catch(() => {}),
    [runOne]
  );
  /** Rejects with the reason so the editor can keep the draft and say why. */
  const editEntry = useCallback(
    async (id: string, patch: LoreEntryEdit): Promise<void> => {
      setActionError(null);
      const key = sectionOfEntry(id);
      const entry = key ? dataRef.current[key]?.entries.find((e) => e.id === id) : undefined;
      try {
        await api.editLoreEntry(id, patch);
      } catch (e) {
        throw new Error(failText(e));
      }
      const changed = new Map<string, string[]>();
      if (key) changed.set(key, [id]);
      await refreshInPlace(changed, entry?.scopeKind === "everyone");
    },
    [failText, refreshInPlace, sectionOfEntry]
  );

  /** A restore already landed; this only re-reads the card where it stands. */
  const restoredEntry = useCallback(
    async (id: string) => {
      const key = sectionOfEntry(id);
      const entry = key ? dataRef.current[key]?.entries.find((e) => e.id === id) : undefined;
      const changed = new Map<string, string[]>();
      if (key) changed.set(key, [id]);
      await refreshInPlace(changed, entry?.scopeKind === "everyone");
    },
    [refreshInPlace, sectionOfEntry]
  );

  // ── 目錄的編號搜尋 ──────────────────────────────────────────────────────
  const [dirSearch, setDirSearch] = useState("");
  const [dirNote, setDirNote] = useState<string | null>(null);
  async function jumpToEntry(raw: string) {
    setDirNote(null);
    const v = raw.trim();
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
  const scrolledToRef = useRef("");
  useEffect(() => {
    if (!highlightLoaded) {
      scrolledToRef.current = "";
      return;
    }
    if (scrolledToRef.current === highlightId) return;
    scrolledToRef.current = highlightId;
    const el = rowElement(scrollRef.current, highlightId);
    el?.scrollIntoView?.({ block: "center" });
    el?.focus({ preventScroll: true });
  }, [highlightLoaded, highlightId]);

  // ── render helpers ──────────────────────────────────────────────────────
  // The section's own boot document, drawn as one bar: 所有人's entries are
  // read first (member sections only), then the target's own, up to the cap.
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
    const cut = cutIndexOf(sd);
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
        <div className="lore-dir-card__legend" data-testid="lore-section-summary">
          {everyoneSeg && (
            <span className="lore-dir-card__legend-item">
              <i className="lore-dir-card__swatch lore-dir-card__swatch--everyone" aria-hidden="true" />
              {msg.loreLegendSegment(
                t.lore.kindEveryone,
                everyoneSeg.entryCount,
                everyoneSeg.fittedCount,
                everyoneSeg.fittedChars,
                true
              )}
            </span>
          )}
          <span className="lore-dir-card__legend-item">
            <i className="lore-dir-card__swatch" aria-hidden="true" />
            {msg.loreLegendSegment(
              sectionName(def),
              own?.entryCount ?? 0,
              own?.fittedCount ?? 0,
              own?.fittedChars ?? 0,
              false
            )}
          </span>
          <span className="lore-dir-card__legend-item">
            {msg.loreLegendLeft(cap.capChars, Math.max(0, cap.capChars - cap.usedChars))}
          </span>
        </div>
        {cap.firstDroppedId !== "" && cut < 0 && !sd.loading && (
          <div className="lore-section__cap-away" data-testid="lore-cap-away">
            {msg.loreCapLineAway(cap.firstDroppedId)}
          </div>
        )}
      </>
    );
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
    const cut = cutIndexOf(sd);
    return (
      <>
        {sd.entries.length === 0 && (
          <div className="lore__empty" data-testid="lore-section-empty">
            {t.lore.sectionEmpty}
          </div>
        )}
        {sd.entries.map((entry, pos) => (
          <div key={entry.id} className="lore-section__item">
            {cut >= 0 && pos === cut && (
              <div className="lore__cap-line" data-testid="lore-cap-line">
                <span className="lore__cap-line-text">
                  {msg.loreCapLine(
                    sd.cap.usedChars,
                    sd.cap.capChars,
                    sd.cap.firstDroppedId,
                    sd.cap.firstDroppedChars
                  )}
                </span>
              </div>
            )}
            <LoreRow
                  entry={entry}
                  dimmed={cut >= 0 && pos >= cut}
                  author={resolveAuthor(entry.authorId)}
                  avatar={authorAvatar(entry.authorId)}
                  scope={resolveScope(entry)}
                  scopeMenu={
                    canSetScope && entry.scopeOptions.length > 0 ? resolveScopeMenu(entry) : null
                  }
                  highlighted={entry.id === highlightId}
                  entryCaps={entryCaps}
                  onOpenChat={openChat}
                  onOpenManual={openManual}
                  onSetState={setEntryState}
                  onSetScope={setEntryScope}
                  onSetLoreType={setEntryLoreType}
                  onSave={editEntry}
                  onRestored={restoredEntry}
                />
          </div>
        ))}
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

  // 撰寫人 → the office, with this entry's id seeded into the composer: the
  // author may hold dozens of entries, and a question without the id has no
  // subject. (`composeTaskNo` carries a 傳承 id here; the prefix tells them apart.)
  function openChat(peerId: string, entryId: string) {
    navigateHash({ page: "office", chatId: peerId, composeTaskNo: entryId });
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
    const groups: { kind: LoreScopeKind; label: string; defs: SectionDef[] }[] = [
      { kind: "everyone", label: t.lore.kindEveryone, defs: [] },
      { kind: "agent", label: t.lore.kindMember, defs: [] },
      { kind: "manual", label: t.lore.kindManual, defs: [] },
    ];
    for (const d of defs) groups.find((g) => g.kind === d.kind)?.defs.push(d);
    return (
      <div className="lore" ref={scrollRef} data-testid="lore-page">
        {errors}
        <div className="lore__dir-search">
          <IdFilterInput
            value={dirSearch}
            onChange={setDirSearch}
            onCommit={(v) => void jumpToEntry(v)}
            label={t.lore.directoryIdLabel}
            testId="lore-directory-id"
            widthCh={7}
          />
          {dirNote && (
            <span className="lore__dir-note" role="status" data-testid="lore-directory-note">
              {dirNote}
            </span>
          )}
        </div>
        {groups.map((g) =>
          g.defs.length === 0 ? null : (
            <div className="lore__group" key={g.kind} data-testid={`lore-group-${g.kind}`}>
              <div className="lore__group-title">
                {g.label}
                <span className="lore__group-count"> · {g.defs.length}</span>
              </div>
              {g.defs.map((def) => (
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
          )
        )}
      </div>
    );
  }

  const sd = currentDef ? data[currentDef.key] : undefined;
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
          <div className="lore-dir-card lore-dir-card--page" data-testid="lore-section-head">
            <div className="lore-dir-card__head">
              {dirLead(currentDef)}
              <span className="lore-dir-card__main">
                {currentDef.kind === "manual" ? (
                  // The way back to the manual for every viewer; the card scope tag offers it only to those who may move entries.
                  <button
                    type="button"
                    className="lore-dir-card__name lore-dir-card__name--link"
                    data-testid="lore-section-name"
                    title={msg.loreOpenManual(sectionName(currentDef))}
                    aria-label={msg.loreOpenManual(sectionName(currentDef))}
                    onClick={() => openManual(currentDef.scopeKey)}
                  >
                    {sectionName(currentDef)}
                  </button>
                ) : (
                  <span className="lore-dir-card__name" data-testid="lore-section-name">
                    {sectionName(currentDef)}
                  </span>
                )}
                {dirBadges(currentDef)}
              </span>
            </div>
            {renderSummary(currentDef, sd)}
          </div>

          <FilterPanel
            testId="lore-filter"
            clearLabel={t.lore.clearFilters}
            onClear={narrowing || !statesAtDefault ? clearFilters : undefined}
          >
            <IdFilterInput
              value={searchDraft}
              onChange={setSearchDraft}
              onCommit={(v) => setAppliedSearch(v.trim())}
              label={t.lore.idSearchLabel}
              testId="lore-filter-id"
              widthCh={7}
            />
            <MultiSelectFilter
              noun={t.lore.filterTypeNoun}
              allLabel={t.lore.filterTypeAll}
              testId="lore-filter-type"
              options={LORE_TYPES.map((lt) => ({ value: lt, label: loreTypeLabel(t, lt) }))}
              selected={loreTypes}
              onChange={(next) => setLoreTypes(next as Set<LoreType>)}
            />
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

      {toast && (
        <div className="lore__dock">
          <div className="lore-toast" role="status" data-testid="lore-toast">
            <span>{msg.loreScopeChanged(toast.id, toast.scopeLabel)}</span>
          </div>
        </div>
      )}
    </div>
  );
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

/** ONE 傳承 row. The body always shows in full: no fold, no chevron. */
function LoreRow({
  entry,
  dimmed,
  author,
  avatar,
  scope,
  scopeMenu,
  onOpenChat,
  onOpenManual,
  onSetState,
  onSetScope,
  onSetLoreType,
  highlighted,
  entryCaps,
  onSave,
  onRestored,
}: {
  entry: LoreEntryView;
  dimmed: boolean;
  author: AuthorIdentity;
  avatar: { src?: string; kind: AvatarKind } | null;
  /** 屬於, already resolved (resolveScope). `manualKey` non-empty offers the
   * manual jump inside the 適用範圍 menu. */
  scope: { kind: "" | LoreScopeKind; label: string; manualKey: string };
  onOpenChat: (peerId: string, entryId: string) => void;
  onOpenManual: (typeKey: string) => void;
  onSetState: (id: string, next: LoreEntryState) => void;
  /** null ⇒ the viewer may not switch this entry's scope; the scope tag is plain text. */
  scopeMenu: ScopeMenu | null;
  onSetScope: (id: string, kind: LoreScopeKind, label: string) => void;
  onSetLoreType: (id: string, loreType: LoreType) => void;
  highlighted: boolean;
  entryCaps: { title: number; body: number };
  /** Rejects with the reason to show; the draft stays. */
  onSave: (id: string, patch: LoreEntryEdit) => Promise<void>;
  onRestored: (id: string) => Promise<void>;
}) {
  const { t, msg } = useI18n();
  const [stateOpen, setStateOpen] = useState(false);
  const statusRef = useRef<HTMLDivElement>(null);
  const [typeOpen, setTypeOpen] = useState(false);
  const typeRef = useRef<HTMLDivElement>(null);
  const [scopeOpen, setScopeOpen] = useState(false);
  const scopeRef = useRef<HTMLSpanElement>(null);
  const [editing, setEditing] = useState(false);

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

  const stateLabel =
    entry.state === "pinned"
      ? t.lore.statePinned
      : entry.state === "active"
        ? t.lore.stateActive
        : t.lore.stateRetired;

  function actionLabel(s: LoreEntryState): string {
    return s === "retired"
      ? t.lore.actionRetire
      : s === "active"
        ? t.lore.actionActivate
        : t.lore.actionPin;
  }

  // 條目編號 chip — the 任務卡's 編號 chip, same behaviour (owner: 「傳承的 id
  // 沒有顯示出來 像 task 那樣」, c-c933a2b41c54). Click copies the id, and the
  // 已複製 flag is set ONLY when the clipboard write actually succeeded:
  // copyText returns false on a denied clipboard and a chip that lied would be
  // worse than one that did nothing. The timer is cleared on unmount so a row
  // that scrolls away mid-feedback cannot setState on a dead component.
  const [copied, setCopied] = useState(false);
  const copiedTimer = useRef<number | null>(null);
  useEffect(
    () => () => {
      if (copiedTimer.current != null) window.clearTimeout(copiedTimer.current);
    },
    []
  );
  async function copyEntryId() {
    const ok = await copyText(entry.id);
    if (!ok) return;
    setCopied(true);
    if (copiedTimer.current != null) window.clearTimeout(copiedTimer.current);
    copiedTimer.current = window.setTimeout(() => setCopied(false), 1600);
  }

  return (
    <article
      className={`lore-row lore-row--${entry.state}${
        dimmed ? " lore-row--dimmed" : ""
      }${highlighted ? " lore-row--highlight" : ""}`}
      data-testid="lore-row"
      data-entry-id={entry.id}
      data-dimmed={dimmed ? "true" : "false"}
      // Focusable from code only: a highlighted entry lands here.
      tabIndex={-1}
    >
      <div className="lore-row__head">
        <div className="lore-row__badges">
          {/* Order: 編號 → 類型 → 狀態 → 適用範圍. The id leads, as on 任務卡,
              because it is what you quote to somebody else (owner: 「make them
              consistent with task」). 傳承 has no 優先權; do not invent one for the slot. */}
          <button
            type="button"
            className="lore-row__id-badge"
            data-testid="lore-entry-id"
            aria-label={msg.loreCopyEntryId(entry.id)}
            title={msg.loreCopyEntryId(entry.id)}
            onClick={copyEntryId}
          >
            {/* 🔴 THE SAME BADGE AS 任務卡's, DOWN TO THE `#` AND THE ICON. The two
                are the same thing — 「這一筆的編號」, click to copy — and they were
                drawn two different ways: the task carried a glyph and a `#`, the
                entry carried neither. The owner spotted it by putting the two
                screenshots side by side, which is how a difference like this gets
                found: never by reading one page against its own spec.
                The icon differs (a book, not a checklist) because it names WHICH
                register the number belongs to — the same icon the 傳承 tab uses.
                What is shared is the SHAPE; what identifies is the glyph. */}
            {copied ? <CheckIcon size={13} /> : <BookIcon size={13} />}#
            {entry.id}
            {copied && (
              <span
                className="lore-row__id-badge-copied"
                role="status"
                data-testid="lore-entry-id-copied"
              >
                {t.lore.entryIdCopied}
              </span>
            )}
          </button>

          <div className="lore-row__status" ref={typeRef}>
            <button
              type="button"
              className="lore-badge lore-badge--lore-type lore-row__status-chip"
              title={t.lore.typeMenuLabel}
              aria-label={t.lore.typeMenuLabel}
              aria-haspopup="menu"
              aria-expanded={typeOpen}
              data-testid="lore-type"
              data-lore-type={entry.loreType}
              onClick={() => setTypeOpen((o) => !o)}
            >
              {loreTypeLabel(t, entry.loreType)}
            </button>
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

          {/* ONE status badge, and it is also the control (spec §6). Both this
              menu and the type menu hang from their badge's LEFT edge
              (.lore-row__status-pop pins left:0): the badges sit at the row's
              left, and a right-anchored pop would open off the row. */}
          <div className="lore-row__status" ref={statusRef}>
            <button
              type="button"
              className={`lore-badge lore-badge--state-${entry.state} lore-row__status-chip`}
              title={t.lore.stateMenuLabel}
              aria-label={t.lore.stateMenuLabel}
              aria-haspopup="menu"
              aria-expanded={stateOpen}
              data-testid="lore-state"
              onClick={() => setStateOpen((o) => !o)}
            >
              {stateLabel}
            </button>
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

          {/* 適用範圍 rides the tag row as the fourth tag. A viewer who may move
              the entry gets the same badge-is-the-control pattern as 類型/狀態;
              anyone else reads it as plain text. The menu hangs from
              .lore-row__head (this wrapper is not positioned) so a tag that
              wrapped to the row's right edge cannot push the menu off a phone. */}
          {scopeMenu ? (
            <span className="lore-row__scope" data-testid="lore-scope" ref={scopeRef}>
              <button
                type="button"
                className="lore-badge lore-badge--scope lore-row__status-chip"
                data-testid="lore-scope-name"
                data-scope-kind={scope.kind}
                title={t.lore.scopeMenuLabel}
                aria-label={`${t.lore.scopeMenuLabel}：${scope.label}`}
                aria-haspopup="menu"
                aria-expanded={scopeOpen}
                onClick={() => setScopeOpen((o) => !o)}
              >
                <span className="lore-badge__text">{scope.label}</span>
              </button>
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
        </div>

        <div className="lore-row__actions">
          <button
            type="button"
            className="doc-btn doc-btn--edit"
            data-testid="lore-edit"
            disabled={editing}
            onClick={() => setEditing(true)}
          >
            <PencilIcon size={14} />
            <span>{t.lore.actionEdit}</span>
          </button>
        </div>
      </div>

      {editing ? (
        <LoreEntryEditor
          entry={entry}
          caps={entryCaps}
          onRestored={() => onRestored(entry.id)}
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
              carrying `note_size_chars` is unaffected.

              The body is always shown in full (owner: no fold on the card). */}
          <div className="lore-row__body" data-testid="lore-body">
            <Markdown source={entry.body} className="lore-row__body-md doc-md" breaks />
          </div>
        </>
      )}

      <div className="lore-row__meta">
        <LoreAuthorChip
          author={author}
          avatar={avatar}
          onOpenChat={(peerId) => onOpenChat(peerId, entry.id)}
        />
        <span className="lore-row__meta-dot" aria-hidden="true">·</span>
        <span
          className="lore-row__effective-ts"
          data-testid="lore-effective"
          title={t.lore.effectiveLabel}
        >
          {formatAbsolute(entry.effectiveTs, Date.now() / 1000)}
        </span>
        <span className="lore-row__meta-dot" aria-hidden="true">·</span>
        <span className="lore-row__size" data-testid="lore-size">
          {msg.loreChars(entryChars(entry.title, entry.body))}
        </span>
      </div>
      {/* No reason, no line: an empty one would teach that retirements come unexplained. */}
      {entry.retireReason !== "" && (
        <div className="lore-row__retire" data-testid="lore-retire-reason">
          {t.lore.retireReasonLabel}：{entry.retireReason}
        </div>
      )}
    </article>
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
  /** Re-reads the card after a 版本紀錄 restore has landed. */
  onRestored: () => Promise<void>;
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
          onRestored={async () => {
            // The restore already landed, so the draft is stale whatever the
            // re-read does: leave edit mode either way, as DocCard does.
            try {
              await onRestored();
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

/** The 撰寫人 pill.
 *
 * 🔴 THE TWO ARMS ARE DIFFERENT ELEMENTS, not one element with a hidden icon.
 * A resolvable author gets a `<button>` carrying avatar + name + 傳訊息 icon; an
 * author who is no longer on the roster gets a plain `<span>` with the same
 * pill look and NO icon. Hiding only the glyph would leave a clickable pill
 * that opens a chat with nobody — the affordance, not just its decoration, is
 * what has to go. (Same construction as TaskCard's creator row.) */
function LoreAuthorChip({
  author,
  avatar,
  onOpenChat,
}: {
  author: AuthorIdentity;
  avatar: { src?: string; kind: AvatarKind } | null;
  onOpenChat: (peerId: string) => void;
}): ReactNode {
  const { t } = useI18n();
  if (author.peerId === "") {
    return (
      <span className="lore-row__chip" data-testid="lore-author-row">
        <span className="lore-row__author" data-testid="lore-author">
          {author.text}
        </span>
      </span>
    );
  }
  return (
    <button
      type="button"
      className="lore-row__chip lore-row__chip--link"
      title={t.lore.messageAuthor}
      aria-label={t.lore.messageAuthor}
      data-testid="lore-author-link"
      onClick={() => onOpenChat(author.peerId)}
    >
      {avatar ? <Avatar size={18} kind={avatar.kind} src={avatar.src} /> : null}
      <span className="lore-row__author" data-testid="lore-author">
        {author.text}
      </span>
      <ChatBubbleIcon size={13} />
    </button>
  );
}
