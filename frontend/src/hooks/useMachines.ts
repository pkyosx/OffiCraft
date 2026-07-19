// hooks/useMachines.ts — load the machine registry (GET /api/machines) through
// the api client + keep it fresh. Mirrors useMonitoring: reconcile-by-refetch on
// any SSE topic mentioning machines or monitoring. The machine picker + the
// Monitor machines panel read this; `online` is honest (never fabricated).

import { useCallback, useEffect, useState } from "react";
import type { MachineView } from "../types";
import { api } from "../api";

interface UseMachines {
  machines: MachineView[];
  loading: boolean;
  /** True when the mount fetch REJECTED (non-401; 401 bounces to login at the
   * http layer). Lets the UI distinguish a failed load from honest-empty. */
  error: boolean;
  refetch: () => Promise<void>;
}

export function useMachines(): UseMachines {
  const [machines, setMachines] = useState<MachineView[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  const refetch = useCallback(async () => {
    const next = await api.listMachines();
    setMachines(next);
  }, []);

  useEffect(() => {
    let alive = true;

    api
      .listMachines()
      .then((next) => {
        if (alive) {
          setMachines(next);
          setError(false);
        }
      })
      .catch((e) => {
        console.warn("useMachines: initial load failed", e);
        if (alive) setError(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });

    // SSE: refetch on any machine/monitoring/member topic (a warden coming online
    // flips a machine's `online`, and onboard/teardown change the registry).
    const unsubscribe = api.subscribeEvents((topic) => {
      if (
        topic.includes("machine") ||
        topic.includes("monitor") ||
        topic.includes("member")
      ) {
        api
          .listMachines()
          .then((next) => {
            if (alive) {
              setMachines(next);
              setError(false);
            }
          })
          .catch((e) => console.warn("useMachines: SSE refetch failed", e));
      }
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, []);

  return { machines, loading, error, refetch };
}
