// T-2ca0 產物浮層 RWD guard story. The existing TaskCardArtifactsStory happens
// to let the 產物 badge WRAP to the start of a fresh line at ~x=14, so its
// anchor sits at the LEFT — a position where a left-anchored popover never
// overflows the right. That is why the pre-existing narrow-390 guard was a
// false-green for the reported bug. This story instead reproduces the real DOM
// (a full-width .task-card__badge-row) with the 產物 badge pushed hard to the
// RIGHT edge of the row on a single line — the owner's real condition (產物 is
// the rightmost badge). The OLD popover (absolute, relative to that far-right
// anchor, fixed width) then spills past the viewport's right edge and clips the
// 連結 tab; the fix reparents the popover's positioning context to the
// full-width, left-aligned badge-row and clamps its width, so it opens from the
// card's left edge and fits regardless of where the badge sits.
import { I18nProvider } from "../../src/i18n";
import { TaskArtifactsBadge } from "../../src/components/TaskArtifactsPopover";
import { WITH_ARTIFACTS } from "./taskFixtures";

export function TaskArtifactsRightEdgeStory() {
  return (
    <I18nProvider>
      {/* Real card / badge-row structure so the fix's narrow-screen rule
          (badge-row becomes the popover's positioning context) actually
          applies — a bare div would not carry .task-card__badge-row. */}
      <div className="task-card" style={{ width: "100%" }}>
        <div className="task-card__head">
          <div className="task-card__head-top">
            <div className="task-card__badge-row">
              {/* A flex spacer eats the row so the badge is shoved to the far
                  right on a single line — its anchor is pinned at the right
                  edge, the condition that made the old popover overflow. */}
              <span style={{ flex: 1 }} />
              <TaskArtifactsBadge
                task={WITH_ARTIFACTS}
                onHydrate={(async () => WITH_ARTIFACTS) as never}
                onRemoveArtifact={undefined}
              />
            </div>
          </div>
        </div>
      </div>
    </I18nProvider>
  );
}
