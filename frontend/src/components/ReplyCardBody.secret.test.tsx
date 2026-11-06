// The shared interior of a `secret` reply card: the masked box in the option
// position, the untouched composer, the value's life in component state, and the
// faces a settled card shows.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { watchSecretValue } from "../test/secretValueWatch";
import {
  ReplyCardAnsweredBody,
  ReplyCardExpiredBody,
  ReplyCardWaitingBody,
} from "./ReplyCardBody";
import type { ReplyCard, ReplyCardSecretView } from "../api/adapter";

const SENTINEL = "dummy-aaaaaaaaaaaa";
const r = zh.replies;

function mkCard(over: Partial<ReplyCard> = {}): ReplyCard {
  return {
    id: "rc-1",
    from: "mira",
    kind: "secret",
    summary: "部署金鑰",
    body: "",
    options: [],
    selectMode: "single",
    status: "waiting",
    attachments: [],
    createdTs: 1000,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
    secret: { state: "waiting", providedTs: null, retrievedTs: null, expiresTs: null },
    ...over,
  };
}

function renderWaiting(props: Partial<Parameters<typeof ReplyCardWaitingBody>[0]> = {}) {
  const onProvideSecret = vi.fn(() => Promise.resolve());
  const utils = render(
    <I18nProvider>
      <ReplyCardWaitingBody
        card={mkCard()}
        onAnswer={() => Promise.resolve()}
        onProvideSecret={onProvideSecret}
        {...props}
      />
    </I18nProvider>,
  );
  return { ...utils, onProvideSecret };
}

afterEach(() => {
  vi.restoreAllMocks();
  document.body.innerHTML = "";
});

