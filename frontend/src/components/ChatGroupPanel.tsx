import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { KeyboardEvent, ReactNode } from "react";
import { createPortal } from "react-dom";
import { useI18n } from "../i18n";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import {
  appendMembers,
  createGroup,
  deleteGroup,
  groupNameProblem,
  moveMember,
  renameGroup,
  reorderGroup,
  setGroupTaskTypes,
  tabGroups,
  ungroupedOfType,
} from "../lib/chatGroups";
import type { ChatGroup, ChatTab, GroupNameProblem } from "../lib/chatGroups";
import type { TaskTypeView } from "../api/adapter";
import { ChatGroupList } from "./ChatGroupList";
import type { ChatGroupRow, MenuAnchor, MenuTarget } from "./ChatGroupList";
import { CheckIcon, ChevronRightIcon, ChevronLeftIcon, PlusIcon } from "./icons";
import "./chat-groups.css";

const EDGE = 8;
const ITEM = "[role='menuitem'], [role='menuitemcheckbox'], [role='menuitemradio']";

type Update = (change: (current: readonly ChatGroup[]) => ChatGroup[]) => void;

function useProblemText() {
  const { t } = useI18n();
  return (p: GroupNameProblem) =>
    p === "blank" ? t.office.groups.nameBlank : p === "long" ? t.office.groups.nameTooLong : t.office.groups.nameTaken;
}

/** Inline group-name entry: new group, or a rename standing in for a heading. */
function NameEntry({
  initial,
  problemOf,
  onDone,
  onCancel,
  testId,
}: {
  initial: string;
  problemOf: (name: string) => GroupNameProblem | null;
  onDone: (name: string) => void;
  onCancel: () => void;
  testId: string;
}) {
  const { t } = useI18n();
  const text = useProblemText();
  const [name, setName] = useState(initial);
  const [shown, setShown] = useState<GroupNameProblem | null>(null);
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    ref.current?.focus();
    ref.current?.select();
  }, []);
  const submit = () => {
    const p = problemOf(name);
    if (p) {
      setShown(p);
      ref.current?.focus();
      return;
    }
    onDone(name);
  };
  return (
    <form
      className="chat-groups__entry"
      data-testid={testId}
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      <div className="chat-groups__entry-line">
        <input
          ref={ref}
          className="chat-groups__entry-input"
          value={name}
          placeholder={t.office.groups.newGroupName}
          aria-label={t.office.groups.newGroupName}
          aria-invalid={shown !== null || undefined}
          onChange={(e) => {
            setName(e.target.value);
            setShown(null);
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              e.preventDefault();
              e.stopPropagation();
              onCancel();
            }
          }}
        />
        <button type="submit" className="chat-groups__entry-btn chat-groups__entry-btn--primary">
          {t.office.groups.create}
        </button>
        <button type="button" className="chat-groups__entry-btn" onClick={onCancel}>
          {t.office.groups.cancel}
        </button>
      </div>
      {shown && (
        <div className="chat-groups__entry-error" role="alert">
          {text(shown)}
        </div>
      )}
    </form>
  );
}

interface MenuItem {
  key: string;
  label: string;
  hint?: string;
  checked?: boolean;
  disabled?: boolean;
  danger?: boolean;
  submenu?: "move" | "autojoin";
  onSelect?: () => void;
}

function focusItems(box: HTMLElement | null) {
  return box ? Array.from(box.querySelectorAll<HTMLElement>(ITEM)) : [];
}

