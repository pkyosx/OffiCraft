import { describe, it, expect } from "vitest";
import { toMonitoring } from "./mappers";
import type { WireMonMachine } from "./wire";

const machine = (id: string, extra: Partial<WireMonMachine> = {}): WireMonMachine => ({
  machine: id,
  display_name: id,
  agents: 0,
  ...extra,
});

describe("toMonitoring · machine disk usage", () => {
  it("under a measured machine every field, category and member is carried across in order; a null or omitted disk_usage reads as null", () => {
    const view = toMonitoring({
      machines: [
        machine("m-server-self", {
          disk_usage: {
            measured_at: 1790000000.5,
            total_bytes: 44798476288,
            database_measured_at: 1790000100,
            categories: [
              { key: "database", parent_key: null, bytes: 2475556864, in_root: true },
              { key: "zeta", parent_key: null, bytes: null, in_root: false },
              { key: "zeta-a", parent_key: "zeta", bytes: null, in_root: false },
              { key: "other", parent_key: null, bytes: 3643949056, in_root: true },
            ],
            members: [
              { member_id: "mira", name: "Mira", roster_status: "active", workspace_bytes: 6348800000 },
              { member_id: "ow-gone", roster_status: "unknown" },
            ],
            disk_free_bytes: 412316860416,
            disk_total_bytes: 1000240963584,
          },
        }),
        machine("mbp5", { disk_usage: null }),
        machine("old-server"),
      ],
    });
    expect(view.machines.map((m) => m.diskUsage)).toEqual([
      {
        measuredAt: 1790000000.5,
        totalBytes: 44798476288,
        databaseMeasuredAt: 1790000100,
        categories: [
          { key: "database", parentKey: null, bytes: 2475556864, inRoot: true },
          { key: "zeta", parentKey: null, bytes: null, inRoot: false },
          { key: "zeta-a", parentKey: "zeta", bytes: null, inRoot: false },
          { key: "other", parentKey: null, bytes: 3643949056, inRoot: true },
        ],
        members: [
          { memberId: "mira", name: "Mira", rosterStatus: "active", workspaceBytes: 6348800000 },
          { memberId: "ow-gone", name: null, rosterStatus: "unknown", workspaceBytes: null },
        ],
        diskFreeBytes: 412316860416,
        diskTotalBytes: 1000240963584,
      },
      null,
      null,
    ]);
  });

  it("under a measurement object with only its required lists every other field reads as null", () => {
    const view = toMonitoring({ machines: [machine("m1", { disk_usage: { categories: [], members: [] } })] });
    expect(view.machines[0].diskUsage).toEqual({
      measuredAt: null,
      totalBytes: null,
      databaseMeasuredAt: null,
      categories: [],
      members: [],
      diskFreeBytes: null,
      diskTotalBytes: null,
    });
  });
});
