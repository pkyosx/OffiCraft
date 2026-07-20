// TaskArtifactsPopover — the 「產物 N」 badge + its popover (T-3dc5).
//
// The load-bearing assertion is the EMPTY-SET one the design pins: 0 artifacts ⇒
// NO badge. It carries a positive control (count > 0 ⇒ the badge renders with
// the count) so a mutant that drops the `count === 0` guard reddens on the empty
// case, and a mutant that always returns null reddens on the populated case.
// The popover cases cover the ONE list (T-49fb dropped the 檔案/圖片/連結 tabs —
// every kind is listed at once), the .md 預覽 action, the owner-only 移除
// affordance, and click-outside dismissal.

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

describe("產物 popover — the one list (T-49fb)", () => {
  const artifacts = [
    mkArtifact({ id: "ta-file", kind: "file", filename: "report.pdf", mime: "application/pdf", url: "/api/chat/attachment/ta-file" }),
    mkArtifact({ id: "ta-img", kind: "image", filename: "shot.png", mime: "image/png", isImage: true, url: "/api/chat/attachment/ta-img" }),
    mkArtifact({ id: "ta-link", kind: "link", label: "PR #123", url: "https://github.com/x/y/pull/123" }),
    mkArtifact({ id: "ta-md", kind: "file", filename: "design.md", mime: "text/markdown", url: "/api/chat/attachment/ta-md" }),
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

  it("T-7bc2: the markdown file's chip IS the preview trigger (a <button>); report.pdf stays a download <a>", async () => {
    const { container } = renderBadge(artifacts, { count: 4 });
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    await waitFor(() => expect(screen.getByText("design.md")).toBeTruthy());
    // design.md is markdown ⇒ its chip renders as a <button> (click opens the
    // preview overlay); report.pdf is not ⇒ stays an <a href download>.
    expect(container.querySelectorAll("button.task-artifacts__chip").length).toBe(1);
    const mdChip = screen.getByText("design.md").closest("button.task-artifacts__chip");
    expect(mdChip).not.toBeNull();
    const pdfChip = screen.getByText("report.pdf").closest("a.task-artifacts__chip");
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
    // The consistency assertion behind 「三型列樣式統一」 (T-90df), now that the
    // tabs are gone: rendered ALONE, each kind gives exactly one row, that row
    // is a .task-artifacts__item, it holds a .task-artifacts__chip whose title
    // is the FULL name, and the actions live in one trailing
    // .task-artifacts__actions column (so they align).
    const cases: Array<{ artifact: TaskArtifactView; fullName: string }> = [
      {
        artifact: mkArtifact({ id: "ta-f", kind: "file", filename: "a-file-with-a-long-name.pdf", mime: "application/pdf", url: "/api/chat/attachment/ta-f" }),
        fullName: "a-file-with-a-long-name.pdf",
      },
      {
        artifact: mkArtifact({ id: "ta-i", kind: "image", filename: "an-image-with-a-long-name.png", mime: "image/png", isImage: true, url: "/api/chat/attachment/ta-i" }),
        fullName: "an-image-with-a-long-name.png",
      },
      {
        artifact: mkArtifact({ id: "ta-l", kind: "link", label: "a link with a rather long label", url: "https://example.com/very/long/path" }),
        fullName: "a link with a rather long label",
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
      [mkArtifact({ id: "ta-link2", kind: "link", label: "PR #999", url: "https://github.com/x/y/pull/999" })],
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
    renderBadge([mkArtifact({ id: "ta-only-link", kind: "link", label: "only a link" })], { count: 1 });
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    await waitFor(() => expect(screen.getByText("only a link")).toBeTruthy());
    expect(screen.queryByText("還沒有產物")).toBeNull();
  });
});

describe("產物 popover — click-outside dismissal (T-49fb)", () => {
  it("closes on an outside mousedown, stays open on an inside one", async () => {
    const { container } = renderBadge(
      [mkArtifact({ id: "ta-x", kind: "link", label: "PR #1" })],
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

  it("does NOT swallow the badge's own toggle (the close-then-reopen trap)", async () => {
    // The badge lives INSIDE the anchor the outside-check measures, so a
    // mousedown on it is never 'outside'. If it were, the panel would close on
    // mousedown and reopen on click — or never appear at all.
    const { container } = renderBadge(
      [mkArtifact({ id: "ta-y", kind: "link", label: "PR #2" })],
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
