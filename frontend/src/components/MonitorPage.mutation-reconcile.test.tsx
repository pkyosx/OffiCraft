// ─────────────────────────────────────────────────────────────────────────────
// 🔴 T-10 · HOW TO TELL WHETHER A GATE IS DRAWN IN THE RIGHT PLACE
//
// Learned expensively across this file and its sibling (the other of
// MonitorPage.mutation-reconcile.test.tsx / e2e_test/tests/
// 10_settings_roles_inline.spec.js): three separate gates were written,
// reviewed and shipped while guarding something narrower than what they said
// they guarded.
//
// THE RULE. A gate's assertion message IS its specification. If a test double
// exists that makes the message FALSE while the assertion still PASSES, the
// gate is drawn in the wrong place — however sensible the expression it
// evaluates happens to look.
//
// HOW TO APPLY IT. Write the property as one sentence (the message usually is
// that sentence). Then list EVERY line that sentence depends on and build a
// double for each. The expression the gate itself reads is only ONE of those
// lines. The three worked examples, all from this ticket:
//   • "the fake must have captured subscribers" → `.length > 0` passed happily
//     on a store-last-only fake, which was precisely the bug it was added to
//     catch. Guarded "not zero"; claimed "the right one".
//   • "must FAN OUT to EVERY subscriber" → `.length > 1` still passed when the
//     emit one line below delivered to `handlers[0]` only. The sentence
//     depended on the emit; the enumeration had covered only the length.
//   • "No later poll, at any speed, can satisfy this" → true of polls, false of
//     the frame-triggered fourth request, which is neither a poll nor later.
//
// SCOPE: assertion messages AND comments, equally. The third example was a
// comment, and a confidently wrong comment is worse than none — the next
// maintainer acts on it. Anything a maintainer will act on is in scope.
//
// 🔴 THE LIMITATION, UNVARNISHED. This rule has caught three cases here, and in
// two of them it only fired because a SECOND person applied it. The author of
// these tests had already adopted the rule, applied it to his own gate, and
// still shipped the fan-out hole — he enumerated doubles for the expression the
// gate read and not for the line directly beneath it. So it is an effective
// REVIEW check and is NOT reliable as an author self-check. If the only person
// who has ever run doubles against a gate is the person who wrote it, that gate
// has not actually been checked yet.
// ─────────────────────────────────────────────────────────────────────────────
//
// MonitorPage · a machine mutation's own `member` frame must not cancel the
// reconcile it triggered (T-10, at the composition layer).
//
// WHY THIS EXISTS SEPARATELY FROM useMachines.test.ts
// The hook-level guard proves the RULE — a `member` frame landing mid-flight
// must not discard the in-flight answer. This proves the WIRING: that the
// machine mutations actually reconcile through that seam, and that the
// user-visible consequence lands without waiting for the 5s trailing poll.
// A hook test cannot see that; it never renders MonitorPage.
//
// WHY ONE CASE AND NOT FOUR
// Four machine endpoints call `putMember` and therefore publish a `member`
// frame that cancels the very refetch they just triggered (server
// api_machines.go): onboard :576, teardown-here :1180, uninstall :1219,
// delete :1299. On the client all four land in ONE code path — MonitorPage
// awaits `refetchMachines()` (the same `useMachines().refetch`) and the hook
// branches on the topic STRING. The hook cannot tell which endpoint published
// the frame, and MonitorPage uses the identical seam for each. So "uninstall
// is also fixed" is not an independent fact about uninstall; it is the same
// fact as "delete is fixed", reached by calling a different api method.
// Writing it out four times would be four copies of one assertion — the kind
// of padding that makes a suite look thorough and detect nothing extra.
//
// 🔴 AND THAT EQUIVALENCE IS AN UNGUARDED ASSUMPTION. It is a claim about how
// MonitorPage is wired TODAY — that uninstall and teardown-here still reconcile
// by awaiting the same `refetchMachines()`. Nothing enforces it. The day someone
// changes uninstall to splice the row out optimistically, or gives it its own
// reconcile path, the equivalence silently stops holding and only delete is
// still covered — this file will keep passing and will keep claiming, in the
// paragraph above, to speak for all three. If you touch any of those handlers,
// re-read that claim before trusting it.
//
// Delete is the representative because it is the most destructive of the
// dialog-driven three and because those three share a shape onboard does NOT
// have: `confirmDelete`/`runUninstall` dismiss the dialog BEFORE awaiting the
// refetch (MonitorPage.tsx :374 / :397), so under the defect the dialog closes,
// the action reports success, and the row stays on screen for up to 5s. That is
// the "I pressed it, it acknowledged, nothing happened" shape this ticket is
// about, and it is worse here than in the onboard path.
//
// ANCHORED CAUSALLY, NOT EVENTUALLY. The trailing poll is excluded by COUNTING
// REQUESTS, not by waiting: the assertions run while exactly two listMachines
// calls have been made (the mount load and the delete's own reconcile), and
// that count is asserted. A poll would be a third call, so the row can only
// have disappeared because the reconciling GET's own answer landed. Asserting
// "the row is gone eventually" would pass on the broken hook too, since the
// poll reaches the same final state ~5s later.
// (Fake timers are deliberately NOT used here: testing-library's findBy* helpers
// poll on real timers and deadlock under them.)
//
// MUTANT (proven 2026-08-27): restore `requestVersion.current += 1;` at the top
// of the monitor/member branch in useMachines.ts → this reddens on the named
// assertion "the deleted row must be gone from the reconcile the delete itself
// triggered — not from the 5s poll", not on a timeout.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import type { Member, MachineView } from "../types";
import { machineAction } from "./machineActions.testHelper";

