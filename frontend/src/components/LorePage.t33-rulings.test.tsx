// LorePage — the writer's name on a 傳承 row: a 外包 or staff writer who has
// left keeps his NAME on the row, and the card has no message box.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { __resetHashRouteForTests } from "../lib/hashRoute";
import { LorePage } from "./LorePage";
import { __resetMock } from "../api/mock";
import { __resetWorkerCodenameCache } from "../hooks/useWorkerCodenames";
import { openEveryLoreDirGroup } from "../test/loreDirectory";
import { api } from "../api";
import type {
  LoreEntryPageView,
  LoreEntryView,
  OutsourceWorkerView,
} from "../api/adapter";

function mkEntry(over: Partial<LoreEntryView> & { id: string }): LoreEntryView {
  return {
    seq: 1,
    scopeKind: "agent",
    scopeKey: "mira",
    taskTypeKey: "",
    scopeOptions: [],
    title: `標題 ${over.id}`,
    body: `內容 ${over.id}`,
    authorId: "mira",
    sourceTaskId: "",
    state: "active",
    retireReason: "",
    effectiveTs: 1788400000,
    createdTs: 1788400000,
    updatedTs: 1788400000,
    loreType: "other",
    ...over,
  };
}

function page(entries: LoreEntryView[]): LoreEntryPageView {
  return {
    entries,
    limit: 30,
    offset: 0,
    capChars: 0,
    firstDroppedId: "",
    usedChars: 0,
    firstDroppedChars: 0,
    budgetSegments: [],
  };
}

function stubList(entries: LoreEntryView[]) {
  return vi.spyOn(api, "listLoreEntries").mockResolvedValue(page(entries));
}

/** Render with Mira's section as the only target and open it. */
async function renderPage(props: { canSetScope?: boolean } = {}) {
  vi.spyOn(api, "listLoreBudgets").mockResolvedValue([
    {
      scopeKind: "agent",
      scopeKey: "mira",
      capChars: 10000,
      usedChars: 0,
      entryCount: 0,
      droppedCount: 0,
    },
  ]);
  const r = render(
    <I18nProvider>
      <LorePage {...props} />
    </I18nProvider>,
  );
  fireEvent.click(
    await waitFor(() => {
      const el = r.container.querySelector<HTMLElement>('[data-testid="lore-target"]');
      if (!el) throw new Error("no directory target");
      return el;
    }, { timeout: 3000 }),
  );
  return r;
}

function rowById(container: HTMLElement, id: string): HTMLElement {
  const el = container.querySelector<HTMLElement>(
    `[data-testid="lore-row"][data-entry-id="${id}"]`,
  );
  if (!el) throw new Error(`no rendered row for ${id}`);
  return el;
}

/** Send one message from the row's box; answers who it went to. */
async function messageRecipientOf(row: HTMLElement): Promise<string> {
  const to: string[] = [];
  vi.spyOn(api, "postChat").mockImplementation(async (m) => {
    to.push(m.to);
  });
  fireEvent.change(row.querySelector('[data-testid="lore-msg-input"]')!, {
    target: { value: "還適用嗎" },
  });
  fireEvent.click(row.querySelector('[data-testid="lore-msg-send"]')!);
  await waitFor(() => expect(to).toHaveLength(1));
  return to[0];
}

beforeEach(() => {
  // The page reads its section from the hash; every spec starts on the directory.
  history.replaceState(null, "", window.location.pathname);
  __resetMock();
  __resetWorkerCodenameCache();
  openEveryLoreDirGroup();
});

afterEach(() => {
  __resetHashRouteForTests();
  vi.restoreAllMocks();
});

