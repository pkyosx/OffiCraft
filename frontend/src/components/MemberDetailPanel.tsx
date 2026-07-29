import { useState, useEffect, useRef } from "react";
import { useI18n } from "../i18n";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { api } from "../api";
import { ApiError } from "../api/errors";
import type {
  Member,
  MemberActivateResult,
  MemberRelocateResult,
} from "../types";
import { formatDuration } from "../lib/duration";
import { useMachines } from "../hooks/useMachines";
import { useWebhooks } from "../hooks/useWebhooks";
import type {
  WebhookEndpoint,
  WebhookPlatform,
  WebhookRequestLog,
  MemberResumeSummaryView,
} from "../api/adapter";
import { AgentDetailPanel, runtimeLabel } from "./AgentDetailPanel";
import { pendingChangeHint, reportedMachine } from "../lib/pendingChange";
import { Avatar } from "./Avatar";
import { avatarKindForMember } from "../lib/avatarKind";
import { ConfirmModal } from "./ConfirmModal";
import { InlineEdit } from "./InlineEdit";
import { ModelEffortEditor } from "./ModelEffortEditor";
import { presenceVisual } from "./LifecycleDot";
import type { LifecycleVisualStatus } from "./LifecycleDot";
import { PresenceBadge } from "./PresenceBadge";
import { MemberActionButtons } from "./MemberActionButtons";
import {
  CheckIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  ClockIcon,
  CloseIcon,
  CopyIcon,
  MonitorIcon,
  TrashIcon,
} from "./icons";
import { DispatchAlert } from "./DispatchAlert";
// 🔴 This panel renders its settings dialog with the .machine-picker* classes,
// so it must import their stylesheet ITSELF (T-7526). Both panels used to
// free-ride on WorkerDetailPanel → useRelocateMachine → MachinePicker →
// machine-picker.css; when the worker panel stopped driving that hook, the
// last production importer went with it and BOTH dialogs rendered unstyled.
// Style ownership follows the class names, not a transitive accident.
import "./machine-picker.css";
import "./member-detail.css";

interface MemberDetailPanelProps {
  member: Member;
  onBack: () => void;
  /** Spawn / wake / respawn a member — AND permanently rebind it — via
   * activateMember(id, machineId). The panel picks the machineId per the machine
   * picker rules (0 online → disabled, 1 → auto-use, 2+ → picker), then calls this
   * with the chosen machine.
   *
   * 🔴 The resolved value is CONSUMED (T-7fa1): `activationPending: true` means
   * the activate was accepted but NOTHING was dispatched, so the optimistic
   * 「喚醒中…」 must be rolled back instead of spinning forever. A caller that
   * returns void keeps the old (silent) behaviour — which is why the wire-up in
   * OfficePage/MonitorPage returns the adapter's result verbatim. */
  onActivate?: (
    machineId?: string,
  ) => void | Promise<MemberActivateResult | void>;
  /** Relocate the member to a machine (owner 改機器) → relocateMember(id, machineId).
   * PLACEMENT ONLY — re-pins desired_machine_id and lets the server reconcile a
   * live member onto it; unlike onActivate it never wakes the member (never
   * touches desired_state). Undefined ⇒ the 改機器 affordance is hidden.
   *
   * 🔴 Resolved value CONSUMED, same contract as onActivate (T-7fa1):
   * `relocationPending: true` = pinned but not landed. */
  onRelocate?: (
    machineId: string,
  ) => void | Promise<MemberRelocateResult | void>;
  /** Graceful stop / cancel-wake → deactivateMember (desired_state=offline). Backs the
   * Stop (online) and Cancel (waking) actions. */
  onDeactivate?: () => void;
  /** Force-stop (immediate kill) → forceStopMember. Backs the "Force stop" action
   * shown once the member is already *stopping*; the panel gates it behind a
   * confirm. May be async so the confirm can surface an in-flight state. */
  onForceStop?: () => void | Promise<void>;
  /** Manual wake (online) / refocus context → refocusMember. May be async so
   * the panel can surface an in-flight / done / error state. */
  onRefocus?: () => void | Promise<void>;
  /** Commit a rename → patchMember({ name }). */
  onRename?: (name: string) => void;
  onUpdateAvatarIndex?: (avatarIndex: number) => Promise<void>;
}

