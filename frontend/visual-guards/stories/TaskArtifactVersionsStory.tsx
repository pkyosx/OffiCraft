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
    // T-92: the LIVE row carries `name` + `description` only — `filename`,
    // `isImage` and `attachmentId` left the wire and are derived by the
    // popover (mime prefix, url tail). The retained VERSION rows below still
    // carry all three, which is not an oversight: a version's name is stored,
    // not derived, so it can be empty and the old fields stay its facts.
    name: "交付說明.txt",
    description: "",
    mime: "text/plain",
    createdTs: 1753776180,
    createdBy: "mira",
    // T-57 — the blob's own name, and it has to sit in the BASE literal rather
    // than being left to `over`: a field that only ever arrives through
    // `Partial<TaskArtifactView>` types as `string | undefined`, which is the
    // error this fixes. The value is the same `.txt` the stub serves under
    // `text/plain`, so the two facts about these bytes agree. Note the versions
    // modal reads the LIVE side under `name`, not this (see its own comment),
    // so this value is not what task-artifact-versions.ct.spec.tsx binds to —
    // it is what keeps the row honest for the popover's `blobFilename`.
    filename: "交付說明.txt",
    // Left at 2 deliberately: `VersionsButton` renders only above 1, and
    // task-artifact-versions.ct.spec.tsx reaches the whole reader through
    // `getByTestId("task-artifact-versions-ta-file")` and then asserts the
    // 「2版」 chip is wider than 26px and unclipped. Drop this to 1 and there is
    // no entry to click.
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
    name: "PR #2",
    description: "",
    mime: "text/uri-list",
    // A link carries none — without this override it would inherit the base
    // row's `.txt`, which is simply not true of this row's bytes.
    filename: "",
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
      name: "交付說明.txt",
      description: "",
      filename: "交付說明.txt",
      // T-57 — this version's OWN facts, matched to what the story's stubbed
      // fetch actually answers for `/api/chat/attachment/att-old`:
      // `text/plain; charset=utf-8`. That agreement is what keeps
      // task-artifact-versions.ct.spec.tsx on its TEXT path, where
      // `ta-versions-content-text` exists and `.ta-versions__body` has 60 lines
      // to scroll (its 「wide」 test asserts `bodyScrolled > 0`).
      // ⚠️ Measured, not assumed: `loadArtifactPayload` decides from the
      // RESPONSE's content-type plus the name, never from this field — so this
      // value is honest bookkeeping rather than the thing the guard binds to.
      mime: "text/plain",
      isImage: false,
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
      name: "PR #1",
      description: "",
      filename: "",
      // T-57 — a LINK version is the documented asymmetry (adapter.ts): the
      // server resolves only the uri-list's BYTES into `url` and leaves
      // mime/filename/isImage empty, so "" / false is this row's real shape,
      // not a placeholder. `filename: ""` above was already spelled that way.
      mime: "",
      isImage: false,
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
        {/* ⚠️ `artifacts: []` and `onHydrate` are GONE (c867f432): the badge now
          * takes ONLY `{ id, artifactCount }` and does its own fetching — the card
          * carries no artifact rows to hand it (T-66/T-92) and there is no hydrate
          * hook to inject. Both are unexpressable, not merely unused, so there is
          * nothing left here to keep in sync. The popover still reads the SAME
          * seeded mock task, so this story measures exactly what it did before. */}
        <TaskArtifactsBadge task={{ id: "t-art", artifactCount: ARTIFACTS.length }} />
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
