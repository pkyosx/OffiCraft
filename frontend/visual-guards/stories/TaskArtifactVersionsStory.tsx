// CT story for the pinned deliverable's version reader (T-60) — the real modal,
// the real sheets, opened the way a user opens it: badge → row entry → reader.
//
// Three facts jsdom cannot see are staged here:
//   ① the reader really OVERLAYS the task page (stacking + hit testing).
//   ② WIDE — the version list and the content sit SIDE BY SIDE, and a long
//      version scrolls inside the reader's own body while the page does not
//      scroll in either direction.
//   ③ NARROW — the two panes STACK instead, the panel stays inside a 360px
//      viewport, and the 「N版」 entry chip is not squeezed to an icon square.
//
// The blob reads are answered by a stubbed `fetch` (the mock adapter has no blob
// store), declaring `text/plain` so the reader takes its TEXT path — the one
// whose content has to scroll.
import { useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { api } from "../../src/api";
import {
  __resetMock,
  __injectMockTask,
  __injectMockArtifactVersions,
} from "../../src/api/mock";
import { TaskArtifactsBadge } from "../../src/components/TaskArtifactsPopover";
import type { TaskArtifactView, TaskView } from "../../src/api/adapter";

/** 300 chars, no space, no hyphen — the shape a pasted token/URL takes. */
const LONG_TOKEN =
  "sha256:" +
  "0123456789abcdef".repeat(18) +
  "/twin(desired_state/desired_machine_id/refocus_since)";

const OLD_TEXT = [
  ...Array.from({ length: 60 }, (_, i) => `${i + 1}. 這一行是舊版產物的第 ${i + 1} 段。`),
  LONG_TOKEN,
].join("\n");
const NEW_TEXT = [
  ...Array.from({ length: 60 }, (_, i) =>
    i === 2 ? `${i + 1}. 這一行是新版產物的第 ${i + 1} 段(已改寫)。` : `${i + 1}. 這一行是舊版產物的第 ${i + 1} 段。`,
  ),
  LONG_TOKEN,
].join("\n");

const BLOBS: Record<string, string> = {
  "/api/chat/attachment/att-old": OLD_TEXT,
  "/api/chat/attachment/att-new": NEW_TEXT,
};

function artifact(over: Partial<TaskArtifactView>): TaskArtifactView {
  return {
    id: "ta-file",
    kind: "file",
    url: "/api/chat/attachment/att-new",
    label: "交付說明.txt",
    filename: "交付說明.txt",
    mime: "text/plain",
    isImage: false,
    attachmentId: "att-new",
    createdTs: 1753776180,
    createdBy: "mira",
    versionCount: 2,
    ...over,
  };
}

const ARTIFACTS = [
  artifact({}),
  artifact({
    id: "ta-link",
    kind: "link",
    url: "https://example.com/pr/2",
    label: "PR #2",
    filename: "",
    mime: "",
    attachmentId: "",
  }),
];

function seed() {
  __resetMock();
  __injectMockTask({
    id: "t-art",
    taskNo: "T-9001",
    title: "產物版本",
    status: "in_progress",
    artifacts: ARTIFACTS,
    artifactCount: ARTIFACTS.length,
    steps: [],
    deps: [],
  } as unknown as TaskView);
  __injectMockArtifactVersions("ta-file", [
    {
      id: 1,
      kind: "file",
      url: "/api/chat/attachment/att-old",
      label: "交付說明.txt",
      filename: "交付說明.txt",
      attachmentId: "att-old",
      createdTs: 1753689780,
      createdBy: "mira",
    },
  ]);
  __injectMockArtifactVersions("ta-link", [
    {
      id: 1,
      kind: "link",
      url: "https://example.com/pr/1",
      label: "PR #1",
      filename: "",
      attachmentId: "",
      createdTs: 1753689780,
      createdBy: "mira",
    },
  ]);
  const realFetch = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(typeof input === "string" ? input : input.toString()).split("?")[0]!;
    const text = BLOBS[path];
    if (text === undefined) return realFetch(input as RequestInfo, init);
    return new Response(text, {
      status: 200,
      headers: { "content-type": "text/plain; charset=utf-8" },
    });
  }) as typeof fetch;
}

export function TaskArtifactVersionsStory() {
  // Seeded once, BEFORE the first render — the popover hydrates on open.
  useState(() => {
    seed();
    return null;
  });
  return (
    <I18nProvider>
      {/* The page the reader has to cover — tall, with a target where the panel
        * lands. */}
      <div style={{ padding: 16 }} data-surface="page">
        <TaskArtifactsBadge
          task={{ id: "t-art", artifactCount: ARTIFACTS.length, artifacts: [] }}
          onHydrate={(id) => api.getTask(id)}
        />
        <div
          data-testid="page-behind"
          style={{ height: 1200, background: "var(--color-surface-sunken, #222)" }}
        >
          任務面
        </div>
      </div>
    </I18nProvider>
  );
}
