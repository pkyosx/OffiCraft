// T-b0bb — the WIRING of ConnectionBanner into the App tree.
//
// 🔴 WHY A WHOLE FILE FOR ONE LINE OF JSX. Review round 4 deleted
// `<ConnectionBanner />` from App.tsx and ran the entire frontend suite: 2475
// tests, all green. Only `tsc` complained, and only with TS6133 (unused
// import) — deleting the import too made even that go quiet. So half of what
// this ticket is FOR (「斷線這件事要看得見」) could be removed by a plausible
// tidy-up and nothing but the e2e would notice.
//
// That gap is a specific shape worth naming: ConnectionBanner.test.tsx pins
// what the component DOES, api/http.sse-recover.test.ts pins what the
// transport PUBLISHES, and between them sits an unguarded assumption that the
// two are connected to each other. A component nobody renders is a component
// that does nothing, however well it is tested in isolation. Unit tests that
// stop at the component boundary leave exactly this seam, and the e2e is too
// expensive and too far away to be the only thing standing on it.
//
// WHAT IS ASSERTED, AND WHY IT IS THE MINIMUM:
//   1. App renders ConnectionBanner AT ALL. That is the mutant above.
//   2. It sits ABOVE the tab strip. The position is a requirement, not taste —
//      the stall is app-wide, so the admission has to be on screen whichever
//      tab the owner is on rather than inside one page's body.
// Nothing here re-tests the banner's own behaviour; that is its file's job.
//
// MEASURED MUTANT (re-runnable by hand against App.tsx):
//   delete the `<ConnectionBanner />` element (and its now-unused import)
//     → reddens both tests in this file.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "./i18n";

// Same stubs as the sibling App tests: the count hooks and heavy page bodies
// have nothing to do with whether the banner is mounted.
vi.mock("./hooks/useChatUnread", () => ({ useChatUnread: () => 0 }));
vi.mock("./hooks/useReplyCardCount", () => ({ useReplyCardCount: () => 0 }));
vi.mock("./hooks/useTaskCount", () => ({ useTaskCount: () => 0 }));
vi.mock("./hooks/useOrgName", () => ({
  useOrgName: (fallback: string) => ({ orgName: fallback, setOrgName: () => {} }),
}));
vi.mock("./components/OfficePage", () => ({ OfficePage: () => null }));
vi.mock("./components/RepliesPage", () => ({ RepliesPage: () => null }));
vi.mock("./components/TasksPage", () => ({ TasksPage: () => null }));
vi.mock("./components/MonitorPage", () => ({ MonitorPage: () => null }));
vi.mock("./components/SettingsPage", () => ({ SettingsPage: () => null }));
vi.mock("./components/UserGuidePage", () => ({ GuidePage: () => null }));

// The banner is replaced by a MARKER on purpose. The real one renders `null`
// unless the stream has been down past its grace window, so asking the real
// DOM "is the bar there?" answers "no" in a healthy app whether or not App
// ever mounted it — the question would be unanswerable through the rendered
// output. Standing in a marker turns "did App mount this component" into
// something the DOM can be asked directly, without reaching into React
// internals and without making the assertion depend on the banner's own logic.
vi.mock("./components/ConnectionBanner", () => ({
  ConnectionBanner: () => <div data-testid="connection-banner-mount" />,
}));
vi.mock("./hooks/useAutoReloadOnVersionChange", () => ({
  useAutoReloadOnVersionChange: vi.fn(),
}));

import App from "./App";
import { useAutoReloadOnVersionChange } from "./hooks/useAutoReloadOnVersionChange";

function renderApp() {
  return render(
    <I18nProvider>
      <App />
    </I18nProvider>,
  );
}

describe("App · ConnectionBanner wiring", () => {
  beforeEach(() => {
    history.replaceState(null, "", window.location.pathname);
  });

  it("mounts ConnectionBanner — without this line the whole visible half of T-b0bb is gone and no unit test says so", () => {
    const { getByTestId } = renderApp();
    expect(getByTestId("connection-banner-mount")).toBeTruthy();
    expect(useAutoReloadOnVersionChange).toHaveBeenCalled();
  });

  it("mounts it ABOVE the tab strip, so a stall is admitted whichever tab is open", () => {
    const { getByTestId, container } = renderApp();
    const banner = getByTestId("connection-banner-mount");
    const tabs = container.querySelector(".nav-tabs");
    expect(tabs, "the tab strip rendered").not.toBe(null);
    // DOCUMENT_POSITION_FOLLOWING: the tab strip comes AFTER the banner.
    expect(
      banner.compareDocumentPosition(tabs as Node) &
        Node.DOCUMENT_POSITION_FOLLOWING,
      "the banner must precede the tabs, not live inside a page body",
    ).toBeTruthy();
  });
});