describe("LorePage — 已離開的撰寫人只留名牌，沒有訊息框", () => {
  const RELEASED = "ow-gone";
  const LIVE = "ow-live";
  const DEPARTED_STAFF = "m-gone";

  function worker(id: string, codename: string): OutsourceWorkerView {
    return {
      id,
      codename,
      model: "claude-opus-5",
      effort: "medium",
      taskId: "t-1111aaaabbbb",
    } as OutsourceWorkerView;
  }

  /** The roster the page resolves against. `members` deliberately holds NEITHER
   * worker: the server drops roster_status='removed' rows from /api/members, so
   * a departed 外包 is exactly an id that list cannot answer for — which is why
   * the ow- arm has to decide reachability from the LIVE outsource list. */
  function stubRoster(live: OutsourceWorkerView[]) {
    vi.spyOn(api, "listMembers").mockResolvedValue([]);
    vi.spyOn(api, "listOutsourceWorkers").mockResolvedValue(live);
    // The per-id route DOES serve released rows — that is what still resolves a
    // departed writer's codename (useWorkerCodenames).
    vi.spyOn(api, "getOutsourceWorker").mockImplementation(async (id) => {
      if (id === RELEASED) return worker(RELEASED, "O-9");
      if (id === LIVE) return worker(LIVE, "O-1");
      if (id === DEPARTED_STAFF) return worker(DEPARTED_STAFF, "阿哲");
      throw new Error(`unknown worker ${id}`);
    });
  }

  it("keeps the codename on the row, and the card has no message box", async () => {
    stubRoster([]); // nobody is on the live roster any more
    stubList([mkEntry({ id: "L-9", authorId: RELEASED })]);
    const { container } = await renderPage();

    const row = await waitFor(() => rowById(container, "L-9"));
    await waitFor(() =>
      expect(row.querySelector('[data-testid="lore-author-row"]')?.textContent).toBe("外包 · O-9"),
    );
    await waitFor(() => expect(row.querySelector('[data-testid="lore-message-box"]')).toBeNull());
  });

  it("under a departed staff writer the row keeps the plain name the per-id read serves, and the card has no message box", async () => {
    stubRoster([]);
    stubList([mkEntry({ id: "L-11", authorId: DEPARTED_STAFF })]);
    const { container } = await renderPage();

    const row = await waitFor(() => rowById(container, "L-11"));
    await waitFor(() =>
      expect(
        row.querySelector('[data-testid="lore-author-row"]')?.textContent,
      ).toBe("阿哲"),
    );
    await waitFor(() => expect(row.querySelector('[data-testid="lore-message-box"]')).toBeNull());
  });

  it("while the roster is still loading no per-id read is issued for a live staff writer, and once it lands a departed one is still named", async () => {
    const roster = await api.listMembers();
    stubRoster([]);
    let land: (rows: typeof roster) => void = () => {};
    vi.spyOn(api, "listMembers").mockReturnValue(
      new Promise((resolve) => (land = resolve)),
    );
    const perId = vi.mocked(api.getOutsourceWorker);
    stubList([
      mkEntry({ id: "L-12", authorId: "mira" }),
      mkEntry({ id: "L-13", authorId: DEPARTED_STAFF }),
    ]);
    const { container } = await renderPage();
    await waitFor(() => rowById(container, "L-13"));
    expect(perId.mock.calls.map(([id]) => id)).toEqual([]);

    land(roster);
    await waitFor(() =>
      expect(
        rowById(container, "L-13").querySelector('[data-testid="lore-author-row"]')
          ?.textContent,
      ).toBe("阿哲"),
    );
    expect(perId.mock.calls.map(([id]) => id)).toEqual([DEPARTED_STAFF]);
  });

  it("when the roster read fails a departed staff writer is still named through the per-id read", async () => {
    stubRoster([]);
    vi.spyOn(api, "listMembers").mockRejectedValue(new Error("roster unavailable"));
    stubList([mkEntry({ id: "L-13", authorId: DEPARTED_STAFF })]);
    const { container } = await renderPage();
    await waitFor(() =>
      expect(
        rowById(container, "L-13").querySelector('[data-testid="lore-author-row"]')
          ?.textContent,
      ).toBe("阿哲"),
    );
  });

  it("a 外包 who is STILL on the live roster gets the card's message himself, addressed by bare 代號", async () => {
    stubRoster([worker(LIVE, "O-1")]);
    stubList([mkEntry({ id: "L-10", authorId: LIVE })]);
    const { container } = await renderPage();

    const row = await waitFor(() => rowById(container, "L-10"));
    await waitFor(() =>
      expect(
        row.querySelector<HTMLTextAreaElement>('[data-testid="lore-msg-input"]')!.placeholder,
      ).toBe("傳訊息給 O-1…"),
    );
    expect(row.querySelector('[data-testid="lore-author-row"]')?.textContent).toBe("外包 · O-1");
    expect(await messageRecipientOf(row)).toBe(LIVE);
  });
});

describe("LorePage — 外包條目的適用範圍選單", () => {
  const LIVE = "ow-live";
  const RELEASED = "ow-gone";

  function boundWorker(id: string, codename: string): OutsourceWorkerView {
    return {
      id,
      codename,
      model: "claude-opus-5",
      effort: "medium",
      taskId: "T-42",
      taskNo: "T-42",
      taskTypeKey: "review-pr",
    } as OutsourceWorkerView;
  }

  function outsourceEntry(id: string, authorId: string): LoreEntryView {
    return mkEntry({
      id,
      authorId,
      scopeKind: "agent",
      scopeKey: authorId,
      taskTypeKey: "review-pr",
      scopeOptions: ["manual", "agent", "everyone"],
    });
  }

  function readMenu(container: HTMLElement, entryId: string) {
    const row = rowById(container, entryId);
    fireEvent.click(row.querySelector('[data-testid="lore-scope-name"]')!);
    const text = row.querySelector('[data-testid="lore-scope-options"]')!
      .textContent;
    fireEvent.mouseDown(document.body);
    return text;
  }

  it("shows only labels and tags for an outsource entry, whether or not its author is still on the roster", async () => {
    vi.spyOn(api, "listTaskManuals").mockResolvedValue([
      { typeKey: "review-pr", displayName: "PR 審查", purpose: "", fields: [] },
    ] as never);
    vi.spyOn(api, "listMembers").mockResolvedValue([]);
    vi.spyOn(api, "listOutsourceWorkers").mockResolvedValue([
      boundWorker(LIVE, "O-1"),
    ]);
    vi.spyOn(api, "getOutsourceWorker").mockImplementation(async (id) => {
      if (id === RELEASED) return boundWorker(RELEASED, "O-9");
      return boundWorker(LIVE, "O-1");
    });
    stubList([outsourceEntry("L-1", LIVE), outsourceEntry("L-2", RELEASED)]);
    const { container } = await renderPage({ canSetScope: true });
    await waitFor(() => rowById(container, "L-2"));

    await waitFor(() =>
      expect(readMenu(container, "L-1")).toBe(
        "適用範圍任務：PR 審查建立者：外包 · O-1預設所有人",
      ),
    );
    await waitFor(() =>
      expect(readMenu(container, "L-2")).toBe(
        "適用範圍任務：PR 審查建立者：外包 · O-9預設所有人",
      ),
    );
  });
});
