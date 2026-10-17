# C｜Learning v3.1 S0：迁移、版本、安全与回滚设计

> 设计稿，不是已执行 SQL。基线：`main@de6f5ad83254`，最新迁移为 `0015_legacy_originals.sql`。**迁移编号、列名与 SQL 必须在实际编码前重新核对最新 main 和开放 PR。**

## 1. 不可破坏的现有数据

`users`、身份组、加密模型凭据、`learning_documents`、`learning_blobs`、`learning_question_revisions`、`learning_revision_assets`、`learning_attempts`、`learning_jobs`、`study_sessions`、`answer_logs`、`review_states`、`learning_word_rounds` 均保留原主键、既有内容和访问路径。不得清空数据库、重建词汇 FSRS 卡片、自动删除原 PDF、覆盖旧题答案或重置 `APP_ENCRYPTION_KEY`。历史作答应继续通过旧 API 可读。

## 2. 建议的增量迁移（逻辑草案）

| 预备迁移 | 新增内容 | 强约束与兼容策略 |
| --- | --- | --- |
| `0016_scoring_evidence.sql`（暂定） | `learning_scoring_rule_revisions`、`learning_score_evidence`、`learning_submission_requests`、评分事件 outbox；在 `learning_attempts` 上新增 nullable 规则修订引用 | 规则版本不可变，发布前审核；`(user_id,client_request_id)` 唯一、载荷指纹冲突返回 409；旧 attempt 无规则修订时保持 legacy 路径 |
| `0017_skill_evidence.sql`（暂定） | 知识映射修订、知识点前置/角色、`learning_skill_evidence`、`learning_user_skill_states` | 复用 `learning_terms`，不复制知识树；证据引用评分事件、题目版本、提示/变式、tracker 版本；按用户隔离 |
| `0018_mistake_tracking.sql`（暂定） | 六科错因分类、候选错因、学生确认/更正历史、迁移验证关系 | AI 候选与学生确认分表/分状态；未确认不得自动写永久掌握结论 |
| 后续独立迁移 | 推荐决策、每日计划、ReviewItem、教学引用、AI 质量记录 | 只有 S1/S2/S3 出口通过后才启用，避免大迁移混在一起 |

具体表名仅为建议，需与 0001–0015 的实际约束、触发器和现有测试对照后定稿。不要创建与 `learning_question_revisions`、`learning_jobs`、`learning_terms` 等价的平行表。

### 2.1 评分规则修订的最低字段

`id`、`question_id`、`question_revision`、`revision`、`rule_json`、`max_points`、`rule_engine_version`、`review_status`、`reviewer_id`、`created_at`、`source_revision`。已发布规则及其内容采用不可变约束；修改产生新版本，不允许原地 UPDATE/DELETE。规则 JSON 需校验：part ID 与题目一致、各分值非负且总分可核、合法选项集合、少选/多选/错选的逐题政策、等价答案列表及来源。数学表达式等价仅在受限、确定性验证器中启用，不对任意输入执行代码。

### 2.2 作答和评分证据的最低字段

`learning_attempts` 保留原有 `question_revision`、`request_id`、`answers`、`feedback`；新增 nullable `scoring_rule_revision_id`、`question_skill_link_revision_id`、`submission_digest`。`learning_score_evidence` 记录 `attempt_id`、`question_revision`、`rule_revision_id`、`raw_answer_digest`、`score`、`max_score`、逐 part 评分、`status`、`confidence`、`engine_version`、`created_at`、`supersedes_id`。AI 参考评分、人工确认与确定性评分**分别持久化**，不能用 AI 返回覆盖旧 score。用户输入的原答案仍在 attempt 原字段保留。

### 2.3 旧数据回填政策

- `0014` 已将当时存在的题目修订快照入库，但不能恢复在 0014 之前被覆盖的历史。先对所有 `learning_attempts(question_id,question_revision)` 检查能否命中快照；缺失时生成报告、阻止强制外键验证，不伪造版本。
- 对旧 `feedback` 仅保留原 JSON 与 legacy 标识；没有当时有效的评分规则/原答案依据，不得推断为 `verified_rule`。可选择管理员审核后追加新评分事件，必须标注重评时间和规则版本。
- 老客户端不传新 `client_request_id` 时继续以现有行锁防重；只有新请求同时带幂等键与摘要时才启用冲突检测。不能突然要求所有老客户端升级。
- 原题版本与资产关联只增量关联；历史 PDF 原件和页码不重写、不自动删除。

