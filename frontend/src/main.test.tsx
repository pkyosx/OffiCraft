// main.tsx — WHICH SIDE OF THE AUTH WALL the compare page is mounted on (T-59).
//
// The app root's one branch is the round's most security-relevant decision and
// it had no test: `diffRouteFromLocation` was pinned, but the ternary consuming
// it was not, so flipping the two arms left every suite green while the signed
// link — the whole point of the feature — met a login form.
//
// The three addresses below are the three arms, asserted by what actually
// reaches the DOM:
//
//   /diff?…&sig=…   the compare page and NO wall. The reader has no session and
//                   is not being asked for one; their credential is in the url.
//   /diff?…         the compare page BEHIND the wall — the internal flavour,
//                   which is a session's business like every other address.
//   anything else   the studio, untouched.
//
// AuthGate and DiffPage are stubbed: what is under test is the choice between
// them, not either one's own behaviour, and the real wall would probe
// /api/auth/status on mount.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import type { ReactNode } from "react";
import { act, screen, waitFor } from "@testing-library/react";
import type { DiffParams } from "./lib/diffLink";

vi.mock("./AuthGate", () => ({
  AuthGate: ({ authed }: { authed?: ReactNode }) => (
    <div data-testid="wall">{authed ?? <span>studio</span>}</div>
  ),
}));

vi.mock("./components/DiffPage", () => ({
  DiffPage: ({ params }: { params: DiffParams }) => (
    <div data-testid="diff-page">{params.before}</div>
  ),
}));

const SIDES = "before=att-0123456789ab&after=doc:global_context/global/current/text";

async function boot(url: string) {
  window.history.replaceState({}, "", url);
  document.body.innerHTML = '<div id="root"></div>';
  vi.resetModules();
  // The root renders on import; act() keeps that first commit inside the test's
  // own turn rather than leaking into the next one.
  await act(async () => {
    await import("./main");
  });
}

beforeEach(() => {
  window.history.replaceState({}, "", "/");
});

afterEach(() => {
  document.body.innerHTML = "";
  window.history.replaceState({}, "", "/");
});

describe("the app root's compare route", () => {
  it("mounts a SIGNED comparison ahead of the auth wall", async () => {
    await boot(`/diff?${SIDES}&sig=server-minted`);

    await waitFor(() => expect(screen.getByTestId("diff-page")).toBeTruthy());
    // The wall never rendered at all. Behind it, this reader would be shown a
    // login form for an account they do not have — over a url that already
    // carries the only credential the server was going to check.
    expect(screen.queryByTestId("wall")).toBeNull();
    expect(screen.getByTestId("diff-page").textContent).toBe("att-0123456789ab");
  });

  it("keeps an UNSIGNED comparison behind the wall", async () => {
    await boot(`/diff?${SIDES}`);

    await waitFor(() => expect(screen.getByTestId("wall")).toBeTruthy());
    // Inside the wall, not instead of it: the reader signs in and then gets the
    // comparison rather than the studio.
    expect(screen.getByTestId("wall").contains(screen.getByTestId("diff-page"))).toBe(
      true,
    );
  });

  it("leaves every other address to the studio", async () => {
    await boot("/settings");

    await waitFor(() => expect(screen.getByTestId("wall")).toBeTruthy());
    expect(screen.queryByTestId("diff-page")).toBeNull();
  });
});
