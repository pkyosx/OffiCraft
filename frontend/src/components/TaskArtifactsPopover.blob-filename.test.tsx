// TaskArtifactsPopover — a deliverable's DISPLAY name and its BLOB's name are
// two different questions, and the preview reads the second one.
//
// THE REGRESSION THIS PINS. T-92 gave an artifact a human-written `name` and
// dropped `filename` from the live wire on the reasoning that the first derives
// from the second and therefore replaces it. It does — on screen. It does not
// where the cockpit decides whether bytes can be RENDERED: `isMarkdownAttachment`
// and its two siblings ask the mime first and fall back to a file EXTENSION,
// and `application/octet-stream` is what the agent upload path stores nearly
// every .md report under. 「第三季稽核報告」 has no extension, so every .md
// artifact stopped previewing and rendered as a plain download row instead.
//
// 🔴 BOTH HALVES ARE THE TEST. A fix that hands the blob's name to the chip
// would restore the preview and silently delete the T-92 feature — the human
// name is what the row is supposed to SAY — so each case here asserts the name
// on screen as well as what the overlay did with the bytes. And the reverse
// face is its own case: a .tgz must go on being un-renderable, or "previewable"
// would just mean "we stopped asking".

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, waitFor, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskArtifactsBadge } from "./TaskArtifactsPopover";
import type { TaskArtifactView } from "../api/adapter";
import { zh } from "../i18n/locales/zh";

// Same stub shape as TaskArtifactsPopover.test.tsx: the rows arrive from the
// server (a task read carries only a count since T-92), so the fetch is the
// only door a row can come through.
const { listTaskArtifacts } = vi.hoisted(() => ({ listTaskArtifacts: vi.fn() }));
vi.mock("../api", () => ({
  api: {
    subscribeEvents: () => () => {},
    getChatAttachmentShareLink: vi.fn(async () => "/api/chat/attachment/att?sig=t"),
    listTaskArtifacts,
    listTaskArtifactVersions: vi.fn(async () => []),
  },
}));

const realFetch = globalThis.fetch;

function mkArtifact(over: Partial<TaskArtifactView>): TaskArtifactView {
  return {
    id: "ta-1",
    kind: "file",
    url: "/api/chat/attachment/att-1",
    name: "artifact",
    description: "",
    filename: "",
    // The mime the agent upload path really produces for these blobs — the
    // whole reason the extension has to be readable somewhere.
    mime: "application/octet-stream",
    createdTs: 0,
    createdBy: "mira",
    versionCount: 1,
    ...over,
  };
}

function renderBadge(artifacts: TaskArtifactView[]) {
  listTaskArtifacts.mockResolvedValue(artifacts);
  return render(
    <I18nProvider>
      <TaskArtifactsBadge task={{ id: "t-1", artifactCount: artifacts.length }} />
    </I18nProvider>,
  );
}

beforeEach(() => {
  listTaskArtifacts.mockReset();
});

afterEach(() => {
  globalThis.fetch = realFetch;
  vi.restoreAllMocks();
});

describe("產物的顯示名與 blob 檔名是兩件事", () => {
  it("previews a .md blob pinned under a human name — and the chip still shows that name", async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      text: async () => "# 稽核結論\n\nthe **plan**",
    })) as unknown as typeof fetch;

    const { container } = renderBadge([
      mkArtifact({
        id: "ta-md",
        name: "第三季稽核報告",
        filename: "audit-2026-q3.md",
        url: "/api/chat/attachment/att-md",
      }),
    ]);
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));

    // Half one: the row says what a human called it. `filename` never reaches
    // the screen, so a fix that swapped the two would redden right here.
    const chip = await waitFor(() => {
      const button = container.querySelector("button.task-artifacts__chip") as HTMLButtonElement;
      expect(button).toBeTruthy();
      return button;
    });
    expect(chip.textContent).toContain("第三季稽核報告");
    expect(chip.textContent).not.toContain("audit-2026-q3.md");

    // Half two: the bytes are RENDERED as markdown. The heading is the proof —
    // the un-previewable branch draws a status line and never fetches at all.
    fireEvent.click(chip);
    await waitFor(() =>
      expect(screen.getByRole("heading", { name: "稽核結論" })).toBeTruthy(),
    );
    expect(document.body.querySelector(".md-preview__status")).toBeNull();
  });

  it("still refuses to render an archive pinned under a human name", async () => {
    // No fetch stub on purpose: an un-previewable blob must not be fetched, so
    // a mutant that widened the detection into "everything is text" would blow
    // up on the real fetch instead of quietly passing.
    const { container } = renderBadge([
      mkArtifact({
        id: "ta-tgz",
        name: "封存打包",
        filename: "release-bundle.tgz",
        url: "/api/chat/attachment/att-tgz",
      }),
    ]);
    fireEvent.click(screen.getByTestId("task-artifacts-badge"));
    const chip = await waitFor(() => {
      const button = container.querySelector("button.task-artifacts__chip") as HTMLButtonElement;
      expect(button).toBeTruthy();
      return button;
    });
    expect(chip.textContent).toContain("封存打包");

    fireEvent.click(chip);
    const status = await waitFor(() => {
      const node = document.body.querySelector(".md-preview__status");
      expect(node).toBeTruthy();
      return node!;
    });
    // 「這個檔案無法在這裡預覽」 — the overlay's own words, taken from the
    // locale rather than retyped here, so a wording change does not read as a
    // behaviour change.
    expect(status.textContent).toBe(zh.chat.mdPreview.unavailable);
  });
});
