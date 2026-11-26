// URL-hash routing: hash → route restore (refresh / deep link) and
// route → hash update, plus the navigateHash home-strip behaviour.

import { describe, it, expect, vi } from "vitest";
import { act, render, waitFor } from "@testing-library/react";
import { useState } from "react";
import { afterEach } from "vitest";
import {
  __resetHashRouteForTests,
  parseHash,
  formatHash,
  navigateHash,
  setNavigationGuard,
  useHashRoute,
} from "./hashRoute";

afterEach(() => {
  __resetHashRouteForTests();
});

describe("parseHash", () => {
  it("restores each page from its anchor", () => {
    expect(parseHash("")).toEqual({ page: "office" });
    expect(parseHash("#")).toEqual({ page: "office" });
    expect(parseHash("#office")).toEqual({ page: "office" });
    expect(parseHash("#replies")).toEqual({ page: "replies" });
    expect(parseHash("#replies/opener/ow-7")).toEqual({ page: "replies", openerId: "ow-7" });
    expect(parseHash("#replies/opener")).toEqual({ page: "replies" });
    expect(parseHash("#monitor")).toEqual({ page: "monitor" });
    expect(parseHash("#secrets")).toEqual({ page: "secrets" });
    expect(parseHash("#shared-docs")).toEqual({ page: "shared_docs" });
    expect(parseHash("#shared-docs/sd-0c41e97a2b58")).toEqual({
      page: "shared_docs",
      sharedDocId: "sd-0c41e97a2b58",
    });
    expect(parseHash("#settings")).toEqual({ page: "settings" });
    expect(parseHash("#settings/guide")).toEqual({
      page: "settings",
      settingsGuide: true,
    });
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
    expect(parseHash("#office/member/kyle/from/tasks")).toEqual({
      page: "office",
      detailId: "kyle",
      backTo: "tasks",
    });
    expect(parseHash("#office/worker/ow-7/from/tasks")).toEqual({
      page: "office",
      workerId: "ow-7",
      backTo: "tasks",
    });
    expect(parseHash("#office/member/kyle/from/replies/rc-1")).toEqual({
      page: "office",
      detailId: "kyle",
      backTo: "replies",
      backToId: "rc-1",
    });
    expect(parseHash("#office/worker/ow-7/from/tasks/task-1")).toEqual({
      page: "office",
      workerId: "ow-7",
      backTo: "tasks",
      backToId: "task-1",
    });
    expect(parseHash("#office/member/kyle/from/foo")).toEqual({
      page: "office",
      detailId: "kyle",
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

  it("restores the open guide doc", () => {
    expect(parseHash("#settings/guide/interface")).toEqual({
      page: "settings",
      settingsGuide: true,
      guideSlug: "interface",
    });
  });

  it("legacy #guide links → the same settings guide routes", () => {
    expect(parseHash("#guide")).toEqual({ page: "settings", settingsGuide: true });
    expect(parseHash("#guide/interface")).toEqual({
      page: "settings",
      settingsGuide: true,
      guideSlug: "interface",
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
    expect(parseHash("#guide/")).toEqual({ page: "settings", settingsGuide: true });
    expect(parseHash("#settings/guide/")).toEqual({
      page: "settings",
      settingsGuide: true,
    });
  });

  it("a malformed percent-escape does not take the page down", () => {
    // 🔴 T-93: parseHash runs in useHashRoute's useMemo, which App calls at the
    // root, and there is no ErrorBoundary anywhere in src/ — so a throw here is
    // a WHITE PAGE, not a failed filter. decodeURIComponent throws on a lone
    // `%` and on `%zz`. This ticket exists so an agent can paste a link the
    // owner clicks; a URL that has been through a chat client, a terminal wrap
    // and a manual re-paste is not guaranteed to arrive intact.
    //
    // Without the try/catch in decodeSegment EVERY line below throws URIError.
    expect(() => parseHash("#replies/card/rc-50%")).not.toThrow();
    expect(() => parseHash("#tasks/T-9%zz")).not.toThrow();
    expect(() => parseHash("#office/chat/%")).not.toThrow();

    // …and an undecodable segment is carried through RAW, so the page lands on
    // the right surface and simply matches no card — the honest empty state.
    expect(parseHash("#replies/card/rc-50%")).toEqual({
      page: "replies",
      replyCardId: "rc-50%",
    });

    // A well-formed escape still decodes — a blanket catch-and-return-raw would
    // pass the three lines above while silently breaking every real link.
    expect(parseHash("#replies/card/rc%2Dabc")).toEqual({
      page: "replies",
      replyCardId: "rc-abc",
    });
  });
});

describe("formatHash", () => {
  it("writes each route as its anchor (bare office = clean root)", () => {
    expect(formatHash({ page: "office" })).toBe("");
    expect(formatHash({ page: "replies" })).toBe("#replies");
    expect(formatHash({ page: "replies", openerId: "mira" })).toBe("#replies/opener/mira");
    expect(formatHash({ page: "monitor" })).toBe("#monitor");
    expect(formatHash({ page: "secrets" })).toBe("#secrets");
    expect(formatHash({ page: "shared_docs" })).toBe("#shared-docs");
    expect(
      formatHash({ page: "shared_docs", sharedDocId: "sd-0c41e97a2b58" }),
    ).toBe("#shared-docs/sd-0c41e97a2b58");
    expect(formatHash({ page: "settings" })).toBe("#settings");
    expect(formatHash({ page: "settings", settingsGuide: true })).toBe(
      "#settings/guide",
    );
    expect(
      formatHash({ page: "settings", settingsGuide: true, guideSlug: "interface" }),
    ).toBe("#settings/guide/interface");
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
    expect(formatHash({ page: "office", detailId: "kyle", backTo: "tasks" })).toBe(
      "#office/member/kyle/from/tasks"
    );
    expect(formatHash({ page: "office", workerId: "ow-7", backTo: "tasks" })).toBe(
      "#office/worker/ow-7/from/tasks"
    );
    expect(
      formatHash({
        page: "office",
        detailId: "kyle",
        backTo: "replies",
        backToId: "rc/1",
      })
    ).toBe("#office/member/kyle/from/replies/rc%2F1");
    expect(
      formatHash({
        page: "office",
        workerId: "ow-7",
        backTo: "tasks",
        backToId: "task-1",
      })
    ).toBe("#office/worker/ow-7/from/tasks/task-1");
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
      { page: "replies" as const, openerId: "ow-7" },
      { page: "replies" as const, replyCardId: "rc-0c41e97a2b58" },
      { page: "tasks" as const },
      { page: "tasks" as const, taskId: "t-7d40" },
      { page: "tasks" as const, executorId: "mira" },
      { page: "monitor" as const },
      { page: "monitor" as const, detailId: "mira" },
      { page: "secrets" as const },
      { page: "shared_docs" as const },
      { page: "shared_docs" as const, sharedDocId: "sd-0c41e97a2b58" },
      { page: "settings" as const, settingsGuide: true },
      { page: "settings" as const, settingsGuide: true, guideSlug: "interface" },
      { page: "settings" as const },
      { page: "settings" as const, manualKey: "review-pr" },
      {
        page: "settings" as const,
        settingsRoles: true,
        roleKey: "role-a1b2",
      },
      { page: "office" as const, chatId: "mira", composeTaskNo: "T-7d40" },
      { page: "office" as const, detailId: "kyle", backTo: "tasks" as const },
      { page: "office" as const, workerId: "ow-7", backTo: "tasks" as const },
      {
        page: "office" as const,
        detailId: "kyle",
        backTo: "replies" as const,
        backToId: "rc/1",
      },
      {
        page: "office" as const,
        workerId: "ow-7",
        backTo: "tasks" as const,
        backToId: "task-1",
      },
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

describe("傳承 routes", () => {
  it("parses the directory, each section kind and an entry highlight", () => {
    expect(parseHash("#lore")).toEqual({ page: "lore" });
    expect(parseHash("#lore/everyone")).toEqual({ page: "lore", loreSection: { kind: "everyone", key: "" } });
    expect(parseHash("#lore/agent/mira")).toEqual({ page: "lore", loreSection: { kind: "agent", key: "mira" } });
    expect(parseHash("#lore/manual/review%20pr/entry/L-12")).toEqual({
      page: "lore",
      loreSection: { kind: "manual", key: "review pr" },
      loreEntryId: "L-12",
    });
    expect(parseHash("#lore/everyone/entry/L-3")).toEqual({
      page: "lore",
      loreSection: { kind: "everyone", key: "" },
      loreEntryId: "L-3",
    });
  });

  it("heals a malformed section to the directory", () => {
    expect(parseHash("#lore/agent")).toEqual({ page: "lore" });
    expect(parseHash("#lore/role/x")).toEqual({ page: "lore" });
    expect(parseHash("#lore/agent/mira/entry")).toEqual({ page: "lore", loreSection: { kind: "agent", key: "mira" } });
  });

  it("formats back to the same hashes", () => {
    for (const h of ["#lore", "#lore/everyone", "#lore/agent/mira", "#lore/manual/review%20pr/entry/L-12"]) {
      expect(formatHash(parseHash(h))).toBe(h);
    }
  });
});

describe("navigation guard", () => {
  it("a refusing guard keeps navigateHash from moving; releasing it lets the next one through", () => {
    history.replaceState(null, "", window.location.pathname + "#lore/agent/mira");
    const asked: string[] = [];
    const release = setNavigationGuard((next) => {
      asked.push(formatHash(next));
      return false;
    });
    navigateHash({ page: "lore" });
    expect(window.location.hash).toBe("#lore/agent/mira");
    expect(asked).toEqual(["#lore"]);
    release();
    navigateHash({ page: "lore" });
    expect(window.location.hash).toBe("#lore");
  });

  it("a refused browser hash change is put back and subscribers are not told", async () => {
    history.replaceState(null, "", window.location.pathname + "#lore/agent/mira");
    const seen: string[] = [];
    function Probe() {
      const [route] = useHashRoute();
      seen.push(formatHash(route));
      return null;
    }
    const { unmount } = render(<Probe />);
    const release = setNavigationGuard(() => false);
    try {
      window.location.hash = "#tasks";
      await waitFor(() => expect(window.location.hash).toBe("#lore/agent/mira"));
      expect(seen.every((h) => h === "#lore/agent/mira")).toBe(true);
    } finally {
      release();
      unmount();
    }
  });
});

describe("navigation guard and real history traversal", () => {
  const wait = () => new Promise((r) => setTimeout(r, 30));

  it("a refused Back keeps both the page and the entry behind it; Back works again once allowed", async () => {
    history.replaceState(null, "", window.location.pathname + "#tasks");
    function Probe() {
      useHashRoute();
      return null;
    }
    const { unmount } = render(<Probe />);
    try {
      navigateHash({ page: "lore", loreSection: { kind: "agent", key: "mira" } });
      await waitFor(() => expect(window.location.hash).toBe("#lore/agent/mira"));
      await wait();
      const lengthBefore = history.length;

      let asked = 0;
      const release = setNavigationGuard(() => {
        asked += 1;
        return false;
      });
      history.back();
      await waitFor(() => expect(asked).toBe(1));
      await waitFor(() => expect(window.location.hash).toBe("#lore/agent/mira"));
      await wait();
      expect(window.location.hash).toBe("#lore/agent/mira");
      expect(history.length).toBe(lengthBefore);
      release();

      // The entry behind was not overwritten: Back still reaches it.
      history.back();
      await waitFor(() => expect(window.location.hash).toBe("#tasks"));
      // …and Forward the section page.
      history.forward();
      await waitFor(() => expect(window.location.hash).toBe("#lore/agent/mira"));
    } finally {
      unmount();
    }
  });
});

describe("history positions after going Back", () => {
  function Probe({ seen }: { seen: string[] }) {
    const [route] = useHashRoute();
    seen.push(formatHash(route));
    return null;
  }

  it("Back, then an in-app navigation, then a refused Back returns to the page on screen", async () => {
    history.replaceState(null, "", window.location.pathname + "#tasks");
    const seen: string[] = [];
    const { unmount } = render(<Probe seen={seen} />);
    try {
      navigateHash({ page: "lore" });
      await waitFor(() => expect(window.location.hash).toBe("#lore"));
      navigateHash({ page: "lore", loreSection: { kind: "agent", key: "mira" } });
      await waitFor(() => expect(window.location.hash).toBe("#lore/agent/mira"));
      history.back();
      await waitFor(() => expect(window.location.hash).toBe("#lore"));
      // A new entry after going Back replaces the forward one.
      navigateHash({ page: "lore", loreSection: { kind: "agent", key: "mira" } });
      await waitFor(() => expect(window.location.hash).toBe("#lore/agent/mira"));

      let asked = 0;
      const release = setNavigationGuard(() => {
        asked += 1;
        return false;
      });
      history.back();
      await waitFor(() => expect(asked).toBe(1));
      await waitFor(() => expect(window.location.hash).toBe("#lore/agent/mira"));
      await new Promise((r) => setTimeout(r, 30));
      expect(window.location.hash).toBe("#lore/agent/mira");
      expect(seen[seen.length - 1]).toBe("#lore/agent/mira");
      release();

      // The page's own link still works, and Back still reaches the directory.
      navigateHash({ page: "lore" });
      await waitFor(() => expect(window.location.hash).toBe("#lore"));
      history.back();
      await waitFor(() => expect(window.location.hash).toBe("#lore/agent/mira"));
    } finally {
      unmount();
    }
  });

  it("a refused Back onto an older, unstamped entry puts the page back without walking further out", async () => {
    // Two entries made before anything stamped them, as an older build would.
    history.replaceState(null, "", window.location.pathname + "#old");
    history.pushState(null, "", window.location.pathname + "#lore/agent/mira");
    const seen: string[] = [];
    const { unmount } = render(<Probe seen={seen} />);
    try {
      const lengthBefore = history.length;
      const release = setNavigationGuard(() => false);
      history.back();
      await waitFor(() => expect(window.location.hash).toBe("#lore/agent/mira"));
      await new Promise((r) => setTimeout(r, 30));
      expect(window.location.hash).toBe("#lore/agent/mira");
      expect(history.length).toBe(lengthBefore);
      release();
      history.back();
      await waitFor(() => expect(window.location.hash).toBe("#old"));
    } finally {
      unmount();
    }
  });
});

describe("the route while a refusal is walked back", () => {
  it("a render in the gap still reads the accepted page, not the refused one", async () => {
    history.replaceState(null, "", window.location.pathname + "#tasks");
    const seen: string[] = [];
    let bump: () => void = () => {};
    function Probe() {
      const [route] = useHashRoute();
      const [, setN] = useState(0);
      bump = () => setN((n) => n + 1);
      seen.push(formatHash(route));
      return null;
    }
    const { unmount } = render(<Probe />);
    try {
      navigateHash({ page: "lore", loreSection: { kind: "agent", key: "mira" } });
      await waitFor(() => expect(seen[seen.length - 1]).toBe("#lore/agent/mira"));
      // The guard re-renders the page, as the lore page does when it shows
      // 「儲存中，完成後才能離開」.
      const release = setNavigationGuard(() => {
        act(() => bump());
        return false;
      });
      const from = seen.length;
      history.back();
      await waitFor(() => expect(seen.length).toBeGreaterThan(from));
      await new Promise((r) => setTimeout(r, 30));
      expect(seen.slice(from).every((h) => h === "#lore/agent/mira")).toBe(true);
      release();
    } finally {
      unmount();
    }
  });
});

describe("a walk-back that never lands", () => {
  async function refusedBackWithStuckGo() {
    history.replaceState(null, "", window.location.pathname + "#tasks");
    const seen: string[] = [];
    let bump: () => void = () => {};
    function Probe() {
      const [route] = useHashRoute();
      const [, setN] = useState(0);
      bump = () => setN((n) => n + 1);
      seen.push(formatHash(route));
      return null;
    }
    const r = render(<Probe />);
    navigateHash({ page: "lore", loreSection: { kind: "agent", key: "mira" } });
    await waitFor(() => expect(seen[seen.length - 1]).toBe("#lore/agent/mira"));
    const go = vi.spyOn(history, "go").mockImplementation(() => {});
    let asked = 0;
    const release = setNavigationGuard(() => {
      asked += 1;
      return false;
    });
    history.back();
    await waitFor(() => expect(asked).toBe(1));
    expect(go).toHaveBeenCalled();
    expect(window.location.hash).toBe("#tasks");
    act(() => bump());
    // Still pinned to the accepted page while the walk-back is pending.
    expect(seen[seen.length - 1]).toBe("#lore/agent/mira");
    return { ...r, seen, bump: () => act(() => bump()), release, go };
  }

  it("a popstate that lands somewhere else puts the accepted URL back", async () => {
    const t = await refusedBackWithStuckGo();
    try {
      window.dispatchEvent(new PopStateEvent("popstate"));
      expect(window.location.hash).toBe("#lore/agent/mira");
      t.bump();
      expect(t.seen[t.seen.length - 1]).toBe("#lore/agent/mira");
    } finally {
      t.release();
      t.go.mockRestore();
      t.unmount();
    }
  });

  it("without a popstate, the fallback puts the accepted URL back instead of switching to the refused one", async () => {
    const t = await refusedBackWithStuckGo();
    try {
      await new Promise((r) => setTimeout(r, 1100));
      expect(window.location.hash).toBe("#lore/agent/mira");
      t.bump();
      expect(t.seen[t.seen.length - 1]).toBe("#lore/agent/mira");
    } finally {
      t.release();
      t.go.mockRestore();
      t.unmount();
    }
  });

  it("the fallback starts at the go(), so a guard that takes longer than it does not cut the walk-back short", async () => {
    history.replaceState(null, "", window.location.pathname + "#tasks");
    function Probe() {
      useHashRoute();
      return null;
    }
    const { unmount } = render(<Probe />);
    navigateHash({ page: "lore", loreSection: { kind: "agent", key: "mira" } });
    await waitFor(() => expect(window.location.hash).toBe("#lore/agent/mira"));
    const go = vi.spyOn(history, "go").mockImplementation(() => {});
    let urlWhenGoCalled = "";
    go.mockImplementation(() => {
      urlWhenGoCalled = window.location.hash;
    });
    let asked = 0;
    const release = setNavigationGuard(() => {
      asked += 1;
      // A confirm dialog left open longer than the fallback.
      const until = Date.now() + 1200;
      while (Date.now() < until) {
        /* busy */
      }
      return false;
    });
    try {
      history.back();
      await waitFor(() => expect(asked).toBe(1));
      // The walk-back is under way from the refused URL; nothing ended it early.
      expect(urlWhenGoCalled).toBe("#tasks");
      await new Promise((r) => setTimeout(r, 20));
      expect(window.location.hash).toBe("#tasks");
    } finally {
      release();
      go.mockRestore();
      unmount();
    }
  });
});