function MenuList({
  label,
  items,
  onOpenSub,
  onBack,
  showBack = false,
  testId,
  boxRef,
}: {
  label: string;
  items: MenuItem[];
  onOpenSub: (sub: "move" | "autojoin", from: HTMLElement) => void;
  onBack?: () => void;
  /** The phone sheet shows a back row; on desktop ArrowLeft goes back. */
  showBack?: boolean;
  testId: string;
  boxRef?: React.RefObject<HTMLDivElement>;
}) {
  const { t } = useI18n();
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const list = focusItems(e.currentTarget);
    const at = list.indexOf(document.activeElement as HTMLElement);
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      e.stopPropagation();
      const step = e.key === "ArrowDown" ? 1 : -1;
      list[(at + step + list.length) % list.length]?.focus();
    } else if (e.key === "Home" || e.key === "End") {
      e.preventDefault();
      (e.key === "Home" ? list[0] : list[list.length - 1])?.focus();
    } else if (e.key === "ArrowLeft" && onBack) {
      e.preventDefault();
      e.stopPropagation();
      onBack();
    }
  };
  return (
    <div
      ref={boxRef}
      role="menu"
      aria-label={label}
      className="chat-groups__menu-panel"
      data-testid={`${testId}-panel`}
      onKeyDown={onKeyDown}
    >
      {onBack && showBack && (
        <button type="button" role="menuitem" className="chat-groups__menu-item chat-groups__menu-back" onClick={onBack}>
          <ChevronLeftIcon size={14} />
          <span>{label}</span>
        </button>
      )}
      {items.map((it) => (
        <button
          key={it.key}
          type="button"
          role={it.checked === undefined ? "menuitem" : "menuitemcheckbox"}
          aria-checked={it.checked}
          aria-disabled={it.disabled || undefined}
          aria-haspopup={it.submenu ? "menu" : undefined}
          className={`chat-groups__menu-item${it.danger ? " chat-groups__menu-item--danger" : ""}`}
          data-testid={`${testId}-${it.key}`}
          onClick={(e) => {
            if (it.disabled) return;
            if (it.submenu) onOpenSub(it.submenu, e.currentTarget);
            else it.onSelect?.();
          }}
          onKeyDown={(e) => {
            if (it.submenu && (e.key === "ArrowRight" || e.key === "Enter" || e.key === " ")) {
              e.preventDefault();
              e.stopPropagation();
              onOpenSub(it.submenu, e.currentTarget);
            }
          }}
        >
          {it.checked !== undefined && (
            <span className="chat-groups__menu-check" aria-hidden="true">
              {it.checked && <CheckIcon size={14} />}
            </span>
          )}
          <span className="chat-groups__menu-text">
            <span>{it.label}</span>
            {it.hint && <span className="chat-groups__menu-hint">{it.hint}</span>}
          </span>
          {it.submenu && <ChevronRightIcon size={14} className="chat-groups__menu-more" />}
        </button>
      ))}
      {items.length === 0 && <div className="chat-groups__menu-empty">{t.office.groups.autoJoinNone}</div>}
    </div>
  );
}