const h = vi.hoisted(() => ({
  listMachines: vi.fn<() => Promise<unknown>>(),
  deleteMachine: vi.fn<() => Promise<unknown>>(),
  // 🔴 ALL subscribers, not the last one. MonitorPage mounts several hooks that
  // each call subscribeEvents (useMachines, useMonitoring, useMembers, useTasks,
  // useOutsourceWorkers…). Storing a single handler silently captured whichever
  // subscribed LAST, so emitting reached the wrong hook and this test passed
  // under the T-10 mutant — a false green of exactly the kind this repo has
  // booked before (a mock installed on the wrong seam never fires). The real
  // downlink fans ONE frame out to EVERY subscriber (api/http.ts), so the fake
  // must too.
  sseHandlers: [] as ((topic: string) => void)[],
}));

vi.mock("../api", () => ({
  api: {
    listMachines: () => h.listMachines(),
    deleteMachine: () => h.deleteMachine(),
    listMembers: async (): Promise<Member[]> => [],
    getMonitoring: async () => ({ accounts: [], sessions: [], machines: [] }),
    listOutsourceWorkers: async () => [],
    listTasks: async () => [],
    listTaskTypes: async () => [],
    getServerSettings: async () => ({ outsourceMaxParallel: 0 }),
    getBackupHealth: async () => ({
      status: "healthy",
      code: "",
      detail: "",
      newestBackupTs: 1785600000,
      newestBackupAgeSecs: 3600,
      staleAfterSecs: 43200,
      sinceTs: null,
      checkedTs: 1785603600,
    }),
    subscribeEvents: (cb: (topic: string) => void) => {
      h.sseHandlers.push(cb);
      return () => {
        h.sseHandlers = h.sseHandlers.filter((x) => x !== cb);
      };
    },
  },
}));

function machine(id: string, name: string): MachineView {
  return {
    machineId: id,
    displayName: name,
    online: false,
    isSelf: false,
    binStatus: null,
    wardenShape: null,
    cutoverEffect: null,
    claudeVersion: null,
    claudeCredSource: null,
    claudeSubReadable: null,
  } as MachineView;
}

const DOOMED = "Doomed";
// A machine's name is split across elements (its last character sits with the
// menu chevron), so a row is found by its name menu's accessible name.
const nameMenu = (name: string) => ({ name: `機器操作（${name}）` });
const KEPT = "Kept";

beforeEach(() => {
  h.listMachines.mockReset();
  h.deleteMachine.mockReset().mockResolvedValue({});
  h.sseHandlers = [];
});
afterEach(() => vi.useRealTimers());

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => { resolve = r; });
  return { promise, resolve };
}

