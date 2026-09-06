// TaskArtifactsPopover — the 「產物 N」 badge + its popover (T-3dc5).
//
// The load-bearing assertion is the EMPTY-SET one the design pins: 0 artifacts ⇒
// NO badge. It carries a positive control (count > 0 ⇒ the badge renders with
// the count) so a mutant that drops the `count === 0` guard reddens on the empty
// case, and a mutant that always returns null reddens on the populated case.
// The popover cases cover the ONE list (T-49fb dropped the 檔案/圖片/連結 tabs —
// every kind is listed at once), the .md 預覽 action, the owner-only 移除
// affordance, and click-outside dismissal.
//
// T-76cd: the preview overlay portals to `document.body`, so it is NOT inside
// the popover's subtree (or inside `container`) any more — it is reached through
// `document.body`. The click-outside case below is the one that cares about more
// than the query: containment used to be what kept the popover open behind an
// open preview, and it is now a `closest(".md-preview")` arm in the handler.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, waitFor, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { api } from "../api";
import { TaskArtifactsBadge } from "./TaskArtifactsPopover";
import type { TaskArtifactView } from "../api/adapter";
import { zh } from "../i18n/locales/zh";

// The popover keeps itself live via api.subscribeEvents (the ChatGalleryPanel
// pattern) — stub it to a no-op unsubscribe so the unit test never touches SSE.
//
// 🔴 `listTaskArtifacts` IS THE POINT OF THE STUB, not scaffolding. Before T-66
// this file handed the rows in through an `onHydrate` prop, so every case here
// would have stayed GREEN against a cockpit that fetched nothing — which is
// exactly the regression that ticket created the risk of. T-92 went one step
// further: the task read carries a COUNT and nothing else, so there is not even
// an id to draw a row from. Every case now goes through this stub, and the stub
// itself is asserted on.
const { listTaskArtifacts } = vi.hoisted(() => ({ listTaskArtifacts: vi.fn() }));
vi.mock("../api", () => ({
  api: {
    subscribeEvents: () => () => {},
    getChatAttachmentShareLink: vi.fn(),
    listTaskArtifacts,
    // T-60: the version reader reads its own state from the server (never from
    // these rows), so the popover's tests have to answer its version call too.
    listTaskArtifactVersions: vi.fn(async () => []),
  },
}));

let seq = 0;
const realFetch = globalThis.fetch;
function mkArtifact(over: Partial<TaskArtifactView>): TaskArtifactView {
  seq += 1;
  return {
    id: `ta-${seq}`,
    kind: "link",
    url: "https://example.com/pr/1",
    name: `artifact-${seq}`,
    description: "",
    mime: "",
    createdTs: 0,
    createdBy: "mira",
    versionCount: 1,
    ...over,
  };
}

function renderBadge(
  artifacts: TaskArtifactView[],
  opts: { count?: number; onRemove?: (t: string, a: string) => Promise<void> } = {},
) {
  const count = opts.count ?? artifacts.length;
  // The rows come from the SERVER, not from a prop: the task the card holds
  // carries only the count — since T-92 not even the artifact ids ride the task
  // read, so there is nothing here a row could be drawn from.
  listTaskArtifacts.mockResolvedValue(artifacts);
  return render(
    <I18nProvider>
      <TaskArtifactsBadge
        task={{ id: "t-1", artifactCount: count }}
        onRemoveArtifact={opts.onRemove}
      />
    </I18nProvider>,
  );
}

beforeEach(() => {
  seq = 0;
  listTaskArtifacts.mockReset();
});

afterEach(() => {
  globalThis.fetch = realFetch;
  vi.restoreAllMocks();
});

describe("產物 badge visibility (the empty-set assertion + positive control)", () => {
  it("renders NO badge when the artifact count is 0", () => {
    renderBadge([], { count: 0 });
    expect(screen.queryByTestId("task-artifacts-badge")).toBeNull();
  });

  it("renders the badge with the count when there is at least one artifact", () => {
    renderBadge([mkArtifact({}), mkArtifact({})], { count: 2 });
    const badge = screen.getByTestId("task-artifacts-badge");
    expect(badge.textContent).toContain("2");
  });
});

