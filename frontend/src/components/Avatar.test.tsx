// T-16a1 P5 — the Avatar picks a per-member-type image off the ACTIVE theme,
// and falls back to the built-in glyph when the theme carries none (office
// never degrades). jsdom is enough: this is DOM-shape logic (which node renders
// for a given kind), not geometry.
//
// T-83ef: the theme a member's avatar comes from is no longer handed to the
// provider by the test — it lives on the server and the provider FETCHES the
// active one. So each case saves its bundle through the mock API and then
// switches to it, exactly as the cockpit does. Two consequences are product
// behaviour, not test scaffolding: a signed-out cockpit never fetches a bundle
// at all (`setTheme` is token-gated), and the paint lands one round trip after
// the switch — hence the token in beforeEach and the waits below.

import { describe, it, expect, beforeEach } from "vitest";
import { render, act, waitFor, fireEvent } from "@testing-library/react";
import type { ReactNode } from "react";
import { I18nProvider, useI18n } from "../i18n";
import { mockApi, __resetMock } from "../api/mock";
import type { ThemeBundle } from "../lib/themeBundle";
import { TOKEN_KEY } from "../api/auth";
import { Avatar } from "./Avatar";
import { themeIconId } from "../lib/themeIconId";

// Two tiny-but-valid base64 rasters (magic bytes only — enough to pass the
// shared validator, which the ThemeSettings upload path enforces before these
// ever reach a bundle).
function b64(bytes: number[]): string {
  return btoa(String.fromCharCode(...bytes));
}
const MEMBER_IMG =
  "data:image/png;base64," + b64([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0]);
const OUTSOURCE_IMG =
  "data:image/webp;base64," +
  b64([0x52, 0x49, 0x46, 0x46, 0x10, 0, 0, 0, 0x57, 0x45, 0x42, 0x50, 0]);
const OWNER_IMG =
  "data:image/png;base64," + b64([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1]);
const ASSISTANT_IMG =
  "data:image/png;base64," + b64([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 2]);

let ctx = null as unknown as ReturnType<typeof useI18n>;
function Capture() {
  ctx = useI18n();
  return null;
}

/** Mount under a provider whose reconcile is allowed to settle — the provider
 * talks to the server on mount now, so a bare `render` would flush that work
 * outside act(). */
async function mount(children: ReactNode) {
  let result!: ReturnType<typeof render>;
  await act(async () => {
    result = render(
      <I18nProvider>
        <Capture />
        {children}
      </I18nProvider>
    );
  });
  return result;
}

/** Switch to a SAVED theme and wait for its one-bundle fetch to land. The sync
 * act is deliberate: in the browser the switch comes from a click, so React has
 * committed the new active id before the fetch resolves. An async act would let
 * the fetch land first and the provider's "ignore a fetch that lost a race to a
 * later switch" guard would (correctly) drop it. */
async function activate(id: string) {
  act(() => {
    ctx.setTheme(id);
  });
  await waitFor(() => expect(ctx.activeThemeBundle?.id).toBe(id));
}

async function seed(bundle: ThemeBundle) {
  await mockApi.putTheme(bundle);
}

