// CT story for T-33 round 4 — 合併的單一入口，在真的瀏覽器裡。
//
// 這支 story 只做三件事，每一件都是為了讓 lore-pending-merge.ct.spec.tsx 量得到
// 東西：
//
//   ① 把 `api.listPendingLoreEntities` 換掉。CT 沒有 vi.mock（jsdom 那支
//      LorePendingSection.test.tsx 用的是它），這個 repo 在 CT 這一側的慣例是
//      直接改 `api` 上的那一格 —— ScheduledMessagesClampStory / DiffUrlOverlay
//      Story / SoftwareUpdateStory 都是這樣寫的。改在 render 之前，元件掛載時
//      的那一次 fetch 就已經拿得到這些列。
//   ② 種**五個候選**，五種 reason 各一個（same_normalized / prefix /
//      substring / edit_distance_1 / edit_distance_2）。五種都要在，因為第二格
//      量的就是「每一個候選都帶著它自己的理由、而且那段理由放得下」——只種一種
//      reason 的話，最長的那一句可能根本沒被排進去。
//   ③ 讓「正文有多長」變成一個**可以轉的旋鈕**（`bodyGrow`）。
//
// 🔴 ③ 是這支 story 存在的第二個理由，而且它比 ①② 都重要。
// fixture 是我自己寫的，沒有任何東西規定它必須是今天資料庫裡的長度。一支只看過
// 短文字的守衛永遠是綠的，而它綠得跟「它會在長文字時叫」一模一樣。所以正文長度
// 在這裡是一個**輸入**：spec 用它做階梯，量出「多長會紅」，答案寫在 spec 的檔頭。
//
// `bodyGrow` 的作法是把一段 impact 句型接在真正的正文後面，而不是改元件、也不是
// 改 `zh.ts` 那一行 —— 元件讀的是 `t.lore.pendingMergeConfirmBody`，所以在 story
// 裡覆寫字典上的那個函式，等於「之後那張票把 problem 換成更長的 impact 句型」以
// 後畫面會長成的樣子，一個字都不用動產品碼。
import { I18nProvider } from "../../src/i18n";
import { zh } from "../../src/i18n/locales/zh";
import { en } from "../../src/i18n/locales/en";
import { api } from "../../src/api";
import { LorePendingSection } from "../../src/components/LorePendingSection";
import type { LorePendingEntityView } from "../../src/types";

/** 真正的正文函式，第一次載入時就先留一份 —— 覆寫是疊在它上面的，不是取代它。 */
const REAL_ZH_BODY = zh.lore.pendingMergeConfirmBody;
const REAL_EN_BODY = en.lore.pendingMergeConfirmBody;

/** 接在正文後面的那一段。刻意寫成「影響」的句型，因為之後那張票要換上去的就是
 * 這種句子；用一段沒有意義的 padding（「啊啊啊…」）量出來的門檻，會跟真文字的
 * 斷行行為對不起來（CJK 幾乎每個字都可以斷，但標點與括號不行）。 */
const IMPACT_TAIL =
  "這件事會往外擴：所有指到舊名字的記憶、所有引用過它的任務紀錄、以及之後任何一次" +
  "用舊名字做的搜尋，都會被重新導到新的名字底下，而導過去之後就沒有一條路可以把它" +
  "們分回來。";

/**
 * `bodyGrow` = 在真正的正文後面接幾段 IMPACT_TAIL。
 *   0 ⇒ 今天線上的正文，一個字都不多。
 *   n ⇒ 之後那張票（或再之後那張）可能長成的樣子。
 */
function installBody(bodyGrow: number) {
  // 小數也吃得下 —— 找門檻要的是連續的旋鈕，不是三段跳。
  const tail = IMPACT_TAIL.repeat(Math.ceil(bodyGrow)).slice(
    0,
    Math.round(IMPACT_TAIL.length * bodyGrow),
  );
  zh.lore.pendingMergeConfirmBody = (from: string, into: string) =>
    REAL_ZH_BODY(from, into) + tail;
  en.lore.pendingMergeConfirmBody = (from: string, into: string) =>
    REAL_EN_BODY(from, into) + tail;
}

/** 五個候選，五種 reason。名字是真的會出現的形狀：一個大小寫不同、一個多了尾
 * 巴、一個打錯一個字、一個打錯兩個、一個是整串被包在更長的 key 裡面。 */
const SIMILAR_REAL = [
  { entityId: "en-a", canonical: "repo:OffCraft-CLI", reason: "same_normalized" },
  {
    entityId: "en-b",
    canonical: "repo:offcraft-cli-toolkit",
    reason: "prefix",
  },
  {
    entityId: "en-c",
    canonical: "repo:hardcoretech/offcraft-cli-mirror",
    reason: "substring",
  },
  { entityId: "en-d", canonical: "repo:offcraft-cll", reason: "edit_distance_1" },
  { entityId: "en-e", canonical: "repo:0ffcraft-cl1", reason: "edit_distance_2" },
];

/** 同樣五種 reason，但名字長到不像話。第二格拿它當 mutant：候選名字變長的時候，
 * 那一列會不會被裁掉、會不會把面板撐破。 */
const SIMILAR_LONG = SIMILAR_REAL.map((s, i) => ({
  ...s,
  canonical:
    s.canonical +
    "-" +
    "very-long-monorepo-path-segment-that-nobody-would-shorten".slice(
      0,
      20 + i * 12,
    ),
}));

function rows(longNames: boolean): LorePendingEntityView[] {
  return [
    {
      entityId: "en-main",
      canonical: "repo:offcraft-cli",
      type: "repo",
      name: "offcraft-cli",
      createdTs: 1788330000,
      createdBy: "m-o197",
      entries: 2,
      entriesEver: 3,
      entryRefs: [
        {
          entryId: "le-1",
          trigger: "要動 offcraft 的 CLI 進入點的時候",
          status: "active",
        },
        {
          entryId: "le-2",
          trigger: "有人問 CLI 的旗標為什麼是這個順序",
          status: "underspecified",
        },
      ],
      similar: longNames ? SIMILAR_LONG : SIMILAR_REAL,
      sampleShort: "CLI 的進入點在 cmd/offcraft，不在 bin/。",
    },
    // 🔴 對照列：沒有候選 ⇒ 沒有那一顆合併鈕。少了它，第一格的「一列一顆」會跟
    // 「每一列都有一顆」分不開，而後者是另一件事（沒得挑的時候給一顆假出口）。
    {
      entityId: "en-lonely",
      canonical: "human:someone-nobody-resembles",
      type: "human",
      name: "someone-nobody-resembles",
      createdTs: 1788330001,
      createdBy: "",
      entries: 0,
      entriesEver: 0,
      entryRefs: [],
      similar: [],
      sampleShort: "",
    },
  ];
}

export function LorePendingMergeStory({
  bodyGrow = 0,
  longNames = false,
}: {
  bodyGrow?: number;
  longNames?: boolean;
}) {
  // 兩個覆寫都在 render 之前跑完：元件掛載時的第一次 fetch 就拿得到這些列，
  // 確認框第一次算 body 的時候字典也已經是要量的那一版。
  installBody(bodyGrow);
  const seeded = rows(longNames);
  api.listPendingLoreEntities = async () => seeded;

  // `.lore` 是這一塊在 LorePage 上真正的容器；沒有它，待審列會拿到整個 viewport
  // 的寬度，而「放不放得下」是相對於那個容器問的。
  return (
    <I18nProvider>
      <div className="lore">
        <LorePendingSection />
      </div>
    </I18nProvider>
  );
}