describe("產物 popover — the one list (T-49fb)", () => {
  const artifacts = [
    mkArtifact({ id: "ta-file", kind: "file", name: "report.pdf", mime: "application/pdf", url: "/api/chat/attachment/att-file" }),
    // An image is an image because its MIME says so (T-92 dropped `is_image` —
    // it was that same fact in a second field).
    mkArtifact({ id: "ta-img", kind: "image", name: "shot.png", mime: "image/png", url: "/api/chat/attachment/att-img" }),
    mkArtifact({ id: "ta-link", kind: "link", name: "PR #123", url: "https://github.com/x/y/pull/123" }),
    mkArtifact({ id: "ta-md", kind: "file", name: "design.md", mime: "text/markdown", url: "/api/chat/attachment/att-md" }),
  ];

  it("opens on click, hydrates, and lists EVERY kind at once with no tabs", async () => {
    const { container } = renderBadge(artifacts, { count: 4 });
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));

    // All four artifacts are on screen simultaneously — no control to operate.
    await waitFor(() => expect(screen.getByText("report.pdf")).toBeTruthy());
    expect(screen.getByText("design.md")).toBeTruthy();
    expect(screen.getByText("PR #123")).toBeTruthy();
    expect(container.querySelectorAll(".task-artifacts__item").length).toBe(4);

    // The tabs are GONE (the T-49fb decision, asserted negatively so a revert
    // to the tabbed body reddens here).
    expect(screen.queryAllByRole("tab").length).toBe(0);
    expect(container.querySelectorAll(".task-artifacts__tab").length).toBe(0);
  });

  it("groups the list 檔案 → 圖片 → 連結 so the kinds still read as families", async () => {
    const { container } = renderBadge(artifacts, { count: 4 });
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    await waitFor(() =>
      expect(container.querySelectorAll(".task-artifacts__item").length).toBe(4),
    );
    const rows = Array.from(container.querySelectorAll(".task-artifacts__item"));
    const kindOf = (row: Element) =>
      row.querySelector(".task-artifacts__thumb")
        ? "image"
        : row.querySelector("a.task-artifacts__link")
          ? "link"
          : "file";
    expect(rows.map(kindOf)).toEqual(["file", "file", "image", "link"]);
  });

  it("opens every stored artifact in the shared modal", async () => {
    const { container } = renderBadge(artifacts, { count: 4 });
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    await waitFor(() => expect(screen.getByText("design.md")).toBeTruthy());
    expect(container.querySelectorAll("button.task-artifacts__chip").length).toBe(2);
    const mdChip = screen.getByText("design.md").closest("button.task-artifacts__chip");
    expect(mdChip).not.toBeNull();
    const pdfChip = screen.getByText("report.pdf").closest("button.task-artifacts__chip");
    expect(pdfChip).not.toBeNull();
  });

  it("renders the owner 移除 affordance only when onRemoveArtifact is wired", async () => {
    const withRemove = renderBadge(artifacts, { count: 4, onRemove: async () => {} });
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    await waitFor(() => expect(screen.getByText("report.pdf")).toBeTruthy());
    expect(
      withRemove.container.querySelectorAll('[aria-label="移除產物"]').length,
    ).toBeGreaterThan(0);
    withRemove.unmount();

    const noRemove = renderBadge(artifacts, { count: 4 });
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    await waitFor(() => expect(screen.getByText("report.pdf")).toBeTruthy());
    expect(
      noRemove.container.querySelectorAll('[aria-label="移除產物"]').length,
    ).toBe(0);
  });

  it("truncates an overlong name in a CHIP that keeps the full name in title=", async () => {
    // T-90df: the chip must not size to its text (that was the bug — a long
    // name stretched the row and pushed the actions out of column). It
    // truncates via CSS, so the whole name has to survive on `title=`.
    const longName =
      "2026-07-20-座艙產物彈窗列表對齊-超長檔名回歸測試用-really-long-artifact-filename.pdf";
    const { container } = renderBadge(
      [mkArtifact({ id: "ta-long", kind: "file", name: longName, mime: "application/pdf", url: "/api/chat/attachment/att-long" })],
      { count: 1 },
    );
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    await waitFor(() => expect(screen.getByText(longName)).toBeTruthy());

    const chip = container.querySelector(".task-artifacts__chip");
    expect(chip).toBeTruthy();
    // The full name is recoverable on hover…
    expect(chip!.getAttribute("title")).toBe(longName);
    // …and the visible text sits in the element the ellipsis rule targets.
    const name = chip!.querySelector(".task-artifacts__chip-name");
    expect(name).toBeTruthy();
    expect(name!.textContent).toBe(longName);
  });

  it("gives all three kinds the SAME row shape: item > chip(title=full name) + actions", async () => {
    // The consistency assertion behind 「三型列樣式統一」 (T-90df), now that the
    // tabs are gone: rendered ALONE, each kind gives exactly one row, that row
    // is a .task-artifacts__item, it holds a .task-artifacts__chip whose title
    // is the FULL name, and the actions live in one trailing
    // .task-artifacts__actions column (so they align).
    const cases: Array<{ artifact: TaskArtifactView; fullName: string }> = [
      {
        artifact: mkArtifact({ id: "ta-f", kind: "file", name: "a-file-with-a-long-name.pdf", mime: "application/pdf", url: "/api/chat/attachment/att-f" }),
        fullName: "a-file-with-a-long-name.pdf",
      },
      {
        artifact: mkArtifact({ id: "ta-i", kind: "image", name: "an-image-with-a-long-name.png", mime: "image/png", url: "/api/chat/attachment/att-i" }),
        fullName: "an-image-with-a-long-name.png",
      },
      {
        artifact: mkArtifact({ id: "ta-l", kind: "link", name: "a link with a rather long name", url: "https://example.com/very/long/path" }),
        fullName: "a link with a rather long name",
      },
    ];

    for (const c of cases) {
      const view = renderBadge([c.artifact], { count: 1, onRemove: async () => {} });
      fireEvent.click(screen.getByTestId("task-artifacts-badge"));
      await waitFor(() =>
        expect(view.container.querySelectorAll(".task-artifacts__item").length).toBe(1),
      );

      const row = view.container.querySelector(".task-artifacts__item")!;
      const chip = row.querySelector(".task-artifacts__chip");
      expect(chip, `${c.fullName}: row must carry a chip`).toBeTruthy();
      expect(chip!.getAttribute("title")).toBe(c.fullName);
      expect(chip!.querySelector(".task-artifacts__chip-name")!.textContent).toBe(c.fullName);
      // Exactly one trailing actions column, and it is the row's LAST child —
      // that is what makes the buttons line up across rows and tabs.
      const actions = row.querySelectorAll(".task-artifacts__actions");
      expect(actions.length).toBe(1);
      expect(row.lastElementChild).toBe(actions[0]);
      view.unmount();
    }
  });

  it("keeps the link row's navigation behaviour while title= carries the name", async () => {
    // Behaviour freeze (owner requirement ③): title= moved to the full name,
    // so the ACTION description has to survive on aria-label, and the anchor
    // must still open in a new tab with the safe rel.
    const { container } = renderBadge(
      [mkArtifact({ id: "ta-link2", kind: "link", name: "PR #999", url: "https://github.com/x/y/pull/999" })],
      { count: 1 },
    );
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    await waitFor(() => expect(screen.getByText("PR #999")).toBeTruthy());

    const anchor = container.querySelector("a.task-artifacts__link") as HTMLAnchorElement;
    expect(anchor.getAttribute("href")).toBe("https://github.com/x/y/pull/999");
    expect(anchor.getAttribute("target")).toBe("_blank");
    expect(anchor.getAttribute("rel")).toBe("noopener noreferrer");
    expect(anchor.getAttribute("title")).toBe("PR #999");
    // The accessible name must still IDENTIFY this link — an aria-label of
    // just the action would make every link row announce identically.
    const ariaLabel = anchor.getAttribute("aria-label")!;
    expect(ariaLabel).toContain("PR #999");
    expect(ariaLabel).toContain("開啟連結");
  });

  it("lists a lone link with no empty state — one list, one kind present", async () => {
    // Pre-T-49fb this case opened on an EMPTY 檔案 tab and the owner had to
    // hunt for the link. Now the single artifact is simply there.
    renderBadge([mkArtifact({ id: "ta-only-link", kind: "link", name: "only a link" })], { count: 1 });
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    await waitFor(() => expect(screen.getByText("only a link")).toBeTruthy());
    expect(screen.queryByText("還沒有產物")).toBeNull();
  });
});

