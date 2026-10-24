// hooks/useDueSoonLeadDays.ts — the workspace's 快到期提前天數 (settings
// `task_due_soon_lead_days`), the lead every task without its own is judged by.
//
// Read through `loadServerSettings` like every other settings consumer. Until
// it lands, and if it fails, the answer is the shipped default: the chip and
// the lanes then judge exactly as the server does on a station nobody tuned.

import { useEffect, useState } from "react";
import { DEFAULT_DUE_SOON_LEAD_DAYS } from "../lib/dueDate";
import { loadServerSettings } from "./sharedServerSettings";

export function useDueSoonLeadDays(): number {
  const [days, setDays] = useState(DEFAULT_DUE_SOON_LEAD_DAYS);

  useEffect(() => {
    let alive = true;
    loadServerSettings()
      .then((s) => {
        if (alive) setDays(s.taskDueSoonLeadDays);
      })
      .catch((e) => {
        console.warn("useDueSoonLeadDays: load failed", e);
      });
    return () => {
      alive = false;
    };
  }, []);

  return days;
}
