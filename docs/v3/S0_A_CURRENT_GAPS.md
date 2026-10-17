# A｜Learning v3.1 S0：现状与差距审计

> 2026-10-17；只读审计基线：`main@de6f5ad8325412027eca9b037113f4ebfdf32d76`。计划来源：[v3.1 Draft PR #13](https://github.com/basumobai/learning/pull/13)。本轮未连接生产数据库或付费 AI，也未本地运行代码测试。

## 1. 事实基线与 PR

- PR #11 已于 2026-10-17 合并；v2 P1–P8 的业务代码、迁移 `0008`–`0015`、测试与 `docs/v2/VALIDATION.md` 均已进入 main。旧 `docs/v2/IMPLEMENTATION_PLAN.md` 中“P1–P8 尚未实施”是其编写时的历史事实，**不是最新进度**。
- 开放 PR #13 为 v3.1 设计计划及许可证来源台账；开放 PR #15 是 Nginx 反代缓存修复。两者都不应自动合并。
- [main CI 39001147257](https://github.com/basumobai/learning/actions/runs/39001147257) 于 2026-10-17 成功。这只说明该工作流所覆盖的检查成功，**不等于真实教材、网盘、AI 计费、恢复演练全部验收**。
- 实际技术栈：Next.js/TypeScript；Go `net/http`；PostgreSQL 持久化任务和学习事实；Redis 通知；独立 Worker；已有 `go-fsrs/v4 v4.0.0`。不迁移 Gin、不重复造 FSRS 或任务队列。

## 2. v2 依赖去重

| v2 能力 | 本轮判断 | 代码/限制 |
| --- | --- | --- |
| P1 模型目录、授权与任务路由 | 已实现 | `model_catalog.go`、`model_routing.go`、`ModelScopeSelector.tsx`；真实模型能力和费用待测 |
| P2 管理后台与账户安全 | 已实现 | `admin_recovery.go`、`admin.go`、管理导航；仍需实际安全审计 |
| P3 原件、A/B/C、Worker/租约 | 已实现 | `0010_resource_workflow.sql`、`resource_store.go`、`resource_worker.go` |
| P4 有界抽样与智能分类 | 已实现 | `resource_classify.go`；真实扫描教材准确率待验证 |
| P5 选择性网盘导入 | 部分完成 | 已授权 OpenList 挂载目录；任意夸克/百度分享链接原生直读未实现 |
| P6 深度解析、断点恢复 | 已实现 | `0013_deep_analysis.sql`、`resource_deep.go`；真实跨页/公式效果待测 |
| P7 审核、正式发布、历史版本 | 已实现 | `0014_review_versions_rules.sql`、`resource_review.go`；迁移前已丢失版本不可恢复 |
| P8 多端与完整栈回归 | 已有测试 | `docs/v2/VALIDATION.md`、CI；真实设备/生产恢复仍待验收 |

## 3. v3.1 S0–S8 差距矩阵

| 阶段 | 状态 | 已有基础及主要缺口 |
| --- | --- | --- |
| S0 审计/合规 | 部分完成 | 本轮四份设计成果待审；根目录未见 `LICENSE`，须权利持有人确认后单独治理 |
| S1 评分/历史修订 | 部分完成 | 已有不可变 `learning_question_revisions` 与图片快照；缺结构化 `ScoringRuleRevision`、评分证据版本、同键不同载荷冲突处理 |
| S2 六科知识能力 | 部分/缺失 | `learning_terms` 和当前 `learning_question_terms` 已有；缺历史映射角色、`SkillEvidence`、`UserSkillState` |
| S3 错因与错题闭环 | 缺失 | 尚无可核实的候选错因→学生确认/更正→新题迁移验证持久化闭环 |
| S4 自主与智能强化 | 部分 | 自由选题和词汇 FSRS 已有；缺可回放推荐日志、跨科智能选题和每日计划 |
| S5 英语专项 | 部分 | 词汇、句子拆解、听力/广东听说入口已有；缺学生先交互的句法与新材料验证证据 |
| S6 AI 辅导/RAG | 部分 | `requestExplanation` 按需调用已有；缺正式资料引用回链、教学状态机、质量评测 |
| S7 广东整卷/音频 | 部分/待核实 | 分年份考试规则已有；未核实完整整卷快照、服务端正式计时及真实语音评分 |
| S8 PWA/恢复/多端 | 部分 | v2 多端和任务恢复已有；缺用户隔离弱网草稿、安全缓存、备份恢复演练 |

## 4. S1 关键事实与风险

1. `migrations/0004_learning_platform.sql` 定义 `learning_attempts`，含 `user_id`、`question_id`、`question_revision`、`request_id`、`answers`、`feedback`，`UNIQUE(user_id,request_id)` 约束在**创建练习**阶段生效。
2. `migrations/0014_review_versions_rules.sql` 已有不可变 `learning_question_revisions`、`learning_revision_assets`。旧练习通过 `(question_id,question_revision)` 加载题目；不能另造平行题目版本表。
3. `internal/platform/content.go` 的 `Solution.Rubric` 是自由文本；`learning.go:feedbackFor` 对客观题采用完整匹配才给分，当前**不支持按题目审核规则给多选部分分**；主观题依赖 AI 参考评价。
4. `submitLearningAttempt` 用行锁和 `submitted_at` 防重复交卷，但尚无独立提交载荷指纹、评分修订与不可变 `ScoreEvidence`。创建练习时复用相同 `request_id` 也未核对选择条件是否相同。
5. `learning_terms` 是知识点起点；当前 `learning_question_terms` 不保留映射历史角色。综合题失分不能直接令所有关联知识点归零。
6. 词汇 `answer_logs`、`review_states` 已有 FSRS 与撤销，不应与六科学科能力证据混写或重复计数。
7. `publicAttempt` 未交卷时移除 `Solution` 和 `Provenance`；新评分、预览、RAG、离线缓存和错误日志必须继续保密答案。
8. 旧 `feedback` 只能标为 legacy 事实，不能无依据倒填成“按新评分规则确认的高可信证据”。

## 5. 下一步决策

**S1 是 S2/S3 的硬前置，S2/S3 是 S4 的硬前置。** 建议先实现评分规则修订、部分分、提交幂等和评分事件，随后构建能力证据和错因闭环。真实北师大 2019 教材、广东各年卷型、付费 AI、真实网盘、生产恢复仍需独立验收，不以合成夹具或 CI 代替。

**本稿仅为审计设计，未完成任何 v3.1 业务代码。**
