// CT story for the answered-card pointer in the resume summary.
//
// This mounts the REAL <ResumeSummaryCard> and only replaces its one network
// read. The pointer is deliberately long enough to wrap on a phone: the
// cockpit must keep the card id reachable without making the task section or
// the page scroll sideways. The light pack is the same kind of custom theme
// payload the product applies at runtime, so the new token-based block is
// exercised in both the built-in and a light palette.
import { useEffect } from "react";
import { I18nProvider } from "../../src/i18n";
import { ResumeSummaryCard } from "../../src/components/ResumeSummaryCard";
import { api } from "../../src/api";
import type { MemberResumeSummaryView } from "../../src/api/adapter";

export const RESUME_LIGHT_PACK: Record<string, string> = {
  "--color-bg": "#c2d492",
  "--color-card": "#fdfbf1",
  "--color-text": "#33301f",
  "--color-text-strong": "#1e1c10",
  "--color-text-muted": "#403d2c",
  "--color-topbar-bg": "rgba(179, 200, 134, 0.8)",
  "--color-nav-bg": "rgba(215, 207, 164, 0.8)",
  "--color-main-bg": "rgba(241, 234, 209, 0.8)",
  "--color-border": "#b0ae83",
  "--color-accent": "#2b450b",
  "--color-overlay": "#241f0d",
};

const RESUME: MemberResumeSummaryView = {
  identity: null,
  chat: [],
  tasks: [
    {
      id: "t-answered",
      taskNo: "T-245",
      title: "依 owner 回覆調整目前方案",
      typeKey: "tm-05f7c776d6ff",
      status: "in_progress",
      priority: "normal",
      waitingReason: "",
      currentStepId: "step-answered",
      currentStepName: "先讀 owner 回覆，再決定下一步",
      progressDone: 2,
      progressTotal: 4,
      updatedTs: 1787200000,
      detailChars: 0,
      answeredCardSteps: [
        {
          stepId: "step-answered",
          stepName: "先讀 owner 回覆，再決定下一步並保留可調整方案",
          cardId: "rc-answered-card-with-a-long-stable-id",
        },
      ],
    },
  ],
  overview: {
    chatCount: 0,
    chatChars: 0,
    tasksReturned: 1,
    tasksOpenTotal: 1,
    tasksDetailChars: 0,
    cardsWaiting: 0,
    cardsAnsweredRecent: 1,
    rosterChars: 0,
    machinesChars: 0,
    stepsOnAnsweredCard: 1,
    stepsOnAnsweredCardChars: 84,
  },
  note: "",
  generatedAt: "",
  chatEarlierOmitted: { omitted: false, hint: "" },
  roster: [],
  machines: null,
};

export function ResumeAnsweredCardStory({
  theme = "office",
}: {
  theme?: "office" | "light";
}) {
  // The component's lazy read is the only seam this story replaces. Keeping
  // the real component and real adapter shape makes the geometry guard cover
  // the production DOM rather than a copied task-row skeleton.
  api.getMemberResumeSummary = async () => RESUME;

  useEffect(() => {
    const root = document.documentElement;
    const previous = new Map(
      Object.keys(RESUME_LIGHT_PACK).map((key) => [
        key,
        root.style.getPropertyValue(key),
      ]),
    );
    if (theme === "light") {
      for (const [key, value] of Object.entries(RESUME_LIGHT_PACK))
        root.style.setProperty(key, value);
    } else {
      for (const key of Object.keys(RESUME_LIGHT_PACK))
        root.style.removeProperty(key);
    }
    document.body.style.background = "var(--color-bg)";
    return () => {
      for (const [key, value] of previous) {
        if (value) root.style.setProperty(key, value);
        else root.style.removeProperty(key);
      }
    };
  }, [theme]);

  return (
    <I18nProvider>
      <div className="app__main" style={{ padding: 12 }}>
        <div className="mp" style={{ height: 900 }}>
          <ResumeSummaryCard agentId="mira" />
        </div>
      </div>
    </I18nProvider>
  );
}
