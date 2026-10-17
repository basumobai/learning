# v3.1 操作与上线

代码按 S1→S8 依赖 PR 审核，不自动合并设计 PR #13 或缓存 PR #15，也不连接生产。

1. 先备份：在仓库根目录运行 `bash scripts/backup-learning.sh /安全目录/全新备份名`。备份含加密密钥和个人数据，目录权限0700，不提交Git。
2. 隔离恢复：`bash scripts/restore-learning-drill.sh /安全目录/备份名`。脚本只创建随机命名副本，不覆盖原数据库；失败保留现场，成功才删除副本。
3. 正常安装器增量更新。保持原 `.env`、`APP_ENCRYPTION_KEY` 与数据卷。0016–0022全部成功后新API才启动。
4. 先在隔离环境按 VALIDATION.md 验收；管理员 `/admin/engine` 为已发布题目审核逐题评分与知识映射。新的修订只影响后续练习。
5. 在 `.env` 分别启用 `LEARNING_V3_SCORING=true`、`LEARNING_V3_SKILLS=true`，然后按验收启用 `MISTAKES/PLANNING/TUTOR/EXAMS`（均带前缀 `LEARNING_V3_`），重建API和Worker。默认全部false，旧练习路径保留。
6. 模型任务新增 mistake_diagnosis / guided_tutor；复用原模型目录和身份组。ASR选用用途“语音转写”，供应商须支持 `/audio/transcriptions`；不会将TTS模型自动当ASR。
7. QUALITY_GATE先保持false以建立有权评测集和人工评审；覆盖六科任务后再设true。修改提示词、模型或提供商配置需重新评测，不能把自动词项命中当教学准确率。
8. 整卷规则需管理员提供有权来源，具体年份、板块小题数、板块总分、duration_seconds和total_points均核验；没有匹配题库就拒绝组卷。平台模拟卷不宣称官方试卷。

回滚：先关闭新开关，停相关消费，再恢复旧应用镜像；保留新增表与事实。已有进行中模拟卷应先恢复/交卷；Worker会按原截止用最后草稿关闭。不得清空卷、重置密钥、删除原PDF或回填伪造历史评分。数据库灾难恢复是独立操作，本次只实现并测试一次性副本演练。

未验收的真实依赖：具体北师大2019 Unit材料、广东各年卷型、真实网盘、付费模型教学/语音质量与账单、生产数据恢复。自动夹具不替代上述材料。专业发音评分和一般符号代数等价未启用；有理数/分数采用受限确定性比较。
