// LorePage — the 傳承 tab: one section per boot-document target (所有人, each
// member, each task manual), collapsed by default, so the landing view is the
// budget overview.
//
// 🔴 EVERY BUDGET NUMBER IS THE SERVER'S. The section headers read
// `listLoreBudgets`, an open section's sentence and 上限線 read the
// `capChars`/`usedChars`/`firstDroppedId`/`budgetSegments` its own list page
// answered. Adding up title+body lengths here would be a second copy of the
// server's picking walk, wrong on every page after the first and wrong for a
// member whose 所有人 entries come first; a line in the wrong place looks
// exactly like one in the right place.
//
// 🔴 THE FILTER TRAVELS WITH THE PAGE REQUEST, never as a `.filter()` over
// downloaded rows. With a narrowing filter every section asks its own
// filtered first page; that page answers both "does this section match" and
// "what to show", correctly under paging. One unscoped request grouped by scope
// would only see the matches that happen to land on its first page.
//
// 🔴 A MUTATION NEVER RELOADS A SECTION FROM PAGE ONE (owner: reloading made
// him lose his place mid-batch). The changed rows are re-read by id and
// replaced where they stand, only the numbers are re-asked, and rows re-sort
// only when a section is reopened or the filter changes.

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
import type { LoreEditEffect } from "../i18n/compose";
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
import { useIsMobile } from "../hooks/useIsMobile";
import { useSuggestedRepliesLoreMessage } from "../hooks/useSuggestedReplies";
import { useTaskManuals } from "../hooks/useTaskManuals";
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
import { copyText } from "../lib/clipboard";
import { formatAbsolute } from "../lib/dateFormat";
import { autosizeTextarea } from "../lib/autosize";
import { enterShouldSend } from "../lib/composerKeys";
import { navigateHash } from "../lib/hashRoute";
import { Avatar } from "./Avatar";
import { ComposerAttachmentPreview } from "./ComposerAttachmentPreview";
import { DocumentHistoryEntry } from "./DocumentHistoryEntry";
import { FilterPanel } from "./FilterPanel";
import { IdFilterInput } from "./IdFilterInput";
import { Markdown } from "./Markdown";
import { MultiSelectFilter } from "./MultiSelectFilter";
import { SuggestedReplies, appendSuggestion } from "./SuggestedReplies";
import {
  BookIcon,
  ChatBubbleIcon,
  CheckIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  GearIcon,
  MoreIcon,
  PaperclipIcon,
  SendIcon,
  UserIcon,
  UsersIcon,
} from "./icons";
import "./lore.css";
// ComposerAttachmentPreview draws `.chat__preview-*` from office.css; the body
// renders through <Markdown>'s `.doc-md` skin from settings.css. Imported here
// rather than relied on transitively (css-layout-traps.md).
import "./office.css";
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

/** `212` and `l-212` both mean L-212; anything else is sent as typed. */
function normalizeEntryId(raw: string): string {
  const v = raw.trim();
  if (/^\d+$/.test(v)) return `L-${v}`;
  if (/^l-/i.test(v)) return `L-${v.slice(2)}`;
  return v;
}

/** Where a changed card goes once its fresh row arrives. 🔴 Owner has not
 * decided yet whether a bumped or re-stated card should move; until then it
 * stays where it stands. Moving it (with the viewport anchored and a 前往/復原
 * toast) is a change to this function and its one caller, nothing else. */
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

