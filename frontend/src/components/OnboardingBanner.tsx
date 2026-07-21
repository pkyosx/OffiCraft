import { useEffect, useState } from "react";
import { useI18n } from "../i18n";
import { api, type OnboardingReportView } from "../api";
import "./onboarding.css";

/**
 * OnboardingBanner (T-ba62) — the ONE place a fresh install can read WHY it is
 * not working.
 *
 * After the owner sets the initial password the server automatically installs
 * this host's warden and wakes the seeded assistant. When that succeeds the
 * cockpit needs no banner: a live assistant IS the signal. When it does NOT,
 * everything the owner would otherwise see is a grey member and an offline
 * machine — the exact silence this ticket exists to remove. So the banner
 * renders ONLY for a non-ok report, and it leads with the step's REASON.
 *
 * Deliberately NOT rendered for `state === "running"`: a run in progress is not
 * a problem, and a banner that appears and then disappears on its own trains
 * the owner to ignore it.
 *
 * Dismissal is per-session (sessionStorage): the underlying condition is
 * durable, so a permanent dismissal would hide a still-broken studio forever,
 * while re-nagging on every render would be noise.
 */
const DISMISS_KEY = "oc.onboarding.dismissed";

export function OnboardingBanner() {
  const { t } = useI18n();
  const [report, setReport] = useState<OnboardingReportView | null>(null);
  const [dismissed, setDismissed] = useState(
    () => sessionStorage.getItem(DISMISS_KEY) === "1"
  );
  const [showDetail, setShowDetail] = useState(false);

  useEffect(() => {
    let live = true;
    void (async () => {
      try {
        const settings = await api.getServerSettings();
        if (live) setReport(settings.onboarding);
      } catch {
        // A settings read that fails is NOT evidence about onboarding — stay
        // silent rather than assert anything we do not know.
      }
    })();
    return () => {
      live = false;
    };
  }, []);

  if (dismissed || !report || report.state !== "failed") return null;
  const failed = report.steps.filter((s) => !s.ok);
  if (failed.length === 0) return null;

  const stepLabel = (name: string) =>
    name === "install_warden"
      ? t.onboarding.stepInstallWarden
      : name === "wake_assistant"
        ? t.onboarding.stepWakeAssistant
        : name;

  return (
    <div className="onboarding-banner" role="status" data-testid="onboarding-banner">
      <div className="onboarding-banner__head">
        <strong>{t.onboarding.titleFailed}</strong>
        <button
          type="button"
          className="onboarding-banner__dismiss"
          data-testid="onboarding-dismiss"
          onClick={() => {
            sessionStorage.setItem(DISMISS_KEY, "1");
            setDismissed(true);
          }}
        >
          {t.onboarding.dismiss}
        </button>
      </div>
      <p className="onboarding-banner__intro">{t.onboarding.intro}</p>
      <ul className="onboarding-banner__steps">
        {failed.map((s) => (
          <li key={s.name} data-testid={`onboarding-step-${s.name}`}>
            <span className="onboarding-banner__step">{stepLabel(s.name)}</span>
            {/* The REASON is the payload — a step name alone is the same
                silence with a label on it. */}
            <span className="onboarding-banner__reason">{s.reason}</span>
          </li>
        ))}
      </ul>
      {failed.some((s) => s.detail !== "") && (
        <>
          <button
            type="button"
            className="onboarding-banner__toggle"
            data-testid="onboarding-detail-toggle"
            onClick={() => setShowDetail((v) => !v)}
          >
            {showDetail ? t.onboarding.detailHide : t.onboarding.detailShow}
          </button>
          {showDetail && (
            <pre className="onboarding-banner__detail" data-testid="onboarding-detail">
              {failed
                .filter((s) => s.detail !== "")
                .map((s) => s.detail)
                .join("\n\n")}
            </pre>
          )}
        </>
      )}
    </div>
  );
}
