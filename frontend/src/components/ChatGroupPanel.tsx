import { Fragment, useEffect, useLayoutEffect, useRef, useState } from "react";
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
  outsideOfType,
} from "../lib/chatGroups";
import type { ChatGroup, ChatTab, GroupNameProblem } from "../lib/chatGroups";
import type { TaskTypeView } from "../api/adapter";
import { ChatGroupList } from "./ChatGroupList";
import type { ChatGroupRow, MenuAnchor, MenuTarget } from "./ChatGroupList";
import { UNGROUPED } from "./ChatGroupList";
import {
  CheckIcon,
  ChevronsDownUpIcon,
  ChevronsUpDownIcon,
  FolderPlusIcon,
  FunnelIcon,
} from "./icons";
import { FILTER_SECTIONS, matchesCheck, matchesFilter, readFilterChecks, writeFilterChecks } from "../lib/chatFilter";
import type { FilterCheck, FilterSection, RowFacts } from "../lib/chatFilter";
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
  /** A number at the row's end (the filter's per-item match count). */
  count?: number;
  /** A small heading shown above the first item of each section; an item
   * without one after a section starts below a divider. */
  section?: string;
  checked?: boolean;
  disabled?: boolean;
  danger?: boolean;
  onSelect?: () => void;
}

function focusItems(box: HTMLElement | null) {
  return box ? Array.from(box.querySelectorAll<HTMLElement>(ITEM)) : [];
}

function MenuList({
  label,
  items,
  testId,
  boxRef,
  lead,
}: {
  label: string;
  items: MenuItem[];
  /** Above the items (the filter's name search). */
  lead?: ReactNode;
  testId: string;
  boxRef?: React.RefObject<HTMLDivElement>;
}) {
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const list = focusItems(e.currentTarget);
    const at = list.indexOf(document.activeElement as HTMLElement);
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      e.stopPropagation();
      const step = e.key === "ArrowDown" ? 1 : -1;
      list[(at + step + list.length) % list.length]?.focus();
    } else if ((e.key === "Home" || e.key === "End") && !(e.target instanceof HTMLInputElement)) {
      e.preventDefault();
      (e.key === "Home" ? list[0] : list[list.length - 1])?.focus();
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
      {lead}

      {items.map((it, i) => (
        <Fragment key={it.key}>
          {it.section !== undefined && it.section !== items[i - 1]?.section && (
            <div className="chat-groups__menu-section">{it.section}</div>
          )}
          {it.section === undefined && items[i - 1]?.section !== undefined && <div className="chat-groups__menu-sep" />}
          <button
            type="button"
            role={it.checked === undefined ? "menuitem" : "menuitemcheckbox"}
            aria-checked={it.checked}
            aria-disabled={it.disabled || undefined}
            className={`chat-groups__menu-item${it.danger ? " chat-groups__menu-item--danger" : ""}`}
            data-testid={`${testId}-${it.key}`}
            onClick={() => {
              if (!it.disabled) it.onSelect?.();
            }}
          >
            {it.checked !== undefined && (
              <span className="chat-groups__menu-check" aria-hidden="true">
                {it.checked && <CheckIcon size={14} />}
              </span>
            )}
            <span className="chat-groups__menu-text">{it.label}</span>
            {it.count !== undefined && <span className="chat-groups__menu-count">{it.count}</span>}
          </button>
        </Fragment>
      ))}
    </div>
  );
}

