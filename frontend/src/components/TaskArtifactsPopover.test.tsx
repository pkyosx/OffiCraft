// TaskArtifactsPopover — the 「產物 N」 badge + tabbed popover (T-3dc5).
//
// The load-bearing assertion is the EMPTY-SET one the design pins: 0 artifacts ⇒
// NO badge. It carries a positive control (count > 0 ⇒ the badge renders with
// the count) so a mutant that drops the `count === 0` guard reddens on the empty
// case, and a mutant that always returns null reddens on the populated case.
// The popover cases cover the three-tab split (each kind lands in its own tab),
// the per-tab empty state, the .md 預覽 action, and the owner-only 移除 affordance.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskArtifactsBadge } from "./TaskArtifactsPopover";
import type { TaskArtifactView } from "../api/adapter";

// The popover keeps itself live via api.subscribeEvents (the ChatGalleryPanel
// pattern) — stub it to a no-op unsubscribe so the unit test never touches SSE.
vi.mock("../api", () => ({
  api: { subscribeEvents: () => () => {} },
}));

let seq = 0;
function mkArtifact(over: Partial<TaskArtifactView>): TaskArtifactView {
  seq += 1;
  return {
    id: `ta-${seq}`,
    kind: "link",
    url: "https://example.com/pr/1",
    label: `artifact-${seq}`,
    filename: "",
    mime: "",
    isImage: false,
    attachmentId: "",
    createdTs: 0,
    createdBy: "mira",
    ...over,
  };
}

function renderBadge(
  artifacts: TaskArtifactView[],
  opts: { count?: number; onRemove?: (t: string, a: string) => Promise<void> } = {},
) {
  const count = opts.count ?? artifacts.length;
  return render(
    <I18nProvider>
      <TaskArtifactsBadge
        task={{ id: "t-1", artifactCount: count, artifacts: [] }}
        onHydrate={async () => ({ artifacts })}
        onRemoveArtifact={opts.onRemove}
      />
    </I18nProvider>,
  );
}

beforeEach(() => {
  seq = 0;
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

describe("產物 popover — the three-tab split", () => {
  const artifacts = [
    mkArtifact({ id: "ta-file", kind: "file", filename: "report.pdf", mime: "application/pdf", url: "/api/chat/attachment/ta-file" }),
    mkArtifact({ id: "ta-img", kind: "image", filename: "shot.png", mime: "image/png", isImage: true, url: "/api/chat/attachment/ta-img" }),
    mkArtifact({ id: "ta-link", kind: "link", label: "PR #123", url: "https://github.com/x/y/pull/123" }),
    mkArtifact({ id: "ta-md", kind: "file", filename: "design.md", mime: "text/markdown", url: "/api/chat/attachment/ta-md" }),
  ];

  it("opens on click, hydrates, and lands each kind in its own tab", async () => {
    renderBadge(artifacts, { count: 4 });
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));

    // Files tab is the default: the two file rows (report.pdf + design.md).
    await waitFor(() => expect(screen.getByText("report.pdf")).toBeTruthy());
    expect(screen.getByText("design.md")).toBeTruthy();
    // The image + link are NOT in the files tab.
    expect(screen.queryByText("PR #123")).toBeNull();

    // Switch to the 連結 tab → the link chip shows, the files do not.
    fireEvent.click(screen.getByRole("tab", { name: /連結/ }));
    expect(screen.getByText("PR #123")).toBeTruthy();
    expect(screen.queryByText("report.pdf")).toBeNull();
  });

  it("shows a .md 預覽 action only on the markdown file", async () => {
    const { container } = renderBadge(artifacts, { count: 4 });
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    await waitFor(() => expect(screen.getByText("design.md")).toBeTruthy());
    // The eye/preview action is present in the files tab (design.md is .md);
    // report.pdf is not markdown, so exactly one preview action shows.
    const previews = container.querySelectorAll(
      '[aria-label="預覽"], [title="預覽"]',
    );
    expect(previews.length).toBe(1);
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
    // filename stretched the row and pushed the actions out of column). It
    // truncates via CSS, so the whole name has to survive on `title=`.
    const longName =
      "2026-07-20-座艙產物彈窗列表對齊-超長檔名回歸測試用-really-long-artifact-filename.pdf";
    const { container } = renderBadge(
      [mkArtifact({ id: "ta-long", kind: "file", filename: longName, mime: "application/pdf", url: "/api/chat/attachment/ta-long" })],
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
    // The consistency assertion behind 「三個 tab 列樣式統一」. Per tab: exactly
    // one row, that row is a .task-artifacts__item, it holds a .task-artifacts__chip
    // whose title is the FULL name, and the actions live in one trailing
    // .task-artifacts__actions column (so they align).
    const cases: Array<{ tab: RegExp; artifact: TaskArtifactView; fullName: string }> = [
      {
        tab: /檔案/,
        artifact: mkArtifact({ id: "ta-f", kind: "file", filename: "a-file-with-a-long-name.pdf", mime: "application/pdf", url: "/api/chat/attachment/ta-f" }),
        fullName: "a-file-with-a-long-name.pdf",
      },
      {
        tab: /圖片/,
        artifact: mkArtifact({ id: "ta-i", kind: "image", filename: "an-image-with-a-long-name.png", mime: "image/png", isImage: true, url: "/api/chat/attachment/ta-i" }),
        fullName: "an-image-with-a-long-name.png",
      },
      {
        tab: /連結/,
        artifact: mkArtifact({ id: "ta-l", kind: "link", label: "a link with a rather long label", url: "https://example.com/very/long/path" }),
        fullName: "a link with a rather long label",
      },
    ];

    for (const c of cases) {
      const view = renderBadge([c.artifact], { count: 1, onRemove: async () => {} });
      fireEvent.click(screen.getByTestId("task-artifacts-badge"));
      fireEvent.click(screen.getByRole("tab", { name: c.tab }));
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
      [mkArtifact({ id: "ta-link2", kind: "link", label: "PR #999", url: "https://github.com/x/y/pull/999" })],
      { count: 1 },
    );
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    fireEvent.click(screen.getByRole("tab", { name: /連結/ }));
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

  it("shows a per-tab empty state when a kind has no artifacts", async () => {
    renderBadge([mkArtifact({ id: "ta-only-link", kind: "link", label: "only a link" })], { count: 1 });
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    // Default files tab is empty (the one artifact is a link).
    await waitFor(() => expect(screen.getByText("還沒有檔案")).toBeTruthy());
    // The 連結 tab has the row.
    fireEvent.click(screen.getByRole("tab", { name: /連結/ }));
    expect(screen.getByText("only a link")).toBeTruthy();
  });
});
