// The mock's cost resets must fan the topics the cockpit listens on (found by
// independent review, T-56).
//
// This matters more than mock fidelity usually does, because the cockpit's
// reaction to a reset is entirely SSE-driven: the components do not update the
// figure themselves, they refetch when a topic says something changed. A mock
// that mutates its data and stays silent therefore rehearses a version of the
// feature where the owner presses the button and nothing on screen moves — and
// every jsdom test driving the mock would still pass.
//
// ⚠️ WHAT THIS DOES NOT PROVE: that the actor reset also clears the SECOND copy
// of the figure (the outsource worker row). The default fixture has no actor
// carrying spend, so there is nothing to clear here; that half is asserted by
// the Go tests against the real server, where one figure has one home.

import { describe, it, expect } from "vitest";
import { mockApi } from "./mock";

async function topicsDuring(run: () => Promise<unknown>): Promise<string[]> {
  const topics: string[] = [];
  const stop = mockApi.subscribeEvents((topic) => topics.push(topic));
  try {
    await run();
  } finally {
    stop();
  }
  return topics;
}

describe("mock cost resets fan the topics the cockpit listens on", () => {
  it("an account reset says monitoring changed, and touches no actor figure", async () => {
    const before = await mockApi.getMonitoring();
    const account = before.accounts[0];
    expect(account, "fixture must carry an account card").toBeTruthy();
    const actorsBefore = before.sessions.map((s) => [s.id, s.cost, s.bankedCost]);

    const topics = await topicsDuring(() =>
      mockApi.resetAccountCost(account.account)
    );

    // The signal is the whole point: without it the card keeps rendering the
    // figure that was just cleared until something else happens to refetch.
    expect(topics).toContain("monitoring");

    const after = await mockApi.getMonitoring();
    expect(
      after.accounts.find((a) => a.account === account.account)?.cost ?? null
    ).toBeNull();
    // 🔴 The separation the ruling is about (rc-5c5d7c7c6dcd): the account
    // button must not reach into any member's figure.
    expect(after.sessions.map((s) => [s.id, s.cost, s.bankedCost])).toEqual(
      actorsBefore
    );
  });

  it("an actor reset says monitoring changed too", async () => {
    const before = await mockApi.getMonitoring();
    const actor = before.sessions[0];
    expect(actor, "fixture must carry a session row").toBeTruthy();

    const topics = await topicsDuring(() => mockApi.resetMemberCost(actor.id));

    expect(topics).toContain("monitoring");
  });
});
