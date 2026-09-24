# 架构

浏览器 → Next.js（同域 `/api/v1` 转发）→ Go API → PostgreSQL。Redis 已列入 Compose，但第一阶段尚未承担任务队列，不把它加入答题路径。Go API 启动时按文件名顺序执行未应用的 SQL migration。

数据库是答题日志和派生复习状态的权威存储。`answer_logs` 原样保留；`review_states` 按 `(user_id,sense_id,dimension)` 维护。每次答题在一个事务中锁定 question、检查是否回答过、判分、记录原始答案、更新对应能力维度。相同 request ID 与完全相同的输入返回原结果；改变输入则拒绝。

部署是本机开发模式，Compose 仅把 Web/API 端口绑定到 `127.0.0.1`。不要将当前无认证体验用户部署到公网。

下一阶段：词库来源与导入、用户注册与权限；再把复习调度替换为 FSRS、建立异步 AI Gateway。性能指标须在部署环境测量，当前未承诺 P95。
