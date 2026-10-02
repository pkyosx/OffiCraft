// The escalation ladder 停止 → 加速停止 → 強制停止 (owner 2026-08-21), as ONE
// UPGRADING BUTTON (owner 2026-08-22, reply card rc-2afe8b557e9c option [D]).
//
// 🔴 THIS FILE'S PREMISE HAS CHANGED THREE TIMES. Each rewrite is recorded
// because the assertions below are only meaningful against the ruling they
// encode:
//   1. `stopping` REPLACES 停止 with 強制停止 — a mis-click hazard, overruled.
//   2. A FIXED THREE-SLOT row with the unreachable rungs disabled in place —
//      overruled by 「不是一開始就顯示三個按鈕」「按了才出現」.
//   3. Three REVEALED rungs standing side by side, the spent one left in place.
//      Overruled by 「停止 → 加速停止 → 強制停止 UI 顯示怪怪的，他應該體感上像是
//      同一個按鈕 升級的概念 不是不同按鈕」.
//
// So the shape under test is: exactly ONE ladder button exists at any moment,
// and climbing the ladder CHANGES it — label, action and testid together. A
// build that puts two rungs on screen at once fails the exhaustive row
// comparison; a build that never upgrades the button fails it just as loudly,
// because the comparison is a full equality against the whole rendered ladder,
// not a keyword or a substring.
//
// And the stage is NOT "the owner pressed 停止". Owner: 「應該說已經觸發軟下線的
// 人可以被觸發加速下線」 — a wind-down the SYSTEM opened at a context threshold
// counts, and that world is `refocusSince` + `refocusOp` with presence still
// plain `online`. The four fixtures below are exactly those four worlds.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import {
  MemberActionButtons,
  stopLadderStageOf,
  LADDER_ARM_MS,
  type StopLadderFacts,
  type StopLadderStage,
} from "./MemberActionButtons";

const forceLabel = zh.lifecycle.action["force-stop"];
const accelLabel = zh.lifecycle.action["accelerated-stop"];
const stopLabel = zh.lifecycle.action.stop;

/** Every id the ladder cell can ever carry. Anything outside this set is a
 * non-ladder button (喚醒) and is not this file's subject. */
const LADDER_IDS = [
  "member-action-stop",
  "member-action-accelerated-stop",
  "member-action-force-stop",
];

/** The four worlds the ruling distinguishes, as the wire actually shapes them.
 * They are deliberately NOT four spellings of the same row — presence, the
 * owner's intent and the CAUSE all differ, so a predicate that collapses any
 * two of them cannot pass this table. */
const WORLDS: {
  name: string;
  facts: StopLadderFacts;
  stage: StopLadderStage;
  /** The visual status the panels derive from the same presence word. */
  status: "online-awake" | "stopping";
}[] = [
  {
    // Awake, nobody has asked for anything. The button is 停止.
    name: "online, nothing winding down",
    facts: { lifecycle: "online", desiredState: "online", refocusSince: null, refocusOp: "" },
    stage: "none",
    status: "online-awake",
  },
  {
    // The owner pressed 停止: desired_state=offline + a live session ⇒ presence
    // `stopping`. This is the 下線 arm.
    name: "soft wind-down the OWNER opened with 停止",
    facts: { lifecycle: "stopping", desiredState: "offline", refocusSince: null, refocusOp: "" },
    stage: "soft",
    status: "stopping",
  },
  {
    // 🔴 The world the 2026-08-21 ruling was CORRECTING for. Context pressure
    // crossed the FIRST threshold, so the server stamped a refocus epoch by
    // itself — desired_state is still online and PresenceState therefore
    // projects plain `online`. Presence alone cannot tell this apart from the
    // first row, which is why presence alone is not the test.
    name: "soft wind-down the SYSTEM opened at the context threshold",
    facts: {
      lifecycle: "online",
      desiredState: "online",
      refocusSince: 1_770_000_000,
      refocusOp: "context_notice",
    },
    stage: "soft",
    status: "online-awake",
  },
  {
    // On the clock: refocus_op is one of winddownKindFor's two clocked causes.
    name: "accelerated arm",
    facts: {
      lifecycle: "stopping",
      desiredState: "offline",
      refocusSince: null,
      refocusOp: "accelerated_stop",
    },
    stage: "accelerated",
    status: "stopping",
  },
];

