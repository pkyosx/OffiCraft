// T-48 guard fixtures. They live OUTSIDE the story module on purpose: the CT
// bundler rewrites imports out of a file that exports components, and a plain
// value imported alongside them collides ("Identifier … has already been
// declared"). Same reason `reassignRefusals.ts` / `identityPanelIds.ts` exist.

/** 一份真的出貨過的淺色 theme pack —— 與 ThemeContrastStory 的 LIGHT_PACK 同一
 * 份調色盤。不要「順手調整」這些值：一旦手調，這條 guard 就不再是在講一個使用者
 * 真的裝得起來的 theme。 */
export const LIGHT_PACK: Record<string, string> = {
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

/** 一段長到在任何視窗寬度都塞不下的訊息，而且帶真的換行 —— 預覽列拿到的 body 是
 * RAW 的（裁切歸 stylesheet 管），所以 fixture 必須真的含有它要活下來的東西，
 * 否則「兩行、高度固定」只量到裁切、量不到不折行。 */
export const LONG_BODY =
  "第一段\n\n第二段有很多空白的第二段   " +
  "這是一段很長的訊息內容，長到足以在任何視窗寬度下超出預覽列能容納的寬度，" +
  "用來確認它會被裁成一行而不是把版面撐開或把輸入框往下推。" +
  "它必須在 1280 也塞不下 —— 實測前一版只有 936px 寬，在 1280 的預覽列裡綽綽有餘，" +
  "於是「內容被裁掉」那條斷言在寬視窗下什麼都沒量到。";
/** 一個長到在 1280 也塞不下的寄件者名稱。display name 是 owner 自由輸入的文字，
 * 沒有上限 —— 而且這裡必須真的塞不下：名字塞得下的話，「兩行各自被裁、高度不變」
 * 就只是因為 fixture 剛好夠短，量不到任何東西。實測 36 字的
 * 「Eva Rhapsody Inbox (ow-8808ccf51794)」在 390 與 1280 都沒被裁。 */
export const LONG_WHO =
  "Eva Rhapsody Inbox (ow-8808ccf51794) 兼北境轉運站夜班聯絡人與週末待命窗口，" +
  "訊息請先看標題再決定要不要點進來";
