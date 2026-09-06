// hooks/useServerSettings.ts — the owner-tunable server PARAMETERS behind
// /api/settings.
//
// 🔴 DELIBERATELY NOT A FIELD LIST. This line used to read "(登入與 agent token
// 有效期 / 自動換手門檻)" — three of the thirty, written when there were three,
// and stale ever since without anything going red. `ServerSettingsView` is the
// list, it is derived from the frozen wire, and it cannot go stale.
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
import {
  adoptServerSettings,
  loadServerSettings,
} from "./sharedServerSettings";

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
    loadServerSettings()
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
      const echo = await api.patchServerSettings(patch);
      adoptServerSettings(echo); // shared snapshot invalidation point (T-8115)
      setSettings(echo);
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
