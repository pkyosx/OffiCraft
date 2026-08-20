import { useEffect, useRef, useState } from "react";
import { useI18n } from "./i18n";
import { useHashRoute } from "./lib/hashRoute";
import {
  RefreshIcon,
  GearIcon,
  ChevronDownIcon,
  OfficeIcon,
  InboxIcon,
  TasksIcon,
  MonitorIcon,
  FileTextIcon,
} from "./components/icons";
import { Avatar } from "./components/Avatar";
import { BrandLogo } from "./components/BrandLogo";
import { NavIcon } from "./components/NavIcon";
import { OfficePage } from "./components/OfficePage";
import { RepliesPage } from "./components/RepliesPage";
import { TasksPage } from "./components/TasksPage";
import { MonitorPage } from "./components/MonitorPage";
import { GuidePage } from "./components/UserGuidePage";
import { SettingsPage } from "./components/SettingsPage";
import { ProfileDropdown } from "./components/ProfileDropdown";
import { OnboardingBanner } from "./components/OnboardingBanner";
import { InlineEdit } from "./components/InlineEdit";
import { PushNotifications } from "./components/PushNotifications";
import { useOrgName } from "./hooks/useOrgName";
import { useOwnerName } from "./hooks/useOwnerName";
import { useReplyCardCount } from "./hooks/useReplyCardCount";
import { useChatUnread } from "./hooks/useChatUnread";
import { useTaskCount } from "./hooks/useTaskCount";
import "./components/chrome.css";

type Tab = "office" | "replies" | "tasks" | "monitor" | "guide";

// Which peer the office was last left on. Browser-local by nature (it is this
// browser's last position, not studio state), so it stays out of the server and
// out of the hash. Every access is guarded: Safari's private mode and 3rd-party
// storage blocking make localStorage throw, and losing the memory must degrade
// to "open the roster", never to a blank console.
const LAST_OFFICE_CHAT_KEY = "oc_last_office_chat";

function readLastOfficeChat(): string | undefined {
  try {
    return window.localStorage.getItem(LAST_OFFICE_CHAT_KEY) || undefined;
  } catch {
    return undefined;
  }
}

function writeLastOfficeChat(id: string | undefined): void {
  try {
    if (id) window.localStorage.setItem(LAST_OFFICE_CHAT_KEY, id);
    else window.localStorage.removeItem(LAST_OFFICE_CHAT_KEY);
  } catch {
    /* storage unavailable — the in-memory ref still covers this session */
  }
}