function Menu({
  anchor,
  mobile,
  title,
  main,
  subs,
  initialSub,
  confirm,
  onClose,
  testId,
}: {
  anchor: MenuAnchor;
  mobile: boolean;
  title: string;
  main: MenuItem[];
  subs: Partial<Record<"move" | "autojoin", { label: string; items: MenuItem[]; hint?: string }>>;
  initialSub?: "move" | "autojoin";
  /** A confirmation shown in place of the menu (delete). */
  confirm: ReactNode | null;
  onClose: (refocus: boolean) => void;
  testId: string;
}) {
  const [sub, setSub] = useState<"move" | "autojoin" | null>(initialSub ?? null);
  const rootRef = useRef<HTMLDivElement>(null);
  const mainRef = useRef<HTMLDivElement>(null);
  const subRef = useRef<HTMLDivElement>(null);
  const subFrom = useRef<HTMLElement | null>(null);
  const [pos, setPos] = useState<{ left: number; top: number; flip: boolean } | null>(null);

  useEscapeLayer(() => {
    if (sub && !mobile) closeSub();
    else onClose(true);
  }, rootRef);

  useLayoutEffect(() => {
    if (mobile) return;
    const box = mainRef.current?.getBoundingClientRect();
    if (!box) return;
    const left = Math.min(Math.max(EDGE, anchor.x), window.innerWidth - EDGE - box.width);
    const top = Math.min(Math.max(EDGE, anchor.y), window.innerHeight - EDGE - box.height);
    setPos({ left, top, flip: left + box.width * 2 + 4 > window.innerWidth - EDGE });
  }, [anchor, mobile]);

  const confirming = confirm !== null;
  // Tab leaves a menu (closing it) but moves between the confirm's buttons.
  const onTab = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== "Tab") return;
    if (!confirming) {
      onClose(false);
      return;
    }
    const buttons = Array.from(rootRef.current?.querySelectorAll<HTMLElement>(".chat-groups__confirm button") ?? []);
    const i = buttons.indexOf(document.activeElement as HTMLElement);
    e.preventDefault();
    buttons[(i + (e.shiftKey ? -1 : 1) + buttons.length) % buttons.length]?.focus();
  };
  useEffect(() => {
    if (confirming) {
      rootRef.current?.querySelector<HTMLElement>(".chat-groups__confirm button")?.focus();
      return;
    }
    const target = sub ? subRef.current : mainRef.current;
    focusItems(target)[0]?.focus();
  }, [sub, pos, confirming]);

  useEffect(() => {
    const onDown = (e: PointerEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) onClose(false);
    };
    document.addEventListener("pointerdown", onDown, true);
    return () => document.removeEventListener("pointerdown", onDown, true);
  }, [onClose]);

  const openSub = (s: "move" | "autojoin", from: HTMLElement) => {
    subFrom.current = from;
    setSub(s);
  };
  const closeSub = () => {
    setSub(null);
    requestAnimationFrame(() => subFrom.current?.focus());
  };
  const subMenu = sub ? subs[sub] : undefined;

  const body = confirm ?? (
    <>
      {(!mobile || !sub) && (
        <MenuList label={title} items={main} onOpenSub={openSub} testId={testId} boxRef={mainRef} />
      )}
      {subMenu && (
        <div
          className={`chat-groups__menu-sub${pos?.flip ? " chat-groups__menu-sub--flip" : ""}`}
          ref={subRef}
        >
          {subMenu.hint && <div className="chat-groups__menu-note">{subMenu.hint}</div>}
          <MenuList
            label={subMenu.label}
            items={subMenu.items}
            onOpenSub={openSub}
            onBack={closeSub}
            showBack={mobile}
            testId={`${testId}-${sub}`}
          />
        </div>
      )}
    </>
  );

  return createPortal(
    mobile ? (
      <div className="chat-groups__sheet-backdrop">
        <div
          ref={rootRef}
          role="dialog"
          aria-modal="true"
          aria-label={title}
          className="chat-groups__sheet"
          data-testid={`${testId}-sheet`}
          onKeyDown={(e) => {
            onTab(e);
          }}
        >
          <div className="chat-groups__sheet-title">{title}</div>
          {body}
        </div>
      </div>
    ) : (
      <div
        ref={rootRef}
        className="chat-groups__menu"
        style={pos ? { left: pos.left, top: pos.top } : { left: -9999, top: 0 }}
        onKeyDown={(e) => {
          onTab(e);
        }}
      >
        {body}
      </div>
    ),
    document.body,
  );
}