export function MemberDetailPanel({
  member,
  onBack,
  onActivate,
  onRelocate,
  onDeactivate,
  onForceStop,
  onRefocus,
  onRename,
  onUpdateAvatarIndex,
}: MemberDetailPanelProps) {
  const { t, msg } = useI18n();
  const online = member.status === "online";
  // Owner presence contract (T-2860): 機器 + Claude Account are RUNTIME facts that
  // exist only while the agent is actually up. When the member is NOT awakened
  // (presence outside online/waking) both cells must read a bare dash — never a
  // desired_machine residual (member.machine can server-resolve to the DESIRED
  // binding via observed_host's desired_state fallback) and never a stale/banked
  // monitoring-session value (joinSessionRuntime keeps joining an ended session's
  // machine/account by member id). One flag gates both cells so offline/stopping/
  // stopped all read "—"; online/waking let the real running values through.
  const awake = member.lifecycle === "online" || member.lifecycle === "waking";

  // Force-stop confirm (二次確認): a *stopping* member's Stop button escalates to an
  // IMMEDIATE kill, so it opens this confirm before firing the force-stop endpoint.
  const [forceStopConfirm, setForceStopConfirm] = useState(false);
  const [forceStopBusy, setForceStopBusy] = useState(false);
  async function confirmForceStop() {
    if (!onForceStop) return;
    setForceStopBusy(true);
    try {
      await onForceStop();
      setForceStopConfirm(false);
    } finally {
      setForceStopBusy(false);
    }
  }

  // Wake-click instant feedback: the activate POST only writes the wake INTENT —
  // server presence (waking_since → presence "waking") follows on the next
  // refetch, so without a local bridge the panel sits on the offline visual and
  // the click reads as "nothing happened". `wakePending` flips the visual to
  // "waking" the moment the wake is fired and clears as soon as the
  // server-driven lifecycle leaves the not-there states (waking/online = the
  // server caught up) — it never paints green, only the honest transition amber.
  const wakePendingClears =
    member.lifecycle !== "offline" && member.lifecycle !== "stopped";
  const [wakePending, setWakePending] = useState(false);
  // T-7fa1: the activate came back saying NOTHING was dispatched. Separate from
  // `wakePending` on purpose — pending is "we are waiting", this is "we are not
  // waiting for anything, because nothing was sent". They are never both true.
  const [wakeUndispatched, setWakeUndispatched] = useState(false);
  useEffect(() => {
    if (wakePendingClears) {
      setWakePending(false);
      // A member that actually left offline/stopped makes any earlier
      // "nothing was sent" notice stale — drop it rather than leave a
      // contradiction on screen next to a live agent.
      setWakeUndispatched(false);
    }
  }, [wakePendingClears]);
  // 🔴 Separate effect keyed on the MEMBER (review r1 SHOULD-1). Neither
  // OfficePage nor MonitorPage passes a `key`, so swapping which member the
  // panel shows is a prop change, not a remount — and if both members are
  // offline, `wakePendingClears` never changes, so the effect above does not
  // fire. Without this the notice follows the owner onto a member they never
  // tried to wake.
  useEffect(() => {
    setWakePending(false);
    setWakeUndispatched(false);
    setRelocateUndispatched(false);
    // 🔴 …and the settings DRAFT, not just the notices (independent review r3).
    // The dialog is prefilled from the member it was opened for, and neither
    // caller passes a `key`, so an open dialog survives the switch holding the
    // PREVIOUS member's runtime/model/effort/machine — one confirm and those
    // values are written to someone else. Closing it is the honest reset: the
    // owner reopens against whoever is on screen now. (useRelocateMachine does
    // the same for its picker; this hand-written twin had dropped that line.)
    setSettingsOpen(false);
    setSettingsError("");
  }, [member.id]);
  // 🔴 …and that reset is a RESET, not a CANCEL (review r2 SHOULD-1). An
  // activate still in flight when the owner switches members resolves AFTER the
  // reset has run, and writes A's verdict into a panel that is already showing
  // B — the identical on-screen lie ("B's wake was never sent") the reset above
  // was added to remove, one code path over. Written during render rather than
  // in an effect because a promise can settle between commit and effect flush;
  // same guarded render-time ref pattern ChatArea uses for its peer switch.
  const shownMemberIdRef = useRef(member.id);
  shownMemberIdRef.current = member.id;
  const wakePendingActive = wakePending && !wakePendingClears;

  // Map the REAL five-state lifecycle onto the one-per-state visual union the
  // action buttons consume — through the SHARED `presenceVisual` (T-59d6), the
  // app's one and only lifecycle→visual mapping, so this panel cannot drift
  // from the dot surfaces. HONEST: `online` maps to `online-awake` — there is no
  // awake/sleeping activity sub-axis in the backend, so there is no
  // `online-sleeping`; `error` likewise has no source and no state.
  // A pending wake surfaces as `waking` immediately (see above).
  const visual: LifecycleVisualStatus = wakePendingActive
    ? "waking"
    : presenceVisual(member.lifecycle);

  // ── Unified launch settings (喚醒 / 更改) ────────────────────────────────────
  // ONE dialog holds runtime + model + effort + machine, and it always opens (the
  // old 0/1/2+-online picker rules are gone; what survives of them is that the
  // entry button stays dead while no machine is online, with the reason in its
  // tooltip). The member's current pin is `member.desiredMachineId` — the
  // machine_id an activate binds to, and the value the machine row is seeded with.
  const { machines } = useMachines();
  const onlineMachines = machines.filter((m) => m.online);
  const firstOnlineMachineId = onlineMachines[0]?.machineId;
  // What the machine <select> may offer: every online machine, PLUS the member's
  // own pin when that machine is not online right now — labelled 離線, exactly as
  // MachinePicker does it. Without that entry the select's value would match no
  // option (blank row, pin still submitted), and dropping the pin instead would
  // move a member the owner deliberately parked. Both are "displayed ≠
  // submitted"; this is the only shape that is neither.
  // Whether the 模型 row upstairs is currently showing a reported value at all
  // (same condition the tag uses): awake, and something was reported.
  const reportedModelOnScreen = awake && (member.actualModel ?? "") !== "";
  const pinnedOfflineMachine =
    member.desiredMachineId &&
    !onlineMachines.some((m) => m.machineId === member.desiredMachineId)
      ? (machines.find((m) => m.machineId === member.desiredMachineId) ?? {
          machineId: member.desiredMachineId,
          displayName: member.desiredMachineId,
        })
      : undefined;
  const settingsMachineOptions = [
    ...onlineMachines.map((m) => ({
      machineId: m.machineId,
      label: m.displayName,
      offline: false,
    })),
    ...(pinnedOfflineMachine
      ? [
          {
            machineId: pinnedOfflineMachine.machineId,
            label: msg.machineOfflineOption(pinnedOfflineMachine.displayName),
            offline: true,
          },
        ]
      : []),
  ];
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsRuntime, setSettingsRuntime] = useState<"claude" | "codex">(
    member.runtime || "claude",
  );
  const [settingsModel, setSettingsModel] = useState(member.model);
  const [settingsEffort, setSettingsEffort] = useState(member.effort);
  const [settingsMachineId, setSettingsMachineId] = useState(
    member.desiredMachineId,
  );
  const [settingsBusy, setSettingsBusy] = useState(false);
  const [settingsError, setSettingsError] = useState("");
  const [relocateUndispatched, setRelocateUndispatched] = useState(false);

  // ⚠️ NO LONGER A TWIN (T-7526). This block used to be the hand-written copy of
  // `useRelocateMachine`'s notice hygiene, and the instruction here was "change
  // both TOGETHER" — because the outsource panel still drove the hook. It does
  // not any more: its relocate folded into the same 更改 dialog, so this is now
  // the ONLY implementation and the hook has no production importer left.
  // Do NOT go and edit `useRelocateMachine` to match a change made here — that
  // file is dead code pending an owner ruling on deleting it (see
  // docs/design/worker-panel-parity.md, 連帶後果). Kept because the reasoning
  // below is load-bearing, not because a second copy exists.
  //
  // 🔴 The relocate verdict's SELF-HEAL, carried over from useRelocateMachine
  // (which the member panel no longer drives). The notice promises "the server
  // keeps retrying in the background", so it needs a path back: without this it
  // was cleared only by ANOTHER relocate, and a move the cadence did land left
  // the panel insisting forever that it had not.
  //
  // `member.machine` is NOT a pure observation — the server's observedHost falls
  // back to desired_machine_id when nobody can see the member, which makes
  // `machine === desiredMachineId` true BY CONSTRUCTION for anyone not awake.
  // Reading that as "it arrived" would retire the notice on a move that never
  // happened, so the signal is gated on `awake` exactly like the 機器 cell, and
  // the ""/null guards keep an UNPINNED member from comparing null === null and
  // swallowing a live verdict.
  const observedMachineId = awake && member.machine ? member.machine : null;
  const relocateLanded =
    observedMachineId != null &&
    member.desiredMachineId !== "" &&
    observedMachineId === member.desiredMachineId;
  // A LATCH, not a momentary guard: once healed the verdict is dead for good, so
  // a member that later drifts off the pin cannot resurrect a verdict about an
  // attempt that is long over. (The render guard below is the momentary half —
  // it lets `landed` win before this effect flushes. Both are load-bearing;
  // deleting either leaves a lie on screen in one of the two timelines.)
  useEffect(() => {
    if (relocateLanded) setRelocateUndispatched(false);
  }, [relocateLanded]);

  // The registry arrives asynchronously. If the owner opens settings before
  // it has loaded, select the first available machine as soon as it does so
  // the unified Wake/Change action cannot be left disabled forever.
  useEffect(() => {
    if (settingsOpen && !settingsMachineId && firstOnlineMachineId) {
      setSettingsMachineId(firstOnlineMachineId);
    }
  }, [firstOnlineMachineId, settingsMachineId, settingsOpen]);

  function openSettings() {
    setSettingsRuntime(member.runtime || "claude");
    setSettingsModel(member.model);
    setSettingsEffort(member.effort);
    // 🔴 Seed the pin VERBATIM, and make sure the option list can hold it (see
    // `settingsMachineOptions`). The first cut of this fixed "displayed ≠
    // submitted" by falling back to the first ONLINE machine — which made
    // `machineChanged` unconditionally true for anyone pinned to a machine that
    // is merely asleep, so opening the dialog to edit a MODEL silently re-pinned
    // the member somewhere else. That is the same defect in the other direction,
    // and it lands hardest on "pin it to my sleeping laptop and save the model
    // for later". MachinePicker's rule is the right one: keep the bound machine
    // in the list, labelled offline, and never invent a different pin.
    setSettingsMachineId(
      member.desiredMachineId || onlineMachines[0]?.machineId || "",
    );
    setSettingsError("");
    setSettingsOpen(true);
  }

  async function runActivate(machineId: string) {
    // Read-only embeddings of the detail panel do not provide an activate
    // callback. Never paint an optimistic wake in that case: no request can
    // have been sent.
    if (!onActivate) return;
    // Instant "waking…" feedback; a rejected activate reverts to the honest
    // offline visual so the owner can retry (no stuck fake-waking).
    setWakePending(true);
    setWakeUndispatched(false); // a fresh attempt clears the previous verdict
    // WHOSE wake this is. Both branches below drop the verdict when the panel
    // has moved on to another member (review r2 SHOULD-1).
    const firedFor = member.id;
    try {
      const result = await onActivate(machineId);
      if (shownMemberIdRef.current !== firedFor) return;
      // 🔴 THE FIX (T-7fa1). An activate answers 200 either way; this is the
      // ONLY thing that distinguishes "a START went out" from "nothing was
      // dispatched". Without this branch the panel keeps painting the amber
      // 「喚醒中…」 until a lifecycle change that is never coming.
      if (result?.activationPending) {
        setWakePending(false);
        setWakeUndispatched(true);
      }
    } catch {
      if (shownMemberIdRef.current !== firedFor) return;
      setWakePending(false);
    }
  }

  /** Persist the launch settings WITHOUT starting anything (creator ruling after
   * independent review r3). Folding relocate + model/effort into one dialog had
   * silently removed two capabilities the panel used to have for an offline
   * member: editing model/effort without waking it, and re-pinning it for its
   * next wake. Neither removal was asked for — the spec describes what the WAKE
   * button does, it never says the settings become unreachable while a member is
   * off. Offered only when the member is NOT AWAKENED (offline/stopped): a live
   * member's settings change is what 更改 (graceful wind-down) is for, and for a
   * `waking` member the confirm path reaches activate, so promising "saved, not
   * started" there would be a lie. */
  async function saveSettingsOnly() {
    if (!settingsMachineId || awake) return;
    const launchChanged =
      settingsRuntime !== (member.runtime || "claude") ||
      settingsModel.trim() !== member.model ||
      settingsEffort !== member.effort;
    const machineChanged = settingsMachineId !== member.desiredMachineId;
    if (!launchChanged && !machineChanged) {
      setSettingsOpen(false);
      return;
    }
    setSettingsBusy(true);
    setSettingsError("");
    try {
      if (launchChanged) {
        await api.patchMember(member.id, {
          runtime: settingsRuntime,
          model: settingsModel.trim(),
          effort: settingsEffort,
        });
      }
      // Placement-only re-pin: the server's relocate never touches
      // desired_state, so for an offline member this is the whole honest effect
      // (it is what the retired 改機器 button did) — and it must NOT reach
      // activate, or "save without waking" would wake.
      if (machineChanged) await onRelocate?.(settingsMachineId);
      setSettingsOpen(false);
    } catch (error) {
      setSettingsError(
        (error instanceof ApiError && error.serverMessage) ||
          t.mp.modelEffortError,
      );
    } finally {
      setSettingsBusy(false);
    }
  }

  async function saveSettings() {
    if (!settingsMachineId) return;
    const launchChanged =
      settingsRuntime !== (member.runtime || "claude") ||
      settingsModel.trim() !== member.model ||
      settingsEffort !== member.effort;
    const machineChanged = settingsMachineId !== member.desiredMachineId;
    // A live Change with no edits is a true no-op. Offline and waking both use
    // this same dialog for Wake/force-revive, so they must still reach
    // activate even when the owner accepts the prefilled settings unchanged.
    if (online && !launchChanged && !machineChanged) {
      setSettingsOpen(false);
      return;
    }
    setSettingsBusy(true);
    setSettingsError("");
    // A fresh attempt drops the previous verdict (independent review r3): the
    // wake path and useRelocateMachine both do this, and without it a relocate
    // that FAILED and was then retried successfully leaves its "nothing was
    // dispatched" alert on screen — a stale notice about an attempt that is over.
    setRelocateUndispatched(false);
    try {
      // 🔴 D: the PATCH goes FIRST. This is one owner edit of one settings
      // block, and the relocate is what actually restarts the agent on the new
      // machine — so the launch intents must already be stored when it fires,
      // or the freshly spawned session comes up on the OLD model/effort and the
      // owner's edit only takes effect one handover later. Reversing these two
      // lines is exactly that bug.
      if (launchChanged) {
        await api.patchMember(member.id, {
          runtime: settingsRuntime,
          model: settingsModel.trim(),
          effort: settingsEffort,
        });
      }
      // Only a confirmed online session is gracefully relocated. A `waking`
      // member's Spawn action is the force-revive path and must reach activate.
      if (online && machineChanged) {
        // WHOSE move this verdict belongs to. The panel is given no `key` by
        // either caller, so switching members is a prop change: a relocate
        // still in flight resolves into a panel that may already be showing
        // someone else (same guard the wake path documents above).
        const firedFor = member.id;
        const result = await onRelocate?.(settingsMachineId);
        if (shownMemberIdRef.current !== firedFor) return;
        // Alert only on the FAILURE half (T-927a). `relocationPending` is also
        // true when the server deliberately deferred the move behind a graceful
        // wind-down — nothing was dispatched, but nothing went wrong either, and
        // the pending destination is already visible next to the 機器 cell. An
        // alert there taught the owner to ignore the alert.
        if (result?.relocationPending && !result.relocationDeferred) {
          setRelocateUndispatched(true);
        }
      }
      // A live member's change is finished by the two calls above: the PATCH is
      // the one graceful handover for a setting-only change, and a placement
      // change was already sent — neither may be turned into an activate, and a
      // relocate is never manufactured for a machine nobody changed. Only a
      // member that is NOT online needs starting.
      if (!online) await runActivate(settingsMachineId);
      setSettingsOpen(false);
    } catch (error) {
      // ⚠️ `mp.modelEffortError` now has TWO consumers with different scopes:
      // AgentDetailPanel's model/effort save, and this dialog's catch-all
      // fallback (any failed launch-settings submit, machine included). Its
      // wording must stay generic ("儲存失敗"). Narrowing it to model/effort
      // would make this fallback lie, and no test would go red for it.
      // 🔴 NOT `error.message` (independent review r3): every ApiError carries the
      // historical `http <status> for <METHOD> <path>` text, which frontend's
      // CLAUDE.md reserves for logs — and `ApiError extends Error`, so an
      // `instanceof Error` ternary shows it to the owner and makes the fallback
      // below dead code. The server's own envelope sentence is the only wire text
      // fit to display; anything else falls back to the dictionary.
      setSettingsError(
        (error instanceof ApiError && error.serverMessage) ||
          t.mp.modelEffortError,
      );
    } finally {
      setSettingsBusy(false);
    }
  }
  // ── 回呼端點 · WEBHOOK (M4) ───────────────────────────────────────────────
  // A collapsible section between the TMUX and initial-PROMPT cards. Webhooks
  // are external inlets bound to THIS member; the panel lists them, toggles
  // enable/disable, edits nothing but status here, and adds/deletes.
  const {
    webhooks,
    error: webhooksError,
    create: createWebhook,
    update: updateWebhook,
    remove: removeWebhook,
  } = useWebhooks(member.id);
  const [showWebhooks, setShowWebhooks] = useState(false);
  const [addingWebhook, setAddingWebhook] = useState(false);
  const [newEndpointId, setNewEndpointId] = useState("");
  const [newPurpose, setNewPurpose] = useState("");
  const [newPlatform, setNewPlatform] =
    useState<WebhookPlatform>("generic");
  const [newSigningSecret, setNewSigningSecret] = useState("");
  const [createWebhookBusy, setCreateWebhookBusy] = useState(false);
  const [createWebhookError, setCreateWebhookError] = useState(false);
  const [copiedToken, setCopiedToken] = useState<string | null>(null);
  const [toggleBusyId, setToggleBusyId] = useState<string | null>(null);
  // Signing-secret rotation, scoped to one endpoint at a time.
  const [rotateSecretId, setRotateSecretId] = useState<string | null>(null);
  const [rotateSecretValue, setRotateSecretValue] = useState("");
  const [rotateSecretBusy, setRotateSecretBusy] = useState(false);
  const [deleteWebhookTarget, setDeleteWebhookTarget] =
    useState<WebhookEndpoint | null>(null);
  const [deleteWebhookBusy, setDeleteWebhookBusy] = useState(false);
  // Per-row 事件統計 popup — stores the endpointId so the window always reads
  // the LIVE endpoint from `webhooks` (counters keep updating while open).
  const [statsEndpointId, setStatsEndpointId] = useState<string | null>(null);
  const statsWebhook =
    statsEndpointId != null
      ? (webhooks.find((wh) => wh.endpointId === statsEndpointId) ?? null)
      : null;
  // The 最近請求 list (server debug ring buffer, last 5 raw requests) is
  // fetched ONLY while the window is open: null = loading. One row at a time
  // expands to its raw headers + body.
  const [statsRequests, setStatsRequests] = useState<
    WebhookRequestLog[] | null
  >(null);
  const [statsRequestsError, setStatsRequestsError] = useState(false);
  const [expandedRequest, setExpandedRequest] = useState<number | null>(null);
  // The stats window lives inside a still-mounted panel, so it holds a layer
  // only while it is open.
  const statsModalRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(
    () => setStatsEndpointId(null),
    statsModalRef,
    statsEndpointId != null,
  );
  useEffect(() => {
    setStatsRequests(null);
    setStatsRequestsError(false);
    setExpandedRequest(null);
    if (statsEndpointId == null) return;
    let alive = true;
    api
      .listWebhookRequests(member.id, statsEndpointId)
      .then((rows) => {
        if (alive) setStatsRequests(rows);
      })
      .catch(() => {
        if (alive) setStatsRequestsError(true);
      });
    return () => {
      alive = false;
    };
  }, [statsEndpointId, member.id]);

  // The full callback URL the copy button yields (masked visually in the row).
  // Composed from the page origin so it stays portable across the tunnel host.
  function webhookUrl(token: string): string {
    return `${window.location.origin}/in?t=${token}`;
  }
  // The 事件統計 window (M4 可觀測性) helpers: an endpoint that has never seen
  // a request reads a friendly empty face; otherwise the top half renders
  // two stat blocks (last received / dropped + reason badge)
  // and the bottom half the raw 最近請求 ring buffer.
  function webhookNeverReceived(wh: WebhookEndpoint): boolean {
    return (
      wh.lastReceivedTs <= 0 &&
      wh.deliveredCount === 0 &&
      wh.droppedCount === 0
    );
  }
  function webhookDropReasonLabel(reason: string): string {
    return reason === "sig_failed"
      ? t.mp.webhook.dropReasonSigFailed
      : reason === "disabled"
        ? t.mp.webhook.dropReasonDisabled
        : reason === "member_gone"
          ? t.mp.webhook.dropReasonMemberGone
          : reason;
  }
  // Outcome → badge tone + label. Drops carry their coarse reason as
  // "dropped:<reason>" — the badge shows 丟棄 · <reason label>.
  function webhookOutcomeTone(
    outcome: string
  ): "delivered" | "dropped" | "neutral" {
    if (outcome === "delivered") return "delivered";
    if (outcome.startsWith("dropped:") || outcome === "dropped")
      return "dropped";
    return "neutral";
  }
  function webhookOutcomeLabel(outcome: string): string {
    if (outcome === "delivered") return t.mp.webhook.outcomeDelivered;
    if (outcome === "challenge") return t.mp.webhook.outcomeChallenge;
    if (outcome === "ping") return t.mp.webhook.outcomePing;
    if (outcome.startsWith("dropped:")) {
      const reason = outcome.slice("dropped:".length);
      return `${t.mp.webhook.outcomeDropped} · ${webhookDropReasonLabel(reason)}`;
    }
    return outcome;
  }
  // Raw header JSON → aligned "Name: value" lines for the expanded request
  // (falls back to the raw string when it isn't the expected JSON map).
  function webhookHeaderLines(headers: string): string {
    try {
      const parsed: unknown = JSON.parse(headers);
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
        return Object.entries(parsed as Record<string, unknown>)
          .map(
            ([k, v]) =>
              `${k}: ${Array.isArray(v) ? v.join(", ") : String(v)}`
          )
          .join("\n");
      }
    } catch {
      // truncated / non-JSON headers — show the raw text honestly
    }
    return headers;
  }

  function webhookHostPath(): string {
    try {
      return `${window.location.host}/in?t=`;
    } catch {
      return "/in?t=";
    }
  }

  async function copyWebhook(token: string) {
    try {
      await navigator.clipboard.writeText(webhookUrl(token));
      setCopiedToken(token);
      window.setTimeout(
        () => setCopiedToken((cur) => (cur === token ? null : cur)),
        1600
      );
    } catch {
      // clipboard unavailable — no fake success
    }
  }

  function resetWebhookForm() {
    setAddingWebhook(false);
    setNewEndpointId("");
    setNewPurpose("");
    setNewPlatform("generic");
    setNewSigningSecret("");
    setCreateWebhookError(false);
  }

  // slack/github require a signing secret; generic never shows the field.
  const newPlatformNeedsSecret =
    newPlatform === "slack" || newPlatform === "github";
  const createWebhookDisabled =
    createWebhookBusy ||
    newEndpointId.trim() === "" ||
    (newPlatformNeedsSecret && newSigningSecret.trim() === "");

  async function submitCreateWebhook() {
    const endpointId = newEndpointId.trim();
    if (!endpointId || createWebhookBusy) return;
    if (newPlatformNeedsSecret && newSigningSecret.trim() === "") return;
    setCreateWebhookBusy(true);
    setCreateWebhookError(false);
    try {
      await createWebhook({
        endpointId,
        purpose: newPurpose.trim(),
        platform: newPlatform,
        // Only send the secret for platforms that use it (generic ignores it).
        ...(newPlatformNeedsSecret
          ? { signingSecret: newSigningSecret.trim() }
          : {}),
      });
      resetWebhookForm();
    } catch {
      setCreateWebhookError(true);
    } finally {
      setCreateWebhookBusy(false);
    }
  }

  function startRotateSecret(endpointId: string) {
    setRotateSecretId(endpointId);
    setRotateSecretValue("");
  }

  function cancelRotateSecret() {
    setRotateSecretId(null);
    setRotateSecretValue("");
  }

  async function submitRotateSecret(endpointId: string) {
    const secret = rotateSecretValue.trim();
    if (!secret || rotateSecretBusy) return;
    setRotateSecretBusy(true);
    try {
      await updateWebhook(endpointId, { signingSecret: secret });
      cancelRotateSecret();
    } catch {
      // a refetch keeps truth; the row stays on its prior state
    } finally {
      setRotateSecretBusy(false);
    }
  }

  async function toggleWebhook(e: WebhookEndpoint) {
    if (toggleBusyId) return;
    setToggleBusyId(e.endpointId);
    try {
      await updateWebhook(e.endpointId, {
        status: e.status === "enabled" ? "disabled" : "enabled",
      });
    } catch {
      // surfaced by the list staying on its prior state; a refetch keeps truth
    } finally {
      setToggleBusyId(null);
    }
  }

  async function confirmDeleteWebhook() {
    if (!deleteWebhookTarget) return;
    setDeleteWebhookBusy(true);
    try {
      await removeWebhook(deleteWebhookTarget.endpointId);
      setDeleteWebhookTarget(null);
    } finally {
      setDeleteWebhookBusy(false);
    }
  }

  // The machine this member is ACTUALLY running on: `member.machine` is the
  // OBSERVED machine_id (server-resolved via observed_host: SSE claim → telemetry
  // → desired_state), NOT `member.desiredMachineId` (the DESIRED binding — they differ
  // after a relocate until reconcile lands). Resolve the id to the registry's
  // friendly display label (fall back to the raw id, then honest "—"). `machine`
  // is null until a position is observed → the panel shows "—", never fabricated.
  const machineName =
    machines.find((m) => m.machineId === member.machine)?.displayName ||
    member.machine ||
    "";
  const desiredMachine = machines.find(
    (m) => m.machineId === member.desiredMachineId,
  );
  const desiredMachineNameRaw =
    desiredMachine?.displayName || member.desiredMachineId || "";
  // …and if the destination is not online, SAY so here too. The option list
  // labels it 離線 two elements away; a hint that drops the label reads as a move
  // that is merely in progress, when the destination cannot accept it at all.
  const desiredMachineName =
    desiredMachine && !desiredMachine.online
      ? msg.machineOfflineOption(desiredMachineNameRaw)
      : desiredMachineNameRaw;
  // Relocation keeps the observed location truthful while making the pending
  // destination visible. Once reconcile reports the new location, the note
  // naturally disappears rather than leaving stale launch intent in the panel.
  //
  // 🔴 No `awake` gate (T-7f28). There used to be one, so re-pinning an OFFLINE
  // member showed nothing at all — the single case where the owner has no other
  // way to tell the move is still outstanding. The comparison now runs against
  // the DURABLE last landing, which is what makes that possible.
  const pendingMachine = pendingChangeHint(
    member.desiredMachineId,
    reportedMachine(member.machine ?? "", member.actualMachine ?? ""),
    msg.memberMachineMovingTo,
    desiredMachineName,
  );
  // The other three cells, same rule, same grey line. `member.runtime` /
  // `.model` / `.effort` are the owner's settings; the `actual*` twins are what
  // the agent reported.
  const pendingRuntime = pendingChangeHint(
    member.runtime || "claude",
    member.actualRuntime ?? "",
    msg.agentPendingChange,
    runtimeLabel(member.runtime || "claude"),
  );
  const pendingModel = pendingChangeHint(
    member.model,
    member.actualModel ?? "",
    msg.agentPendingChange,
  );
  const pendingEffort = pendingChangeHint(
    member.effort,
    member.actualEffort ?? "",
    msg.agentPendingChange,
  );
  // 累計總花費 = 已 banked 的歷史成本 + 當前 live session 成本(dto 保證兩者分開不重疊)。
  // honest:兩者皆無源(null)才顯 dash;任一有值則計入(缺的一方視為尚未產生成本=0)。
  const totalCost =
    member.estimatedCost == null && member.bankedCost == null
      ? null
      : (member.estimatedCost ?? 0) + (member.bankedCost ?? 0);

  const identityCard = (
    <>
      {/* identity card */}
      <div className="mp-card mp-identity">
        {/* Avatar dot dropped here: the 7-state LifecycleDot on the status line
            below is now the single source of presence colour (replaces the old
            3-state Avatar dot in this panel). */}
        <Avatar
          size={52}
          kind={avatarKindForMember(member)}
          avatarIndex={member.avatarIndex}
        />
        {onUpdateAvatarIndex && (
          <input
            className="inline-edit__input"
            type="number"
            min={0}
            step={1}
            key={member.avatarIndex ?? 0}
            defaultValue={member.avatarIndex ?? 0}
            aria-label="avatar index"
            onBlur={(event) => {
              const next = Number(event.target.value);
              if (Number.isInteger(next) && next >= 0) {
                void onUpdateAvatarIndex(next);
              }
            }}
            onKeyDown={(event) => {
              if (event.key === "Enter") event.currentTarget.blur();
            }}
          />
        )}
        <div className="mp-identity__body">
          <div className="mp-identity__line">
            <InlineEdit
              value={member.name}
              onCommit={(next) => onRename?.(next)}
              placeholder={t.mp.renamePlaceholder}
              ariaLabel={t.mp.rename}
              displayClassName="mp-identity__name"
            />
            <span className="badge mp-identity__id">{member.memberId}</span>
          </div>
          <div className="mp-identity__status">
            {/* Shared presence badge: lifecycle dot (colour = presence) + role.
                Trimmed — no status word, no last-seen (presence was expressed
                three times; the dot's colour is now the single presence signal). */}
            {/* NO role-settings gear here. It landed on the roster row
                (2faa5ce), moved into this panel (owner's 1st ruling), and
                owner 2026-07-17 moved it AGAIN — out of the panel entirely,
                onto the chat window header (ChatArea's 角色設定 button). The
                panel's status line is back to pure presence. */}
            <PresenceBadge member={member} />
          </div>
        </div>
        {/* State-ized action group replaces the single wake button. Every button
            is backed by a REAL endpoint: spawn=activate, cancel/stop=deactivate.
            (Refocus is NOT offered here — it lives with the context cell below,
            its natural home; the header no longer duplicates it. Dismiss lost its
            UI entry per owner acceptance — DELETE /api/members stays a pure
            backend seam with no button.) MemberActionButtons' button map is
            aligned to the five real presence states. */}
        <div className="mp-identity__actions">
          {/* 更改 ＋ 停止 on ONE row (owner 2026-07-31). 更改 is written FIRST so
              the reading order matches the ruling's wording and the outsource
              panel's row — the DispatchAlerts below stay in the column, because
              a verdict about a click belongs UNDER the button that was clicked,
              not beside it. */}
          <div className="mp-identity__buttons">
          {online && (
            <button
              type="button"
              className="btn btn--accent-ghost"
              data-testid="mp-change"
              onClick={openSettings}
            >
              {t.mp.change}
            </button>
          )}
          <MemberActionButtons
            status={visual}
            // Do not open a second settings flow while the first wake is in
            // flight. `waking` still renders Spawn as the recovery affordance,
            // but this local bridge keeps it honestly unavailable until the
            // activate result or server lifecycle settles.
            onSpawn={wakePendingActive || onlineMachines.length === 0 ? undefined : openSettings}
            onCancel={onDeactivate}
            onStop={onDeactivate}
            reasons={
              onlineMachines.length === 0
                ? { spawn: t.machine.noOnlineMachine }
                : undefined
            }
            // In `stopping`, the Stop button IS force-stop → open the confirm first
            // (an immediate kill that bypasses the graceful grace).
            onForceStop={
              onForceStop ? () => setForceStopConfirm(true) : undefined
            }
            // A locally pending wake precedes the server presence flip — carry
            // the instant feedback INSIDE the (disabled) wake button, the same
            // in-progress presentation the Monitor machine table uses for
            // "安裝中…", until the refetched lifecycle takes over.
            labels={
              wakePendingActive ? { spawn: t.mp.wakePendingNote } : undefined
            }
          />
          </div>
          {/* T-7fa1: sits directly under the wake button the owner just pressed
              — the click and its outcome in one place. */}
          {wakeUndispatched && (
            <DispatchAlert kind="wake" testId="mp-wake-undispatched" />
          )}
          {relocateUndispatched && !relocateLanded && (
            <DispatchAlert kind="relocate" testId="mp-relocate-undispatched" />
          )}
        </div>
      </div>
    </>
  );

  const overlayCards = (
    <>
      {forceStopConfirm && (
        <div
          className="mp-confirm"
          data-testid="mp-force-stop-confirm"
          role="dialog"
          aria-modal="true"
        >
          <div className="mp-confirm__box">
            <div className="mp-confirm__title">{t.mp.forceStopConfirmTitle}</div>
            <p className="mp-confirm__body">
              {msg.memberForceStopConfirmBody(member.name)}
            </p>
            <div className="mp-confirm__actions">
              <button
                type="button"
                className="btn btn--ghost"
                onClick={() => setForceStopConfirm(false)}
                disabled={forceStopBusy}
              >
                {t.common.cancel}
              </button>
              <button
                type="button"
                className="btn btn--danger-ghost"
                data-testid="mp-force-stop-confirm-btn"
                onClick={() => void confirmForceStop()}
                disabled={forceStopBusy}
              >
                {forceStopBusy
                  ? t.mp.forceStopBusy
                  : t.mp.forceStopConfirmAction}
              </button>
            </div>
          </div>
        </div>
      )}

      {settingsOpen && (
        <div
          className="machine-picker"
          role="dialog"
          aria-modal="true"
          data-testid="mp-settings-dialog"
        >
          <div className="machine-picker__box">
            {/* 🔴 C′: the wording follows `online`, NOT `awake`. Only a
                confirmed online session takes the graceful path (relocate /
                PATCH); a `waking` member's confirm reaches `runActivate`
                (see saveSettings' `!online` branch), which is a start, not a
                handover. Saying 「更改」 there promises a graceful wrap-up the
                click never performs — the button text would be a lie. */}
            <div className="machine-picker__title">
              {online ? t.mp.change : t.lifecycle.action.spawn}
            </div>
            {/* The other half of the same honesty fix: this dialog edits the
                CONFIGURED launch model, while the card above shows the REPORTED
                one. Tagging only the card leaves the owner looking at two
                different values under one name. */}
            <div className="mp-field__hint" data-testid="mp-settings-intent-note">
              {t.mp.settingsIntentNote}
              {/* The second half only when the card actually HAS a reported model
                  to compare against. Unconditional, it pointed at a dash and
                  invited "so this agent never reported" — which is not what an
                  empty cell means for a member that is simply not awake. */}
              {reportedModelOnScreen && ` ${t.mp.settingsIntentNoteReported}`}
            </div>
            <ModelEffortEditor
              runtime={settingsRuntime}
              model={settingsModel}
              effort={settingsEffort}
              onRuntimeChange={setSettingsRuntime}
              onModelChange={setSettingsModel}
              onEffortChange={(effort) =>
                setSettingsEffort(effort as typeof settingsEffort)
              }
            />
            <label className="machine-picker__field">
              <span className="machine-picker__label">{t.mp.machine}</span>
              <select
                className="machine-picker__select"
                value={settingsMachineId}
                onChange={(e) => setSettingsMachineId(e.target.value)}
              >
                {settingsMachineOptions.map((machine) => (
                  <option
                    key={machine.machineId}
                    value={machine.machineId}
                    // MachinePicker's other half: the offline entry exists so the
                    // owner's own pin stays visible and unchanged, NOT so a live
                    // member can be moved onto a machine whose warden is not
                    // there — that would wind the member down into nothing, and
                    // the deferred-move signal deliberately suppresses the alert.
                    // A disabled option still renders as the current value.
                    disabled={machine.offline}
                  >
                    {machine.label}
                  </option>
                ))}
              </select>
            </label>
            <div className="machine-picker__actions">
              <button type="button" className="btn btn--ghost" disabled={settingsBusy} onClick={() => setSettingsOpen(false)}>
                {t.common.cancel}
              </button>
              {!awake && (
                <button
                  type="button"
                  className="btn btn--ghost"
                  data-testid="mp-settings-save-only"
                  disabled={settingsBusy || !settingsMachineId}
                  onClick={() => void saveSettingsOnly()}
                >
                  {t.mp.settingsSaveOnly}
                </button>
              )}
              <button
                type="button"
                className="btn btn--accent"
                data-testid="mp-settings-confirm"
                disabled={settingsBusy || !settingsMachineId}
                onClick={() => void saveSettings()}
              >
                {online ? t.mp.change : t.lifecycle.action.spawn}
              </button>
            </div>
            {settingsError && <div className="mp-field__hint mp-relocate__reason">{settingsError}</div>}
          </div>
        </div>
      )}
    </>
  );

  // ── RESUME SUMMARY (T-8b0d) ────────────────────────────────────────────────
  // Below 初始 PROMPT (afterPromptCards, the panel's last slot). 🔴 HARD
  // REQUIREMENT: the panel's default load must not issue any request for this
  // section — the fetch fires ONLY on first expand. This is the initial-prompt
  // pattern (AgentDetailPanel.tsx), NOT useWebhooks' eager-prefetch shape:
  // fetch fn read through a ref (never in the effect's deps — an inline arrow
  // rebuilt every render would tear the effect down mid-flight on any repaint,
  // T-7526), effect deps `[showResumeSummary, member.id]` only, loaded-stamp
  // written on ARRIVAL (not at fetch start) so a failed read can retry.
  const [showResumeSummary, setShowResumeSummary] = useState(false);
  const [resumeSummary, setResumeSummary] = useState<{
    data: MemberResumeSummaryView | null;
    loading: boolean;
    error: boolean;
  }>({ data: null, loading: false, error: false });
  const resumeSummaryLoadedKeyRef = useRef<string | null>(null);
  const resumeSummaryInFlightKeyRef = useRef<string | null>(null);
  const resumeSummaryFetchRef = useRef<() => Promise<MemberResumeSummaryView>>(
    () => api.getMemberResumeSummary(member.id),
  );
  resumeSummaryFetchRef.current = () => api.getMemberResumeSummary(member.id);

  function runResumeSummaryFetch(key: string) {
    resumeSummaryInFlightKeyRef.current = key;
    setResumeSummary({ data: null, loading: true, error: false });
    resumeSummaryFetchRef
      .current()
      .then((data) => {
        if (resumeSummaryInFlightKeyRef.current !== key) return;
        resumeSummaryInFlightKeyRef.current = null;
        resumeSummaryLoadedKeyRef.current = key; // stamped on ARRIVAL only
        setResumeSummary({ data, loading: false, error: false });
      })
      .catch(() => {
        if (resumeSummaryInFlightKeyRef.current !== key) return;
        resumeSummaryInFlightKeyRef.current = null;
        // No stamp: the read failed, so re-expanding (or 重試) reads again.
        setResumeSummary({ data: null, loading: false, error: true });
      });
  }

  useEffect(() => {
    if (!showResumeSummary) return;
    if (resumeSummaryLoadedKeyRef.current === member.id) return;
    if (resumeSummaryInFlightKeyRef.current === member.id) return;
    runResumeSummaryFetch(member.id);
    // NO cleanup that cancels the read (a repaint/unmount is not a
    // cancellation); staleness is decided by comparing the key, not an
    // `alive` flag a repaint can flip.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [showResumeSummary, member.id]);

  const resumeSummaryCard = (
    <div className="mp-card mp-expand">
      <button
        type="button"
        className="mp-expand__head"
        aria-expanded={showResumeSummary}
        onClick={() => setShowResumeSummary((v) => !v)}
        data-testid="mp-resume-toggle"
      >
        <ClockIcon size={15} className="mp-expand__icon" />
        <span className="mp-expand__title">{t.mp.resumeSummary.title}</span>
        {showResumeSummary ? (
          <ChevronDownIcon size={16} className="mp-expand__chevron" />
        ) : (
          <ChevronRightIcon size={16} className="mp-expand__chevron" />
        )}
      </button>
      {showResumeSummary && (
        <div className="mp-expand__body" data-testid="mp-resume-body">
          {/* `!resumeSummary.data && !resumeSummary.error` covers the one
           * render tick between the toggle click and the effect's own
           * setState — treated as loading, not as a fabricated empty state. */}
          {resumeSummary.loading ||
          (!resumeSummary.data && !resumeSummary.error) ? (
            t.mp.resumeSummary.loading
          ) : resumeSummary.error ? (
            <div data-testid="mp-resume-error">
              <span>{t.mp.resumeSummary.error}</span>{" "}
              <button
                type="button"
                className="doc-btn"
                data-testid="mp-resume-retry"
                onClick={() => runResumeSummaryFetch(member.id)}
              >
                {t.mp.resumeSummary.retry}
              </button>
            </div>
          ) : resumeSummary.data ? (
            <>
              <div className="mp-resume__note">{resumeSummary.data.note}</div>
              <div
                className="mp-resume__statsgrid"
                data-testid="mp-resume-overview"
              >
                <div className="mp-resume__stat">
                  <div className="mp-resume__statlabel">
                    {t.mp.resumeSummary.chatCount}
                  </div>
                  <div
                    className="mp-resume__statvalue"
                    data-testid="mp-resume-stat-chatCount"
                  >
                    {resumeSummary.data.overview.chatCount}
                  </div>
                </div>
                <div className="mp-resume__stat">
                  <div className="mp-resume__statlabel">
                    {t.mp.resumeSummary.chatChars}
                  </div>
                  <div
                    className="mp-resume__statvalue"
                    data-testid="mp-resume-stat-chatChars"
                  >
                    {resumeSummary.data.overview.chatChars}
                  </div>
                </div>
                <div className="mp-resume__stat">
                  <div className="mp-resume__statlabel">
                    {t.mp.resumeSummary.tasksReturned}
                  </div>
                  <div
                    className="mp-resume__statvalue"
                    data-testid="mp-resume-stat-tasksReturned"
                  >
                    {resumeSummary.data.overview.tasksReturned}
                  </div>
                </div>
                <div className="mp-resume__stat">
                  <div className="mp-resume__statlabel">
                    {t.mp.resumeSummary.tasksOpenTotal}
                  </div>
                  <div
                    className="mp-resume__statvalue"
                    data-testid="mp-resume-stat-tasksOpenTotal"
                  >
                    {resumeSummary.data.overview.tasksOpenTotal}
                  </div>
                </div>
                <div className="mp-resume__stat">
                  <div className="mp-resume__statlabel">
                    {t.mp.resumeSummary.tasksDetailChars}
                  </div>
                  <div
                    className="mp-resume__statvalue"
                    data-testid="mp-resume-stat-tasksDetailChars"
                  >
                    {resumeSummary.data.overview.tasksDetailChars}
                  </div>
                </div>
                <div className="mp-resume__stat">
                  <div className="mp-resume__statlabel">
                    {t.mp.resumeSummary.cardsWaiting}
                  </div>
                  <div
                    className="mp-resume__statvalue"
                    data-testid="mp-resume-stat-cardsWaiting"
                  >
                    {resumeSummary.data.overview.cardsWaiting}
                  </div>
                </div>
                <div className="mp-resume__stat">
                  <div className="mp-resume__statlabel">
                    {t.mp.resumeSummary.cardsAnsweredRecent}
                  </div>
                  <div
                    className="mp-resume__statvalue"
                    data-testid="mp-resume-stat-cardsAnsweredRecent"
                  >
                    {resumeSummary.data.overview.cardsAnsweredRecent}
                  </div>
                </div>
              </div>

              <div className="mp-resume__section">
                <div className="mp-resume__sectiontitle">
                  {t.mp.resumeSummary.chatSection}
                </div>
                {resumeSummary.data.chat.length === 0 ? (
                  <div className="mp-resume__empty">
                    {t.mp.resumeSummary.chatEmpty}
                  </div>
                ) : (
                  resumeSummary.data.chat.map((m) => (
                    <div className="mp-resume__chatrow" key={m.id}>
                      <span className="mp-resume__chatfrom">
                        {m.from === member.id ? "→" : "←"}
                      </span>
                      <span className="mp-resume__chatbody">{m.body}</span>
                    </div>
                  ))
                )}
              </div>

              <div className="mp-resume__section">
                <div className="mp-resume__sectiontitle">
                  {t.mp.resumeSummary.tasksSection}
                </div>
                {resumeSummary.data.tasks.length === 0 ? (
                  <div className="mp-resume__empty">
                    {t.mp.resumeSummary.tasksEmpty}
                  </div>
                ) : (
                  resumeSummary.data.tasks.map((rt) => (
                    <div className="mp-resume__taskrow" key={rt.id}>
                      <code className="mp-resume__taskno">{rt.taskNo}</code>
                      <span className="mp-resume__tasktitle">{rt.title}</span>
                      <span className="mp-resume__taskstatus">
                        {rt.status}
                      </span>
                    </div>
                  ))
                )}
              </div>
            </>
          ) : (
            <div className="mp-resume__empty" data-testid="mp-resume-empty">
              {t.mp.resumeSummary.chatEmpty}
            </div>
          )}
        </div>
      )}
    </div>
  );

  const webhookCards = (
    <>
      {/* expandable: webhook endpoints (M4 回呼端點) */}
      <div className="mp-card mp-expand mp-webhook">
        <button
          type="button"
          className="mp-expand__head"
          aria-expanded={showWebhooks}
          onClick={() => setShowWebhooks((v) => !v)}
          data-testid="mp-webhook-toggle"
        >
          <MonitorIcon size={15} className="mp-expand__icon" />
          <span className="mp-expand__title">{t.mp.webhook.title}</span>
          {webhooks.length > 0 && (
            <span className="mp-webhook__count">{webhooks.length}</span>
          )}
          {showWebhooks ? (
            <ChevronDownIcon size={16} className="mp-expand__chevron" />
          ) : (
            <ChevronRightIcon size={16} className="mp-expand__chevron" />
          )}
        </button>
        {showWebhooks && (
          <div className="mp-expand__body mp-webhook__body">
            {webhooksError ? (
              <div className="mp-webhook__error">{t.mp.webhook.loadError}</div>
            ) : (
              <>
                {webhooks.length === 0 && !addingWebhook && (
                  <div className="mp-webhook__empty">{t.mp.webhook.empty}</div>
                )}
                {webhooks.map((wh) => {
                  const on = wh.status === "enabled";
                  return (
                    <div className="mp-webhook__row" key={wh.endpointId}>
                      <div className="mp-webhook__rowhead">
                        <span
                          className={`mp-webhook__dot mp-webhook__dot--${on ? "on" : "off"}`}
                        />
                        <code className="mp-webhook__chip">{wh.endpointId}</code>
                        {/* T-069d: the row-head entry reads a constant 事件統計
                            label — owner asked to keep the numbers out of the
                            row; clicking it still opens the full window. */}
                        <button
                          type="button"
                          className="mp-webhook__statssummary"
                          onClick={() => setStatsEndpointId(wh.endpointId)}
                          data-testid={`mp-webhook-stats-${wh.endpointId}`}
                        >
                          {t.mp.webhook.statsTitle}
                        </button>
                        <span className="mp-webhook__spacer" />
                        <span className="mp-webhook__statusword">
                          {on ? t.mp.webhook.enabled : t.mp.webhook.disabled}
                        </span>
                        <button
                          type="button"
                          role="switch"
                          aria-checked={on}
                          aria-label={t.mp.webhook.title + " " + wh.endpointId}
                          disabled={toggleBusyId === wh.endpointId}
                          className={`mp-toggle${on ? " mp-toggle--on" : ""}`}
                          onClick={() => toggleWebhook(wh)}
                          data-testid={`mp-webhook-toggle-${wh.endpointId}`}
                        >
                          <span className="mp-toggle__knob" />
                        </button>
                      </div>
                      {wh.purpose && (
                        <div className="mp-webhook__purpose">{wh.purpose}</div>
                      )}
                      <div className="mp-webhook__urlrow">
                        <code className="mp-webhook__url" title={t.mp.webhook.copy}>
                          {webhookHostPath()}
                          <span className="mp-webhook__mask">
                            {"•".repeat(12)}
                          </span>
                        </code>
                        <button
                          type="button"
                          className="btn mp-webhook__copy"
                          onClick={() => copyWebhook(wh.token)}
                        >
                          {copiedToken === wh.token ? (
                            <CheckIcon size={14} />
                          ) : (
                            <CopyIcon size={14} />
                          )}
                          <span>
                            {copiedToken === wh.token
                              ? t.mp.webhook.copied
                              : t.mp.webhook.copy}
                          </span>
                        </button>
                        <button
                          type="button"
                          className="mp-webhook__delete"
                          aria-label={t.mp.webhook.deleteLabel}
                          onClick={() => setDeleteWebhookTarget(wh)}
                          data-testid={`mp-webhook-delete-${wh.endpointId}`}
                        >
                          <TrashIcon size={15} />
                        </button>
                      </div>
                      {/* signing-secret rotation — only for platforms that use it */}
                      {wh.platform !== "generic" &&
                        (rotateSecretId === wh.endpointId ? (
                          <div className="mp-webhook__rotate">
                            <label className="mp-webhook__field">
                              <span className="mp-webhook__fieldlabel">
                                {t.mp.webhook.signingSecretLabel}
                              </span>
                              <input
                                type="password"
                                className="mp-webhook__input"
                                placeholder={
                                  t.mp.webhook.signingSecretPlaceholder
                                }
                                value={rotateSecretValue}
                                onChange={(e) =>
                                  setRotateSecretValue(e.target.value)
                                }
                                autoFocus
                                data-testid={`mp-webhook-rotate-input-${wh.endpointId}`}
                              />
                            </label>
                            <div className="mp-webhook__formactions">
                              <button
                                type="button"
                                className="btn"
                                onClick={cancelRotateSecret}
                                disabled={rotateSecretBusy}
                              >
                                {t.mp.webhook.cancel}
                              </button>
                              <button
                                type="button"
                                className="btn mp-webhook__submit"
                                onClick={() => submitRotateSecret(wh.endpointId)}
                                disabled={
                                  rotateSecretBusy ||
                                  rotateSecretValue.trim() === ""
                                }
                                data-testid={`mp-webhook-rotate-save-${wh.endpointId}`}
                              >
                                {t.mp.webhook.rotateSecretSave}
                              </button>
                            </div>
                          </div>
                        ) : (
                          <button
                            type="button"
                            className="mp-webhook__rotatelink"
                            onClick={() => startRotateSecret(wh.endpointId)}
                            data-testid={`mp-webhook-rotate-${wh.endpointId}`}
                          >
                            {t.mp.webhook.rotateSecret}
                          </button>
                        ))}
                    </div>
                  );
                })}

                {addingWebhook ? (
                  <div className="mp-webhook__form">
                    <label className="mp-webhook__field">
                      <span className="mp-webhook__fieldlabel">
                        {t.mp.webhook.endpointIdLabel}
                      </span>
                      <input
                        type="text"
                        className="mp-webhook__input"
                        placeholder={t.mp.webhook.endpointIdPlaceholder}
                        value={newEndpointId}
                        onChange={(e) => setNewEndpointId(e.target.value)}
                        autoFocus
                      />
                    </label>
                    <label className="mp-webhook__field">
                      <span className="mp-webhook__fieldlabel">
                        {t.mp.webhook.purposeLabel}
                      </span>
                      <input
                        type="text"
                        className="mp-webhook__input"
                        placeholder={t.mp.webhook.purposePlaceholder}
                        value={newPurpose}
                        onChange={(e) => setNewPurpose(e.target.value)}
                      />
                    </label>
                    <label className="mp-webhook__field">
                      <span className="mp-webhook__fieldlabel">
                        {t.mp.webhook.platformLabel}
                      </span>
                      <select
                        className="mp-webhook__input mp-webhook__select"
                        value={newPlatform}
                        onChange={(e) =>
                          setNewPlatform(e.target.value as WebhookPlatform)
                        }
                        data-testid="mp-webhook-platform-select"
                      >
                        <option value="generic">
                          {t.mp.webhook.platformGeneric}
                        </option>
                        <option value="slack">
                          {t.mp.webhook.platformSlack}
                        </option>
                        <option value="github">
                          {t.mp.webhook.platformGithub}
                        </option>
                      </select>
                    </label>
                    {newPlatformNeedsSecret && (
                      <>
                        <label className="mp-webhook__field">
                          <span className="mp-webhook__fieldlabel">
                            {t.mp.webhook.signingSecretLabel}
                            <span
                              className="mp-webhook__required"
                              aria-hidden="true"
                            >
                              {" *"}
                            </span>
                          </span>
                          <input
                            type="password"
                            className="mp-webhook__input"
                            placeholder={t.mp.webhook.signingSecretPlaceholder}
                            value={newSigningSecret}
                            onChange={(e) =>
                              setNewSigningSecret(e.target.value)
                            }
                            required
                            aria-required="true"
                            data-testid="mp-webhook-secret-input"
                          />
                        </label>
                        <div className="mp-webhook__helper">
                          {newPlatform === "slack"
                            ? t.mp.webhook.helperSlack
                            : t.mp.webhook.helperGithub}
                        </div>
                        {newSigningSecret.trim() === "" && (
                          <div className="mp-webhook__hint">
                            {t.mp.webhook.signingSecretRequired}
                          </div>
                        )}
                      </>
                    )}
                    {createWebhookError && (
                      <div className="mp-webhook__error">
                        {t.mp.webhook.createError}
                      </div>
                    )}
                    <div className="mp-webhook__formactions">
                      <button
                        type="button"
                        className="btn"
                        onClick={resetWebhookForm}
                        disabled={createWebhookBusy}
                      >
                        {t.mp.webhook.cancel}
                      </button>
                      <button
                        type="button"
                        className="btn mp-webhook__submit"
                        onClick={submitCreateWebhook}
                        disabled={createWebhookDisabled}
                        data-testid="mp-webhook-create"
                      >
                        {t.mp.webhook.create}
                      </button>
                    </div>
                  </div>
                ) : (
                  <button
                    type="button"
                    className="mp-webhook__add"
                    onClick={() => setAddingWebhook(true)}
                    data-testid="mp-webhook-add"
                  >
                    + {t.mp.webhook.add}
                  </button>
                )}
              </>
            )}
          </div>
        )}
      </div>

      {deleteWebhookTarget && (
        <ConfirmModal
          body={t.mp.webhook.deleteConfirm}
          cancelLabel={t.mp.webhook.cancel}
          confirmLabel={t.mp.webhook.deleteLabel}
          danger
          busy={deleteWebhookBusy}
          onCancel={() => setDeleteWebhookTarget(null)}
          onConfirm={confirmDeleteWebhook}
          testId="mp-webhook-delete-confirm"
        />
      )}

      {/* 事件統計 window — read-only observability for ONE endpoint, opened
          from the per-row link: two stat blocks up top (never-received face
          when the endpoint has no traffic at all), the raw 最近請求 ring
          buffer below (one row expands to its headers + body). Closes via ✕,
          Esc, or clicking the dimmed backdrop. T-2c1c: no endpoint chip in
          the title (the modal opens from that row — repeating it adds no
          information) and no delivered tile. */}
      {statsWebhook && (
        <div
          ref={statsModalRef}
          className="mp-webhook__statsmodal"
          role="dialog"
          aria-modal="true"
          aria-label={t.mp.webhook.statsTitle}
          data-testid="mp-webhook-stats-modal"
          onClick={() => setStatsEndpointId(null)}
        >
          <div
            className="mp-webhook__statsbox"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="mp-webhook__statshead">
              <span className="mp-webhook__statstitle">
                {t.mp.webhook.statsTitle}
              </span>
              <span className="mp-webhook__spacer" />
              <button
                type="button"
                className="mp-webhook__statsclose"
                aria-label={t.mp.webhook.statsClose}
                onClick={() => setStatsEndpointId(null)}
                data-testid="mp-webhook-stats-close"
              >
                <CloseIcon size={15} />
              </button>
            </div>
            <div
              className="mp-webhook__statsbody"
              data-testid="mp-webhook-stats-body"
            >
              {webhookNeverReceived(statsWebhook) ? (
                <div className="mp-webhook__statsempty">
                  <div className="mp-webhook__statsempty-title">
                    {t.mp.webhook.statsNever}
                  </div>
                  <div className="mp-webhook__statsempty-hint">
                    {t.mp.webhook.statsNeverHint}
                  </div>
                </div>
              ) : (
                <>
                  <div className="mp-webhook__statsgrid">
                    <div className="mp-webhook__stat">
                      <span className="mp-webhook__statlabel">
                        {t.mp.webhook.statsLastReceivedLabel}
                      </span>
                      <span className="mp-webhook__statvalue">
                        {statsWebhook.lastReceivedTs > 0
                          ? t.mp.webhook.statsAgo(
                              formatDuration(
                                Date.now() / 1000 - statsWebhook.lastReceivedTs
                              )
                            )
                          : t.mp.dash}
                      </span>
                    </div>
                    <div className="mp-webhook__stat">
                      <span className="mp-webhook__statlabel">
                        {t.mp.webhook.statsDroppedLabel}
                      </span>
                      <span
                        className={`mp-webhook__statvalue${
                          statsWebhook.droppedCount > 0
                            ? " mp-webhook__statvalue--dropped"
                            : ""
                        }`}
                      >
                        {statsWebhook.droppedCount}
                      </span>
                      {statsWebhook.droppedCount > 0 &&
                        statsWebhook.lastDropReason && (
                          <span className="mp-webhook__statnote">
                            {webhookDropReasonLabel(
                              statsWebhook.lastDropReason
                            )}
                          </span>
                        )}
                    </div>
                  </div>
                  <div
                    className="mp-webhook__requests"
                    data-testid="mp-webhook-requests"
                  >
                    <div className="mp-webhook__requeststitle">
                      {t.mp.webhook.requestsTitle}
                    </div>
                    {statsRequestsError ? (
                      <div className="mp-webhook__requestsnote">
                        {t.mp.webhook.requestsError}
                      </div>
                    ) : statsRequests == null ? (
                      <div className="mp-webhook__requestsnote">
                        {t.mp.webhook.requestsLoading}
                      </div>
                    ) : statsRequests.length === 0 ? (
                      <div className="mp-webhook__requestsnote">
                        {t.mp.webhook.requestsEmpty}
                      </div>
                    ) : (
                      <ul className="mp-webhook__requestlist">
                        {statsRequests.map((req, i) => (
                          <li className="mp-webhook__request" key={i}>
                            <button
                              type="button"
                              className="mp-webhook__requestrow"
                              aria-expanded={expandedRequest === i}
                              onClick={() =>
                                setExpandedRequest(
                                  expandedRequest === i ? null : i
                                )
                              }
                              data-testid={`mp-webhook-request-${i}`}
                            >
                              <span
                                className={`mp-webhook__outcome mp-webhook__outcome--${webhookOutcomeTone(req.outcome)}`}
                              >
                                {webhookOutcomeLabel(req.outcome)}
                              </span>
                              <span className="mp-webhook__requesttime">
                                {t.mp.webhook.statsAgo(
                                  formatDuration(Date.now() / 1000 - req.ts)
                                )}
                              </span>
                              <span className="mp-webhook__spacer" />
                              {req.truncated && (
                                <span className="mp-webhook__requesttrunc">
                                  {t.mp.webhook.requestTruncated}
                                </span>
                              )}
                              {expandedRequest === i ? (
                                <ChevronDownIcon
                                  size={14}
                                  className="mp-webhook__requestchevron"
                                />
                              ) : (
                                <ChevronRightIcon
                                  size={14}
                                  className="mp-webhook__requestchevron"
                                />
                              )}
                            </button>
                            {expandedRequest === i && (
                              <div
                                className="mp-webhook__requestdetail"
                                data-testid={`mp-webhook-request-detail-${i}`}
                              >
                                <div className="mp-webhook__requestsection">
                                  {t.mp.webhook.requestHeaders}
                                </div>
                                <pre className="mp-webhook__requestpre">
                                  {webhookHeaderLines(req.headers)}
                                </pre>
                                <div className="mp-webhook__requestsection">
                                  {t.mp.webhook.requestBody}
                                </div>
                                <pre className="mp-webhook__requestpre">
                                  {req.body || t.mp.webhook.requestBodyEmpty}
                                </pre>
                              </div>
                            )}
                          </li>
                        ))}
                      </ul>
                    )}
                  </div>
                </>
              )}
            </div>
          </div>
        </div>
      )}

    </>
  );

  return (
    <AgentDetailPanel
      onBack={onBack}
      identity={identityCard}
      overlays={overlayCards}
      extraExpandCards={webhookCards}
      afterPromptCards={resumeSummaryCard}
      vm={{
        testIdPrefix: "mp",
        online,
        runtime: member.runtime || "claude",
        // The READOUT is the reported runtime — honest dash until something
        // reports one. The configured value above only labels the account row.
        reportedRuntime: member.actualRuntime ?? "",
        pending: {
          runtime: pendingRuntime,
          model: pendingModel,
          effort: pendingEffort,
          machine: pendingMachine,
        },
        // The details panel reports what is actually running. A configured
        // launch model is intentionally kept out of this read-only surface.
        model: awake ? (member.actualModel ?? "") : "",
        modelIsReported: true,
        // Same rule as 模型 one line up: the panel states what is RUNNING. The
        // configured effort lives in the 設定 dialog below, which is the only
        // place it may be shown or written.
        effort: awake ? (member.actualEffort ?? "") : "",
        // Gate on `awake` (owner presence contract T-2860): 機器 + Claude
        // Account are runtime facts — not-awakened reads a bare dash, never a
        // desired/stale residual.
        machineText: awake ? machineName : "",
        accountText: (awake && member.account) || "",
        contextPct: member.contextPct,
        compactionCount: member.compactionCount,
        cost: totalCost,
        onRefocus: onRefocus ? async () => void (await onRefocus()) : undefined,
        refocusSince: member.refocusSince,
        refocusOp: member.refocusOp,
        refocusDeadline: member.refocusDeadline,
        refocusSubmittedNote: t.mp.refocusSubmittedNote,
        refocusSinceLabel: msg.memberRefocusSince,
        lastOp: member.lastOp,
        lastOpVerb:
          member.lastOp === "start"
            ? t.mp.lastOpStart
            : member.lastOp === "stop"
              ? t.mp.lastOpStop
              : member.lastOp,
        lastOpOk: member.lastOpOk,
        lastOpLog: member.lastOpLog,
        lastOpReason: member.lastOpReason ?? "",
        lastOpAt: member.lastOpAt,
        tmuxSession: member.tmuxSession,
        terminalHint: t.mp.terminalHint,
        // Initial boot prompt: fetched live from /api/bootstrap by ROLE (the
        // server mints NO token for a role-only preview), re-fetched when the
        // viewed member's role changes.
        prompt: {
          fetch: async () => (await api.getBootstrap(member.role)).context,
          cacheKey: member.role,
          hint: t.mp.expandableHint,
        },
      }}
    />
  );
}
