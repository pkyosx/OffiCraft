import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  createStaleBuildWatch,
  reportStaleAsset,
  STALE_BUILD_RELOADED_KEY,
  WRITE_SETTLE_RETRY_MS,
  type StaleBuildWatchDeps,
} from "./staleBuild";

function memoryStorage(): Storage {
  const m = new Map<string, string>();
  return {
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => void m.set(k, v),
  } as Storage;
}

function harness(overrides: Partial<StaleBuildWatchDeps> = {}) {
  const shas: (string | Error)[] = [];
  const connection = new Set<(s: string) => void>();
  const state = { watched: false, unsaved: false, writing: false };
  const reload = vi.fn();
  const deps: StaleBuildWatchDeps = {
    readServerSha: () => {
      const next = shas.shift();
      if (next === undefined) throw new Error("test read more versions than it queued");
      return next instanceof Error ? Promise.reject(next) : Promise.resolve(next);
    },
    subscribeConnection: (cb) => {
      connection.add(cb);
      return () => connection.delete(cb);
    },
    isWatched: () => state.watched,
    hasUnsavedInput: () => state.unsaved,
    hasWritesInFlight: () => state.writing,
    reload,
    storage: memoryStorage(),
    ...overrides,
  };
  const watch = createStaleBuildWatch(deps);
  const changes = vi.fn();
  watch.subscribe(changes);
  const settle = async () => {
    for (let i = 0; i < 5; i++) await Promise.resolve();
  };
  const setConnection = async (s: string) => {
    for (const cb of [...connection]) cb(s);
    await settle();
  };
  return { watch, shas, state, reload, changes, deps, settle, setConnection, connection };
}

async function startedOn(h: ReturnType<typeof harness>, pageSha: string) {
  h.shas.push(pageSha);
  const stop = h.watch.start();
  await h.settle();
  await h.setConnection("connecting");
  await h.setConnection("live");
  return stop;
}

let stops: (() => void)[] = [];
afterEach(() => {
  for (const stop of stops) stop();
  stops = [];
});
beforeEach(() => {
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
});

