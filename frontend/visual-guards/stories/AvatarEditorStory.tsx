// T-c826 browser fixture for the owner-only personal avatar editor. The four
// panels expose the states the owner sees without replacing the real component:
// empty, successful upload, in-flight upload, and API failure.
import { useState } from "react";
import type { ReactNode } from "react";
import { AvatarEditor } from "../../src/components/AvatarEditor";
import "../../src/components/member-detail.css";
import { I18nProvider } from "../../src/i18n";
import { MEMBER_IMG, OUTSOURCE_IMG } from "./avatarKindImages";

const neverSettles = () => new Promise<void>(() => {});

function Fixture({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <section
      style={{
        minWidth: 0,
        padding: 16,
        border: "1px solid var(--color-border)",
        borderRadius: "var(--radius-card)",
        background: "var(--color-card)",
      }}
    >
      <h2
        style={{
          margin: "0 0 12px",
          color: "var(--color-text-strong)",
          fontSize: 14,
          textAlign: "center",
        }}
      >
        {title}
      </h2>
      {children}
    </section>
  );
}

function SuccessEditor() {
  const [src, setSrc] = useState<string>();
  return (
    <AvatarEditor
      kind="member"
      src={src}
      onUpload={async () => setSrc(MEMBER_IMG)}
      onRemove={async () => setSrc(undefined)}
    />
  );
}

export function AvatarEditorStory() {
  return (
    <I18nProvider>
      <main
        data-testid="avatar-editor-matrix"
        style={{
          display: "grid",
          gridTemplateColumns: "repeat(auto-fit, minmax(150px, 1fr))",
          gap: 12,
          width: "100%",
          maxWidth: 920,
          margin: "0 auto",
          padding: 16,
        }}
      >
        <Fixture title="空白 / 上傳成功">
          <div data-testid="success-editor">
            <SuccessEditor />
          </div>
        </Fixture>
        <Fixture title="已有個人頭像">
          <div data-testid="existing-editor">
            <AvatarEditor
              kind="outsource"
              src={OUTSOURCE_IMG}
              onUpload={async () => {}}
              onRemove={async () => {}}
            />
          </div>
        </Fixture>
        <Fixture title="上傳處理中">
          <div data-testid="loading-editor">
            <AvatarEditor
              kind="member"
              onUpload={neverSettles}
              onRemove={async () => {}}
            />
          </div>
        </Fixture>
        <Fixture title="儲存失敗">
          <div data-testid="error-editor">
            <AvatarEditor
              kind="member"
              onUpload={async () => {
                throw new Error("fixture failure");
              }}
              onRemove={async () => {}}
            />
          </div>
        </Fixture>
      </main>
    </I18nProvider>
  );
}