export function LorePage({
  canSetScope: canSetScopeProp,
}: {
  canSetScope?: boolean;
} = {}) {
  const { t, msg } = useI18n();
  const { members, loading: membersLoading } = useMembers();
  const canSetScope = canSetScopeProp ?? viewerMaySetLoreScope(members);
  const { workers } = useOutsourceWorkers();
  const { manuals } = useTaskManuals();
  const isMobile = useIsMobile();

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

  // ── 篩選 ────────────────────────────────────────────────────────────────
  const [idDraft, setIdDraft] = useState("");
  const [appliedId, setAppliedId] = useState("");
  const [states, setStates] = useState<Set<LoreEntryState>>(new Set(DEFAULT_STATES));
  const [authors, setAuthors] = useState<Set<string>>(new Set());
  const [loreTypes, setLoreTypes] = useState<Set<LoreType>>(new Set());

  const baseOpts = useMemo<LoreListOptions>(() => {
    const next: LoreListOptions = {};
    if (states.size > 0) next.states = [...states];
    if (authors.size > 0) next.authorIds = [...authors];
    if (loreTypes.size > 0) next.loreTypes = [...loreTypes];
    if (appliedId !== "") next.entryIds = [appliedId];
    return next;
  }, [states, authors, loreTypes, appliedId]);
  const filterKey = JSON.stringify(baseOpts);

  // Ticking 已失效 only ADDS rows to a section, so it does not count as a
  // search; dropping 置頂 or 生效中 narrows like any other filter.
  const statesNarrow =
    states.size > 0 && DEFAULT_STATES.some((s) => !states.has(s));
  const narrowing =
    appliedId !== "" || authors.size > 0 || loreTypes.size > 0 || statesNarrow;
  const statesAtDefault =
    states.size === DEFAULT_STATES.length && DEFAULT_STATES.every((s) => states.has(s));

  function clearFilters() {
    setIdDraft("");
    setAppliedId("");
    setStates(new Set(DEFAULT_STATES));
    setAuthors(new Set());
    setLoreTypes(new Set());
  }

  // ── sections ───────────────────────────────────────────────────────────
  const [budgets, setBudgets] = useState<LoreBudgetTargetView[] | null>(null);
  const [budgetsError, setBudgetsError] = useState(false);
  const [open, setOpen] = useState<Set<string>>(new Set());
  const [data, setData] = useState<Record<string, SectionData>>({});
  // Sections whose filtered first page came back non-empty; null while there
  // is no narrowing filter.
  const [matches, setMatches] = useState<Set<string> | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

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
        },
      }));
      return api
        .listLoreEntries({ ...baseOptsRef.current, ...scopeOpts(def), limit: PAGE, offset: 0 })
        .then((page) => {
          if (genRef.current.get(def.key) !== gen) return -1;
          patchSection(def.key, (p) => ({
            entries: page.entries,
            cap: isLatestCap(def.key, capSeq) ? capOf(page) : p.cap,
            loading: false,
            fetchingMore: false,
            error: false,
            exhausted: page.entries.length < PAGE,
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
    (def: SectionDef) => {
      const held = dataRef.current[def.key];
      if (!held || held.exhausted || held.error || flightRef.current.has(def.key)) return;
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
          patchSection(def.key, (p) => {
            // An entry that changed between two reads can slide across the page
            // boundary and arrive twice; the first copy keeps its place.
            const ids = new Set(p.entries.map((e) => e.id));
            return {
              ...p,
              entries: p.entries.concat(page.entries.filter((e) => !ids.has(e.id))),
              cap: isLatestCap(def.key, capSeq) ? capOf(page) : p.cap,
              exhausted: page.entries.length < PAGE,
            };
          });
        })
        .catch((e) => {
          // The rows already shown stay; the next scroll retries.
          console.warn("LorePage: page load failed", e);
        })
        .finally(() => {
          if (genRef.current.get(def.key) !== gen) return;
          patchSection(def.key, (p) => ({ ...p, fetchingMore: false }));
          flightRef.current.delete(def.key);
        });
    },
    [patchSection]
  );

  const budgetsReady = budgets !== null;
  const narrowingRef = useRef(narrowing);
  narrowingRef.current = narrowing;
  // A narrowing run's answers settle matches/open only while it is the
  // newest run; a cleared or replaced filter must not reopen sections.
  const narrowRunRef = useRef(0);
  // A filter change re-asks: with a narrowing filter every section asks its
  // filtered first page and the matching ones open; otherwise the sections that
  // are open are re-asked under the new filter and the rest stay collapsed.
  useEffect(() => {
    if (!budgetsReady) return;
    const run = ++narrowRunRef.current;
    const all = defsRef.current;
    if (narrowingRef.current) {
      setMatches(null);
      applyOpen(new Set());
      setData({});
      const found = new Set<string>();
      let pending = all.length;
      if (pending === 0) setMatches(found);
      for (const def of all) {
        void loadSection(def).then((n) => {
          if (run !== narrowRunRef.current) return;
          if (n > 0) found.add(def.key);
          pending -= 1;
          if (pending === 0) {
            setMatches(new Set(found));
            applyOpen(new Set(found));
          }
        });
      }
      return;
    }
    setMatches(null);
    setData({});
    for (const def of all) {
      if (openRef.current.has(def.key)) void loadSection(def);
    }
    // defs and the section maps are read through refs: a budgets refresh after
    // a mutation must not re-ask every open section from page one.
  }, [filterKey, budgetsReady, loadSection]);

  function toggleSection(def: SectionDef) {
    if (openRef.current.has(def.key)) {
      const next = new Set(openRef.current);
      next.delete(def.key);
      applyOpen(next);
      bumpGen(def.key);
      flightRef.current.delete(def.key);
      setData((prev) => {
        const next = { ...prev };
        delete next[def.key];
        return next;
      });
      return;
    }
    applyOpen(new Set(openRef.current).add(def.key));
    void loadSection(def);
  }

  function collapseAll() {
    for (const key of openRef.current) {
      bumpGen(key);
      flightRef.current.delete(key);
    }
    applyOpen(new Set());
    setData({});
  }

  // ── 捲到底: each open section pages on its own, when its bottom nears the
  // scrollport's bottom. jsdom has no geometry, so there every open section
  // that has more is asked.
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
  const bumpEntry = useCallback(
    (id: string) => void runOne(id, () => api.bumpLoreEntry(id)).catch(() => {}),
    [runOne]
  );
  const setEntryScope = useCallback(
    (id: string, kind: LoreScopeKind) =>
      void runOne(id, () => api.setLoreEntryScope(id, kind), true).catch(() => {}),
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

  // ── 多選 + 批次 ────────────────────────────────────────────────────────
  const [selected, setSelected] = useState<string[]>([]);
  const [batchRunning, setBatchRunning] = useState(false);
  const [retireOpen, setRetireOpen] = useState(false);
  const [retireReason, setRetireReason] = useState("");
  const [batchResult, setBatchResult] = useState<{ text: string; failed: boolean } | null>(null);

  const visibleDefs = useMemo(
    () => (matches ? defs.filter((d) => matches.has(d.key)) : narrowing ? [] : defs),
    [matches, defs, narrowing]
  );
  // A selection only ever names cards on screen: a batch must not reach an
  // entry the reader can no longer see, and could not refresh it if it did.
  useEffect(() => {
    const shown = new Set(
      visibleDefs
        .filter((d) => open.has(d.key))
        .flatMap((d) => (data[d.key]?.entries ?? []).map((e) => e.id))
    );
    setSelected((prev) => {
      const kept = prev.filter((id) => shown.has(id));
      return kept.length === prev.length ? prev : kept;
    });
  }, [visibleDefs, open, data]);

  const toggleSelect = useCallback((id: string) => {
    setBatchResult(null);
    setSelected((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]));
  }, []);

  // One call per entry, in order; the numbers are re-asked once at the end.
  async function runBatch(state: LoreEntryState, reason?: string) {
    setBatchRunning(true);
    setActionError(null);
    const done: string[] = [];
    const failed: { id: string; reason: string }[] = [];
    const changed = new Map<string, string[]>();
    let everyoneTouched = false;
    for (const id of selected) {
      try {
        await api.setLoreEntryState(id, state, reason);
        done.push(id);
        const key = sectionOfEntry(id);
        if (key) {
          changed.set(key, [...(changed.get(key) ?? []), id]);
          if (key.startsWith("everyone:")) everyoneTouched = true;
        }
      } catch (e) {
        failed.push({ id, reason: failText(e) });
      }
    }
    await refreshInPlace(changed, everyoneTouched);
    setSelected(failed.map((f) => f.id));
    setRetireOpen(false);
    setRetireReason("");
    setBatchRunning(false);
    setBatchResult({ text: msg.loreBatchResult(done, failed), failed: failed.length > 0 });
  }

  // ── render helpers ──────────────────────────────────────────────────────
  function editDescriber(sd: SectionData) {
    const cut = cutIndexOf(sd);
    const { capChars, usedChars, firstDroppedId, firstDroppedChars } = sd.cap;
    return (entry: LoreEntryView, oldChars: number, newChars: number): string => {
      let effect: LoreEditEffect = { mode: "size" };
      const pos = sd.entries.findIndex((e) => e.id === entry.id);
      // Above the line when it sits before the line on screen, or — with no
      // narrowing filter, so the rows are a prefix of the server's order —
      // when the line has not been loaded yet.
      const above =
        firstDroppedId === ""
          ? true
          : cut >= 0
            ? pos >= 0 && pos < cut
            : !narrowing && !sd.exhausted;
      if (capChars > 0 && entry.state !== "retired") {
        if (entry.id === firstDroppedId) {
          const room = capChars - usedChars;
          effect = { mode: "self", remain: room, readable: newChars <= room };
        } else if (above) {
          const remain = capChars - usedChars + oldChars - newChars;
          effect =
            remain < 0
              ? { mode: "over", over: -remain }
              : {
                  mode: "remain",
                  remain,
                  firstDropped:
                    firstDroppedId === ""
                      ? null
                      : { id: firstDroppedId, chars: firstDroppedChars, readable: remain >= firstDroppedChars },
                };
        }
      }
      return msg.loreEditEffect(oldChars, newChars, effect);
    };
  }

  function renderHeaderUsage(def: SectionDef) {
    const b = def.target;
    const usage =
      def.kind === "everyone"
        ? msg.loreEveryoneUsed(b.usedChars)
        : `${b.usedChars.toLocaleString("en-US")} / ${b.capChars.toLocaleString("en-US")}`;
    return (
      <span className="lore-section__usage" data-testid="lore-section-usage">
        <span className="lore-section__chars">{usage}</span>
        {b.droppedCount > 0 ? (
          <span className="lore-section__dropped" data-testid="lore-section-dropped">
            {msg.loreDropped(b.droppedCount)}
          </span>
        ) : (
          <span className="lore-section__fit">{t.lore.allFit}</span>
        )}
      </span>
    );
  }

  function renderSummary(def: SectionDef, sd: SectionData) {
    const cap = sd.cap;
    if (cap.capChars <= 0) return null;
    const own =
      [...cap.budgetSegments].reverse().find((s) => s.scopeKind === def.kind) ?? null;
    const everyoneSeg =
      def.kind === "agent"
        ? cap.budgetSegments.find((s) => s.scopeKind === "everyone") ?? null
        : null;
    const cut = cutIndexOf(sd);
    return (
      <>
        <div className="lore-section__summary" data-testid="lore-section-summary">
          {msg.loreBudgetSentence({
            capChars: cap.capChars,
            usedChars: cap.usedChars,
            ownName: sectionName(def),
            everyone: everyoneSeg
              ? {
                  count: everyoneSeg.entryCount,
                  chars: everyoneSeg.chars,
                  fitted: everyoneSeg.fittedCount,
                  fittedChars: everyoneSeg.fittedChars,
                }
              : null,
            own: {
              count: own?.entryCount ?? 0,
              fitted: own?.fittedCount ?? 0,
              fittedChars: own?.fittedChars ?? 0,
            },
            firstDroppedId: cap.firstDroppedId,
            firstDroppedChars: cap.firstDroppedChars,
          })}
          {cap.firstDroppedId !== "" && cut < 0 && !sd.loading && (
            <div className="lore-section__cap-away" data-testid="lore-cap-away">
              {msg.loreCapLineAway(cap.firstDroppedId)}
            </div>
          )}
        </div>
        {everyoneSeg && (
          <div className="lore-section__everyone-ref" data-testid="lore-everyone-ref">
            <span>{msg.loreEveryoneRef(everyoneSeg.entryCount)}</span>
            <span className="lore-section__everyone-ref-chars">
              {msg.loreChars(everyoneSeg.chars)}
            </span>
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
    const describe = editDescriber(sd);
    return (
      <>
        {renderSummary(def, sd)}
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
              isMobile={isMobile}
              selected={selected.includes(entry.id)}
              entryCaps={entryCaps}
              describeEdit={(oldChars, newChars) => describe(entry, oldChars, newChars)}
              onToggleSelect={toggleSelect}
              onOpenChat={openChat}
              onOpenManual={openManual}
              onSetState={setEntryState}
              onSetScope={setEntryScope}
              onBump={bumpEntry}
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

  // 撰寫人 offers the live roster only; a machine-layer member can never write
  // 傳承 (the route refuses it), so `kind === "staff"` is an allow-list.
  const authorOptions = [
    ...members
      .filter((m) => m.kind === "staff")
      .map((m) => ({ value: m.id, label: m.name })),
    ...workers.map((w) => ({
      value: w.id,
      label: w.codename ? msg.outsourceLabel(w.codename) : w.id,
    })),
  ];

  const groups: { kind: LoreScopeKind; label: string; defs: SectionDef[] }[] = [
    { kind: "everyone", label: t.lore.kindEveryone, defs: [] },
    { kind: "agent", label: t.lore.kindMember, defs: [] },
    { kind: "manual", label: t.lore.kindManual, defs: [] },
  ];
  for (const d of visibleDefs) groups.find((g) => g.kind === d.kind)?.defs.push(d);
  const kindChip = (k: LoreScopeKind) =>
    k === "everyone" ? t.lore.kindEveryone : k === "manual" ? t.lore.kindManual : t.lore.kindMember;

  return (
    <div className="lore" ref={scrollRef} data-testid="lore-page">
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

      <FilterPanel
        testId="lore-filter"
        clearLabel={t.lore.clearFilters}
        onClear={narrowing || !statesAtDefault ? clearFilters : undefined}
        actions={
          <button
            type="button"
            className="lore__collapse-all"
            data-testid="lore-collapse-all"
            onClick={collapseAll}
          >
            {t.lore.collapseAll}
          </button>
        }
      >
        <IdFilterInput
          value={idDraft}
          onChange={setIdDraft}
          onCommit={(v) => setAppliedId(normalizeEntryId(v))}
          label={t.lore.idSearchLabel}
          testId="lore-filter-id"
          widthCh={7}
        />
        <MultiSelectFilter
          noun={t.lore.filterAuthorNoun}
          allLabel={t.lore.filterAuthorAll}
          testId="lore-filter-author"
          options={authorOptions}
          selected={authors}
          onChange={setAuthors}
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

      {narrowing && matches && matches.size > 0 && (
        <div className="lore__search-summary" data-testid="lore-search-summary">
          {msg.loreSearchSummary(matches.size)}
        </div>
      )}
      {narrowing && matches && matches.size === 0 && (
        <div className="lore__empty" data-testid="lore-empty">
          {t.lore.emptyFiltered}
        </div>
      )}

      {groups.map((g) =>
        g.defs.length === 0 ? null : (
          <div className="lore__group" key={g.kind} data-testid={`lore-group-${g.kind}`}>
            <div className="lore__group-title">{g.label}</div>
            {g.defs.map((def) => {
              const isOpen = open.has(def.key);
              return (
                <section
                  key={def.key}
                  className={`lore-section${isOpen ? " lore-section--open" : ""}`}
                  data-testid="lore-section"
                  data-section={def.key}
                >
                  <button
                    type="button"
                    className="lore-section__head"
                    data-testid="lore-section-toggle"
                    aria-expanded={isOpen}
                    onClick={() => toggleSection(def)}
                  >
                    <span className="lore-section__mark" aria-hidden="true">
                      {isOpen ? <ChevronDownIcon size={14} /> : <ChevronRightIcon size={14} />}
                    </span>
                    <span className="lore-section__name" data-testid="lore-section-name">
                      {sectionName(def)}
                    </span>
                    <span className="lore-section__kind">{kindChip(def.kind)}</span>
                    <span className="lore-section__count" data-testid="lore-section-count">
                      {msg.loreEntryCount(def.target.entryCount)}
                    </span>
                    {renderHeaderUsage(def)}
                  </button>
                  {isOpen && (
                    <div className="lore-section__body" data-testid="lore-section-body">
                      {renderSectionBody(def, data[def.key])}
                    </div>
                  )}
                </section>
              );
            })}
          </div>
        )
      )}

      {(selected.length > 0 || batchResult) && (
        <div className="lore-batch" data-testid="lore-batch-bar" role="region">
          {selected.length > 0 && !retireOpen && (
            <>
              <span className="lore-batch__count" data-testid="lore-batch-count">
                {msg.loreBatchSelected(selected.length)}
              </span>
              {canSetScope && (
                <button
                  type="button"
                  className="lore-batch__btn"
                  data-testid="lore-batch-pin"
                  disabled={batchRunning}
                  onClick={() => void runBatch("pinned")}
                >
                  {t.lore.batchPin}
                </button>
              )}
              <button
                type="button"
                className="lore-batch__btn"
                data-testid="lore-batch-activate"
                disabled={batchRunning}
                onClick={() => void runBatch("active")}
              >
                {t.lore.batchActivate}
              </button>
              <button
                type="button"
                className="lore-batch__btn lore-batch__btn--danger"
                data-testid="lore-batch-retire"
                disabled={batchRunning}
                onClick={() => {
                  setBatchResult(null);
                  setRetireOpen(true);
                }}
              >
                {t.lore.batchRetire}
              </button>
              <button
                type="button"
                className="lore-batch__link"
                data-testid="lore-batch-clear"
                disabled={batchRunning}
                onClick={() => {
                  setSelected([]);
                  setBatchResult(null);
                }}
              >
                {t.lore.batchClear}
              </button>
              {batchRunning && <span className="lore-batch__working">{t.lore.batchWorking}</span>}
            </>
          )}
          {selected.length > 0 && retireOpen && (
            <>
              <input
                type="text"
                className="lore-batch__reason"
                data-testid="lore-batch-reason"
                aria-label={t.lore.batchRetireReason}
                placeholder={t.lore.batchRetireReason}
                value={retireReason}
                disabled={batchRunning}
                onChange={(e) => setRetireReason(e.target.value)}
              />
              <button
                type="button"
                className="lore-batch__btn lore-batch__btn--danger"
                data-testid="lore-batch-retire-confirm"
                disabled={batchRunning}
                onClick={() => void runBatch("retired", retireReason.trim())}
              >
                {t.lore.batchRetireConfirm}
              </button>
              <button
                type="button"
                className="lore-batch__link"
                data-testid="lore-batch-retire-back"
                disabled={batchRunning}
                onClick={() => setRetireOpen(false)}
              >
                {t.lore.batchBack}
              </button>
            </>
          )}
          {batchResult && (
            <div
              className={`lore-batch__result${batchResult.failed ? " lore-batch__result--failed" : ""}`}
              role="status"
              data-testid="lore-batch-result"
            >
              <span>{batchResult.text}</span>
              <button
                type="button"
                className="lore-batch__link"
                data-testid="lore-batch-dismiss"
                onClick={() => setBatchResult(null)}
              >
                {t.lore.batchDismiss}
              </button>
            </div>
          )}
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

/** ONE 傳承 row. Collapsed by default; the whole row is the toggle surface. */
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
  onBump,
  isMobile,
  selected,
  entryCaps,
  describeEdit,
  onToggleSelect,
  onSave,
  onRestored,
}: {
  entry: LoreEntryView;
  dimmed: boolean;
  author: AuthorIdentity;
  avatar: { src?: string; kind: AvatarKind } | null;
  /** 屬於, already resolved (resolveScope). `manualKey` non-empty is what
   * offers the manual jump inside the 適用範圍 menu; `kind` picks the glyph and,
   * without a menu, whether the pill is the manual button. Both come from that
   * one place. */
  scope: { kind: "" | LoreScopeKind; label: string; manualKey: string };
  onOpenChat: (peerId: string, entryId: string) => void;
  onOpenManual: (typeKey: string) => void;
  onSetState: (id: string, next: LoreEntryState) => void;
  /** null ⇒ the viewer may not switch this entry's scope; the badge is inert. */
  scopeMenu: ScopeMenu | null;
  onSetScope: (id: string, kind: LoreScopeKind) => void;
  onBump: (id: string) => void;
  /** Phone width: 提到最新 and 編輯 fold into the 「⋯」 menu. */
  isMobile: boolean;
  selected: boolean;
  entryCaps: { title: number; body: number };
  /** The sentence about what saving does to the section's 上限線. */
  describeEdit: (oldChars: number, newChars: number) => string;
  onToggleSelect: (id: string) => void;
  /** Rejects with the reason to show; the draft stays. */
  onSave: (id: string, patch: LoreEntryEdit) => Promise<void>;
  onRestored: (id: string) => Promise<void>;
}) {
  const { t, msg } = useI18n();
  const [expanded, setExpanded] = useState(false);
  const [stateOpen, setStateOpen] = useState(false);
  const statusRef = useRef<HTMLDivElement>(null);
  const [scopeOpen, setScopeOpen] = useState(false);
  const scopeRef = useRef<HTMLSpanElement>(null);
  const [moreOpen, setMoreOpen] = useState(false);
  const moreRef = useRef<HTMLDivElement>(null);
  const [editing, setEditing] = useState(false);

  useEffect(() => {
    if (!moreOpen) return;
    function onDown(e: MouseEvent) {
      if (!moreRef.current?.contains(e.target as Node)) setMoreOpen(false);
    }
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [moreOpen]);

  useEffect(() => {
    if (!scopeOpen) return;
    function onDown(e: MouseEvent) {
      if (!scopeRef.current?.contains(e.target as Node)) setScopeOpen(false);
    }
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [scopeOpen]);

  const scopeText =
    scope.kind === "manual"
      ? msg.loreScopeManual(scope.label)
      : scope.kind === "agent"
        ? msg.loreScopeAgent(scope.label)
        : scope.label;
  const scopeGlyph =
    scope.kind === "manual" ? (
      <GearIcon size={13} className="lore-row__scope-glyph lore-row__scope-glyph--task" />
    ) : scope.kind === "agent" ? (
      <UserIcon size={13} className="lore-row__scope-glyph lore-row__scope-glyph--member" />
    ) : scope.kind === "everyone" ? (
      <UsersIcon size={13} className="lore-row__scope-glyph lore-row__scope-glyph--everyone" />
    ) : null;

  useEffect(() => {
    if (!stateOpen) return;
    function onDown(e: MouseEvent) {
      if (!statusRef.current?.contains(e.target as Node)) setStateOpen(false);
    }
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [stateOpen]);

  // Copied verbatim in SHAPE from TaskCard.onCardToggleClick: a click that
  // landed on an interactive descendant does that control's job and must not
  // also flip the row, and a click that ended a drag-selection is reading. The
  // [role='button'] entry also matches THIS element (the article carries it),
  // so the `hit !== e.currentTarget` guard is what keeps ordinary body clicks
  // toggling instead of being swallowed.
  function onRowToggleClick(e: React.MouseEvent<HTMLElement>) {
    const target = e.target as HTMLElement;
    // `.lore-row__composer` is in this list for the same reason the controls
    // are: it is a WRITING surface that now renders on the COLLAPSED row, so a
    // click that lands on its padding (beside the box, not on it) would fold
    // the entry away mid-sentence. The textarea/buttons inside it are already
    // covered by the element entries; this covers the box AROUND them.
    const hit = target.closest(
      "button, a, textarea, input, select, label, .lore-row__composer, .lore-row__editor, [role='button'], [role='menu'], [role='dialog']"
    );
    if (hit && hit !== e.currentTarget) return;
    const sel = window.getSelection();
    if (sel && sel.rangeCount > 0 && !sel.isCollapsed) return;
    setExpanded((v) => !v);
  }
  function onRowToggleKeyDown(e: React.KeyboardEvent<HTMLElement>) {
    if (e.target !== e.currentTarget) return;
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      setExpanded((v) => !v);
    }
  }

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
      }`}
      data-testid="lore-row"
      data-entry-id={entry.id}
      data-dimmed={dimmed ? "true" : "false"}
      role="button"
      tabIndex={0}
      aria-expanded={expanded}
      aria-label={expanded ? t.lore.collapseCard : t.lore.expandCard}
      onClick={onRowToggleClick}
      onKeyDown={onRowToggleKeyDown}
    >
      <div className="lore-row__head">
        <input
          type="checkbox"
          className="lore-row__select"
          data-testid="lore-select"
          aria-label={t.lore.selectEntry}
          checked={selected}
          onChange={() => onToggleSelect(entry.id)}
        />
        <div className="lore-row__badges">
          {/* The type tag leads the row, ahead of the id, so a collapsed row
              shows it before anything else. */}
          <span
            className="lore-badge lore-badge--lore-type"
            data-testid="lore-type"
            data-lore-type={entry.loreType}
          >
            {loreTypeLabel(t, entry.loreType)}
          </span>
          {/* 🔴 THE BADGE ORDER IS 任務卡'S ORDER (owner, 2026-09-07: 「The order
              of buttons / filters matters」「make them consistent with task」).
              That card reads 編號 → 優先權 → 狀態 → 類型, so this row reads
              編號 → 狀態 → 屬於, behind the type tag the owner put at the very
              front. The id leads the card's order on both because it is what you
              quote to somebody else; it used to sit second here, behind the state,
              and the two pages disagreed about where a reader's eye should land.
              傳承 has no 優先權 — there is nothing to put in that slot, and
              inventing one to fill the gap would be consistency in the shape only. */}
          {/* 條目編號, right of the status chip. It is a <button>, so the row's
              own toggle handler lets it through the same way it lets the status
              chip through — clicking the id copies it and does NOT expand the
              row. */}
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

          {/* ONE status badge, and it is also the control (spec §6). The menu
              hangs from the badge's LEFT edge — .lore-row__status-pop pins
              left:0 — because the badge sits at the row's left end and a
              right-anchored pop would open off the row. */}
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

          {/* 屬於 — which scope pays for this entry.
              🔴 IT IS ON THE COLLAPSED ROW, WHICH IS THE WHOLE POINT. It used to
              sit in the expanded body, and the reason given for putting it on the
              row at all was that 全部 — where the page OPENS — mixes every scope
              together and the reader cannot tell a 角色傳承 from one manual's.
              That reader is looking at a COLLAPSED list. A chip they have to open
              a row to reach answers the question only for somebody who already
              went looking, which is not the person who was lost (owner, card
              rc-11734523eb52; the first attempt landed inside `expanded &&`).
              It is a <button> when it leads somewhere — the 適用範圍 menu, or the
              manual — so the row's closest() filter lets the click through
              instead of toggling. */}
          <span className="lore-row__scope" data-testid="lore-scope" ref={scopeRef}>
            {/* ONE badge naming the current scope (T-236 mockup): 任務：<手冊>
                with the gear, 建立者：<作者> with the person glyph, 所有人 with the
                group glyph; an unknown kind shows its raw key and no glyph.
                For a viewer who may switch scopes the badge is the 適用範圍 menu
                trigger, and the manual jump moves into that menu. Otherwise a
                manual badge still opens the manual and the rest are plain text. */}
            {scopeMenu ? (
              <button
                type="button"
                className="lore-badge lore-badge--type lore-row__scope-chip lore-row__scope-chip--editable"
                data-testid="lore-scope-name"
                data-scope-kind={scope.kind}
                aria-haspopup="menu"
                aria-expanded={scopeOpen}
                title={t.lore.scopeMenuLabel}
                onClick={() => setScopeOpen((o) => !o)}
              >
                {scopeGlyph}
                <span className="lore-row__scope-name">{scopeText}</span>
                <ChevronDownIcon size={12} className="lore-row__scope-caret" />
              </button>
            ) : scope.kind === "manual" ? (
              <button
                type="button"
                className="lore-badge lore-badge--type lore-row__scope-chip"
                data-testid="lore-scope-name"
                data-scope-kind="manual"
                aria-label={msg.loreOpenManual(scope.label)}
                title={msg.loreOpenManual(scope.label)}
                onClick={() => onOpenManual(scope.manualKey)}
              >
                {scopeGlyph}
                <span className="lore-row__scope-name">{scopeText}</span>
              </button>
            ) : (
              <span
                className="lore-badge lore-badge--type"
                data-testid="lore-scope-name"
                data-scope-kind={scope.kind}
              >
                {scopeGlyph}
                <span className="lore-row__scope-name">{scopeText}</span>
              </span>
            )}
            {scopeMenu && scopeOpen && (
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
                        if (!current) onSetScope(entry.id, c.kind);
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
        </div>

        <div className="lore-row__actions">
          {!isMobile && (
            <span className="lore-row__size" data-testid="lore-size">
              {msg.loreChars(entryChars(entry.title, entry.body))}
            </span>
          )}
          {isMobile ? (
            <div className="lore-row__more" ref={moreRef}>
              <button
                type="button"
                className="lore-row__action"
                data-testid="lore-more-actions"
                aria-label={t.lore.moreActions}
                title={t.lore.moreActions}
                aria-haspopup="menu"
                aria-expanded={moreOpen}
                onClick={() => setMoreOpen((o) => !o)}
              >
                <MoreIcon size={16} />
              </button>
              {moreOpen && (
                <div
                  className="lore-row__menu-pop lore-row__more-pop"
                  role="menu"
                  data-testid="lore-more-menu"
                >
                  <button
                    type="button"
                    role="menuitem"
                    className="lore-row__menu-item"
                    data-testid="lore-bump"
                    onClick={() => {
                      setMoreOpen(false);
                      onBump(entry.id);
                    }}
                  >
                    {t.lore.bump}
                  </button>
                  <button
                    type="button"
                    role="menuitem"
                    className="lore-row__menu-item"
                    data-testid="lore-edit"
                    onClick={() => {
                      setMoreOpen(false);
                      setEditing(true);
                    }}
                  >
                    {t.lore.actionEdit}
                  </button>
                </div>
              )}
            </div>
          ) : (
            <>
              <button
                type="button"
                className="lore-row__action"
                data-testid="lore-bump"
                onClick={() => onBump(entry.id)}
              >
                {t.lore.bump}
              </button>
              <button
                type="button"
                className="lore-row__action"
                data-testid="lore-edit"
                disabled={editing}
                onClick={() => setEditing(true)}
              >
                {t.lore.actionEdit}
              </button>
            </>
          )}
        </div>

        {/* A pure STATE INDICATOR: aria-hidden, no role, pointer-events:none in
            CSS, and a DIFFERENT ICON per state rather than one icon rotated. */}
        <span
          className="lore-row__expand-mark"
          data-testid="lore-expand-mark"
          aria-hidden="true"
        >
          {expanded ? <ChevronDownIcon size={18} /> : <ChevronRightIcon size={18} />}
        </span>
      </div>

      {editing ? (
        <LoreEntryEditor
          entry={entry}
          caps={entryCaps}
          describeEdit={describeEdit}
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

          {/* 🔴 內容 IS THE ONE THING 展開／收合 STILL CONTROLS (owner 2026-09-08,
              card rc-2e10ee03b97c, superseding his own ruling 90 seconds earlier:
              「我覺得好像應該唯一要折疊的是 content 然後要支援 md format」). So the
              clamp stays HERE, and everything below — 撰寫人 / 生效期 / 失效理由 —
              moved OUT of `expanded` and now shows on a closed row too. The earlier
              reading (「才五百字好像沒什麼好折疊的」 ⇒ body always full, meta still
              hidden) was the exact opposite arrangement and is dead.

              🔴 THE TRUNCATION IS OF THE RENDERED OUTPUT, NEVER OF THE SOURCE. It
              is CSS (-webkit-line-clamp on .lore-row__body--clamped): the markdown
              is parsed in full and the BOX shows one line of it. Slicing the source
              string to N characters would cut a `**` or a fence in half and hand
              the parser something that is not the entry's markdown at all.

              🔴 MARKDOWN GOES THROUGH THE SHARED, XSS-SAFE RENDERER, exactly as the
              chat bubble does (ChatArea.tsx:2052). Never dangerouslySetInnerHTML:
              a 傳承 body is agent-authored free text.
              `breaks` for the same reason chat turns it on — this text was WRITTEN
              as plain text, where Enter meant "new line", and standard markdown's
              soft-wrap would silently join every such line into one run-on.

              ⚠️ WHAT THIS COSTS THE ENTRIES THAT ALREADY EXIST, stated rather than
              papered over: every stored body was written under a plain-text
              contract, so characters that are now syntax are now read as syntax.
              Chat had the identical problem with the identical corpus and did NOT
              escape the text (it renders `m.body` raw through the same component),
              and this follows chat — one behaviour for one kind of text, rather
              than a second, page-local escaping rule that would make the same
              sentence render two ways on two pages. What limits the blast radius is
              the renderer's own inline set: only `**bold**`, `` `code` `` and
              links. `_` is NOT inline syntax there, so a body carrying
              `note_size_chars` is unaffected — the case that prompted the worry. */}
          <div
            className={`lore-row__body${
              expanded ? "" : " lore-row__body--clamped"
            }`}
            data-testid="lore-body"
          >
            <Markdown source={entry.body} className="lore-row__body-md doc-md" breaks />
          </div>
        </>
      )}

      {/* 🔴 ALWAYS RENDERED — NOT `expanded &&` ANY MORE (owner 2026-09-08,
          card rc-2e10ee03b97c). 展開／收合 now governs the CONTENT's length and
          nothing else, so 撰寫人 / 生效期 / 失效理由 are on the closed row too.
          Re-wrapping this in `expanded` would put the page back to hiding the
          three rows the owner asked to see. */}
      <div className="lore-row__meta">
        {/* 撰寫人自成一列: label left, avatar+name pill right, and the
            傳訊息 icon INSIDE the pill. */}
        <span className="lore-row__meta-label">{t.lore.authorLabel}</span>
        <LoreAuthorChip
          author={author}
          avatar={avatar}
          onOpenChat={(peerId) => onOpenChat(peerId, entry.id)}
        />

        {/* 生效期 directly UNDER 撰寫人 (owner: 「生效期移到撰寫人下面一列」). */}
        <span className="lore-row__meta-label">{t.lore.effectiveLabel}</span>
        <span className="lore-row__effective-ts" data-testid="lore-effective">
          {formatAbsolute(entry.effectiveTs, Date.now() / 1000)}
        </span>

        {/* 失效理由沒填就整列不顯示 — no label, no empty value, no dash. An
            empty reason is not a reason, and a row that renders one teaches
            the reader that retirements come without explanations. */}
        {entry.retireReason !== "" && (
          <>
            <span className="lore-row__meta-label">
              {t.lore.retireReasonLabel}
            </span>
            <span
              className="lore-row__retire-reason"
              data-testid="lore-retire-reason"
            >
              {entry.retireReason}
            </span>
          </>
        )}
      </div>

      {/* The box to write to this entry's author — LAST ON THE CARD, in BOTH
          states (owner 2026-09-08, screenshot: 「訊息匡應該在最下方」). It sat
          between 內容 and 撰寫人／生效期, which put a writing surface in the
          middle of the things you read.
          🔴 IT IS OUTSIDE `expanded` AND MUST STAY OUTSIDE IT, the way 任務卡's
          message box is always on the card (owner: 「訊息輸入框預設就要在」). A
          reader who wants to ask one question about a row they can already read
          should not have to open the row first — a step the 任務 page does not
          ask for. Putting it after the expanded block is what satisfies BOTH
          rulings at once: collapsed, the block above renders nothing and this is
          the last thing on the card anyway; expanded, it is below 撰寫人 /
          生效期 / 失效理由. Moving it back INSIDE `expanded` to "put it at the
          bottom" would trade one owner ruling for the other.
          🔴 Only a reachable author gets one. The departed author's pill is
          deliberately not a button, and a composer that sends to nobody would be
          that same dead affordance in a larger shape.
          It is not a cell of `.lore-row__meta`'s grid, so lore.css gives it the
          full row width itself rather than a `grid-column` span. */}
      {author.peerId !== "" && (
        <LoreAuthorComposer entryId={entry.id} author={author} />
      )}
    </article>
  );
}

/** 編輯 turns the card's title and body into this, in place. */
function LoreEntryEditor({
  entry,
  caps,
  describeEdit,
  onRestored,
  onCancel,
  onSave,
}: {
  entry: LoreEntryView;
  caps: { title: number; body: number };
  describeEdit: (oldChars: number, newChars: number) => string;
  /** Re-reads the card after a 版本紀錄 restore has landed. */
  onRestored: () => Promise<void>;
  onCancel: () => void;
  onSave: (patch: LoreEntryEdit) => Promise<void>;
}): ReactNode {
  const { t } = useI18n();
  const [title, setTitle] = useState(entry.title);
  const [body, setBody] = useState(entry.body);
  const [loreType, setLoreType] = useState<LoreType>(entry.loreType);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const bodyRef = useRef<HTMLTextAreaElement>(null);
  useLayoutEffect(() => {
    if (bodyRef.current) autosizeTextarea(bodyRef.current);
  }, [body]);

  const titleLen = [...title].length;
  const bodyLen = [...body].length;
  const titleBlank = title.trim() === "";
  const over = titleLen > caps.title || bodyLen > caps.body;
  const changed = title !== entry.title || body !== entry.body || loreType !== entry.loreType;
  const canSave = !saving && changed && !titleBlank && !over;

  async function save() {
    if (!canSave) return;
    const patch: LoreEntryEdit = {};
    if (title !== entry.title) patch.title = title;
    if (body !== entry.body) patch.body = body;
    if (loreType !== entry.loreType) patch.loreType = loreType;
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
        <span className="lore-row__editor-label">
          {t.lore.editTitleLabel}
          <span
            className={`lore-row__editor-count${titleLen > caps.title ? " lore-row__editor-count--over" : ""}`}
            data-testid="lore-edit-title-count"
          >
            {titleLen} / {caps.title}
          </span>
        </span>
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
        <span className="lore-row__editor-label">
          {t.lore.editBodyLabel}
          <span
            className={`lore-row__editor-count${bodyLen > caps.body ? " lore-row__editor-count--over" : ""}`}
            data-testid="lore-edit-body-count"
          >
            {bodyLen} / {caps.body}
          </span>
        </span>
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
      <div className="lore-row__editor-field">
        <span className="lore-row__editor-label">{t.lore.editTypeLabel}</span>
        <div className="lore-row__segmented" role="radiogroup" aria-label={t.lore.editTypeLabel}>
          {LORE_TYPES.map((lt) => (
            <button
              key={lt}
              type="button"
              role="radio"
              aria-checked={loreType === lt}
              className={`lore-row__segment${loreType === lt ? " lore-row__segment--on" : ""}`}
              data-testid={`lore-edit-type-${lt}`}
              disabled={saving}
              onClick={() => setLoreType(lt)}
            >
              {loreTypeLabel(t, lt)}
            </button>
          ))}
        </div>
      </div>
      <div className="lore-row__editor-effect" data-testid="lore-edit-effect">
        {describeEdit(entryChars(entry.title, entry.body), titleLen + bodyLen)}
      </div>
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

/** The box under 撰寫人 — one sentence to the person who wrote this entry,
 * sent without leaving the page.
 *
 * 🔴 THE ENTRY ID IS PREPENDED BY THIS BOX, NOT TYPED BY THE READER. The whole
 * reason this affordance exists is that the author may hold dozens of entries
 * and 「這條還適用嗎」 without a subject costs them a round trip to ask which
 * one. Leaving the reader to type the id would put the failure back exactly
 * where it was.
 *
 * ⚠️ NOTHING ON SCREEN SAYS SO ANY MORE. There used to be a line under the box
 * (`.lore-row__composer-note`) — the only place the reader learned that what
 * goes out is not exactly what they typed — and owner removed it in the round
 * that made this row look like a 任務卡 (任務卡 has no such line). The PREFIX
 * ITSELF WAS DELIBERATELY LEFT ALONE: that round was 外觀 only. So the silent
 * rewrite the note existed to disclose is now silent, and that is a known,
 * chosen cost, not an oversight to quietly re-fix here.
 *
 * 🔴 A FAILED SEND KEEPS THE DRAFT AND SAYS SO. A box that clears itself on a
 * failure looks identical to one that succeeded, and what is lost is the
 * reader's own sentence.
 *
 * ATTACHMENTS RIDE THE SAME MESSAGE, through the SAME staging machine the 任務卡
 * composer and the chat composer use (useAttachmentStaging): paste an image into
 * the box or pick files with the 📎, and they go out on the one postChat call
 * this box makes. The slot is per-mount, so one entry's staged files can never
 * surface under another's box.
 */
function LoreAuthorComposer({
  entryId,
  author,
}: {
  entryId: string;
  author: AuthorIdentity;
}): ReactNode {
  const { t } = useI18n();
  const isMobile = useIsMobile();
  // The owner's 建議回覆 for THIS box (T-33) — its own settings list, never the
  // 請示卡 or 任務 one. Empty (the shipped state, and the answer to a failed
  // settings read) renders nothing at all, and the box is untouched by it.
  const suggestedReplies = useSuggestedRepliesLoreMessage();
  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);
  const [failed, setFailed] = useState(false);
  const [sent, setSent] = useState(false);
  const boxRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const isComposingRef = useRef(false);
  // The SHARED staging machine (same caps, same paste/pick funnels as the chat
  // composer / 任務卡). `STAGING_TARGET_PER_MOUNT` is the honest slot for this
  // surface: a LoreRow is mounted per entry, so the files die with the entry's
  // box and one row's staging cannot appear under another row's.
  const {
    pendingAttachments,
    attachError,
    onPaste,
    onPickFile,
    removeAttachment,
    clearAttachments,
  } = useAttachmentStaging(STAGING_TARGET_PER_MOUNT);
  // 🔴 TEXT **OR** FILES — the same rule 任務卡 uses. A message that is only a
  // screenshot is a legitimate message; requiring a sentence beside it would
  // make the 📎 an affordance that cannot complete on its own.
  const canSend =
    !sending && (draft.trim().length > 0 || pendingAttachments.length > 0);

  // Auto-grow to the draft, same as the chat composer; the CSS max-height caps
  // it and the box scrolls beyond that.
  useLayoutEffect(() => {
    if (boxRef.current) autosizeTextarea(boxRef.current);
  }, [draft]);

  async function send() {
    if (!canSend) return;
    const attachments: ChatAttachmentInput[] = pendingAttachments.map((a) => ({
      dataB64: a.dataUri,
      ...(a.filename ? { filename: a.filename } : {}),
    }));
    setSending(true);
    setSent(false);
    try {
      // The id LEADS the message, the same shape the 任務 message box sends
      // 「[TaskID=T-33] …」 in — and it now NAMES THE KIND OF ID it carries
      // (owner rc-01a07b1b2a12 [0][1][2], rc-379631993586 「ok. B.」). 「[L-3] …」
      // said the id without ever saying what it was an id OF, which is only
      // readable by someone who already knows both id namespaces.
      //
      // 🔴 THE LITERAL IS WRITTEN HERE AND IS NOT AN i18n KEY, deliberately.
      // That is the reason the owner picked this form: the reader on the other
      // end is an agent, not this browser, and a prefix that changed with the
      // cockpit's language would be a prefix nothing can match on.
      //
      // 🔴 `meta.lore_entry_id` IS THE OTHER HALF AND IS NOT A DUPLICATE OF IT.
      // The prefix is for a PERSON and is display — the owner can reword it,
      // and a round of 外觀 work already deleted the note that used to sit
      // beside it. `meta` is for a PROGRAM: an agent receiving this message has
      // to know WHICH ENTRY it is about without parsing a human-facing string
      // that is free to change. The task message box has carried meta.task_id
      // since it existed; this box carried an empty meta until now.
      //
      // The server copies unknown meta keys through WHOLESALE — read, not
      // assumed: HandlePostChatApiChatPost (api_chat.go) builds its map from
      // body.Meta, then `delete`s `reply_to` and `reply_excerpt` (it is the
      // only writer of both) and OVERWRITES `attachments` when the post
      // carries any. Every other key, this one included, is stored verbatim.
      await api.postChat({
        to: author.peerId,
        body: `[LoreID=${entryId}] ${draft.trim()}`,
        meta: { lore_entry_id: entryId },
        ...(attachments.length > 0 ? { attachments } : {}),
      });
      setDraft("");
      clearAttachments();
      setFailed(false);
      setSent(true);
    } catch (e) {
      console.warn("LorePage: message to author failed", e);
      // The typed content stays — retry-friendly, and the notice says so. The
      // STAGED FILES stay for the same reason: clearing them on a failure would
      // silently cost the reader the thing they cannot retype.
      setFailed(true);
    } finally {
      setSending(false);
    }
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    // Shared send rule (IME gate + mobile newline) — see lib/composerKeys. On a
    // phone Enter is left un-prevented so the box inserts a native newline and
    // the button is the only way to send.
    if (enterShouldSend(e, { isMobile, composing: isComposingRef.current })) {
      e.preventDefault();
      void send();
    }
  }

  return (
    <div className="lore-row__composer" data-testid="lore-author-composer">
      {/* Staged-attachment preview strip — the SAME component (and therefore the
       * same markup and classes) the chat composer / ReplyComposer / 任務卡 use.
       * The mount guard lives here, as it does at every other call site. */}
      {(pendingAttachments.length > 0 || attachError) && (
        <ComposerAttachmentPreview
          pendingAttachments={pendingAttachments}
          attachError={attachError}
          onRemove={removeAttachment}
        />
      )}
      <div className="lore-row__composer-row">
        <input
          ref={fileInputRef}
          type="file"
          accept={ATTACH_ACCEPT}
          multiple
          onChange={onPickFile}
          hidden
        />
        <button
          type="button"
          className="lore-row__composer-attach"
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
          className="lore-row__composer-input"
          rows={1}
          value={draft}
          disabled={sending}
          placeholder={t.lore.messagePlaceholder(author.text)}
          data-testid="lore-msg-input"
          onChange={(e) => {
            setDraft(e.target.value);
            setSent(false);
          }}
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
          className="lore-row__composer-send"
          disabled={!canSend}
          data-testid="lore-msg-send"
          onClick={() => void send()}
        >
          <SendIcon size={14} />
          {t.lore.messageSend}
        </button>
      </div>
      {/* T-33: a pick FILLS this box and leaves the send to the owner — the same
       * rule the other two boxes keep. This message goes to the entry's writer
       * and cannot be recalled, so a mis-tap must cost a keystroke, never a
       * message. It sits OUTSIDE `.lore-row__composer-row` (the flex row holding
       * the 📎, the textarea and 送出): a row of its own, under that one, as on
       * 任務卡. */}
      <SuggestedReplies
        replies={suggestedReplies}
        testId="lore-suggested-replies"
        onPick={(suggestion) => {
          setDraft((cur) => appendSuggestion(cur, suggestion));
          boxRef.current?.focus();
        }}
      />
      {failed && (
        <div
          className="lore-row__composer-error"
          role="status"
          data-testid="lore-msg-failed"
        >
          {t.lore.messageFailed}
        </div>
      )}
      {sent && !failed && (
        <div
          className="lore-row__composer-sent"
          role="status"
          data-testid="lore-msg-sent"
        >
          {t.lore.messageSent}
        </div>
      )}
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
