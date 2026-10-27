import { Fragment, useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import type { CSSProperties, ReactNode } from "react";
import {
  DndContext,
  DragOverlay,
  KeyboardSensor,
  MouseSensor,
  TouchSensor,
  closestCenter,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import type {
  Announcements,
  DraggableAttributes,
  DraggableSyntheticListeners,
  Modifier,
  UniqueIdentifier,
} from "@dnd-kit/core";
import {
  SortableContext,
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useI18n } from "../i18n";
import { ChevronDownIcon, PinIcon } from "./icons";
import "./chat-pins.css";

// Same hold on mouse and touch (owner: like rearranging the iPhone home
// screen). Moving past the tolerance before the hold ends cancels it, so a
// press-and-move is a scroll / text gesture, never a drag.
const LONG_PRESS = { delay: 450, tolerance: 6 };
// A toggle moves its row between the pinned section and the plain list, so
// React remounts the row and the focused button with it. The remounted button
// takes focus back by this key.
let refocusToggle: string | null = null;

/** Vertical only, and never out of the pinned section (the dragged row's
 * parent). */
const withinSection: Modifier = ({ transform, draggingNodeRect, containerNodeRect }) => {
  if (!draggingNodeRect || !containerNodeRect) return { ...transform, x: 0 };
  const minY = containerNodeRect.top - draggingNodeRect.top;
  const maxY = containerNodeRect.bottom - draggingNodeRect.bottom;
  return { ...transform, x: 0, y: Math.min(Math.max(transform.y, minY), maxY) };
};

/** Which of a tab's two sections are collapsed. */
export interface ChatPinCollapsed {
  pinned: boolean;
  rest: boolean;
}

export interface ChatPinRow {
  ref: (el: HTMLElement | null) => void;
  style: CSSProperties;
  className: string;
  attributes: DraggableAttributes;
  listeners: DraggableSyntheticListeners;
  /** False during a drag and right after it: the row must not navigate. */
  canActivate: () => boolean;
}

/** The pin toggle that sits right after a row's name. */
export function ChatPinToggle({
  pinned,
  onToggle,
  focusKey,
  testId,
}: {
  pinned: boolean;
  onToggle: () => void;
  /** Unique per row; keeps keyboard focus on this toggle across the move. */
  focusKey: string;
  testId?: string;
}) {
  const { t } = useI18n();
  const ref = useRef<HTMLButtonElement>(null);
  useLayoutEffect(() => {
    if (refocusToggle === focusKey) {
      refocusToggle = null;
      ref.current?.focus();
    }
  });
  const label = pinned ? t.office.pins.unpin : t.office.pins.pin;
  const stop = (e: { stopPropagation: () => void }) => e.stopPropagation();
  return (
    <button
      ref={ref}
      type="button"
      className={`chat-pins__toggle${pinned ? " chat-pins__toggle--on" : ""}`}
      aria-pressed={pinned}
      aria-label={label}
      title={label}
      data-testid={testId}
      onClick={(e) => {
        e.stopPropagation();
        if (document.activeElement === e.currentTarget) refocusToggle = focusKey;
        onToggle();
        // Nothing remounted (the write was ignored): do not let a later mount
        // of this row steal focus.
        queueMicrotask(() => {
          if (refocusToggle === focusKey) refocusToggle = null;
        });
      }}
      // Keep the row's chat entry and its drag sensors out of it.
      onKeyDown={stop}
      onMouseDown={stop}
      onTouchStart={stop}
    >
      <PinIcon size={14} />
    </button>
  );
}

function SortableRow({
  id,
  canActivate,
  render,
}: {
  id: string;
  canActivate: () => boolean;
  render: (row: ChatPinRow) => ReactNode;
}) {
  const { t } = useI18n();
  const s = useSortable({
    id,
    attributes: { roleDescription: t.office.pins.roleDescription },
  });
  return (
    <>
      {render({
        ref: s.setNodeRef,
        style: { transform: CSS.Translate.toString(s.transform), transition: s.transition },
        className: `chat-pins__row${s.isDragging ? " chat-pins__row--placeholder" : ""}`,
        attributes: s.attributes,
        listeners: s.listeners,
        canActivate,
      })}
    </>
  );
}

/** One tab's chat list. With pins, two collapsible sections: 已置頂 (the first
 * `pinnedCount` ids, reorderable by long-press drag) and the rest. With
 * nothing pinned the list is just the plain rows, no headings. */
export function ChatPinList({
  ids,
  pinnedCount,
  nameOf,
  onReorder,
  renderRow,
  renderOverlay,
  className,
  testId,
  restTitle,
  unreadOf,
  collapsed,
  onCollapsedChange,
}: {
  ids: readonly string[];
  pinnedCount: number;
  nameOf: (id: string) => string;
  onReorder: (id: string, index: number) => void;
  renderRow: (id: string, row: ChatPinRow | undefined) => ReactNode;
  renderOverlay: (id: string) => ReactNode;
  className: string;
  testId?: string;
  /** Heading of the unpinned section (所有成員 / 所有外包). */
  restTitle: string;
  unreadOf: (id: string) => number;
  collapsed: ChatPinCollapsed;
  onCollapsedChange: (next: ChatPinCollapsed) => void;
}) {
  const { t, msg } = useI18n();
  const pinned = ids.slice(0, pinnedCount);
  const rest = ids.slice(pinnedCount);
  const [activeId, setActiveId] = useState<string | null>(null);
  // Enter both drops a keyboard drag and opens a row's chat; the row checks
  // this so a drop never also opens the chat.
  const draggingRef = useRef(false);
  const canActivate = () => !draggingRef.current;
  const titleId = useId();
  const pinnedSectionId = useId();
  const restSectionId = useId();

  // A newly pinned row must be seen landing, so a collapsed 已置頂 opens.
  const prevPinnedRef = useRef(pinnedCount);
  useEffect(() => {
    if (pinnedCount > prevPinnedRef.current && collapsed.pinned) {
      onCollapsedChange({ ...collapsed, pinned: false });
    }
    prevPinnedRef.current = pinnedCount;
  });

  const sensors = useSensors(
    useSensor(MouseSensor, { activationConstraint: LONG_PRESS }),
    useSensor(TouchSensor, { activationConstraint: LONG_PRESS }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
      // Enter stays the row's own "open this chat".
      keyboardCodes: { start: ["Space"], cancel: ["Escape"], end: ["Space", "Enter", "Tab"] },
    }),
  );

  const landing = (id: UniqueIdentifier, over: UniqueIdentifier) =>
    arrayMove(pinned, pinned.indexOf(String(id)), pinned.indexOf(String(over))).indexOf(
      String(id),
    );
  const name = (id: UniqueIdentifier) => nameOf(String(id));

  const announcements: Announcements = {
    onDragStart: ({ active }) => msg.pinDragPickedUp(name(active.id)),
    onDragOver: ({ active, over }) =>
      over ? msg.pinDragOverPinned(name(active.id), landing(active.id, over.id) + 1) : undefined,
    onDragEnd: ({ active, over }) =>
      over
        ? msg.pinDragDroppedPinned(name(active.id), landing(active.id, over.id) + 1)
        : msg.pinDragCancelled(name(active.id)),
    onDragCancel: ({ active }) => msg.pinDragCancelled(name(active.id)),
  };

  const finish = () => {
    draggingRef.current = false;
    setActiveId(null);
  };

  const heading = (
    key: keyof ChatPinCollapsed,
    label: string,
    sectionIds: readonly string[],
    controls: string,
    id?: string,
  ) => {
    const isCollapsed = collapsed[key];
    const unread = isCollapsed ? sectionIds.reduce((n, x) => n + unreadOf(x), 0) : 0;
    return (
      <button
        type="button"
        className="chat-pins__heading"
        id={id}
        aria-expanded={!isCollapsed}
        aria-controls={controls}
        data-testid={`chat-pins-heading-${key}`}
        onClick={() => onCollapsedChange({ ...collapsed, [key]: !isCollapsed })}
      >
        <span className="chat-pins__heading-label">
          {msg.pinSectionHeading(label, sectionIds.length)}
        </span>
        <ChevronDownIcon size={14} className="chat-pins__chevron" />
        {unread > 0 && (
          <span className="member-card__unread chat-pins__heading-unread">
            {unread > 99 ? "99+" : unread}
          </span>
        )}
      </button>
    );
  };
  const section = (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCenter}
      modifiers={[withinSection]}
      accessibility={{
        announcements,
        screenReaderInstructions: { draggable: t.office.pins.instructions },
      }}
      onDragStart={({ active }) => {
        draggingRef.current = true;
        setActiveId(String(active.id));
      }}
      onDragCancel={finish}
      onDragEnd={({ active, over }) => {
        if (over && active.id !== over.id) {
          onReorder(String(active.id), landing(active.id, over.id));
        }
        finish();
      }}
    >
      <div
        className="chat-pins__section"
        id={pinnedSectionId}
        role="group"
        aria-labelledby={titleId}
        data-testid="chat-pins-section"
      >
        <SortableContext items={pinned} strategy={verticalListSortingStrategy}>
          {pinned.map((id) => (
            <SortableRow
              key={id}
              id={id}
              canActivate={canActivate}
              render={(row) => renderRow(id, row)}
            />
          ))}
        </SortableContext>
      </div>
      <DragOverlay modifiers={[withinSection]}>
        {activeId !== null && (
          <div className="chat-pins__lifted" data-testid="chat-pins-lifted">
            <div className="chat-pins__wobble">{renderOverlay(activeId)}</div>
          </div>
        )}
      </DragOverlay>
    </DndContext>
  );

  if (pinnedCount === 0) {
    return (
      <div className={className} data-testid={testId}>
        {rest.map((id) => (
          <Fragment key={id}>{renderRow(id, undefined)}</Fragment>
        ))}
      </div>
    );
  }

  return (
    <div className={className} data-testid={testId}>
      {heading("pinned", t.office.pins.title, pinned, pinnedSectionId, titleId)}
      {!collapsed.pinned && section}
      {rest.length > 0 && heading("rest", restTitle, rest, restSectionId)}
      {!collapsed.rest && (
        <div className="chat-pins__section" id={restSectionId} data-testid="chat-pins-rest">
          {rest.map((id) => (
            <Fragment key={id}>{renderRow(id, undefined)}</Fragment>
          ))}
        </div>
      )}
    </div>
  );
}
