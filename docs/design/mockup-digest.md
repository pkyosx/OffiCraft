# Mockup 設計摘要 — Seth 互動原型（視覺權威基準）

> 來源：`seth-mockup.html`（19MB，client-rendered JS app，title "Bundled Page"）。
> 渲染路徑：**Playwright + Chromium 實測導覽**（逐畫面 click 截圖）＋ Chrome headless 對照。
> 所有文案／版面＝**render 實測**；顏色 hex＝從 **rendered computed style 實測**（rgb→hex）。
> ⚠️ 已知：載入時有一支 top-level JS error `stopHeaderEdit is not defined`（畫面底部紅條），但不影響任何畫面 render 或導覽點擊。
> ⚠️ 每個畫面 body 文字最上方都有一個「iPhone 預覽」字串（原型內建的手機預覽標籤，desktop 版面照常渲染，可忽略）。

---

## 🔑 最關鍵結論：語言／狀態詞用哪一套？

**Mockup 本尊預設用「中文那一套」——喚醒 / 離線 / 線上，不是 Activate / Inactive。**

- 動作鈕文字＝**「喚醒」**（成員面板）／**「喚醒 Mira」**（聊天空態）／**「聊聊」**（成員卡）。**不是** Activate / Chat。
- 狀態徽章＝**「離線」**（灰色 outline badge）。**不是** Inactive。
- 成員列標題＝**「辦公室成員 · 1」**。**不是** Members · 1 / Core members。
- 角色＝**「助理」**。**不是** Liaison。
- 站名＝**「AI 工作室」**。**不是** AI Office。使用者＝**「CEO（你）」**。

> bundle 內同時含中英兩套 i18n 字串（`Activate`×14、`喚醒`×30…），但**偏好子頁的語言切換預設停在「中文」**（見 mockup-preferences.png，中文鈕為 active 態），所以預設 render 全中文。English 是可切換的第二語言。
>
> ➡️ **對 LIVE 的意義**：LIVE 目前顯示 Activate/Inactive/Members/Core members/Liaison＝英文那套；而權威 mockup 預設是中文那套。若要對齊 mockup，LIVE 預設語言／文案應改回中文（喚醒/離線/辦公室成員·N/助理）。
>
> ⚠️ 只有「離線」狀態能實測（開箱 Mira 離線）。**「喚醒中」「線上」徽章文字無法由 render 驗證**（需真的喚醒成員才會出現；bundle 內 `線上`×8 存在，但 `喚醒中` 連續字串 0 次—waking 態實際用詞待 LIVE 端確認）。

---

## 視覺 Token（computed style 實測，rgb→hex）

| Token | 值 | 來源 |
|---|---|---|
| 頁面背景 | `#191C24` (rgb 25,28,36) | 根 wrapper bg |
| 卡片 / 邊框 | `#242832` (rgb 36,40,50) | logo tile / 卡片 border |
| 主要文字 | `#E7E8EE` (rgb 231,232,238) | body color |
| 標題／強調白 | `#F1F2F6` (rgb 241,242,246) | 站名 18px/600 |
| 次要／灰字 | `#9AA0AD` (rgb 154,160,173) | 副標、離線徽章 |
| **主色 綠（薄荷/emerald）** | **`#6FD6B0` (rgb 111,214,176)** | 喚醒鈕文字/border、聊聊鈕文字、logo 綠 |
| 導航 active（靛藍） | `#2C3350` (rgb 44,51,80) | 「辦公室」active tab bg，radius 10px |
| 待命/離線狀態點 | `#4A5060` (rgb 74,80,96)，⌀9px 圓 | 成員卡狀態點（灰） |
| 圓角 | 卡片 ~11px、tab 10px、徽章 pill | — |
| 字體 | `"Schibsted Grotesk", "Noto Sans TC", system-ui` | body font-family |

> **主色是綠（薄荷 #6FD6B0），不是藍。** 靛藍 `#2C3350` 只用於「導航 active tab」與送出鈕，屬次要 UI 色。
> 狀態點色（實測僅離線灰 #4A5060）；線上綠點、喚醒中黃點無法實測——但依 spec 綠=線上、黃=喚醒中，且 monitor 帳號卡實測有綠點(正常)/紅點(過熱)。