describe("產物面板：打開才抓 (T-66)", () => {
  // Owner c-cd063427fb2f:「我覺得任務產物，只需要預設給標題跟ID, 有需要再透過
  // 另一隻去拿就好了」— so the panel is the thing that goes and gets them, and
  // it does it when it OPENS.
  const rows = [
    mkArtifact({ id: "ta-l", kind: "link", name: "PR #77", url: "https://github.com/x/y/pull/77" }),
  ];

  it("fetches NOTHING until the badge is clicked, then fetches THIS task's set", async () => {
    renderBadge(rows, { count: 1 });
    // Rendering the card costs no artifact fetch — that is the whole saving.
    expect(listTaskArtifacts).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    await waitFor(() => expect(screen.getByText("PR #77")).toBeTruthy());
    expect(listTaskArtifacts).toHaveBeenCalledWith("t-1");
    // ONE call for the WHOLE ticket (owner c-f2d0fecb1168:「應該是指名任務？」),
    // never one per row.
    expect(listTaskArtifacts).toHaveBeenCalledTimes(1);
  });

  it("draws rows from FIELDS THE TASK READ DOES NOT CARRY, so it must have fetched", async () => {
    // 🔴 THE REGRESSION CASE. `url` / `name` / `mime` do not ride the task
    // response at all, so the only way this assertion can hold is that the panel
    // called the server itself. A cockpit that went on reading the card's own
    // artifacts would have nothing to read — T-92 left a bare count there — and
    // so no href, no thumbnail and no name to show.
    const { container } = renderBadge(
      [
        mkArtifact({ id: "ta-f", kind: "file", name: "spec.pdf", mime: "application/pdf", url: "/api/chat/attachment/att-f" }),
        mkArtifact({ id: "ta-l2", kind: "link", name: "PR #88", url: "https://github.com/x/y/pull/88" }),
      ],
      { count: 2 },
    );
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    await waitFor(() => expect(screen.getByText("spec.pdf")).toBeTruthy());
    const anchor = container.querySelector("a.task-artifacts__link") as HTMLAnchorElement;
    expect(anchor.getAttribute("href")).toBe("https://github.com/x/y/pull/88");
  });

  it("says it is LOADING rather than showing an empty panel", async () => {
    // A pending fetch must not read as 「還沒有產物」 — the badge the reader just
    // clicked has already told them there is one.
    listTaskArtifacts.mockReturnValue(new Promise(() => {}));
    render(
      <I18nProvider>
        <TaskArtifactsBadge task={{ id: "t-1", artifactCount: 1 }} />
      </I18nProvider>,
    );
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    await waitFor(() => expect(screen.getByTestId("task-artifacts-loading")).toBeTruthy());
    expect(screen.queryByText("還沒有產物")).toBeNull();
  });

  it("shows a FAILURE the reader can see, never a silent blank", async () => {
    listTaskArtifacts.mockRejectedValue(new Error("boom"));
    vi.spyOn(console, "warn").mockImplementation(() => {});
    render(
      <I18nProvider>
        <TaskArtifactsBadge task={{ id: "t-1", artifactCount: 1 }} />
      </I18nProvider>,
    );
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    await waitFor(() => expect(screen.getByTestId("task-artifacts-error")).toBeTruthy());
    // And it is NOT reported as an empty set: those two mean different things.
    expect(screen.queryByText("還沒有產物")).toBeNull();
    expect(screen.queryByTestId("task-artifacts-loading")).toBeNull();
  });

  it("names a nameless link by its url, and a url-less one by its id — never blank", async () => {
    // The fallback chain lives on the SERVER since T-92 (stored name → blob
    // filename → link target → id tail), so `name` is already non-empty on any
    // current server. What this pins is what the component does when it arrives
    // empty ANYWAY — an older server, a fixture, a field dropped in between:
    // the old chain rendered an anchor with NO TEXT, invisible and unclickable
    // and one row short of the count the badge promised.
    const { container } = renderBadge(
      [
        mkArtifact({ id: "ta-noname", kind: "link", name: "", url: "https://example.com/no-name" }),
        mkArtifact({ id: "ta-nothing", kind: "link", name: "", url: "" }),
      ],
      { count: 2 },
    );
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    await waitFor(() =>
      expect(container.querySelectorAll("a.task-artifacts__link").length).toBe(2),
    );
    const names = Array.from(
      container.querySelectorAll("a.task-artifacts__link .task-artifacts__chip-name"),
    ).map((n) => n.textContent);
    expect(names).toEqual(["https://example.com/no-name", "#ta-nothing".replace("#ta-", "#")]);
    for (const n of names) expect(n).not.toBe("");
  });
});

