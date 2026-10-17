# B｜Learning v3.1 S0：模块、状态流与接口设计

> 设计基线：`main@de6f5ad83254`；**尚未实现**。以 [v3.1 计划书](https://github.com/basumobai/learning/pull/13) 为需求边界，沿用现有 Go `net/http` 单体模块、Next.js、PostgreSQL、独立 Worker 与 Redis 通知。

## 1. 单一真值与模块归属

| 模块 | 现有复用入口 | 新增职责 |
| --- | --- | --- |
| ResourcePublisher | `resource_review.go`、`learning_unit_publications` | 只有审核通过才能发布题目和评分规则；固定来源修订 |
| QuestionSnapshot | `learning_question_revisions`、`learning_revision_assets` | 继续复用不可变题干/答案/图片；关联评分与知识映射版本 |
| ScoringEngine | `learning.go:feedbackFor` | 确定性单选、多选部分分、多空、等价规则；生成可复算 ScoreEvidence |
| AttemptStore | `learning_attempts` | 原始答案、服务端截止、幂等键/指纹、原子交卷及审计 |
| KnowledgeCatalog | `learning_terms`、`learning_question_terms` | 知识点前置、主要/辅助/前置角色、历史映射修订 |
| SkillTracker | 拟增量新增 | 从可信评分产生不可变 SkillEvidence；派生可重算 UserSkillState |
| MistakeDiagnoser | 已有 AI 讲解任务 | 候选错因与原题/学生答案依据；学生确认、纠正和待定 |
| ReviewScheduler | `internal/learning/schedule.go`、FSRS | 仅服务端 go-fsrs/v4 调度主动回忆项；与能力状态分离 |
| PracticeRecommender | `startLearningAttempt` | 保留自由选题；智能选题决策可回放、题库不足可解释 |
| TeachingAssistant | `requestExplanation`、`learning_jobs` | 分步提示、按需完整讲解、已审核资料 RAG、新题迁移验证 |
| EvidenceReporter | 作答/评分/证据表 | 真实数据库聚合；AI 可解释但不得捏造指标 |

不引入第二套队列、Gin、独立 FSRS 算法或微服务；模型目录和权限继续使用 v2 实现。

## 2. 学生作答状态机

```text
管理员审核并发布题目/规则/知识映射版本
                ↓
       CREATE（幂等创建）
                ↓
     ANSWERING ←→ DRAFT（草稿）
                ↓
   SUBMIT（服务端事务、行锁、截止）
                ↓
   SUBMITTED（原始答案不可覆盖）
                ↓
  ┌─────────────┴──────────────┐
  ↓                            ↓
DETERMINISTIC_SCORE       NEEDS_REVIEW
  ↓                            ↓
ScoreEvidence             AI 仅提供参考评分
  │                            ↓
  │                     HUMAN_CONFIRMED / PENDING
  └─────────────┬──────────────┘
                ↓
    可信评分 → SkillEvidence
                ↓
   候选错因 → 学生确认/更正/待定
                ↓
  自主练习 / 智能强化 / AI 辅导
                ↓
   原题再做与新情境变式验证
                ↓
       可解释报告和复习计划
```

资源处理阶段（A/B/C）、内容审核状态、评分确认状态、知识状态、复习到期状态是**不同维度**，不能共用一个 status 字段。超时交卷必须采用数据库保存的最后有效草稿，不能接受客户端在截止后替换答案。撤销必须保留审计并以补偿事件回退派生状态，不删除原事实。

## 3. 版本与证据契约

作答在开始时固定：`user_id`、`attempt_id`、`session_id`（如适用）、`question_id`、`question_revision`、`scoring_rule_revision_id`、`question_skill_link_revision_id`。提交增加 `client_request_id`、请求摘要、服务端时间、提示使用与时间信息；允许老客户端暂不提供新字段并保留 legacy 路径。

- **事实层不可变**：原题快照、原答案、首次提交、服务端截止、原始提示/耗时、旧评分记录。修改规则只能生成新的修订与评分事件。
- **派生层可重算**：候选错因、能力状态、推荐排序、复习风险、学习报告；必须保留模型/规则版本与重算时间。
- **评分可信度**：`verified_rule`、`legacy`、`ai_suggestion`、`unresolved`、`human_confirmed` 等必须可区分；未经确认的主观题不得作为高置信度能力事实。
- **知识点证据**：区分独立首答、提示后答对、原题重复、新题迁移；综合题错不让所有关联知识点自动清零；证据不足时显示“证据有限”。

## 4. API 契约草案（非已上线）

**创建**：沿用 `POST /api/v1/learning/attempts`；返回无答案题目、题目修订、允许展示的图片、服务器截止；服务端固定规则修订。相同 `(user_id,request_id)` 同请求返回原 attempt，不同请求条件返回 409（需要新增指纹校验）。

**提交**：沿用 `POST /api/v1/learning/attempts/{id}/submit`，新增可选 `client_request_id`；相同键+相同载荷返回原评分，相同键+不同载荷返回 409；已交卷不得产生第二次有效能力事件。旧客户端无键仍以 attempt 行锁与提交状态防重。

**评分结果示意**：

```json
{
  "attempt_id": "opaque",
  "question_revision": 3,
  "scoring_rule_revision_id": "rule-r2",
  "status": "deterministic",
  "score": 3.0,
  "max_score": 5.0,
  "parts": [{"part_id": "p1", "awarded": 1.0, "max": 2.0, "basis": "approved_rule"}],
  "confidence": "verified_rule",
  "engine_version": "scoring-v1"
}
```

未交卷响应、缓存、推荐日志、RAG 检索均不得包含当前题正确选项、完整评分细则、未审核答案；敏感服务端日志脱敏。评分和 AI 参考评价分字段持久化，不得混称正式得分。

## 5. 幂等与事务边界

1. 锁定本人 attempt，核验版本/截止/请求摘要；在一个 PostgreSQL 事务中写原答卷、确定性评分、outbox 事件，再提交。外部 AI 调用在事务外执行。
2. 评分事件使用 `(attempt_id, scoring_rule_revision_id, scoring_run_id)` 唯一约束；能力应用使用 `(score_evidence_id, skill_id, tracker_version)` 唯一约束，重复消息只处理一次。
3. Worker 租约、generation、资源版本沿用 v2；重算生成新版本和新事件，不静默覆盖旧记录；旧 Worker 不得写入新 generation。
4. PostgreSQL 是真值；Redis 丢失后可从 outbox/任务表重建通知。外部模型重复收费不能承诺 exactly-once，本地事实必须幂等。

## 6. 错因、辅导与 RAG

错因候选需包含科目标签、原题片段/页码、学生答案片段、证据来源、模型/提示词版本和不确定性。学生可 `confirm`、`correct`、`unsure` 或标记“输入失误”；更正只追加事件。未经确认的 AI 猜测可用于临时建议，不写永久能力事实。

教学状态：`not_started → independent_attempt → optional_hint → submitted → diagnosis → remediation → transfer_check → completed`。学生可跳过提示、请求完整讲解，但服务端仍须阻止未交卷获取本题答案。RAG 仅引用本人有权访问的正式审核资源；每段带 `document_revision_id`、物理页码、引用片段及审核状态；无法核实时明确“不确定”，禁止伪造页码。

## 7. 多端与失败降级

手机单列、平板双栏切换、桌面并排；共享 DTO 和 API，服务端为唯一状态真值。AI/Redis 故障时已审核题库、确定性评分、历史回看继续工作。PWA 只缓存公开或学生本人获授权的安全内容；正式考试答案、未审核 PDF 和评分规则绝不缓存，登出清理敏感缓存。用户无每日 AI 调用额度；技术性并发/超时/防滥用限制可保留。

**评审门槛：S1 先落地评分与版本，再让 S2/S3 消费证据，S4 最后接推荐。未经用户确认不进行大规模编码。**
