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
