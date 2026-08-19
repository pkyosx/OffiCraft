// [T-1500] the DECISION-level guards for the paint cache (jsdom
// cannot see colour — the frame-sequence proof lives in the real-load probe).
import { describe, it, expect, beforeEach } from "vitest";
import { render, waitFor, act } from "@testing-library/react";
import { I18nProvider, useI18n } from "./index";
import { mockApi, __resetMock } from "../api/mock";
import { TOKEN_KEY } from "../api/auth";

function bg(): string {
  return document.documentElement.style.getPropertyValue("--color-bg");
}

function Probe() {
  const { theme } = useI18n();
  return (
    <div
      data-testid="probe"
      data-theme={theme}
      data-theme2={theme}
    />
  );
}

let ctx = null as unknown as ReturnType<typeof useI18n>;
function Capture() {
  ctx = useI18n();
  return null;
}

const MIDNIGHT = {
  id: "midnight",
  name: "Midnight",
  colors: { "--color-bg": "#010203" },
};

function seedPaint(bundle: unknown, v = 1) {
  localStorage.setItem("oc.themePaint", JSON.stringify({ v, bundle }));
}

describe("D2 paint cache", () => {
  beforeEach(() => {
    __resetMock();
    localStorage.clear();
    document.documentElement.removeAttribute("style");
    delete document.documentElement.dataset.theme;
  });

  it("(i) pre-auth: an unresolved bundle paints the CACHED colours", () => {
    localStorage.setItem("oc.theme", "midnight");
    seedPaint(MIDNIGHT);
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>
    );
    expect(bg()).toBe("#010203");
  });

  it("(ii) a record whose id is not the active id is ignored", () => {
    localStorage.setItem("oc.theme", "sunrise");
    seedPaint(MIDNIGHT);
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>
    );
    expect(bg()).toBe("");
  });

  it("(iii-a) a tampered colour value is dropped whole, no throw", () => {
    localStorage.setItem("oc.theme", "midnight");
    seedPaint({ ...MIDNIGHT, colors: { "--color-bg": "url(javascript:1)" } });
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>
    );
    expect(bg()).toBe("");
  });

  it("(iii-b) an off-whitelist token name is dropped whole", () => {
    localStorage.setItem("oc.theme", "midnight");
    seedPaint({ ...MIDNIGHT, colors: { "--color-bg": "#010203", "--evil": "#000" } });
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>
    );
    expect(bg()).toBe("");
  });

  it("(iii-c) an illegal canvasMode is dropped whole (M4: never a TypeError)", () => {
    localStorage.setItem("oc.theme", "midnight");
    seedPaint({
      ...MIDNIGHT,
      backgrounds: { canvas: "data:image/png;base64,iVBORw0KGgo=" },
      backgroundModes: { canvas: "EVIL" },
    });
    expect(() =>
      render(
        <I18nProvider>
          <Probe />
        </I18nProvider>
      )
    ).not.toThrow();
    expect(bg()).toBe("");
  });

  it("(iii-d) garbage JSON is dropped, no throw", () => {
    localStorage.setItem("oc.theme", "midnight");
    localStorage.setItem("oc.themePaint", "{not json");
    expect(() =>
      render(
        <I18nProvider>
          <Probe />
        </I18nProvider>
      )
    ).not.toThrow();
    expect(bg()).toBe("");
  });

  it("(iii-e) a record from a future schema version is ignored", () => {
    localStorage.setItem("oc.theme", "midnight");
    seedPaint(MIDNIGHT, 2);
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>
    );
    expect(bg()).toBe("");
  });

  it("(iv) THE DELETED-THEME GUARD: once reconcile has spoken and the id does not resolve, the cached picture must be dropped", async () => {
    localStorage.setItem("oc.theme", "midnight");
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    seedPaint(MIDNIGHT);
    // server: the theme was deleted elsewhere → empty set, displayTheme ""
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>
    );
    // pre-reconcile it IS painted (that is the whole point)
    expect(bg()).toBe("#010203");
    await waitFor(() =>
      expect(bg()).toBe("")
    );
    // and the stale record is gone from storage
    expect(localStorage.getItem("oc.themePaint")).toBeNull();
  });

  it("(v) M3: EDITING an existing theme's colours rewrites the paint cache", async () => {
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    await mockApi.putTheme(MIDNIGHT);
    await mockApi.patchServerSettings({ displayTheme: "midnight" });
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
    await waitFor(() => expect(ctx.theme).toBe("midnight"));
    await waitFor(() => expect(ctx.activeThemeBundle?.id).toBe("midnight"));
    // the editor's save path: same id, new colours — ONE theme, not the set
    await act(async () => {
      await ctx.saveTheme({
        ...MIDNIGHT,
        colors: { "--color-bg": "#aabbcc" },
      });
    });
    const rec = JSON.parse(localStorage.getItem("oc.themePaint") ?? "null");
    expect(rec?.bundle?.colors?.["--color-bg"]).toBe("#aabbcc");
  });

  // [T-83ef] The paint record is written ONLY once the bundle is actually in
  // hand. The provider used to hold every bundle, so a switch had one; now it
  // fetches, and a fetch can fail. Writing a record from nothing would CLEAR the
  // cached picture — and the next pre-auth load would flash. So a failed switch
  // must leave the record exactly as it was.
  it("(v-c) a switch whose bundle fetch fails must not overwrite the cached picture", async () => {
    localStorage.setItem("oc.theme", "midnight");
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    await mockApi.putTheme(MIDNIGHT);
    await mockApi.patchServerSettings({ displayTheme: "midnight" });
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
    await waitFor(() => expect(ctx.activeThemeBundle?.id).toBe("midnight"));
    const before = localStorage.getItem("oc.themePaint");
    expect(before).not.toBeNull();

    // The owner picks a theme the server does not have (deleted on another
    // device): both the settings PATCH and the bundle GET refuse.
    act(() => ctx.setTheme("sunrise"));
    await waitFor(() => expect(ctx.theme).toBe("sunrise"));
    await act(async () => {
      await Promise.resolve();
    });
    expect(localStorage.getItem("oc.themePaint")).toBe(before);
  });

  // [T-1500] step-5 ③ CROSS-DEVICE: device A edited this theme's COLOURS — the
  // id did not change, so B's cached record still resolves and still paints. B
  // is allowed to show the old colours for a moment (that is the whole point of
  // painting from cache), but it must not STOP there: reconcile has to land the
  // new colours on the DOM *and* rewrite the record, or B stays wrong until the
  // next edit. The deleted-theme guard (iv) does not cover this — there the id
  // stops resolving; here it keeps resolving with different content.
  it("(v-b) cross-device: colours edited elsewhere must not stay stale after reconcile", async () => {
    localStorage.setItem("oc.theme", "midnight");
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    seedPaint(MIDNIGHT); // B's cache: the colours as of B's last visit
    // A's edit already landed on the server: same id, new colours.
    await mockApi.putTheme({ ...MIDNIGHT, colors: { "--color-bg": "#ff0000" } });
    await mockApi.patchServerSettings({ displayTheme: "midnight" });
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>
    );
    // the cached picture is up first — allowed, and asserted so a regression
    // that simply stops painting cannot pass this test by accident.
    expect(bg()).toBe("#010203");
    // and it must converge on the server's truth, not sit on the stale one.
    await waitFor(() => expect(bg()).toBe("#ff0000"));
    // the record is rewritten too, or the NEXT reload repaints the stale colours.
    const rec = JSON.parse(localStorage.getItem("oc.themePaint") ?? "null");
    expect(rec?.bundle?.colors?.["--color-bg"]).toBe("#ff0000");
  });

  it("(vi) logout clears the picture", async () => {
    localStorage.setItem("oc.theme", "midnight");
    seedPaint(MIDNIGHT);
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
    await act(async () => {
      ctx.resetPreferences();
    });
    expect(localStorage.getItem("oc.themePaint")).toBeNull();
  });
});

