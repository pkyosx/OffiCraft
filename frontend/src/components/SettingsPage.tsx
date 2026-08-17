import { useEffect, useRef, useState, type ReactNode } from "react";
import { useI18n } from "../i18n";
import type {
  RoleSummaryView,
  VersionView,
  ReleaseCheckView,
} from "../types";
import { api, type ServerSettingsView, type ServerSettingsPatch } from "../api";
import { ApiError } from "../api/errors";
import { useVersion } from "../hooks/useVersion";
import { useBackupHealth } from "../hooks/useBackupHealth";
import {
  backupIndicatorState,
  backupStatusLabel,
  backupReasonText,
} from "../lib/backupHealth";
import { formatDuration } from "../lib/duration";
import { formatBuildVersion } from "../lib/versionFormat";
import { useGlobalContext } from "../hooks/useGlobalContext";
import { useRole, useRoles } from "../hooks/useRoles";
import { useServerSettings } from "../hooks/useServerSettings";
import { refreshServerSettings } from "../hooks/sharedServerSettings";
import { useTaskManual, useTaskManuals } from "../hooks/useTaskManuals";
import { useMembers } from "../hooks/useMembers";
import {
  TaskManualsList,
  TaskManualHub,
  TaskManualDefinitionPage,
  TaskManualLearningsPage,
} from "./TaskManualsPage";
import type { TaskManualPatch } from "../api/adapter";
import { isHttpStatus } from "../api/errors";
import { BootDocPage } from "./BootDocPage";
import { DocCard } from "./DocCard";
import { LessonsCard } from "./LessonsCard";
import { InsightCard } from "./InsightCard";
import { navigateHash } from "../lib/hashRoute";
import { DOC_CAP_CHARS_DEFAULTS } from "../api/docCap";
import {
  CHAT_BUDGET_CHARS_MAX,
  CHAT_BUDGET_CHARS_MIN,
} from "../api/chatBudget";

/** The adjustable document caps (T-ae38, widened by T-30f1), in the order the
 * parameters card lists them: the three role-journal segments in journal order
 * (Duty → Insight → Learning), then the task manual's pair. The key IS the ServerSettingsView /
 * ServerSettingsPatch field, so the row cannot read one setting and write
 * another.
 *
 * `min` is per row and is NOT decoration: Duty's floor is its OWN shipped
 * default. Sharing the other segments' floor here would make the local guard
 * reject the value the server ships with — the field would refuse its own
 * current contents. The numbers come from DOC_CAP_CHARS_DEFAULTS (docCap.ts,
 * mirroring server/ocserverd/domain.go) and are never restated: they are
 * owner-adjustable settings, so a literal here is a guard that goes stale. */
type DocCapField =
  | "docCapCharsDuty"
  | "docCapCharsInsight"
  | "docCapCharsLearning"
  | "docCapCharsManualSop"
  | "docCapCharsManualLearnings";

const DOC_CAP_FIELDS: Record<
  DocCapField,
  { min: number; inputId: string; labelKey: "docCapDuty" | "docCapInsight" | "docCapLearning" | "docCapManualSop" | "docCapManualLearnings"; subKey: "docCapDutySub" | "docCapInsightSub" | "docCapLearningSub" | "docCapManualSopSub" | "docCapManualLearningsSub" }
> = {
  docCapCharsDuty: { min: DOC_CAP_CHARS_DEFAULTS.duty, inputId: "param-doc-cap-duty", labelKey: "docCapDuty", subKey: "docCapDutySub" },
  docCapCharsInsight: { min: DOC_CAP_CHARS_DEFAULTS.insight, inputId: "param-doc-cap-insight", labelKey: "docCapInsight", subKey: "docCapInsightSub" },
  docCapCharsLearning: { min: DOC_CAP_CHARS_DEFAULTS.learning, inputId: "param-doc-cap-learning", labelKey: "docCapLearning", subKey: "docCapLearningSub" },
  docCapCharsManualSop: { min: DOC_CAP_CHARS_DEFAULTS.manualSop, inputId: "param-doc-cap-manual-sop", labelKey: "docCapManualSop", subKey: "docCapManualSopSub" },
  docCapCharsManualLearnings: { min: DOC_CAP_CHARS_DEFAULTS.manualLearnings, inputId: "param-doc-cap-manual-learnings", labelKey: "docCapManualLearnings", subKey: "docCapManualLearningsSub" },
};

const DOC_CAP_ORDER: DocCapField[] = [
  "docCapCharsDuty",
  "docCapCharsInsight",
  "docCapCharsLearning",
  "docCapCharsManualSop",
  "docCapCharsManualLearnings",
];
import { Breadcrumbs, type Crumb } from "./Breadcrumbs";
import {
  ChevronRightIcon,
  DownloadIcon,
  FileTextIcon,
  UsersIcon,
  GlobeIcon,
  UserIcon,
  PencilIcon,
  BoltIcon,
  LogOutIcon,
  MonitorIcon,
  GearIcon,
  TrashIcon,
  RefreshIcon,
  MoonIcon,
} from "./icons";
import { ThemeSettings } from "./ThemeSettings";
import { ConfirmModal } from "./ConfirmModal";
import "./settings.css";

// Which settings sub-view is showing. Navigation is internal to the page; the
// user leaves Settings entirely by clicking a nav tab (App owns that).
// The old single "global" doc is the blocks of the assembled boot context
// (global-context-3block-restructure): "system" (系統互動) / "custom"
// (使用者自訂, the additive block behind /api/global-context) / "boot"
// (啟動程序, ONE VIEW PER RUNTIME — see the boot view below). T-791e: all of
// them are owner-editable now; "read-only seed" no longer describes any of
// them, and the two boot sequences are separate documents.
type View =
  | { kind: "landing" }
  | { kind: "software" }
  | { kind: "roles" }
  | { kind: "params" }
  | { kind: "theme" }
  | { kind: "system" }
  | { kind: "custom" }
  // 啟動程序 is TWO documents. `boot` is the INDEX — two nav rows, one per
  // runtime — and `bootDoc` is one runtime's own page (T-bac4, owner:「可以改成
  // 像任務手冊那樣嗎」). The index carries no document of its own, which is why
  // it may be keyless; every document still has its own page.
  | { kind: "boot" }
  | { kind: "bootDoc"; runtime: "claude" | "codex" }
  // 下線程序 (T-c9c0) — ONE document, so no index/detail split: the nav row
  // opens the page directly, the shape 系統互動 already has.
  | { kind: "offboard" }
  | { kind: "role"; key: string }
  | { kind: "manuals" }
  // 任務手冊詳情 = hub (摘要卡 + 任務規劃入口卡): the two 任務規劃 cards
  // (任務定義/學習經驗) PUSH their own sub-page (owner 2026-07-20).
  | { kind: "manual"; key: string }
  | { kind: "manualDef"; key: string }
  | { kind: "manualLearnings"; key: string };