---

## 逐畫面

### 1. Office 有成員卡（首頁）— `mockup-office.png`
- **版面**：頂列（左 logo+「AI 工作室」+鉛筆 ｜ 右：重新整理⟳、齒輪⚙、「CEO（你）▾」）；下方 tab「辦公室 / 監控」（辦公室 active，靛藍 bg）；主體左＝成員列、右＝對話區。
- **左欄**：標題「**辦公室成員 · 1**」。成員卡：頭像+灰狀態點、名字「Mira」、「**離線**」灰 outline 徽章、角色「助理」、「上次上線 8 分鐘前」、右側綠色「**聊聊**」按鈕。
- **右欄（對話區，成員離線態）**：頂部「Mira ⌄ / 助理 · 上次上線 8 分鐘前」+右上綠色「**喚醒**」鈕；中央離線空態：🌙 icon +「**Mira 目前離線**」+「**這位成員目前不在線上，喚醒後才能開始對話。**」+ 綠色「**喚醒 Mira**」鈕；底部輸入框 placeholder「**回覆 Mira…**」+ 送出鈕。
- 註：spec 的空態文字「這個範圍還沒有訊息」在**成員離線時不出現**（改顯示上述離線 placeholder）；「這個範圍還沒有訊息」應是成員線上但無訊息時的文字（本 render 未達）。

### 2. Office 空態
- 開箱本來就有 Mira 一位成員，**無真正的「零成員空態」畫面**可 render。最接近的空態＝右側「對話區離線 placeholder」（見上）。

### 3. 成員詳情面板 — `mockup-member-detail.png`
- **版面**：頂部「‹ 返回」；身分卡（頭像｜名字「Mira」+「**MB-1IF746**」徽章+改名鉛筆｜「助理 · 上次上線 8 分鐘前」+灰狀態點｜右側綠「**喚醒**」鈕）。
- **資訊卡 1**（左右兩欄）：左「模型 / **claude-sonnet-4.5**」；右「**EFFORT · 思考強度** / **中 (medium)**」。
- **資訊卡 2 「運行狀況」**（右上角小字「**待命中**」）：左「🧠 context / —」+右側「**重新聚焦**」鈕（待命時 disabled 灰）；右「💲 估計$ / —」。
- **資訊卡 3 「終端 · TMUX」**：程式碼框「`$ tmux attach -t mira`」+右側「**複製指令**」鈕；下方說明「在你自己的終端機貼上執行，即可接上這個成員的 session。」
- **可展開列**：「📄 **初始 PROMPT · 下次喚醒／聚焦生效** ⌄」、「≋ **過往學習經驗 · 下次喚醒／聚焦生效** ⌄」。

### 4. Monitor 三區 — `mockup-monitor.png`
- **區1「帳號資訊」**：每帳號一卡。①「●seth_wang · Seths-MacBook-Pro」右「估計 $13.93」；「5 小時窗 · 用量 0% · 時間 32.8%」進度條；「7 天窗 · 用量 96% · 時間 95.4%」(黃條)。②「●eva · Mac」(紅點) 右「估計 $314.10」；「5 小時窗 · 用量 8% · 時間 62.8%」；「7 天窗 · 用量 54% · 時間 40.7% **· 過熱**」(紅條)。狀態點：綠=正常、紅=過熱。
- **區2「機器資訊」**（表格）：欄「機器 ｜ 帳號 ｜ 🖥 CPU ｜ 🎚 RAM ｜ 🔋 電量 ｜ 🔌 電源」。列：Seths-MacBook-Pro ｜ seth_wang ｜ 18.4% ｜ 52.1% ｜ 100% ｜ 🔌。
- **區3「AI 會話」**（表格）：欄「成員 ｜ 機器 ｜ 帳號 ｜ 模型 ｜ 🧠 context ｜ 💲 估計$」。列：Mira(頭像+狀態點+「助理 · 上次上線 8 分鐘前」) ｜ Seths-MacBook-Pro ｜ seth_wang ｜ 「Sonnet 4.6」+「medium」灰徽章 ｜ 28% ｜ $4.20。

