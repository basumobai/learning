# 数据库

迁移文件在 `/migrations`。新增结构必须创建新编号文件，不可在已有实例上直接更改旧 migration。

核心关系：`words` 一词一行；`word_senses` 一义一行；`sense_aliases` 为该词义的规范化中文别名；`study_questions` 对应词义与题型；`answer_logs` 保存原始答案、答题时间、提示和结果；`review_states` 根据日志生成按维度的下一次复习时间。

`answer_logs.question_id` 唯一，`(user_id,request_id)` 唯一，防止重复提交；如改变规则，优先新增可重新计算的派生状态，不覆盖历史答题记录。示例用户 `demo` 由第一条 migration 创建，正式账户阶段应迁移至登录身份。

当前尚未建立 `difficulty/stability/retrievability`：正式 FSRS 属于后续阶段。新增算法时用新 migration 扩展 review_states 或建立版本化新表，同时保留历史日志。
