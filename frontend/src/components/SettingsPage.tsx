import { useEffect, useRef, useState, type ReactNode } from "react";
import { useI18n } from "../i18n";
import type {
  GlobalContextView,
  RoleDefView,
  VersionView,
  ReleaseCheckView,
} from "../types";
import { api, type ServerSettingsView, type ServerSettingsPatch } from "../api";
import { ApiError } from "../api/errors";
import { useVersion } from "../hooks/useVersion";
import { formatBuildVersion } from "../lib/versionFormat";
import { useGlobalContext } from "../hooks/useGlobalContext";
import { useRoles } from "../hooks/useRoles";
import { useServerSettings } from "../hooks/useServerSettings";
import { useTaskManuals } from "../hooks/useTaskManuals";
import { useMembers } from "../hooks/useMembers";
import {
  TaskManualsList,
  TaskManualHub,
  TaskManualDefinitionPage,
  TaskManualLearningsPage,
} from "./TaskManualsPage";
import type { TaskManualPatch } from "../api/adapter";
import {
  SEED_BOOT_SEQUENCE_MD,
  SEED_SYSTEM_INTERACTION_MD,
} from "../api/seeds";
import { isHttpStatus } from "../api/errors";
import { Markdown } from "./Markdown";
import { LessonsCard } from "./LessonsCard";
import { navigateHash } from "../lib/hashRoute";
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
  GearIcon,
  TrashIcon,
} from "./icons";
import { ConfirmModal } from "./ConfirmModal";
import { InlineEdit } from "./InlineEdit";
import "./settings.css";

// Which settings sub-view is showing. Navigation is internal to the page; the
// user leaves Settings entirely by clicking a nav tab (App owns that).
// The old single "global" doc is now the THREE blocks of the assembled boot
// context (global-context-3block-restructure): "system" (系統互動, read-only
// seed) / "custom" (使用者自訂, the owner-editable additive block behind
// /api/global-context) / "boot" (啟動程序, read-only seed).
type View =
  | { kind: "landing" }
  | { kind: "software" }
  | { kind: "roles" }
  | { kind: "params" }
  | { kind: "system" }
  | { kind: "custom" }
  | { kind: "boot" }
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
    const onSave = (patch: TaskManualPatch) => manualsH.update(key, patch);
    return view.kind === "manualDef" ? (
      <TaskManualDefinitionPage
        manual={manual}
        crumbs={subCrumbs}
        onSave={onSave}
      />
    ) : (
      <TaskManualLearningsPage
        manual={manual}
        crumbs={subCrumbs}
        onSave={onSave}
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

  if (view.kind === "system") {
    // 系統互動 — the read-only FIRST block of every agent's boot context. The
    // backend has NO write endpoint for it BY CONSTRUCTION (enforcement by
    // construction, not validation), so the card is READ-ONLY and the text
    // mirrors dal/seeds/system_interaction.md via SEED_SYSTEM_INTERACTION_MD —
    // the same constant assemble_boot_context heads with. No filename: blocks
    // are presented as content, not files.
    return (
      <DocDetail
        title={t.settings.systemName}
        doc={{ text: SEED_SYSTEM_INTERACTION_MD.trim(), isDefault: false }}
        badge={t.settings.readOnlyBadge}
        readOnly
        crumbs={[crumbRoot, crumbRoles, { label: t.settings.systemName }]}
      />
    );
  }

  if (view.kind === "custom") {
    // 使用者自訂 — the owner-editable ADDITIVE block (/api/global-context).
    // Empty text + isDefault=true = never written; the assembled boot context
    // skips the block entirely. Save = whole-block replace, reset = tombstone.
    return (
      <DocDetail
        title={t.settings.customName}
        doc={gc.ctx}
        crumbs={[crumbRoot, crumbRoles, { label: t.settings.customName }]}
        onSave={gc.save}
        onReset={gc.reset}
      />
    );
  }

  if (view.kind === "boot") {
    // Boot sequence is a FIXED studio SOP: unlike the user-custom block it has
    // NO owner overlay (backend seed_boot_sequence_text reads the file directly —
    // no write route), so the card is READ-ONLY. The text mirrors
    // dal/seeds/boot_sequence.md via SEED_BOOT_SEQUENCE_MD, the same constant
    // assemble_boot_context tails into every agent's boot context.
    return (
      <DocDetail
        title={t.settings.bootName}
        doc={{ text: SEED_BOOT_SEQUENCE_MD.trim(), isDefault: false }}
        badge={t.settings.bootBadge}
        readOnly
        crumbs={[crumbRoot, crumbRoles, { label: t.settings.bootName }]}
      />
    );
  }

  if (view.kind === "role") {
    const role = rolesH.roles.find((r) => r.key === view.key);
    // The persona page: role definition (top) + this role's OWN lessons
    // (per-role-learnings step1). The lessons card is the SAME shared
    // <LessonsCard> the app uses everywhere — scoped here to view.key so the
    // owner edits exactly this persona's accumulated learnings. `extra` renders
    // inside DocDetail's <div className="settings"> so the card inherits page
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
      <DocDetail
        title={roleTitle}
        // 角色名 rename — CUSTOM roles only (seed titles are i18n-localized by
        // key AND server-side name-locked). Same pencil inline-edit pattern as
        // the machine row; the save rides the existing role PATCH choke, and
        // the roster's role display names follow (single truth: role.name).
        onRenameTitle={
          role && !role.isSeed
            ? (name) => rolesH.save(view.key, { name })
            : undefined
        }
        doc={
          role
            ? { text: role.definitionMd, isDefault: role.isDefault }
            : null
        }
        crumbs={[crumbRoot, crumbRoles, { label: roleTitle }]}
        onSave={(text) => rolesH.save(view.key, { definitionMd: text })}
        // 重置 = "restore the FILE SEED" — only a seed role has one. A custom
        // role's doc IS its only truth (the server 404s its reset — verified
        // live), so the affordance is omitted rather than left half-dead.
        onReset={role?.isSeed ? () => rolesH.reset(view.key) : undefined}
        extra={<LessonsCard roleKey={view.key} />}
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
      </div>
    </div>
  );
}

