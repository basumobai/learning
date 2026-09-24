# API v1（第一阶段）

所有端点位于 `/api/v1`；当前身份是仅本机可用的示例用户，不是生产认证 API。

| 方法 | 路径 | 响应 |
| --- | --- | --- |
| GET | `/health` | `{"status":"ok"}` |
| GET | `/dashboard` | `display_name,due,studied,demo_words` |
| POST | `/study/sessions` | 创建 15 题，返回 `id,question_count` |
| GET | `/study/sessions/{id}` | 题目列表；未答题不暴露答案 |
| POST | `/study/sessions/{id}/answers` | `result`, 可选 `expected` |

答题请求：`{"question_id":1,"request_id":"任意唯一且至少 8 字符的字符串","answer":"采用","response_time_ms":2400,"used_hint":false}`。`request_id` 应为每次答题生成 UUID；网络重试必须复用原值及其余字段。已答题改答案返回 HTTP 409。无效请求返回 400，找不到题目返回 404。`pending` 不返回参考答案。不同题共用 request ID 返回 409。

注意：此阶段的 Session 仍使用固定示例题顺序；优先级调度及批量提交尚未实现。
