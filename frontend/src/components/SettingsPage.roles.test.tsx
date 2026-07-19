// M2-2 角色誌新增/刪除 — the Settings › 角色誌 custom-role lifecycle UI.
//
//   1. 新增角色定義 sits at the BOTTOM of the role list as a shared `.add-entry` row (centered "+ label", low-key neutral frame);
//      clicking it grows an INLINE editable row with a single 角色名 field
//      (owner-aligned pattern) — Enter/確認 creates, Esc/取消 collapses the row.
//      Creating is one role + one founding member whose NAME the server picks
//      from the pool (隨機成員名) and whose model/effort ride server defaults.
//   2. 刪除 (trash icon button, left of the row chevron) shows ONLY on custom
//      roles (never the seed assistant), and commits only through an explicit
//      CENTERED CONFIRM MODAL (Esc / 取消 close it; the row stays untouched).
//   3. The delete 409 防線: a role with an ONLINE member is refused server-side
//      and the UI surfaces the honest 「有成員在線上，無法刪除」 line (the row
//      stays).
//
// Runs against the REAL mock adapter (mock parity with handle_create_role /
// handle_delete_role), like the sibling global-context test.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { api } from "../api";
import { __resetMock, __setMockMemberOnline } from "../api/mock";

const s = zh.settings;

async function openRolesLog() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByText(s.roles));
  await utils.findByText(s.systemName);
  return utils;
}

/** Create through the INLINE row: open, type the 角色名, press Enter. */
async function createViaRow(
  utils: Awaited<ReturnType<typeof openRolesLog>>,
  { roleName = "研究員" } = {}
) {
  fireEvent.click(utils.getByText(`+ ${s.addRole}`));
  const input = utils.getByTestId("role-create-name");
  fireEvent.change(input, { target: { value: roleName } });
  fireEvent.keyDown(input, { key: "Enter" });
  await utils.findByText(roleName);
}

/** The founding member of the (single) custom role on the mock roster. */
async function foundingMember() {
  const custom = (await api.listRoles()).find((r) => !r.isSeed);
  if (!custom) return undefined;
  return (await api.listMembers()).find((m) => m.role === custom.key);
}

beforeEach(() => {
  __resetMock();
});

describe("SettingsPage · #settings/roles deep-link (T-f074 正職 ➕👤)", () => {
  it("opens straight on the 角色誌 list when initialRoles is set", async () => {
    const utils = render(
      <I18nProvider>
        <SettingsPage initialRoles />
      </I18nProvider>
    );
    // No manual navigation: the roles list (its 新增角色 row + system block)
    // renders on mount, ready for the owner to add a role.
    await utils.findByText(s.systemName);
    expect(utils.getByText(`+ ${s.addRole}`)).toBeTruthy();
  });

  it("opens the 角色誌 list already in CREATE mode when initialRolesCreate is set (T-25b7 #settings/roles/new)", async () => {
    const utils = render(
      <I18nProvider>
        <SettingsPage initialRolesCreate />
      </I18nProvider>
    );
    // The inline 新增角色 create row is already expanded on mount — no click on
    // the "+ 新增角色" entry needed (that entry is replaced by the row).
    const input = await utils.findByTestId("role-create-name");
    expect(input).toBeTruthy();
    expect(document.activeElement).toBe(input); // autoFocus lands on the field
  });

  it("opens straight on the role's definition page when initialRoleKey is set (#settings/roles/<roleKey>)", async () => {
    const utils = render(
      <I18nProvider>
        <SettingsPage initialRoleKey="assistant" />
      </I18nProvider>
    );
    // The role detail (localized seed title + its lessons card) renders on
    // mount — no list navigation needed.
    // (findAll — the localized title renders in both the breadcrumb and the h1)
    const titles = await utils.findAllByText(zh.office.role.assistant);
    expect(titles.length).toBeGreaterThan(0);
    await utils.findAllByText(s.edit);
    expect(utils.queryByText(`+ ${s.addRole}`)).toBeNull();
  });

  it("self-heals an unknown initialRoleKey to the 角色誌 list once roles load", async () => {
    const utils = render(
      <I18nProvider>
        <SettingsPage initialRoleKey="ghost-role" />
      </I18nProvider>
    );
    await utils.findByText(s.systemName);
    expect(utils.getByText(`+ ${s.addRole}`)).toBeTruthy();
  });
});

