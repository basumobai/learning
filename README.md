# Learning · 广东高中 AI 学习平台

当前专注 **Web**：语文、数学、英语、物理、化学、生物专项练习，PDF题目/答案配对入库、插图裁剪、按需动态讲解、步骤内AI追问、作文多模型评审，以及保留的FSRS-6英语词汇/听音/听力训练。教材词库由管理员自行导入，字段顺序为 **单词、音标、词性、中文**。内置五个原创示例词仅用于开发，不代表教材已导入。

多学科管理入口 `/admin/learning`，原词汇复习 `/english`，极速四选一 `/challenge`。[完整流程、训练规则、数据库说明与可选Codex入库](docs/LEARNING.md)。实际题库由管理员导入并审核发布。

## 本地启动

需要 Docker 与 Docker Compose。复制配置并自行填写密码和加密密钥：

```bash
cp .env.example .env
openssl rand -base64 32
```

将生成的值填入 `APP_ENCRYPTION_KEY`，设置 `POSTGRES_PASSWORD` 和 15–128 字符的 `ELS_ADMIN_PASSWORD`。首次启动创建 `ELS_ADMIN_USERNAME` 指定的管理员；已有管理员时不会被环境变量覆盖。妥善保管加密密钥，否则已加密的 API Key 和验证器密钥无法解密。`.env` 不提交到 Git。

```bash
docker compose up --build -d
```

打开 `http://localhost:3000` 登录。入口只绑定本机。数据库卷持续保存记录；`docker compose down` 保留数据，不要随意添加 `-v`。

公开测试环境需设置真实 HTTPS `APP_ORIGIN`，让浏览器与 API 通过 Next.js 同源访问。不要将数据库或 API 直接公开。配置、备份及账户恢复见 [SECURITY](docs/SECURITY.md)。本次改造不自动部署或修改生产环境。

## 管理员配置

1. **管理 → 服务商与模型**：填 `https://openrouter.ai/api/v1` 和自己的 API Key。也可使用其他公开 HTTPS OpenAI 兼容接口，例如 `https://api.openai.com/v1`。
2. 添加聊天模型，填写供应商的完整 model ID。支持 JSON mode 的模型可勾选该参数；未勾选时依靠 JSON 提示与服务端结构校验。
3. 添加语音模型。OpenRouter 可使用 `openai/gpt-4o-mini-tts-2025-12-15`，音色例如 `cedar, marin, coral`；其他模型需按供应商文档填写音色。朗读风格可配置，不支持 instructions 参数时留空。
4. **权限组**：勾选允许的聊天和语音模型，再给每个用户分配组。原英语入口继续按所属组授权；多学科管理员入库可使用启用的任务模型，学生调用平台模型始终遵循所属组。**没有应用调用次数额度或对话数量额度**。
5. **账户**：创建用户并提供初始密码；首次登录强制改密。可停用账户、重置密码及遗失的两步验证。管理写入需要管理员当前密码再次验证。
6. **多学科与 AI 任务**：进入 `/admin/learning` 配置各用途的默认提示词与模型，再上传题目和答案PDF、选小节、核对草稿后发布。学生可在设置接入个人API；云盘支持WebDAV。
7. **教材词库**：上传 UTF-8 CSV/TSV/TXT 或粘贴四字段词表，先预览再确认；重复词义跳过，不同词性/释义及其音标分别保留。保留教材词形大小写（如 English、I），大小写重复不会生成两个单词。

词表示例：

```text
adapt /əˈdæpt/ v. 适应
necessary /ˈnesəsəri/ adj. 必要的
```

CSV 可带表头 `word,phonetic,pos,chinese`；中文含逗号时按 CSV 规则加引号。

## 学习

- 今日复习：到期优先，同词义题型按学习日避让；答后核对，再评价记忆。四种能力独立安排，听音拼写先播放服务端语音，交卷后显示单词和音标。
- 账户与设置：每日新词预算、目标记忆率、时区、自然语音试听、密码、两步验证、退出其他设备、个人学习数据导出。
- 词库：搜索、查看音标/词性/中文、暂停或恢复自己的词条，查看反复未独立记住的次数。
- AI 助学：可恢复辅导对话、句子主干/成分/语法/理解检查、原创听力材料与选择题。听力交卷后显示原文、译文及精确答案依据。

真实模型和语音调用需要管理员填入有效供应商密钥。自动化测试使用模拟供应商，不消耗真实模型余额；模型质量、实际可用音色及账户余额须在配置后试听与验证。AI 生成材料明确标识，不自动当成教材来源或词汇语义判分。

## 验证

Go 1.26、Node 24：

```bash
cd services/api
go test -race ./...
go vet ./...
```

```bash
cd apps/web
npm ci
npm test
npm run build
```

数据库集成测试需要 `TEST_DATABASE_URL` 指向可创建临时 schema 的 PostgreSQL；未设置时明确跳过。CI 使用 PostgreSQL 17 运行真实迁移、并发、账户隔离、权限、AI 重试、听力交卷、每日预算和撤销测试，并启动 Compose 栈执行带登录的冒烟测试。增量迁移保留已有学习数据。

[本轮验证与新界面预览](docs/LEARNING_VALIDATION.md) · [前端设计与预览](docs/FRONTEND.md) · [AnkiDroid 对比与剩余差距](docs/ANKIDROID_COMPARISON.md) · [产品行为](docs/PRODUCT.md) · [接口](docs/API.md) · [架构](docs/ARCHITECTURE.md) · [AI](docs/AI.md)
