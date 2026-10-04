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
  it("under a measured machine every field and member is carried across; a null or omitted disk_usage reads as null", () => {
    const view = toMonitoring({
      machines: [
        machine("m-server-self", {
          disk_usage: {
            measured_at: 1790000000.5,
            total_bytes: 44798476288,
            database_bytes: 2475556864,
            backups_bytes: 13669425152,
            database_measured_at: 1790000100,
            workspace_bytes: 16698077184,
            conversation_bytes: 8311468032,
            claude_conversation_bytes: 3751768064,
            codex_conversation_bytes: 4559699968,
            other_bytes: 3643949056,
            members: [
              {
                member_id: "mira",
                name: "Mira",
                roster_status: "active",
                workspace_bytes: 6348800000,
                conversation_bytes: 2150400000,
                total_bytes: 8499200000,
              },
              { member_id: "ow-gone", roster_status: "unknown", total_bytes: 1024 },
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
        databaseBytes: 2475556864,
        backupsBytes: 13669425152,
        databaseMeasuredAt: 1790000100,
        workspaceBytes: 16698077184,
        conversationBytes: 8311468032,
        claudeConversationBytes: 3751768064,
        codexConversationBytes: 4559699968,
        otherBytes: 3643949056,
        members: [
          {
            memberId: "mira",
            name: "Mira",
            rosterStatus: "active",
            workspaceBytes: 6348800000,
            conversationBytes: 2150400000,
            totalBytes: 8499200000,
          },
          {
            memberId: "ow-gone",
            name: null,
            rosterStatus: "unknown",
            workspaceBytes: null,
            conversationBytes: null,
            totalBytes: 1024,
          },
        ],
        diskFreeBytes: 412316860416,
        diskTotalBytes: 1000240963584,
      },
      null,
      null,
    ]);
  });

  it("under a measurement object with only its required members list every other field reads as null", () => {
    const view = toMonitoring({ machines: [machine("m1", { disk_usage: { members: [] } })] });
    expect(view.machines[0].diskUsage).toEqual({
      measuredAt: null,
      totalBytes: null,
      databaseBytes: null,
      backupsBytes: null,
      databaseMeasuredAt: null,
      workspaceBytes: null,
      conversationBytes: null,
      claudeConversationBytes: null,
      codexConversationBytes: null,
      otherBytes: null,
      members: [],
      diskFreeBytes: null,
      diskTotalBytes: null,
    });
  });
});