// [T-1500] the case that separates `themesLoaded` from the §2.5 removal:
// reconcile's REMOVAL of the stale record fails (quota / storage error). The
// removal can no longer be the guard, so only the boolean is left holding it.
describe("D2 paint cache — themesLoaded is not redundant", () => {
  beforeEach(() => {
    __resetMock();
    localStorage.clear();
    document.documentElement.removeAttribute("style");
    delete document.documentElement.dataset.theme;
  });

  it("(vii) a deleted theme stops painting even when the stale record cannot be removed", async () => {
    localStorage.setItem("oc.theme", "midnight");
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    seedPaint(MIDNIGHT);
    const ls = window.localStorage;
    const realRemove = ls.removeItem.bind(ls);
    const realSet = ls.setItem.bind(ls);
    // make every WRITE to the paint key fail, exactly as a full quota would
    ls.removeItem = (k: string) => {
      if (k === "oc.themePaint") throw new DOMException("QuotaExceededError");
      realRemove(k);
    };
    ls.setItem = (k: string, v: string) => {
      if (k === "oc.themePaint") throw new DOMException("QuotaExceededError");
      realSet(k, v);
    };
    try {
      render(
        <I18nProvider>
          <Probe />
        </I18nProvider>
      );
      expect(bg()).toBe("#010203");
      await waitFor(() => expect(bg()).toBe(""));
      // the record really did survive — so the removal was NOT what saved us
      expect(localStorage.getItem("oc.themePaint")).not.toBeNull();
    } finally {
      delete (ls as unknown as Record<string, unknown>).removeItem;
      delete (ls as unknown as Record<string, unknown>).setItem;
    }
  });
});