describe("任務產物 markdown 預覽的分享連結", () => {
  it("Escape closes an artifact popup without closing its parent artifact panel", async () => {
    const { container } = renderBadge([
      mkArtifact({ id: "ta-escape", kind: "file", name: "bundle.zip", mime: "application/zip", url: "/api/chat/attachment/att-escape" }),
    ]);
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    await waitFor(() => expect(container.querySelector("button.task-artifacts__chip")).toBeTruthy());
    fireEvent.click(container.querySelector("button.task-artifacts__chip")!);
    await waitFor(() => expect(document.body.querySelector(".md-preview")).toBeTruthy());
    // Two layers are up. The first Esc must reach the preview ALONE…
    fireEvent.keyDown(window, { key: "Escape" });
    expect(document.body.querySelector(".md-preview")).toBeNull();
    expect(container.querySelector(".task-artifacts")).toBeTruthy();
    // …and only the second one, now that the preview is gone, closes the
    // popover underneath it.
    fireEvent.keyDown(window, { key: "Escape" });
    expect(container.querySelector(".task-artifacts")).toBeNull();
  });

  it("shares a download-only artifact from the popup using its backing att- id", async () => {
    const mint = vi
      .mocked(api.getChatAttachmentShareLink)
      .mockResolvedValue("/api/chat/attachment/att-backing?sig=test");
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText: vi.fn(async () => {}) }, configurable: true,
    });
    const { container } = renderBadge([
      mkArtifact({
        id: "ta-binary", kind: "file", name: "bundle.zip",
        mime: "application/zip", url: "/api/chat/attachment/att-backing",
      }),
    ]);
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    const chip = await waitFor(() => {
      const button = container.querySelector("button.task-artifacts__chip") as HTMLButtonElement;
      expect(button).toBeTruthy();
      return button;
    });
    fireEvent.click(chip);
    const share = await waitFor(() => {
      const button = document.body.querySelector("button.md-preview__share") as HTMLButtonElement;
      expect(button).toBeTruthy();
      return button;
    });
    fireEvent.click(share);
    await waitFor(() => expect(mint).toHaveBeenCalledWith("att-backing"));
    expect(mint).not.toHaveBeenCalledWith("ta-binary");
  });

  it("uses the backing att- id while keeping the ta- id for artifact lookup", async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      text: async () => "# task artifact",
    })) as unknown as typeof fetch;
    const mint = vi
      .mocked(api.getChatAttachmentShareLink)
      .mockResolvedValue("/api/chat/attachment/att-backing?sig=test");
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText: vi.fn(async () => {}) },
      configurable: true,
    });
    const { container } = renderBadge(
      [
        mkArtifact({
          id: "ta-artifact",
          kind: "file",
          name: "task.md",
          mime: "text/markdown",
          // T-92: the blob id is the TAIL OF `url`, never a field of its own —
          // one string said twice was the drift the DTO was reshaped to remove.
          // So the artifact's own `ta-` id and its backing blob differ exactly
          // here, which is what this case is about.
          url: "/api/chat/attachment/att-backing",
        }),
      ],
      { count: 1 },
    );

    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    await waitFor(() =>
      expect(container.querySelector("button.task-artifacts__chip")).toBeTruthy(),
    );
    const chip = container.querySelector("button.task-artifacts__chip")!;
    fireEvent.click(chip);
    await screen.findByRole("heading", { name: "task artifact" });

    // The message bubble also has a same-named hover button. Scope to this
    // overlay's action row so this test cannot pass by clicking that twin.
    const actions = document.body.querySelector(".md-preview__actions")!;
    const share = actions.querySelector("button.md-preview__share")!;
    fireEvent.click(share);
    await waitFor(() => expect(mint).toHaveBeenCalledWith("att-backing"));
    expect(mint).not.toHaveBeenCalledWith("ta-artifact");
  });
});

