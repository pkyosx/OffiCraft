import { describe, it, expect, afterEach, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { api } from "../api";
import type { SharedDocView } from "../types";
import { useSharedDocs } from "./useSharedDocs";

function doc(id: string): SharedDocView {
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
    const { result } = renderHook(() => useSharedDocs("failed"));
    await waitFor(() => expect(pending).toHaveLength(1));
    act(() => void result.current.reload());
    await waitFor(() => expect(pending).toHaveLength(2));

    const newer = [doc("sd-a"), doc("sd-b")];
    await act(async () => pending[1]!(newer));
    await act(async () => pending[0]!([doc("sd-a")]));

    expect(result.current.docs).toEqual(newer);
    expect(result.current.loading).toBe(false);
  });
});