export function SettingsPage({
  initialManualKey,
  initialRoles,
  initialRolesCreate,
  initialRoleKey,
}: {
  /** #settings/manuals/<key> deep-link (T-e987 任務類型 label 跳轉): open
   * straight on that manual's hub. Read once as the initial view — an
   * unknown/deleted key self-heals to the manuals list (the {kind:"manual"}
   * render below). App keys SettingsPage on the manual key so a fresh
   * deep-link re-mounts here. */
  initialManualKey?: string;
  /** #settings/roles deep-link (T-f074 正職 header ➕👤): open straight on the
   * 角色誌 list so the owner can add a role. Same one-shot initial-view read
   * as initialManualKey; App keys SettingsPage on it so a fresh deep-link
   * re-mounts here. */
  initialRoles?: boolean;
  /** #settings/roles/new deep-link (T-25b7 roster ➕👤): open the 角色誌 list
   * with the inline 新增角色 create row already expanded. Implies the roles
   * view; a one-shot initial read (App keys SettingsPage on it), consumed the
   * moment the user navigates away from the roles view. */
  initialRolesCreate?: boolean;
  /** #settings/roles/<roleKey> deep-link (roster role-line gear): open
   * straight on that role's definition page. Same one-shot initial-view read
   * as initialManualKey (App keys SettingsPage on it); an unknown/deleted key
   * self-heals to the roles list once roles have loaded. */
  initialRoleKey?: string;
} = {}) {
  const { t } = useI18n();
  const [view, setView] = useState<View>(
    initialManualKey
      ? { kind: "manual", key: initialManualKey }
      : initialRoleKey
        ? { kind: "role", key: initialRoleKey }
        : initialRoles || initialRolesCreate
          ? { kind: "roles" }
          : { kind: "landing" }
  );
  // One-shot create-mode intent from the #settings/roles/new deep-link. Seeds
  // the roles list's inline create row on the initial landing, then clears the
  // moment we leave the roles view — so navigating role detail ⇄ back to the
  // list does NOT force the create row open again.
  const [rolesCreatePending, setRolesCreatePending] = useState(
    !!initialRolesCreate
  );
  useEffect(() => {
    if (view.kind !== "roles") setRolesCreatePending(false);
  }, [view.kind]);

  // Lifted so edits/resets stay coherent across the list ⇄ detail navigation
  // (a single source of truth per resource).
  const version = useVersion();
  const gc = useGlobalContext();
  const rolesH = useRoles();
  const params = useServerSettings();
  // 任務手冊 (SPEC §5) — lifted like the others so list ⇄ detail stay coherent;
  // the roster feeds the manual detail's 負責成員 member picker.
  const manualsH = useTaskManuals();
  const { members } = useMembers();
  // T-1170: neither list answer carries its long documents any more, so the
  // pages that RENDER one read it themselves — named, and only while that page
  // is on screen (`""` requests nothing). Called unconditionally here because
  // hooks must be; the branches below just consume the result.
  const roleDoc = useRole(view.kind === "role" ? view.key : "");
  const manualDoc = useTaskManual(
    view.kind === "manualDef" || view.kind === "manualLearnings"
      ? view.key
      : ""
  );

  // ── unified breadcrumb navigation (T-8f6e) ──
  // Crumb jumps move the internal view via setView; where the target segment
  // has a hash route (#settings / #settings/roles) the hash is written too —
  // through lib/hashRoute, the single routing seam — so App (which keys
  // SettingsPage on the route) re-mounts deep-linked sessions onto the same
  // view and the URL never lies about where the owner is.
  const goLanding = () => {
    setView({ kind: "landing" });
    navigateHash({ page: "settings" });
  };
  const goRoles = () => {
    setView({ kind: "roles" });
    navigateHash({ page: "settings", settingsRoles: true });
  };
  const crumbRoot: Crumb = { label: t.settings.title, onClick: goLanding };
  const crumbRoles: Crumb = { label: t.settings.roles, onClick: goRoles };

  if (view.kind === "software") {
    return (
      <SoftwareUpdate
        version={version.version}
        onRefreshVersion={version.refresh}
        settings={params.settings}
        settingsError={params.error}
        saveError={params.saveError}
        onSave={params.save}
        onClearSaveError={params.clearSaveError}
        crumbs={[crumbRoot, { label: t.settings.software }]}
      />
    );
  }

  // A #settings/roles/<roleKey> deep-link whose key no longer resolves
  // self-heals to the roles list once loading settled (manual-hub precedent)
  // — never render a fabricated role. While roles are still loading the role
  // view below renders its own honest loading state.
  const roleViewStale =
    view.kind === "role" &&
    !rolesH.loading &&
    !rolesH.roles.some((r) => r.key === view.key);

  if (view.kind === "roles" || roleViewStale) {
    return (
      <RolesLog
        roles={rolesH.roles}
        // Honest: a failed role/global-context load must not read as "no roles".
        error={rolesH.error || gc.error}
        crumbs={[crumbRoot, { label: t.settings.roles }]}
        onOpenSystem={() => setView({ kind: "system" })}
        onOpenCustom={() => setView({ kind: "custom" })}
        onOpenBoot={() => setView({ kind: "boot" })}
        onOpenOffboard={() => setView({ kind: "offboard" })}
        onOpenRole={(key) => setView({ kind: "role", key })}
        onCreate={rolesH.create}
        onDelete={rolesH.remove}
        autoCreate={rolesCreatePending}
      />
    );
  }

  const manualsCrumbs: Crumb[] = [crumbRoot, { label: t.settings.manuals }];

  if (view.kind === "manuals") {
    return (
      <TaskManualsList
        manuals={manualsH.manuals}
        loading={manualsH.loading}
        error={manualsH.error}
        crumbs={manualsCrumbs}
        onOpen={(key) => setView({ kind: "manual", key })}
        onCreate={manualsH.create}
        onDelete={manualsH.remove}
      />
    );
  }

  if (view.kind === "manual") {
    const key = view.key;
    const manual = manualsH.manuals.find((m) => m.typeKey === key);
    // A deleted/unknown key self-heals to the list (same rule as stale hash
    // ids elsewhere) — never render a fabricated manual.
    if (!manual) {
      return (
        <TaskManualsList
          manuals={manualsH.manuals}
          loading={manualsH.loading}
          error={manualsH.error}
          crumbs={manualsCrumbs}
          onOpen={(k) => setView({ kind: "manual", key: k })}
          onCreate={manualsH.create}
          onDelete={manualsH.remove}
        />
      );
    }
    return (
      <TaskManualHub
        manual={manual}
        members={members}
        crumbs={[
          crumbRoot,
          {
            label: t.settings.manuals,
            onClick: () => setView({ kind: "manuals" }),
          },
          // T-fa76: the crumb shows the human label (mono only when falling
          // back to the raw type_key).
          {
            label: manual.displayName || manual.typeKey,
            mono: !manual.displayName,
          },
        ]}
        onSave={(patch) => manualsH.update(key, patch)}
        onOpenDefinition={() => setView({ kind: "manualDef", key })}
        onOpenLearnings={() => setView({ kind: "manualLearnings", key })}
      />
    );
  }

  // 任務定義 / 學習經驗 sub-pages (owner 2026-07-20) — pushed from the hub. Both
  // self-heal to the manuals list on an unknown/deleted key (the hub's rule),
  // and their breadcrumb's <type> segment jumps back to the hub.
  if (view.kind === "manualDef" || view.kind === "manualLearnings") {
    const key = view.key;
    const manual = manualsH.manuals.find((m) => m.typeKey === key);
    if (!manual) {
      return (
        <TaskManualsList
          manuals={manualsH.manuals}
          loading={manualsH.loading}
          error={manualsH.error}
          crumbs={manualsCrumbs}
          onOpen={(k) => setView({ kind: "manual", key: k })}
          onCreate={manualsH.create}
          onDelete={manualsH.remove}
        />
      );
    }
    const subCrumbs: Crumb[] = [
      crumbRoot,
      { label: t.settings.manuals, onClick: () => setView({ kind: "manuals" }) },
      {
        label: manual.displayName || manual.typeKey,
        mono: !manual.displayName,
        onClick: () => setView({ kind: "manual", key }),
      },
      {
        label:
          view.kind === "manualDef"
            ? t.settings.manualTabDefinition
            : t.settings.manualTabLearnings,
      },
    ];
    // The update echo is the manual AFTER the edit, so this page adopts it
    // rather than waiting for the list refetch (which no longer carries either
    // document) or for an SSE frame.
    const onSave = async (patch: TaskManualPatch) => {
      const next = await manualsH.update(key, patch);
      manualDoc.adopt(next);
      return next;
    };
    // A restore rewrites ONE of the manual's documents server-side, so this
    // page re-reads its own manual; the list follows for the row it shows.
    const onRestored = async () => {
      await manualDoc.refetch();
      await manualsH.refetch();
    };
    return view.kind === "manualDef" ? (
      <TaskManualDefinitionPage
        manual={manualDoc.manual}
        loadError={manualDoc.error}
        crumbs={subCrumbs}
        onSave={onSave}
        onRestored={onRestored}
      />
    ) : (
      <TaskManualLearningsPage
        manual={manualDoc.manual}
        loadError={manualDoc.error}
        crumbs={subCrumbs}
        onSave={onSave}
        onRestored={onRestored}
      />
    );
  }

  if (view.kind === "params") {
    return (
      <ServerParams
        settings={params.settings}
        error={params.error}
        saveError={params.saveError}
        onSave={params.save}
        onClearSaveError={params.clearSaveError}
        crumbs={[crumbRoot, { label: t.settings.params }]}
      />
    );
  }

  if (view.kind === "theme") {
    // 主題 — the theme MANAGEMENT surface (T-16a1 P3b). Add / import / export /
    // edit (friendly colours + 用詞 overlay) / delete. The ProfileDropdown keeps
    // only the theme SELECTOR + language (owner IA: 偏好=選擇, 設定=管理).
    return (
      <ThemeSettings crumbs={[crumbRoot, { label: t.settings.themeManage }]} />
    );
  }

  if (view.kind === "system") {
    // 系統互動 — the FIRST block of every agent's boot context, and editable
    // since T-791e. It used to be a read-only render of SEED_SYSTEM_INTERACTION_MD
    // ("the backend has NO write endpoint BY CONSTRUCTION"); it now has one, so
    // the page reads the LIVE document through the api seam. The seed still
    // exists — as the FACTORY version behind 還原出廠版 and the history list's
    // 初始版本 row — but it is no longer what the page body renders.
    return (
      <BootDocPage
        kind="system_interaction"
        docKey="global"
        title={t.settings.systemName}
        historyTitle={t.settings.historyBootSystemTitle}
        crumbs={[crumbRoot, crumbRoles, { label: t.settings.systemName }]}
      />
    );
  }

  if (view.kind === "offboard") {
    // 下線程序 — what an agent is handed at the moment the server collects its
    // session (T-c9c0). Same editable-document shape as 系統互動, and a
    // SINGLETON for the same reason: being collected is one procedure whatever
    // runtime an agent runs, so there is no runtime to choose here.
    return (
      <BootDocPage
        kind="offboard"
        docKey="global"
        title={t.settings.offboardName}
        historyTitle={t.settings.historyBootOffboardTitle}
        crumbs={[crumbRoot, crumbRoles, { label: t.settings.offboardName }]}
      />
    );
  }

  if (view.kind === "custom") {
    // 使用者自訂 — the owner-editable ADDITIVE block (/api/global-context).
    // Empty text + isDefault=true = never written; the assembled boot context
    // skips the block entirely. Save = whole-block replace, reset = tombstone.
    return (
      <DocCard
        title={t.settings.customName}
        doc={gc.ctx}
        crumbs={[crumbRoot, crumbRoles, { label: t.settings.customName }]}
        onSave={gc.save}
        onReset={gc.reset}
        // 版本紀錄 (T-1f39): the entry sits in the EDIT toolbar, where 重置
        // used to. Restoring rewrites the live block, so the visible doc is
        // re-read from the server rather than assumed.
        history={{
          kind: "global_context",
          docKey: "global",
          title: t.settings.historyGlobalTitle,
          // The live block under its WIRE field name — the modal's diff
          // compares a revision against what the server currently stores,
          // which is exactly the text this page is rendering. `undefined`
          // while it loads, so the diff never compares against a blank.
          currentContent: gc.ctx ? { text: gc.ctx.text } : undefined,
          onRestored: gc.refetch,
        }}
      />
    );
  }

  if (view.kind === "boot") {
    // 🔴 AN INDEX OF TWO, NOT TWO DOCUMENTS STACKED (T-bac4, owner 2026-08-15
    // on rc-08e1e073c293:「我覺得呈現方式不好，可以改成像任務手冊那樣嗎」, with
    // the 任務手冊 hub as the reference picture). Same `.set-entry` rows the
    // 任務規劃 cards use — icon, title, one-line sub, right chevron — and each
    // one PUSHES its runtime's own page.
    //
    // WHAT THIS REPLACED, AND WHY THE HISTORY MATTERS. The page used to render
    // BOTH documents stacked, then both stacked-and-collapsed (T-6278). Both
    // shapes were answers to the same complaint: he met this page on a phone,
    // scrolled the first document to its end, and read that as the end of the
    // PAGE — 啟動程序 (Codex CLI) sat below the fold and might as well not have
    // existed. Collapsing put both headings on one screen; an index does it
    // without an expand/collapse mechanism at all, so the identity of each
    // document is carried by a permanent row rather than by a heading whose
    // position moves when its neighbour opens.
    //
    // ⚠️ THE INVARIANT THAT SURVIVES EVERY SHAPE: the two remain two SEPARATE
    // documents with separate editors, save buttons and version histories, and
    // nothing writes both. Their third step means OPPOSITE things (claude
    // attaches `ocagent listen` itself; codex must NOT — the sidecar does), so
    // copying one runtime's text over the other stops that runtime's agents
    // ever coming online, silently. Separate PAGES make that copy harder than
    // the stacked shape did, not easier.
    //
    // 🔴 EACH ROW NAMES ITS OWN DOCUMENT. T-6278's review sent that build back
    // precisely here: its collapse toggles carried an aria-label that covered
    // both documents' identity, so the very defect being fixed was reproduced
    // in the accessibility tree. The rows below take their accessible name from
    // their own visible title (bootClaudeName / bootCodexName), which is the
    // runtime — do not replace that with a shared label.
    return (
      <div className="settings">
        <Breadcrumbs
          items={[crumbRoot, crumbRoles, { label: t.settings.bootName }]}
        />
        <h1 className="settings__title">{t.settings.bootName}</h1>
        <div className="set-entries">
          <button
            type="button"
            className="set-entry"
            data-testid="boot-entry-claude"
            onClick={() => setView({ kind: "bootDoc", runtime: "claude" })}
          >
            <span className="set-entry__icon set-entry__icon--violet">
              <BoltIcon size={18} />
            </span>
            <span className="set-entry__body">
              <span className="set-entry__name">{t.settings.bootClaudeName}</span>
              <span className="set-entry__sub">{t.settings.bootClaudeSub}</span>
            </span>
            <ChevronRightIcon size={18} className="set-entry__chev" />
          </button>
          <button
            type="button"
            className="set-entry"
            data-testid="boot-entry-codex"
            onClick={() => setView({ kind: "bootDoc", runtime: "codex" })}
          >
            <span className="set-entry__icon set-entry__icon--blue">
              <MonitorIcon size={18} />
            </span>
            <span className="set-entry__body">
              <span className="set-entry__name">{t.settings.bootCodexName}</span>
              <span className="set-entry__sub">{t.settings.bootCodexSub}</span>
            </span>
            <ChevronRightIcon size={18} className="set-entry__chev" />
          </button>
        </div>
      </div>
    );
  }

  if (view.kind === "bootDoc") {
    // One runtime, one page, its own editor / save / version history / restore
    // — all of it BootDocPage's, untouched by T-bac4. `collapsible` is
    // deliberately NOT passed: a page that holds one document has nothing to
    // collapse, and the fold it existed to solve is gone with the stack.
    const claude = view.runtime === "claude";
    return (
      <BootDocPage
        kind="boot_sequence"
        docKey={view.runtime}
        title={claude ? t.settings.bootClaudeName : t.settings.bootCodexName}
        historyTitle={
          claude
            ? t.settings.historyBootClaudeTitle
            : t.settings.historyBootCodexTitle
        }
        // The runtime's own name is the TERMINAL crumb, so 啟動程序 sits one
        // step up and stays clickable — Breadcrumbs renders the last segment as
        // plain text on purpose, so a trail ending at 啟動程序 would leave the
        // reader unable to reach the other runtime without going out to 角色誌
        // and back in.
        crumbs={[
          crumbRoot,
          crumbRoles,
          { label: t.settings.bootName, onClick: () => setView({ kind: "boot" }) },
          {
            label: claude
              ? t.settings.bootClaudeName
              : t.settings.bootCodexName,
          },
        ]}
      />
    );
  }

  if (view.kind === "role") {
    const role = rolesH.roles.find((r) => r.key === view.key);
    // The persona page: role definition (top) + this role's OWN lessons
    // (per-role-learnings step1). The lessons card is the SAME shared
    // <LessonsCard> the app uses everywhere — scoped here to view.key so the
    // owner edits exactly this persona's accumulated learnings. `extra` renders
    // inside DocCard's <div className="settings"> so the card inherits page
    // width/gutters and sits directly under the role_def card.
    //
    // Localized role label (matches the office/monitor roster + mockup 助理),
    // NOT the raw seed DTO.name ("Assistant"): the whole app localizes role
    // names by key. Falls back to the DTO name / key for an unknown role.
    const roleTitle =
      (t.office.role as Record<string, string>)[view.key] ??
      role?.name ??
      view.key;
    return (
      <DocCard
        title={roleTitle}
        // 角色名 rename — CUSTOM roles only (seed titles are i18n-localized by
        // key AND server-side name-locked). Same pencil inline-edit pattern as
        // the machine row; the save rides the existing role PATCH choke, and
        // the roster's role display names follow (single truth: role.name).
        onRenameTitle={
          role && !role.isSeed
            ? async (name) => {
                roleDoc.adopt(await rolesH.save(view.key, { name }));
              }
            : undefined
        }
        // T-1170: the persona body is NOT on the roster row any more, so the
        // page's own read supplies it. `null` until it lands — DocCard already
        // treats that as "not loaded" (edit stays disabled) rather than as an
        // empty document, which is what stops 完成編輯 writing a blank over a
        // real definition.
        doc={
          roleDoc.role
            ? {
                text: roleDoc.role.definitionMd,
                isDefault: roleDoc.role.isDefault,
              }
            : null
        }
        // 預設 comes off the ROSTER ROW, not off the document. The roster has
        // carried `is_default` all along and it answers on its own request, so
        // this stays true through the window where `getRole` is in flight or
        // has failed — which is exactly the window in which reading it off the
        // (absent) document badged an owner-edited role as shipped-default.
        isDefault={role?.isDefault}
        // The roster answering while THIS read failed is a new state (they are
        // two requests now), and it has to say so instead of showing an empty
        // card under a real role's title.
        errorNote={
          roleDoc.error ? (
            <div className="set-error" data-testid="role-doc-load-error">
              {t.settings.loadError}
            </div>
          ) : roleDoc.loading ? (
            // Loading and "this document is empty" are different screens, and
            // an empty <Markdown> is indistinguishable from the second. The
            // manuals page draws no card at all until the body lands
            // (TaskManualsPage's `{manual && …}`); this page keeps its card
            // because the title, the breadcrumb and 版本紀錄 are all readable
            // without the body — so it says which state it is in instead.
            <div className="doc-card__note" data-testid="role-doc-loading">
              {t.settings.historyLoading}
            </div>
          ) : undefined
        }
        crumbs={[crumbRoot, crumbRoles, { label: roleTitle }]}
        // The Duty doc has had a cap since T-ae38, and this is the only place
        // an owner or an agent sees how close it is to it. Omitted while the
        // role has not loaded — an invented 0/0 would read as a real budget.
        //
        // ⚠️ ALSO omitted while the DOCUMENT has not landed, even though the
        // roster row (which carries both numbers) has. The two facts come from
        // two requests now, and printing 「310 / 1000」 above a body that is
        // blank because its own read failed puts two statements on screen that
        // contradict each other — and the reader has no way to tell which one
        // is the broken half.
        usage={
          role && roleDoc.role
            ? { size: role.sizeChars, cap: role.capChars }
            : undefined
        }
        // Adopt the write echo: this page is no longer the roster's array, so
        // nothing else would put the saved text back on screen until an SSE
        // frame arrived — and a save must not depend on the stream being up.
        onSave={async (text) => {
          roleDoc.adopt(await rolesH.save(view.key, { definitionMd: text }));
        }}
        // 重置 = "restore the FILE SEED" — only a seed role has one. A custom
        // role's doc IS its only truth (the server 404s its reset — verified
        // live), so the affordance is omitted rather than left half-dead: on a
        // seed role it becomes the list's 初始版本 row, on a custom one there
        // is no such row at all.
        onReset={
          role?.isSeed
            ? async () => {
                roleDoc.adopt(await rolesH.reset(view.key));
              }
            : undefined
        }
        history={{
          kind: "role_definition",
          docKey: view.key,
          // This page carries TWO versioned documents (the definition and,
          // below it, the lessons), so a list headed plain 「版本紀錄」 cannot
          // say which one it holds.
          title: t.settings.historyRoleDefTitle,
          // The definition text alone: the role's name is not versioned
          // (owner ruling 2026-07-31), so putting it on this side would make a
          // rename show up as a difference nothing can restore.
          currentContent: roleDoc.role
            ? { definition_md: roleDoc.role.definitionMd }
            : undefined,
          // Only a CUSTOM role has a delete affordance (seed roles are
          // server-refused), so only there is the scope note true.
          docDeletable: role ? !role.isSeed : false,
          onRestored: async () => {
            // A restore rewrites the LIVE document, so the page re-reads its
            // own doc; the roster follows for the size it shows.
            await roleDoc.refetch();
            await rolesH.refetch();
          },
        }}
        // Duty (the role_def card above) → Insight → Learning: the three
        // blocks of the role journal, in the order the owner ruled on
        // 2026-08-03 — Insight is what the role decided it believes, so it sits
        // directly under the duty it interprets, and Learning (the longest,
        // most append-only of the three) closes the page. `extra` is a
        // ReactNode, so a fragment needs no prop or type change here.
        extra={
          <>
            <InsightCard roleKey={view.key} />
            <LessonsCard roleKey={view.key} />
          </>
        }
      />
    );
  }

  // ── landing ──
  // The root page follows the same pattern as every sub-page (T-8f6e): a
  // breadcrumb on top — here just the single 「設定」 current segment — with
  // the page title directly below.
  return (
    <div className="settings">
      <Breadcrumbs items={[{ label: t.settings.title }]} />
      <h1 className="settings__title">{t.settings.title}</h1>
      <div className="set-entries">
        <button
          type="button"
          className="set-entry"
          onClick={() => setView({ kind: "software" })}
        >
          <span className="set-entry__icon set-entry__icon--neutral">
            <DownloadIcon size={18} />
          </span>
          <span className="set-entry__name">{t.settings.software}</span>
          <ChevronRightIcon size={18} className="set-entry__chev" />
        </button>
        <button
          type="button"
          className="set-entry"
          onClick={() => setView({ kind: "roles" })}
        >
          <span className="set-entry__icon set-entry__icon--violet">
            <UsersIcon size={18} />
          </span>
          <span className="set-entry__name">{t.settings.roles}</span>
          <ChevronRightIcon size={18} className="set-entry__chev" />
        </button>
        {/* 任務手冊 (SPEC §5) — 與角色誌並列: the task-type / playbook
         * definition surface. */}
        <button
          type="button"
          className="set-entry"
          data-testid="settings-manuals-entry"
          onClick={() => setView({ kind: "manuals" })}
        >
          <span className="set-entry__icon set-entry__icon--blue">
            <FileTextIcon size={18} />
          </span>
          <span className="set-entry__name">{t.settings.manuals}</span>
          <ChevronRightIcon size={18} className="set-entry__chev" />
        </button>
        {/* 參數調整 — the owner-tunable server knobs (登入有效期 / 自動換手門檻).
         * They used to live in the profile dropdown's 偏好設定 sub-view; owner
         * 2026-07-12 pulled them here so PARAMETERS live together in 設定 and the
         * avatar menu keeps only appearance + account identity (主題/語言/密碼). */}
        <button
          type="button"
          className="set-entry"
          data-testid="settings-params-entry"
          onClick={() => setView({ kind: "params" })}
        >
          <span className="set-entry__icon set-entry__icon--blue">
            <GearIcon size={18} />
          </span>
          <span className="set-entry__name">{t.settings.params}</span>
          <ChevronRightIcon size={18} className="set-entry__chev" />
        </button>
        {/* 主題 — theme MANAGEMENT (T-16a1 P3b): add / edit colours (friendly,
         * grouped) / 用詞 overlay / import / export / delete. Moved here from the
         * profile dropdown, which keeps only the theme SELECTOR + language
         * (owner IA: 偏好=選擇, 設定=管理). */}
        <button
          type="button"
          className="set-entry"
          data-testid="settings-theme-entry"
          onClick={() => setView({ kind: "theme" })}
        >
          <span className="set-entry__icon set-entry__icon--violet">
            <MoonIcon size={18} />
          </span>
          <span className="set-entry__name">{t.settings.themeManage}</span>
          <ChevronRightIcon size={18} className="set-entry__chev" />
        </button>
        {/* 使用說明 is NOT here any more — it is a top-level nav tab, to the
         * right of 監控 (owner 2026-07-22:「user guide 改放在 tab 中,監控的
         * 右邊,不要放在 settings 裡」). See components/UserGuidePage.tsx. */}
      </div>
    </div>
  );
}