describe("產物 popover — click-outside dismissal (T-49fb)", () => {
  it("closes on an outside mousedown, stays open on an inside one", async () => {
    const { container } = renderBadge(
      [mkArtifact({ id: "ta-x", kind: "link", name: "PR #1" })],
      { count: 1 },
    );
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    await waitFor(() => expect(container.querySelector(".task-artifacts")).toBeTruthy());

    // Inside the panel → still open.
    fireEvent.mouseDown(container.querySelector(".task-artifacts__header")!);
    expect(container.querySelector(".task-artifacts")).toBeTruthy();

    // Outside → dismissed.
    fireEvent.mouseDown(document.body);
    await waitFor(() => expect(container.querySelector(".task-artifacts")).toBeNull());
  });

  it("keeps the panel open when the mousedown lands on an open preview (owner 2026-07-20)", async () => {
    // 「點其他地方都不會自動關閉,一定要點 X」. This used to hold for free: the
    // preview rendered inside the anchor, so its backdrop was 'inside'. It
    // portals to document.body now (T-76cd), so the handler has to recognise it
    // by selector — and a mousedown on the BACKDROP, which is the overlay root
    // itself, is the point where that matters.
    const { container } = renderBadge([
      mkArtifact({
        id: "ta-preview",
        kind: "file",
        name: "bundle.zip",
        mime: "application/zip",
        url: "/api/chat/attachment/att-preview",
      }),
    ]);
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    await waitFor(() =>
      expect(container.querySelector("button.task-artifacts__chip")).toBeTruthy(),
    );
    fireEvent.click(container.querySelector("button.task-artifacts__chip")!);
    await waitFor(() => expect(document.body.querySelector(".md-preview")).toBeTruthy());

    fireEvent.mouseDown(document.body.querySelector(".md-preview")!);
    expect(container.querySelector(".task-artifacts")).toBeTruthy();

    // The control: the rule is scoped to the preview, not "never close".
    fireEvent.mouseDown(document.body);
    await waitFor(() => expect(container.querySelector(".task-artifacts")).toBeNull());
  });

  it("does NOT swallow the badge's own toggle (the close-then-reopen trap)", async () => {
    // The badge lives INSIDE the anchor the outside-check measures, so a
    // mousedown on it is never 'outside'. If it were, the panel would close on
    // mousedown and reopen on click — or never appear at all.
    const { container } = renderBadge(
      [mkArtifact({ id: "ta-y", kind: "link", name: "PR #2" })],
      { count: 1 },
    );
    const badge = screen.getByTestId("task-artifacts-badge");
    fireEvent.mouseDown(badge);
    fireEvent.click(badge);
    await waitFor(() => expect(container.querySelector(".task-artifacts")).toBeTruthy());

    // A second badge press closes it (the toggle still works).
    fireEvent.mouseDown(badge);
    fireEvent.click(badge);
    await waitFor(() => expect(container.querySelector(".task-artifacts")).toBeNull());
  });
});

