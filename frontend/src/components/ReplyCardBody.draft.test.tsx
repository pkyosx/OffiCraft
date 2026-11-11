import { describe, it, expect } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ReplyCardAnsweredBody, ReplyCardWaitingBody } from "./ReplyCardBody";
import type { ReplyCard, ReplyCardAnswerInput } from "../api/adapter";
import {
  getChatAttachError,
  replyCardAnswerDraftKey,
  saveChatDraftQuote,
  setChatAttachError,
} from "../lib/chatDraftStore";

function mkCard(over: Partial<ReplyCard> = {}): ReplyCard {
  return {
    id: "rc-1",
    from: "mira",
    kind: "decision",
    summary: "這批要走哪幾條線？",
    body: "",
    options: [
      { text: "走海運", aiPick: false },
      { text: "走空運", aiPick: false },
    ],
    selectMode: "multi",
    status: "waiting",
    attachments: [],
    createdTs: Date.now() / 1000 - 600,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
    ...over,
  };
}

function answeredCard(over: Partial<ReplyCard> = {}): ReplyCard {
  return mkCard({
    status: "answered",
    answeredTs: Date.now() / 1000 - 60,
    answer: {
      optionIdxs: [0],
      text: "",
      attachments: [],
      replyExcerpt: "原本引用的那段",
    },
    ...over,
  });
}

function txtFile(name: string): File {
  return new File(["hello"], name, { type: "text/plain" });
}

function renderWaiting(
  card: ReplyCard,
  onAnswer: (input: ReplyCardAnswerInput) => Promise<void> = async () => {},
) {
  const u = render(
    <I18nProvider>
      <ReplyCardWaitingBody card={card} onAnswer={onAnswer} />
    </I18nProvider>,
  );
  return {
    ...u,
    input: () => u.container.querySelector(".chat__input") as HTMLTextAreaElement,
    send: () => u.container.querySelector(".chat__send") as HTMLButtonElement,
    quote: () => u.queryByTestId("reply-composer-quote")?.textContent ?? null,
    files: () =>
      [...u.container.querySelectorAll(".chat__preview-file")].map(
        (e) => e.textContent,
      ),
    pick: (file: File) =>
      fireEvent.change(
        u.container.querySelector(".chat__file-input") as HTMLInputElement,
        { target: { files: [file] } },
      ),
  };
}

function renderAnswered(
  card: ReplyCard,
  onReanswer: (input: ReplyCardAnswerInput) => Promise<void> = async () => {},
) {
  const u = render(
    <I18nProvider>
      <ReplyCardAnsweredBody card={card} onReanswer={onReanswer} />
    </I18nProvider>,
  );
  return {
    ...u,
    input: () => u.container.querySelector(".chat__input") as HTMLTextAreaElement | null,
    quote: () => u.queryByTestId("reply-composer-quote")?.textContent ?? null,
  };
}

describe("ReplyCardWaitingBody", () => {
  it("restores the typed text, staged file and quote after the card is unmounted and mounted again", async () => {
    saveChatDraftQuote(replyCardAnswerDraftKey("rc-1"), "要引用的段落");
    const first = renderWaiting(mkCard());
    fireEvent.change(first.input(), { target: { value: "先寫一半" } });
    first.pick(txtFile("plan.txt"));
    await waitFor(() => expect(first.files()).toHaveLength(1));
    first.unmount();

    const second = renderWaiting(mkCard());
    expect(second.input().value).toBe("先寫一半");
    expect(second.files()).toEqual([expect.stringContaining("plan.txt")]);
    expect(second.quote()).toBe("引用原文要引用的段落");
  });

  it("keeps each card's draft to itself", () => {
    const a = renderWaiting(mkCard({ id: "rc-a" }));
    fireEvent.change(a.input(), { target: { value: "給 A 的回覆" } });
    a.unmount();

    const b = renderWaiting(mkCard({ id: "rc-b" }));
    expect(b.input().value).toBe("");
    b.unmount();

    const again = renderWaiting(mkCard({ id: "rc-a" }));
    expect(again.input().value).toBe("給 A 的回覆");
  });

  it("starts empty after a successful answer, even when the card unmounts before the answer settles", async () => {
    let settle: () => void = () => {};
    const first = renderWaiting(
      mkCard(),
      () => new Promise<void>((resolve) => (settle = resolve)),
    );
    fireEvent.change(first.input(), { target: { value: "送出這句" } });
    fireEvent.click(first.send());
    first.unmount();
    settle();
    await new Promise((resolve) => setTimeout(resolve, 0));

    const second = renderWaiting(mkCard());
    expect(second.input().value).toBe("");
  });

  it("drops a staging refusal once the last composer is gone, while the draft stays", async () => {
    const key = replyCardAnswerDraftKey("rc-1");
    const first = renderWaiting(mkCard());
    fireEvent.change(first.input(), { target: { value: "留著" } });
    setChatAttachError(key, "檔案太大");
    first.unmount();
    await Promise.resolve();

    expect(getChatAttachError(key)).toBeNull();
    expect(renderWaiting(mkCard()).input().value).toBe("留著");
  });

  it("keeps the typed text after a failed answer", async () => {
    const sent: ReplyCardAnswerInput[] = [];
    const first = renderWaiting(mkCard(), async (input) => {
      sent.push(input);
      throw new Error("offline");
    });
    fireEvent.change(first.input(), { target: { value: "送不出去" } });
    fireEvent.click(first.send());
    await waitFor(() => expect(sent).toHaveLength(1));
    await waitFor(() => expect(first.send().disabled).toBe(false));
    first.unmount();

    const second = renderWaiting(mkCard());
    expect(second.input().value).toBe("送不出去");
  });
});

