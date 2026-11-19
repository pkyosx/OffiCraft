import { describe, it, expect, beforeEach, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";

const h = vi.hoisted(() => ({
  listMembers: vi.fn(),
  subscribeEvents: vi.fn(),
}));

vi.mock("../api", () => ({
  api: {
    listMembers: h.listMembers,
    subscribeEvents: h.subscribeEvents,
  },
}));

vi.mock("../i18n", () => ({
  useI18n: () => ({ t: { user: "使用者" } }),
}));

import { useActorName } from "./useActorName";
import { OwnerNameProvider } from "./useOwnerName";

function wrapper({ children }: { children: ReactNode }) {
  return <OwnerNameProvider value="Eva">{children}</OwnerNameProvider>;
}

beforeEach(() => {
  h.listMembers.mockReset().mockResolvedValue([
    { id: "mira", name: "Mira" },
  ]);
  h.subscribeEvents.mockReset().mockReturnValue(vi.fn());
});

describe("useActorName", () => {
  it("resolves a roster member to their display name", async () => {
    const { result } = renderHook(() => useActorName(), { wrapper });

    await waitFor(() => expect(result.current("mira")).toBe("Mira"));
  });

  it("uses the configured owner display name", () => {
    const { result } = renderHook(() => useActorName(), { wrapper });

    expect(result.current("owner")).toBe("Eva");
  });

  it("preserves an id that is absent from the roster", async () => {
    const { result } = renderHook(() => useActorName(), { wrapper });

    await waitFor(() => expect(result.current("mira")).toBe("Mira"));
    expect(result.current("m-retired")).toBe("m-retired");
  });
});