export default function App({ onLogout }: { onLogout?: () => void } = {}) {
  const { t } = useI18n();
  // The studio name is server-backed (T-d693); the localized dict string is the
  // fallback until the fetch lands / when the owner has not named the studio.
  const { orgName, setOrgName } = useOrgName(t.orgName);
  // The owner nickname is server-backed too (T-0b41), so the topbar pill syncs
  // across devices; t.user is the fallback until the fetch lands / when unset.
  const { ownerName: userName, setOwnerName } = useOwnerName(t.user);
  // No 備份健康 indicator in the topbar (T-5e71, owner 2026-08-02): the backup
  // verdict now lives in Settings › 系統更新與備份 next to the software update
  // it belongs with. The topbar carries no status lights.
  // The browser-tab title tracks the studio name so it matches the org name the
  // owner sets in the topbar (owner ask: "Can title align with our org name").
  // orgName already resolves to t.orgName when the server value is empty/unloaded
  // (see useOrgName), so the fallback flows through here for free. index.html's
  // static <title> is only the pre-mount / pre-auth first paint.
  useEffect(() => {
    document.title = orgName;
  }, [orgName]);
  // Navigational state (page tab / settings overlay / member selections) lives
  // in the URL hash — a refresh (incl. the top-bar reload button) restores the
  // same view, and every view is deep-linkable. See lib/hashRoute.ts.
  const [route, setRoute] = useHashRoute();
  const tab: Tab =
    route.page === "monitor"
      ? "monitor"
      : route.page === "guide"
        ? "guide"
        : route.page === "replies"
          ? "replies"
          : route.page === "tasks"
            ? "tasks"
            : "office";
  // The 等我回覆 nav badge: how many reply cards are WAITING (answered never
  // counts). Live via the count endpoint + "reply_card" SSE deltas. A separate
  // signal from the per-member chat unread red dot (different clearing rules —
  // they never merge).
  const replyCount = useReplyCardCount();
  // The 辦公室 nav unread badge: TOTAL chat unread across every peer (> 0 → a
  // red count pill, >99 → "99+"; the same recipe as the 等我回覆/任務 badges).
  // Live via /api/chat/unread-count + "chat" / "chat_read" SSE deltas. A
  // separate signal from the 等我回覆 waiting-card badge (different clearing
  // rules — they never merge).
  const chatUnread = useChatUnread();
  // The 任務 nav badge: how many tasks are OPEN (non-terminal; 已完成/終止
  // never count — spec §1). Live via /api/tasks/count + "task" SSE deltas.
  const taskCount = useTaskCount().open;
  // The gear opens Settings as an OVERLAY route (#settings); clicking a nav
  // tab navigates back to that tab.
  const settingsOpen = route.page === "settings";
  // Bump on every gear click so SettingsPage re-mounts (its internal sub-view
  // resets to the landing page) — clicking the gear ALWAYS returns to Settings
  // home, even when already inside a Settings sub-page.
  const [settingsNonce, setSettingsNonce] = useState(0);

  // 辦公室 remembers the chat you were last in, so leaving for another tab and
  // coming back does not drop you on the roster (owner 2026-08-20:「切換分頁的
  // 時候可以固定住最後的對話視窗」). Only the peer id is remembered — NOT msgId
  // / composeSeed / the member-detail overlay, which are one-shot intents
  // (locate this message, seed the composer with a task no) and would re-fire
  // on every return.
  //
  // It is kept in localStorage, not just in memory, because the console is also
  // embedded as a conductor tab whose <iframe src> is the bare console URL and
  // is UNMOUNTED on every conductor tab switch (owner 2026-08-20:「在 Conductor
  // 切換 iframe 的時候能夠記住嗎?」). A remount is a cold page load, so an
  // in-memory ref would already be gone by the time the office renders.
  //
  // Two refs, deliberately: `bootOfficeChatRef` is the value as it stood at
  // mount and is never written again, because the recorder effect below fires
  // BEFORE the boot effect on a bare load (effects run in declaration order)
  // and would blank the live ref with this load's empty chatId — the restore
  // would then have nothing left to restore.
  const bootOfficeChatRef = useRef<string | undefined>(readLastOfficeChat());
  const lastOfficeChatRef = useRef<string | undefined>(
    bootOfficeChatRef.current,
  );
  useEffect(() => {
    if (route.page !== "office") return;
    lastOfficeChatRef.current = route.chatId;
    writeLastOfficeChat(route.chatId);
  }, [route.page, route.chatId]);

  // Cold load straight onto the bare console URL (the conductor iframe, or a
  // bookmark of the root) → reopen the remembered chat. Scoped to an EMPTY hash
  // on the FIRST render only: an explicit "#office" is the owner asking for the
  // roster, and every other deep link owns its own destination.
  const bootRestoredRef = useRef(false);
  useEffect(() => {
    if (bootRestoredRef.current) return;
    bootRestoredRef.current = true;
    if (window.location.hash === "" && bootOfficeChatRef.current) {
      lastOfficeChatRef.current = bootOfficeChatRef.current;
      setRoute({ page: "office", chatId: bootOfficeChatRef.current });
    }
    // Boot-only by design — deliberately not re-run when the route changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function selectTab(next: Tab) {
    // Restoring is for a tab SWITCH. Clicking 辦公室 while already inside it
    // keeps its existing meaning — reset to the roster, the only way to close
    // a chat on mobile — instead of re-opening what you just closed. The test
    // is on route.page, not `tab`: the Settings overlay (#settings) resolves to
    // tab "office" via that chain's fallback, and returning from Settings is a
    // switch like any other.
    if (next === "office" && route.page !== "office" && lastOfficeChatRef.current) {
      setRoute({ page: "office", chatId: lastOfficeChatRef.current });
      return;
    }
    setRoute({ page: next });
  }

  const [profileOpen, setProfileOpen] = useState(false);
  const profileMenuRef = useRef<HTMLDivElement>(null);

  // Close the profile dropdown on outside click (ref wraps pill + menu).
  useEffect(() => {
    if (!profileOpen) return;
    function onDown(e: MouseEvent) {
      if (!profileMenuRef.current?.contains(e.target as Node)) {
        setProfileOpen(false);
      }
    }
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [profileOpen]);

  return (
    <div className="app">
      <header className="topbar">
        <div className="topbar__brand">
          {/* The studio logo is the site-wide HOME link: clicking it clears the
              hash back to root (default office view). The org NAME next to it
              stays an InlineEdit — only the mark itself navigates. */}
          <button
            type="button"
            className="topbar__logo"
            aria-label={t.nav.home}
            title={t.nav.home}
            onClick={() => setRoute({ page: "office" })}
          >
            <BrandLogo size={20} />
          </button>
          <InlineEdit
            value={orgName}
            onCommit={setOrgName}
            placeholder={t.orgName}
            ariaLabel={t.profile.rename}
            displayClassName="topbar__org"
          />
          {/* No version chip here (T-e9d1 round 3, owner final): the topbar
              shows no build identity — it lives in Settings › 系統更新與備份 only,
              as the unified v<yymmdd>-<hhmm>-<shortsha> label. */}
        </div>

        <div className="topbar__actions">
          <button
            className="icon-btn"
            type="button"
            aria-label="refresh"
            onClick={() => window.location.reload()}
          >
            <RefreshIcon size={16} />
          </button>
          <PushNotifications />
          <button
            className={`icon-btn${settingsOpen ? " icon-btn--active" : ""}`}
            type="button"
            aria-label="settings"
            aria-pressed={settingsOpen}
            onClick={() => {
              setRoute({ page: "settings" });
              setSettingsNonce((n) => n + 1);
            }}
          >
            <GearIcon size={16} />
          </button>
          <div className="profile-menu" ref={profileMenuRef}>
            <button
              className="profile-pill"
              type="button"
              aria-haspopup="menu"
              aria-expanded={profileOpen}
              onClick={() => setProfileOpen((o) => !o)}
            >
              <span className="profile-pill__avatar">
                <Avatar size={20} kind="owner" />
              </span>
              <span className="profile-pill__name">{userName}</span>
              <ChevronDownIcon size={15} className="profile-pill__chevron" />
            </button>
            <ProfileDropdown
              open={profileOpen}
              onClose={() => setProfileOpen(false)}
              onLogout={onLogout}
              userName={userName}
              setOwnerName={setOwnerName}
            />
          </div>
        </div>
      </header>

      {/* T-ba62: the ONLY surface on which a fresh install can read WHY the
          automatic first-run setup did not produce a working studio. Renders
          nothing at all unless that run actually failed. */}
      <OnboardingBanner />

      <nav className="nav-tabs">
        <div className="nav-tabs__seg">
          <button
            type="button"
            className={`nav-tab${
              !settingsOpen && tab === "office" ? " nav-tab--active" : ""
            }`}
            onClick={() => selectTab("office")}
          >
            <NavIcon tabKey="office" fallback={<OfficeIcon size={15} />} />
            <span>{t.nav.office}</span>
            {chatUnread > 0 && (
              <span className="nav-tab__badge" data-testid="office-unread-badge">
                {chatUnread > 99 ? "99+" : chatUnread}
              </span>
            )}
          </button>
          <button
            type="button"
            className={`nav-tab${
              !settingsOpen && tab === "replies" ? " nav-tab--active" : ""
            }`}
            onClick={() => selectTab("replies")}
          >
            <NavIcon tabKey="replies" fallback={<InboxIcon size={15} />} />
            <span>{t.nav.replies}</span>
            {replyCount > 0 && (
              <span className="nav-tab__badge" data-testid="replies-badge">
                {replyCount > 99 ? "99+" : replyCount}
              </span>
            )}
          </button>
          <button
            type="button"
            className={`nav-tab${
              !settingsOpen && tab === "tasks" ? " nav-tab--active" : ""
            }`}
            onClick={() => selectTab("tasks")}
          >
            <NavIcon tabKey="tasks" fallback={<TasksIcon size={15} />} />
            <span>{t.nav.tasks}</span>
            {taskCount > 0 && (
              <span className="nav-tab__badge" data-testid="tasks-badge">
                {taskCount > 99 ? "99+" : taskCount}
              </span>
            )}
          </button>
          <button
            type="button"
            className={`nav-tab${
              !settingsOpen && tab === "monitor" ? " nav-tab--active" : ""
            }`}
            onClick={() => selectTab("monitor")}
          >
            <NavIcon tabKey="monitor" fallback={<MonitorIcon size={15} />} />
            <span>{t.nav.monitor}</span>
          </button>
          {/* 使用說明 — LAST tab, immediately right of 監控 (owner 2026-07-22:
              「user guide 改放在 tab 中,監控的右邊,不要放在 settings 裡」).
              It used to be a settings sub-page; a first-run owner had to open
              the gear to find out how the product works, which is the wrong
              place for the one page that explains the product. No badge: the
              docs are baked into the binary, so there is no count to show. */}
          <button
            type="button"
            className={`nav-tab${
              !settingsOpen && tab === "guide" ? " nav-tab--active" : ""
            }`}
            onClick={() => selectTab("guide")}
          >
            <NavIcon tabKey="guide" fallback={<FileTextIcon size={15} />} />
            <span>{t.nav.guide}</span>
          </button>
        </div>
      </nav>

      <main className="app__main">
        {settingsOpen ? (
          // A #settings/manuals/<key> deep-link opens straight on that manual's
          // hub (T-e987 任務類型 label 跳轉). Keyed on the manual key too so a
          // deep-link navigation re-mounts SettingsPage on the right initial
          // view; the gear's settingsNonce bump still returns to the landing.
          <SettingsPage
            key={`${settingsNonce}:${route.manualKey ?? ""}:${
              route.settingsRoles ? "roles" : ""
            }:${route.settingsRolesNew ? "new" : ""}:${route.roleKey ?? ""}`}
            initialManualKey={route.manualKey}
            initialRoles={route.settingsRoles}
            initialRolesCreate={route.settingsRolesNew}
            initialRoleKey={route.roleKey}
          />
        ) : tab === "office" ? (
          <OfficePage />
        ) : tab === "replies" ? (
          <RepliesPage replyCardId={route.replyCardId} />
        ) : tab === "tasks" ? (
          <TasksPage />
        ) : tab === "guide" ? (
          <GuidePage />
        ) : (
          <MonitorPage />
        )}
      </main>
    </div>
  );
}
