# 使用者操作契約清單

這份清單的每一條 `sentence` 都只描述一個外部可觀察的「使用者動作 → 結果」。
`scope` 是明確的畫面集合；集合中的每一個畫面都必須在同一條目列出一個具名
e2e 斷言，不能把涵蓋範圍留給讀者推測。`surface_set=reply-card-body-callers`
表示 scope 不是手數出來的：守衛會反向掃 production tree 中所有直接 import
`ReplyCardBody` 的 caller，或經 re-export barrel 到達的 rendered caller；barrel 本身不算畫面，再和
[`user-operation-contract-surfaces.json`](user-operation-contract-surfaces.json)
逐項比對。`ruling` 是 owner 裁定卡的出處，`evidence` 只列出目前產品說明中
可重讀的行。

任何既有條目內容（包括句子、scope、surface_set、斷言綁定與出處）改動，都必須把該
條目的 `ruling=` 值改成不同的新 owner 裁定；只移動 `evidence=` 行號不算新裁定。
新增條目則必須在加入的 metadata 行帶上 owner 裁定。守衛會把 metadata、sentence、
每個 assertion 與結尾標記組成完整 block，按真正 PR base（`merge-base`）到 `HEAD`
的每個 committed snapshot 逐段比對，並另比對 working tree 與 `HEAD`；因此中間 commit
後再疊無關 commit，也不能把未裁定的語意反轉藏起來。守衛也會把每個 scope 對到 e2e
裡的 `UOC_ASSERT` marker。

<!-- user-operation-contract: id=UOC-RC-SINGLE-TAP scope=replies-page,chat-page,tasks-page surface_set=reply-card-body-callers ruling=rc-b4184fcbcde8 evidence=docs/guide/glossary.md:45,docs/guide/quickstart.md:95,docs/design/SPEC.md:319 -->
- sentence: 單選請示卡在請示列表頁、聊天頁與任務頁展開後的內嵌請示卡上，點一下選項就直接送出，不需要第二次按送出。
- assertion: screen=replies-page marker=single_option_tap_answers_on_replies_page
- assertion: screen=chat-page marker=single_option_tap_answers_in_chat
- assertion: screen=tasks-page marker=single_option_tap_answers_on_tasks_page
<!-- /user-operation-contract -->

<!-- user-operation-contract: id=UOC-RC-SINGLE-DRAFT scope=replies-page,chat-page,tasks-page surface_set=reply-card-body-callers ruling=rc-b4184fcbcde8 evidence=docs/guide/quickstart.md:97,docs/design/SPEC.md:253,docs/design/SPEC.md:319 -->
- sentence: 單選請示卡在請示列表頁、聊天頁與任務頁展開後的內嵌請示卡上，點一下選項時，已打在框裡的字會跟著同一次送出，不會被丟掉。
- assertion: screen=replies-page marker=single_option_keeps_draft_on_replies_page
- assertion: screen=chat-page marker=single_option_keeps_draft_in_chat
- assertion: screen=tasks-page marker=single_option_keeps_draft_on_tasks_page
<!-- /user-operation-contract -->
