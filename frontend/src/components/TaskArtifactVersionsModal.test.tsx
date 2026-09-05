// TaskArtifactVersionsModal — reading a pinned deliverable's retained versions
// (T-60).
//
// 🔴 The load-bearing assertion is the one the document reader states as its
// only criterion: what the diff says must equal the actual state. Here that
// means the 「目前版本」 side is the artifact the SERVER hands back when the modal
// opens, never a row this client was already holding — the stale-cache case
// below hands the two different content and requires the server's to win.
//
// The 差異 option now exists ONLY for two texts (owner 2026-09-03,
// c-5d9766b7f0a0: 「好像只有文字檔需要有差異那個選項」), so the rest of this file
// pins two separate things: that a text pair — including the octet-stream .md
// report the mime alone would call opaque — really reaches the shared DiffView,
// and that the 內容 pane still shows every other kind what it always showed.
// A non-text response is still never read as text — the body is dropped unread.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { api } from "../api";
import { TaskArtifactVersionsModal } from "./TaskArtifactVersionsModal";
import { toTaskArtifactVersion } from "../api/mappers";
import type {
  TaskArtifactView,
  TaskArtifactVersionView,
} from "../api/adapter";

vi.mock("../api", () => ({
  api: {
    listTaskArtifacts: vi.fn(),
    listTaskArtifactVersions: vi.fn(),
    subscribeEvents: () => () => {},
  },
}));

const mockedApi = api as unknown as {
  listTaskArtifacts: ReturnType<typeof vi.fn>;
  listTaskArtifactVersions: ReturnType<typeof vi.fn>;
};

const realFetch = globalThis.fetch;

// The LIVE artifact row, T-92 shape: `name` (never empty on the wire — the
// server derives one from the blob's filename when the row has no stored one)
// plus `description`, and no filename/is_image/attachment_id of its own.
function mkArtifact(over: Partial<TaskArtifactView>): TaskArtifactView {
  return {
    id: "ta-1",
    kind: "file",
    url: "/api/chat/attachment/att-live",
    name: "spec.txt",
    description: "",
    mime: "text/plain",
    createdTs: 2000,
    createdBy: "mira",
    versionCount: 2,
    ...over,
  };
}

// A retained VERSION, which T-92 deliberately did NOT narrow: it still carries
// its own filename/is_image/attachment_id, and its `name` is the stored column
// rather than a derivation — so unlike the live row's it can be empty.
function mkVersion(over: Partial<TaskArtifactVersionView>): TaskArtifactVersionView {
  return {
    id: 1,
    kind: "file",
    url: "/api/chat/attachment/att-old",
    name: "spec.txt",
    description: "",
    filename: "spec.txt",
    mime: "text/plain",
    isImage: false,
    attachmentId: "att-old",
    createdTs: 1000,
    createdBy: "mira",
    ...over,
  };
}

function mkArtifacts(artifacts: TaskArtifactView[]): TaskArtifactView[] {
  return artifacts;
}

/** A fetch that answers per blob path with a declared content type. `cancel`
 * records that a body was dropped unread; `text` records that one was read. */
function stubFetch(blobs: Record<string, { mime: string; text?: string }>) {
  const cancel = vi.fn(async () => {});
  const readText = vi.fn();
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input).split("?")[0]!;
    const blob = blobs[path];
    if (!blob) {
      return { ok: false, status: 404, headers: new Headers() } as unknown as Response;
    }
    return {
      ok: true,
      status: 200,
      headers: new Headers({ "content-type": blob.mime }),
      text: async () => {
        readText(path);
        return blob.text ?? "";
      },
      body: { cancel },
    } as unknown as Response;
  }) as unknown as typeof fetch;
  return { cancel, readText };
}

function openModal() {
  return render(
    <I18nProvider>
      <TaskArtifactVersionsModal taskId="t-1" artifactId="ta-1" onClose={() => {}} />
    </I18nProvider>,
  );
}

/** One left-column row's rendered text — the name the reader picks the version
 * by, read off the row rather than searched for in the panel's prose. */