// ── 參數調整 ────────────────────────────────────────────────────────────────
/** The login-TTL choices — the server-side whitelist verbatim
 * (SettingsUpdateDTO: 12h / 24h / 7d / 30d). */
const TTL_CHOICES = [43200, 86400, 604800, 2592000] as const;

/**
 * 參數調整 — 登入有效期 (token_ttl) + 自動換手門檻 (handover_pct), both durable
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
    if (!Number.isInteger(n) || n < 40 || n > 90) {
      // Local guard mirrors the server's 422 range — snap back, mark it.
      setRangeError(true);
      setHandoverDraft(null);
      return;
    }
    setHandoverDraft(null);
    if (n === settings.handoverPct) return;
    void onSave({ handoverPct: n });
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
              value={settings.tokenTtl}
              onChange={(e) => {
                setRangeError(false);
                void onSave({ tokenTtl: Number(e.target.value) });
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

// ── 軟體更新 ────────────────────────────────────────────────────────────────

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
      const echo = await api.getServerSettings();
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
                <div className="sw-build__headline">
                  <code className="sw-build__version">
                    {version.version !== "0.0.0"
                      ? version.version
                      : formatBuildVersion(version.gitSha, version.gitTime)}
                  </code>
                </div>
              </div>
              {version.updateAvailable ? (
                <span className="sw-badge sw-badge--new">
                  {t.settings.updateAvailable}
                  {version.latestVersion ? ` ${version.latestVersion}` : ""}
                </span>
              ) : (
                <span className="sw-badge sw-badge--ok">
                  {t.settings.upToDate}
                </span>
              )}
            </>
          ) : (
            <div className="sw-build__time">{t.mp.dash}</div>
          )}
        </div>
        {/* Upgrade button appears ONLY when a real newer version exists —
         * and only ever fires on the owner's click (no auto-upgrade). */}
        {version?.updateAvailable && (
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

      {/* ── explicit 檢查更新 (fresh GitHub verdict on the owner's click) ── */}
      <div className="sw-check">
        <button
          type="button"
          className="btn sw-check__btn"
          disabled={checkState.kind === "checking"}
          onClick={runCheck}
          data-testid="settings-check-release"
        >
          {checkState.kind === "checking"
            ? t.settings.checkingUpdate
            : t.settings.checkUpdate}
        </button>
        {checkState.kind === "failed" && (
          <span className="set-error param-error">{t.settings.checkFailed}</span>
        )}
        {checkState.kind === "done" && (
          <span className="sw-check__verdict" data-testid="settings-check-verdict">
            {checkState.verdict.status === "up_to_date" && t.settings.upToDate}
            {checkState.verdict.status === "unknown" && t.settings.checkUnknown}
            {checkState.verdict.status === "update_available" && (
              <>
                {t.settings.updateAvailable}
                {checkState.verdict.latestTag ? ` ${checkState.verdict.latestTag}` : ""}
                {checkState.verdict.releaseUrl && (
                  <>
                    {" · "}
                    <a
                      href={checkState.verdict.releaseUrl}
                      target="_blank"
                      rel="noreferrer"
                    >
                      {t.settings.viewRelease}
                    </a>
                  </>
                )}
              </>
            )}
          </span>
        )}
      </div>

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
    </div>
  );
}