describe("SettingsPage · 角色誌 新增角色 auto-scroll (T-25b7 owner feedback)", () => {
  // jsdom has no layout engine and no scrollIntoView; install a spy so the
  // effect's call is observable (production guards it with `?.` so an absent
  // method is a no-op).
  let scrollSpy: ReturnType<typeof vi.fn>;
  beforeEach(() => {
    scrollSpy = vi.fn();
    Element.prototype.scrollIntoView = scrollSpy;
  });
  afterEach(() => {
    // @ts-expect-error — restore jsdom's (missing) default
    delete Element.prototype.scrollIntoView;
  });

  it("scrolls the create row into view on the #settings/roles/new deep-link, re-firing AFTER the async role list loads", async () => {
    const utils = render(
      <I18nProvider>
        <SettingsPage initialRolesCreate />
      </I18nProvider>
    );
    // The row renders in create mode immediately (autoCreate seeds `adding`).
    await utils.findByTestId("role-create-row");
    // …but the role journal loads async. Wait for a real role row to land so
    // the list has its full height (the case where the row is below the fold).
    await utils.findByText(zh.office.role.assistant);

    // The row was scrolled into view, centered, and the LAST call targets the
    // create row itself — i.e. the scroll ran once the loaded list existed, so
    // the data load did not "eat" the scroll.
    await waitFor(() => expect(scrollSpy).toHaveBeenCalled());
    const calls = scrollSpy.mock.calls;
    expect(calls[calls.length - 1]?.[0]).toMatchObject({ block: "center" });
    const row = utils.getByTestId("role-create-row");
    const instances = scrollSpy.mock.instances;
    expect(instances[instances.length - 1]).toBe(row);
  });

  it("scrolls the create row into view when create mode is opened by clicking + 新增角色", async () => {
    const utils = await openRolesLog();
    scrollSpy.mockClear(); // ignore any scrolls from the roles-list mount
    fireEvent.click(utils.getByText(`+ ${s.addRole}`));
    const row = await utils.findByTestId("role-create-row");
    await waitFor(() => expect(scrollSpy).toHaveBeenCalled());
    const instances = scrollSpy.mock.instances;
    expect(instances[instances.length - 1]).toBe(row);
  });

  it("does NOT scroll when the roles list is opened WITHOUT create mode", async () => {
    const utils = render(
      <I18nProvider>
        <SettingsPage initialRoles />
      </I18nProvider>
    );
    await utils.findByText(zh.office.role.assistant);
    // The plain roles deep-link keeps the create row collapsed → no scroll.
    expect(utils.queryByTestId("role-create-row")).toBeNull();
    expect(scrollSpy).not.toHaveBeenCalled();
  });
});

