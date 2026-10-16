# 架构

Next.js Web 同源转发 `/api/v1/*` 到 Go 标准库 HTTP API；PostgreSQL 是全部学习、账户和配置的权威来源。Redis 预留，当前不依赖它实现队列或会话。

Go 模块按认证/MFA、管理员配置、词库导入、偏好、复习队列、答题/评级/撤销、AI 供应商、辅导对话、训练、TTS 与导出分文件。internal/learning 只适配官方 FSRS/判分/队列；internal/security 实现密码、加密、CSRF 派生与 TOTP；internal/vocabulary 解析四字段文本。

认证中间件查不透明会话，构建用户上下文并验证 CSRF、初始改密、管理员角色。业务资源通过上下文 user_id 校验所有者，不接受传入用户替代身份。权限组查询每次由 users → group_models → ai_models → ai_providers 完成，密钥只在服务端解密。

学习状态写入先锁本人 users 行，再以短事务维护日志、FSRS 和撤销快照。用户之间没有共享 demo 身份。AI 请求用短事务预留幂等编号，网络调用在事务之外进行，完成后事务保存历史及响应。相同对话并发请求被拒绝，避免历史越序。请求失败/供应商超时可重试；进程崩溃的 running 预留三分钟后可重取，外部计费不宣称 exactly-once。

AI 文本使用 Chat Completions JSON 结构，语音使用 Audio Speech MP3 字节流。自定义传输拒绝私有 DNS 地址、代理和重定向，维持 HTTPS 校验；客户端看不到 API Key。生成训练先验证字段与原文依据，再保存并输出。未交卷听力不输出原文及答案，音频由服务端取原文合成。

Web 保留 Glass 风格、深色/减少动效/窄屏支持，新增登录、管理、设置、词库和 AI 页面。身份保存在 HttpOnly Cookie；CSRF 仅存内存。lib/study.ts 和 lib/ai-state.ts 分别解析学习与 AI 待确认请求，恢复记录按账户隔离；sessionStorage 只保存学习请求，不保存 API Key 或密码。文本用 React 转义渲染，模型代码不执行；公式仅由关闭trust的KaTeX生成转义后的数学标记。

CI 包含 Go 与真实 PostgreSQL 17 隔离 schema 测试、前端状态/TypeScript/build、带登录的完整 Compose 冒烟。模拟供应商检验权限、上下文、结构和缓存，不调用真实付费模型。改动保留在审查分支，不自动合并或部署。

Learning新增的多学科、题目/答案PDF、动态讲解协议、工作队列、个人API及独立Codex入库服务见[LEARNING](LEARNING.md)。UI新增六科首页、`/practice`、`/challenge`和`/admin/learning`；原词汇学习在`/english`。
