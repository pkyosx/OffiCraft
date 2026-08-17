// CT global styles — load the REAL app CSS so the guards measure production
// layout, not a mock. The whole point of these tests is that the stylesheet is
// under test; a fixture that skipped these imports would silently measure the
// browser default and report a layout that never ships (exactly the trap the
// tasks.css:724 comment warns about — a probe page that doesn't load the sheet
// measures flex-basis:auto and reports the UNFIXED layout).
import "../src/styles/theme.css";
import "../src/styles/global.css";
import "../src/components/tasks.css";
import "../src/components/office.css";
// T-55ad: the chat markdown code block inherits its in-block scroll + width
// clamp from `.doc-md pre { max-width: 100%; overflow-x: auto }`, which lives in
// settings.css (app-wide in production). The chat-no-hscroll guard asserts that
// wide code keeps its OWN horizontal scroll, so the guard must measure with this
// sheet loaded too — else it measures a browser-default <pre> that never ships.
import "../src/components/settings.css";
// T-ee17: the reply card's 任務資訊 row is measured by
// reply-task-title-truncate.ct.spec.tsx, and the whole thing under test —
// which element gives way when the task title is too long — lives in
// replies.css. Without this sheet that guard measures an unstyled row, where
// nothing shrinks and nothing is clipped, and reports a layout that never
// ships.
import "../src/components/replies.css";
// T-49fb: the artifact-popover overflow guard measures the card at its REAL
// x-offset, which comes from `.app__main`'s 22px side padding — an app-shell
// rule that lives here. Without this sheet the card mounts ~22px further left,
// the popover's old over-wide box happens to fit, and the guard is a false
// green (the precise reason the pre-existing 390px guards missed the bug).
import "../src/components/chrome.css";
// The wake-snapshot panel's own sheet. The resume-chat-row-align guard measures
// `.mp-resume__chatrow` and its children against the production rules; without
// this import it would measure browser-default block layout, in which the body
// already sits at the left edge — a false GREEN on exactly the bug it is for.
import "../src/components/member-detail.css";