describe("createStaleBuildWatch", () => {
  it("reports no newer build when the server sha after a reconnect is the page's own", async () => {
    const h = harness();
    stops.push(await startedOn(h, "aaa1111"));

    h.shas.push("aaa1111");
    await h.setConnection("connecting");
    await h.setConnection("live");

    expect(h.watch.getNewerSha()).toBe(null);
    expect(h.changes).not.toHaveBeenCalled();
    expect(h.reload).not.toHaveBeenCalled();
  });

  it("does not read the version again on the first connect", async () => {
    const h = harness();
    stops.push(await startedOn(h, "aaa1111"));
    expect(h.shas).toEqual([]);
  });

  it("reports the newer build and does not reload while someone is looking at the page", async () => {
    const h = harness();
    h.state.watched = true;
    stops.push(await startedOn(h, "aaa1111"));

    h.shas.push("bbb2222");
    await h.setConnection("connecting");
    await h.setConnection("live");

    expect(h.watch.getNewerSha()).toBe("bbb2222");
    expect(h.changes).toHaveBeenCalledTimes(1);
    expect(h.reload).not.toHaveBeenCalled();
  });

  it("reloads a page nobody is looking at as soon as the newer build is seen, and records the sha", async () => {
    const h = harness();
    stops.push(await startedOn(h, "aaa1111"));

    h.shas.push("bbb2222");
    await h.setConnection("connecting");
    await h.setConnection("live");

    expect(h.reload).toHaveBeenCalledTimes(1);
    expect(h.deps.storage.getItem(STALE_BUILD_RELOADED_KEY)).toBe("bbb2222");
  });

  it("does not reload a page holding unsaved input, and reloads once the input is gone and the window loses focus", async () => {
    const h = harness();
    h.state.unsaved = true;
    stops.push(await startedOn(h, "aaa1111"));

    h.shas.push("bbb2222");
    await h.setConnection("connecting");
    await h.setConnection("live");
    window.dispatchEvent(new Event("blur"));
    expect(h.watch.getNewerSha()).toBe("bbb2222");
    expect(h.reload).not.toHaveBeenCalled();

    h.state.unsaved = false;
    window.dispatchEvent(new Event("blur"));
    expect(h.reload).toHaveBeenCalledTimes(1);
  });

  it("waits for a sent write to be answered before reloading an unwatched page", async () => {
    vi.useFakeTimers();
    try {
      const h = harness();
      h.state.writing = true;
      stops.push(await startedOn(h, "aaa1111"));
      h.shas.push("bbb2222");
      await h.setConnection("connecting");
      await h.setConnection("live");
      expect(h.reload).not.toHaveBeenCalled();

      vi.advanceTimersByTime(WRITE_SETTLE_RETRY_MS);
      expect(h.reload).not.toHaveBeenCalled();
      h.state.writing = false;
      vi.advanceTimersByTime(WRITE_SETTLE_RETRY_MS);
      expect(h.reload).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it("reloads when the watched tab is sent to the background", async () => {
    const h = harness();
    h.state.watched = true;
    stops.push(await startedOn(h, "aaa1111"));
    h.shas.push("bbb2222");
    await h.setConnection("connecting");
    await h.setConnection("live");
    expect(h.reload).not.toHaveBeenCalled();

    h.state.watched = false;
    document.dispatchEvent(new Event("visibilitychange"));
    expect(h.reload).toHaveBeenCalledTimes(1);
  });

  it("reloads at most once per newer sha in the same tab", async () => {
    const h = harness();
    h.deps.storage.setItem(STALE_BUILD_RELOADED_KEY, "bbb2222");
    stops.push(await startedOn(h, "aaa1111"));

    h.shas.push("bbb2222");
    await h.setConnection("connecting");
    await h.setConnection("live");
    window.dispatchEvent(new Event("blur"));

    expect(h.watch.getNewerSha()).toBe("bbb2222");
    expect(h.reload).not.toHaveBeenCalled();

    h.shas.push("ccc3333");
    await h.setConnection("connecting");
    await h.setConnection("live");
    expect(h.reload).toHaveBeenCalledTimes(1);
    expect(h.deps.storage.getItem(STALE_BUILD_RELOADED_KEY)).toBe("ccc3333");
  });

  it("never reloads on its own when session storage is refused", async () => {
    const refused = {
      getItem: () => {
        throw new Error("SecurityError");
      },
      setItem: () => {
        throw new Error("SecurityError");
      },
    };
    const h = harness({ storage: refused });
    stops.push(await startedOn(h, "aaa1111"));

    h.shas.push("bbb2222");
    await h.setConnection("connecting");
    await h.setConnection("live");

    expect(h.watch.getNewerSha()).toBe("bbb2222");
    expect(h.reload).not.toHaveBeenCalled();
  });

  it.each([
    ["unknown", "unknown"],
    ["empty", ""],
    ["failed", new Error("offline")],
  ])("does nothing when the server version after a reconnect is %s", async (_, answer) => {
    const h = harness();
    stops.push(await startedOn(h, "aaa1111"));

    h.shas.push(answer);
    await h.setConnection("connecting");
    await h.setConnection("live");

    expect(h.watch.getNewerSha()).toBe(null);
    expect(h.reload).not.toHaveBeenCalled();
  });

  it("takes the first readable version as the page's own when the version at load was unknown", async () => {
    const h = harness();
    stops.push(await startedOn(h, "unknown"));

    h.shas.push("aaa1111");
    await h.setConnection("connecting");
    await h.setConnection("live");
    expect(h.watch.getNewerSha()).toBe(null);

    h.shas.push("bbb2222");
    await h.setConnection("connecting");
    await h.setConnection("live");
    expect(h.watch.getNewerSha()).toBe("bbb2222");
  });

  it("checks the version when a lazily loaded file of this build is missing", async () => {
    const h = harness();
    stops.push(await startedOn(h, "aaa1111"));

    h.shas.push("bbb2222");
    reportStaleAsset();
    await h.settle();
    expect(h.watch.getNewerSha()).toBe("bbb2222");
  });

  it("checks the version when a lazy import fails", async () => {
    const h = harness();
    stops.push(await startedOn(h, "aaa1111"));

    h.shas.push("bbb2222");
    window.dispatchEvent(new Event("vite:preloadError"));
    await h.settle();
    expect(h.watch.getNewerSha()).toBe("bbb2222");
  });

  it("stops listening once stopped", async () => {
    const h = harness();
    const stop = await startedOn(h, "aaa1111");
    stop();

    reportStaleAsset();
    window.dispatchEvent(new Event("vite:preloadError"));
    await h.settle();
    expect(h.connection.size).toBe(0);
    expect(h.shas).toEqual([]);
  });
});
