# API v1

同源路径 `/api/v1`，Next.js 转发到 Go API。当前身份为 demo，全部仅供本机。

| 接口 | 行为 |
|---|---|
| GET /health | API 与数据库健康 |
| GET /dashboard | 原有 due/studied/demo_words，新增 today、dimensions、scheduler、timezone |
| POST /study/sessions | 创建或恢复活动练习，body `{mode,types,limit,new_limit}`，可省略字段 |
| GET /study/sessions/active | `{session: Session \| null}`，含未评级反馈 |
| GET /study/sessions/{id} | questions 按 position 排序，含 result、rating、提交后的 answer/expected/example、评级预估 intervals |
| POST /study/sessions/{id}/answers | `{question_id,request_id,answer,response_time_ms,used_hint}`；判分并保留反馈，**此时不再自动调度** |
| POST /study/sessions/{id}/review | `{question_id,request_id,rating,resolved_result?}`；确认后调度，返回 next_review_at、schedule_applied |
| POST /study/sessions/{id}/undo | `{request_id}`；撤销最近答题并恢复原复习状态 |

`mode` 为 scheduled 或 practice。`types` 为 recognition / meaning_recall / spelling，不能重复，默认三种；limit 默认 20，范围 1–100；new_limit 默认 10，范围 0–50，明确传 0 表示只复习。new_limit 是本轮上限，不是每日预算。

创建成功 201，恢复已有练习 200（resumed:true）；空队列 200，id:null/question_count:0/message；不会写入空 Session。答案/评价/撤销重放 200，replayed:true；相同题目提交不同内容或请求冲突返回 409。输入无效 400，不存在 404，数据库错误 500 且服务端记录原因。JSON 不接受未知字段、多段值、超过 4096 字节的 body。

request_id 为 8–128 位字母/数字/下划线/连字符。答案最多 200 字符，耗时 0–3600000ms。评级 1–4；答错或有提示只能 1，pending 必须附 resolved_result:correct/wrong，其它答案禁止人工重写自动判分。已确认的评级只允许完全相同的请求重放。

升级注意：客户端判断题目完成必须用 rating 非空，不能只用 result 非空；submitted/pending 都可能处于核对页。expected 和 example 仅在提交后返回；待评的 intervals 使用当前时刻预估，到提交评级时由服务端重新计算。