function Menu({
  anchor,
  mobile,
  title,
  main,
  confirm,
  onClose,
  testId,
  lead,
}: {
  anchor: MenuAnchor;
  mobile: boolean;
  title: string;
  main: MenuItem[];
  /** A confirmation shown in place of the menu (delete). */
  confirm: ReactNode | null;
  onClose: (refocus: boolean) => void;
  testId: string;
  /** Above the main items; its text field takes focus and Tab stays inside. */
  lead?: ReactNode;
}) {
  const rootRef = useRef<HTMLDivElement>(null);
  const mainRef = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null);

  useEscapeLayer(() => onClose(true), rootRef);

  const confirming = confirm !== null;
  useLayoutEffect(() => {
    if (mobile) return;
    const box = mainRef.current?.getBoundingClientRect();
    if (!box) return;
    const left = Math.min(Math.max(EDGE, anchor.x), window.innerWidth - EDGE - box.width);
    const top = Math.min(Math.max(EDGE, anchor.y), window.innerHeight - EDGE - box.height);
    setPos({ left, top });
  }, [anchor, mobile]);

  // Tab leaves a menu (closing it) but moves between the confirm's buttons.
  const onTab = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== "Tab") return;
    if (!confirming) {
      if (!lead) onClose(false);
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
    const target = mainRef.current;
    (target?.querySelector("input") ?? focusItems(target)[0])?.focus();
  }, [pos, confirming]);

  useEffect(() => {
    const onDown = (e: PointerEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) onClose(false);
    };
    document.addEventListener("pointerdown", onDown, true);
    return () => document.removeEventListener("pointerdown", onDown, true);
  }, [onClose]);

  const body = confirm ?? <MenuList label={title} items={main} testId={testId} boxRef={mainRef} lead={lead} />;

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

/** Tab and Shift+Tab go round the buttons inside `box`. */
function trapTab(e: KeyboardEvent<HTMLElement>, box: HTMLElement | null) {
  if (e.key !== "Tab" || !box) return;
  const buttons = Array.from(box.querySelectorAll<HTMLElement>("button"));
  const i = buttons.indexOf(document.activeElement as HTMLElement);
  const next = e.shiftKey ? (i <= 0 ? buttons.length - 1 : i - 1) : (i + 1) % buttons.length;
  e.preventDefault();
  buttons[next].focus();
}

/** 自動加入 for one outsource group: a centred dialog (a bottom sheet on
 * phones) with the task-type checklist; 完成, Esc or a click outside closes it.
 * The move-in question opens over it and takes Esc first (the later layer). */