function rowName(testId: string): string {
  const name = screen.getByTestId(testId).querySelector(".ta-versions__row-name");
  return name?.textContent ?? "";
}

/** The same row's secondary line — the live row carries its name there, under a
 * fixed 「目前版本」 heading. */
function rowMeta(testId: string): string {
  const meta = screen.getByTestId(testId).querySelector(".ta-versions__row-meta");
  return meta?.textContent ?? "";
}

/** The rendered diff's text cells, in order — the comparison as DATA, not as
 * a keyword search over the panel's prose. */
function diffLinesOnScreen(): string[] {
  return [...screen.getByTestId("ta-versions-diff").querySelectorAll(".diff-view__text")]
    .map((cell) => cell.textContent ?? "")
    // The unified view renders one text cell per row; a blank line is an NBSP.
    .map((s) => s.replace(/\u00a0/g, ""));
}

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(() => {
  globalThis.fetch = realFetch;
});

describe("TaskArtifactVersionsModal", () => {
  it("diffs against the artifact the SERVER holds, not a row handed in from elsewhere", async () => {
    // The task read is the ONLY source of the 「目前版本」 side. If this modal ever
    // learns to accept the popover's artifact row instead, this stays the
    // assertion that reddens: the popover's row is stale by construction.
    mockedApi.listTaskArtifactVersions.mockResolvedValue([mkVersion({})]);
    mockedApi.listTaskArtifacts.mockResolvedValue(
      mkArtifacts([mkArtifact({ url: "/api/chat/attachment/att-fresh" })]),
    );
    stubFetch({
      "/api/chat/attachment/att-old": { mime: "text/plain", text: "one\n" },
      "/api/chat/attachment/att-fresh": { mime: "text/plain", text: "two\n" },
      "/api/chat/attachment/att-live": { mime: "text/plain", text: "STALE\n" },
    });
    openModal();

    fireEvent.click(await screen.findByTestId("ta-versions-pane-diff"));
    await waitFor(() => expect(screen.getByTestId("ta-versions-diff")).toBeTruthy());
    expect(diffLinesOnScreen()).toEqual(["one", "two"]);
    expect(mockedApi.listTaskArtifacts).toHaveBeenCalledWith("t-1");
  });

  // The artifact was un-pinned from another surface while this was opening, so
  // there is no 「目前版本」 to read. The retained version is still readable; the
  // current row says out loud that it is gone rather than showing the last thing
  // this client happened to know.
  it("says the current version is gone while still reading the retained one", async () => {
    mockedApi.listTaskArtifactVersions.mockResolvedValue([mkVersion({})]);
    mockedApi.listTaskArtifacts.mockResolvedValue(mkArtifacts([]));
    stubFetch({ "/api/chat/attachment/att-old": { mime: "text/plain", text: "one\n" } });
    openModal();

    await waitFor(() =>
      expect(screen.getByTestId("ta-versions-content-text").textContent).toBe("one\n"),
    );
    fireEvent.click(screen.getByTestId("ta-versions-row-live"));
    expect(screen.getByTestId("ta-versions-unpinned")).toBeTruthy();
  });

  it("compares two text files through the shared DiffView", async () => {
    mockedApi.listTaskArtifactVersions.mockResolvedValue([mkVersion({})]);
    mockedApi.listTaskArtifacts.mockResolvedValue(mkArtifacts([mkArtifact({})]));
    stubFetch({
      "/api/chat/attachment/att-old": { mime: "text/plain", text: "alpha\nbeta\n" },
      "/api/chat/attachment/att-live": { mime: "text/plain", text: "alpha\ngamma\n" },
    });
    openModal();

    fireEvent.click(await screen.findByTestId("ta-versions-pane-diff"));
    await waitFor(() => expect(screen.getByTestId("ta-versions-diff")).toBeTruthy());
    expect(diffLinesOnScreen()).toEqual(["alpha", "beta", "gamma"]);
  });

  // 🔴 The deliverable class this reader exists for: an agent-uploaded report
  // comes back `application/octet-stream`, which is an upload path saying it
  // does not know rather than a claim of binary. A mime-only rule would send
  // every .md / log / spec to the 前/後 toggle, where it can never be diffed.
  it("diffs an octet-stream .md report the mime alone would call opaque", async () => {
    mockedApi.listTaskArtifactVersions.mockResolvedValue([
      mkVersion({ name: "recon.md", filename: "" }),
    ]);
    mockedApi.listTaskArtifacts.mockResolvedValue(
      mkArtifacts([mkArtifact({ name: "recon.md", mime: "application/octet-stream" })]),
    );
    stubFetch({
      "/api/chat/attachment/att-old": {
        mime: "application/octet-stream",
        text: "alpha\nbeta\n",
      },
      "/api/chat/attachment/att-live": {
        mime: "application/octet-stream",
        text: "alpha\ngamma\n",
      },
    });
    openModal();

    fireEvent.click(await screen.findByTestId("ta-versions-pane-diff"));
    await waitFor(() => expect(screen.getByTestId("ta-versions-diff")).toBeTruthy());
    expect(diffLinesOnScreen()).toEqual(["alpha", "beta", "gamma"]);
  });

  // 🔴 The same report, with the name the wire ACTUALLY carries for it: a
  // retained version's `name` is the stored column and is usually empty (nothing
  // makes an agent pass one), so its only name is the filename resolved from its
  // own retained blob. The LIVE side has no such gap — the server derives its
  // `name` from that same blob filename — and this asymmetry is exactly why the
  // version row kept its `filename` when T-92 narrowed the live one. A reader
  // that consults `name` alone leaves this — the ticket's own motivating
  // artifact — permanently on the 前/後 toggle.
  it("diffs a name-less octet-stream version its own filename names as .md", async () => {
    mockedApi.listTaskArtifactVersions.mockResolvedValue([
      mkVersion({ name: "", filename: "recon.md" }),
    ]);
    mockedApi.listTaskArtifacts.mockResolvedValue(
      mkArtifacts([mkArtifact({ name: "recon.md", mime: "application/octet-stream" })]),
    );
    stubFetch({
      "/api/chat/attachment/att-old": {
        mime: "application/octet-stream",
        text: "alpha\nbeta\n",
      },
      "/api/chat/attachment/att-live": {
        mime: "application/octet-stream",
        text: "alpha\ngamma\n",
      },
    });
    openModal();

    fireEvent.click(await screen.findByTestId("ta-versions-pane-diff"));
    await waitFor(() => expect(screen.getByTestId("ta-versions-diff")).toBeTruthy());
    expect(diffLinesOnScreen()).toEqual(["alpha", "beta", "gamma"]);
  });

  // 🔴 THE SHAPE THE SERVER ACTUALLY SENDS, mapped rather than invented. Every
  // other case here builds its version through mkVersion, which hands itself a
  // url — and a version list whose url the wire never carried is exactly how a
  // file version that reached this panel with url "" (and was read as gone)
  // stayed invisible to a green suite. So this one starts from the wire JSON a
  // replaced .md report produces and runs it through the real mapper.
  it("diffs a retained report built from the wire shape the server sends", async () => {
    mockedApi.listTaskArtifactVersions.mockResolvedValue([
      toTaskArtifactVersion({
        id: 1,
        kind: "file",
        url: "/api/chat/attachment/att-old",
        name: "",
        description: "",
        filename: "report.md",
        mime: "application/octet-stream",
        is_image: false,
        attachment_id: "att-old",
        created_ts: 1000,
        created_by: "mira",
      }),
    ]);
    mockedApi.listTaskArtifacts.mockResolvedValue(
      mkArtifacts([
        mkArtifact({
          name: "report.md",
          mime: "application/octet-stream",
        }),
      ]),
    );
    stubFetch({
      "/api/chat/attachment/att-old": {
        mime: "application/octet-stream",
        text: "alpha\nbeta\n",
      },
      "/api/chat/attachment/att-live": {
        mime: "application/octet-stream",
        text: "alpha\ngamma\n",
      },
    });
    openModal();

    fireEvent.click(await screen.findByTestId("ta-versions-pane-diff"));
    await waitFor(() => expect(screen.getByTestId("ta-versions-diff")).toBeTruthy());
    expect(diffLinesOnScreen()).toEqual(["alpha", "beta", "gamma"]);
  });

  // The other side of the same rule: the fallback is a CLOSED list of textual
  // extensions, not "octet-stream means try reading it".
  it("calls an octet-stream .bin opaque and reads none of its bytes", async () => {
    mockedApi.listTaskArtifactVersions.mockResolvedValue([
      mkVersion({ name: "core.bin", filename: "" }),
    ]);
    mockedApi.listTaskArtifacts.mockResolvedValue(
      mkArtifacts([mkArtifact({ name: "core.bin", mime: "application/octet-stream" })]),
    );
    const { cancel, readText } = stubFetch({
      "/api/chat/attachment/att-old": { mime: "application/octet-stream" },
      "/api/chat/attachment/att-live": { mime: "application/octet-stream" },
    });
    openModal();

    await waitFor(() => expect(screen.getByTestId("ta-versions-content-opaque")).toBeTruthy());
    expect(readText).not.toHaveBeenCalled();
    expect(cancel).toHaveBeenCalled();
  });

  it("shows a link version's own url and fetches nothing to do it", async () => {
    mockedApi.listTaskArtifactVersions.mockResolvedValue([
      mkVersion({ kind: "link", url: "https://x/pr/1", filename: "", attachmentId: "" }),
    ]);
    mockedApi.listTaskArtifacts.mockResolvedValue(
      mkArtifacts([
        mkArtifact({
          kind: "link",
          url: "https://x/pr/2",
          name: "https://x/pr/2",
          mime: "",
        }),
      ]),
    );
    const { cancel } = stubFetch({});
    openModal();

    await waitFor(() =>
      expect(screen.getByTestId("ta-versions-content-link").textContent).toBe("https://x/pr/1"),
    );
    fireEvent.click(screen.getByTestId("ta-versions-row-live"));
    await waitFor(() =>
      expect(screen.getByTestId("ta-versions-content-link").textContent).toBe("https://x/pr/2"),
    );
    // A link is not a blob: nothing was fetched to answer this.
    expect(globalThis.fetch).not.toHaveBeenCalled();
    expect(cancel).not.toHaveBeenCalled();
  });

  // DELETED: the 前/後 toggle over one viewing area, which this file used to
  // exercise with a pair of application/pdf versions. The toggle no longer
  // exists, and nothing was written in its place.
  //
  // 🔴 WHAT IS NOW UNGUARDED, so nobody reads the file above and assumes
  // otherwise: an `application/pdf` pair reaching the 差異 tab again — because
  // someone widened `diffable` past "both sides read as text", or dropped one
  // side's half of that test — would not redden anything here. Neither would
  // that pair losing its 內容-pane 「無法顯示」 notice, or its bytes being
  // downloaded to be thrown away, for a mime the RESPONSE (not the name) calls
  // non-text: the .bin case above only covers the name-cannot-vouch half of
  // that rule, and no case covers the mime half.

  it("shows each image version under its own src", async () => {
    mockedApi.listTaskArtifactVersions.mockResolvedValue([
      mkVersion({ kind: "image", url: "/api/chat/attachment/att-shot1", filename: "shot1.png" }),
    ]);
    mockedApi.listTaskArtifacts.mockResolvedValue(
      mkArtifacts([
        mkArtifact({
          kind: "image",
          name: "shot2.png",
          mime: "image/png",
          url: "/api/chat/attachment/att-shot2",
        }),
      ]),
    );
    stubFetch({});
    openModal();

    await waitFor(() =>
      expect(
        screen.getByTestId("ta-versions-content-image").getAttribute("src"),
      ).toBe("/api/chat/attachment/att-shot1"),
    );
    fireEvent.click(screen.getByTestId("ta-versions-row-live"));
    await waitFor(() =>
      expect(
        screen.getByTestId("ta-versions-content-image").getAttribute("src"),
      ).toBe("/api/chat/attachment/att-shot2"),
    );
    // An image is displayed by the browser; this panel does not fetch it itself.
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it("selects the newest retained version first and can switch to the current one", async () => {
    mockedApi.listTaskArtifactVersions.mockResolvedValue([
      mkVersion({ id: 2, url: "/api/chat/attachment/att-v2" }),
      mkVersion({ id: 1, url: "/api/chat/attachment/att-v1" }),
    ]);
    mockedApi.listTaskArtifacts.mockResolvedValue(mkArtifacts([mkArtifact({})]));
    stubFetch({
      "/api/chat/attachment/att-v2": { mime: "text/plain", text: "v2" },
      "/api/chat/attachment/att-v1": { mime: "text/plain", text: "v1" },
      "/api/chat/attachment/att-live": { mime: "text/plain", text: "live" },
    });
    openModal();

    await waitFor(() =>
      expect(screen.getByTestId("ta-versions-content-text").textContent).toBe("v2"),
    );
    fireEvent.click(screen.getByTestId("ta-versions-row-1"));
    await waitFor(() =>
      expect(screen.getByTestId("ta-versions-content-text").textContent).toBe("v1"),
    );
    fireEvent.click(screen.getByTestId("ta-versions-row-live"));
    await waitFor(() =>
      expect(screen.getByTestId("ta-versions-content-text").textContent).toBe("live"),
    );
    // DROPPED from this case: the 「這就是目前版本，沒有可比對的對象」 notice, which
    // was what the 差異 tab said when the current version was selected. That
    // notice no longer exists.
    //
    // 🔴 WHAT IS NOW UNGUARDED: the current version being offered a comparison
    // against itself. If the 差異 tab reappears while the live row is selected —
    // or the pane fails to fall back to 內容 when the reader switches to that row
    // from an open diff — no test here says a word.
  });

  // A version's `name` is the stored column and nothing makes an agent send one,
  // so the common deliverable arrives name-less and is named only by its blob.
  // Reading the left column under `name` alone printed a row of 「未命名」 beneath
  // a named current version — and lost the one fact two file versions most often
  // differ by, which is the name the file was re-filed under. The live side needs
  // no such fallback since T-92: the server derives its `name` from that same
  // blob filename before it reaches here.
  it("names a name-less version by its own filename, not as unnamed", async () => {
    mockedApi.listTaskArtifactVersions.mockResolvedValue([
      mkVersion({ id: 2, name: "", filename: "report.md", url: "/api/chat/attachment/att-v2" }),
    ]);
    mockedApi.listTaskArtifacts.mockResolvedValue(
      mkArtifacts([mkArtifact({ name: "report-final.md" })]),
    );
    stubFetch({
      "/api/chat/attachment/att-v2": { mime: "text/plain", text: "old" },
      "/api/chat/attachment/att-live": { mime: "text/plain", text: "new" },
    });
    openModal();

    await waitFor(() => expect(screen.getByTestId("ta-versions-row-2")).toBeTruthy());
    expect(rowName("ta-versions-row-2")).toBe("report.md");
    // The rename is the difference the reader came for, so both sides must show
    // their own name rather than one named side and one anonymous one.
    expect(rowMeta("ta-versions-row-live")).toContain("report-final.md");
  });

  it("says the version history could not be read rather than showing an empty list", async () => {
    mockedApi.listTaskArtifactVersions.mockRejectedValue(new Error("boom"));
    mockedApi.listTaskArtifacts.mockResolvedValue(mkArtifacts([mkArtifact({})]));
    stubFetch({});
    vi.spyOn(console, "warn").mockImplementation(() => {});
    openModal();

    await waitFor(() => expect(screen.getByTestId("ta-versions-load-error")).toBeTruthy());
    expect(screen.queryByTestId("ta-versions-empty")).toBeNull();
  });
});