## 3. 分阶段上线与可回滚路径

1. **只读基线**：备份 PostgreSQL、`data/pdf/`、对象存储、`.env` 和加密密钥；在隔离环境做一次实际恢复并校验用户数、题目数、历史作答数、PDF hash 与 FSRS 记录。
2. **Expand**：先添加新表/nullable 列/索引，不更改现有表主键或旧 API；长耗时索引采用适当在线策略并记录锁等待。对大型表采用小批量迁移，避免长事务。
3. **Shadow**：新评分引擎在隔离环境以影子模式运行，保存比较结果但不改正式分数/能力状态；用固定题库验证部分分与旧题回看。
4. **Dual-read**：新 attempt 使用已审核规则版本；旧 attempt 仍按旧路径展示，UI 明确“旧版评分”。新证据写入通过 feature flag 控制，不默认回填旧数据。
5. **Activate**：只在 S1 验收后开启正式评分事件；S2/S3 的能力更新和错因确认另设独立开关，按小流量/测试用户渐进启用。
6. **Rollback**：首先关闭新特性开关、停止相关 Worker 消费，恢复旧服务镜像；保留新增表和新证据，不执行 DROP/DELETE。确认旧代码对新增列向后兼容后回滚应用；数据库灾难恢复只能使用已演练备份并单独批准。

任何迁移失败应中止启动并记录失败 SQL、schema_migrations 状态及可重试条件。不得为了继续部署跳过迁移错误、重置数据库卷或更换密钥。

## 4. 事务、并发与任务恢复

- `submit`：在事务中锁本人 attempt，校验截止、题目/规则修订和请求摘要；写入唯一提交事实、确定性评分及 outbox，事务提交后再触发 AI/派生计算。
- 相同幂等键相同请求返回相同结果；同键不同请求 409；重复 Worker、Redis 消息重投和服务重启不重复计分。对每个 `(score_evidence_id,skill_id,tracker_version)` 建立唯一证据应用约束。
- 现有 `learning_jobs` 的 lease/generation 继续使用；旧代次不得更新新的派生状态。重评和重算是新的版本化事件，原评分及历史状态保留可追溯记录。
- AI 供应商请求可重复收费，不承诺供应商侧 exactly-once；预算与失败重试按现有 v2 路由记录，绝不把估算费用说成真实账单。

## 5. 权限、安全与版权

- 所有 attempt/score/skill/mistake/plan 查询强制 `user_id` 隔离，管理员权限只限最小必要的审计视图；跨用户、跨角色、CSRF/Origin 负向测试必须通过。
- 未交卷时服务端绝不返回答案、评分规则细节、未审核 PDF、RAG 命中的解答片段；Service Worker 不缓存受保护答案。日志不记录完整答案/令牌/模型密钥。
- PDF/网页/模型输出均视为不可信数据，不执行其中指令、HTML/JS、公式代码或外部请求；数学等价检查需白名单表达式解析与 CPU/时间限制。
- 复用第三方源码前按 `OPEN_SOURCE_REFERENCES.md` 逐文件核对完整提交 SHA、许可证、版权、依赖链与 NOTICE。MIT 来源保留原声明；AGPL/GPL/自定义许可源码不直接混入 MIT 原创模块。课程、试卷、音频和模型素材独立核验版权。
- 项目根目录 `LICENSE` 需权利持有人确认后由独立治理 PR 添加；不能凭计划书宣布全仓库 MIT。

## 6. 迁移验收/阻断条件

阻断上线：旧 attempt 找不到其原题快照；同键冲突仍双计；提交前泄露答案；旧 FSRS 卡片发生无故变化；迁移未完成却启动新写入；AI 评分未经确认污染能力；跨用户读取成功；恢复演练不能还原数据库与 PDF。所有失败必须保留现场和测试证据，禁止“重置后测试通过”冒充兼容升级。

**本文件未执行任何迁移；所有 SQL、表名和 feature flag 为待评审设计。**
