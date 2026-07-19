// hooks/useVersion.ts — load the build identity for the software-update card.
//
// Read-only, mount-fetch + explicit refresh. The running build's identity does
// not change under the app's feet, but `update_available` / `latest_version`
// DO once an updater server is configured (the server refreshes its cached
// check in the background) — the card calls `refresh()` after saving updater
// settings so the answer lands without a page reload. Mirrors the other
// hooks' seam so the real backend swap is a no-op here.

import { useCallback, useEffect, useState } from "react";
import type { VersionView } from "../types";
import { api } from "../api";

interface UseVersion {
  version: VersionView | null;
  loading: boolean;
  /** True when the fetch REJECTED (non-401; 401 bounces to login at the
   * http layer). Guards against a failed load reading as "no build identity". */
  error: boolean;
  /** Re-fetch /api/version (e.g. after the updater settings changed). */
  refresh: () => void;
}

export function useVersion(): UseVersion {
  const [version, setVersion] = useState<VersionView | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  // Bumping the epoch re-runs the fetch effect; the effect's `alive` guard
  // keeps a stale response from landing after unmount / a newer run.
  const [epoch, setEpoch] = useState(0);

  useEffect(() => {
    let alive = true;
    api
      .getVersion()
      .then((next) => {
        if (alive) {
          setVersion(next);
          setError(false);
        }
      })
      .catch((e) => {
        console.warn("useVersion: load failed", e);
        if (alive) setError(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [epoch]);

  const refresh = useCallback(() => setEpoch((n) => n + 1), []);

  return { version, loading, error, refresh };
}