// ── 參數調整 ────────────────────────────────────────────────────────────────
/** The login-TTL choices — the server-side whitelist verbatim
 * (SettingsUpdateDTO: 12h / 24h / 7d / 30d). */
const TTL_CHOICES = [43200, 86400, 604800, 2592000] as const;

/**
 * 參數調整 — 登入與 agent 有效期 + 自動換手門檻, both durable
 * and live immediately (PATCH echoes the effective values back). Honest states:
 * a REJECTED load renders the error line instead of a fabricated form, and an
 * out-of-range / rejected write snaps the field back to the last server-confirmed
 * value rather than leaving a lie on screen.
 */
function ServerParams({
  settings,
  error,
  saveError,
  onSave,
  onClearSaveError,
  crumbs,
}: {
  settings: ServerSettingsView | null;
  error: boolean;
  saveError: boolean;
  onSave: (patch: ServerSettingsPatch) => Promise<void>;
  onClearSaveError: () => void;
  crumbs: Crumb[];
}) {
  const { t } = useI18n();

  // The % field is a free-text draft until blur/Enter — committing on every
  // keystroke would PATCH the server mid-typing ("5" on the way to "50").
  const [handoverDraft, setHandoverDraft] = useState<string | null>(null);
  const [codexHandoverDraft, setCodexHandoverDraft] = useState<string | null>(null);
  const [noticeDraft, setNoticeDraft] = useState<string | null>(null);
  const [codexNoticeDraft, setCodexNoticeDraft] = useState<string | null>(null);
  const [monitoringRefreshDraft, setMonitoringRefreshDraft] = useState<string | null>(null);
  // T-ae38, widened by T-30f1: five independent caps, so five independent
  // drafts. A shared draft would make typing in one field snap the others back.
  const [docCapDrafts, setDocCapDrafts] = useState<
    Partial<Record<DocCapField, string>>
  >({});
  const [chatBudgetDraft, setChatBudgetDraft] = useState<string | null>(null);
  const [rangeError, setRangeError] = useState(false);

  const ttlLabel: Record<number, string> = {
    43200: t.settings.ttl12h,
    86400: t.settings.ttl24h,
    604800: t.settings.ttl7d,
    2592000: t.settings.ttl30d,
  };

  function commitHandover() {
    if (!settings) return;
    if (handoverDraft === null) return;
    const n = Number(handoverDraft);
    if (!Number.isInteger(n) || n < 40 || n > 90 || n <= settings.noticePct) {
      // Local guard mirrors the server's 422 range AND the pair order — snap
      // back, mark it.
      setRangeError(true);
      setHandoverDraft(null);
      return;
    }
    setHandoverDraft(null);
    if (n === settings.handoverPct) return;
    void onSave({ handoverPct: n });
  }

  // The two offboard points are a PAIR, so each local guard checks the ORDER as
  // well as the range — the server refuses a crossing pair with a 422, and
  // letting the input accept it here would show a saved value the server never
  // took. Checked against the OTHER field's live value, so either box can be
  // edited on its own.
  function commitNotice() {
    if (!settings || noticeDraft === null) return;
    const n = Number(noticeDraft);
    if (!Number.isInteger(n) || n < 1 || n > 89 || n >= settings.handoverPct) {
      setRangeError(true);
      setNoticeDraft(null);
      return;
    }
    setNoticeDraft(null);
    if (n !== settings.noticePct) void onSave({ noticePct: n });
  }

  function commitCodexNotice() {
    if (!settings || codexNoticeDraft === null) return;
    const n = Number(codexNoticeDraft);
    if (!Number.isInteger(n) || n < 1 || n > 10 || n >= settings.codexCompactionThreshold) {
      setRangeError(true);
      setCodexNoticeDraft(null);
      return;
    }
    setCodexNoticeDraft(null);
    if (n !== settings.codexNoticeRound) void onSave({ codexNoticeRound: n });
  }

  function commitCodexHandover() {
    if (!settings || codexHandoverDraft === null) return;
    const n = Number(codexHandoverDraft);
    if (!Number.isInteger(n) || n < 1 || n > 10 || n <= settings.codexNoticeRound) {
      setRangeError(true); setCodexHandoverDraft(null); return;
    }
    setCodexHandoverDraft(null);
    if (n !== settings.codexCompactionThreshold) void onSave({ codexCompactionThreshold: n });
  }

  function commitMonitoringRefresh() {
    if (!settings || monitoringRefreshDraft === null) return;
    const n = Number(monitoringRefreshDraft);
    if (!Number.isInteger(n) || n < 1 || n > 60) { setRangeError(true); setMonitoringRefreshDraft(null); return; }
    setMonitoringRefreshDraft(null);
    if (n !== settings.monitoringRefreshSeconds) void onSave({ monitoringRefreshSeconds: n });
  }

  // Each floor is THAT segment's shipped default, so a knob only raises its own
  // cap (owner 2026-07-31; five of them since T-30f1) — the local guard mirrors
  // the server's 422 range exactly, INCLUDING Duty's own smaller floor. Reusing
  // the other four's floor here would locally reject the shipped Duty default.
  // T-c9b4: the wake snapshot's chat budget. Deliberately its OWN row and its
  // own commit rather than a sixth entry in DOC_CAP_FIELDS — that table's floor
  // is each segment's shipped default and its ceiling is 100000, and neither is
  // true here. This one may be turned DOWN (the block is repacked on every read,
  // so a smaller budget just returns fewer messages), and its ceiling is pinned
  // to how many messages the server reads before packing.
  function commitChatBudget() {
    if (!settings || chatBudgetDraft === null) return;
    const n = Number(chatBudgetDraft);
    if (
      !Number.isInteger(n) ||
      n < CHAT_BUDGET_CHARS_MIN ||
      n > CHAT_BUDGET_CHARS_MAX
    ) {
      setRangeError(true);
      setChatBudgetDraft(null);
      return;
    }
    setChatBudgetDraft(null);
    if (n !== settings.chatBudgetChars) void onSave({ chatBudgetChars: n });
  }

  function commitDocCap(field: DocCapField) {
    const draft = docCapDrafts[field];
    if (!settings || draft === undefined) return;
    const n = Number(draft);
    const clear = () =>
      setDocCapDrafts((d) => {
        const next = { ...d };
        delete next[field];
        return next;
      });
    if (!Number.isInteger(n) || n < DOC_CAP_FIELDS[field].min || n > 100000) {
      setRangeError(true);
      clear();
      return;
    }
    clear();
    if (n !== settings[field]) void onSave({ [field]: n });
  }

  return (
    <div className="settings">
      <Breadcrumbs items={crumbs} />
      <h1 className="settings__title settings__title--doc">
        {t.settings.params}
      </h1>

      {/* Honest load-failure notice — never a fabricated form over a dead fetch. */}
      {error && <div className="set-error">{t.settings.paramsLoadError}</div>}

      {settings && (
        <div className="param-card">
          <div className="param-row">
            <div className="param-row__body">
              <label className="param-row__name" htmlFor="param-ttl">
                {t.settings.sessionTtl}
              </label>
              <div className="param-row__sub">{t.settings.sessionTtlSub}</div>
            </div>
            <select
              id="param-ttl"
              className="param-select"
              aria-label={t.settings.sessionTtl}
              value={settings.ownerTokenTtl}
              onChange={(e) => {
                setRangeError(false);
                void onSave({ ownerTokenTtl: Number(e.target.value) });
              }}
            >
              {TTL_CHOICES.map((secs) => (
                <option key={secs} value={secs}>
                  {ttlLabel[secs]}
                </option>
              ))}
            </select>
          </div>

          <div className="param-row">
            <div className="param-row__body">
              <label className="param-row__name" htmlFor="param-agent-ttl">{t.settings.agentTokenTtl}</label>
              <div className="param-row__sub">{t.settings.agentTokenTtlSub}</div>
            </div>
            <select id="param-agent-ttl" className="param-select" aria-label={t.settings.agentTokenTtl}
              value={settings.agentTokenTtl}
              onChange={(e) => { setRangeError(false); void onSave({ agentTokenTtl: Number(e.target.value) }); }}>
              {TTL_CHOICES.map((secs) => <option key={secs} value={secs}>{ttlLabel[secs]}</option>)}
            </select>
          </div>

          <div className="param-row">
            <div className="param-row__body">
              <label className="param-row__name" htmlFor="param-notice">
                {t.settings.notice}
              </label>
              <div className="param-row__sub">{t.settings.noticeSub}</div>
            </div>
            <div className="param-pct">
              <input
                id="param-notice"
                className="param-input"
                type="number"
                min={1}
                max={89}
                aria-label={t.settings.notice}
                value={noticeDraft ?? String(settings.noticePct)}
                onChange={(e) => {
                  setRangeError(false);
                  onClearSaveError();
                  setNoticeDraft(e.target.value);
                }}
                onBlur={commitNotice}
                onKeyDown={(e) => {
                  if (e.key === "Enter") commitNotice();
                }}
              />
              <span className="param-pct__sign">%</span>
            </div>
          </div>

          <div className="param-row">
            <div className="param-row__body">
              <label className="param-row__name" htmlFor="param-handover">
                {t.settings.handover}
              </label>
              <div className="param-row__sub">{t.settings.handoverSub}</div>
            </div>
            <div className="param-pct">
              <input
                id="param-handover"
                className="param-input"
                type="number"
                min={40}
                max={90}
                aria-label={t.settings.handover}
                value={handoverDraft ?? String(settings.handoverPct)}
                onChange={(e) => {
                  setRangeError(false);
                  onClearSaveError();
                  setHandoverDraft(e.target.value);
                }}
                onBlur={commitHandover}
                onKeyDown={(e) => {
                  if (e.key === "Enter") commitHandover();
                }}
              />
              <span className="param-pct__sign">%</span>
            </div>
          </div>

          <div className="param-row">
            <div className="param-row__body">
              <label className="param-row__name" htmlFor="param-codex-notice">
                {t.settings.codexNotice}
              </label>
              <div className="param-row__sub">{t.settings.codexNoticeSub}</div>
            </div>
            <div className="param-pct">
              <input
                id="param-codex-notice"
                className="param-input"
                type="number"
                min={1}
                max={10}
                aria-label={t.settings.codexNotice}
                value={codexNoticeDraft ?? String(settings.codexNoticeRound)}
                onChange={(e) => { setRangeError(false); onClearSaveError(); setCodexNoticeDraft(e.target.value); }}
                onBlur={commitCodexNotice}
                onKeyDown={(e) => { if (e.key === "Enter") commitCodexNotice(); }}
              />
            </div>
          </div>

          <div className="param-row">
            <div className="param-row__body">
              <div className="param-row__name">{t.settings.codexHandover}</div>
              <div className="param-row__sub">{t.settings.codexHandoverSub}</div>
            </div>
            <div className="param-pct">
              <input
                id="param-codex-handover"
                className="param-input"
                type="number"
                min={1}
                max={10}
                aria-label={t.settings.codexHandover}
                value={codexHandoverDraft ?? String(settings.codexCompactionThreshold)}
                onChange={(e) => { setRangeError(false); onClearSaveError(); setCodexHandoverDraft(e.target.value); }}
                onBlur={commitCodexHandover}
                onKeyDown={(e) => { if (e.key === "Enter") commitCodexHandover(); }}
              />
              <span className="param-pct__sign">次</span>
            </div>
          </div>

          <div className="param-row">
            <div className="param-row__body">
              <div className="param-row__name">{t.settings.monitoringRefresh}</div>
              <div className="param-row__sub">{t.settings.monitoringRefreshSub}</div>
            </div>
            <div className="param-pct">
              <input id="param-monitoring-refresh" className="param-input" type="number" min={1} max={60}
                aria-label={t.settings.monitoringRefresh}
                value={monitoringRefreshDraft ?? String(settings.monitoringRefreshSeconds)}
                onChange={(e) => { setRangeError(false); onClearSaveError(); setMonitoringRefreshDraft(e.target.value); }}
                onBlur={commitMonitoringRefresh} onKeyDown={(e) => { if (e.key === "Enter") commitMonitoringRefresh(); }} />
              <span className="param-pct__sign">{t.settings.seconds}</span>
            </div>
          </div>

          {DOC_CAP_ORDER.map((field) => {
            const spec = DOC_CAP_FIELDS[field];
            const label = t.settings[spec.labelKey];
            return (
              <div className="param-row" key={field}>
                <div className="param-row__body">
                  <div className="param-row__name">{label}</div>
                  <div className="param-row__sub">{t.settings[spec.subKey]}</div>
                </div>
                <div className="param-pct">
                  <input id={spec.inputId} className="param-input" type="number" min={spec.min} max={100000}
                    aria-label={label}
                    value={docCapDrafts[field] ?? String(settings[field])}
                    onChange={(e) => { setRangeError(false); onClearSaveError(); setDocCapDrafts((d) => ({ ...d, [field]: e.target.value })); }}
                    onBlur={() => commitDocCap(field)} onKeyDown={(e) => { if (e.key === "Enter") commitDocCap(field); }} />
                  <span className="param-pct__sign">{t.settings.chars}</span>
                </div>
              </div>
            );
          })}

          <div className="param-row">
            <div className="param-row__body">
              <div className="param-row__name">{t.settings.chatBudget}</div>
              <div className="param-row__sub">{t.settings.chatBudgetSub}</div>
            </div>
            <div className="param-pct">
              <input id="param-chat-budget" className="param-input" type="number"
                min={CHAT_BUDGET_CHARS_MIN} max={CHAT_BUDGET_CHARS_MAX}
                aria-label={t.settings.chatBudget}
                value={chatBudgetDraft ?? String(settings.chatBudgetChars)}
                onChange={(e) => { setRangeError(false); onClearSaveError(); setChatBudgetDraft(e.target.value); }}
                onBlur={commitChatBudget} onKeyDown={(e) => { if (e.key === "Enter") commitChatBudget(); }} />
              <span className="param-pct__sign">{t.settings.chars}</span>
            </div>
          </div>

          {(saveError || rangeError) && (
            <div className="set-error param-error">
              {t.settings.paramsSaveError}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ── 系統更新與備份 ────────────────────────────────────────────────────────────────

/**
 * HONEST software-update card (GitHub Releases, t-dc68). The version headline
 * is the single human-facing identity: an OFFICIAL package carries its GitHub
 * Release tag in `version`; a self-build keeps the honest "0.0.0" and only
 * then does the composed build label v<yymmdd>-<hhmm>-<shortsha> (git sha +
 * commit time) stand in. `update_available` / `latest_version` mirror the
 * server's cached GitHub Releases check; the 升級 button renders ONLY when a
 * real newer release exists. 檢查更新 is the owner's explicit fresh check
 * (GET /api/release/check): up_to_date / update_available (tag + release
 * link) / unknown (GitHub unreachable — the honest degraded verdict).
 * Clicking 升級 is the owner's EXPLICIT trigger (POST /api/update/upgrade);
 * a rejection surfaces the server's own message (409 preconditions / 502
 * download-verify-swap failures — the old build keeps serving). A 200 means
 * the verified swap already landed and the server is re-exec'ing: the card
 * shows the restart notice and polls /api/version until git_sha advances,
 * then reloads the page.
 *
 * Below the card sit the two software-update toggles (/api/settings knobs,
 * both default OFF): 接收 Beta (follow GitHub prereleases too) and 自動更新
 * (unattended background self-upgrade running the same verified body).
 */
function SoftwareUpdate({
  version,
  onRefreshVersion,
  settings,
  settingsError,
  saveError,
  onSave,
  onClearSaveError,
  crumbs,
}: {
  version: VersionView | null;
  onRefreshVersion: () => void;
  settings: ServerSettingsView | null;
  settingsError: boolean;
  saveError: boolean;
  onSave: (patch: ServerSettingsPatch) => Promise<void>;
  onClearSaveError: () => void;
  crumbs: Crumb[];
}) {
  const { t } = useI18n();

  // Explicit 檢查更新: idle → checking → the server's fresh verdict (kept
  // until the next click) / "failed" (transport/gate error — NOT the server's
  // honest "unknown", which arrives as a verdict).
  const [checkState, setCheckState] = useState<
    { kind: "idle" } | { kind: "checking" } | { kind: "failed" } | { kind: "done"; verdict: ReleaseCheckView }
  >({ kind: "idle" });
  // Read-back verdict for the verified auto_update commit (存檔測連通,
  // T-1c2e): idle → saving → ok/fail — "ok" means the write was read BACK and
  // matched, never a local echo.
  const [verifyStatus, setVerifyStatus] = useState<
    "idle" | "saving" | "ok" | "fail"
  >("idle");
  const [upgradeBusy, setUpgradeBusy] = useState(false);
  // The server's own message from a rejected upgrade (409 preconditions /
  // 502 download-or-verify failures) — honest, verbatim; "" = no error.
  const [upgradeError, setUpgradeError] = useState("");
  // True between a 200 {status:"restarting"} and the new build answering:
  // the swap already LANDED server-side and the process is re-exec'ing —
  // poll /api/version until git_sha moves, then reload the page (the SPA
  // ships inside the binary, so only a reload shows the new frontend).
  const [upgradeRestarting, setUpgradeRestarting] = useState(false);
  const preUpgradeSha = useRef<string | null>(null);
  const restartPoll = useRef<number | null>(null);
  const restartDeadline = useRef<number | null>(null);
  const refreshTimer = useRef<number | null>(null);

  // After an updater-settings save the server kicks its update check in the
  // background — re-read /api/version shortly after so the badge reflects the
  // new updater without a reload (best-effort; the TTL cadence self-heals).
  function scheduleVersionRefresh() {
    if (refreshTimer.current !== null) window.clearTimeout(refreshTimer.current);
    refreshTimer.current = window.setTimeout(onRefreshVersion, 1500);
  }
  useEffect(
    () => () => {
      if (refreshTimer.current !== null) window.clearTimeout(refreshTimer.current);
      stopRestartWatch();
    },
    []
  );

  function stopRestartWatch() {
    if (restartPoll.current !== null) window.clearInterval(restartPoll.current);
    if (restartDeadline.current !== null) window.clearTimeout(restartDeadline.current);
    restartPoll.current = null;
    restartDeadline.current = null;
  }

  // The restart watch's exit: the polled /api/version answers with a NEW
  // git_sha → the upgraded build is serving; reload to load its SPA.
  useEffect(() => {
    if (!upgradeRestarting || !version) return;
    if (preUpgradeSha.current !== null && version.gitSha !== preUpgradeSha.current) {
      window.location.reload();
    }
  }, [upgradeRestarting, version]);

  function runCheck() {
    setCheckState({ kind: "checking" });
    api
      .checkRelease()
      .then((verdict) => {
        setCheckState({ kind: "done", verdict });
        // The synchronous check also refreshed the server-side cache — re-read
        // /api/version so the badge follows without waiting out the TTL.
        scheduleVersionRefresh();
      })
      .catch(() => setCheckState({ kind: "failed" }));
  }

  /** The card's ONE status line. A live explicit check (checking / failed /
   * done) wins over the cached /api/version verdict; once it settles it just
   * REPLACES the badge's content. All three post-click states — 檢查中 /
   * 有新版 / 錯誤 — resolve here, so none of them can spawn a second line.
   * Only ever called with `version` non-null (the caller's guard). */
  function renderStatus(): ReactNode {
    if (checkState.kind === "checking")
      return (
        <span className="sw-badge sw-badge--busy">
          {t.settings.checkingUpdate}
        </span>
      );
    if (checkState.kind === "failed")
      return (
        <span className="sw-badge sw-badge--bad">{t.settings.checkFailed}</span>
      );
    if (checkState.kind === "done") {
      const v = checkState.verdict;
      // GitHub unreachable — the server's HONEST degraded verdict, not a
      // fabricated "up to date".
      if (v.status === "unknown")
        return (
          <span className="sw-badge sw-badge--bad">
            {t.settings.checkUnknown}
          </span>
        );
      if (v.status === "update_available")
        return (
          <span className="sw-badge sw-badge--new">
            {t.settings.updateAvailable}
            {v.latestTag ? ` ${v.latestTag}` : ""}
            {v.releaseUrl && (
              <>
                {" · "}
                <a href={v.releaseUrl} target="_blank" rel="noreferrer">
                  {t.settings.viewRelease}
                </a>
              </>
            )}
          </span>
        );
      return (
        <span className="sw-badge sw-badge--ok">{t.settings.upToDate}</span>
      );
    }
    // idle: the cached verdict that came with /api/version.
    return version?.updateAvailable ? (
      <span className="sw-badge sw-badge--new">
        {t.settings.updateAvailable}
        {version.latestVersion ? ` ${version.latestVersion}` : ""}
      </span>
    ) : (
      <span className="sw-badge sw-badge--ok">{t.settings.upToDate}</span>
    );
  }

  /** Does a REAL newer release exist right now? The single source both the
   * status badge and the 升級 button answer to. Precedence mirrors
   * renderStatus() exactly: a settled explicit check is FRESHER than the
   * cached /api/version flag, so it wins outright — including when it says
   * "no" (a stale-positive cache must not offer an upgrade the server would
   * reject with 409). While a check is in flight, or when it failed / came
   * back `unknown`, there is no fresh answer to trust, so the cached flag
   * stands: an unreachable GitHub must not retract a known-good offer. */
  const upgradeOffered =
    checkState.kind === "done" && checkState.verdict.status !== "unknown"
      ? checkState.verdict.status === "update_available"
      : !!version?.updateAvailable;

  // Toggle writes go straight through (no draft: a switch IS its commit);
  // flipping the channel re-kicks the server-side check, so re-read the
  // version shortly after — same best-effort refresh as the URL/code saves.
  function commitToggle(patch: ServerSettingsPatch) {
    onClearSaveError();
    setVerifyStatus("idle");
    void onSave(patch).then(scheduleVersionRefresh);
  }

  /** Commit a patch, then verify it landed by reading the settings BACK and
   * comparing (存檔測連通 — T-1c2e's core honesty rule, migrated here from
   * the retired 伺服器設定 view). `onSave` never rejects (the hook folds
   * failures into saveError), so the fresh GET is the single truth test: an
   * unreachable server OR a rejected write both read back as "not what I
   * wrote" → fail, and the switch stays on the server-confirmed value. */
  async function commitVerified(
    patch: ServerSettingsPatch,
    landed: (echo: ServerSettingsView) => boolean
  ) {
    setVerifyStatus("saving");
    await onSave(patch);
    // The verdict line below is this commit's single feedback — clear the
    // hook's generic saveError so a rejected write isn't double-reported.
    onClearSaveError();
    try {
      // A REAL read, never the shared cache: proving the server agrees is the
      // entire point of 存檔測連通 (T-8115).
      const echo = await refreshServerSettings();
      setVerifyStatus(landed(echo) ? "ok" : "fail");
    } catch {
      setVerifyStatus("fail");
    }
    scheduleVersionRefresh();
  }

  function triggerUpgrade() {
    setUpgradeBusy(true);
    setUpgradeError("");
    api
      .triggerUpgrade()
      .then(() => {
        // 200 {status:"restarting"}: the verified binary swap has already
        // landed and the server is re-exec'ing itself. Watch /api/version
        // until git_sha advances (reload then), bounded by a deadline so a
        // restart that never comes back reads as an honest failure.
        preUpgradeSha.current = version?.gitSha ?? null;
        setUpgradeRestarting(true);
        restartPoll.current = window.setInterval(onRefreshVersion, 2000);
        restartDeadline.current = window.setTimeout(() => {
          stopRestartWatch();
          setUpgradeRestarting(false);
          setUpgradeBusy(false);
          setUpgradeError(t.settings.upgradeTimeout);
        }, 90_000);
      })
      .catch((e) => {
        // Surface the server's own honest message (409 preconditions / 502
        // download-verify-swap failures — the old build keeps serving);
        // fall back to the generic line.
        const msg =
          e instanceof ApiError && e.serverMessage
            ? e.serverMessage
            : t.settings.upgradeFailed;
        setUpgradeError(msg);
        setUpgradeBusy(false);
      });
  }

  return (
    <div className="settings">
      <Breadcrumbs items={crumbs} />
      <h1 className="settings__title settings__title--doc">
        {t.settings.software}
      </h1>
      <div className="sw-card">
        <div className="sw-card__main">
          <div className="sw-card__label">{t.settings.currentVersion}</div>
          {version ? (
            <>
              {/* ONE unified version label (t-dc68): the GitHub Release tag
               * when this is an official package (version ≠ "0.0.0"); a
               * self-build falls back to the composed build label
               * v<yymmdd>-<hhmm>-<shortsha> from git sha + commit time. */}
              <div className="sw-build">
                {/* ── the version row (owner round-2, 2026-07-20) ──
                 * The refresh control is PINNED to the version-number line,
                 * not to the status chip's line. Why: the status text is
                 * variable-length (已是最新版 / 有可用的新版本 vX · 查看
                 * release / the two-clause error sentences), so anchoring the
                 * icon to it let a long verdict wrap the icon down to a second
                 * row, orphaned at the bottom-left. The version number is a
                 * short, bounded string, so this row's geometry is stable
                 * across every state.
                 *
                 * MEASURED on the pre-fix layout (real Chromium), so the next
                 * reader inherits the numbers rather than a guess: the state
                 * that actually orphans the icon is `update_available` with a
                 * long release tag (v0.9.9-rc.4+build.20260720.arm64) —
                 * WRAPPED=true at 320/360/375/390/414px alike, icon landing at
                 * x=21. `unknown` does NOT wrap at 375px (badge width 262.2,
                 * icon still inline at x=291.2); it only wraps at 320px. */}
                <div className="sw-build__headline">
                  <code className="sw-build__version">
                    {version.version !== "0.0.0"
                      ? version.version
                      : formatBuildVersion(version.gitSha, version.gitTime)}
                  </code>
                  <button
                    type="button"
                    className="sw-refresh"
                    disabled={checkState.kind === "checking"}
                    onClick={runCheck}
                    data-testid="settings-check-release"
                  >
                    {/* Icon-only button: the accessible name comes from real
                     * (visually clipped) text content, NOT aria-label — this
                     * repo has been bitten by aria-label REPLACING an element's
                     * name. The svg is hidden so the name is exactly the text. */}
                    <span className="sw-refresh__icon" aria-hidden="true">
                      <RefreshIcon size={15} />
                    </span>
                    <span className="sw-refresh__label">
                      {t.settings.checkUpdate}
                    </span>
                  </button>
                </div>
              </div>
              {/* ── THE status line (owner 2026-07-20 verdict on T-dc68) ──
               * One row, one truth. The cached /api/version verdict and the
               * explicit fresh check share this single node: clicking the
               * refresh icon mutates THIS badge in place (checking → verdict)
               * instead of appending a second result line below the card,
               * which is what put "已是最新版" on screen twice. */}
              <div className="sw-status">
                <span className="sw-status__badge" data-testid="settings-update-status">
                  {renderStatus()}
                </span>
              </div>
            </>
          ) : (
            <div className="sw-build__time">{t.mp.dash}</div>
          )}
        </div>
        {/* Upgrade button appears ONLY when a real newer version exists —
         * and only ever fires on the owner's click (no auto-upgrade).
         * SAME SOURCE as the status badge (owner round-2 item ③): once an
         * explicit check has answered, ITS fresh verdict decides, exactly as
         * renderStatus() does. Gating on the cached /api/version flag alone
         * produced the split screen owner saw — the badge saying 有可用的新版本
         * while no 升級 button existed, because the cache had not yet caught
         * up (the frontend only re-reads /api/version 1.5s later, and that
         * re-read can silently fail). The fresh verdict also correctly HIDES
         * the button when the cache is stale-positive but GitHub now says up
         * to date — a button that would only ever 409. */}
        {upgradeOffered && (
          <button
            type="button"
            className="btn btn--accent sw-upgrade"
            disabled={upgradeBusy}
            onClick={triggerUpgrade}
          >
            {t.settings.upgrade}
          </button>
        )}
      </div>
      {upgradeRestarting && (
        <div className="sw-restarting">{t.settings.upgradeRestarting}</div>
      )}
      {upgradeError && (
        <div className="set-error param-error">{upgradeError}</div>
      )}

      {/* NOTE (T-dc68 fixup): there is deliberately NO separate 檢查更新 row
       * here any more. The check's result is rendered by renderStatus() into
       * the card's single status line above — no second result element is
       * ever created. */}

      {/* ── the two software-update toggles (/api/settings; both default OFF) ── */}
      <h2 className="settings__title settings__title--doc">
        {t.settings.updateSettings}
      </h2>
      {settingsError && (
        <div className="set-error">{t.settings.paramsLoadError}</div>
      )}
      {settings && (
        <div className="param-card">
          {/* ── the two dual-channel toggles (both default OFF) ── */}
          <div className="param-row">
            <div className="param-row__body">
              <span className="param-row__name">{t.settings.receiveBeta}</span>
              <div className="param-row__sub">{t.settings.receiveBetaSub}</div>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={settings.updaterReceiveBeta}
              aria-label={t.settings.receiveBeta}
              className={`set-toggle${settings.updaterReceiveBeta ? " set-toggle--on" : ""}`}
              onClick={() =>
                commitToggle({ updaterReceiveBeta: !settings.updaterReceiveBeta })
              }
              data-testid="settings-receive-beta"
            >
              <span className="set-toggle__knob" />
            </button>
          </div>
          <div className="param-row">
            <div className="param-row__body">
              <span className="param-row__name">{t.settings.autoUpdate}</span>
              <div className="param-row__sub">{t.settings.autoUpdateSub}</div>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={settings.updaterAutoUpdate}
              aria-label={t.settings.autoUpdate}
              className={`set-toggle${settings.updaterAutoUpdate ? " set-toggle--on" : ""}`}
              onClick={() => {
                // Verified commit (存檔測連通): PATCH, then re-GET and compare
                // — the verdict reports what the server actually stored.
                onClearSaveError();
                const next = !settings.updaterAutoUpdate;
                void commitVerified(
                  { updaterAutoUpdate: next },
                  (echo) => echo.updaterAutoUpdate === next
                );
              }}
              data-testid="settings-auto-update"
            >
              <span className="set-toggle__knob" />
            </button>
          </div>
          {saveError && (
            <div className="set-error param-error">
              {t.settings.paramsSaveError}
            </div>
          )}
          {/* the read-back verdict for the verified commit, one line, honest */}
          {verifyStatus === "saving" && (
            <div className="config-status config-status--saving">
              {t.settings.configSaving}
            </div>
          )}
          {verifyStatus === "ok" && (
            <div className="config-status config-status--ok">
              {t.settings.configSaved}
            </div>
          )}
          {verifyStatus === "fail" && (
            <div className="set-error param-error">
              {t.settings.configSaveFailed}
            </div>
          )}
        </div>
      )}

      {/* ── 備份健康 (T-5e71, owner 2026-08-02) ── the backup verdict lives
          HERE, under 系統更新與備份, and nowhere else: it used to sit on the
          monitor page plus a topbar light, which the owner did not want. */}
      <BackupHealthCard />
    </div>
  );
}

/**
 * 備份健康 block inside 系統更新與備份 (T-5e71) — the only surface that says
 * whether the scheduled backup is still producing retreat points, and the only
 * place that explains WHY it is not.
 *
 * Wording rules carried over from the monitor-page card it replaces:
 *  - The primary sentence comes from the closed `code` vocabulary via i18n
 *    (`backupReasonText`), never from the server's `detail`.
 *  - `detail` IS shown — clearly labelled as the server's own diagnostic. It is
 *    English engineer-facing text, so it is secondary, never the headline.
 *  - `unknown` (and a failed load, and a load still in flight) renders muted,
 *    never green. A retreat point we cannot see must not look like one we have.
 */
function BackupHealthCard() {
  const { t } = useI18n();
  const { health, loading, error } = useBackupHealth();
  const d = t.backupHealth;
  const state = backupIndicatorState(health, error);
  const reason = backupReasonText(d, health, error);

  // Elapsed since the incident opened, read off the render clock. The incident
  // outlives a server restart (the server keeps `since_ts`), so a backup broken
  // for three days still says three days on day three.
  const nowSecs = Math.floor(Date.now() / 1000);
  const sinceSecs =
    health?.sinceTs != null ? Math.max(0, nowSecs - health.sinceTs) : null;

  return (
    <>
      <h2 className="settings__title settings__title--doc">{d.title}</h2>
      <div className="param-card" data-testid="set-backup-health">
        {loading && !health && !error ? (
          <div className="sw-backup sw-backup__reason">{d.loading}</div>
        ) : (
          <div className="sw-backup">
            <div
              className={`sw-backup__status sw-backup__status--${state}`}
              data-testid="set-backup-status"
              data-backup-state={state}
            >
              {backupStatusLabel(d, state)}
            </div>
            {/* Empty only when healthy — a healthy backup has no failure to
                explain, and filler text there would dilute the red case. */}
            {reason !== "" && (
              <div className="sw-backup__reason" data-testid="set-backup-reason">
                {reason}
              </div>
            )}
            <div className="sw-backup__facts">
              <div className="sw-backup__row">
                <span className="sw-backup__label">{d.newestLabel}</span>
                <span className="sw-backup__value" data-testid="set-backup-newest">
                  {/* "Never" is a fact, not a missing value — it is precisely
                      the never_ran alarm, so it gets words rather than a
                      dash. */}
                  {health && health.newestBackupAgeSecs != null
                    ? `${formatDuration(health.newestBackupAgeSecs)} ${d.ago}`
                    : health
                      ? d.newestNever
                      : "—"}
                </span>
              </div>
              {sinceSecs != null && (
                <div className="sw-backup__row">
                  <span className="sw-backup__label">{d.sinceLabel}</span>
                  <span className="sw-backup__value" data-testid="set-backup-since">
                    {formatDuration(sinceSecs)}
                  </span>
                </div>
              )}
              {health && (
                <div className="sw-backup__row">
                  <span className="sw-backup__label">{d.staleAfterLabel}</span>
                  <span className="sw-backup__value">
                    {formatDuration(health.staleAfterSecs)}
                  </span>
                </div>
              )}
              {health && health.detail !== "" && (
                <div className="sw-backup__row">
                  <span className="sw-backup__label">{d.detailLabel}</span>
                  <span
                    className="sw-backup__value sw-backup__value--code"
                    data-testid="set-backup-detail"
                  >
                    {health.detail}
                  </span>
                </div>
              )}
            </div>
          </div>
        )}
      </div>
    </>
  );
}

// ── 角色誌 (list) ───────────────────────────────────────────────────────────
// `firstBodyLine` lived here — the roster row's one-line persona preview. It
// went with T-1170: the roster answer no longer carries `definition_md`, so
// there is no text to take a first line from, and the only way to keep the
// preview would be one document fetch per row, which is the download the
// directory exists to stop. Deleted rather than left dead: a helper with no
// caller reads exactly like a live one.

function RolesLog({
  roles,
  error,
  crumbs,
  onOpenSystem,
  onOpenCustom,
  onOpenBoot,
  onOpenOffboard,
  onOpenRole,
  onCreate,
  onDelete,
  autoCreate,
}: {
  roles: RoleSummaryView[];
  error: boolean;
  crumbs: Crumb[];
  onOpenSystem: () => void;
  onOpenCustom: () => void;
  /** 🔴 Takes the RUNTIME. A no-argument opener would have to pick one, and
   * whichever it picked would be the wrong document half the time — with no
   * way for the reader to tell, because the two pages look identical. */
  onOpenBoot: (runtime: "claude" | "codex") => void;
  /** 下線程序 (T-c9c0) — one document, so no runtime to pass. */
  onOpenOffboard: () => void;
  onOpenRole: (key: string) => void;
  onCreate: (input: { name: string }) => Promise<unknown>;
  onDelete: (key: string) => Promise<void>;
  /** #settings/roles/new deep-link: land with the inline 新增角色 create row
   * already open (T-25b7). One-shot — seeds the initial state only. */
  autoCreate?: boolean;
}) {
  const { t, msg } = useI18n();

  // ── 新增角色定義 — the INLINE create row (owner-aligned pattern): clicking
  // the add entry grows ONE editable row in the list with a single
  // 角色名 field (Enter/確認 creates, Esc/取消 collapses). The founding
  // member's name + model/effort are SERVER defaults now (隨機成員名 +
  // model=CLI default / effort=medium) — the create flow sends only {name}.
  const [adding, setAdding] = useState(!!autoCreate);
  const [roleName, setRoleName] = useState("");
  const [createBusy, setCreateBusy] = useState(false);
  const [createError, setCreateError] = useState(false);
  // IME composition guard (same rule as InlineEdit): an Enter that confirms a
  // CJK candidate must not submit the row.
  const composingRef = useRef(false);

  // T-25b7 owner feedback: the inline 新增角色 row sits at the BOTTOM of the
  // list, so on a #settings/roles/new deep-link (or the roster ➕👤) a long
  // role journal leaves the create row below the fold — autoFocus alone lands
  // an invisible field. Scroll the row into view whenever create mode is open.
  // Depending on roles.length re-fires the scroll AFTER the async role load
  // lands: the first mount renders an empty list (row in view), then the roles
  // arrive and push the row down, so the initial scroll must be redone once the
  // real list height exists — otherwise the load "eats" the scroll.
  const createRowRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (!adding) return;
    createRowRef.current?.scrollIntoView?.({
      block: "center",
      behavior: "smooth",
    });
  }, [adding, roles.length]);

  // ── 刪除 (custom roles only): centered confirm MODAL + honest error line ──
  const [confirmKey, setConfirmKey] = useState<string | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  function resetForm() {
    setAdding(false);
    setRoleName("");
    setCreateError(false);
  }

  async function submitCreate() {
    if (!roleName.trim()) {
      setCreateError(true);
      return;
    }
    setCreateBusy(true);
    setCreateError(false);
    try {
      await onCreate({ name: roleName.trim() });
      resetForm();
    } catch {
      setCreateError(true);
    } finally {
      setCreateBusy(false);
    }
  }

  async function confirmDelete(key: string) {
    setDeleteBusy(true);
    setDeleteError(null);
    try {
      await onDelete(key);
      setConfirmKey(null);
    } catch (e) {
      // 409 = a member of the role is still ONLINE (server-side 防線) — the
      // honest, actionable message; anything else is a generic failure.
      setDeleteError(
        isHttpStatus(e, 409) ? t.settings.deleteRoleOnline : t.settings.deleteRoleError
      );
    } finally {
      setDeleteBusy(false);
    }
  }

  return (
    <div className="settings">
      <Breadcrumbs items={crumbs} />
      <h1 className="settings__title settings__title--doc">
        {t.settings.roles}
      </h1>

      {/* Honest load-failure notice: a rejected fetch (500/network; 401 already
       * bounced to login) never masquerades as an empty role journal. */}
      {error && <div className="set-error">{t.settings.loadError}</div>}

      {/* zone 1: the global-context blocks, in boot-assembly order:
       * 系統互動 (heads the boot context) → 使用者自訂 (the additive block) →
       * 啟動程序, ONE row that opens a page holding both runtimes' documents.
       * All of them are editable since T-791e. No filenames — the blocks are
       * content, not files. */}
      <div className="set-group-label">{t.settings.globalSection}</div>
      <div className="set-entries">
        <button type="button" className="set-entry" onClick={onOpenSystem}>
          <span className="set-entry__icon set-entry__icon--violet">
            <GlobeIcon size={18} />
          </span>
          <span className="set-entry__body">
            <span className="set-entry__name">{t.settings.systemName}</span>
            <span className="set-entry__sub">{t.settings.systemSub}</span>
          </span>
          <ChevronRightIcon size={18} className="set-entry__chev" />
        </button>
        <button type="button" className="set-entry" onClick={onOpenCustom}>
          <span className="set-entry__icon set-entry__icon--violet">
            <PencilIcon size={18} />
          </span>
          <span className="set-entry__body">
            <span className="set-entry__name">{t.settings.customName}</span>
            <span className="set-entry__sub">{t.settings.customSub}</span>
          </span>
          <ChevronRightIcon size={18} className="set-entry__chev" />
        </button>
        {/* ONE row, not one per runtime (owner 2026-08-14, card rc-e1abbc506b70
          * option 1). He asked why this had been "split into so many", and he
          * was right about the half that matters: the two seed FILES predate
          * this work, but the settings list carried a single 啟動程序 row until
          * T-791e made both editable and gave each its own. The runtime is
          * chosen INSIDE the page now.
          *
          * What did NOT change, and must not: the two documents stay separate.
          * Their third step means opposite things (claude attaches `ocagent
          * listen` itself; codex must NOT and hands that to the sidecar), so
          * merging the TEXT would silently stop one runtime's agents ever
          * coming online. One entry, two documents. */}
        <button
          type="button"
          className="set-entry"
          onClick={() => onOpenBoot("claude")}
        >
          <span className="set-entry__icon set-entry__icon--violet">
            <BoltIcon size={18} />
          </span>
          <span className="set-entry__body">
            <span className="set-entry__name">{t.settings.bootName}</span>
            <span className="set-entry__sub">{t.settings.bootSub}</span>
          </span>
          <ChevronRightIcon size={18} className="set-entry__chev" />
        </button>
        {/* 下線程序 sits FOURTH, right after 啟動程序: the two are the same
          * agent's life read end to end, so the list runs 開機 → 下線. This
          * document is NOT part of the boot fold — the server hands it over at
          * the moment it collects a session — which is exactly why it needs a
          * row of its own here rather than a paragraph inside another block. */}
        <button type="button" className="set-entry" onClick={onOpenOffboard}>
          <span className="set-entry__icon set-entry__icon--violet">
            <LogOutIcon size={18} />
          </span>
          <span className="set-entry__body">
            <span className="set-entry__name">{t.settings.offboardName}</span>
            <span className="set-entry__sub">{t.settings.offboardSub}</span>
          </span>
          <ChevronRightIcon size={18} className="set-entry__chev" />
        </button>
      </div>

      {/* zone 2: role definitions */}
      <div className="set-group-label">{t.settings.roleDefsSection}</div>
      <div className="set-entries">
        {roles.map((r) => {
          return (
            <div className="set-entry-row" key={r.key}>
              {/* __main is the visual role card: the open-detail button fills
               * it. Row action layout (owner feedback): the chevron stays
               * pinned at the row's RIGHT EDGE, and the CUSTOM-role trash
               * button overlays INSIDE the card just LEFT of the chevron. */}
              <div className="set-entry-row__main">
              <button
                type="button"
                className="set-entry"
                onClick={() => onOpenRole(r.key)}
              >
                <span className="set-entry__icon set-entry__icon--blue">
                  <UserIcon size={18} />
                </span>
                <span className="set-entry__body">
                  <span className="set-entry__name">
                    {(t.office.role as Record<string, string>)[r.key] ?? r.name}
                    {r.isDefault && (
                      <span className="set-badge">{t.settings.defaultBadge}</span>
                    )}
                    {!r.isSeed && (
                      <span className="set-badge">{t.settings.customBadge}</span>
                    )}
                  </span>
                  {/* NO body preview any more (T-1170). The roster answer
                    * carries no `definition_md`, so the first line of a
                    * persona is not something this list has — and fetching
                    * every role's document to print one line each is exactly
                    * what the directory exists to stop. The row keeps the
                    * name and its badges; the text is one click away. */}
                </span>
                <ChevronRightIcon size={18} className="set-entry__chev" />
              </button>
              {/* 刪除: CUSTOM roles only (seed roles are server-refused anyway —
               * the UI simply offers no affordance). Icon-only trash button;
               * click opens the centered confirm MODAL (the row itself stays
               * untouched); 409 (member online) surfaces honestly in it. */}
              {!r.isSeed && (
                <button
                  type="button"
                  className="set-entry-row__delete"
                  data-testid={`role-delete-${r.key}`}
                  aria-label={t.settings.deleteRole}
                  title={t.settings.deleteRole}
                  onClick={() => {
                    setConfirmKey(r.key);
                    setDeleteError(null);
                  }}
                >
                  <TrashIcon size={16} />
                </button>
              )}
              </div>
            </div>
          );
        })}

        {/* 新增角色定義 — the BOTTOM of the role list. The add entry grows an
         * INLINE editable row (owner-aligned pattern): one 角色名 field only —
         * Enter/確認 creates, Esc/取消 collapses the row back. The founding
         * member (server-named), model and effort ride server defaults;
         * everything is editable afterwards on the detail pages.
         * Owner feedback (M2 acceptance + 修仙 batch 1): the button shows a
         * "+" — never an avatar icon tile — and uses the SHARED `.add-entry`
         * silhouette (centered "+ label", solid low-key neutral frame, no
         * accent green), identical to 監控's 新增機器. */}
        {!adding ? (
          <button
            type="button"
            className="add-entry"
            onClick={() => setAdding(true)}
          >
            + {t.settings.addRole}
          </button>
        ) : (
          <div
            ref={createRowRef}
            className="set-entry set-add-inline"
            data-testid="role-create-row"
          >
            <span className="set-entry__icon set-entry__icon--violet">
              <UserIcon size={18} />
            </span>
            <input
              className="set-add-inline__input"
              value={roleName}
              autoFocus
              placeholder={t.settings.addRoleName}
              aria-label={t.settings.addRoleName}
              onChange={(e) => setRoleName(e.target.value)}
              onCompositionStart={() => {
                composingRef.current = true;
              }}
              onCompositionEnd={(e) => {
                composingRef.current = false;
                setRoleName(e.currentTarget.value);
              }}
              onKeyDown={(e) => {
                if (
                  e.nativeEvent.isComposing ||
                  e.keyCode === 229 ||
                  composingRef.current
                ) {
                  return;
                }
                if (e.key === "Enter" && !createBusy) void submitCreate();
                if (e.key === "Escape") {
                  // Spent here — see InlineEdit: the shared Esc dispatcher
                  // must not also close the surface around this field.
                  e.preventDefault();
                  resetForm();
                }
              }}
              data-testid="role-create-name"
            />
            <button
              type="button"
              className="doc-btn"
              disabled={createBusy}
              onClick={resetForm}
            >
              {t.settings.addRoleCancel}
            </button>
            <button
              type="button"
              className="doc-btn doc-btn--accent"
              disabled={createBusy}
              onClick={() => void submitCreate()}
              data-testid="role-create-submit"
            >
              {t.settings.addRoleSubmit}
            </button>
          </div>
        )}
        {adding && createError && (
          <div className="set-error">{t.settings.addRoleError}</div>
        )}
      </div>

      {/* 刪除確認 MODAL (owner feedback: centered dialog, not an inline block
       * under the row). Same copy as before; Esc / 取消 closes; a failed
       * delete keeps it open with the honest error line. */}
      {(() => {
        const target = roles.find((r) => r.key === confirmKey);
        if (!target) return null;
        return (
          <ConfirmModal
            testId="role-delete-confirm"
            confirmTestId="role-delete-confirm-btn"
            danger
            body={msg.deleteRoleConfirm(target.name || target.key)}
            error={deleteError}
            busy={deleteBusy}
            cancelLabel={t.settings.addRoleCancel}
            confirmLabel={t.settings.deleteRoleConfirmAction}
            onCancel={() => {
              setConfirmKey(null);
              setDeleteError(null);
            }}
            onConfirm={() => void confirmDelete(target.key)}
          />
        );
      })()}
    </div>
  );
}

