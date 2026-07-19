# API 錯誤 envelope — 統一 wire 錯誤形狀

**Status**: 定案(owner Seth 已裁:換掉原框架隱性 `{"detail": …}`)。
**實作**:Go server `server/ocserverd/`(`server.go` 統一錯誤寫出 + `api_helpers.go` 422/400 分流)· wire 宣告在凍結 `spec/openapi.json`(每 route `422`/`4XX`/`5XX` → ErrorEnvelope)· FE 消費 `frontend/src/api/client.ts::ApiError` · 黑箱回歸 `conformance/test_error_envelope.py`。(原 Python 實作已退役——tag `py-final`;本文描述的 wire 契約不變。)

## WHY

原實作(FastAPI)的預設錯誤體 `{"detail": …}` 是框架副產品、不是契約,而且**不是單一形狀**:
handler 錯誤時 `detail` 是字串,request-validation(422)時
`detail` 是 error object 的 **list**。client 永遠讀不到一個穩定形狀,
FE 只好放棄讀 body、退化成從 thrown message 字串 regex 出 status。envelope 把
錯誤面升級為顯式 wire 契約。

## 形狀(唯一,422 也走)

```json
{"error": {"code": "<機器可讀 snake_case>", "message": "<人讀文字>"}}
```

- **`code`**:機器可讀、snake_case、閉集語彙(見下表)。由 HTTP status 單一映射
  推導——handler 只給 status + message,錯誤寫出統一摺進 envelope;要新 code 加進
  映射表,不在 handler 內散寫字串。
- **`message`**:人讀文字 = 原本的 `detail` 字串(語意逐條保留)。
  request-validation 錯誤被摺成一條人讀字串(deterministic)。500 兜底 message
  固定 `"internal server error"`——內部細節永不上 wire(traceback 留 server log)。
- **不帶 `fields`**:掃過 FE 全部消費面(client.ts / mock.ts / isHttpStatus /
  各 component),**沒有任何地方逐欄消費 validation 錯誤**——極簡優先,先不加;
  未來 FE 真要逐欄呈現時再擴充 optional `fields`(加欄 = optional,DTO 相容範本)。

## code 語彙表(閉集)

| status | code |
|---|---|
| 400 | `validation_error`(現有 400 raise 點全是輸入形狀拒絕:base64/型別/大小,語意上與 422 同類) |
| 401 | `unauthorized` |
| 403 | `forbidden` |
| 404 | `not_found` |
| 405 | `method_not_allowed` |
| 409 | `conflict` |
| 422 | `validation_error`(handler 顯式 422 與 RequestValidationError 同碼) |
| 503 | `service_unavailable` |
| 其他 5xx | `internal_error`;其他未映射 4xx | `bad_request` |

## 覆蓋面(no escape)

錯誤路徑全數走同一個寫出:handler 錯誤的全部 4xx/5xx、框架自發 404/405、
param-bind 失敗的 422、未處理 crash 的 500 兜底——一律 envelope。

**MCP 面免費對齊**:tools/call 是 in-process loopback,錯誤 body 原樣進
`structuredContent` —— tool error 同樣是 envelope 形狀。

## OpenAPI / codegen

凍結 `spec/openapi.json` 對每條 route 宣告 `422` + `4XX` + `5XX` → ErrorEnvelope,
只描述一種錯誤形狀(FE `npm run gen:api` 同源重生)。

## FE 消費契約(`frontend/src/api/client.ts`)

- middleware 於非 2xx **讀 envelope body**,throw `ApiError`:
  `{ status: number, code: string, serverMessage: string }`。
  `Error.message` **保形不變**:`http <status> for <METHOD> <path>`(歷史契約,
  client.test.ts 釘住;body 讀不到 envelope 時 code/serverMessage honest-empty `""`)。
- 讀 status 一律走 `err instanceof ApiError && err.status === n`
  (`SettingsPage.tsx::isHttpStatus`,deleteRole 409 防線),不再 regex message;
  regex 保留為 non-ApiError 的 fallback。mock(`api/mock.ts`)throw 同一個
  `ApiError` 保持 mock ↔ http 行為 parity。

## 測試

`conformance/test_error_envelope.py`(黑箱):401/403/404/405/409/400/422 代表性
錯誤路徑逐類發射,斷言 body 恰為 `{error:{code,message}}`、code 符合閉集、
無 legacy `{"detail":…}` 殘留。
