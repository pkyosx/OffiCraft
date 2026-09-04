// M2-3 member file & image gallery panel (batch-16 upgrade: member-perspective
// scope + 圖片/檔案 tabs).
//
// The panel's whole unit-level surface lives here. This header used to list
// what that surface was; the list went stale twice over before anyone noticed,
// so it is gone — the `it(...)` names below are the inventory, and unlike a
// header they cannot fall behind the tests they describe.

import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  render,
  fireEvent,
  waitFor,
  screen,
  act,
} from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatGalleryPanel, isPreviewableMime } from "./ChatGalleryPanel";
import type { Member } from "../types";
import type { GalleryAttachment } from "../api/adapter";

let galleryRows: GalleryAttachment[] = [];
const listChatAttachments = vi.fn(
  async (_withId: string): Promise<GalleryAttachment[]> => galleryRows,
);
const getChatAttachmentShareLink = vi.fn(
  async (id: string): Promise<string> =>
    `/api/chat/attachment/${id}?sig=test-sig`,
);

let handlers: ((topic: string, delta?: unknown) => void)[] = [];

vi.mock("../api", () => ({
  api: {
    listChatAttachments: (withId: string) => listChatAttachments(withId),
    getChatAttachmentShareLink: (id: string) => getChatAttachmentShareLink(id),
    subscribeEvents: (cb: (topic: string, delta?: unknown) => void) => {
      handlers.push(cb);
      return () => {
        handlers = handlers.filter((x) => x !== cb);
      };
    },
  },
}));

/** Drive the panel's own refetch the way the wire does: a chat delta naming
 * this member. A bare topic (no delta) is the honest "you may have missed
 * anything" branch and re-pulls unconditionally, which is all this needs. */
