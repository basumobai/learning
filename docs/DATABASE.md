# 数据库与迁移

PostgreSQL 是词库、答题、复习和进度的权威来源；Redis 预留，当前复习不依赖缓存。

0001 保留词、词义、释义别名、用户、Session、题目、答题及分能力复习模型。0002 是增量迁移：

- review_states.fsrs_card 保存 go-fsrs 的完整 Card JSON；next_review_at 为查询队列的到期索引字段，attempts/correct_count 保留累计计数。
- study_sessions.mode 标记 scheduled 或 practice。
- answer_logs 保存 initial_result（原判分）、result（确认后结果）、resolved_result（自评）、rating、reviewed_at、schedule_applied、previous_review、fsrs_log、next_review_at、review_request_id、undone_at。
- 每题只有一条未撤销日志（部分唯一索引）；历史请求编号保持唯一，旧请求不能重新解释成新作答。
- undo_requests 保存撤销请求的幂等结果，不删除答题历史。

旧日志与状态不重置。旧版 correct/wrong 视为已完成，不伪造 FSRS 记忆参数；pending 留在核对阶段。旧卡的既有到期时间保留到首次新评价，此时初始化 FSRS。没有旧快照的已完成记录拒绝撤销。新的撤销在一个事务内软标记日志并恢复完整原状态；原状态不存在则删除此次新建的 review_state。

创建、答题、评级、撤销先锁定体验用户行，防止多标签页并发越序或重复更新；读取不修改状态。今后引入账户时必须从认证身份获取 user_id。迁移路径默认为 /app/migrations，可通过 MIGRATIONS_DIR 指定本地测试路径。

数据库集成测试以 TEST_DATABASE_URL 创建独立临时 schema，验证迁移重放、评级、撤销、待判自评、自由练习、到期选题和并发恢复；cleanup 仅删除该测试 schema，不删除共享数据库。
