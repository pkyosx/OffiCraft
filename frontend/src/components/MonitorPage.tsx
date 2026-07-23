import { useEffect, useRef, useState } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { ApiError } from "../api/errors";
import { formatCost } from "../lib/cost";
import { useMembers } from "../hooks/useMembers";
import { useMonitoring } from "../hooks/useMonitoring";
import { useMachines } from "../hooks/useMachines";
import { useOutsourceWorkers } from "../hooks/useOutsourceWorkers";
import type {
  MonAccountView,
  MonSessionView,
  Member,
  MachineView,
  UninstallResultView,
  BootstrapResultView,
} from "../types";
import type { OutsourceWorkerView } from "../api/adapter";
import { joinSessionRuntime } from "../lib/runtime";
import { useHashRoute } from "../lib/hashRoute";
import { Avatar } from "./Avatar";
import { InlineEdit } from "./InlineEdit";
import { MemberDetailPanel } from "./MemberDetailPanel";
import { PresenceBadge } from "./PresenceBadge";
import { CopyIcon, CheckIcon, CloseIcon } from "./icons";
import "./monitor.css";

export function MonitorPage() {
  const { t } = useI18n();
  const { monitoring, refetch } = useMonitoring();
  // The machine registry (GET /api/machines) is the source for the machines
  // panel identity + online + teardown target — NOT the monitoring telemetry.
  const { machines, refetch: refetchMachines } = useMachines();
  // Inline-rename failure surface (e.g. server 422 on a blank/whitespace name).
  // Never silently swallow the PATCH rejection — show an honest banner.
  const [renameError, setRenameError] = useState<string | null>(null);
  // 帳號詳情 modal target (T-a9a7): the account whose real identity (key /
  // email / org / cost) is being inspected; null = closed. Same
  // target-state pattern as the uninstall/delete modals below.
  const [detailAccount, setDetailAccount] = useState<MonAccountView | null>(
    null
  );
  // The roster is the join source for a session's effort badge + the member the
  // detail panel needs. subscribeEvents inside each hook reconciles by refetch.
  const { members, refetch: refetchMembers } = useMembers();
  // Outsource workers (O-xx) are ALSO live AI sessions — they burn context and
  // cost the same way members do (owner report 2026-07-19). The office 外包 panel's
  // existing hook already carries every column this table needs
  // (machine/account/model/context_pct/cost + banked), so we reuse it verbatim
  // and render its rows alongside the member sessions below — no wire change.
  const outsource = useOutsourceWorkers();
  // The open member-detail rides on the URL hash (#monitor/member/<id>) so a
  // refresh restores it; a stale id self-heals (lookup below misses → list view).
  const [route, setRoute] = useHashRoute();
  const detailId = route.detailId ?? null;
  const setDetailId = (id: string | null) =>
    setRoute({ page: "monitor", detailId: id ?? undefined });

  // ── 新增機器 / 上線 (onboard) ── the "+新增機器" add entry sits BELOW the
  // machine table card (the entry is the only frame — never nested
  // inside the card's border); clicking it grows an INLINE editable row
  // (owner-aligned pattern, same as 角色誌's 新增角色定義): one machine-name
  // field — Enter/確認 creates the row, Esc/取消 collapses it back.
  const [onboardAdding, setOnboardAdding] = useState(false);
  const [onboardName, setOnboardName] = useState("");
  const [onboardBusy, setOnboardBusy] = useState(false);
  const [onboardError, setOnboardError] = useState<string | null>(null);
  // IME composition guard (same rule as InlineEdit): an Enter confirming a CJK
  // candidate must not submit the row.
  const onboardComposingRef = useRef(false);

  // ── install (安裝) ── the verb has TWO shapes by row kind:
  //   • the server-self row → an in-place bootstrap-on-server, run DIRECTLY with
  //     NO dialog (the server installs the warden on itself); see installSelf.
  //   • every other (remote) machine → a SINGLE uniform copy-command dialog
  //     (`installTarget`): copy the command, run it on that machine.
  const [installTarget, setInstallTarget] = useState<MachineView | null>(null);
  // The in-place bootstrap-on-server result for the server-self row (POST
  // /bootstrap-here): {ok, exitCode, log}; on !ok the `log` (failure reason, e.g.
  // the one-warden guard) is surfaced verbatim — never swallowed. `bootstrapTarget`
  // marks the machine acted on so the result block below the table names it.
  const [bootstrapTarget, setBootstrapTarget] = useState<MachineView | null>(
    null
  );
  const [bootstrapBusy, setBootstrapBusy] = useState(false);
  const [bootstrapError, setBootstrapError] = useState<string | null>(null);
  const [bootstrapResult, setBootstrapResult] =
    useState<BootstrapResultView | null>(null);

  // ── uninstall (解除安裝) state ── the "uninstall" verb → POST
  // /api/machines/{id}/uninstall: drive the uninstall RPC to the warden (runs
  // `ocwarden uninstall` on its box). ONLINE-ONLY (an offline machine has nothing
  // to uninstall). A HOST-mutating remote action → confirm first. The record is
  // KEPT (re-installable) — the row does NOT drop; the machine goes offline once
  // the warden reports the receipt. The result carries {dispatched}.
  const [uninstallTarget, setUninstallTarget] = useState<MachineView | null>(
    null
  );
  // Guard: when members are still ACTUALLY ONLINE on the machine, clicking
  // uninstall opens THIS warning dialog first (advise taking them offline)
  // instead of the plain confirm. Proceed runs the same uninstall; a machine
  // whose agents are all offline keeps the direct-to-confirm behavior.
  const [uninstallWarnTarget, setUninstallWarnTarget] =
    useState<MachineView | null>(null);
  const [uninstallBusy, setUninstallBusy] = useState(false);
  const [uninstallError, setUninstallError] = useState<string | null>(null);
  const [uninstallResult, setUninstallResult] = useState<{
    machine: MachineView;
    result: UninstallResultView;
  } | null>(null);

  // ── delete (刪除) state ── the "delete" verb → DELETE /api/machines/{id}: a PURE
  // roster soft-delete (delete ≠ uninstall ≠ stop). It removes the machine record;
  // it does NOT tear the warden off the box (that is uninstall). A destructive
  // action → confirm first; on success the row drops (refetch). The result carries
  // {removed}.
  // one-click upgrade (T-5f01 rework): per-row busy + "升級中" latch + banner.
  // upgradeSent holds the pressed row in the disabled "upgrading" face until
  // the VERDICT ITSELF converges — a later refetch reports binStatus
  // "current" and the effect below releases the latch (owner: 按下去變成升級中,
  // 直到該機收斂為最新才恢復). The FE never fabricates the convergence.
  const [upgradeBusy, setUpgradeBusy] = useState<string | null>(null);
  const [upgradeSent, setUpgradeSent] = useState<Record<string, boolean>>({});
  const [upgradeError, setUpgradeError] = useState<string | null>(null);

  // Release the "升級中" latch ONLY when the row's verdict converges to
  // "current" (or the row left the registry). A still-stale refetch keeps the
  // latch — the swap simply hasn't landed/heartbeated yet.
  useEffect(() => {
    setUpgradeSent((prev) => {
      const keep = Object.keys(prev).filter((id) => {
        const m = machines.find((x) => x.machineId === id);
        return m !== undefined && m.binStatus !== "current";
      });
      if (keep.length === Object.keys(prev).length) return prev;
      return Object.fromEntries(keep.map((id) => [id, true]));
    });
  }, [machines]);

  const [deleteTarget, setDeleteTarget] = useState<MachineView | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  // Live session telemetry is the SAME source the "AI 會話" rows below read.
  const sessions = monitoring?.sessions ?? [];

  const rosterDetail = detailId
    ? members.find((m) => m.id === detailId)
    : undefined;
  // Join the live session's context/cost onto the member so the detail panel
  // shows the SAME value as the session row (same source, never divergent).
  const detail = rosterDetail
    ? joinSessionRuntime(rosterDetail, sessions)
    : undefined;

  // Clicking a session row opens the SAME member-detail panel the office uses,
  // wired to the SAME api mutations (activate/refocus/rename → refetch).
  if (detail) {
    return (
      <MemberDetailPanel
        member={detail}
        onBack={() => setDetailId(null)}
        onActivate={async (machineId) => {
          // Return the result (T-7fa1) — the Monitor entry renders the SAME
          // MemberDetailPanel, so it must feed it the same verdict.
          const result = await api.activateMember(detail.id, machineId);
          try {
            await refetchMembers();
          } catch {
            /* the verdict outlives a failed refresh (NIT-4) */
          }
          return result;
        }}
        // 改機器 (placement only): re-pin the member's desired machine and let the
        // server reconcile a live member onto it — NEVER wakes the member (never
        // touches desired_state), unlike onActivate. Mirrors OfficePage's
        // MemberDetailPanel entry; the Monitor entry was the only member-detail
        // surface still missing it. Refetch via THIS page's roster hook
        // (refetchMembers) so the new pin surfaces the same way every other
        // action here reconciles — not OfficePage's own refetch().
        onRelocate={async (machineId) => {
          const result = await api.relocateMember(detail.id, machineId);
          try {
            await refetchMembers();
          } catch {
            /* the verdict outlives a failed refresh (NIT-4) */
          }
          return result;
        }}
        // Graceful stop / cancel-wake (retains the row). Refetch and let
        // server-driven presence surface stopping → stopped. Same stop/force-stop
        // capability the office entry offers — the shared MemberDetailPanel must
        // behave identically no matter which entry opened it.
        onDeactivate={async () => {
          await api.deactivateMember(detail.id);
          await refetchMembers();
        }}
        // Force-stop (immediate kill): escalate a *stopping* member past the 120s
        // grace — the server dispatches the robust STOP to the warden now.
        onForceStop={async () => {
          await api.forceStopMember(detail.id);
          await refetchMembers();
        }}
        onRefocus={async () => {
          await api.refocusMember(detail.id);
          await refetchMembers();
        }}
        onRename={async (name) => {
          await api.patchMember(detail.id, { name });
          await refetchMembers();
        }}
      />
    );
  }

  const dash = t.monitor.dash;
  // `machines` is the registry (useMachines) — the machines panel's source.
  const accounts = monitoring?.accounts ?? [];
  // Hardware telemetry per host (Monitor §2 fold). We JOIN it onto each registry
  // machine so one row carries identity/online/actions (registry) AND CPU/RAM/
  // POWER (telemetry). The telemetry card's `machine` key IS the host machine-id
  // (the warden's own id, e.g. `m-server-self`) — the SAME value as the registry
  // row's `machineId`. So the join key is `machineId` directly; we do NOT bounce
  // through the warden member's `desiredMachineId` (which is empty for a warden that
  // hosts itself → that stale indirection made every hardware cell show dash).
  // No telemetry for a machine → hardware columns show dash (the row NEVER
  // disappears; regression #1 was the whole hardware block being dropped).
  const monMachines = monitoring?.machines ?? [];
  const hwByHost = new Map(monMachines.map((mm) => [mm.machine, mm]));

  // Rename a machine/account display label: PATCH (by stable id) then refetch
  // monitoring for the fresh label (the PATCH returns a narrow alias, not a row —
  // we never merge it in). On failure (server 422 on blank, etc.) show an honest
  // banner; InlineEdit already blocks empty/unchanged commits client-side.
  // One-click upgrade (T-5f01): fire-and-forget — POST /upgrade enqueues the
  // `update` command onto the warden's live SSE downstream. `dispatched=false`
  // (raced offline) surfaces as the offline hint, never a silent success. On a
  // dispatched send the button latches into the disabled "升級中" face; the
  // verdict itself converges via refetch when the next heartbeat's fingerprints
  // turn current (reconcile-by-refetch — the FE never fabricates the status;
  // the effect above releases the latch on convergence).
  const requestUpgrade = async (m: MachineView) => {
    if (upgradeBusy !== null) return;
    setUpgradeError(null);
    setUpgradeBusy(m.machineId);
    try {
      const res = await api.upgradeMachine(m.machineId);
      if (res.dispatched) {
        setUpgradeSent((prev) => ({ ...prev, [m.machineId]: true }));
      } else {
        setUpgradeError(t.monitor.machine.upgradeOfflineHint);
      }
      await refetchMachines();
    } catch {
      setUpgradeError(t.monitor.machine.upgradeError);
    } finally {
      setUpgradeBusy(null);
    }
  };

  const renameMachine = (id: string, next: string) => {
    setRenameError(null);
    api
      .patchMachine(id, { displayName: next })
      .then(() => refetchMachines())
      .catch(() => setRenameError(t.monitor.renameError));
  };
  const renameAccount = (id: string, next: string) => {
    setRenameError(null);
    api
      .patchAccount(id, { displayName: next })
      .then(() => refetch())
      .catch(() => setRenameError(t.monitor.renameError));
  };

  // Collapse the inline onboard row back (Esc / 取消 / after a create).
  const resetOnboardRow = () => {
    setOnboardAdding(false);
    setOnboardName("");
    setOnboardError(null);
  };

  // Add a machine with the name typed in the inline row (blank → keep the row
  // open, nothing created). On success we refetch the registry so the new
  // machine surfaces as a new row (offline until its warden reports in). The
  // onboard result's boot_command is deliberately discarded — it is re-mintable
  // anytime via that row's Install (api.getMachineBootCommand).
  const addMachine = async () => {
    const name = onboardName.trim();
    if (onboardBusy || !name) return;
    setOnboardBusy(true);
    setOnboardError(null);
    try {
      await api.onboardMachine(name);
      await refetchMachines();
      resetOnboardRow();
    } catch {
      setOnboardError(t.monitor.machine.onboardError);
    } finally {
      setOnboardBusy(false);
    }
  };

  // Open the copy-command install dialog for a REMOTE (non-server-self) machine.
  const openInstall = (machine: MachineView) => {
    setInstallTarget(machine);
  };
  const closeInstall = () => {
    setInstallTarget(null);
  };

  // Install the server-self machine IN PLACE → POST bootstrap-here (owner-only HOST
  // action): the server installs the warden on itself. NO dialog — run directly on
  // click. A failed install is a REAL result (ok=false + log), NOT a thrown error —
  // only a transport/gate failure lands in catch.
  //
  // T-ba62: the log is KEPT ON SUCCESS TOO. It used to be discarded on the ok
  // branch, on the reasoning that "the row flipping online IS the signal" — but
  // the installer's most important output was a WARNING it emitted while still
  // exiting 0 (claude unresolvable), and that branch threw it away unread. So
  // "installed cleanly" and "installed with a warning that guarantees every
  // spawn will fail" rendered identically: no panel, no log, a green row. The
  // block below now shows both outcomes and labels which one it is; the owner
  // dismisses it.
  const installSelf = async (machine: MachineView) => {
    if (bootstrapBusy) return;
    setBootstrapTarget(machine);
    setBootstrapResult(null);
    setBootstrapError(null);
    setBootstrapBusy(true);
    try {
      const result = await api.bootstrapOnServer(machine.machineId);
      setBootstrapResult(result);
      if (result.ok) {
        await Promise.all([refetchMachines(), refetchMembers()]);
      }
    } catch (e) {
      // Surface the server's error detail (e.g. the 503 "ocwarden binary is
      // not available" reason) — a bare "request failed" hides the actual fix.
      const detail = e instanceof ApiError ? e.serverMessage : "";
      setBootstrapError(
        detail
          ? t.monitor.machine.bootstrapErrorDetail(detail)
          : t.monitor.machine.bootstrapError
      );
    } finally {
      setBootstrapBusy(false);
    }
  };

  // Confirm uninstall → POST /api/machines/{id}/uninstall (owner-only remote HOST
  // action): drive the uninstall RPC to the warden. The record is KEPT
  // (re-installable) — the row does NOT drop. A transport/gate failure lands in
  // catch (honest error banner). On success we surface the {dispatched} result and
  // refetch so the machine's online state (→ offline once the warden reports)
  // reconciles by refetch, never by an optimistic guess.
  // The real AI members ACTUALLY ONLINE on a machine right now (owner-decided
  // criterion, same as the server's 409 gate: live presence + observed
  // position — an offline member merely BOUND here via desiredMachineId never
  // counts, so an all-offline machine uninstalls/deletes without a warning).
  // The machine's own warden is excluded — it IS the machine.
  const membersOnMachine = (machineId: string) =>
    members.filter(
      (m) =>
        m.kind !== "warden" && m.status === "online" && m.machine === machineId
    );

  // Machine mid-uninstall (①): the warden member still carries the one-shot
  // desired_state="uninstall" intent — the server consumes it (→ "offline")
  // the moment it observes the warden disconnect. Until then the row shows the
  // same in-progress treatment as installing: button re-labelled + disabled;
  // the member-delta refetch flips it back automatically.
  const uninstalling = (machineId: string) =>
    members.find((m) => m.id === machineId)?.desiredState === "uninstall";

  const runUninstall = async (machine: MachineView) => {
    if (uninstallBusy) return;
    setUninstallBusy(true);
    setUninstallError(null);
    try {
      const result = await api.uninstallMachine(machine.machineId);
      setUninstallResult({ machine, result });
      setUninstallTarget(null);
      setUninstallWarnTarget(null);
      await Promise.all([refetchMachines(), refetchMembers()]);
    } catch {
      setUninstallError(t.monitor.machine.uninstallError);
    } finally {
      setUninstallBusy(false);
    }
  };

  const confirmUninstall = () => {
    if (!uninstallTarget) return;
    void runUninstall(uninstallTarget);
  };

  // Confirm delete → DELETE /api/machines/{id} (owner-only): a PURE roster
  // soft-delete (no warden command). On success the row drops (refetch). A
  // transport/gate failure lands in catch (honest error banner; the row stays).
  const confirmDelete = async () => {
    if (!deleteTarget || deleteBusy) return;
    setDeleteBusy(true);
    setDeleteError(null);
    try {
      await api.deleteMachine(deleteTarget.machineId);
      setDeleteTarget(null);
      await Promise.all([refetchMachines(), refetchMembers()]);
    } catch {
      setDeleteError(t.monitor.machine.deleteError);
    } finally {
      setDeleteBusy(false);
    }
  };

  // "AI 會話" lists only real AI assistant sessions. A warden is a machine-layer
  // telemetry daemon (a script, not an AI) — its hardware already shows under
  // 機器資訊, so it must NOT appear here as if it were an AI agent (Seth's
  // warden≠LLM rule; the office roster is already filtered the same way in
  // OfficePage). A session with no roster match is left visible (honest: we
  // can't prove it's non-AI).
  const aiSessions = sessions.filter((s) => {
    const m = members.find((x) => x.id === s.id);
    return m?.kind !== "warden";
  });

  return (
    <div className="monitor">
      {renameError && <div className="mon-error">{renameError}</div>}
      {/* ── §1 帳號資訊 (Accounts) ── */}
      <section className="mon-section">
        <div className="mon-section__title">{t.monitor.accountsTitle}</div>
        {accounts.length === 0 ? (
          <div className="mon-empty">{t.monitor.accountsEmpty}</div>
        ) : (
          <div className="mon-accounts">
            {accounts.map((a) => (
              <AccountCard
                key={`${a.account}@${a.machine}`}
                account={a}
                onRename={(next) => renameAccount(a.account, next)}
                onDetail={() => setDetailAccount(a)}
              />
            ))}
          </div>
        )}
        {detailAccount && (
          <AccountDetailModal
            account={detailAccount}
            onClose={() => setDetailAccount(null)}
          />
        )}
      </section>

      {/* ── §2 機器資訊 (Machines) ── */}
      <section className="mon-section">
        <div className="mon-section__head">
          <div className="mon-section__title">{t.monitor.machinesTitle}</div>
        </div>

        {onboardError && <div className="mon-error">{onboardError}</div>}
        {upgradeError && <div className="mon-error">{upgradeError}</div>}

        <div className="mon-table-wrap">
          <table className="mon-table">
            <thead>
              <tr>
                <th className="mon-table__left">{t.monitor.machineCol.machine}</th>
                <th className="mon-table__left">{t.monitor.machineCol.status}</th>
                <th className="mon-table__left">{t.monitor.machineCol.claude}</th>
                <th>{t.monitor.machineCol.cpu}</th>
                <th>{t.monitor.machineCol.ram}</th>
                <th>{t.monitor.machineCol.power}</th>
                <th className="mon-table__right">
                  {t.monitor.machine.actionsCol}
                </th>
              </tr>
            </thead>
            <tbody>
              {machines.length === 0 ? (
                <tr>
                  <td className="mon-table__left mon-muted" colSpan={7}>
                    {t.monitor.machine.machinesEmpty}
                  </td>
                </tr>
              ) : (
                machines.map((m) => {
                  // Join hardware telemetry by this machine's own id (see
                  // hwByHost above — its key IS the host machine-id). Undefined
                  // when the machine reported no telemetry → hardware cells fall
                  // back to dash, row stays.
                  const hw = hwByHost.get(m.machineId);
                  return (
                  <tr key={m.machineId}>
                    {/* display_name is the editable label; the PATCH target is the
                     * stable machineId, NOT the label. */}
                    <td
                      className="mon-table__left"
                      data-label={t.monitor.machineCol.machine}
                    >
                      <div className="mon-machine-name">
                        <InlineEdit
                          value={m.displayName}
                          onCommit={(next) => renameMachine(m.machineId, next)}
                          ariaLabel={t.monitor.renameMachine}
                          placeholder={t.monitor.renamePlaceholder}
                          displayClassName={`mon-table__strong${
                            m.isSelf ? " mon-self-name" : ""
                          }`}
                        />
                        {/* Stable machine id (the warden member's own id / token
                            sub) — the machine's identity, never editable. Mirrors
                            the member detail panel's id badge. */}
                        <span
                          className="mon-machine-id"
                          data-testid="mon-machine-id"
                          title={m.machineId}
                        >
                          {m.machineId}
                        </span>
                      </div>
                    </td>
                    {/* online badge — honest passthrough of the registry's online */}
                    <td
                      className="mon-table__left"
                      data-label={t.monitor.machineCol.status}
                    >
                      <span
                        className={`mon-online${
                          m.online ? " mon-online--on" : " mon-online--off"
                        }`}
                      >
                        <span
                          className={`status-dot ${
                            m.online ? "status-dot--online" : "status-dot--offline"
                          }`}
                          aria-hidden
                        />
                        {m.online
                          ? t.monitor.machine.online
                          : t.monitor.machine.offline}
                      </span>
                    </td>
                    {/* warden-probed claude CLI version — honest dash when the
                     * machine never reported one (old warden / no probe). */}
                    <td
                      className="mon-table__left"
                      data-label={t.monitor.machineCol.claude}
                      data-testid="mon-claude-version"
                    >
                      {m.claudeVersion ?? dash}
                    </td>
                    {/* Hardware telemetry (joined by host). Honest dash when the
                     * host reported no telemetry — never a fabricated number. */}
                    <td data-label={t.monitor.machineCol.cpu}>
                      {pctText(hw?.cpuPct ?? null, dash)}
                    </td>
                    <td data-label={t.monitor.machineCol.ram}>
                      {pctText(hw?.ramPct ?? null, dash)}
                    </td>
                    <td data-label={t.monitor.machineCol.power}>
                      {powerText(hw ? hw.acPower : null, hw?.batteryPct ?? null, dash)}
                    </td>
                    {/* Actions — the machine-lifecycle verbs (T-IUD):
                     *   install   → server-self: in-place bootstrap-on-server, run
                     *               DIRECTLY (no dialog); other machines: a single
                     *               copy-command dialog.
                     *   uninstall → POST /uninstall (drive the uninstall RPC to the
                     *               warden). ONLINE-ONLY — an offline machine has
                     *               nothing to uninstall (disabled + reason tooltip).
                     *   delete    → DELETE /machines/{id} (PURE roster soft-delete);
                     *               NOT offered for the server-self row (undeletable).
                     */}
                    <td
                      className="mon-table__right"
                      data-label={t.monitor.machine.actionsCol}
                    >
                      <div className="mon-actions">
                        {/* upgrade (T-5f01 rework) — lives IN the action group,
                         * no version column/badge. Shown ONLY for an installed
                         * machine (warden online = the agent tooling is on the
                         * box and reachable; an offline warden has no downstream
                         * to command and self-updates on its next connect).
                         * Enabled ONLY when the server's fingerprint verdict
                         * says a newer build exists (stale); current/unknown
                         * render it disabled with the honest reason as tooltip.
                         * A dispatched send wears the disabled 升級中 face until
                         * the verdict converges to current (refetch — see the
                         * upgradeSent release effect). */}
                        {m.online && (
                          <button
                            type="button"
                            className="btn btn--accent-ghost"
                            data-testid="mon-upgrade-btn"
                            disabled={
                              m.binStatus !== "stale" ||
                              upgradeBusy === m.machineId ||
                              upgradeSent[m.machineId] === true
                            }
                            {...(m.binStatus === "current"
                              ? { title: t.monitor.machine.upgradeCurrentHint }
                              : m.binStatus === null
                                ? { title: t.monitor.machine.upgradeUnknownHint }
                                : {})}
                            onClick={() => void requestUpgrade(m)}
                          >
                            {upgradeBusy === m.machineId ||
                            upgradeSent[m.machineId] === true
                              ? t.monitor.machine.upgrading
                              : t.monitor.machine.upgrade}
                          </button>
                        )}
                        <button
                          type="button"
                          className="btn btn--accent-ghost"
                          data-testid="mon-install-btn"
                          disabled={(m.isSelf && bootstrapBusy) || m.online}
                          onClick={() =>
                            m.isSelf ? void installSelf(m) : openInstall(m)
                          }
                        >
                          {m.isSelf && bootstrapBusy
                            ? t.monitor.machine.bootstrapBusy
                            : t.monitor.machine.install}
                        </button>
                        {/* Mid-uninstall (intent still pending on the warden) the
                         * button wears the SAME in-progress treatment as install:
                         * transitional label + disabled, until the server consumes
                         * the one-shot intent on the warden's disconnect. */}
                        <button
                          type="button"
                          className="btn btn--accent-ghost"
                          data-testid="mon-uninstall-btn"
                          disabled={!m.online || uninstalling(m.machineId)}
                          {...(!m.online
                            ? { title: t.monitor.machine.uninstallOfflineHint }
                            : {})}
                          onClick={() => {
                            setUninstallError(null);
                            if (membersOnMachine(m.machineId).length > 0) {
                              setUninstallWarnTarget(m);
                            } else {
                              setUninstallTarget(m);
                            }
                          }}
                        >
                          {uninstalling(m.machineId)
                            ? t.monitor.machine.uninstallInProgress
                            : t.monitor.machine.uninstall}
                        </button>
                        {/* The server-self row is NOT deletable — the button stays
                         * (disabled) so every row's columns line up. */}
                        <button
                          type="button"
                          className="btn btn--danger-ghost"
                          data-testid="mon-delete-btn"
                          disabled={m.isSelf}
                          onClick={() => {
                            if (m.isSelf) return;
                            setDeleteError(null);
                            setDeleteTarget(m);
                          }}
                        >
                          {t.monitor.machine.deleteMachine}
                        </button>
                      </div>
                    </td>
                  </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>

        {/* "+新增機器 / 上線" — a standalone add entry BELOW the machine
         * table card. Owner feedback (M2 acceptance): the entry must be the
         * ONLY frame around the add affordance — as a table row it rendered
         * boxed inside the card's own border (frame-in-frame). 修仙 batch 1:
         * it wears the SHARED `.add-entry` silhouette (centered "+ label",
         * solid low-key neutral frame, no accent green), identical to 角色誌's
         * 新增角色定義. Clicking grows the INLINE editable row in place
         * (owner-aligned pattern): one machine-name field, Enter/確認 creates,
         * Esc/取消 collapses. */}
        <div className="mon-onboard">
          {!onboardAdding ? (
            <button
              type="button"
              id="mon-onboard-entry"
              className="add-entry"
              onClick={() => setOnboardAdding(true)}
            >
              + {t.monitor.machine.onboardEntry}
            </button>
          ) : (
            <div className="mon-onboard-edit" data-testid="mon-onboard-row">
              <input
                className="mon-onboard-edit__input"
                value={onboardName}
                autoFocus
                placeholder={t.monitor.machine.onboardNamePlaceholder}
                aria-label={t.monitor.machine.onboardNamePlaceholder}
                onChange={(e) => setOnboardName(e.target.value)}
                onCompositionStart={() => {
                  onboardComposingRef.current = true;
                }}
                onCompositionEnd={(e) => {
                  onboardComposingRef.current = false;
                  setOnboardName(e.currentTarget.value);
                }}
                onKeyDown={(e) => {
                  if (
                    e.nativeEvent.isComposing ||
                    e.keyCode === 229 ||
                    onboardComposingRef.current
                  ) {
                    return;
                  }
                  if (e.key === "Enter") void addMachine();
                  if (e.key === "Escape") resetOnboardRow();
                }}
                data-testid="mon-onboard-name"
              />
              <button
                type="button"
                className="btn btn--ghost"
                disabled={onboardBusy}
                onClick={resetOnboardRow}
              >
                {t.common.cancel}
              </button>
              <button
                type="button"
                className="btn btn--accent-ghost"
                disabled={onboardBusy}
                onClick={() => void addMachine()}
                data-testid="mon-onboard-confirm"
              >
                {onboardBusy
                  ? t.monitor.machine.onboardBusy
                  : t.monitor.machine.onboardConfirm}
              </button>
            </div>
          )}
        </div>

        {/* in-place install RESULT for the server-self row (POST /bootstrap-here):
         * the `log` (surfaced verbatim — never swallowed) in a dismissible block.
         * T-ba62: shown for SUCCESS as well as failure. A successful exit code
         * does not mean a clean install — the installer's warnings ride the same
         * log, and discarding them on the ok branch is what made a warden that
         * refuses every spawn look exactly like a healthy one. */}
        {bootstrapTarget && (bootstrapResult || bootstrapError) && (
          <div className="mon-cmd" data-testid="mon-bootstrap-result-block">
            <div className="mon-cmd__head">
              <span className="mon-cmd__title">
                {t.monitor.machine.installTitle} · {bootstrapTarget.displayName}
              </span>
              <button
                type="button"
                className="btn btn--ghost mon-cmd__close"
                aria-label={t.monitor.machine.close}
                onClick={() => {
                  setBootstrapTarget(null);
                  setBootstrapResult(null);
                  setBootstrapError(null);
                }}
              >
                {t.monitor.machine.close}
              </button>
            </div>
            {bootstrapError && <div className="mon-error">{bootstrapError}</div>}
            {bootstrapResult && (
              <div
                className={
                  bootstrapResult.ok ? "mon-cmd" : "mon-cmd mon-cmd--err"
                }
              >
                <p className="mon-cmd__hint">
                  {bootstrapResult.ok
                    ? t.monitor.machine.bootstrapSucceeded
                    : t.monitor.machine.bootstrapFailed(bootstrapResult.exitCode)}
                </p>
                <pre className="mon-log" data-testid="mon-bootstrap-log">
                  {bootstrapResult.log}
                </pre>
              </div>
            )}
          </div>
        )}

        {/* install dialog (remote machines only) — a SINGLE uniform screen: copy
         * the command, run it on that machine to install the warden (the command
         * re-mints a fresh token — SECURITY: rendered into a copy control only,
         * never logged). The server-self row does NOT open this dialog (its Install
         * is an in-place bootstrap-on-server run directly). */}
        {installTarget && (
          <div
            className="mon-confirm"
            data-testid="mon-install-dialog"
            role="dialog"
            aria-modal="true"
          >
            <div className="mon-confirm__box mon-confirm__box--accent mon-install__box">
              <div className="mon-cmd__head">
                <span className="mon-confirm__title">
                  {t.monitor.machine.installTitle} · {installTarget.displayName}
                </span>
                <button
                  type="button"
                  className="btn btn--ghost mon-cmd__close"
                  aria-label={t.monitor.machine.close}
                  onClick={closeInstall}
                >
                  {t.monitor.machine.close}
                </button>
              </div>
              <p className="mon-cmd__hint">
                {t.monitor.machine.installRemoteHint}
              </p>
              <CopyBootCommandButton machineId={installTarget.machineId} />
            </div>
          </div>
        )}

        {/* uninstall guard — members are still bound to this machine. Warn (advise
         * taking them offline first) before the HOST-mutating uninstall; proceed
         * runs the same uninstall, cancel backs out. Machines with no members
         * bound skip straight to the plain confirm below. */}
        {uninstallWarnTarget && (
          <div
            className="mon-confirm"
            data-testid="mon-uninstall-warn"
            role="dialog"
            aria-modal="true"
          >
            <div className="mon-confirm__box">
              <div className="mon-confirm__title">
                {t.monitor.machine.uninstallWarnTitle}
              </div>
              <p className="mon-confirm__body">
                {t.monitor.machine.uninstallWarnBody(
                  uninstallWarnTarget.displayName,
                  membersOnMachine(uninstallWarnTarget.machineId).length
                )}
              </p>
              <ul
                className="mon-confirm__members"
                data-testid="mon-uninstall-warn-members"
              >
                {membersOnMachine(uninstallWarnTarget.machineId).map((mem) => (
                  <li key={mem.id}>{mem.name}</li>
                ))}
              </ul>
              {uninstallError && (
                <div className="mon-error">{uninstallError}</div>
              )}
              <div className="mon-confirm__actions">
                <button
                  type="button"
                  className="btn btn--ghost"
                  onClick={() => setUninstallWarnTarget(null)}
                  disabled={uninstallBusy}
                >
                  {t.common.cancel}
                </button>
                <button
                  type="button"
                  className="btn btn--danger-ghost"
                  data-testid="mon-uninstall-warn-proceed-btn"
                  onClick={() => void runUninstall(uninstallWarnTarget)}
                  disabled={uninstallBusy}
                >
                  {uninstallBusy
                    ? t.monitor.machine.uninstallBusy
                    : t.monitor.machine.uninstallWarnProceed}
                </button>
              </div>
            </div>
          </div>
        )}

        {/* uninstall confirm (二次確認) — a HOST-mutating remote action */}
        {uninstallTarget && (
          <div
            className="mon-confirm"
            data-testid="mon-uninstall-confirm"
            role="dialog"
            aria-modal="true"
          >
            <div className="mon-confirm__box">
              <div className="mon-confirm__title">
                {t.monitor.machine.uninstallConfirmTitle}
              </div>
              <p className="mon-confirm__body">
                {t.monitor.machine.uninstallConfirmBody(
                  uninstallTarget.displayName
                )}
              </p>
              {uninstallError && (
                <div className="mon-error">{uninstallError}</div>
              )}
              <div className="mon-confirm__actions">
                <button
                  type="button"
                  className="btn btn--ghost"
                  onClick={() => setUninstallTarget(null)}
                  disabled={uninstallBusy}
                >
                  {t.common.cancel}
                </button>
                <button
                  type="button"
                  className="btn btn--danger-ghost"
                  data-testid="mon-uninstall-confirm-btn"
                  onClick={() => void confirmUninstall()}
                  disabled={uninstallBusy}
                >
                  {uninstallBusy
                    ? t.monitor.machine.uninstallBusy
                    : t.monitor.machine.uninstallConfirm}
                </button>
              </div>
            </div>
          </div>
        )}

        {/* uninstall result — the machine is going offline (dispatched) OR was
         * already offline (nothing dispatched). The record is KEPT either way. */}
        {uninstallResult && (
          <div className="mon-cmd" data-testid="mon-uninstall-result">
            <div className="mon-cmd__head">
              <span className="mon-cmd__title">
                {t.monitor.machine.uninstallResultTitle} ·{" "}
                {uninstallResult.machine.displayName}
              </span>
              <button
                type="button"
                className="btn btn--ghost mon-cmd__close"
                aria-label={t.monitor.machine.close}
                onClick={() => setUninstallResult(null)}
              >
                {t.monitor.machine.close}
              </button>
            </div>
            <p className="mon-cmd__hint" data-testid="mon-uninstall-note">
              {uninstallResult.result.dispatched
                ? t.monitor.machine.uninstallDispatched
                : t.monitor.machine.uninstallAlreadyOffline}
            </p>
          </div>
        )}

        {/* delete confirm (二次確認) — a PURE roster soft-delete (no warden command) */}
        {deleteTarget && (
          <div
            className="mon-confirm"
            data-testid="mon-delete-confirm"
            role="dialog"
            aria-modal="true"
          >
            <div className="mon-confirm__box">
              <div className="mon-confirm__title">
                {t.monitor.machine.deleteConfirmTitle}
              </div>
              <p className="mon-confirm__body">
                {t.monitor.machine.deleteConfirmBody(deleteTarget.displayName)}
              </p>
              {deleteError && <div className="mon-error">{deleteError}</div>}
              <div className="mon-confirm__actions">
                <button
                  type="button"
                  className="btn btn--ghost"
                  onClick={() => setDeleteTarget(null)}
                  disabled={deleteBusy}
                >
                  {t.common.cancel}
                </button>
                <button
                  type="button"
                  className="btn btn--danger-ghost"
                  data-testid="mon-delete-confirm-btn"
                  onClick={() => void confirmDelete()}
                  disabled={deleteBusy}
                >
                  {deleteBusy
                    ? t.monitor.machine.deleteBusy
                    : t.monitor.machine.deleteConfirm}
                </button>
              </div>
            </div>
          </div>
        )}
      </section>

      {/* ── §3 AI 會話 (AI Sessions) ── */}
      <section className="mon-section">
        <div className="mon-section__title">{t.monitor.sessionsTitle}</div>
        <div className="mon-table-wrap">
          <table className="mon-table mon-table--sessions">
            <thead>
              <tr>
                <th className="mon-table__left">{t.monitor.sessionCol.member}</th>
                <th className="mon-table__left">{t.monitor.sessionCol.machine}</th>
                <th className="mon-table__left">{t.monitor.sessionCol.account}</th>
                <th className="mon-table__left">{t.monitor.sessionCol.model}</th>
                <th>🧠 {t.monitor.sessionCol.context}</th>
                <th>💲 {t.monitor.sessionCol.estCost}</th>
              </tr>
            </thead>
            <tbody>
              {aiSessions.map((s) => (
                <SessionRow
                  key={s.id}
                  session={s}
                  members={members}
                  dash={dash}
                  onOpen={() => setDetailId(s.id)}
                />
              ))}
              {/* Outsource workers (O-xx) share this one table — they are live AI
               * sessions too. Rendered AFTER the member rows through the same td
               * shape so the two read as one list; the member rows above are
               * untouched. Each cell falls back to an honest dash when the worker
               * never reported that column. */}
              {outsource.workers.map((w) => (
                <OutsourceSessionRow
                  key={w.id}
                  worker={w}
                  dash={dash}
                  // T-cf32: owner ruling — the whole row is clickable, SAME
                  // affordance as the member SessionRow above (no separate
                  // avatar hit-target; that option was shown and declined).
                  // The destination is the office page's EXISTING worker
                  // detail route (#office/worker/<id> — WorkerDetailPanel,
                  // already wired with every mutation there), reused via the
                  // shared setRoute/HashRoute helper — not a hand-built hash
                  // string, and not a duplicate panel embedded here.
                  onOpen={() => setRoute({ page: "office", workerId: w.id })}
                />
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}

/** Format a percentage, honest "—" when the source is null (never fabricated). */
function pctText(v: number | null, dash: string): string {
  return v != null ? `${v}%` : dash;
}

/** Power state for a machine row: AC (🔌) vs battery (🔋), with the battery
 * level appended when known. Honest "—" when no power source was reported. */
function powerText(
  acPower: boolean | null,
  batteryPct: number | null,
  dash: string
): string {
  if (acPower == null) return dash;
  const icon = acPower ? "🔌" : "🔋";
  return batteryPct != null ? `${icon} ${batteryPct}%` : icon;
}

/** A per-machine "copy install command" button — scenario 2 of the install
 * dialog (run it on ANOTHER machine). On click it RE-FETCHES the machine's boot
 * command (api.getMachineBootCommand re-mints a fresh token) and copies the
 * returned string to the clipboard. SECURITY: the command is a secret; it is
 * written straight to the clipboard and NEVER held in state, logged, or rendered.
 * A fetch/clipboard failure surfaces an honest error label (no fake "copied"). */
function CopyBootCommandButton({ machineId }: { machineId: string }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);

  async function run() {
    if (busy) return;
    setBusy(true);
    setFailed(false);
    try {
      const command = await api.getMachineBootCommand(machineId);
      await navigator.clipboard.writeText(command);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      // fetch or clipboard unavailable — honest error label, no fake success
      setFailed(true);
      window.setTimeout(() => setFailed(false), 2400);
    } finally {
      setBusy(false);
    }
  }

  const label = failed
    ? t.monitor.machine.copyBootCmdError
    : copied
      ? t.monitor.machine.copied
      : t.monitor.machine.copyBootCmd;

  return (
    <button
      type="button"
      className="btn btn--accent-ghost"
      data-testid="mon-copy-boot-cmd-btn"
      disabled={busy}
      onClick={() => void run()}
    >
      {copied ? <CheckIcon size={14} /> : <CopyIcon size={14} />}
      <span>{label}</span>
    </button>
  );
}

/** One session row. The member cell prefers the roster member (real name /
 * status / lastSeen); the click-through only appears when a roster match exists.
 * The effort badge shows the REAL live effort self-reported from the session's
 * telemetry (NOT the roster's owner-intent member.effort) — dash when unreported. */
function SessionRow({
  session,
  members,
  dash,
  onOpen,
}: {
  session: MonSessionView;
  members: Member[];
  dash: string;
  onOpen: () => void;
}) {
  const { t } = useI18n();
  const roster = members.find((m) => m.id === session.id);

  const name = roster?.name ?? session.name;
  const roleKey = roster?.role ?? session.role;
  // REAL live effort from the session's telemetry (self-reported statusLine);
  // "" → no badge (honest dash, never the roster owner-intent fallback).
  const effort = session.effort;

  // i18n label for a known seed key; a CUSTOM role falls back to the roster
  // member's server-resolved title (roleName), then the raw key.
  const roleLabel =
    (t.office.role as Record<string, string>)[roleKey] ??
    (roster?.roleName || roleKey);

  // Cumulative cost = live + banked, aligned with the detail panel est.$
  // (MemberDetailPanel). On the edge-triggered live→banked pop at ended
  // sessions the live cost moves into bankedCost, so summing keeps an idle
  // row's total instead of blinking to "—". Honest: dash only when BOTH are
  // null (no source at all); live/banked never overlap (no double-count).
  const totalCost =
    session.cost == null && session.bankedCost == null
      ? null
      : (session.cost ?? 0) + (session.bankedCost ?? 0);

  return (
    <tr
      className={roster ? "mon-row--clickable" : undefined}
      onClick={roster ? onOpen : undefined}
      role={roster ? "button" : undefined}
      tabIndex={roster ? 0 : undefined}
      onKeyDown={
        roster
          ? (e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                onOpen();
              }
            }
          : undefined
      }
    >
      <td className="mon-table__left" data-label={t.monitor.sessionCol.member}>
        <div className="mon-member">
          <Avatar size={34} kind="member" />
          <div className="mon-member__body">
            <div className="mon-member__name">{name}</div>
            <div className="mon-member__sub">
              {/* Roster match → the SHARED PresenceBadge (single presence
               * truth). No roster match → session-only data has no lifecycle
               * (no real presence to show), so fall back to role ONLY — never a
               * fake last-seen "Never online". */}
              {roster ? (
                <PresenceBadge member={roster} />
              ) : (
                <span>{roleLabel}</span>
              )}
            </div>
          </div>
        </div>
      </td>
      <td className="mon-table__left" data-label={t.monitor.sessionCol.machine}>
        {session.machine || dash}
      </td>
      {/* account tag joined onto the session wire (toMonSession) — honest dash
       * when the member never reported one */}
      <td
        className={`mon-table__left${session.account ? "" : " mon-muted"}`}
        data-label={t.monitor.sessionCol.account}
      >
        {session.account || dash}
      </td>
      <td className="mon-table__left" data-label={t.monitor.sessionCol.model}>
        <span className="mon-model">{session.model || dash}</span>
        {effort && <span className="mon-badge">{effort}</span>}
      </td>
      <td data-label={t.monitor.sessionCol.context}>
        {pctText(session.contextPct, dash)}
      </td>
      <td data-label={t.monitor.sessionCol.estCost}>
        {totalCost != null ? formatCost(totalCost) : dash}
      </td>
    </tr>
  );
}

/** One outsource-worker session row. Shares the SessionRow td shape so the two
 * kinds read as a single list. The member cell shows the anonymous codename
 * (O-xx) over its task context (title → type → T-xxxx) so the reader can tell
 * WHAT the worker is doing; every runtime column falls back to an honest dash
 * when the worker never reported it.
 *
 * The row IS clickable (T-cf32; owner ruling — same whole-row affordance as
 * SessionRow above, no separate avatar hit-target). This note used to say the
 * opposite ("non-clickable — a worker has no detail entry, a fake click
 * target would be dishonest"); that premise is now STALE, not current fact —
 * `WorkerDetailPanel` (frontend/src/components/WorkerDetailPanel.tsx) and its
 * route (`#office/worker/<id>`, hashRoute.ts) have existed since T-ba6b/
 * T-f190 and OfficePage's OutsourcePanel already opens them. `onOpen` (passed
 * by the caller) routes there — a REAL, already-existing destination, not an
 * invented one, so the honesty concern the old comment raised still holds; it
 * is just satisfied a different way than the member row (which stays on
 * Monitor's own `#monitor/member/<id>`; there is no monitor-scoped worker
 * route, so this one crosses to the office page that owns the panel instead
 * of duplicating it here). */
function OutsourceSessionRow({
  worker,
  dash,
  onOpen,
}: {
  worker: OutsourceWorkerView;
  dash: string;
  onOpen: () => void;
}) {
  const { t } = useI18n();

  // Task context for the sub-line: the bound task's title first, then its type
  // name, then the T-xxxx number — honest dash when none resolved.
  const context =
    worker.taskTitle || worker.taskTypeName || worker.taskNo || dash;

  // Cumulative cost = live + banked (same summing rule as the member SessionRow);
  // honest dash only when BOTH sources are null.
  const totalCost =
    worker.cost == null && worker.bankedCost == null
      ? null
      : (worker.cost ?? 0) + (worker.bankedCost ?? 0);

  return (
    <tr
      className="mon-row--clickable"
      onClick={onOpen}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onOpen();
        }
      }}
      data-testid="mon-outsource-row"
    >
      <td className="mon-table__left" data-label={t.monitor.sessionCol.member}>
        <div className="mon-member">
          <Avatar size={34} kind="outsource" />
          <div className="mon-member__body">
            <div className="mon-member__name">
              {worker.codename ? t.office.outsource.label(worker.codename) : dash}
            </div>
            <div className="mon-member__sub">
              <span>{context}</span>
            </div>
          </div>
        </div>
      </td>
      <td className="mon-table__left" data-label={t.monitor.sessionCol.machine}>
        {worker.machine || dash}
      </td>
      <td
        className={`mon-table__left${worker.account ? "" : " mon-muted"}`}
        data-label={t.monitor.sessionCol.account}
      >
        {worker.account || dash}
      </td>
      <td className="mon-table__left" data-label={t.monitor.sessionCol.model}>
        <span className="mon-model">{worker.model || dash}</span>
        {worker.effort && <span className="mon-badge">{worker.effort}</span>}
      </td>
      <td data-label={t.monitor.sessionCol.context}>
        {pctText(worker.contextPct ?? null, dash)}
      </td>
      <td data-label={t.monitor.sessionCol.estCost}>
        {totalCost != null ? formatCost(totalCost) : dash}
      </td>
    </tr>
  );
}

/** One account usage card (Monitor §1). Shape-complete for the warden slice; in
 * M1 accounts is empty so this never renders. Every metric is honest — "—" when
 * the source is null. */
function AccountCard({
  account,
  onRename,
  onDetail,
}: {
  account: MonAccountView;
  onRename: (next: string) => void;
  onDetail: () => void;
}) {
  const { t } = useI18n();
  const dash = t.monitor.dash;
  const overheated = account.sevenDay?.overheated === true;

  return (
    <div className="mon-acct">
      <div className="mon-acct__head">
        <div className="mon-acct__id">
          <span
            className={`status-dot ${
              overheated ? "status-dot--offline mon-dot--hot" : "status-dot--online"
            }`}
            aria-hidden
          />
          {/* display_name is editable; the PATCH target is the stable id
           * (account.account, wired via onRename at the call site) */}
          <InlineEdit
            value={account.displayName}
            onCommit={onRename}
            ariaLabel={t.monitor.renameAccount}
            placeholder={t.monitor.renamePlaceholder}
            displayClassName="mon-acct__name"
          />
          {/* 機器名 chip removed (T-cb1f): it read the SAME MonAccountView.machine
           * string the 帳號詳情 modal's 使用機器 row renders, so the modal is a
           * strict superset — single- or multi-machine, the text was identical.
           * Zero information lost; the card head stays for identity + rename. */}
          {/* 詳情 entry (T-a9a7) — a dedicated small button, NOT a whole-card
           * onClick (the head already hosts the InlineEdit rename click). */}
          <button
            type="button"
            className="mon-acct__detailbtn"
            onClick={onDetail}
            data-testid="mon-acct-detail-open"
          >
            {t.monitor.detail.open}
          </button>
        </div>
        <div className="mon-acct__cost">
          {t.monitor.estimate}{" "}
          {account.cost != null ? formatCost(account.cost) : dash}
        </div>
      </div>

      <UsageBar
        label={t.monitor.fiveHour}
        usagePct={account.fiveHour?.usagePct ?? null}
        timePct={account.fiveHour?.timePct ?? null}
        overheated={false}
        dash={dash}
        usageLabel={t.monitor.usage}
        timeLabel={t.monitor.time}
        overheatedLabel={t.monitor.overheated}
      />
      <UsageBar
        label={t.monitor.sevenDay}
        usagePct={account.sevenDay?.usagePct ?? null}
        timePct={account.sevenDay?.timePct ?? null}
        overheated={overheated}
        dash={dash}
        usageLabel={t.monitor.usage}
        timeLabel={t.monitor.time}
        overheatedLabel={t.monitor.overheated}
      />
    </div>
  );
}

/** Split the stable account key "<userID>/<orgUuid>" at the LAST "/" — the
 * reporter (contextreport.readClaudeAccount, T-f694) makes the second dimension
 * the org uuid OR absent, so a bare userID (no "/") yields orgUuid null.
 * Exported for unit tests. */
export function splitAccountKey(key: string): {
  userId: string;
  orgUuid: string | null;
} {
  const i = key.lastIndexOf("/");
  if (i < 0) return { userId: key, orgUuid: null };
  return { userId: key.slice(0, i), orgUuid: key.slice(i + 1) };
}

/** Parse the reporter label "<base>(<org>)" (contextreport.readClaudeAccountLabel
 * fixed contract: base — email, or displayName fallback — first, org name as
 * the TRAILING parenthesis; no parenthesis → no org). Exported for unit tests. */
export function parseAccountLabel(label: string): {
  base: string;
  org: string | null;
} {
  if (label.endsWith(")")) {
    const i = label.lastIndexOf("(");
    if (i > 0) {
      return { base: label.slice(0, i), org: label.slice(i + 1, -1) };
    }
  }
  return { base: label, org: null };
}

/** 帳號詳情 modal (T-a9a7) — the real identity behind one claude account row:
 * key 全文 / userID hash / org UUID 維度 / email / org / 回報標籤原文 / 機器 / 成本.
 * email+org derive ONLY from the owner-only accountLabel (null → honest "—",
 * never guessed from displayName). Same dim-overlay + card language as
 * MemberDetailPanel's 事件統計 window; closes via ✕, Esc, or the backdrop. */
function AccountDetailModal({
  account,
  onClose,
}: {
  account: MonAccountView;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const dash = t.monitor.dash;
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const { userId, orgUuid } = splitAccountKey(account.account);
  const label =
    account.accountLabel != null ? parseAccountLabel(account.accountLabel) : null;

  const rows: { key: string; label: string; value: string; code?: boolean }[] = [
    { key: "key", label: t.monitor.detail.accountKey, value: account.account, code: true },
    { key: "user", label: t.monitor.detail.userId, value: userId || dash, code: true },
    {
      key: "orgUuid",
      label: t.monitor.detail.orgUuid,
      value: orgUuid ?? dash,
      code: orgUuid != null,
    },
    { key: "email", label: t.monitor.detail.email, value: label?.base || dash },
    { key: "org", label: t.monitor.detail.org, value: label?.org || dash },
    {
      key: "label",
      label: t.monitor.detail.labelRaw,
      value: account.accountLabel ?? dash,
    },
    {
      key: "machines",
      label: t.monitor.detail.machines,
      value: account.machine || dash,
    },
    {
      key: "cost",
      label: t.monitor.detail.estCost,
      value: account.cost != null ? formatCost(account.cost) : dash,
    },
  ];

  return (
    <div
      className="mon-detailmodal"
      role="dialog"
      aria-modal="true"
      aria-label={t.monitor.detail.title}
      data-testid="mon-acct-detail-modal"
      onClick={onClose}
    >
      <div className="mon-detailbox" onClick={(e) => e.stopPropagation()}>
        <div className="mon-detailhead">
          <span className="mon-detailtitle">{t.monitor.detail.title}</span>
          <code className="mon-detailchip">{account.displayName}</code>
          <span className="mon-detailspacer" />
          <button
            type="button"
            className="mon-detailclose"
            aria-label={t.monitor.detail.close}
            onClick={onClose}
            data-testid="mon-acct-detail-close"
          >
            <CloseIcon size={15} />
          </button>
        </div>
        <div className="mon-detailgrid" data-testid="mon-acct-detail-body">
          {rows.map((row) => (
            <div className="mon-detailrow" key={row.key}>
              <span className="mon-detaillabel">{row.label}</span>
              {row.code && row.value !== dash ? (
                <code className="mon-detailvalue mon-detailvalue--code">
                  {row.value}
                </code>
              ) : (
                <span className="mon-detailvalue">{row.value}</span>
              )}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

/** One usage window: a "用量 X% · 時間 Y%" label + a progress bar (fill = usage,
 * marker = time). Red when the window is overheated. */
function UsageBar({
  label,
  usagePct,
  timePct,
  overheated,
  dash,
  usageLabel,
  timeLabel,
  overheatedLabel,
}: {
  label: string;
  usagePct: number | null;
  timePct: number | null;
  overheated: boolean;
  dash: string;
  usageLabel: string;
  timeLabel: string;
  overheatedLabel: string;
}) {
  const usageText = usagePct != null ? `${usagePct}%` : dash;
  const timeText = timePct != null ? `${timePct}%` : dash;
  const fillW = usagePct != null ? Math.min(100, Math.max(0, usagePct)) : 0;
  const markerL = timePct != null ? Math.min(100, Math.max(0, timePct)) : null;

  return (
    <div className="mon-usage">
      <div className="mon-usage__label">
        <span className="mon-usage__window">{label}</span>
        <span className="mon-usage__stats">
          · {usageLabel} {usageText} · {timeLabel} {timeText}
          {overheated && (
            <span className="mon-usage__hot"> · {overheatedLabel}</span>
          )}
        </span>
      </div>
      <div className="mon-usage__track">
        <div
          className={`mon-usage__fill${overheated ? " mon-usage__fill--hot" : ""}`}
          style={{ width: `${fillW}%` }}
        />
        {markerL != null && (
          <div className="mon-usage__marker" style={{ left: `${markerL}%` }} />
        )}
      </div>
    </div>
  );
}
