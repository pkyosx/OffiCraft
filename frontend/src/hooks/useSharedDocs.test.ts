import { describe, it, expect, afterEach, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { api } from "../api";
import type { SharedDocView } from "../types";
import { useSharedDocs } from "./useSharedDocs";

function doc(id: string, archivedTs: number | null): SharedDocView {
  return {
    id,
    kind: "link",
    attachmentId: "att-1",
    name: id,
    description: "",
    filename: "",
    mime: "text/uri-list",
    url: "https://example.com",
    createdTs: 1,
    createdBy: "mira",
    updatedTs: 1,
    updatedBy: "mira",
    archivedTs,
    archivedBy: archivedTs === null ? null : "mira",
    versionCount: 1,
  };
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useSharedDocs", () => {
  it("a list asked for earlier that answers last does not replace the newer answer", async () => {
    const pending: Array<(list: SharedDocView[]) => void> = [];
    vi.spyOn(api, "listSharedDocs").mockImplementation(
      () => new Promise((resolve) => pending.push(resolve)),
    );
    const { result, rerender } = renderHook(
      ({ archived }) => useSharedDocs(archived, "failed"),
      { initialProps: { archived: false } },
    );
    rerender({ archived: true });
    await waitFor(() => expect(pending).toHaveLength(2));

    const withArchived = [doc("sd-active", null), doc("sd-archived", 200)];
    await act(async () => pending[1]!(withArchived));
    await act(async () => pending[0]!([doc("sd-active", null)]));

    expect(result.current.docs).toEqual(withArchived);
    expect(result.current.loading).toBe(false);
    expect(vi.mocked(api.listSharedDocs).mock.calls).toEqual([[false], [true]]);
  });
});
