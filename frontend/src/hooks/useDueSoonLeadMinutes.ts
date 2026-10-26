// hooks/useDueSoonLeadMinutes.ts — the workspace's 快到期提醒 in minutes (settings
// `task_due_soon_lead_minutes`), the lead every task without its own is judged by.
//
// Read through `loadServerSettings` like every other settings consumer. Until
// it lands, and if it fails, the answer is the shipped default: the chip and
// the lanes then judge exactly as the server does on a station nobody tuned.
//
// A change made in another tab reaches this one as a `task` delta that names no
// task (the server's lead-change notice has a null payload; a resync names
// nothing either), so such a delta re-reads the setting rather than waiting for
// a reload. A delta naming a task is a task write and cannot move the lead.

import { useEffect, useState } from "react";
import { api } from "../api";
import { DEFAULT_DUE_SOON_LEAD_MINUTES } from "../lib/dueDate";
import { loadServerSettings, refreshServerSettings } from "./sharedServerSettings";

export function useDueSoonLeadMinutes(): number {
  const [minutes, setMinutes] = useState(DEFAULT_DUE_SOON_LEAD_MINUTES);

  useEffect(() => {
    let alive = true;
    const land = (read: Promise<{ taskDueSoonLeadMinutes: number }>) =>
      read
        .then((s) => {
          if (alive) setMinutes(s.taskDueSoonLeadMinutes);
        })
        .catch((e) => {
          console.warn("useDueSoonLeadMinutes: load failed", e);
        });
    land(loadServerSettings());
    const unsubscribe = api.subscribeEvents((topic, delta) => {
      if (topic === "task" && (delta === undefined || delta.ids.length === 0)) {
        land(refreshServerSettings());
      }
    });
    return () => {
      alive = false;
      unsubscribe();
    };
  }, []);

  return minutes;
}
