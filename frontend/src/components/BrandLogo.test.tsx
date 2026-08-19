// T-ea81 — BrandLogo renders the active theme's studio logo image when present,
// and falls back to the built-in LogoMark (an <svg>) otherwise.
//
// T-83ef: the logo now arrives with the ACTIVE theme's bundle, which the
// provider fetches from the server rather than being handed the whole set. The
// case therefore saves its bundle through the mock API and switches to it —
// and, because `setTheme` is token-gated, does so signed in.

import { describe, it, expect, beforeEach } from "vitest";
import { render, act, waitFor } from "@testing-library/react";
import { I18nProvider, useI18n } from "../i18n";
import { mockApi, __resetMock } from "../api/mock";
import { TOKEN_KEY } from "../api/auth";
import { BrandLogo } from "./BrandLogo";

function b64(bytes: number[]): string {
  return btoa(String.fromCharCode(...bytes));
}
const LOGO_IMG =
  "data:image/png;base64," + b64([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0]);

let ctx = null as unknown as ReturnType<typeof useI18n>;
function Capture() {
  ctx = useI18n();
  return null;
}

async function mount() {
  let result!: ReturnType<typeof render>;
  await act(async () => {
    result = render(
      <I18nProvider>
        <Capture />
        <BrandLogo size={20} />
      </I18nProvider>
    );
  });
  return result;
}

/** Sync act on purpose — see the note in Avatar.test.tsx: the browser commits
 * the new active id before the bundle fetch resolves, and the provider drops a
 * fetch that lost a race to a later switch. */
async function activate(id: string) {
  act(() => {
    ctx.setTheme(id);
  });
  await waitFor(() => expect(ctx.activeThemeBundle?.id).toBe(id));
}

describe("BrandLogo", () => {
  beforeEach(() => {
    __resetMock();
    localStorage.clear();
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
  });

  it("renders the built-in LogoMark (an svg, no img) under the office theme", async () => {
    const { container } = await mount();
    expect(container.querySelector("img.topbar__logo-img")).toBeNull();
    expect(container.querySelector("svg")).not.toBeNull();
  });

  it("renders the theme logo image when the active theme carries one", async () => {
    await mockApi.putTheme({
      id: "branded",
      name: "Branded",
      colors: { "--color-bg": "#101018" },
      logo: LOGO_IMG,
    });
    const { container } = await mount();
    await activate("branded");
    expect(
      container.querySelector("img.topbar__logo-img")?.getAttribute("src")
    ).toBe(LOGO_IMG);
  });
});