describe("the waiting box", () => {
  it("takes the option chips' place above the composer and leaves the composer as is", () => {
    const { getByTestId, queryAllByTestId, container } = renderWaiting();
    expect(queryAllByTestId("reply-option")).toEqual([]);
    const box = getByTestId("reply-secret-box");
    const composer = container.querySelector(".reply-composer")!;
    expect(composer).not.toBeNull();
    expect(box.compareDocumentPosition(composer) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(composer.querySelector("textarea")!.getAttribute("placeholder")).toBe(r.inputPlaceholder);
  });

  it("starts three lines tall, keeps line breaks, and the browser neither fills nor checks it", () => {
    const { getByTestId } = renderWaiting();
    const input = getByTestId("reply-secret-input") as HTMLTextAreaElement;
    expect(input.tagName).toBe("TEXTAREA");
    expect(input.className).toBe("secret-value-input__field secret-value-input__field--masked reply-secret__input");
    expect(input.getAttribute("rows")).toBe("3");
    expect(input.getAttribute("autocomplete")).toBe("off");
    expect(input.getAttribute("spellcheck")).toBe("false");
  });

  it("starts hidden, the eye shows and hides the value, and a cleared value hides again", async () => {
    const { getByTestId, onProvideSecret } = renderWaiting();
    const input = getByTestId("reply-secret-input") as HTMLTextAreaElement;
    const eye = getByTestId("secret-reveal-toggle");
    const shown = () => ({
      masked: input.classList.contains("secret-value-input__field--masked"),
      pressed: eye.getAttribute("aria-pressed"),
      label: eye.getAttribute("aria-label"),
      slashed: eye.innerHTML.includes("M3 3l18 18"),
    });
    expect(shown()).toEqual({ masked: true, pressed: "false", label: "顯示內容", slashed: true });
    fireEvent.change(input, { target: { value: SENTINEL } });
    fireEvent.click(eye);
    expect(shown()).toEqual({ masked: false, pressed: "true", label: "隱藏內容", slashed: false });
    expect(input.value).toBe(SENTINEL);
    expect(onProvideSecret).not.toHaveBeenCalled();
    fireEvent.click(eye);
    expect(shown()).toEqual({ masked: true, pressed: "false", label: "顯示內容", slashed: true });
    fireEvent.click(eye);
    fireEvent.click(getByTestId("reply-secret-send"));
    await waitFor(() => expect(input.value).toBe(""));
    expect(shown()).toEqual({ masked: true, pressed: "false", label: "顯示內容", slashed: true });
  });

  it("names its actions without naming the author", () => {
    const { getByTestId } = renderWaiting();
    expect(getByTestId("reply-secret-input").getAttribute("placeholder")).toBe("貼上密鑰");
    expect(getByTestId("reply-secret-send").textContent).toBe("送出密鑰");
    expect(getByTestId("reply-secret-box").textContent).toBe("送出密鑰");
  });

  it("the send button does nothing until there is a value", () => {
    const { getByTestId, onProvideSecret } = renderWaiting();
    expect((getByTestId("reply-secret-send") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(getByTestId("reply-secret-send"));
    expect(onProvideSecret).not.toHaveBeenCalled();
  });

  it("sends the exact value, multi-line included, and clears it on success", async () => {
    const { getByTestId, onProvideSecret } = renderWaiting();
    const input = getByTestId("reply-secret-input") as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: `${SENTINEL}\nline two\n` } });
    fireEvent.click(getByTestId("reply-secret-send"));
    await waitFor(() => expect(input.value).toBe(""));
    expect(onProvideSecret).toHaveBeenCalledWith(`${SENTINEL}\nline two\n`);
  });

  it("keeps the value on failure so the owner can retry, and logs none of it", async () => {
    const placesHoldingValue = watchSecretValue(SENTINEL);
    const onProvideSecret = vi.fn(() => Promise.reject(new Error("http 500")));
    const { getByTestId } = renderWaiting({ onProvideSecret });
    const input = getByTestId("reply-secret-input") as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: SENTINEL } });
    fireEvent.click(getByTestId("reply-secret-send"));
    await waitFor(() => expect(onProvideSecret).toHaveBeenCalledTimes(1));
    await waitFor(() => expect((getByTestId("reply-secret-send") as HTMLButtonElement).disabled).toBe(false));
    expect(input.value).toBe(SENTINEL);
    expect(placesHoldingValue()).toEqual([]);
  });

  it("the value stays in the box: no storage, console, global or composer draft", async () => {
    const placesHoldingValue = watchSecretValue(SENTINEL);
    const { getByTestId, container, onProvideSecret } = renderWaiting();
    fireEvent.change(getByTestId("reply-secret-input"), { target: { value: SENTINEL } });
    expect(placesHoldingValue()).toEqual([]);
    expect((container.querySelector(".reply-composer textarea") as HTMLTextAreaElement).value).toBe("");
    fireEvent.click(getByTestId("reply-secret-send"));
    await waitFor(() => expect(onProvideSecret).toHaveBeenCalled());
    expect(placesHoldingValue()).toEqual([]);
  });

  it("the watch used above names every place a value could land", () => {
    const placesHoldingValue = watchSecretValue(SENTINEL);
    const globals = globalThis as Record<string, unknown>;
    localStorage.setItem("oc_probe", `x${SENTINEL}x`);
    sessionStorage.setItem("oc_probe", SENTINEL);
    console.log({ value: SENTINEL });
    console.error(new Error(SENTINEL));
    globals.__secretProbe = { value: SENTINEL };
    try {
      expect(placesHoldingValue()).toEqual([
        "localStorage",
        "sessionStorage",
        "console.log",
        "console.error",
        "globalThis.__secretProbe",
      ]);
    } finally {
      localStorage.removeItem("oc_probe");
      sessionStorage.removeItem("oc_probe");
      delete globals.__secretProbe;
    }
  });

  it("takes focus only when the caller says the owner opened the card", () => {
    const first = renderWaiting({ autoFocusSecret: false });
    expect(document.activeElement).not.toBe(first.getByTestId("reply-secret-input"));
    first.unmount();
    const onSecretAutoFocused = vi.fn();
    const second = renderWaiting({ autoFocusSecret: true, onSecretAutoFocused });
    expect(document.activeElement).toBe(second.getByTestId("reply-secret-input"));
    expect(onSecretAutoFocused).toHaveBeenCalledTimes(1);
  });

  it("a click on the box's frame does not reach the card behind it", () => {
    const onCardClick = vi.fn();
    const { getByTestId } = render(
      <I18nProvider>
        <div onClick={onCardClick}>
          <ReplyCardWaitingBody
            card={mkCard()}
            onAnswer={() => Promise.resolve()}
            onProvideSecret={() => Promise.resolve()}
          />
        </div>
      </I18nProvider>,
    );
    fireEvent.click(getByTestId("reply-secret-box"));
    expect(onCardClick).not.toHaveBeenCalled();
  });

  it("other kinds keep their chips and show no box", () => {
    const { queryByTestId, getAllByTestId } = renderWaiting({
      card: mkCard({
        kind: "decision",
        secret: null,
        options: [{ text: "寄出", aiPick: true }],
      }),
    });
    expect(queryByTestId("reply-secret-box")).toBeNull();
    expect(getAllByTestId("reply-option")).toHaveLength(1);
  });
});

