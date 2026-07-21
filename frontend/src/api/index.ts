// api/index.ts — the single swap point for the api client seam.
//
// The UI imports `api` from here and NOWHERE else imports mock/http. Flipping
// USE_MOCK (or wiring it to an env flag) swaps the whole backend with zero UI
// changes — that is the entire point of the seam.

import type { Api } from "./adapter";
import { mockApi } from "./mock";
import { httpApi } from "./http";

// M1 defaults to the mock adapter. Set VITE_USE_MOCK="false" at build time to
// swap the WHOLE backend to httpApi (real server) with zero UI changes — that is
// the point of the seam. Default (unset / any non-"false" value) stays mock, so
// the running :8770 deploy is unaffected until a build explicitly opts in.
export const USE_MOCK = import.meta.env.VITE_USE_MOCK !== "false";

export const api: Api = USE_MOCK ? mockApi : httpApi;

export type {
  Api,
  ChatMessage,
  MemberPatch,
  RolePatch,
  AliasPatch,
  OnboardOptions,
  ServerSettingsView,
  ServerSettingsPatch,
  OnboardingReportView,
  OnboardingStepView,
} from "./adapter";
