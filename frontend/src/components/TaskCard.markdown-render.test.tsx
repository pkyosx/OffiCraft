// T-13af — the task card's description and step DoD are owner/agent-authored
// free text that may carry markdown (headings/bold/lists/code/links); both
// must render through the shared, XSS-safe `Markdown` component (never raw
// text, never dangerouslySetInnerHTML).

import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, act, waitFor, within, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskCard } from "./TaskCard";
import { AttachmentModalHost } from "./AttachmentModalHost";
import { api } from "../api";
import type { Member } from "../types";
import type { TaskView, TaskStepView, OutsourceWorkerView, ReplyCard } from "../api/adapter";

let seq = 0;
function mkStep(over: Partial<TaskStepView>): TaskStepView {
  seq += 1;
  return {
    id: `step-${seq}`, name: `節點-${seq}`, dod: "", status: "pending",
    isGate: false, replyCardId: "", parallelGroup: "", orderIdx: seq,
    startedTs: 0, finishedTs: 0, ...over,
  };
}
function mkTask(over: Partial<TaskView>): TaskView {
  return {
    id: "T-13af", taskNo: "T-13af", title: "markdown render 任務", typeKey: "",
    description: "", status: "in_progress", priority: "high",
    executorKind: "staff", executorId: "mira", creatorId: "", dedupeKey: "",
    deps: [], waitingReason: "", duplicateOf: "", createdTs: 1000, updatedTs: 2000,
    closedTs: null, progressDone: 0, progressTotal: 1, steps: [], ...over,
  };
}

const MIRA = { id: "mira", name: "Mira", kind: "agent" } as unknown as Member;
const NOVA = { id: "nova", name: "Nova", kind: "agent" } as unknown as Member;
const noop = async () => {};
const workers: OutsourceWorkerView[] = [];

function renderCard(task: TaskView, onHydrate: (id: string) => Promise<TaskView>) {
  return render(
    <I18nProvider>
      <AttachmentModalHost>
        <TaskCard
          task={task} allTasks={[task]} members={[MIRA, NOVA]} workers={workers} nowTs={3000}
          onTerminate={noop as never} onMarkDuplicate={noop as never} onSetPriority={noop as never}
          onReassign={noop as never}
          onSendMessage={noop as never} onHydrate={onHydrate}
        />
      </AttachmentModalHost>
    </I18nProvider>
  );
}

