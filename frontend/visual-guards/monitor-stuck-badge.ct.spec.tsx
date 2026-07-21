// GUARD (T-5896) — the monitor §3 AI-session STUCK badge.
//
// Contract: the owner-facing stuck-suspicion pill (data-testid="stuck-badge")
// renders in the session row ONLY when `session.stuck === true`. A false or
// absent flag must render nothing — no false alarm on a healthy session.
//
// This mounts the REAL <SessionRow> against the REAL monitor.css, so the
// assertion is driven by the production `session.stuck === true` guard, not a
// story-local copy.
//
// WIDTH: the badge is a boolean presence toggle, not a layout invariant (its
// visibility does not depend on viewport). One width is sufficient to pin the
// guard; a second width would exercise identical branch logic. (Contrast the
// long-token guards, which ARE geometry and so test 375 + 1280.)
//
// MUTANT (verified red→green): flip the `session.stuck === true` guard in
// SessionRow to a constant false (or drop the data-testid) → the "true → badge
// appears" assertion goes red while the "false → no badge" stays green.
import { test, expect } from "@playwright/experimental-ct-react";
import { MonitorStuckBadgeStory } from "./stories/MonitorStuckBadgeStory";

test("session.stuck=true → the stuck badge is visible", async ({ mount }) => {
  const cmp = await mount(<MonitorStuckBadgeStory stuck={true} />);
  const badge = cmp.getByTestId("stuck-badge");
  await expect(badge).toBeVisible();
  await expect(badge).toHaveText("無回應?");
});

test("session.stuck=false → no stuck badge renders", async ({ mount }) => {
  const cmp = await mount(<MonitorStuckBadgeStory stuck={false} />);
  await expect(cmp.getByTestId("stuck-badge")).toHaveCount(0);
});

test("session.stuck undefined → no stuck badge renders", async ({ mount }) => {
  const cmp = await mount(<MonitorStuckBadgeStory />);
  await expect(cmp.getByTestId("stuck-badge")).toHaveCount(0);
});
