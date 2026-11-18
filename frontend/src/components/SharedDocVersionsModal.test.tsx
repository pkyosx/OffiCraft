import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { SharedDocVersionsModal } from "./SharedDocVersionsModal";
import { useSharedDocAction } from "../hooks/useSharedDocs";
import { __resetMock } from "../api/mock";
import { api } from "../api";
import { formatAbsolute } from "../lib/dateFormat";

const BLOBS: Record<string, string> = {
  "/api/chat/attachment/att-5d0c11a2b3c4": "# Review Rule\n\nline two\n",
  "/api/chat/attachment/att-5d0c11a2b3b0": "# Review Rule\n",
};

function Host({ onClose }: { onClose: () => void }) {
  const run = useSharedDocAction("failed");
  return (
    <SharedDocVersionsModal
      docId="sd-3f9a2c71b0e4"
      actorName={(a) => (a === "mira" ? "Mira" : a)}
      run={run}
      onClose={onClose}
    />
  );
}

function renderModal(onClose = () => {}) {
  return render(
    <I18nProvider>
      <Host onClose={onClose} />
    </I18nProvider>,
  );
}

beforeEach(() => {
  __resetMock();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const path = url.split("?")[0]!;
      return new Response(BLOBS[path] ?? "", {
        status: path in BLOBS ? 200 : 404,
        headers: { "content-type": "text/markdown" },
      });
    }),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("SharedDocVersionsModal", () => {
  it("lists the current version and each retained one with when and by whom, the newest retained selected", async () => {
    const { getByTestId } = renderModal();
    const now = Math.floor(Date.now() / 1000);
    await waitFor(() => getByTestId("sd-versions-row-1"));
    expect(getByTestId("sd-versions-row-current").textContent).toBe(
      `目前版本${formatAbsolute(1763366700, now)} · 修改者 Mira`,
    );
    expect(getByTestId("sd-versions-row-1").textContent).toBe(
      `${formatAbsolute(1762900000, now)}修改者 Mira`,
    );
    expect(getByTestId("sd-versions-row-1").getAttribute("aria-pressed")).toBe("true");
    await waitFor(() => expect(getByTestId("sd-versions-text").textContent).toBe("# Review Rule\n"));
  });

  it("差異 compares the selected text version with the current one", async () => {
    const { getByTestId } = renderModal();
    fireEvent.click(await waitFor(() => getByTestId("sd-versions-pane-diff")));
    await waitFor(() => getByTestId("sd-versions-diff"));
    expect(getByTestId("sd-versions-diff").textContent).toContain("line two");
  });

  it("還原這一版 makes the selected version current and the displaced one takes its place", async () => {
    const { getByTestId, queryByTestId } = renderModal();
    fireEvent.click(await waitFor(() => getByTestId("sd-versions-restore")));
    await waitFor(() => expect(queryByTestId("sd-versions-row-1")).toBeNull());
    await waitFor(() => getByTestId("sd-versions-row-101"));
    const doc = await api.getSharedDoc("sd-3f9a2c71b0e4");
    expect(doc).toMatchObject({ attachmentId: "att-5d0c11a2b3b0", versionCount: 2 });
    expect((await api.listSharedDocVersions("sd-3f9a2c71b0e4")).map((v) => v.attachmentId)).toEqual([
      "att-5d0c11a2b3c4",
    ]);
  });

  it("selecting 目前版本 hides 還原這一版", async () => {
    const { getByTestId, queryByTestId } = renderModal();
    await waitFor(() => getByTestId("sd-versions-restore"));
    fireEvent.click(getByTestId("sd-versions-row-current"));
    expect(queryByTestId("sd-versions-restore")).toBeNull();
  });
});
