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
