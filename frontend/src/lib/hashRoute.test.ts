// URL-hash routing: hash → route restore (refresh / deep link) and
// route → hash update, plus the navigateHash home-strip behaviour.

import { describe, it, expect } from "vitest";
import { parseHash, formatHash, navigateHash } from "./hashRoute";

describe("parseHash", () => {
  it("restores each page from its anchor", () => {
    expect(parseHash("")).toEqual({ page: "office" });
    expect(parseHash("#")).toEqual({ page: "office" });
    expect(parseHash("#office")).toEqual({ page: "office" });
    expect(parseHash("#replies")).toEqual({ page: "replies" });
    expect(parseHash("#monitor")).toEqual({ page: "monitor" });
    expect(parseHash("#settings")).toEqual({ page: "settings" });
  });

  it("restores office chat / detail selections", () => {
    expect(parseHash("#office/chat/mira")).toEqual({
      page: "office",
      chatId: "mira",
    });
    expect(parseHash("#office/member/kyle")).toEqual({
      page: "office",
      detailId: "kyle",
    });
    expect(parseHash("#office/chat/mira/member/kyle")).toEqual({
      page: "office",
      chatId: "mira",
      detailId: "kyle",
    });
    expect(parseHash("#office/chat/mira/msg/c-1")).toEqual({
      page: "office",
      chatId: "mira",
      msgId: "c-1",
    });
    expect(parseHash("#office/chat/mira/msg/c-1/member/kyle")).toEqual({
      page: "office",
      chatId: "mira",
      msgId: "c-1",
      detailId: "kyle",
    });
    expect(parseHash("#office/worker/ow-7")).toEqual({
      page: "office",
      workerId: "ow-7",
    });
    expect(parseHash("#office/chat/ow-7/worker/ow-7")).toEqual({
      page: "office",
      chatId: "ow-7",
      workerId: "ow-7",
    });
  });

  it("restores the settings task-manual deep-link (T-e987 任務類型 label)", () => {
    expect(parseHash("#settings/manuals/review-pr")).toEqual({
      page: "settings",
      manualKey: "review-pr",
    });
    // A bare/short settings anchor stays the plain overlay.
    expect(parseHash("#settings/manuals")).toEqual({ page: "settings" });
  });

  it("restores the settings roles deep-link (T-f074 正職 ➕👤)", () => {
    expect(parseHash("#settings/roles")).toEqual({
      page: "settings",
      settingsRoles: true,
    });
  });

  it("restores the settings roles CREATE deep-link (T-25b7 roster ➕👤)", () => {
    expect(parseHash("#settings/roles/new")).toEqual({
      page: "settings",
      settingsRoles: true,
      settingsRolesNew: true,
    });
  });

  it("restores the settings role-definition deep-link (roster role-line gear)", () => {
    expect(parseHash("#settings/roles/assistant")).toEqual({
      page: "settings",
      settingsRoles: true,
      roleKey: "assistant",
    });
  });

  it("restores the chat compose seed segment (T-e987 負責人/建立者 跳轉)", () => {
    expect(parseHash("#office/chat/mira/compose/T-7d40")).toEqual({
      page: "office",
      chatId: "mira",
      composeTaskNo: "T-7d40",
    });
    // compose and msg are mutually exclusive segments after the chat id.
    expect(parseHash("#office/chat/ow-7/compose/T-1042")).toEqual({
      page: "office",
      chatId: "ow-7",
      composeTaskNo: "T-1042",
    });
  });

  it("restores the monitor member detail", () => {
    expect(parseHash("#monitor/member/mira")).toEqual({
      page: "monitor",
      detailId: "mira",
    });
  });

  it("restores the tasks page ± the located task (§3.6 請示 → 任務)", () => {
    expect(parseHash("#tasks")).toEqual({ page: "tasks" });
    expect(parseHash("#tasks/t-7d40")).toEqual({
      page: "tasks",
      taskId: "t-7d40",
    });
  });

  it("restores the executor filter seed (#tasks/executor/<id>, T-dfae)", () => {
    // 聊天 header 的任務圖示 routes here. A SEPARATE segment shape from
    // #tasks/<id> on purpose — that one means "show me THIS task, overriding
    // every filter", the opposite of "narrow the list by executor".
    expect(parseHash("#tasks/executor/mira")).toEqual({
      page: "tasks",
      executorId: "mira",
    });
    // "executor" is a reserved first segment: it must NOT be read as a task
    // whose id is literally "executor".
    expect(parseHash("#tasks/executor")).toEqual({ page: "tasks" });
    // …and the plain anchor still works beside it.
    expect(parseHash("#tasks/t-7d40")).toEqual({
      page: "tasks",
      taskId: "t-7d40",
    });
  });

  it("decodes URL-encoded ids", () => {
    expect(parseHash("#office/chat/a%2Fb")).toEqual({
      page: "office",
      chatId: "a/b",
    });
  });

  it("falls back to office home on unknown or malformed hashes", () => {
    expect(parseHash("#nope")).toEqual({ page: "office" });
    expect(parseHash("#office/chat")).toEqual({ page: "office" });
    expect(parseHash("#monitor/member")).toEqual({ page: "monitor" });
  });
});

