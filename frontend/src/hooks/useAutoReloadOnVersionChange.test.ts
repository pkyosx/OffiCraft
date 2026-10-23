import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { composerDraftPersistence } from "../lib/composerDraftPersistence";

const connection = vi.hoisted(() => {
  let state = "live";
  let listener: ((next: string) => void) | null = null;
  const getVersion = vi.fn();
  const subscribeConnection = vi.fn((next: (value: string) => void) => {
    listener = next;
    next(state);
    return () => {
      if (listener === next) listener = null;
    };
  });

  return {
    getVersion,
    subscribeConnection,
    publish(next: string) {
      state = next;
      listener?.(next);
    },
    reset() {
      state = "live";
      listener = null;
      getVersion.mockReset();
      subscribeConnection.mockClear();
    },
  };
});

vi.mock("../api", () => ({
  api: {
    getVersion: connection.getVersion,
    subscribeConnection: connection.subscribeConnection,
  },
}));

import { useAutoReloadOnVersionChange } from "./useAutoReloadOnVersionChange";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}

describe("useAutoReloadOnVersionChange", () => {
  beforeEach(() => connection.reset());
  afterEach(() => vi.restoreAllMocks());

  it("reloads when the version changes after an SSE reconnect", async () => {
    connection.getVersion
      .mockResolvedValueOnce({ gitSha: "old-sha" })
      .mockResolvedValueOnce({ gitSha: "new-sha" });
    const reload = vi.fn();

    renderHook(() => useAutoReloadOnVersionChange(reload));
    await waitFor(() => expect(connection.getVersion).toHaveBeenCalledTimes(1));

    act(() => {
      connection.publish("connecting");
      connection.publish("live");
    });

    await waitFor(() => expect(reload).toHaveBeenCalledTimes(1));
    expect(connection.getVersion).toHaveBeenCalledTimes(2);
  });

  it("does not reload when an SSE reconnect returns the same version", async () => {
    connection.getVersion.mockResolvedValue({ gitSha: "same-sha" });
    const reload = vi.fn();

    renderHook(() => useAutoReloadOnVersionChange(reload));
    await waitFor(() => expect(connection.getVersion).toHaveBeenCalledTimes(1));

    act(() => {
      connection.publish("connecting");
      connection.publish("live");
    });
    await waitFor(() => expect(connection.getVersion).toHaveBeenCalledTimes(2));

    act(() => {
      connection.publish("connecting");
      connection.publish("live");
    });
    await waitFor(() => expect(connection.getVersion).toHaveBeenCalledTimes(3));
    expect(reload).not.toHaveBeenCalled();
  });

  it("establishes a version baseline after the initial read fails", async () => {
    connection.getVersion
      .mockRejectedValueOnce(new Error("temporary failure"))
      .mockResolvedValueOnce({ gitSha: "recovered-sha" })
      .mockResolvedValueOnce({ gitSha: "new-sha" });
    const reload = vi.fn();

    renderHook(() => useAutoReloadOnVersionChange(reload));
    await waitFor(() => expect(connection.getVersion).toHaveBeenCalledTimes(1));

    act(() => {
      connection.publish("connecting");
      connection.publish("live");
    });
    await waitFor(() => expect(connection.getVersion).toHaveBeenCalledTimes(2));
    expect(reload).not.toHaveBeenCalled();

    act(() => {
      connection.publish("connecting");
      connection.publish("live");
    });
    await waitFor(() => expect(reload).toHaveBeenCalledTimes(1));
    expect(connection.getVersion).toHaveBeenCalledTimes(3);
  });

  it("rechecks the version when the initial read fails during reconnect", async () => {
    const initialRead = deferred<{ gitSha: string }>();
    connection.getVersion
      .mockReturnValueOnce(initialRead.promise)
      .mockResolvedValueOnce({ gitSha: "recovered-sha" });
    const reload = vi.fn();

    renderHook(() => useAutoReloadOnVersionChange(reload));
    act(() => {
      connection.publish("connecting");
      connection.publish("live");
    });

    await act(async () => {
      initialRead.reject(new Error("temporary failure"));
      await initialRead.promise.catch(() => {});
    });
    await waitFor(() => expect(connection.getVersion).toHaveBeenCalledTimes(2));
    expect(reload).not.toHaveBeenCalled();
  });

  it("retries a failed version read during reconnect", async () => {
    connection.getVersion
      .mockResolvedValueOnce({ gitSha: "old-sha" })
      .mockRejectedValueOnce(new Error("temporary failure"))
      .mockResolvedValueOnce({ gitSha: "new-sha" });
    const reload = vi.fn();

    renderHook(() => useAutoReloadOnVersionChange(reload));
    await waitFor(() => expect(connection.getVersion).toHaveBeenCalledTimes(1));

    act(() => {
      connection.publish("connecting");
      connection.publish("live");
    });

    await waitFor(() => expect(reload).toHaveBeenCalledTimes(1));
    expect(connection.getVersion).toHaveBeenCalledTimes(3);
  });

  it("requests at most one reload after the loaded version changes", async () => {
    connection.getVersion
      .mockResolvedValueOnce({ gitSha: "old-sha" })
      .mockResolvedValueOnce({ gitSha: "new-sha" });
    const reload = vi.fn();

    renderHook(() => useAutoReloadOnVersionChange(reload));
    await waitFor(() => expect(connection.getVersion).toHaveBeenCalledTimes(1));

    act(() => {
      connection.publish("connecting");
      connection.publish("live");
    });
    await waitFor(() => expect(reload).toHaveBeenCalledTimes(1));

    act(() => {
      connection.publish("connecting");
      connection.publish("live");
    });
    expect(connection.getVersion).toHaveBeenCalledTimes(2);
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it("waits for the initial version read and pending draft writes before reloading", async () => {
    const initialRead = deferred<{ gitSha: string }>();
    const draftWrites = deferred<void>();
    connection.getVersion
      .mockReturnValueOnce(initialRead.promise)
      .mockResolvedValueOnce({ gitSha: "new-sha" });
    const flush = vi.spyOn(composerDraftPersistence, "flush").mockReturnValue(draftWrites.promise);
    const reload = vi.fn();

    renderHook(() => useAutoReloadOnVersionChange(reload));
    act(() => {
      connection.publish("connecting");
      connection.publish("live");
    });
    expect(connection.getVersion).toHaveBeenCalledTimes(1);

    await act(async () => {
      initialRead.resolve({ gitSha: "old-sha" });
      await initialRead.promise;
    });
    await waitFor(() => expect(flush).toHaveBeenCalledTimes(1));
    expect(reload).not.toHaveBeenCalled();

    await act(async () => {
      draftWrites.resolve();
      await draftWrites.promise;
    });
    await waitFor(() => expect(reload).toHaveBeenCalledTimes(1));
  });
});
