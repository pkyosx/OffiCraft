// T-ea81 — NavIcon renders the active theme's per-tab icon image when present,
// and falls back to the built-in icon otherwise (a tab the theme omits keeps
// its built-in icon).
//
// T-83ef: the nav icons now arrive with the ACTIVE theme's bundle, which the
// provider fetches from the server rather than being handed the whole set. The
// case therefore saves its bundle through the mock API and switches to it —
// and, because `setTheme` is token-gated, does so signed in.

import { describe, it, expect, beforeEach } from "vitest";
import { render, act, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { I18nProvider, useI18n } from "../i18n";
import { mockApi, __resetMock } from "../api/mock";
import { TOKEN_KEY } from "../api/auth";
import { NavIcon } from "./NavIcon";
import { OfficeIcon, TasksIcon } from "./icons";

function b64(bytes: number[]): string {
  return btoa(String.fromCharCode(...bytes));
}
const OFFICE_IMG =
  "data:image/png;base64," + b64([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0]);

let ctx = null as unknown as ReturnType<typeof useI18n>;
function Capture() {
  ctx = useI18n();
  return null;
}

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

/** Sync act on purpose — see the note in Avatar.test.tsx. */
async function activate(id: string) {
  act(() => {
    ctx.setTheme(id);
  });
  await waitFor(() => expect(ctx.activeThemeBundle?.id).toBe(id));
}

describe("NavIcon", () => {
  beforeEach(() => {
    __resetMock();
    localStorage.clear();
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
  });

  it("renders the built-in fallback icon (an svg, no img) under the office theme", async () => {
    const { container } = await mount(
      <NavIcon tabKey="office" fallback={<OfficeIcon size={15} />} />
    );
    expect(container.querySelector("img.nav-tab__icon-img")).toBeNull();
    expect(container.querySelector("svg")).not.toBeNull();
  });

  it("renders the theme icon image only for a tab the theme provides", async () => {
    await mockApi.putTheme({
      id: "icons",
      name: "Icons",
      colors: { "--color-bg": "#101018" },
      navIcons: { office: OFFICE_IMG },
    });
    const { getByTestId } = await mount(
      <>
        <div data-testid="office">
          <NavIcon tabKey="office" fallback={<OfficeIcon size={15} />} />
        </div>
        <div data-testid="tasks">
          <NavIcon tabKey="tasks" fallback={<TasksIcon size={15} />} />
        </div>
      </>
    );
    await activate("icons");
    expect(
      getByTestId("office").querySelector("img.nav-tab__icon-img")?.getAttribute("src")
    ).toBe(OFFICE_IMG);
    // tasks has no themed icon → built-in fallback svg, no img
    expect(getByTestId("tasks").querySelector("img.nav-tab__icon-img")).toBeNull();
    expect(getByTestId("tasks").querySelector("svg")).not.toBeNull();
  });
});