describe("ReplyCardAnsweredBody", () => {
  it("reopens 重新決定 with the typed text and the quote after the card is unmounted and mounted again", () => {
    const first = renderAnswered(answeredCard());
    fireEvent.click(first.getByText("查看當初選項"));
    fireEvent.click(first.getByText("重新決定"));
    fireEvent.change(first.input()!, { target: { value: "改成這樣" } });
    first.unmount();

    const second = renderAnswered(answeredCard());
    expect(second.input()?.value).toBe("改成這樣");
    expect(second.quote()).toBe("引用原文原本引用的那段");
  });

  it("brings 重新決定 back when 查看當初選項 is collapsed and expanded again", () => {
    const u = renderAnswered(answeredCard());
    fireEvent.click(u.getByText("查看當初選項"));
    fireEvent.click(u.getByText("重新決定"));
    fireEvent.change(u.input()!, { target: { value: "改到一半" } });
    fireEvent.click(u.getByText("收合選項"));
    expect(u.input()).toBeNull();

    fireEvent.click(u.getByText("查看當初選項"));
    expect(u.input()?.value).toBe("改到一半");
  });

  it("keeps a removed quote removed when 重新決定 comes back", () => {
    const u = renderAnswered(answeredCard());
    fireEvent.click(u.getByText("查看當初選項"));
    fireEvent.click(u.getByText("重新決定"));
    fireEvent.click(u.getByLabelText("取消引用"));
    fireEvent.change(u.input()!, { target: { value: "不引用了" } });
    fireEvent.click(u.getByText("收合選項"));

    fireEvent.click(u.getByText("查看當初選項"));
    expect(u.input()?.value).toBe("不引用了");
    expect(u.quote()).toBeNull();
  });

  it("drops the 重新決定 draft when 取消 is pressed", () => {
    const first = renderAnswered(answeredCard());
    fireEvent.click(first.getByText("查看當初選項"));
    fireEvent.click(first.getByText("重新決定"));
    fireEvent.change(first.input()!, { target: { value: "算了" } });
    fireEvent.click(first.getByText("取消"));
    first.unmount();

    const second = renderAnswered(answeredCard());
    expect(second.input()).toBeNull();
    fireEvent.click(second.getByText("查看當初選項"));
    fireEvent.click(second.getByText("重新決定"));
    expect(second.input()?.value).toBe("");
    expect(second.quote()).toBe("引用原文原本引用的那段");
  });

  it("keeps the 重新決定 draft apart from the answer draft of the same card", () => {
    const waiting = renderWaiting(mkCard());
    fireEvent.change(waiting.input(), { target: { value: "作答草稿" } });
    waiting.unmount();

    const answered = renderAnswered(answeredCard());
    expect(answered.input()).toBeNull();
    fireEvent.click(answered.getByText("查看當初選項"));
    fireEvent.click(answered.getByText("重新決定"));
    expect(answered.input()?.value).toBe("");
  });
});
