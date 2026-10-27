import { Fragment, useRef, useState } from "react";
import type { CSSProperties, KeyboardEvent, MouseEvent, ReactNode } from "react";
import {
  DndContext,
  DragOverlay,
  KeyboardSensor,
  MeasuringStrategy,
  MouseSensor,
  TouchSensor,
  closestCenter,
  pointerWithin,
  useDroppable,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import type {
  Announcements,
  CollisionDetection,
  DraggableAttributes,
  DraggableSyntheticListeners,
  Modifier,
  UniqueIdentifier,
} from "@dnd-kit/core";
import type { SortingStrategy } from "@dnd-kit/sortable";
import {
  SortableContext,
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useI18n } from "../i18n";
import { layoutTab, moveMember, tabGroups } from "../lib/chatGroups";
import type { ChatGroup, ChatTab } from "../lib/chatGroups";
import { ChevronDownIcon, MoreIcon } from "./icons";
import "./chat-groups.css";

// Same hold on mouse and touch (owner: like rearranging the iPhone home
// screen). Moving past the tolerance before the hold ends cancels it, so a
// press-and-move is a scroll / text gesture, never a drag.
const LONG_PRESS = { delay: 450, tolerance: 6 };
export const UNGROUPED = "__ungrouped";

// Draggable / droppable ids carry their kind: m: member row, g: group heading,
// s: a section's drop area, h: the 未分組 heading.
const mid = (id: string) => `m:${id}`;
const gid = (id: string) => `g:${id}`;
const sid = (id: string) => `s:${id}`;
const kindOf = (id: UniqueIdentifier) => String(id)[0];
const bare = (id: UniqueIdentifier) => String(id).slice(2);

// 未分組 has no order of its own (owner): its rows never make room for a drag.
const keepOrder: SortingStrategy = () => null;

const verticalOnly: Modifier = ({ transform }) => ({ ...transform, x: 0 });


export type MenuTarget = { kind: "member" | "group"; id: string };
export type MenuAnchor = { x: number; y: number };

export interface ChatGroupRow {
  /** The row's id, put on the row as `data-chat-row`. */
  rowId: string;
  ref: (el: HTMLElement | null) => void;
  style: CSSProperties;
  className: string;
  attributes: DraggableAttributes;
  listeners: DraggableSyntheticListeners;
  /** False during a drag: the row must not navigate. */
  canActivate: () => boolean;
  /** Keeps Android's own long-press menu off a touch lift; a mouse right-click
   * gets the browser's menu (owner: no app menu on right-click). */
  onContextMenu: (e: MouseEvent) => void;
  /** Opens the menu on the context-menu key or Shift+F10; true when it did. */
  menuKey: (e: KeyboardEvent) => boolean;
}

// Android fires contextmenu on a long press, which here is a lift.
const fromTouch = (e: MouseEvent) => (e.nativeEvent as PointerEvent).pointerType === "touch";
const keepTouchLift = (e: MouseEvent) => {
  if (fromTouch(e)) e.preventDefault();
};
const isMenuKey = (e: KeyboardEvent) => e.key === "ContextMenu" || (e.shiftKey && e.key === "F10");
const anchorOf = (el: Element): MenuAnchor => {
  const r = el.getBoundingClientRect();
  return { x: r.left + 16, y: r.bottom };
};

function SortableRow({
  id,
  canActivate,
  openMenu,
  render,
}: {
  id: string;
  canActivate: () => boolean;
  openMenu: (target: MenuTarget, at: MenuAnchor) => void;
  render: (row: ChatGroupRow) => ReactNode;
}) {
  const { t } = useI18n();
  const s = useSortable({ id: mid(id), attributes: { roleDescription: t.office.groups.roleDescription } });
  return (
    <>
      {render({
        rowId: id,
        ref: s.setNodeRef,
        style: { transform: CSS.Translate.toString(s.transform), transition: s.transition },
        className: `chat-groups__row${s.isDragging ? " chat-groups__row--placeholder" : ""}`,
        attributes: s.attributes,
        listeners: s.listeners,
        canActivate,
        onContextMenu: keepTouchLift,
        menuKey: (e) => {
          if (!isMenuKey(e)) return false;
          e.preventDefault();
          openMenu({ kind: "member", id }, anchorOf(e.currentTarget));
          return true;
        },
      })}
    </>
  );
}

function HeadingBody({ label, collapsed, unread }: { label: string; collapsed: boolean; unread: number }) {
  return (
    <>
      <span className="chat-groups__heading-label">{label}</span>
      <ChevronDownIcon size={14} className="chat-groups__chevron" />
      <span className="member-card__unread-slot chat-groups__pill-slot">
        {collapsed && unread > 0 && (
          <span className="member-card__unread chat-groups__heading-unread">{unread > 99 ? "99+" : unread}</span>
        )}
      </span>
    </>
  );
}

function GroupHeading({
  group,
  label,
  collapsed,
  unread,
  over,
  canActivate,
  onToggle,
  openMenu,
}: {
  group: ChatGroup;
  label: string;
  collapsed: boolean;
  unread: number;
  over: boolean;
  canActivate: () => boolean;
  onToggle: () => void;
  openMenu: (target: MenuTarget, at: MenuAnchor) => void;
}) {
  const { t, msg } = useI18n();
  const s = useSortable({ id: gid(group.id), attributes: { roleDescription: t.office.groups.groupRoleDescription } });
  const options = msg.groupOptions(group.name);
  return (
    <div
      ref={s.setNodeRef}
      className={`chat-groups__heading-row chat-groups__row${s.isDragging ? " chat-groups__row--placeholder" : ""}`}
      style={{ transform: CSS.Translate.toString(s.transform), transition: s.transition }}
    >
      <button
        ref={s.setActivatorNodeRef}
        {...s.attributes}
        {...s.listeners}
        type="button"
        className={`chat-groups__heading${over ? " chat-groups__heading--over" : ""}`}
        aria-expanded={!collapsed}
        data-testid={`chat-group-heading-${group.id}`}
        onClick={() => {
          if (canActivate()) onToggle();
        }}
        onContextMenu={keepTouchLift}
        onKeyDown={(e) => {
          if (isMenuKey(e)) {
            e.preventDefault();
            openMenu({ kind: "group", id: group.id }, anchorOf(e.currentTarget));
            return;
          }
          s.listeners?.onKeyDown?.(e);
        }}
      >
        <HeadingBody label={label} collapsed={collapsed} unread={unread} />
      </button>
      {/* Outside the heading (the drag handle), so pressing it never lifts the
          group, and its click never reaches the collapse toggle. */}
      <button
        type="button"
        className="chat-groups__more"
        aria-label={options}
        aria-haspopup="menu"
        title={options}
        data-testid={`chat-group-more-${group.id}`}
        onClick={(e) => openMenu({ kind: "group", id: group.id }, anchorOf(e.currentTarget))}
      >
        <MoreIcon size={16} />
      </button>
    </div>
  );
}

function UngroupedHeading({
  label,
  collapsed,
  unread,
  over,
  onToggle,
}: {
  label: string;
  collapsed: boolean;
  unread: number;
  over: boolean;
  onToggle: () => void;
}) {
  const d = useDroppable({ id: `h:${UNGROUPED}` });
  // 未分組 cannot be renamed, deleted or given auto-join, so it has no menu.
  return (
    <div className="chat-groups__heading-row">
      <button
        ref={d.setNodeRef}
        type="button"
        className={`chat-groups__heading${over ? " chat-groups__heading--over" : ""}`}
        aria-expanded={!collapsed}
        data-testid="chat-group-heading-ungrouped"
        onClick={onToggle}
      >
        <HeadingBody label={label} collapsed={collapsed} unread={unread} />
      </button>
    </div>
  );
}

/** A group's rows; an empty group shows where to drop instead (no guide
 * line), and the whole section stays the drop target. */
function Section({
  id,
  over,
  empty,
  children,
}: {
  id: string;
  over: boolean;
  empty?: ReactNode;
  children: ReactNode;
}) {
  const d = useDroppable({ id: sid(id) });
  return (
    <div
      ref={d.setNodeRef}
      className={`chat-groups__section${over ? " chat-groups__section--over" : ""}${empty ? " chat-groups__section--empty" : ""}`}
      data-testid={`chat-group-section-${id === UNGROUPED ? "ungrouped" : id}`}
    >
      {empty ? (
        <div className="chat-groups__drop-here" data-testid={`chat-group-drop-${id}`}>
          {empty}
        </div>
      ) : (
        children
      )}
    </div>
  );
}

/**
 * One tab's chat list, laid out by the owner's groups: each group's heading
 * and rows, then 未分組 with every other row in the tab's default order. With
 * no groups in the tab it is just the rows, no headings. Rows and group
 * headings lift on a long press: rows move between groups, headings reorder
 * groups.
 */
export function ChatGroupList({
  tab,
  ids,
  groups,
  nameOf,
  unreadOf,
  renderRow,
  renderOverlay,
  collapsed,
  onCollapsedChange,
  onMoveMember,
  onReorderGroup,
  onOpenMenu,
  creating,
  editorFor,
  filtering = false,
  empty,
  className,
  testId,
}: {
  tab: ChatTab;
  /** The tab's rows in default order. */
  ids: readonly string[];
  groups: readonly ChatGroup[];
  nameOf: (id: string) => string;
  unreadOf: (id: string) => number;
  renderRow: (id: string, row: ChatGroupRow) => ReactNode;
  renderOverlay: (id: string) => ReactNode;
  /** Collapsed sections by group id (and UNGROUPED). */
  collapsed: Readonly<Record<string, boolean>>;
  onCollapsedChange: (next: Record<string, boolean>) => void;
  onMoveMember: (memberId: string, groupId: string | null, index?: number) => void;
  onReorderGroup: (groupId: string, index: number) => void;
  onOpenMenu: (target: MenuTarget, at: MenuAnchor) => void;
  /** The inline 新增群組 entry, shown where the new group will appear. */
  creating?: ReactNode;
  /** An inline rename editor that stands in for a group's heading. */
  editorFor?: (groupId: string) => ReactNode | null;
  /** `ids` are a filter's matches: groups without one are left out, the rest
   * show open (the saved folding is untouched), and nothing can be dragged. */
  filtering?: boolean;
  /** Shown instead of the list when filtering leaves no row. */
  empty?: ReactNode;
  className: string;
  testId?: string;
}) {
  const { t, msg } = useI18n();
  // While a member is dragged over another group, it is shown in that group
  // (the rows there make room); nothing is written until the drop.
  const [preview, setPreview] = useState<readonly ChatGroup[] | null>(null);
  const { sections, ungrouped } = layoutTab(preview ?? groups, tab, ids, (x) => x);
  const hasGroups = sections.length > 0;
  const shownSections = filtering ? sections.filter((x) => x.items.length > 0) : sections;
  const showUngrouped = !filtering || ungrouped.length > 0;
  const [active, setActive] = useState<string | null>(null);
  const [overId, setOverId] = useState<string | null>(null);
  // Enter both drops a keyboard drag and opens a row's chat; the row checks
  // this so a drop never also opens the chat.
  const draggingRef = useRef(false);
  const canActivate = () => !draggingRef.current;
  // Lifting a group folds every section away, which moves the heading itself;
  // keep the lifted heading (and its collision box) under the pointer.
  const startTopRef = useRef<number | null>(null);
  const shiftRef = useRef(0);
  const followStart: Modifier = ({ transform, activeNodeRect }) => {
    if (startTopRef.current === null || !activeNodeRect) return transform;
    shiftRef.current = startTopRef.current - activeNodeRect.top;
    return { ...transform, y: transform.y + shiftRef.current };
  };

  const noSensors = useSensors();
  const sensors = useSensors(
    useSensor(MouseSensor, { activationConstraint: LONG_PRESS }),
    useSensor(TouchSensor, { activationConstraint: LONG_PRESS }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
      keyboardCodes: { start: ["Space"], cancel: ["Escape"], end: ["Space", "Enter", "Tab"] },
    }),
  );

  const savedGroupOf = (memberId: string): string | null =>
    tabGroups(groups, tab).find((g) => g.memberIds.includes(memberId))?.id ?? null;
  const groupOf = (memberId: string): string | null =>
    sections.find((s) => s.items.includes(memberId))?.group.id ?? null;
  const groupName = (g: string | null) =>
    g === null ? t.office.groups.ungrouped : (sections.find((s) => s.group.id === g)?.group.name ?? "");
  const tabGroupIds = sections.map((s) => s.group.id);

  /** Where a dragged member would land: a group (or null = ungrouped) and an
   * index in it (undefined = end; ungrouped has no order). */
  const memberLanding = (member: string, over: UniqueIdentifier): { group: string | null; index?: number } => {
    const k = kindOf(over);
    if (k === "m") {
      const target = bare(over);
      const g = groupOf(target);
      if (g === null) return { group: null };
      const list = sections.find((s) => s.group.id === g)!.items;
      if (groupOf(member) === g) {
        return { group: g, index: arrayMove(list, list.indexOf(member), list.indexOf(target)).indexOf(member) };
      }
      return { group: g, index: list.indexOf(target) };
    }
    const g = bare(over);
    return { group: g === UNGROUPED ? null : g };
  };
  const groupLanding = (group: string, over: UniqueIdentifier) =>
    arrayMove(tabGroupIds, tabGroupIds.indexOf(group), tabGroupIds.indexOf(bare(over))).indexOf(group);

  const collision: CollisionDetection = (args) => {
    const dragKind = kindOf(args.active.id);
    const droppableContainers = args.droppableContainers.filter((c) => dragKind !== "g" || kindOf(c.id) === "g");
    // A lifted group folds the sections away and moves its own heading, so
    // judge by the pointer, not by the heading's (moved) box.
    if (dragKind === "g" && args.pointerCoordinates) {
      const y = args.pointerCoordinates.y;
      let best: { id: UniqueIdentifier; d: number } | null = null;
      for (const c of droppableContainers) {
        const r = args.droppableRects.get(c.id);
        if (!r) continue;
        const d = Math.abs(r.top + r.height / 2 - y);
        if (!best || d < best.d) best = { id: c.id, d };
      }
      if (best) return [{ id: best.id, data: { droppableContainer: droppableContainers.find((c) => c.id === best!.id), value: best.d } }];
    }
    if (dragKind === "m") {
      const within = pointerWithin({ ...args, droppableContainers });
      const row = within.find((c) => kindOf(c.id) === "m");
      if (row) return [row];
      if (within.length) return [within[0]];
    }
    return closestCenter({ ...args, droppableContainers });
  };

  const name = (id: UniqueIdentifier) =>
    kindOf(id) === "g" ? groupName(bare(id)) : nameOf(bare(id));
  const describe = (activeId: UniqueIdentifier, over: UniqueIdentifier, done: boolean) => {
    if (kindOf(activeId) === "g") {
      const n = groupLanding(bare(activeId), over) + 1;
      return done ? msg.groupDragDroppedPosition(name(activeId), n) : msg.groupDragOverPosition(name(activeId), n);
    }
    const target = groupName(memberLanding(bare(activeId), over).group);
    return done ? msg.groupDragDroppedGroup(name(activeId), target) : msg.groupDragOverGroup(name(activeId), target);
  };
  const announcements: Announcements = {
    onDragStart: ({ active: a }) => msg.groupDragPickedUp(name(a.id)),
    onDragOver: ({ active: a, over }) => (over ? describe(a.id, over.id, false) : undefined),
    onDragEnd: ({ active: a, over }) => (over ? describe(a.id, over.id, true) : msg.groupDragCancelled(name(a.id))),
    onDragCancel: ({ active: a }) => msg.groupDragCancelled(name(a.id)),
  };

  /** Show a dragged member in the group it is over, at the hovered row (or
   * the end of the group's empty area); 未分組 keeps its default order. */
  const previewOver = (activeId: UniqueIdentifier, over: UniqueIdentifier) => {
    if (kindOf(activeId) !== "m") return;
    const member = bare(activeId);
    const k = kindOf(over);
    let target: string | null;
    let index: number | undefined;
    if (k === "m") {
      target = groupOf(bare(over));
      if (target !== null) index = sections.find((x) => x.group.id === target)!.items.indexOf(bare(over));
    } else if (k === "s") {
      target = bare(over) === UNGROUPED ? null : bare(over);
    } else {
      return;
    }
    if (target === groupOf(member)) return;
    setPreview(moveMember(preview ?? groups, member, target, index));
  };
  const finish = () => {
    setPreview(null);
    draggingRef.current = false;
    startTopRef.current = null;
    shiftRef.current = 0;
    setActive(null);
    setOverId(null);
  };

  // The section the dragged member is over (to light it up).
  const overSection = (() => {
    if (!active || kindOf(active) !== "m" || !overId) return null;
    const landing = memberLanding(bare(active), overId);
    return landing.group ?? UNGROUPED;
  })();
  const draggingGroup = active !== null && kindOf(active) === "g";
  const isCollapsed = (key: string) => !filtering && collapsed[key] === true;
  const toggle = (key: string) => onCollapsedChange({ ...collapsed, [key]: !isCollapsed(key) });
  const sum = (list: readonly string[]) => list.reduce((n, x) => n + unreadOf(x), 0);

  const rows = (list: readonly string[], strategy: SortingStrategy = verticalListSortingStrategy) => (
    <SortableContext items={list.map(mid)} strategy={strategy}>
      {list.map((id) => (
        <SortableRow
          key={id}
          id={id}
          canActivate={canActivate}
          openMenu={onOpenMenu}
          render={(row) => renderRow(id, row)}
        />
      ))}
    </SortableContext>
  );

  return (
    <DndContext
      sensors={filtering ? noSensors : sensors}
      collisionDetection={collision}
      modifiers={[verticalOnly, followStart]}
      measuring={{ droppable: { strategy: MeasuringStrategy.Always } }}
      accessibility={{
        announcements,
        screenReaderInstructions: { draggable: t.office.groups.instructions },
      }}
      onDragStart={({ active: a }) => {
        draggingRef.current = true;
        startTopRef.current =
          kindOf(a.id) === "g"
            ? (document
                .querySelector(`[data-testid="chat-group-heading-${bare(a.id)}"]`)
                ?.getBoundingClientRect().top ?? null)
            : null;
        setActive(String(a.id));
      }}
      onDragOver={({ active: a, over }) => {
        setOverId(over ? String(over.id) : null);
        if (over) previewOver(a.id, over.id);
      }}
      onDragCancel={finish}
      onDragEnd={({ active: a, over }) => {
        finish();
        // Over its own place is a no-op, unless that place is in the group the
        // member was previewed into.
        if (!over || (over.id === a.id && preview === null)) return;
        if (kindOf(a.id) === "g") {
          if (kindOf(over.id) === "g") onReorderGroup(bare(a.id), groupLanding(bare(a.id), over.id));
          return;
        }
        const member = bare(a.id);
        const landing = memberLanding(member, over.id);
        if (landing.group === null && savedGroupOf(member) === null) return;
        onMoveMember(member, landing.group, landing.index);
      }}
    >
      <div className={className} data-testid={testId}>
        {creating}
        {filtering && ids.length === 0 ? (
          empty
        ) : hasGroups ? (
          <>
            <SortableContext items={tabGroupIds.map(gid)} strategy={verticalListSortingStrategy}>
              {shownSections.map(({ group, items }) => (
                <Fragment key={group.id}>
                  {editorFor?.(group.id) ?? (
                  <GroupHeading
                    group={group}
                    label={msg.groupHeading(group.name, items.length)}
                    collapsed={isCollapsed(group.id) || draggingGroup}
                    unread={sum(items)}
                    over={overSection === group.id}
                    canActivate={canActivate}
                    onToggle={() => toggle(group.id)}
                    openMenu={onOpenMenu}
                  />
                  )}
                  {!isCollapsed(group.id) && !draggingGroup && (
                    <Section
                      id={group.id}
                      over={overSection === group.id}
                      empty={items.length === 0 ? t.office.groups.dropHere : undefined}
                    >
                      {rows(items)}
                    </Section>
                  )}
                </Fragment>
              ))}
            </SortableContext>
            {showUngrouped && (
              <UngroupedHeading
                label={msg.groupHeading(t.office.groups.ungrouped, ungrouped.length)}
                collapsed={isCollapsed(UNGROUPED)}
                unread={sum(ungrouped)}
                over={overSection === UNGROUPED}
                onToggle={() => toggle(UNGROUPED)}
              />
            )}
            {showUngrouped && !isCollapsed(UNGROUPED) && !draggingGroup && (
              <Section id={UNGROUPED} over={overSection === UNGROUPED}>
                {rows(ungrouped, keepOrder)}
              </Section>
            )}
          </>
        ) : (
          rows(ungrouped, keepOrder)
        )}
        <DragOverlay modifiers={[verticalOnly]}>
          {active !== null && (
            <div className="chat-groups__lifted" data-testid="chat-groups-lifted">
              <div className="chat-groups__wobble">
                {kindOf(active) === "g" ? (
                  <div className="chat-groups__heading chat-groups__heading--overlay">
                    <HeadingBody
                      label={msg.groupHeading(
                        groupName(bare(active)),
                        sections.find((s) => s.group.id === bare(active))?.items.length ?? 0,
                      )}
                      collapsed
                      unread={0}
                    />
                  </div>
                ) : (
                  renderOverlay(bare(active))
                )}
              </div>
            </div>
          )}
        </DragOverlay>
      </div>
    </DndContext>
  );
}