function BringInDialog({
  text,
  names,
  onAnswer,
}: {
  text: string;
  names: string[];
  onAnswer: (bring: boolean) => void;
}) {
  const { t } = useI18n();
  const boxRef = useRef<HTMLDivElement>(null);
  const titleId = "chat-groups-bring-in-title";
  useEscapeLayer(() => onAnswer(false), boxRef);
  useEffect(() => {
    boxRef.current?.querySelector<HTMLElement>("button")?.focus();
  }, []);
  return createPortal(
    <div className="chat-groups__dialog-backdrop">
      <div
        ref={boxRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="chat-groups__dialog"
        data-testid="chat-groups-bring-in"
        onKeyDown={(e) => {
          if (e.key !== "Tab") return;
          const buttons = Array.from(boxRef.current!.querySelectorAll<HTMLElement>("button"));
          const i = buttons.indexOf(document.activeElement as HTMLElement);
          const next = e.shiftKey ? (i <= 0 ? buttons.length - 1 : i - 1) : (i + 1) % buttons.length;
          e.preventDefault();
          buttons[next].focus();
        }}
      >
        <p id={titleId} className="chat-groups__dialog-text">
          {text}
        </p>
        <ul className="chat-groups__dialog-list">
          {names.map((n) => (
            <li key={n}>{n}</li>
          ))}
        </ul>
        <div className="chat-groups__dialog-actions">
          <button type="button" className="chat-groups__entry-btn chat-groups__entry-btn--primary" onClick={() => onAnswer(true)}>
            {t.office.groups.bringInAccept}
          </button>
          <button type="button" className="chat-groups__entry-btn" onClick={() => onAnswer(false)}>
            {t.office.groups.bringInDecline}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}

export type ChatGrouping = Pick<
  Parameters<typeof ChatGroupPanel>[0],
  "groups" | "update" | "newGroupId" | "collapsed" | "onCollapsedChange" | "mobile" | "taskTypes"
>;

/**
 * One tab's grouped chat list with everything that edits the groups: the
 * 新增群組 entry, the group and member menus (right-click, the menu key, or a
 * touch long-press released in place), inline rename, delete confirmation and
 * the auto-join bring-in question.
 */
export function ChatGroupPanel({
  tab,
  ids,
  groups,
  update,
  newGroupId,
  nameOf,
  unreadOf,
  renderRow,
  renderOverlay,
  collapsed,
  onCollapsedChange,
  mobile,
  taskTypes,
  workers,
  className,
  testId,
}: {
  tab: ChatTab;
  ids: readonly string[];
  groups: readonly ChatGroup[];
  update: Update;
  newGroupId: () => string;
  nameOf: (id: string) => string;
  unreadOf: (id: string) => number;
  renderRow: (id: string, row: ChatGroupRow) => ReactNode;
  renderOverlay: (id: string) => ReactNode;
  collapsed: Readonly<Record<string, boolean>>;
  onCollapsedChange: (next: Record<string, boolean>) => void;
  mobile: boolean;
  /** Outsource only: every task type, for 自動加入. */
  taskTypes?: readonly TaskTypeView[];
  /** Outsource only: the tab's workers in default order with their task type. */
  workers?: readonly { id: string; taskTypeKey?: string }[];
  className: string;
  testId?: string;
}) {
  const { t, msg } = useI18n();
  const [menu, setMenu] = useState<{ target: MenuTarget; anchor: MenuAnchor; sub?: "move" | "autojoin" } | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [creating, setCreating] = useState<{ memberId?: string } | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  const [bringIn, setBringIn] = useState<{ groupId: string; keys: string[]; typeName: string; ids: string[] } | null>(
    null,
  );
  const returnTo = useRef<HTMLElement | null>(null);
  const mine = tabGroups(groups, tab);

  const openMenu = (target: MenuTarget, anchor: MenuAnchor) => {
    returnTo.current = document.activeElement as HTMLElement | null;
    setConfirmDelete(false);
    setMenu({ target, anchor });
  };
  const closeMenu = (refocus: boolean) => {
    setMenu(null);
    setConfirmDelete(false);
    if (refocus) returnTo.current?.focus();
  };

  const problemOf = (except?: string) => (name: string) => groupNameProblem(groups, tab, name, except);

  const memberItems = (memberId: string): MenuItem[] => {
    const current = mine.find((g) => g.memberIds.includes(memberId))?.id ?? null;
    return [
      ...mine.map((g) => ({
        key: g.id,
        label: g.name,
        checked: g.id === current,
        disabled: g.id === current,
        onSelect: () => {
          update((s) => moveMember(s, memberId, g.id));
          closeMenu(true);
        },
      })),
      {
        key: "ungrouped",
        label: t.office.groups.ungrouped,
        checked: current === null,
        disabled: current === null,
        onSelect: () => {
          update((s) => moveMember(s, memberId, null));
          closeMenu(true);
        },
      },
      {
        key: "new",
        label: t.office.groups.moveToNew,
        onSelect: () => {
          closeMenu(false);
          setCreating({ memberId });
        },
      },
    ];
  };

  const autoJoinItems = (group: ChatGroup): MenuItem[] =>
    (taskTypes ?? []).map((tt) => {
      const owner = mine.find((g) => g.taskTypeKeys.includes(tt.typeKey));
      const on = owner?.id === group.id;
      return {
        key: tt.typeKey,
        label: tt.displayName,
        hint: owner && !on ? msg.groupCurrentlyIn(owner.name) : undefined,
        checked: on,
        onSelect: () => {
          const keys = on
            ? group.taskTypeKeys.filter((k) => k !== tt.typeKey)
            : [...group.taskTypeKeys, tt.typeKey];
          const candidates = on ? [] : ungroupedOfType(groups, workers ?? [], tt.typeKey);
          if (candidates.length > 0) {
            closeMenu(false);
            setBringIn({ groupId: group.id, keys, typeName: tt.displayName, ids: candidates });
            return;
          }
          update((s) => setGroupTaskTypes(s, group.id, keys));
        },
      };
    });

  let menuNode: ReactNode = null;
  if (menu) {
    const { target } = menu;
    if (target.kind === "member") {
      menuNode = (
        <Menu
          anchor={menu.anchor}
          mobile={mobile}
          title={nameOf(target.id)}
          main={[{ key: "move", label: t.office.groups.moveTo, submenu: "move" }]}
          subs={{ move: { label: t.office.groups.moveTo, items: memberItems(target.id) } }}
          initialSub={mobile ? "move" : undefined}
          confirm={null}
          onClose={closeMenu}
          testId="chat-member-menu"
        />
      );
    } else {
      const group = mine.find((g) => g.id === target.id);
      if (group) {
        menuNode = (
          <Menu
            anchor={menu.anchor}
            mobile={mobile}
            title={group.name}
            main={[
              {
                key: "rename",
                label: t.office.groups.rename,
                onSelect: () => {
                  closeMenu(false);
                  setRenaming(group.id);
                },
              },
              ...(tab === "outsource"
                ? [{ key: "autojoin", label: t.office.groups.autoJoin, submenu: "autojoin" as const }]
                : []),
              { key: "delete", label: t.office.groups.delete, danger: true, onSelect: () => setConfirmDelete(true) },
            ]}
            subs={{
              autojoin: { label: t.office.groups.autoJoin, items: autoJoinItems(group), hint: t.office.groups.autoJoinHint },
            }}
            confirm={
              confirmDelete ? (
                <div className="chat-groups__confirm" role="alertdialog" aria-label={t.office.groups.delete}>
                  <p className="chat-groups__dialog-text">{msg.groupDeleteConfirm(group.name)}</p>
                  <div className="chat-groups__dialog-actions">
                    <button
                      type="button"
                      className="chat-groups__entry-btn chat-groups__entry-btn--danger"
                      data-testid="chat-group-delete-confirm"
                      onClick={() => {
                        update((s) => deleteGroup(s, group.id));
                        closeMenu(false);
                      }}
                    >
                      {t.office.groups.deleteConfirm}
                    </button>
                    <button
                      type="button"
                      className="chat-groups__entry-btn"
                      data-testid="chat-group-delete-cancel"
                      onClick={() => closeMenu(true)}
                    >
                      {t.office.groups.cancel}
                    </button>
                  </div>
                </div>
              ) : null
            }
            onClose={closeMenu}
            testId="chat-group-menu"
          />
        );
      }
    }
  }

  return (
    <>
      <ChatGroupList
        tab={tab}
        ids={ids}
        groups={groups}
        nameOf={nameOf}
        unreadOf={unreadOf}
        renderRow={renderRow}
        renderOverlay={renderOverlay}
        collapsed={collapsed}
        onCollapsedChange={onCollapsedChange}
        onMoveMember={(m, g, i) => update((s) => moveMember(s, m, g, i))}
        onReorderGroup={(g, i) => update((s) => reorderGroup(s, g, i))}
        onOpenMenu={openMenu}
        className={className}
        testId={testId}
        editorFor={(groupId) =>
          renaming === groupId ? (
            <NameEntry
              initial={mine.find((g) => g.id === groupId)?.name ?? ""}
              problemOf={problemOf(groupId)}
              onDone={(name) => {
                update((s) => renameGroup(s, groupId, name));
                setRenaming(null);
              }}
              onCancel={() => setRenaming(null)}
              testId="chat-group-rename"
            />
          ) : null
        }
        creating={
          creating ? (
            <NameEntry
              initial=""
              problemOf={problemOf()}
              onDone={(name) => {
                update((s) =>
                  createGroup(s, {
                    id: newGroupId(),
                    tab,
                    name,
                    memberIds: creating.memberId ? [creating.memberId] : [],
                  }),
                );
                setCreating(null);
              }}
              onCancel={() => setCreating(null)}
              testId="chat-group-new-entry"
            />
          ) : null
        }
        footer={
          !creating && (
            <button
              type="button"
              className="chat-groups__new"
              data-testid="chat-group-new"
              onClick={() => setCreating({})}
            >
              <PlusIcon size={14} />
              <span>{t.office.groups.newGroup}</span>
            </button>
          )
        }
      />
      {menuNode}
      {bringIn && (
        <BringInDialog
          text={msg.groupBringIn(
            bringIn.ids.length,
            bringIn.typeName,
            mine.find((g) => g.id === bringIn.groupId)?.name ?? "",
          )}
          names={bringIn.ids.map(nameOf)}
          onAnswer={(bring) => {
            const { groupId, keys, ids: joining } = bringIn;
            update((s) => {
              const ruled = setGroupTaskTypes(s, groupId, keys);
              return bring ? appendMembers(ruled, groupId, joining) : ruled;
            });
            setBringIn(null);
            returnTo.current?.focus();
          }}
        />
      )}
    </>
  );
}