### 5. 設定 landing — `mockup-settings.png`
- 標題「**設定**」；兩個入口列「**軟體更新**」「**角色誌**」。

### 6. 軟體更新 — `mockup-software-update.png`
- 頂部「‹ 返回」；一張卡：「目前版本 / **v1.2.0**」+「**有新版**」靛藍徽章 + 右側「**升級到最新版**」鈕；下方「↑ **有可用的新版本 v1.3.0**」。

### 7. 角色誌 — `mockup-roles-log.png`
- 頂部「‹ 返回」。**區1「全域情境（GLOBAL CONTEXT）」**：一列「🌐 **全域情境** / global-context.md · 套用到所有角色」+右 ›。**區2「角色定義」**：角色卡「👤 **助理** / 協助你統籌、傳達指令、彙整回報」+右 ›。

### 8. 全域情境 — `mockup-global-context.png`
- ⚠️ 此檔實際截到的是**角色誌列表頁**（同 #7，click「全域情境」未成功進入詳情頁）。**全域情境詳情頁（markdown 檢視／編輯模式）未截到**——這是唯一漏截的畫面。依 spec 應為 markdown 檢視 +「編輯→取消編輯/重置/完成編輯」。

### 9. 角色詳情（助理）— `mockup-role-detail.png`
- 頂部「‹ 返回」；大標題「**助理**」。卡片：「**職責說明** / 協助你統籌、傳達指令、彙整回報」；分隔線下「📄 **role-assistant.md**」+右側「✏ **編輯**」鈕；下方 markdown 渲染（「助理」H1、內文、「職責」/「行為原則」小標 + bullet list）。

### 10. Profile 下拉 — `mockup-profile-dropdown.png`
- 點「CEO（你）▾」展開下拉：頂部「個人檔案 / **CEO（你）**」+改名鉛筆；「⚙ **偏好設定** / 名稱、外觀、語言」+右 ›；「⇥ **登出**」（紅色）。

### 11. 偏好子頁 — `mockup-preferences.png`
- 下拉內二級頁「‹ **偏好設定**」。「**主題**」：兩顆分段鈕「**辦公室**」(active) / 「**修仙**」。「**語言**」：「**中文**」(active) / 「**English**」。
- ⚠️ 注意主題選項是「辦公室 / 修仙」，語言預設 active＝「中文」→ 佐證預設中文。

---

## 截圖檔名清單（全部在 scratchpad 同目錄）
| 檔名 | 畫面 |
|---|---|
| `mockup-office.png` | Office 首頁（有成員卡+離線對話空態） |
| `mockup-member-detail.png` | 成員詳情面板 |
| `mockup-monitor.png` | Monitor 三區 |
| `mockup-settings.png` | 設定 landing |
| `mockup-software-update.png` | 軟體更新 |
| `mockup-roles-log.png` | 角色誌列表 |
| `mockup-global-context.png` | ⚠️＝角色誌列表（詳情頁未進去） |
| `mockup-role-detail.png` | 角色詳情（助理） |
| `mockup-profile-dropdown.png` | Profile 下拉 |
| `mockup-preferences.png` | 偏好子頁（主題/語言） |

## 誠實標註
- ✅ **實測 render**：全部 10 個畫面的版面/文案/顏色，皆由 Playwright+Chromium 實際導覽截圖 + computed style 讀取。
- ⚠️ **未截到**：全域情境**詳情頁**（markdown 檢視/編輯模式）——click 沒進去，僅到列表。
- ⚠️ **無法實測**：「喚醒中」「線上」狀態徽章文字與線上綠點/喚醒中黃點（開箱成員恆離線，未觸發喚醒）；Office 真正零成員空態（開箱固定有 Mira）。
- ❌ **無臆造**：所有 hex 皆 computed style 換算；未見到的文案一律標「未達/無法驗證」。