describe("the settled faces", () => {
  const now = Date.now() / 1000;
  const answered = (secret: ReplyCardSecretView, over: Partial<ReplyCard> = {}) =>
    mkCard({
      status: "answered",
      answeredTs: secret.providedTs,
      answer: { optionIdxs: null, text: "", attachments: [] },
      secret,
      ...over,
    });
  const renderAnswered = (card: ReplyCard) =>
    render(
      <I18nProvider>
        <ReplyCardAnsweredBody card={card} onReanswer={() => Promise.resolve()} />
      </I18nProvider>,
    );

  it("holding a value: reads as its state, without a time", () => {
    for (const [secret, want] of [
      [{ state: "provided", providedTs: now - 60, retrievedTs: null, expiresTs: now + 3600 }, "已提供"],
      [{ state: "retrieved", providedTs: now - 60, retrievedTs: now - 30, expiresTs: now + 570 }, "已取得"],
      [{ state: "retrieved", providedTs: now - 60, retrievedTs: now - 30, expiresTs: null }, "已取得"],
    ] as const) {
      const { getByTestId, queryByTestId, unmount } = renderAnswered(answered(secret));
      expect(getByTestId("secret-face").textContent).toBe(want);
      expect(queryByTestId("final-answer")).toBeNull();
      unmount();
    }
  });

  it("replied: the answer as usual, and re-decide is one press away", () => {
    const { getByTestId, getByText, queryByTestId, queryByText } = renderAnswered(
      answered(
        { state: "replied", providedTs: null, retrievedTs: null, expiresTs: null },
        { answer: { optionIdxs: null, text: "哪個環境?", attachments: [] } },
      ),
    );
    expect(getByTestId("final-answer").textContent).toContain("哪個環境?");
    expect(queryByTestId("secret-face")).toBeNull();
    expect(queryByText(r.viewOptions)).toBeNull();
    fireEvent.click(getByText(r.redecide));
    expect(queryByTestId("reply-secret-box")).toBeNull();
    expect(document.querySelector(".reply-composer")).not.toBeNull();
  });

  it("expired: no longer valid, with no options to review", () => {
    const { getByTestId, queryByTestId } = render(
      <I18nProvider>
        <ReplyCardExpiredBody
          card={mkCard({
            status: "expired",
            expiredTs: now,
            secret: { state: "expired", providedTs: now - 60, retrievedTs: null, expiresTs: null },
          })}
        />
      </I18nProvider>,
    );
    expect(getByTestId("secret-face").textContent).toContain("已失效");
    expect(queryByTestId("reply-option")).toBeNull();
  });
});

describe("what the owner reads", () => {
  it("names no developer term in either language", async () => {
    const { en } = await import("../i18n/locales/en");
    const texts = [
      ...Object.entries(zh.replies),
      ...Object.entries(zh.settings),
      ...Object.entries(en.replies),
      ...Object.entries(en.settings),
    ]
      .filter(([k]) => k.startsWith("secret"))
      .map(([, v]) => String(v));
    expect(texts.length).toBeGreaterThan(20);
    for (const text of texts) {
      expect(text).not.toMatch(/ciphertext|密文|vault|409|HTTP|encrypt|試驗/i);
    }
  });
});