describe("formatHash", () => {
  it("writes each route as its anchor (bare office = clean root)", () => {
    expect(formatHash({ page: "office" })).toBe("");
    expect(formatHash({ page: "replies" })).toBe("#replies");
    expect(formatHash({ page: "monitor" })).toBe("#monitor");
    expect(formatHash({ page: "settings" })).toBe("#settings");
    expect(formatHash({ page: "office", chatId: "mira" })).toBe(
      "#office/chat/mira"
    );
    expect(
      formatHash({ page: "office", chatId: "mira", detailId: "kyle" })
    ).toBe("#office/chat/mira/member/kyle");
    expect(formatHash({ page: "office", chatId: "mira", msgId: "c-1" })).toBe(
      "#office/chat/mira/msg/c-1"
    );
    // msg without an open chat is meaningless → dropped.
    expect(formatHash({ page: "office", msgId: "c-1" })).toBe("");
    expect(formatHash({ page: "monitor", detailId: "mira" })).toBe(
      "#monitor/member/mira"
    );
    expect(formatHash({ page: "tasks" })).toBe("#tasks");
    expect(formatHash({ page: "tasks", taskId: "t-7d40" })).toBe(
      "#tasks/t-7d40"
    );
    // T-dfae executor seed — its own segment shape (see parseHash above).
    expect(formatHash({ page: "tasks", executorId: "mira" })).toBe(
      "#tasks/executor/mira"
    );
    // executor and taskId are mutually exclusive (narrow vs. override-all);
    // executor wins if a caller somehow sets both, rather than emitting an
    // anchor that would silently short-circuit the filter it promised.
    expect(
      formatHash({ page: "tasks", executorId: "mira", taskId: "t-7d40" })
    ).toBe("#tasks/executor/mira");
    expect(formatHash({ page: "office", workerId: "ow-7" })).toBe(
      "#office/worker/ow-7"
    );
    expect(
      formatHash({ page: "office", chatId: "ow-7", workerId: "ow-7" })
    ).toBe("#office/chat/ow-7/worker/ow-7");
    // T-e987 settings deep-link + compose seed segments.
    expect(formatHash({ page: "settings", manualKey: "review-pr" })).toBe(
      "#settings/manuals/review-pr"
    );
    // T-f074 settings roles deep-link; manualKey wins if both are set.
    expect(formatHash({ page: "settings", settingsRoles: true })).toBe(
      "#settings/roles"
    );
    expect(
      formatHash({ page: "settings", manualKey: "review-pr", settingsRoles: true })
    ).toBe("#settings/manuals/review-pr");
    // T-25b7 roles CREATE deep-link round-trips to #settings/roles/new.
    expect(
      formatHash({
        page: "settings",
        settingsRoles: true,
        settingsRolesNew: true,
      })
    ).toBe("#settings/roles/new");
    // Roster role-line gear deep-link (create mode wins if both are set —
    // "new" is the reserved segment).
    expect(
      formatHash({ page: "settings", settingsRoles: true, roleKey: "assistant" })
    ).toBe("#settings/roles/assistant");
    expect(
      formatHash({ page: "office", chatId: "mira", composeTaskNo: "T-7d40" })
    ).toBe("#office/chat/mira/compose/T-7d40");
    // compose without an open chat is meaningless → dropped.
    expect(formatHash({ page: "office", composeTaskNo: "T-7d40" })).toBe("");
    // msg wins if both are somehow set (msg is the more specific locate).
    expect(
      formatHash({
        page: "office",
        chatId: "mira",
        msgId: "c-1",
        composeTaskNo: "T-7d40",
      })
    ).toBe("#office/chat/mira/msg/c-1");
  });

  it("round-trips through parseHash", () => {
    const routes = [
      { page: "office" as const },
      { page: "office" as const, chatId: "mira" },
      { page: "office" as const, chatId: "mira", detailId: "kyle" },
      { page: "office" as const, chatId: "mira", msgId: "c-1" },
      { page: "office" as const, detailId: "kyle" },
      { page: "office" as const, workerId: "ow-7" },
      { page: "office" as const, chatId: "ow-7", workerId: "ow-7" },
      { page: "replies" as const },
      { page: "tasks" as const },
      { page: "tasks" as const, taskId: "t-7d40" },
      { page: "tasks" as const, executorId: "mira" },
      { page: "monitor" as const },
      { page: "monitor" as const, detailId: "mira" },
      { page: "settings" as const },
      { page: "settings" as const, manualKey: "review-pr" },
      {
        page: "settings" as const,
        settingsRoles: true,
        roleKey: "role-a1b2",
      },
      { page: "office" as const, chatId: "mira", composeTaskNo: "T-7d40" },
    ];
    for (const r of routes) {
      expect(parseHash(formatHash(r))).toEqual(r);
    }
  });
});

describe("navigateHash", () => {
  it("writes the anchor into location.hash", () => {
    navigateHash({ page: "monitor" });
    expect(window.location.hash).toBe("#monitor");
    navigateHash({ page: "office", chatId: "mira" });
    expect(window.location.hash).toBe("#office/chat/mira");
  });

  it("navigating home strips the hash entirely and still notifies", () => {
    navigateHash({ page: "monitor" });
    let notified = false;
    const onChange = () => {
      notified = true;
    };
    window.addEventListener("hashchange", onChange);
    navigateHash({ page: "office" });
    window.removeEventListener("hashchange", onChange);
    expect(window.location.hash).toBe("");
    expect(notified).toBe(true);
  });
});