function renderRow(
  world: (typeof WORLDS)[number],
  over: Partial<React.ComponentProps<typeof MemberActionButtons>> = {},
) {
  return render(
    <I18nProvider>
      <MemberActionButtons
        status={world.status}
        stage={stopLadderStageOf(world.facts)}
        onSpawn={vi.fn()}
        onStop={vi.fn()}
        onAcceleratedStop={vi.fn()}
        onForceStop={vi.fn()}
        {...over}
      />
    </I18nProvider>,
  );
}

/** The WHOLE ladder as rendered — id, verbatim label and pressability, in
 * document order. Compared by full equality, so an extra rung, a missing one, a
 * reordered one or a re-worded one all fail here. */
function ladderCells(container: HTMLElement) {
  return Array.from(container.querySelectorAll("[data-testid^='member-action-']"))
    .filter((el) => LADDER_IDS.includes(el.getAttribute("data-testid") ?? ""))
    .map((el) => ({
      testid: el.getAttribute("data-testid"),
      label: el.textContent,
      disabled: (el as HTMLButtonElement).disabled,
    }));
}

afterEach(() => {
  vi.useRealTimers();
});

describe("stopLadderStageOf", () => {
  it("reads each wind-down world as its own stage", () => {
    for (const w of WORLDS) {
      expect(stopLadderStageOf(w.facts), w.name).toBe(w.stage);
    }
  });

  it("counts the system-opened wind-down, not just the one the owner pressed", () => {
    // Owner 2026-08-21: 「應該說已經觸發軟下線的人可以被觸發加速下線」. Same
    // presence word as a plain awake member — only refocus_since separates them.
    const awake = { lifecycle: "online", desiredState: "online" };
    expect(stopLadderStageOf(awake)).toBe("none");
    expect(stopLadderStageOf({ ...awake, refocusSince: 1_770_000_000, refocusOp: "context_notice" }))
      .toBe("soft");
  });

  it("reads the worker view model's presence field under its own name", () => {
    // 外包 keeps the wire name `presence`; 正職 renames it `lifecycle`. Both
    // panels call THIS function, so both spellings have to land on one answer.
    expect(stopLadderStageOf({ presence: "stopping", desiredState: "offline" })).toBe("soft");
  });

  it("never reports the accelerated arm on an actor with no wind-down open", () => {
    // A stale cause with nothing open must not conjure 強制停止 out of nothing.
    expect(
      stopLadderStageOf({ lifecycle: "online", desiredState: "online", refocusOp: "accelerated_stop" }),
    ).toBe("none");
  });
});