describe("TaskCard markdown render (T-13af)", () => {
  it("renders the description's markdown (heading/bold/list/code/link) as elements, not literal syntax", async () => {
    const description = [
      "## 說明",
      "**重點**是 `foo()`:",
      "- 一",
      "- 二",
      "見 [文件](https://example.com/doc)",
    ].join("\n");
    const detail = mkTask({ description });
    const onHydrate = vi.fn(async () => detail);
    const { container, findByTestId } = renderCard(mkTask({}), onHydrate);

    fireEvent.click(await findByTestId("task-card"));
    await act(async () => {});

    const desc = container.querySelector(".task-card__desc")!;
    expect(desc.querySelector("h2")?.textContent).toBe("說明");
    expect(desc.querySelector("strong")?.textContent).toBe("重點");
    expect(desc.querySelector("code")?.textContent).toBe("foo()");
    expect(desc.querySelectorAll("ul > li").length).toBe(2);
    const link = desc.querySelector("a");
    expect(link?.getAttribute("href")).toBe("https://example.com/doc");
    expect(link?.getAttribute("target")).toBe("_blank");
    // never falls back to raw markdown syntax
    expect(desc.textContent).not.toContain("**重點**");
    expect(desc.textContent).not.toContain("##");
  });

  it("sanitizes a malicious description — no raw HTML injected, script text stays literal", async () => {
    const detail = mkTask({
      description: "<img src=x onerror=alert(1)> and [bad](javascript:alert(1))",
    });
    const onHydrate = vi.fn(async () => detail);
    const { container, findByTestId } = renderCard(mkTask({}), onHydrate);

    fireEvent.click(await findByTestId("task-card"));
    await act(async () => {});

    const desc = container.querySelector(".task-card__desc")!;
    expect(desc.querySelector("img")).toBeNull();
    expect(desc.querySelector("a")).toBeNull();
    expect(desc.textContent).toContain("<img src=x onerror=alert(1)>");
    expect(desc.textContent).toContain("[bad](javascript:alert(1))");
  });

  it("renders a step's DoD markdown (list) under the DoD label", async () => {
    const step = mkStep({ dod: "- 條件一\n- 條件二", status: "in_progress" });
    const detail = mkTask({ steps: [step], progressDone: 0, progressTotal: 1 });
    const onHydrate = vi.fn(async () => detail);
    const { container, findByTestId } = renderCard(mkTask({}), onHydrate);

    fireEvent.click(await findByTestId("task-card"));
    await act(async () => {});

    const dod = container.querySelector(".task-step__dod")!;
    expect(dod.querySelectorAll("ul > li").length).toBe(2);
    expect(dod.textContent).not.toContain("- 條件一");
  });

  describe("links to a stored document", () => {
    const overlay = () => document.body.querySelector<HTMLElement>(".md-preview[role=dialog]");

    async function commentOnLink(container: HTMLElement, linkName: string, attachmentId: string) {
      localStorage.setItem(
        `oc_doc_comments:${attachmentId}`,
        JSON.stringify([{ id: "c1", excerpt: "文件", comment: "看過" }]),
      );
      fireEvent.click(within(container).getByRole("link", { name: linkName }));
      await screen.findByRole("heading", { name: "文件" });
      fireEvent.click(within(overlay()!).getByRole("button", { name: "註解" }));
      const recipient = overlay()!.querySelector(".doc-comments__recipient strong")?.textContent;
      fireEvent.click(screen.getByRole("button", { name: "送出給 AI" }));
      await waitFor(() => expect(overlay()).toBeNull());
      return recipient;
    }

    it("sends comments on the description's document to the executor, and on a step reply card's document to that card's author", async () => {
      const realFetch = globalThis.fetch;
      globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => "# 文件" })) as unknown as typeof fetch;
      const post = vi.spyOn(api, "postChat").mockResolvedValue(undefined);
      const askedByNova: ReplyCard = {
        id: "rc-nova", from: "nova", kind: "decision", summary: "要採用嗎？",
        body: "見 [附錄.md](/api/chat/attachment/att-222222222222)",
        options: [], selectMode: "single", status: "waiting", attachments: [],
        createdTs: 1000, answeredTs: null, chatMessageId: "msg-1", answer: null,
      };
      vi.spyOn(api, "getReplyCard").mockResolvedValue(askedByNova);
      const detail = mkTask({
        description: "見 [規格.md](/api/chat/attachment/att-111111111111)",
        steps: [mkStep({ status: "in_progress", replyCardId: "rc-nova" })],
      });
      try {
        const { container, findByTestId } = renderCard(mkTask({}), vi.fn(async () => detail));
        fireEvent.click(await findByTestId("task-card"));
        await within(container).findByRole("link", { name: "附錄.md" });

        expect(await commentOnLink(container, "規格.md", "att-111111111111")).toBe("Mira");
        expect(await commentOnLink(container, "附錄.md", "att-222222222222")).toBe("Nova");
        expect(post.mock.calls.map(([arg]) => arg.to)).toEqual(["mira", "nova"]);
      } finally {
        globalThis.fetch = realFetch;
        vi.restoreAllMocks();
        localStorage.clear();
      }
    });

    it("opens a document linked from an unassigned task read-only", async () => {
      const realFetch = globalThis.fetch;
      globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => "# 文件" })) as unknown as typeof fetch;
      const unassigned = { executorKind: "outsource" as const, executorId: "" };
      const detail = mkTask({ ...unassigned, description: "見 [規格.md](/api/chat/attachment/att-111111111111)" });
      try {
        const { container, findByTestId } = renderCard(mkTask(unassigned), vi.fn(async () => detail));
        fireEvent.click(await findByTestId("task-card"));
        fireEvent.click(await within(container).findByRole("link", { name: "規格.md" }));
        await screen.findByRole("heading", { name: "文件" });
        expect(within(overlay()!).queryByRole("button", { name: "註解" })).toBeNull();
      } finally {
        globalThis.fetch = realFetch;
      }
    });
  });
});