describe("SettingsPage · 角色誌 新增角色定義 (inline row)", () => {
  it("grows an inline single-field row and creates one role + one server-named member", async () => {
    const utils = await openRolesLog();
    // The add affordance exists; the row is closed until clicked.
    // Owner feedback (M2 + 修仙 batch 1): the button shows the "+" and carries
    // NO avatar icon, and it wears the SHARED `.add-entry` silhouette
    // (centered low-key neutral row — no accent green), identical to 監控's
    // 新增機器.
    const addBtn = utils
      .getByText(`+ ${s.addRole}`)
      .closest("button") as HTMLButtonElement;
    expect(addBtn).toBeTruthy();
    expect(addBtn.className).toBe("add-entry");
    expect(addBtn.textContent).toContain("+");
    expect(addBtn.querySelector("svg")).toBeNull();
    expect(utils.queryByTestId("role-create-row")).toBeNull();

    fireEvent.click(utils.getByText(`+ ${s.addRole}`));
    // The inline row carries exactly ONE field: the 角色名 input. No member
    // name / model / effort inputs — those are server defaults now.
    const row = utils.getByTestId("role-create-row");
    expect(row.querySelectorAll("input").length).toBe(1);
    expect(row.querySelector("select")).toBeNull();

    fireEvent.change(utils.getByTestId("role-create-name"), {
      target: { value: "研究員" },
    });
    fireEvent.keyDown(utils.getByTestId("role-create-name"), { key: "Enter" });
    await utils.findByText("研究員");

    // The new role renders in the list with the 自訂 badge; the row collapsed.
    expect(utils.getByText(s.customBadge)).toBeTruthy();
    expect(utils.queryByTestId("role-create-row")).toBeNull();

    // One founding member landed on the roster — offline, SERVER-named (pool
    // pick, never blank), with the default launch knobs (mock parity with
    // handle_create_role).
    const member = await foundingMember();
    expect(member).toBeTruthy();
    expect(member!.name.length).toBeGreaterThan(0);
    expect(member!.name).not.toBe("Mira"); // the pool never shadows the seed
    expect(member!.status).toBe("offline"); // 初始離線 — creating never spawns
    expect(member!.model).toBe(""); // blank ⇒ CLI default
    expect(member!.effort).toBe("medium");
    expect(member!.roleName).toBe("研究員");
  });

  it("Esc collapses the row without creating; the confirm button also creates", async () => {
    const utils = await openRolesLog();
    fireEvent.click(utils.getByText(`+ ${s.addRole}`));
    fireEvent.change(utils.getByTestId("role-create-name"), {
      target: { value: "顧問" },
    });
    fireEvent.keyDown(utils.getByTestId("role-create-name"), { key: "Escape" });
    expect(utils.queryByTestId("role-create-row")).toBeNull();
    expect((await api.listRoles()).length).toBe(1); // still only the seed

    // Reopen: the draft was discarded, and the 建立 button commits too.
    fireEvent.click(utils.getByText(`+ ${s.addRole}`));
    const input = utils.getByTestId("role-create-name") as HTMLInputElement;
    expect(input.value).toBe("");
    fireEvent.change(input, { target: { value: "顧問" } });
    fireEvent.click(utils.getByTestId("role-create-submit"));
    await utils.findByText("顧問");
    expect((await api.listRoles()).some((r) => r.name === "顧問")).toBe(true);
  });

  it("a blank 角色名 surfaces the honest error and creates nothing", async () => {
    const utils = await openRolesLog();
    fireEvent.click(utils.getByText(`+ ${s.addRole}`));
    fireEvent.click(utils.getByTestId("role-create-submit"));
    await utils.findByText(s.addRoleError);
    expect((await api.listRoles()).length).toBe(1); // still only the seed
  });
});

describe("SettingsPage · 角色誌 刪除 (M2-2)", () => {
  it("offers delete ONLY on custom roles and commits through the confirm modal", async () => {
    const utils = await openRolesLog();
    // Seed assistant: no delete affordance at all.
    expect(utils.queryByTestId("role-delete-assistant")).toBeNull();
    expect(utils.queryByLabelText(s.deleteRole)).toBeNull();

    await createViaRow(utils);
    const roles = await api.listRoles();
    const custom = roles.find((r) => !r.isSeed)!;
    const member = await foundingMember();

    // The custom row carries the icon delete button (aria-label = 刪除);
    // clicking opens the CENTERED confirm modal while the row itself stays
    // rendered untouched (button + chevron still on the row).
    const deleteBtn = utils.getByTestId(`role-delete-${custom.key}`);
    expect(deleteBtn.getAttribute("aria-label")).toBe(s.deleteRole);
    fireEvent.click(deleteBtn);
    const modal = await utils.findByTestId("role-delete-confirm");
    expect(modal.getAttribute("role")).toBe("dialog");
    expect(modal.getAttribute("aria-modal")).toBe("true");
    expect(
      utils.getByText(s.deleteRoleConfirm("研究員"))
    ).toBeTruthy();
    expect(utils.getByTestId(`role-delete-${custom.key}`)).toBeTruthy();

    // Esc closes without deleting; reopening still works.
    fireEvent.keyDown(window, { key: "Escape" });
    await waitFor(() =>
      expect(utils.queryByTestId("role-delete-confirm")).toBeNull()
    );
    expect((await api.listRoles()).some((r) => r.key === custom.key)).toBe(
      true
    );
    fireEvent.click(utils.getByTestId(`role-delete-${custom.key}`));
    await utils.findByTestId("role-delete-confirm");

    fireEvent.click(utils.getByTestId("role-delete-confirm-btn"));
    // The role row drops; the founding member is gone from the roster too
    // (hard cascade, mock parity).
    await waitFor(() => expect(utils.queryByText("研究員")).toBeNull());
    expect((await api.listRoles()).some((r) => r.key === custom.key)).toBe(
      false
    );
    expect((await api.listMembers()).some((m) => m.id === member!.id)).toBe(
      false
    );
  });

  it("keeps the role and shows 有成員在線上,無法刪除 on the 409 防線", async () => {
    const utils = await openRolesLog();
    await createViaRow(utils);
    const member = (await foundingMember())!;
    __setMockMemberOnline(member.id, true); // the hub-projection stand-in

    const custom = (await api.listRoles()).find((r) => !r.isSeed)!;
    fireEvent.click(utils.getByTestId(`role-delete-${custom.key}`));
    fireEvent.click(await utils.findByTestId("role-delete-confirm-btn"));

    // The honest 409 message renders IN the still-open modal; nothing was
    // deleted.
    await utils.findByText(s.deleteRoleOnline);
    expect(utils.getByTestId("role-delete-confirm")).toBeTruthy();
    expect(utils.getByText("研究員")).toBeTruthy();
    expect((await api.listRoles()).some((r) => r.key === custom.key)).toBe(
      true
    );

    // Once the member goes offline the same confirm goes through.
    __setMockMemberOnline(member.id, false);
    fireEvent.click(utils.getByTestId("role-delete-confirm-btn"));
    await waitFor(() => expect(utils.queryByText("研究員")).toBeNull());
  });
});