describe("Avatar avatars-by-kind (T-16a1 P5)", () => {
  beforeEach(() => {
    __resetMock();
    localStorage.clear();
    // Signed in: a signed-out cockpit never fetches a bundle, so there would be
    // no theme avatars to assert on.
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
  });

  it("renders the built-in glyph (no <img>) under the office theme", async () => {
    const { container } = await mount(<Avatar size={40} kind="member" />);
    expect(container.querySelector("img.avatar__img")).toBeNull();
    // the fallback UserIcon is an <svg>
    expect(container.querySelector("svg")).not.toBeNull();
  });

  it("selects the member image for kind=member and the outsource image for kind=outsource", async () => {
    await seed({
      id: "portraits",
      name: "Portraits",
      colors: { "--color-bg": "#101018" },
      avatarPools: {
        member: [{ image: MEMBER_IMG }],
        outsource: [{ image: OUTSOURCE_IMG }],
      },
    });
    const { getByTestId } = await mount(
      <>
        <div data-testid="member">
          <Avatar size={40} kind="member" />
        </div>
        <div data-testid="outsource">
          <Avatar size={40} kind="outsource" />
        </div>
      </>
    );
    await activate("portraits");
    expect(
      getByTestId("member").querySelector("img.avatar__img")?.getAttribute("src")
    ).toBe(MEMBER_IMG);
    expect(
      getByTestId("outsource")
        .querySelector("img.avatar__img")
        ?.getAttribute("src")
    ).toBe(OUTSOURCE_IMG);
  });

  it("selects the owner image for kind=owner and the assistant image for kind=assistant", async () => {
    await seed({
      id: "roles",
      name: "Roles",
      colors: { "--color-bg": "#101018" },
      avatars: { owner: OWNER_IMG, assistant: ASSISTANT_IMG },
    });
    const { getByTestId } = await mount(
      <>
        <div data-testid="owner">
          <Avatar size={40} kind="owner" />
        </div>
        <div data-testid="assistant">
          <Avatar size={40} kind="assistant" />
        </div>
      </>
    );
    await activate("roles");
    expect(
      getByTestId("owner").querySelector("img.avatar__img")?.getAttribute("src")
    ).toBe(OWNER_IMG);
    expect(
      getByTestId("assistant")
        .querySelector("img.avatar__img")
        ?.getAttribute("src")
    ).toBe(ASSISTANT_IMG);
  });

  it("falls back to the glyph for owner / assistant when the theme carries none", async () => {
    await seed({
      id: "memberonly",
      name: "MemberOnly",
      colors: { "--color-bg": "#101018" },
      avatarPools: { member: [{ image: MEMBER_IMG }] },
    });
    const { getByTestId } = await mount(
      <>
        <div data-testid="owner">
          <Avatar size={40} kind="owner" />
        </div>
        <div data-testid="assistant">
          <Avatar size={40} kind="assistant" />
        </div>
      </>
    );
    await activate("memberonly");
    expect(getByTestId("owner").querySelector("img.avatar__img")).toBeNull();
    expect(getByTestId("owner").querySelector("svg")).not.toBeNull();
    expect(getByTestId("assistant").querySelector("img.avatar__img")).toBeNull();
    expect(getByTestId("assistant").querySelector("svg")).not.toBeNull();
  });

  it("falls back per-kind: a theme with only a member image keeps the glyph for outsource", async () => {
    await seed({
      id: "half",
      name: "Half",
      colors: { "--color-bg": "#101018" },
      avatarPools: { member: [{ image: MEMBER_IMG }] },
    });
    const { getByTestId } = await mount(
      <>
        <div data-testid="member">
          <Avatar size={40} kind="member" />
        </div>
        <div data-testid="outsource">
          <Avatar size={40} kind="outsource" />
        </div>
      </>
    );
    await activate("half");
    expect(
      getByTestId("member").querySelector("img.avatar__img")?.getAttribute("src")
    ).toBe(MEMBER_IMG);
    // outsource kind has no image on this theme → built-in glyph, no <img>
    expect(getByTestId("outsource").querySelector("img.avatar__img")).toBeNull();
    expect(getByTestId("outsource").querySelector("svg")).not.toBeNull();
  });

  // T-cd6f: the member's face is a CHOICE inside the active theme, addressed by
  // a stable icon id. These three cases are the whole resolution order.
  it("renders the image the chosen id names, wherever it sits in the pool", async () => {
    await seed({
      id: "byid",
      name: "ById",
      colors: { "--color-bg": "#101018" },
      avatarPools: { member: [{ image: OWNER_IMG }, { image: MEMBER_IMG }] },
    });
    const chosen = await themeIconId(MEMBER_IMG);
    const { container } = await mount(
      <Avatar size={40} kind="member" avatarIconId={chosen} />
    );
    await activate("byid");
    await waitFor(() =>
      expect(
        container.querySelector("img.avatar__img")?.getAttribute("src")
      ).toBe(MEMBER_IMG)
    );
  });

  // 🔴 An id the pool can no longer resolve — the image was removed — falls back
  // to the pool's FIRST image, the same thing a member who never chose sees. It
  // must NOT fall back to "whatever sits at that position now": that is how the
  // retired index model handed a member another member's face.
  it("falls back to the first image when the chosen id is gone, never to a neighbour", async () => {
    await seed({
      id: "removed",
      name: "Removed",
      colors: { "--color-bg": "#101018" },
      avatarPools: { member: [{ image: MEMBER_IMG }, { image: OWNER_IMG }] },
    });
    const { container } = await mount(
      <Avatar size={40} kind="member" avatarIconId="icn-removed" />
    );
    await activate("removed");
    await waitFor(() =>
      expect(
        container.querySelector("img.avatar__img")?.getAttribute("src")
      ).toBe(MEMBER_IMG)
    );
  });

  it("falls back to the built-in glyph when the image fails to load", async () => {
    await seed({
      id: "broken",
      name: "Broken",
      colors: { "--color-bg": "#101018" },
      avatarPools: { member: [{ image: MEMBER_IMG }] },
    });
    const { container } = await mount(<Avatar size={40} kind="member" />);
    await activate("broken");
    await waitFor(() =>
      expect(container.querySelector("img.avatar__img")).not.toBeNull()
    );
    fireEvent.error(container.querySelector("img.avatar__img")!);
    expect(container.querySelector("img.avatar__img")).toBeNull();
    expect(container.querySelector("svg")).not.toBeNull();
  });
});
