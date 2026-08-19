// Story — the reinstall-over-a-live-warden confirm (Monitor §2, server-self row
// while ONLINE). The new copy this ticket adds is a long, consequence-naming
// paragraph, and the ONE thing that has to hold for it is that it carries no
// colour of its own: every value comes from the theme token layer, so a theme
// pack that repaints the app (a light pack, say) repaints this dialog too.
//
// HONEST CAVEAT (same shape as MonitorTableLongTokenStory): this mirrors
// MonitorPage.tsx's dialog markup by hand rather than mounting <MonitorPage/>,
// which would need the API seam. The class names are the contract between the
// two, and MonitorPage.install-online.test.tsx pins that the real dialog
// renders `mon-confirm__title` / `mon-confirm__body` — so if the page's markup
// drifts away from what is measured here, that unit test goes red.
import "../../src/components/monitor.css";
import "../../src/styles/global.css";

export const CONFIRM_TITLE = "確認在伺服器上重新安裝";
export const CONFIRM_BODY =
  "「本機」目前在線上,已經有一個正在服役的 warden。再安裝一次會直接覆蓋它:" +
  "這台機器上的成員會全部斷線,而且此動作不可逆 —— 被覆蓋掉的 warden 無法還原," +
  "只能重新安裝並讓成員重新上線。";

export function MonitorBootstrapConfirmStory() {
  return (
    <div
      className="mon-confirm"
      data-testid="mon-bootstrap-confirm"
      role="dialog"
      aria-modal="true"
    >
      <div className="mon-confirm__box">
        <div className="mon-confirm__title" data-testid="confirm-title">
          {CONFIRM_TITLE}
        </div>
        <p className="mon-confirm__body" data-testid="confirm-body">
          {CONFIRM_BODY}
        </p>
        <div className="mon-confirm__actions">
          <button type="button" className="btn btn--ghost">
            取消
          </button>
          <button
            type="button"
            className="btn btn--danger-ghost"
            data-testid="confirm-btn"
          >
            覆蓋並重新安裝
          </button>
        </div>
      </div>
    </div>
  );
}