describe("MemberActionButtons", () => {
  it("renders ONE ladder button and UPGRADES it — never a second rung beside it", () => {
    // 🔴 MUTANT GUARD ①. This is the assertion that goes red, by name, if the
    // ladder is put back to rungs standing side by side: the comparison is the
    // ENTIRE rendered ladder against a one-element list, so a second rung — in
    // any state, disabled included — cannot pass it. It is also the assertion
    // that fails if the button stops upgrading, because the id and the label are
    // compared verbatim, per stage.
    const expected: Record<StopLadderStage, ReturnType<typeof ladderCells>> = {
      none: [{ testid: "member-action-stop", label: stopLabel, disabled: false }],
      soft: [
        { testid: "member-action-accelerated-stop", label: accelLabel, disabled: false },
      ],
      accelerated: [
        { testid: "member-action-force-stop", label: forceLabel, disabled: false },
      ],
    };
    for (const world of WORLDS) {
      const { container, unmount } = renderRow(world);
      expect(ladderCells(container), `${world.name}: the ladder cell`).toEqual(
        expected[world.stage],
      );
      unmount();
    }
  });

  it("each stage's button fires that stage's action and no other", () => {
    // 🔴 REWRITTEN FOR 2026-08-22. It used to click 加速停止 and 強制停止 on ONE
    // row, because at the accelerated stage both were on screen at once. Under
    // the single button they never coexist, so the same fact — a revealed rung
    // reaches exactly its own handler — has to be walked stage by stage.
    for (const world of WORLDS) {
      const onStop = vi.fn();
      const onAcceleratedStop = vi.fn();
      const onForceStop = vi.fn();
      const { getByTestId, unmount } = renderRow(world, {
        onStop,
        onAcceleratedStop,
        onForceStop,
      });
      const expectedId = {
        none: "member-action-stop",
        soft: "member-action-accelerated-stop",
        accelerated: "member-action-force-stop",
      }[world.stage];
      fireEvent.click(getByTestId(expectedId));
      const calls = {
        none: [1, 0, 0],
        soft: [0, 1, 0],
        accelerated: [0, 0, 1],
      }[world.stage];
      expect(
        [onStop.mock.calls.length, onAcceleratedStop.mock.calls.length, onForceStop.mock.calls.length],
        `${world.name}: stop / accelerated / force call counts`,
      ).toEqual(calls);
      unmount();
    }
  });

  it("upgrades 停止 out of existence once the wind-down it opened is running", () => {
    // 🔴 REWRITTEN FOR 2026-08-22, and this is where a whole guard was RETIRED,
    // not merely re-spelled.
    //
    // The old assertion here was: once the wind-down is running, 停止 STAYS on
    // screen in its own slot, spent (disabled, titled `alreadyStopping`), so the
    // rung that appears takes a NEW slot and the finger that just pressed 停止
    // cannot land on a harsher action. That premise is gone: the owner ruled the
    // ladder is ONE button that upgrades, which is precisely the recycled slot
    // the old assertion existed to prevent. The only survivor of that guard is
    // LADDER_ARM_MS, pinned below.
    //
    // What replaces it is the new truth, asserted just as tightly: at the soft
    // stage 停止 is ABSENT (not disabled, not renamed — absent) and the one cell
    // reads 加速停止.
    const onStop = vi.fn();
    const { queryByTestId, getByTestId } = renderRow(WORLDS[1], { onStop });
    expect(queryByTestId("member-action-stop")).toBeNull();
    const cell = getByTestId("member-action-accelerated-stop");
    expect(cell.textContent).toBe(accelLabel);
    expect((cell as HTMLButtonElement).disabled).toBe(false);
    expect(onStop).not.toHaveBeenCalled();
  });

  it("still explains itself when presence says stopping but no rung is open yet", () => {
    // The residual world for `alreadyStopping`: presence has flipped to
    // `stopping` while the wind-down facts have not (yet) put the actor on a
    // rung, so the cell is still 停止 and pressing it again would only re-stamp
    // an anchor nothing on screen reflects. It renders, it does not fire, and it
    // says why — the honesty rule the old spent-slot assertion also carried.
    const onStop = vi.fn();
    const { getByTestId } = render(
      <I18nProvider>
        <MemberActionButtons status="stopping" stage="none" onStop={onStop} />
      </I18nProvider>,
    );
    const stop = getByTestId("member-action-stop");
    expect((stop as HTMLButtonElement).disabled).toBe(true);
    expect(stop.getAttribute("title")).toBe(zh.lifecycle.reason.alreadyStopping);
    fireEvent.click(stop);
    expect(onStop).not.toHaveBeenCalled();
  });

  it("offers 加速停止, not 停止, on a wind-down the owner never asked for", () => {
    // 🔴 REWRITTEN FOR 2026-08-22, and it records a CAPABILITY LOSS rather than
    // hiding one. The old assertion was that 停止 stays LIVE here: the
    // system-opened arm is still desired-online, so 停止 there is a REAL,
    // different action (ask for the shutdown), not a repeat of one. With one
    // button per stage there is nowhere left to offer it — the single cell is
    // the stage's rung, and this stage's rung is 加速停止. Pinned so the loss is
    // visible and deliberate instead of a silent regression.
    const onStop = vi.fn();
    const onAcceleratedStop = vi.fn();
    const { queryByTestId, getByText } = renderRow(WORLDS[2], {
      onStop,
      onAcceleratedStop,
    });
    expect(queryByTestId("member-action-stop")).toBeNull();
    fireEvent.click(getByText(accelLabel));
    expect(onAcceleratedStop).toHaveBeenCalledTimes(1);
    expect(onStop).not.toHaveBeenCalled();
  });

  it("holds the button inert for LADDER_ARM_MS after it upgrades", () => {
    // 🔴 MUTANT GUARD ②, and since 2026-08-22 it is the ONLY guard between a
    // burst of clicks and the next rung: the upgrade happens in the slot the
    // finger is already resting on. Deleting the cooldown — or shortening it to
    // zero — fails here by name.
    // The window has to be long enough to be a guard at all: a "cooldown" of
    // zero, or of one frame, is the same as none for a hand that double-clicks.
    // Pinned first so shortening the constant fails HERE, with its own message,
    // rather than somewhere downstream.
    expect(
      LADDER_ARM_MS,
      "the cooldown must cover a platform double-click (~250-500ms)",
    ).toBeGreaterThanOrEqual(250);
    vi.useFakeTimers();
    const onAcceleratedStop = vi.fn();
    const { getByTestId, rerender } = render(
      <I18nProvider>
        <MemberActionButtons status="online-awake" stage="none" onStop={vi.fn()}
          onAcceleratedStop={onAcceleratedStop} onForceStop={vi.fn()} />
      </I18nProvider>,
    );
    rerender(
      <I18nProvider>
        <MemberActionButtons status="stopping" stage="soft" onStop={vi.fn()}
          onAcceleratedStop={onAcceleratedStop} onForceStop={vi.fn()} />
      </I18nProvider>,
    );
    // The upgrade is HONEST while it is inert: the new label is already showing,
    // it just will not fire yet.
    const accel = getByTestId("member-action-accelerated-stop");
    expect(accel.textContent).toBe(accelLabel);
    expect((accel as HTMLButtonElement).disabled).toBe(true);
    expect(accel.getAttribute("title")).toBe(zh.lifecycle.reason.justAppeared);
    fireEvent.click(accel);
    expect(onAcceleratedStop).not.toHaveBeenCalled();

    // One tick short of the window it is STILL inert — otherwise "there is a
    // cooldown" would be satisfied by a cooldown of a single frame.
    act(() => {
      vi.advanceTimersByTime(LADDER_ARM_MS - 1);
    });
    expect(
      (getByTestId("member-action-accelerated-stop") as HTMLButtonElement).disabled,
    ).toBe(true);

    act(() => {
      vi.advanceTimersByTime(2);
    });
    expect((getByTestId("member-action-accelerated-stop") as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(getByTestId("member-action-accelerated-stop"));
    expect(onAcceleratedStop).toHaveBeenCalledTimes(1);
  });

  it("re-arms on EVERY upgrade, so 加速停止 → 強制停止 is guarded too", () => {
    // The step that actually kills. One cooldown at the bottom of the ladder
    // would leave the last, irreversible upgrade unprotected.
    vi.useFakeTimers();
    const onForceStop = vi.fn();
    const row = (stage: StopLadderStage) => (
      <I18nProvider>
        <MemberActionButtons status="stopping" stage={stage} onStop={vi.fn()}
          onAcceleratedStop={vi.fn()} onForceStop={onForceStop} />
      </I18nProvider>
    );
    const { getByTestId, rerender } = render(row("soft"));
    rerender(row("accelerated"));
    const force = getByTestId("member-action-force-stop");
    expect((force as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(force);
    expect(onForceStop).not.toHaveBeenCalled();

    act(() => {
      vi.advanceTimersByTime(LADDER_ARM_MS + 1);
    });
    fireEvent.click(getByTestId("member-action-force-stop"));
    expect(onForceStop).toHaveBeenCalledTimes(1);
  });

  it("arms immediately when the panel OPENS on a rung that was already there", () => {
    // Nothing changed under anybody's finger — the owner navigated to it. A
    // window here would be a delay with no hazard to prevent.
    const onForceStop = vi.fn();
    const { getByText } = renderRow(WORLDS[3], { onForceStop });
    fireEvent.click(getByText(forceLabel));
    expect(onForceStop).toHaveBeenCalledTimes(1);
  });

  it("offers only 喚醒 to an actor with no session, and only 停止 to a waking one", () => {
    const rows: [
      "offline" | "stopped" | "waking",
      StopLadderStage,
      { testid: string | null; label: string | null }[],
    ][] = [
      ["offline", "accelerated", [{ testid: "member-action-spawn", label: "喚醒" }]],
      ["stopped", "accelerated", [{ testid: "member-action-spawn", label: "喚醒" }]],
      ["waking", "none", [{ testid: "member-action-stop", label: "停止" }]],
    ];
    for (const [status, stage, expected] of rows) {
      const { container, unmount } = render(
        <I18nProvider>
          <MemberActionButtons status={status} stage={stage} onSpawn={vi.fn()}
            onStop={vi.fn()} onAcceleratedStop={vi.fn()} onForceStop={vi.fn()} />
        </I18nProvider>,
      );
      const row = Array.from(container.querySelectorAll("button")).map((el) => ({
        testid: el.getAttribute("data-testid"),
        label: el.textContent,
      }));
      expect(row, status).toEqual(expected);
      unmount();
    }
  });
});