function AutoJoinDialog({
  title,
  items,
  mobile,
  onDone,
}: {
  title: string;
  items: MenuItem[];
  mobile: boolean;
  onDone: () => void;
}) {
  const { t } = useI18n();
  const boxRef = useRef<HTMLDivElement>(null);
  const titleId = "chat-groups-autojoin-title";
  useEscapeLayer(onDone, boxRef);
  useEffect(() => {
    boxRef.current?.querySelector<HTMLElement>("button")?.focus();
  }, []);
  return createPortal(
    <div
      className={mobile ? "chat-groups__sheet-backdrop" : "chat-groups__dialog-backdrop"}
      onMouseDown={(e) => {
        if (e.target !== e.currentTarget) return;
        // Keeps the click from taking focus off the 「⋯」 onDone hands it to.
        e.preventDefault();
        onDone();
      }}
    >
      <div
        ref={boxRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className={mobile ? "chat-groups__sheet" : "chat-groups__dialog"}
        data-testid="chat-group-autojoin"
        onKeyDown={(e) => trapTab(e, boxRef.current)}
      >
        <h2 id={titleId} className="chat-groups__dialog-title">
          {title}
        </h2>
        <p className="chat-groups__dialog-hint">{t.office.groups.autoJoinHint}</p>
        <div className="chat-groups__autojoin-list" role="group" aria-labelledby={titleId}>
          {items.map((it) => (
            <button
              key={it.key}
              type="button"
              role="checkbox"
              aria-checked={it.checked}
              className="chat-groups__menu-item"
              data-testid={`chat-group-autojoin-${it.key}`}
              onClick={it.onSelect}
            >
              <span className="chat-groups__menu-check chat-groups__menu-check--box" aria-hidden="true">
                {it.checked && <CheckIcon size={12} />}
              </span>
              <span className="chat-groups__autojoin-label">{it.label}</span>
              {it.hint && <span className="chat-groups__autojoin-owner">{it.hint}</span>}
            </button>
          ))}
          {items.length === 0 && <div className="chat-groups__menu-empty">{t.office.groups.autoJoinNone}</div>}
        </div>
        <div className="chat-groups__dialog-actions chat-groups__dialog-actions--end">
          <button
            type="button"
            className="chat-groups__entry-btn chat-groups__entry-btn--primary"
            data-testid="chat-group-autojoin-done"
            onClick={onDone}
          >
            {t.office.groups.autoJoinDone}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}

/** The move-in question when a ticked type has workers outside the group:
 * each one ticked by default, its current place on the right. Answers with the
 * ids to move, or none (都不搬 or Esc). */
function BringInDialog({
  title,
  workers,
  onAnswer,
}: {
  title: string;
  workers: { id: string; name: string; place: string }[];
  onAnswer: (moving: string[]) => void;
}) {
  const { t } = useI18n();
  const boxRef = useRef<HTMLDivElement>(null);
  const titleId = "chat-groups-bring-in-title";
  const [ticked, setTicked] = useState<string[]>(() => workers.map((w) => w.id));
  useEscapeLayer(() => onAnswer([]), boxRef);
  useEffect(() => {
    boxRef.current?.querySelector<HTMLElement>(".chat-groups__entry-btn--primary")?.focus();
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
        onKeyDown={(e) => trapTab(e, boxRef.current)}
      >
        <h2 id={titleId} className="chat-groups__dialog-title">
          {title}
        </h2>
        <div className="chat-groups__autojoin-list" role="group" aria-labelledby={titleId}>
          {workers.map((w) => {
            const on = ticked.includes(w.id);
            return (
              <button
                key={w.id}
                type="button"
                role="checkbox"
                aria-checked={on}
                className="chat-groups__menu-item"
                data-testid={`chat-groups-bring-in-${w.id}`}
                onClick={() => setTicked(on ? ticked.filter((x) => x !== w.id) : [...ticked, w.id])}
              >
                <span className="chat-groups__menu-check chat-groups__menu-check--box" aria-hidden="true">
                  {on && <CheckIcon size={12} />}
                </span>
                <span className="chat-groups__autojoin-label">{w.name}</span>
                <span className="chat-groups__autojoin-owner">{w.place}</span>
              </button>
            );
          })}
        </div>
        <div className="chat-groups__dialog-actions chat-groups__dialog-actions--end">
          <button
            type="button"
            className="chat-groups__entry-btn chat-groups__entry-btn--primary"
            data-testid="chat-groups-bring-in-move"
            disabled={ticked.length === 0}
            onClick={() => onAnswer(workers.map((w) => w.id).filter((id) => ticked.includes(id)))}
          >
            {t.office.groups.bringInMoveTicked}
          </button>
          <button
            type="button"
            className="chat-groups__entry-btn"
            data-testid="chat-groups-bring-in-none"
            onClick={() => onAnswer([])}
          >
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
 * 新增群組 entry, the group and member menus (a heading's 「⋯」 or the menu
 * key), inline rename, delete confirmation, the 自動加入
 * dialog and its bring-in question.
 */
export function ChatGroupPanel({
  tab,
  ids,
  groups,
  update,
  newGroupId,
  nameOf,
  unreadOf,
  factsOf,
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
  /** What the 篩選 reads for a row besides its unread count: the same
   * presence and activity the row shows, and the names it is searched by. */
  factsOf: (id: string) => Omit<RowFacts, "unread">;
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
  const [menu, setMenu] = useState<{ target: MenuTarget; anchor: MenuAnchor } | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [creating, setCreating] = useState<{ memberId?: string } | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  const [bringIn, setBringIn] = useState<{
    groupId: string;
    keys: string[];
    typeName: string;
    workers: { id: string; from: string | null }[];
  } | null>(null);
  const returnTo = useRef<HTMLElement | null>(null);
  // The group whose 自動加入 dialog is open, and the box the move-in
  // question was asked from (focus goes back to it).
  const [autoJoin, setAutoJoin] = useState<string | null>(null);
  const bringInFrom = useRef<HTMLElement | null>(null);
  const mine = tabGroups(groups, tab);

  const [checks, setChecks] = useState<FilterCheck[]>(() => readFilterChecks(tab));
  const [query, setQuery] = useState("");
  const [filterAt, setFilterAt] = useState<MenuAnchor | null>(null);
  const filterRef = useRef<HTMLButtonElement>(null);
  const changeChecks = (next: FilterCheck[]) => {
    setChecks(next);
    writeFilterChecks(tab, next);
  };
  const facts = (id: string): RowFacts => ({ unread: unreadOf(id) > 0, ...factsOf(id) });
  const activeFilters = checks.length + (query.trim() === "" ? 0 : 1);
  const filtering = activeFilters > 0;
  const shownIds = filtering ? ids.filter((id) => matchesFilter(facts(id), checks, query)) : ids;
  const checkLabel: Record<FilterCheck, string> = {
    unread: t.office.groups.filterUnread,
    ...t.office.presence,
    ...t.activity.state,
  };
  const sectionLabel: Record<FilterSection, string> = {
    unread: t.office.groups.filterSectionUnread,
    presence: t.office.groups.filterSectionPresence,
    activity: t.office.groups.filterSectionActivity,
  };

  const sectionKeys = [...mine.map((g) => g.id), UNGROUPED];
  const anyExpanded = sectionKeys.some((k) => collapsed[k] !== true);

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
    const section = t.office.groups.moveTo;
    return [
      ...mine.map((g) => ({
        key: `move-${g.id}`,
        section,
        label: g.name,
        checked: g.id === current,
        disabled: g.id === current,
        onSelect: () => {
          update((s) => moveMember(s, memberId, g.id));
          closeMenu(true);
        },
      })),
      {
        key: "move-ungrouped",
        section,
        label: t.office.groups.ungrouped,
        checked: current === null,
        disabled: current === null,
        onSelect: () => {
          update((s) => moveMember(s, memberId, null));
          closeMenu(true);
        },
      },
      {
        key: "move-new",
        label: t.office.groups.moveToNew,
        onSelect: () => {
          closeMenu(false);
          setCreating({ memberId });
        },
      },
    ];
  };

  // Ticking saves at once, as every other group change does; 完成 only closes.
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
          const outside = on ? [] : outsideOfType(groups, workers ?? [], tt.typeKey, group.id);
          if (outside.length > 0) {
            bringInFrom.current = document.activeElement as HTMLElement | null;
            setBringIn({ groupId: group.id, keys, typeName: tt.displayName, workers: outside });
            return;
          }
          update((s) => setGroupTaskTypes(s, group.id, keys));
        },
      };
    });
  const autoJoinGroup = autoJoin ? mine.find((g) => g.id === autoJoin) : undefined;

  let menuNode: ReactNode = null;
  if (menu) {
    const { target } = menu;
    if (target.kind === "member") {
      menuNode = (
        <Menu
          anchor={menu.anchor}
          mobile={mobile}
          title={nameOf(target.id)}
          main={memberItems(target.id)}
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
                ? [
                    {
                      key: "autojoin",
                      label: t.office.groups.autoJoinOpen,
                      onSelect: () => {
                        setMenu(null);
                        setAutoJoin(group.id);
                      },
                    },
                  ]
                : []),
              { key: "delete", label: t.office.groups.delete, danger: true, onSelect: () => setConfirmDelete(true) },
            ]}
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

  const filterMenu = filterAt && (
    <Menu
      anchor={filterAt}
      mobile={mobile}
      title={t.office.groups.filter}
      lead={
        <input
          type="search"
          className="chat-groups__menu-search"
          data-testid="chat-filter-search"
          placeholder={t.office.groups.filterSearch}
          aria-label={t.office.groups.filterSearch}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      }
      main={[
        ...(Object.keys(FILTER_SECTIONS) as FilterSection[]).flatMap((s) =>
          FILTER_SECTIONS[s].map((c) => ({
            key: c,
            section: sectionLabel[s],
            label: checkLabel[c],
            checked: checks.includes(c),
            count: ids.filter((id) => matchesCheck(facts(id), c)).length,
            onSelect: () => changeChecks(checks.includes(c) ? checks.filter((x) => x !== c) : [...checks, c]),
          })),
        ),
        ...(filtering
          ? [
              {
                key: "clear",
                label: t.office.groups.filterClear,
                onSelect: () => {
                  changeChecks([]);
                  setQuery("");
                },
              },
            ]
          : []),
      ]}
      confirm={null}
      onClose={(refocus) => {
        setFilterAt(null);
        if (refocus) filterRef.current?.focus();
      }}
      testId="chat-filter-menu"
    />
  );
  const collapseLabel = anyExpanded ? t.office.groups.collapseAll : t.office.groups.expandAll;

  return (
    <>
      <div className="chat-groups__toolbar" data-testid="chat-groups-toolbar">
        <button
          ref={filterRef}
          type="button"
          className={`chat-groups__tool chat-groups__filter${filtering ? " chat-groups__filter--on" : ""}`}
          aria-haspopup="menu"
          aria-expanded={filterAt !== null}
          data-testid="chat-filter"
          onClick={(e) => {
            const r = e.currentTarget.getBoundingClientRect();
            setFilterAt(filterAt ? null : { x: r.left, y: r.bottom + 4 });
          }}
        >
          <FunnelIcon size={14} />
          <span>{msg.groupFilterLabel(activeFilters)}</span>
        </button>
        <span className="chat-groups__toolbar-gap" />
        <button
          type="button"
          className="chat-groups__tool chat-groups__tool--icon"
          aria-label={collapseLabel}
          title={collapseLabel}
          disabled={mine.length === 0 || filtering}
          data-testid="chat-groups-collapse-all"
          onClick={() => onCollapsedChange(anyExpanded ? Object.fromEntries(sectionKeys.map((k) => [k, true])) : {})}
        >
          {anyExpanded ? <ChevronsDownUpIcon size={16} /> : <ChevronsUpDownIcon size={16} />}
        </button>
        <button
          type="button"
          className="chat-groups__tool chat-groups__tool--icon"
          aria-label={t.office.groups.newGroup}
          title={t.office.groups.newGroup}
          data-testid="chat-group-new"
          onClick={() => setCreating({})}
        >
          <FolderPlusIcon size={16} />
        </button>
      </div>
      <ChatGroupList
        tab={tab}
        ids={shownIds}
        filtering={filtering}
        empty={
          <div className="chat-groups__empty" data-testid="chat-groups-empty">
            {t.office.groups.filterEmpty}
          </div>
        }
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
      />
      {menuNode}
      {filterMenu}
      {autoJoinGroup && (
        <AutoJoinDialog
          title={msg.groupAutoJoinTitle(autoJoinGroup.name)}
          items={autoJoinItems(autoJoinGroup)}
          mobile={mobile}
          onDone={() => {
            setAutoJoin(null);
            returnTo.current?.focus();
          }}
        />
      )}
      {bringIn && (
        <BringInDialog
          title={msg.groupBringInTitle(bringIn.typeName, mine.find((g) => g.id === bringIn.groupId)?.name ?? "")}
          workers={bringIn.workers.map((w) => ({
            id: w.id,
            name: nameOf(w.id),
            place:
              w.from === null
                ? t.office.groups.ungrouped
                : msg.groupCurrentlyIn(mine.find((g) => g.id === w.from)?.name ?? ""),
          }))}
          onAnswer={(moving) => {
            const { groupId, keys } = bringIn;
            update((s) => appendMembers(setGroupTaskTypes(s, groupId, keys), groupId, moving));
            setBringIn(null);
            bringInFrom.current?.focus();
          }}
        />
      )}
    </>
  );
}
