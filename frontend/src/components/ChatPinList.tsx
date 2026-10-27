import { Fragment, useRef, useState } from "react";
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
import { PinIcon } from "./icons";
import "./chat-pins.css";

// Same hold on mouse and touch (owner: like rearranging the iPhone home
// screen). Moving past the tolerance before the hold ends cancels it, so a
// press-and-move is a scroll / text gesture, never a drag.
const LONG_PRESS = { delay: 450, tolerance: 6 };
// A drag ends on mouseup, and the browser may still deliver the click that
// follows it to the row.
const CLICK_AFTER_DRAG_MS = 300;

/** Vertical only, and never out of the pinned section (the dragged row's
 * parent). */
const withinSection: Modifier = ({ transform, draggingNodeRect, containerNodeRect }) => {
  if (!draggingNodeRect || !containerNodeRect) return { ...transform, x: 0 };
  const minY = containerNodeRect.top - draggingNodeRect.top;
  const maxY = containerNodeRect.bottom - draggingNodeRect.bottom;
  return { ...transform, x: 0, y: Math.min(Math.max(transform.y, minY), maxY) };
};

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
  testId,
}: {
  pinned: boolean;
  onToggle: () => void;
  testId?: string;
}) {
  const { t } = useI18n();
  const label = pinned ? t.office.pins.unpin : t.office.pins.pin;
  const stop = (e: { stopPropagation: () => void }) => e.stopPropagation();
  return (
    <button
      type="button"
      className={`chat-pins__toggle${pinned ? " chat-pins__toggle--on" : ""}`}
      aria-pressed={pinned}
      aria-label={label}
      title={label}
      data-testid={testId}
      onClick={(e) => {
        e.stopPropagation();
        onToggle();
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

/** One tab's chat list. The first `pinnedCount` ids form a 已置頂 section that
 * can be reordered by long-press drag; the rest render as plain rows. With
 * nothing pinned the list is just the plain rows. */
export function ChatPinList({
  ids,
  pinnedCount,
  nameOf,
  onReorder,
  renderRow,
  renderOverlay,
  className,
  testId,
}: {
  ids: readonly string[];
  pinnedCount: number;
  nameOf: (id: string) => string;
  onReorder: (id: string, index: number) => void;
  renderRow: (id: string, row: ChatPinRow | undefined) => ReactNode;
  renderOverlay: (id: string) => ReactNode;
  className: string;
  testId?: string;
}) {
  const { t, msg } = useI18n();
  const pinned = ids.slice(0, pinnedCount);
  const rest = ids.slice(pinnedCount);
  const [activeId, setActiveId] = useState<string | null>(null);
  const draggingRef = useRef(false);
  const quietUntilRef = useRef(0);
  const canActivate = () =>
    !draggingRef.current && performance.now() > quietUntilRef.current;

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
    quietUntilRef.current = performance.now() + CLICK_AFTER_DRAG_MS;
    setActiveId(null);
  };

  return (
    <div
      className={className}
      data-testid={testId}
      onClickCapture={(e) => {
        if (!canActivate()) {
          e.preventDefault();
          e.stopPropagation();
        }
      }}
    >
      {pinnedCount > 0 && (
        <>
          <div className="chat-pins__title">
            <PinIcon size={12} />
            <span>{t.office.pins.title}</span>
          </div>
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
            <div className="chat-pins__section" data-testid="chat-pins-section">
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
          <div className="chat-pins__rule" data-testid="chat-pins-divider" />
        </>
      )}
      {rest.map((id) => (
        <Fragment key={id}>{renderRow(id, undefined)}</Fragment>
      ))}
    </div>
  );
}