describe("MonitorPage · a mutation's own member frame must not cancel its reconcile", () => {
  it("drops the deleted row from the delete's own refetch, with no poll available to repair it", async () => {
    // Mount: both machines present.
    h.listMachines.mockResolvedValueOnce([machine("m-1", DOOMED), machine("m-2", KEPT)]);
    render(
      <I18nProvider>
        <MonitorPage />
      </I18nProvider>,
    );
    expect(await screen.findByRole("button", nameMenu(DOOMED))).toBeTruthy();
    expect(h.listMachines, "mount load only, so far").toHaveBeenCalledTimes(1);

    // Open the confirm dialog and press delete.
    fireEvent.click(await machineAction("mon-delete-btn", 0));
    await screen.findByTestId("mon-delete-confirm");

    // The reconciling GET that confirmDelete awaits stays open…
    const reconcile = deferred<unknown>();
    h.listMachines.mockImplementationOnce(() => reconcile.promise);
    await act(async () => {
      fireEvent.click(screen.getByTestId("mon-delete-confirm-btn"));
      await Promise.resolve();
    });

    // …and DELETE /api/machines/{id} publishes `member` (api_machines.go :1299),
    // which arrives while that GET is still in flight.
    // 🔴 This gate guards the FAN-OUT, not merely "a subscriber exists". The
    // false green it exists to prevent was never "zero captured" — it was "ONE
    // captured, and the wrong one". `toBeGreaterThan(0)` passes happily on a
    // store-last-only fake (`h.sseHandlers = [cb]`), which is exactly the bug,
    // so it guarded nothing. Measured 2026-08-27: MonitorPage mounts 4
    // subscribers (useMachines, useMonitoring, useMembers, useTasks-family).
    // The threshold is >1 rather than ===4 deliberately: pinning the exact
    // count would redden every time a hook is legitimately added or removed,
    // which is the false-RED failure this ticket must not create. >1 is the
    // minimal property that separates "fans out" from "keeps only the last
    // one". If MonitorPage ever genuinely drops to a single subscriber this
    // reddens loudly and says why — at which point this test's premise needs
    // revisiting anyway, so a loud failure is the correct outcome.
    expect(
      h.sseHandlers.length,
      "the SSE fake must FAN OUT to every subscriber — MonitorPage mounts several, so " +
        "capturing only one means the frame is going to the wrong hook and this run proves nothing",
    ).toBeGreaterThan(1);

    // …and the gate above only checks CAPTURE. Its message says "fan out to
    // EVERY subscriber", and that claim depends on the emit below as much as on
    // the length above: emitting to `h.sseHandlers[0]` — the single-send idiom
    // used in ~13 sibling tests here, so a very natural thing to copy in —
    // leaves length at 4, sails through the gate, and delivers the frame to the
    // wrong hook. Measured: that revives the false green outright. So count what
    // was actually DELIVERED and require it to equal what was captured.
    let delivered = 0;
    act(() => {
      for (const cb of [...h.sseHandlers]) {
        delivered += 1;
        cb("member");
      }
    });
    expect(
      delivered,
      "the frame must have been DELIVERED to every captured subscriber — emitting to only one " +
        "sends it to the wrong hook and this run proves nothing",
    ).toBe(h.sseHandlers.length);

    // The GET answers, and its answer already reflects the delete.
    await act(async () => {
      reconcile.resolve([machine("m-2", KEPT)]);
      await Promise.resolve();
    });

    expect(
      h.listMachines,
      "exactly two loads: the mount and the delete's own reconcile — a trailing poll would be a third, " +
        "so nothing below can be attributed to one",
    ).toHaveBeenCalledTimes(2);
    expect(
      screen.queryByRole("button", nameMenu(DOOMED)),
      "the deleted row must be gone from the reconcile the delete itself triggered — not from the 5s poll",
    ).toBeNull();
    expect(
      screen.queryByRole("button", nameMenu(KEPT)),
      "the surviving machine must still be listed (the reconcile landed, not a wipe)",
    ).toBeTruthy();
  });
});
