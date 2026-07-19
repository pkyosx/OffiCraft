// hooks/useServerSettings.ts — the owner-tunable server PARAMETERS
// (登入有效期 token_ttl / 自動換手門檻 handover_pct) behind /api/settings.
//
// Home is the 設定 page's 參數調整 entry (owner 2026-07-12: parameter knobs
// belong together in 設定, not scattered in the profile menu). The dropdown used
// to fetch these itself; the page owns them now, so the fetch lives here — same
// seam, one hook, mirroring useVersion/useRoles/useGlobalContext.
//
// Mount-fetch + PATCH echo: the server returns the EFFECTIVE values from a PATCH,
// so `save` adopts the response rather than optimistically guessing.

import { useEffect, useState } from "react";
import { api, type ServerSettingsView, type ServerSettingsPatch } from "../api";

interface UseServerSettings {
  settings: ServerSettingsView | null;
  /** True when the mount fetch REJECTED (non-401; 401 bounces to login at the
   * http layer) — a failed load must never read as "no parameters". */
  error: boolean;
  /** True when the last save REJECTED (or was locally out of range). */
  saveError: boolean;
  /** PATCH the given knobs; adopts the server's echoed effective values. */
  save: (patch: ServerSettingsPatch) => Promise<void>;
  /** Clear a stale save error (e.g. the owner re-edits the field). */
  clearSaveError: () => void;
}

export function useServerSettings(): UseServerSettings {
  const [settings, setSettings] = useState<ServerSettingsView | null>(null);
  const [error, setError] = useState(false);
  const [saveError, setSaveError] = useState(false);

  useEffect(() => {
    let alive = true;
    api
      .getServerSettings()
      .then((next) => {
        if (alive) {
          setSettings(next);
          setError(false);
        }
      })
      .catch((e) => {
        console.warn("useServerSettings: load failed", e);
        if (alive) setError(true);
      });
    return () => {
      alive = false;
    };
  }, []);

  async function save(patch: ServerSettingsPatch): Promise<void> {
    setSaveError(false);
    try {
      setSettings(await api.patchServerSettings(patch));
    } catch (e) {
      console.warn("useServerSettings: save failed", e);
      // Keep the last server-confirmed values; the caller snaps its draft back.
      setSaveError(true);
    }
  }

  return {
    settings,
    error,
    saveError,
    save,
    clearSaveError: () => setSaveError(false),
  };
}
