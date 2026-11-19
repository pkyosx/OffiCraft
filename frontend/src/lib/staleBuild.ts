// The SPA ships inside the server binary and every build replaces all hashed
// asset names, so a page left open across a server update keeps asking for
// files that no longer exist. This module notices the update and either
// reloads a page nobody is looking at or tells the person looking.

const staleAssetListeners = new Set<() => void>();

/** A lazily loaded file of this build answered 404: the server may be running
 * a newer build. Safe to call from any module; it only asks the watch to check. */
export function reportStaleAsset(): void {
  for (const cb of [...staleAssetListeners]) cb();
}

function onStaleAsset(cb: () => void): () => void {
  staleAssetListeners.add(cb);
  return () => staleAssetListeners.delete(cb);
}

export const STALE_BUILD_RELOADED_KEY = "oc.staleBuildReloadedSha";

/** The server reports "unknown" when it cannot read its own git sha. */
function usableSha(sha: string | null | undefined): string | null {
  return sha && sha !== "unknown" ? sha : null;
}

export interface StaleBuildWatchDeps {
  readServerSha: () => Promise<string>;
  subscribeConnection: (cb: (state: string) => void) => () => void;
  isWatched: () => boolean;
  hasUnsavedInput: () => boolean;
  reload: () => void;
  /** Throws when the browser refuses storage; the watch then never reloads on its own. */
  storage: Pick<Storage, "getItem" | "setItem">;
}

export interface StaleBuildWatch {
  start: () => () => void;
  subscribe: (cb: () => void) => () => void;
  /** The newer server build's sha once one is seen, else null. */
  getNewerSha: () => string | null;
}

export function createStaleBuildWatch(deps: StaleBuildWatchDeps): StaleBuildWatch {
  let pageSha: string | null = null;
  let pageShaRead = false;
  let newerSha: string | null = null;
  const listeners = new Set<() => void>();

  function setNewerSha(sha: string): void {
    if (newerSha === sha) return;
    newerSha = sha;
    for (const cb of [...listeners]) cb();
  }

  function maybeReload(): void {
    if (newerSha === null || deps.isWatched() || deps.hasUnsavedInput()) return;
    try {
      // One automatic reload per tab per build: a reload that comes back still
      // stale must not turn into a reload loop.
      if (deps.storage.getItem(STALE_BUILD_RELOADED_KEY) === newerSha) return;
      deps.storage.setItem(STALE_BUILD_RELOADED_KEY, newerSha);
    } catch {
      return;
    }
    deps.reload();
  }

  function check(): void {
    deps
      .readServerSha()
      .then((raw) => {
        const sha = usableSha(raw);
        if (sha === null) return;
        if (!pageShaRead) {
          pageSha = sha;
          pageShaRead = true;
          return;
        }
        if (pageSha !== null && sha !== pageSha) {
          setNewerSha(sha);
          maybeReload();
        }
      })
      .catch(() => {});
  }

  function start(): () => void {
    check();
    let everLive = false;
    let wasLive = false;
    const offConnection = deps.subscribeConnection((state) => {
      const live = state === "live";
      if (live && everLive && !wasLive) check();
      if (live) everLive = true;
      wasLive = live;
    });
    const offStaleAsset = onStaleAsset(check);
    window.addEventListener("vite:preloadError", check);
    window.addEventListener("blur", maybeReload);
    document.addEventListener("visibilitychange", maybeReload);
    return () => {
      offConnection();
      offStaleAsset();
      window.removeEventListener("vite:preloadError", check);
      window.removeEventListener("blur", maybeReload);
      document.removeEventListener("visibilitychange", maybeReload);
    };
  }

  return {
    start,
    subscribe: (cb) => {
      listeners.add(cb);
      return () => listeners.delete(cb);
    },
    getNewerSha: () => newerSha,
  };
}
