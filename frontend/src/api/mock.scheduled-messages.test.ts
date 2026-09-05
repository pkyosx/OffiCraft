// mock ↔ server parity for the `custom_months` set (T-49e7 round 2).
//
// The mock is what the cockpit runs against in dev, in every jsdom suite, and
// in the CT stories, so a rule it does not implement is a rule nobody meets
// until production. `custom_months` carries the only ABSENT-MEANS-SOMETHING
// contract in this feature, and it has three readings that must stay apart:
//
//   omitted   every month (1-12) — a client written before round 2 never sends
//             the field, and its schedules always did fire every month.
//   []        the caller asked for a schedule that never fires → 422.
//   listed    exactly those months, 1-12.
//
// The server draws the line in `resolveCustomMonths` (api_scheduled_messages.go)
// BEFORE `ValidateScheduledMessageCustomSets` ever sees the row, which is why
// nothing downstream there has to guess which of the three it is looking at.
// `resolveMockMonths` is the twin of that function and these cases are its
// contract.

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi } from "./mock";
import { ApiError } from "./errors";
import type { ScheduledMessage } from "./adapter";

const ALL_MONTHS = Array.from({ length: 12 }, (_, i) => i + 1);
const MEMBER = "mira";

/** The three sets that are NOT months, so each case varies one thing. */
const OTHER_SETS = {
  customDays: [1, 15],
  customHours: [9],
  customMinutes: [0, 30],
};

/** Remove every schedule this file created, so the seeded mira fixture is the
 * only row a later case can see. The mock's store is module-level. */
async function clean(): Promise<void> {
  const rows = await mockApi.listScheduledMessages(MEMBER);
  for (const r of rows) {
    if (r.label.startsWith("T49E7 ")) {
      await mockApi.deleteScheduledMessage(MEMBER, r.id);
    }
  }
}

beforeEach(clean);

async function create(
  over: Partial<{
    customMonths: number[];
    customDays: number[];
    cadence: "daily" | "custom";
  }> = {}
): Promise<ScheduledMessage> {
  const { cadence = "custom" as const, ...rest } = over;
  const { id } = await mockApi.createScheduledMessage(MEMBER, {
    label: "T49E7 " + Math.random().toString(16).slice(2),
    body: "巡檢",
    cadence,
    timezone: "Asia/Taipei",
    ...(cadence === "custom" ? OTHER_SETS : { hour: 9, minute: 0 }),
    ...rest,
  });
  return readRow(id);
}

/** The stored row for `id`. T-91: create/update answer the minted id only, so
 * every assertion about what a write STORED reads the row back — the same
 * round the cockpit itself makes (useScheduledMessages refetches). */
async function readRow(id: string): Promise<ScheduledMessage> {
  return (await mockApi.listScheduledMessages(MEMBER)).find((r) => r.id === id)!;
}

/** The 422 an invalid set earns, or `null` when the call resolved. */
async function refusal(run: () => Promise<unknown>): Promise<ApiError | null> {
  try {
    await run();
    return null;
  } catch (e) {
    if (e instanceof ApiError) return e;
    throw e;
  }
}

describe("mockApi.createScheduledMessage", () => {
  it("reads an OMITTED custom_months as the whole year, listed out", async () => {
    const created = await create();
    // Listed, not a sentinel: the mock world's rows have to look like the
    // server's, where the whole year is twelve numbers and never an empty
    // "means everything" column.
    expect(created.customMonths).toEqual(ALL_MONTHS);
    // …and the list read gives the same row back, so the cockpit renders
    // twelve ticked boxes rather than an empty months group.
    const listed = (await mockApi.listScheduledMessages(MEMBER)).find(
      (r) => r.id === created.id
    );
    expect(listed!.customMonths).toEqual(ALL_MONTHS);
  });

  it("refuses an EXPLICITLY empty custom_months with a 422 instead of reading it as every month", async () => {
    const err = await refusal(() => create({ customMonths: [] }));
    expect(err).not.toBeNull();
    expect(err!.status).toBe(422);
    expect(err!.serverMessage).toContain("custom_months");
    // The hint is the whole point of refusing rather than defaulting: it tells
    // the caller which of the two shapes it should have sent.
    expect(err!.serverMessage).toContain("OMIT");
    // And nothing landed — a refusal that half-applied would be worse than none.
    expect(await mockApi.listScheduledMessages(MEMBER)).toHaveLength(1);
  });

  it("keeps a stated months set verbatim and refuses one outside 1-12", async () => {
    const created = await create({ customMonths: [12, 3, 3, 6] });
    // Sorted and deduplicated, the way the server canonicalises at the write
    // seam — so re-saving the same choice in another order is not a change.
    expect(created.customMonths).toEqual([3, 6, 12]);

    for (const bad of [[0], [13], [1, 13]]) {
      const err = await refusal(() => create({ customMonths: bad }));
      expect(err, `custom_months ${JSON.stringify(bad)} was accepted`).not.toBeNull();
      expect(err!.status).toBe(422);
      expect(err!.serverMessage).toContain("1-12");
    }
  });

  it("does not apply the empty-set refusal to a non-custom cadence", async () => {
    // The server folds an empty array to nil on the way in and only judges the
    // sets when the cadence is `custom`; a mock that refused this would teach
    // the caller a rule the wire does not have.
    const daily = await create({ cadence: "daily", customMonths: [] });
    expect(daily.cadence).toBe("daily");
    expect(daily.customMonths).toEqual([]);
    // …and an omitted months on a non-custom row stays empty rather than being
    // backfilled to the whole year: nothing reads it, and a row that listed
    // twelve months it never consults would read as a choice somebody made.
    const alsoDaily = await create({ cadence: "daily" });
    expect(alsoDaily.customMonths).toEqual([]);
  });

  it("refuses a months × days pair that no calendar ever has, and keeps the leap-year one", async () => {
    // Every value is in range and no set is empty, so nothing else in the
    // validator objects — yet these three deliver nothing for the rest of time.
    for (const [months, days] of [
      [[2], [30]],
      [[2], [31]],
      [[4, 6, 9, 11], [31]],
    ]) {
      const err = await refusal(() =>
        create({ customMonths: months, customDays: days })
      );
      expect(
        err,
        `months ${JSON.stringify(months)} × days ${JSON.stringify(days)} was accepted`
      ).not.toBeNull();
      expect(err!.status).toBe(422);
      // Readable enough to act on: which two sets, and what to change.
      expect(err!.serverMessage).toContain("custom_months");
      expect(err!.serverMessage).toContain("custom_days");
      expect(err!.serverMessage).toContain("never occur together");
    }

    // The positive sample the line was drawn around: February with the 29th is
    // a leap-year schedule somebody meant to write.
    const leap = await create({ customMonths: [2], customDays: [29] });
    expect(leap.customMonths).toEqual([2]);
    expect(leap.customDays).toEqual([29]);
  });
});

