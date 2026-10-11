# 数据库与迁移

0001 原始词汇/练习结构与五个示例词保留。0002 增加 FSRS 卡片、评价、快照和幂等撤销。0003 增量加入账户与 AI，不丢弃旧学习记录。

## 主要数据

| 表 | 作用 |
|---|---|
| users、auth_sessions、login_attempts | 用户密码/角色/组/两步验证，哈希会话、认证防暴力破解 |
| account_audit | 管理、密码、MFA 操作的非敏感审计 |
| permission_groups、group_models | 用户分组与已授权模型；不含调用次数额度 |
| ai_providers、ai_models | 兼容接口、AES-GCM API Key、模型用途/音色/指令 |
| user_preferences | 每日新词预算、目标记忆率、学习日时区、语音偏好 |
| words、word_senses、sense_aliases | 单词、词义音标、独立词性/中文义项、维护的释义别名 |
| vocabulary_imports、suspended_senses | 幂等导入结果、个人暂停 |
| review_states | 独立能力的完整 FSRS Card、到期与累计计数 |
| study_sessions、study_questions、answer_logs、undo_requests | 练习次序、判分/评级、完整快照、软撤销与幂等结果 |
| ai_conversations、ai_messages、ai_requests | 个人对话、模型来源、请求预留/结果缓存 |
| training_sessions | 句子拆解、私有听力原文/答案、首次交卷结果 |
| speech_cache | 按用户和参数隔离、三十天过期的 MP3 音频 |

旧 demo 用户没有 username/password_hash，不能登录；旧记录保留，未擅自分配给新账户。首次管理员根据环境配置创建。已有管理员时不重设凭据。

词义唯一索引为 word_id + part_of_speech + meaning_zh，导入不会替换旧词义及其复习引用。words.lemma 保留首次入库的规范大小写，以 lower(lemma) 唯一索引合并大小写重复；音标保存在 word_senses，可保留 record 名词/动词等不同读音；默认仅为空音标补值，显式允许时覆盖对应义项音标。自评不自动扩充释义别名。

复习状态维度包含新增 listening_spelling，与传统 spelling 独立。调度与日志写入在同一用户事务。previous_review 保存完整旧值；首次新增用 JSON null，撤销删除新状态并恢复每日预算。已有状态撤销回完整快照，不用旧 attempts 推断 FSRS 参数。

migration 按文件版本逐个事务执行，默认路径 /app/migrations，可用 MIGRATIONS_DIR 指向本地。迁移可重放。测试为 TEST_DATABASE_URL 创建唯一临时 schema，清理仅删除该 schema。详细备份策略与加密密钥保管见 SECURITY。
