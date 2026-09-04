// AuthGate — the INTERNAL compare url is a page behind the same wall (T-59).
//
// The compare url has two flavours and only one of them is a session's
// business. The signed one never reaches this component at all (main.tsx mounts
// the page ahead of the wall — a wall in front of a page whose credential is in
// its own url would be a login form asked of someone who has no account). The
// unsigned one is exactly what the wall is for, and what it must render behind
// the wall is the COMPARISON, not the studio: the reader followed a link to one
// comparison, and a page of nav, badges and reply-card polling is neither what
// they asked for nor something this page needs a session's worth of chrome to
// draw.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";

// Same reason as AuthGate.mfa.test.tsx: USE_MOCK is read at module-evaluation
// time, so the switch has to be flipped with the hoisted vi.mock.
vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  USE_MOCK: false,
}));

import { AuthGate } from "./AuthGate";
import { api } from "./api";
import { I18nProvider } from "./i18n";
import { zh } from "./i18n/locales/zh";
import { TOKEN_KEY } from "./api/auth";

beforeEach(() => {
  localStorage.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
});

describe("AuthGate", () => {
  it("renders the given page instead of the studio once the wall is passed", async () => {
    localStorage.setItem(TOKEN_KEY, "owner-token");
    render(
      <I18nProvider>
        <AuthGate authed={<div>比較畫面</div>} />
      </I18nProvider>,
    );
    expect(screen.getByText("比較畫面")).toBeTruthy();
    // None of the studio's session-shaped chrome came with it.
    expect(screen.queryByText(zh.nav.office)).toBeNull();
  });

  it("still walls that page off when there is no session", async () => {
    const probe = vi
      .spyOn(api, "getAuthStatus")
      .mockResolvedValue({ passwordSet: true, mfaRequired: false });
    render(
      <I18nProvider>
        <AuthGate authed={<div>比較畫面</div>} />
      </I18nProvider>,
    );
    await waitFor(() => expect(probe).toHaveBeenCalled());
    await waitFor(() => expect(screen.getByText(zh.login.submit)).toBeTruthy());
    expect(screen.queryByText("比較畫面")).toBeNull();
  });
});