// ── 角色誌 (list) ───────────────────────────────────────────────────────────
/** First non-heading, non-empty line of a markdown doc, trimmed — an HONEST
 * preview drawn from the real seed (never the mockup's illustrative desc). */
function firstBodyLine(md: string): string {
  for (const raw of md.split("\n")) {
    const line = raw.trim();
    if (line === "" || line.startsWith("#") || line.startsWith(">")) continue;
    return line.replace(/^[-*]\s+/, "").replace(/\*\*/g, "");
  }
  return "";
}

function RolesLog({
  roles,
  error,
  crumbs,
  onOpenSystem,
  onOpenCustom,
  onOpenBoot,
  onOpenRole,
  onCreate,
  onDelete,
  autoCreate,
}: {
  roles: RoleDefView[];
  error: boolean;
  crumbs: Crumb[];
  onOpenSystem: () => void;
  onOpenCustom: () => void;
  onOpenBoot: () => void;
  onOpenRole: (key: string) => void;
  onCreate: (input: { name: string }) => Promise<unknown>;
  onDelete: (key: string) => Promise<void>;
  /** #settings/roles/new deep-link: land with the inline 新增角色 create row
   * already open (T-25b7). One-shot — seeds the initial state only. */
  autoCreate?: boolean;
}) {
  const { t } = useI18n();

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

      {/* zone 1: the THREE global-context blocks, in boot-assembly order:
       * 系統互動 (read-only seed, heads the boot context) → 使用者自訂 (the
       * owner-editable additive block) → 啟動程序 (read-only seed, appended
       * LAST). No filenames — the blocks are content, not files. */}
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
        <button type="button" className="set-entry" onClick={onOpenBoot}>
          <span className="set-entry__icon set-entry__icon--violet">
            <BoltIcon size={18} />
          </span>
          <span className="set-entry__body">
            <span className="set-entry__name">{t.settings.bootName}</span>
            <span className="set-entry__sub">{t.settings.bootSub}</span>
          </span>
          <ChevronRightIcon size={18} className="set-entry__chev" />
        </button>
      </div>

      {/* zone 2: role definitions */}
      <div className="set-group-label">{t.settings.roleDefsSection}</div>
      <div className="set-entries">
        {roles.map((r) => {
          const preview = firstBodyLine(r.definitionMd);
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
                  {preview && (
                    <span className="set-entry__sub">{preview}</span>
                  )}
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
                if (e.key === "Escape") resetForm();
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
            body={t.settings.deleteRoleConfirm(target.name || target.key)}
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

// ── Doc detail (global context / role def): view + edit ─────────────────────
interface DocDetailDoc {
  text: string;
  isDefault: boolean;
}

function DocDetail({
  title,
  onRenameTitle,
  doc,
  crumbs,
  onSave,
  onReset,
  extra,
  readOnly = false,
  badge,
}: {
  title: string;
  /** Rename the doc's TITLE (custom roles only — the 角色名 is owner-editable
   * there; seed roles pass none and keep the plain heading). Renders the shared
   * pencil InlineEdit in the heading; commits ride the role PATCH choke. */
  onRenameTitle?: (name: string) => Promise<void> | void;
  doc: DocDetailDoc | GlobalContextView | null;
  /** The unified settings breadcrumb (T-8f6e) — 設定 › 角色誌 › <this doc>. */
  crumbs: Crumb[];
  /** Save/reset are omitted for read-only docs (e.g. the boot sequence, a fixed
   * studio SOP with no owner overlay). */
  onSave?: (text: string) => Promise<void> | void;
  onReset?: () => Promise<void> | void;
  /** Optional content rendered below the doc card (e.g. the persona page's
   * per-role <LessonsCard>). The global-context view passes none. */
  extra?: ReactNode;
  /** Read-only mode: no edit/reset affordances, just the rendered markdown.
   * Used by the boot-sequence card (fixed studio SOP, no owner overlay). */
  readOnly?: boolean;
  /** Overrides the "Default" is_default badge (e.g. "Studio SOP" for boot). */
  badge?: string;
}) {
  const { t } = useI18n();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);

  const text = doc ? doc.text : "";
  const isDefault = doc ? doc.isDefault : true;

  function startEdit() {
    setDraft(text);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setDraft("");
  }

  async function commit() {
    if (!onSave) return;
    setBusy(true);
    try {
      await onSave(draft);
      setEditing(false);
      setDraft("");
    } finally {
      setBusy(false);
    }
  }

  async function doReset() {
    if (!onReset) return;
    setBusy(true);
    try {
      await onReset();
      setEditing(false);
      setDraft("");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="settings">
      <Breadcrumbs items={crumbs} />
      <h1 className="settings__title settings__title--doc">
        {onRenameTitle ? (
          <InlineEdit
            value={title}
            onCommit={(next) => void onRenameTitle(next)}
            ariaLabel={t.settings.renameRole}
            placeholder={t.settings.addRoleName}
          />
        ) : (
          title
        )}
      </h1>

      <div className="doc-card">
        <div className="doc-card__head">
          {/* No filename chip here — docs are presented as CONTENT, not files
           * (the role page's internal role-….md name was implementation detail
           * the owner should never see). The head keeps only the badge. */}
          <span className="doc-card__file">
            {badge ? (
              <span className="set-badge">{badge}</span>
            ) : (
              isDefault && (
                <span className="set-badge">{t.settings.defaultBadge}</span>
              )
            )}
          </span>
          {readOnly ? null : editing ? (
            <div className="doc-card__actions">
              {/* 重置 renders only where a seed exists to restore (onReset
               * omitted ⇒ no button — e.g. custom roles, whose reset the
               * server 404s). */}
              {onReset && (
                <button
                  type="button"
                  className="doc-btn"
                  onClick={doReset}
                  disabled={busy}
                >
                  {t.settings.reset}
                </button>
              )}
              <button
                type="button"
                className="doc-btn"
                onClick={cancelEdit}
                disabled={busy}
              >
                {t.settings.cancel}
              </button>
              <button
                type="button"
                className="doc-btn doc-btn--accent"
                onClick={commit}
                disabled={busy}
              >
                {t.settings.doneEdit}
              </button>
            </div>
          ) : (
            <button
              type="button"
              className="doc-btn doc-btn--edit"
              onClick={startEdit}
            >
              <PencilIcon size={14} />
              <span>{t.settings.edit}</span>
            </button>
          )}
        </div>

        <div className="doc-card__body">
          {editing && !readOnly ? (
            <textarea
              className="doc-editor"
              value={draft}
              autoFocus
              spellCheck={false}
              placeholder={t.settings.editorPlaceholder}
              onChange={(e) => setDraft(e.target.value)}
            />
          ) : (
            <Markdown source={text} className="doc-md" />
          )}
        </div>
      </div>
      {extra}
    </div>
  );
}