describe("mockApi.updateScheduledMessage", () => {
  it("leaves the stored months alone when the patch does not mention them", async () => {
    const created = await create({ customMonths: [3, 6, 9, 12] });
    await mockApi.updateScheduledMessage(MEMBER, created.id, {
      label: "T49E7 改名",
    });
    expect((await readRow(created.id)).customMonths).toEqual([3, 6, 9, 12]);
  });

  it("gives a never-custom row the whole year when it switches to custom without naming months", async () => {
    // 🔴 This is the shape that keeps a pre-round-2 client working: it flips a
    // daily schedule to `custom`, sends the three sets it knows about, and
    // never mentions months. Resolving against the cadence the row will HAVE
    // (not the one it had) is what lets that land as every month instead of as
    // a 422 or, worse, a schedule that never fires.
    const daily = await create({ cadence: "daily" });
    expect(daily.customMonths).toEqual([]);
    await mockApi.updateScheduledMessage(MEMBER, daily.id, {
      cadence: "custom",
      ...OTHER_SETS,
    });
    expect((await readRow(daily.id)).customMonths).toEqual(ALL_MONTHS);
  });

  it("refuses an explicitly empty months patch and changes nothing", async () => {
    const created = await create({ customMonths: [5] });
    const err = await refusal(() =>
      mockApi.updateScheduledMessage(MEMBER, created.id, {
        label: "T49E7 不該被寫進去",
        customMonths: [],
      })
    );
    expect(err!.status).toBe(422);
    const after = (await mockApi.listScheduledMessages(MEMBER)).find(
      (r) => r.id === created.id
    )!;
    // The label rode in the SAME request. A mock that validated after applying
    // would leave a half-written row behind, and the cockpit would show an
    // error over a value that had already changed.
    expect(after.label).toBe(created.label);
    expect(after.customMonths).toEqual([5]);
  });

  it("refuses a months patch that narrows the row onto a date it can never reach", async () => {
    // January has a 31st, so this row is perfectly ordinary when it is created.
    const created = await create({ customMonths: [1, 2], customDays: [31] });
    const err = await refusal(() =>
      mockApi.updateScheduledMessage(MEMBER, created.id, {
        label: "T49E7 不該被寫進去",
        customMonths: [2],
      })
    );
    expect(err!.status).toBe(422);
    // The days set is not in this request at all, so the mock has to judge the
    // whole row the way the server does rather than only what was stated.
    expect(err!.serverMessage).toContain("custom_days");
    const after = (await mockApi.listScheduledMessages(MEMBER)).find(
      (r) => r.id === created.id
    )!;
    expect(after.label).toBe(created.label);
    expect(after.customMonths).toEqual([1, 2]);
  });

  it("keeps the months a row carried after it switches AWAY from custom", async () => {
    const created = await create({ customMonths: [4] });
    await mockApi.updateScheduledMessage(MEMBER, created.id, {
      cadence: "daily",
      hour: 9,
      minute: 0,
    });
    // Parked, not cleared — switching back must not lose the choice, which is
    // exactly what the PATCH clause of the frozen spec promises.
    expect((await readRow(created.id)).customMonths).toEqual([4]);
    await mockApi.updateScheduledMessage(MEMBER, created.id, {
      cadence: "custom",
    });
    expect((await readRow(created.id)).customMonths).toEqual([4]);
  });

});

// ⚠️ DELIBERATELY NOT TESTED HERE: that a months edit re-aims the delivery
// cursor. The mock's `reAimed` clause carries `patch.customMonths` for symmetry
// with the server, but `mockScheduleSlot` derives the cursor string from
// hour/minute/timezone ALONE and the mock runs no tick loop — so on a `custom`
// row the "re-aimed" value is byte-identical to the old one and any assertion
// either way is green on both branches. The real guard is the server's
// TestPatchingMonthsReAimsTheCursorOnlyWhenTheyChange; writing a mock twin of it
// would only look like coverage.