describe("SettingsPage · 自訂角色 改名 (custom-only rename)", () => {
  it("renames a custom role from its detail page and the list follows", async () => {
    const utils = await openRolesLog();
    await createViaRow(utils);

    // Open the custom role's detail page; the title carries the pencil
    // inline-edit (the same pattern as the machine row's name).
    fireEvent.click(utils.getByText("研究員"));
    const pencil = await utils.findByLabelText(zh.settings.renameRole);
    fireEvent.click(pencil);
    const input = utils.getByLabelText(
      zh.settings.renameRole,
      { selector: "input" }
    ) as HTMLInputElement;
    expect(input.value).toBe("研究員");
    fireEvent.change(input, { target: { value: "資料科學家" } });
    fireEvent.keyDown(input, { key: "Enter" });

    // The detail title (and its breadcrumb's terminal segment) follow, and so
    // do the roles list + the roster's resolved role_name (single truth: the
    // role doc's name).
    await utils.findAllByText("資料科學家");
    expect(
      (await api.listRoles()).some((r) => r.name === "資料科學家")
    ).toBe(true);
    const member = await foundingMember();
    expect(member!.roleName).toBe("資料科學家");

    // Back to the list via the 角色誌 breadcrumb (T-8f6e: no back button).
    fireEvent.click(utils.getByRole("button", { name: zh.settings.roles }));
    await utils.findByText("資料科學家"); // the list row shows the new name
  });

  it("offers NO rename on a seed role's detail page", async () => {
    const utils = await openRolesLog();
    fireEvent.click(utils.getByText(zh.office.role.assistant));
    await utils.findAllByText(zh.settings.edit); // 編輯 renders (doc + lessons cards)
    expect(utils.queryByLabelText(zh.settings.renameRole)).toBeNull();
  });
});

describe("SettingsPage · 角色詳情清理 (no filename chip · custom-role reset gone)", () => {
  it("shows NO internal filename chip on the role detail page", async () => {
    const utils = await openRolesLog();
    fireEvent.click(utils.getByText(zh.office.role.assistant));
    await utils.findAllByText(zh.settings.edit);
    // The role-….md implementation detail never renders.
    expect(utils.queryByText(/^role-.+\.md$/)).toBeNull();
  });

  it("edit mode offers 重置 on a seed role but NOT on a custom role", async () => {
    const utils = await openRolesLog();

    // Seed assistant: edit mode carries the reset (back-to-seed) button.
    fireEvent.click(utils.getByText(zh.office.role.assistant));
    fireEvent.click((await utils.findAllByText(zh.settings.edit))[0]);
    expect(utils.getByText(zh.settings.reset)).toBeTruthy();
    fireEvent.click(utils.getByText(zh.settings.cancel));
    // Back to the list via the 角色誌 breadcrumb (T-8f6e: no back button).
    fireEvent.click(utils.getByRole("button", { name: zh.settings.roles }));

    // Custom role: NO reset affordance — there is no seed to restore (the
    // server 404s a custom reset; verified live), so the button is omitted
    // instead of left half-dead.
    await createViaRow(utils);
    fireEvent.click(utils.getByText("研究員"));
    fireEvent.click((await utils.findAllByText(zh.settings.edit))[0]);
    expect(utils.queryByText(zh.settings.reset)).toBeNull();
    expect(utils.getByText(zh.settings.doneEdit)).toBeTruthy(); // still editable
  });
});