async function refetchBurst() {
  await act(async () => {
    for (const cb of [...handlers]) cb("chat");
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

function mkMember(id = "m1", name = "Mira"): Member {
  return {
    id,
    name,
    role: "assistant",
    status: "online",
    lifecycle: "online",
    model: "opus",
    effort: "medium",
    kind: "staff",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "member-m1",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
  };
}

function row(
  id: string,
  mime: string,
  from: string,
  fromName: string,
  ts: number,
  filename = `${id}.bin`,
): GalleryAttachment {
  return {
    id,
    url: `/api/chat/attachment/${id}`,
    filename,
    mime,
    isImage: mime.startsWith("image/"),
    messageId: `msg-${id}`,
    from,
    fromName,
    to: from === "owner" ? "m1" : "owner",
    ts,
  };
}

function renderPanel(
  onClose: () => void = () => {},
  resolveSender?: (id: string) => string,
) {
  return render(
    <I18nProvider>
      <ChatGalleryPanel
        member={mkMember()}
        resolveSender={resolveSender}
        onClose={onClose}
      />
    </I18nProvider>,
  );
}

const itemsIn = (container: HTMLElement) => [
  ...container.querySelectorAll<HTMLElement>(".chat__gallery-item"),
];

describe("ChatGalleryPanel", () => {
  beforeEach(() => {
    galleryRows = [];
    listChatAttachments.mockClear();
    localStorage.clear();
  });

  it("fetches the member's flattened gallery (listChatAttachments)", async () => {
    renderPanel();
    await waitFor(() => expect(listChatAttachments).toHaveBeenCalledWith("m1"));
  });

  it("splits 圖片/檔案 tabs and renders sender + time per row (incl. inter-agent)", async () => {
    galleryRows = [
      // Server order: newest→oldest. An inter-agent row (Bob→Mira) rides the
      // SAME list — it must surface, labelled with the SERVER-resolved name.
      row("a3", "application/pdf", "m2", "Bob", 300, "from-bob.pdf"),
      row("a2", "application/pdf", "m1", "Mira", 200, "r.pdf"),
      row("a1", "image/png", "owner", "", 100, "shot.png"),
    ];
    const { container } = renderPanel();
    // Default tab: 圖片 — only the image shows.
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    expect(itemsIn(container)[0].querySelector("img")).toBeTruthy();
    // The owner's row reads 我 (zh default locale); time from the message ts.
    expect(itemsIn(container)[0].textContent).toContain("我");
    expect(
      itemsIn(container)[0].querySelector(".chat__gallery-sub")?.textContent,
    ).toMatch(/\d{1,2}:\d{2}/);
    expect(container.textContent).not.toContain("r.pdf");
    // Switch to 檔案 — both files show, newest first, sender names shown; the
    // raw member ids never render (only display names).
    fireEvent.click(screen.getByRole("tab", { name: "檔案" }));
    await waitFor(() => expect(itemsIn(container).length).toBe(2));
    expect(itemsIn(container)[0].textContent).toContain("from-bob.pdf");
    expect(itemsIn(container)[0].textContent).toContain("Bob");
    expect(itemsIn(container)[1].textContent).toContain("Mira");
    expect(container.textContent).not.toContain("m2");
    expect(container.textContent).not.toContain("shot.png");
  });

  it("defers every thumbnail's fetch instead of requesting the whole tab at once", async () => {
    galleryRows = Array.from({ length: 30 }, (_, i) =>
      row(`img${i}`, "image/png", "owner", "", 1000 - i, `shot-${i}.png`),
    );
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(30));
    const thumbs = [
      ...container.querySelectorAll<HTMLImageElement>("img.chat__gallery-thumb"),
    ];
    expect(thumbs.length).toBe(30);
    // jsdom loads no images, so what is knowable here is the attribute that
    // makes a real browser skip the ones below the fold. The request counts
    // themselves are in the step note, measured in Chromium.
    expect(thumbs.every((img) => img.getAttribute("loading") === "lazy")).toBe(
      true,
    );
  });

  it("opens every stored attachment in the shared modal", async () => {
    galleryRows = [
      row("a1", "image/png", "owner", "", 100, "img.png"),
      row("a2", "text/markdown", "owner", "", 100, "notes.md"),
      row("a3", "application/pdf", "owner", "", 100, "doc.pdf"),
      row("a4", "application/zip", "owner", "", 100, "bundle.zip"),
    ];
    const { container } = renderPanel();
    fireEvent.click(screen.getByRole("tab", { name: "檔案" }));
    await waitFor(() => expect(itemsIn(container).length).toBe(3));
    const byName = (name: string) =>
      itemsIn(container).find((item) => item.textContent?.includes(name))!;
    fireEvent.click(byName("notes.md"));
    expect(
      await screen.findByRole("dialog", { name: "notes.md" }),
    ).toBeTruthy();
    fireEvent.click(screen.getByLabelText("關閉預覽"));
    fireEvent.click(byName("doc.pdf"));
    expect(await screen.findByRole("dialog", { name: "doc.pdf" })).toBeTruthy();
    // T-36 (B1) — the pdf still cannot be DRAWN in the panel, but the header
    // now carries 「在新頁面顯示」 for it, so the body line points at that button
    // instead of back at 下載 once the share link has been minted.
    expect(
      await screen.findByText(
        "此檔案無法在這裡預覽，請用上方的「在新頁面顯示」開啟。",
      ),
    ).toBeTruthy();
    fireEvent.click(screen.getByLabelText("關閉預覽"));
    fireEvent.click(byName("bundle.zip"));
    expect(
      await screen.findByRole("dialog", { name: "bundle.zip" }),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("tab", { name: "圖片" }));
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    fireEvent.click(itemsIn(container)[0]);
    expect(await screen.findByRole("dialog", { name: "img.png" })).toBeTruthy();
  });

  it("opens PDF and binaries only from their row, not a duplicate share button", async () => {
    galleryRows = [
      row("pdf-att", "application/pdf", "owner", "", 100, "doc.pdf"),
      row("zip-att", "application/zip", "owner", "", 99, "bundle.zip"),
    ];
    const { container } = renderPanel();
    fireEvent.click(screen.getByRole("tab", { name: "檔案" }));
    await waitFor(() => expect(itemsIn(container).length).toBe(2));
    const pdf = itemsIn(container).find((item) =>
      item.textContent?.includes("doc.pdf"),
    )!;
    const zip = itemsIn(container).find((item) =>
      item.textContent?.includes("bundle.zip"),
    )!;
    expect(container.querySelector(".chat__gallery-share")).toBeNull();
    fireEvent.click(pdf);
    expect(await screen.findByRole("dialog", { name: "doc.pdf" })).toBeTruthy();
    fireEvent.click(screen.getByLabelText("關閉預覽"));
    fireEvent.click(zip);
    expect(
      await screen.findByRole("dialog", { name: "bundle.zip" }),
    ).toBeTruthy();
  });

  it("opens a previewable row from Enter and Space without letting the nested share button open it", async () => {
    galleryRows = [row("image-att", "image/png", "owner", "", 100, "shot.png")];
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    const galleryRow = itemsIn(container)[0];
    expect(galleryRow.getAttribute("role")).toBe("button");
    expect(galleryRow.getAttribute("tabindex")).toBe("0");
    fireEvent.keyDown(galleryRow, { key: "Enter" });
    expect(
      await screen.findByRole("dialog", { name: "shot.png" }),
    ).toBeTruthy();
    fireEvent.click(screen.getByLabelText("關閉預覽"));
    fireEvent.keyDown(galleryRow, { key: " " });
    expect(
      await screen.findByRole("dialog", { name: "shot.png" }),
    ).toBeTruthy();
  });

  it("shows per-tab honest empty states once loaded", async () => {
    galleryRows = [row("a1", "application/pdf", "m1", "Mira", 100, "only.pdf")];
    const first = renderPanel();
    // 圖片 tab (default) is empty even though a FILE exists.
    expect(await screen.findByText("還沒有圖片")).toBeTruthy();
    fireEvent.click(screen.getByRole("tab", { name: "檔案" }));
    await waitFor(() => expect(screen.queryByText("還沒有圖片")).toBeNull());
    expect(screen.getByText("only.pdf")).toBeTruthy();
    first.unmount();
    // And the 檔案 tab's own empty state when nothing at all exists.
    galleryRows = [];
    renderPanel();
    await screen.findByRole("dialog");
    fireEvent.click(screen.getByRole("tab", { name: "檔案" }));
    expect(await screen.findByText("還沒有檔案")).toBeTruthy();
  });

  // ── Uploader filter (batch 18, reshaped by T-51 ②) ────────────────────────
  // ONE control under the tabs; the uploaders live behind it in a fixed-height
  // scrolling popover with a checkbox each. What it replaced was a wrapping
  // chip row with no cap: measured on a 2,200-file corpus it stood 1,168px tall
  // inside a 696px panel and pushed the file list off the screen entirely.
  //
  // 🔴 THE OPTIONS ARE CUT FROM THE TAB BEING SHOWN. They used to come from
  // every row in both tabs while the list applied the tab, so 圖片 offered
  // uploaders who had only ever sent non-images and ticking one answered with
  // an empty gallery — 66 of the owner's 114 uploaders were dead options that
  // way (Kyle, 2026-09-02).
  //
  // 🔴 That kills the DEAD OPTION, not the over-filtered empty view. A tick
  // outlives the rows it was made on (a refetch can take a ticked uploader's
  // images away while their files stay, and the prune only drops uploaders
  // absent from EVERY row), so the panel still needs its third sentence — see
  // "says the FILTER is empty, not the gallery" below. An earlier version of
  // this note claimed the view was unreachable and that nothing tested it; both
  // halves were wrong.

  const filterToggle = () =>
    screen.getByRole("button", { name: "依上傳者篩選" });
  /** Open the popover (a real click sends mousedown too, and the popover's
   * dismiss-on-outside-click listens for it — firing only `click` here would
   * describe a product that does not exist). */
  const openFilter = () => {
    fireEvent.mouseDown(filterToggle());
    fireEvent.click(filterToggle());
  };
  const optionLabels = () =>
    [...document.querySelectorAll(".chat__gallery-sender-option-name")].map(
      (n) => n.textContent,
    );
  const tickOption = (label: string) => {
    const opt = [
      ...document.querySelectorAll<HTMLLabelElement>(
        ".chat__gallery-sender-option",
      ),
    ].find((l) => l.textContent?.startsWith(label))!;
    fireEvent.click(opt.querySelector("input")!);
  };

  it("offers one option per actual uploader of the tab being shown, named not id'd", async () => {
    galleryRows = [
      row("a4", "image/png", "m2", "Bob", 400, "bob.png"),
      row("a3", "image/png", "owner", "", 300, "mine.png"),
      row("a2", "image/png", "m1", "Mira", 200, "mira.png"),
      row("a1", "application/pdf", "m2", "Bob", 100, "bob.pdf"),
    ];
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(3));
    openFilter();
    // The three uploaders WITH AN IMAGE — owner reads 「我」, others by their
    // server-resolved names; no raw internal ids leak.
    // ORDER IS DELIBERATELY NOT ASSERTED HERE. Sorting is by name
    // (`localeCompare(…, "zh-Hant")`), and where Han sits relative to Latin is
    // the platform's collation, not this component's decision — pinning it here
    // would make an unrelated test fail on an engine with a different ICU. The
    // order that IS ours is pinned in "orders uploaders by name", on an
    // all-ASCII fixture where every collation agrees.
    expect(optionLabels()).toHaveLength(3);
    expect(new Set(optionLabels())).toEqual(new Set(["Bob", "我", "Mira"]));
    // Tick Bob → only Bob's image remains on the 圖片 tab.
    tickOption("Bob");
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    expect(itemsIn(container)[0].textContent).toContain("bob.png");
    expect(container.textContent).not.toContain("mine.png");
    expect(container.textContent).not.toContain("mira.png");
  });

  it("orders uploaders by name, not by how much they sent", async () => {
    // 🔴 Order is what makes a list of a hundred people usable: unordered, the
    // dropdown is the chip row's problem in a smaller box. BY NAME is the
    // owner's ruling (`c-6143bd5a861d`), overturning this PR's first version,
    // which sorted by volume. The fixture is built so the two orders DISAGREE:
    // by volume it would read Charlie(3), Bravo(2), Alpha(1).
    galleryRows = [
      row("a5", "image/png", "m1", "Alpha", 500, "one.png"),
      row("a4", "image/png", "m2", "Charlie", 400, "three-a.png"),
      row("a3", "image/png", "m2", "Charlie", 300, "three-b.png"),
      row("a2", "image/png", "m2", "Charlie", 200, "three-c.png"),
      row("a1", "image/png", "m3", "Bravo", 100, "two-a.png"),
      row("a0", "image/png", "m3", "Bravo", 50, "two-b.png"),
    ];
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(6));
    openFilter();
    expect(optionLabels()).toEqual(["Alpha", "Bravo", "Charlie"]);
  });

  it("offers no uploader whose only files are on the other tab", async () => {
    galleryRows = [
      row("a2", "image/png", "m1", "Mira", 200, "mira.png"),
      row("a1", "application/pdf", "m2", "Bob", 100, "bob.pdf"),
    ];
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    openFilter();
    expect(
      optionLabels(),
      "Bob has no image, so 圖片 must not offer a tick that answers with nothing",
    ).toEqual(["Mira"]);
    // His file is one tab away, and there he IS an option. (mousedown too: an
    // open popover dismisses on an outside mousedown, and a click that skipped
    // it would leave the popover open and turn the openFilter() below into a
    // close.)
    fireEvent.mouseDown(screen.getByRole("tab", { name: "檔案" }));
    fireEvent.click(screen.getByRole("tab", { name: "檔案" }));
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    openFilter();
    expect(optionLabels()).toEqual(["Bob"]);
  });

  it("keeps each tab's ticks while the reader looks at the other tab", async () => {
    // 🔴 A GLANCE MUST NOT COST A SELECTION. The first draft kept one shared set
    // and pruned it whenever the options changed, so ticking someone on 圖片,
    // switching to 檔案 and switching back left the filter silently on 全部 —
    // the reader's choice deleted by the act of looking somewhere else. The two
    // tabs hold different populations, so each keeps its own ticks.
    galleryRows = [
      row("a3", "image/png", "m2", "Bob", 300, "bob.png"),
      row("a2", "image/png", "m1", "Mira", 200, "mira.png"),
      row("a1", "application/pdf", "m1", "Mira", 100, "mira.pdf"),
    ];
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(2));
    openFilter();
    tickOption("Bob");
    await waitFor(() => expect(itemsIn(container).length).toBe(1));

    fireEvent.mouseDown(screen.getByRole("tab", { name: "檔案" }));
    fireEvent.click(screen.getByRole("tab", { name: "檔案" }));
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    expect(
      filterToggle().textContent,
      "the other tab has its own ticks — Bob's does not follow the reader there",
    ).toContain("全部");

    fireEvent.mouseDown(screen.getByRole("tab", { name: "圖片" }));
    fireEvent.click(screen.getByRole("tab", { name: "圖片" }));
    await waitFor(() =>
      expect(filterToggle().textContent).toContain("已選 1 位"),
    );
    expect(itemsIn(container).length).toBe(1);
    expect(itemsIn(container)[0].textContent).toContain("bob.png");
  });

  it("drops the ticks when the panel is pointed at a different member", async () => {
    // 🔴 A FILTER BELONGS TO THE GALLERY IT WAS MADE IN. Carried across a member
    // switch it filters rows it was never about: tick 「我」 on A's 圖片, switch
    // to B who has images but none from me, and the panel says 「還沒有圖片」
    // about a gallery that is not empty. The refetch prune cannot catch it — it
    // only drops ids absent from EVERY row, and 「我」 is in plenty of B's.
    galleryRows = [row("a1", "image/png", "owner", "", 100, "mine.png")];
    const { container, rerender } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    openFilter();
    tickOption("我");
    expect(filterToggle().textContent).toContain("已選 1 位");

    // 🔴 B'S ROWS MUST INCLUDE THE TICKED UPLOADER. With B's gallery holding
    // nothing from 「我」, the refetch prune above (it drops ids absent from
    // EVERY fresh row) already lands on 「全部」 — and this test passes with the
    // member-switch reset deleted, which is the one thing it exists to pin.
    // 「我」 is in plenty of a real member's rows; that is the whole premise of
    // the comment above.
    galleryRows = [
      row("b1", "image/png", "m9", "Bea", 100, "bea.png"),
      row("b2", "image/png", "owner", "", 90, "mine-on-b.png"),
    ];
    rerender(
      <I18nProvider>
        <ChatGalleryPanel
          member={{ ...mkMember(), id: "m-other" }}
          onClose={() => {}}
        />
      </I18nProvider>,
    );
    // The filter text goes INSIDE the wait: as a bare assertion after a
    // `waitFor` on the row count, a regression prints "expected 1 to be 2" from
    // the count and the named assertions never run — the CI line would not say
    // what broke.
    await waitFor(() => {
      expect(
        filterToggle().textContent,
        "the new member's gallery opens unfiltered",
      ).toContain("全部");
      expect(itemsIn(container).length).toBe(2);
    });
    expect(
      itemsIn(container)
        .map((el) => el.textContent)
        .join(" "),
      "B's own rows, not the ones A's filter would have kept",
    ).toContain("bea.png");
  });

  it("says the FILTER is empty, not the gallery, when a refetch empties the tab under a live tick", async () => {
    // The reachable shape of the two-sentence empty state, and the reason the
    // panel carries a third string at all. The prune only drops uploaders that
    // vanished from EVERY row — so an uploader whose images go away while their
    // files remain keeps their tick, with nothing left to show on 圖片. Saying
    // 「還沒有圖片」 there tells the reader their files are gone; they are not,
    // their filter is on.
    galleryRows = [
      row("a1", "image/png", "m9", "Bea", 100, "bea.png"),
      row("a2", "image/png", "owner", "", 90, "mine.png"),
    ];
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(2));
    openFilter();
    tickOption("Bea");
    await waitFor(() => expect(itemsIn(container).length).toBe(1));

    // Bea's image is gone; her FILE is not, so the prune keeps her tick alive.
    galleryRows = [
      row("a3", "application/zip", "m9", "Bea", 80, "bea.zip"),
      row("a2", "image/png", "owner", "", 90, "mine.png"),
    ];
    await refetchBurst();

    expect(
      await screen.findByText("選取的上傳者在這個分頁沒有檔案"),
    ).toBeTruthy();
    expect(
      screen.queryByText("還沒有圖片"),
      "the gallery is not empty — 「我」 still has an image here",
    ).toBeNull();
  });

  it("keeps a departed member in the list so their old files stay reachable", async () => {
    // The server resolves a sender's name at ANY roster status on purpose
    // (api_chat.go: "ANY roster status — dismissed still reads by name").
    // Folding the row into a dropdown must not become "removed from the list":
    // the gallery is where an old file is found, and old files come from people
    // who have since left.
    galleryRows = [
      row("a2", "image/png", "m9", "已解僱的同事", 200, "old.png"),
      row("a1", "image/png", "m1", "Mira", 100, "mira.png"),
    ];
    renderPanel();
    await waitFor(() => expect(screen.queryByText("old.png")).toBeTruthy());
    openFilter();
    expect(optionLabels()).toContain("已解僱的同事");
  });

  it("resolves an unnamed outsource sender through resolveSender, not the raw id", async () => {
    // A row whose from_name the server left "" — whatever the cause. The
    // caller-provided resolver (ChatArea's nameOf codename chain) is what names
    // the row and its uploader option; without a resolver hit the raw id shows.
    //
    // ⚠️ This used to say the blank is guaranteed, because the gallery handler's
    // names table is `WHERE kind != 'outsource'`. That reason is dead twice over
    // (the handler had already moved to the wider roster read, and T-14 項目 6
    // deleted the narrow query), so this is a test of the RESOLVER PATH given a
    // blank name, not evidence that outsource names arrive blank.
    galleryRows = [
      row("a1", "image/png", "ow-533c0c4f9dba", "", 100, "work.png"),
    ];
    const { container } = renderPanel(undefined, (id) =>
      id === "ow-533c0c4f9dba" ? "外包 · X-1" : id,
    );
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    expect(itemsIn(container)[0].textContent).toContain("外包 · X-1");
    openFilter();
    expect(optionLabels()).toEqual(["外包 · X-1"]);
    expect(container.textContent).not.toContain("ow-533c0c4f9dba");
  });

  it("narrows to the union of every ticked uploader and says how many are ticked", async () => {
    galleryRows = [
      row("a3", "image/png", "m2", "Bob", 300, "bob.png"),
      row("a2", "image/png", "m1", "Mira", 200, "mira.png"),
      row("a1", "image/png", "m3", "Cy", 100, "cy.png"),
    ];
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(3));
    openFilter();
    tickOption("Bob");
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    expect(filterToggle().textContent).toContain("已選 1 位");
    // A SECOND tick widens the answer. The chip row this replaced could only
    // ever hold one uploader — picking a second one dropped the first.
    tickOption("Mira");
    await waitFor(() => expect(itemsIn(container).length).toBe(2));
    expect(container.textContent).toContain("bob.png");
    expect(container.textContent).toContain("mira.png");
    // Clearing goes back to 全部, not to "one of them".
    fireEvent.click(screen.getByRole("button", { name: "清除選取" }));
    await waitFor(() => expect(itemsIn(container).length).toBe(3));
    expect(filterToggle().textContent).toContain("全部");
  });

  it("stays one line high when closed, however many uploaders there are", async () => {
    // 🔴 THE HEIGHT IS THE BUG. jsdom applies no CSS, so this cannot measure
    // pixels — what it CAN pin is the structural cause of those pixels: how
    // many controls the closed filter puts on the page. The chip row rendered
    // one per uploader (plus 全部); this must render exactly one whatever the
    // count, with the uploaders behind it. The pixel geometry is measured in a
    // real browser by the visual guard.
    galleryRows = Array.from({ length: 60 }, (_, i) =>
      row(`a${i}`, "image/png", `m${i}`, `Sender ${i}`, 100 + i, `s${i}.png`),
    );
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(60));
    const filter = container.querySelector(".chat__gallery-senders")!;
    expect(filter.querySelectorAll("button").length).toBe(1);
    expect(
      document.querySelectorAll(".chat__gallery-sender-option").length,
      "the 60 uploaders are behind the toggle until it is opened",
    ).toBe(0);
    openFilter();
    expect(
      document.querySelectorAll(".chat__gallery-sender-option").length,
    ).toBe(60);
  });

  it("pages the preview across the list the reader is looking at, not every attachment", async () => {
    galleryRows = [
      row("a3", "image/png", "m2", "Bob", 300, "bob.png"),
      row("a2", "image/png", "m1", "Mira", 200, "mira.png"),
      row("a1", "application/pdf", "m1", "Mira", 100, "mira.pdf"),
    ];
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(2));
    fireEvent.click(itemsIn(container)[0]);
    // Two images on this tab; the pdf is not one of them, and the counter is
    // the pager's own statement of what it will walk.
    expect(
      document.querySelector(".md-preview__pager-count")?.textContent,
    ).toBe("1 / 2");
    fireEvent.click(screen.getByRole("button", { name: "下一個" }));
    expect(
      document.querySelector(".md-preview__pager-count")?.textContent,
    ).toBe("2 / 2");
    expect(
      (screen.getByRole("button", { name: "下一個" }) as HTMLButtonElement)
        .disabled,
      "the last item has nothing after it, and the control says so",
    ).toBe(true);
    expect(
      (screen.getByRole("button", { name: "上一個" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false);
  });

  it("closes an open preview when the member changes, never over the new gallery", async () => {
    // The panel is re-rendered with a new `member` rather than remounted, so a
    // key minted for the previous member's row is still on screen — and it
    // still resolves against the rows underneath until the new fetch replaces
    // them (T-48, R9-2).
    galleryRows = [row("a1", "image/png", "m1", "Mira", 100, "A-的機密.png")];
    const view = render(
      <I18nProvider>
        <ChatGalleryPanel member={mkMember()} onClose={() => {}} />
      </I18nProvider>,
    );
    await waitFor(() => expect(itemsIn(view.container).length).toBe(1));
    fireEvent.click(itemsIn(view.container)[0]);
    expect(await screen.findByRole("dialog", { name: "A-的機密.png" })).toBeTruthy();

    view.rerender(
      <I18nProvider>
        <ChatGalleryPanel member={mkMember("m2", "Bruno")} onClose={() => {}} />
      </I18nProvider>,
    );
    expect(screen.queryByRole("dialog", { name: "A-的機密.png" })).toBeNull();
  });

  it("keeps gallery rows free of a duplicate share control", async () => {
    galleryRows = [row("a1", "image/png", "owner", "", 100, "shot.png")];
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    expect(container.querySelector(".chat__gallery-share")).toBeNull();
    fireEvent.click(itemsIn(container)[0]);
    const popup = await screen.findByRole("dialog", { name: "shot.png" });
    expect(popup.querySelector("button.md-preview__share")).toBeTruthy();
  });

  it("closes via the close button and via Escape", async () => {
    const onClose = vi.fn();
    renderPanel(onClose);
    await screen.findByRole("dialog");
    fireEvent.click(screen.getByLabelText("關閉檔案庫"));
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(window, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it("lets Escape close an open preview without also closing the gallery", async () => {
    galleryRows = [row("a1", "image/png", "owner", "", 100, "shot.png")];
    const onClose = vi.fn();
    const { container } = renderPanel(onClose);
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    fireEvent.click(itemsIn(container)[0]);
    expect(
      await screen.findByRole("dialog", { name: "shot.png" }),
    ).toBeTruthy();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("dialog", { name: "shot.png" })).toBeNull();
    expect(container.querySelector(".chat__gallery")).toBeTruthy();
    expect(onClose).not.toHaveBeenCalled();
  });
});

describe("isPreviewableMime (pure)", () => {
  it("mirrors the server's preview table", () => {
    expect(isPreviewableMime("image/webp")).toBe(true);
    expect(isPreviewableMime("text/html")).toBe(true);
    expect(isPreviewableMime("text/markdown")).toBe(true);
    expect(isPreviewableMime("application/pdf")).toBe(true);
    expect(isPreviewableMime("application/zip")).toBe(false);
    expect(isPreviewableMime("application/octet-stream")).toBe(false);
  });
});