// T-60 — the row's 「N版」 entry. `versionCount` counts the LIVE version too, so
// the entry exists only above 1; 0 is what an older server that never sends the
// field reads as, and it must be as quiet as 1 rather than as loud as 2.
describe("產物列的版本入口 (T-60)", () => {
  it("offers the versions entry only for an artifact that HAS been replaced", async () => {
    renderBadge([
      mkArtifact({ id: "ta-replaced", versionCount: 3 }),
      mkArtifact({ id: "ta-untouched", versionCount: 1 }),
      mkArtifact({ id: "ta-oldserver", versionCount: 0 }),
    ]);
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    await waitFor(() =>
      expect(screen.getByTestId("task-artifact-versions-ta-replaced")).toBeTruthy(),
    );
    expect(screen.queryByTestId("task-artifact-versions-ta-untouched")).toBeNull();
    expect(screen.queryByTestId("task-artifact-versions-ta-oldserver")).toBeNull();
  });

  it("opens the version reader and keeps the panel open while it is up", async () => {
    renderBadge([mkArtifact({ id: "ta-replaced", versionCount: 2 })]);
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    fireEvent.click(await screen.findByTestId("task-artifact-versions-ta-replaced"));
    const modal = await screen.findByTestId("ta-versions-modal");

    // The reader portals to document.body, so the popover's click-outside
    // handler no longer contains it — a mousedown anywhere in the reader would
    // close the artifacts panel out from under it without the matching arm.
    fireEvent.mouseDown(modal);
    expect(screen.getByTestId("ta-versions-modal")).toBeTruthy();
    expect(screen.getByText(t_panelTitle())).toBeTruthy();
  });
});

/** The panel's own title, read from the dictionary rather than retyped — this
 * file asserts the panel is still MOUNTED, not what it is called. */
function t_panelTitle(): string {
  return zh.tasks.artifacts.panelTitle;
}
