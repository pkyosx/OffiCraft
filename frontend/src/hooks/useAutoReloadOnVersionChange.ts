import { useEffect } from "react";
import { api } from "../api";
import { composerDraftPersistence } from "../lib/composerDraftPersistence";

const reloadPage = () => window.location.reload();

export function useAutoReloadOnVersionChange(reload: () => void = reloadPage): void {
  useEffect(() => {
    let active = true;
    let initialSha: string | null = null;
    let initialReadFinished = false;
    let disconnected = false;
    let pendingCheck = false;
    let reloadRequested = false;
    let checking = false;
    let queuedCheck = false;

    async function checkVersion(): Promise<void> {
      if (!active || reloadRequested) return;
      if (!initialReadFinished) {
        pendingCheck = true;
        return;
      }
      if (checking) {
        queuedCheck = true;
        return;
      }
      checking = true;
      try {
        const retryDelays = [250, 750];
        for (let attempt = 0; ; attempt += 1) {
          if (!active || reloadRequested) return;
          try {
            const current = await api.getVersion();
            if (!active) return;
            if (!initialSha) {
              initialSha = current.gitSha || null;
              return;
            }
            if (
              current.gitSha &&
              current.gitSha !== initialSha
            ) {
              reloadRequested = true;
              await composerDraftPersistence.flush();
              if (active) reload();
            }
            return;
          } catch {
            if (attempt >= retryDelays.length) return;
            await new Promise<void>((resolve) =>
              window.setTimeout(resolve, retryDelays[attempt]),
            );
          }
        }
      } finally {
        checking = false;
        if (active && queuedCheck && !reloadRequested) {
          queuedCheck = false;
          void checkVersion();
        }
      }
    }

    const unsubscribe = api.subscribeConnection((state) => {
      if (state === "live") {
        if (disconnected) {
          disconnected = false;
          void checkVersion();
        }
      } else {
        disconnected = true;
      }
    });

    void api.getVersion().then(
      (version) => {
        if (!active) return;
        initialSha = version.gitSha || null;
        initialReadFinished = true;
        if (pendingCheck) {
          pendingCheck = false;
          void checkVersion();
        }
      },
      () => {
        if (!active) return;
        initialReadFinished = true;
        if (pendingCheck) {
          pendingCheck = false;
          void checkVersion();
        }
      },
    );

    return () => {
      active = false;
      unsubscribe();
    };
  }, [reload]);
}
