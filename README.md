# Learning

面向广东高中学习的 Web 平台，覆盖 **语文、数学、英语、物理、化学、生物**：题库/PDF 入库、练习与按需 AI 讲解、作文多模型评审、英语词汇 FSRS 复习、听力和限时四选一。

- **先安装，再导入资料**：项目仅附开发示例词，不包含完整教材、试卷或预置收费模型。
- **当前提供 Web**：兼容手机、平板与桌面浏览器；暂不提供 Android 安装包。
- **AI 模型由管理员接入**：兼容 OpenAI 格式的公开 HTTPS API（如 OpenRouter）；实际调用由所用服务商计费。

## 🚀 三步启动

准备 **Docker Engine + Docker Compose v2**，并安装 **Git、OpenSSL、curl**。可参考 [Docker 官方安装说明](https://docs.docker.com/engine/install/)；无需在主机单独安装 Go、Node 或 PostgreSQL。

```bash
git clone https://github.com/basumobai/learning.git
cd learning
bash scripts/install.sh
```

首次运行会自动生成 `.env` 中的数据库密码、管理员初始密码与 32 字节加密密钥，构建并启动容器，检查 Web/API 是否就绪。**请保存终端首次显示的管理员密码**，首次登录后建议立即修改。已存在的 `.env` 与数据卷不会被安装器重置。

本机打开 **http://localhost:3000**，使用 `admin` 和首次生成的密码登录。

> **VPS 默认只在本机监听端口，不会直接公网开放。** 若通过自己的电脑访问 VPS，可运行 `ssh -L 3000:127.0.0.1:3000 用户名@服务器地址`，再在本机浏览器打开 `http://localhost:3000`。正式使用域名时，在 HTTPS 反向代理前端转发到服务器 `127.0.0.1:3000`，并将 `.env` 里的 `APP_ORIGIN` 改成实际的 `https://...` 站点源地址。

### 安装后做什么

1. 进入 **管理 → 服务商与模型**，配置 OpenRouter 等服务商的 API Key，添加所需聊天/TTS 模型。
2. 在 **身份组** 中批量选择、授权模型，再到 **账户** 创建学生账号。
3. 进入 **多学科管理** `/admin/learning`，配置 AI 任务模型、导入并审核题库；或在管理页导入英语词库（单词、音标、词性、中文）。

## 常用运维命令

在仓库目录执行：

| 操作 | 命令 |
| --- | --- |
| 查看状态 | `docker compose ps` |
| 查看最新日志 | `docker compose logs --tail=100` |
| 持续查看日志 | `docker compose logs -f --tail=100` |
| 重启 | `docker compose restart` |
| 拉取代码后更新 | `git pull --ff-only && docker compose up --build -d` |
| 停止（保留数据库） | `docker compose down` |
| 只生成配置，不启动 | `bash scripts/install.sh --config-only` |

**不要执行 `docker compose down -v`**，除非确认要销毁本项目数据库卷。更新前请备份数据库与 `.env`；`APP_ENCRYPTION_KEY` 不能随意更换，否则已经加密的 API Key 和两步验证密钥将无法解密。安装器不会重设已有管理员密码。

安装问题可先看 `docker compose ps` 和 `docker compose logs --tail=100`。数据库备份、安全配置、HTTPS 部署与常见错误详见 [部署及排障](docs/DEPLOY.md)。

## 项目功能与文档

| 功能 | 入口 |
| --- | --- |
| 六科练习与动态讲解 | `/` → 选择科目、开始练习 |
| 词汇复习 | `/english` |
| 限时四选一 | `/challenge` |
| AI 助学 / 句子拆解 / 听力 | `/assistant` |
| 服务商、模型、身份组与用户 | `/admin` |
| PDF 题库与多学科配置 | `/admin/learning` |

- [功能与学习流程](docs/LEARNING.md) · [界面设计](docs/FRONTEND.md) · [安全与账号](docs/SECURITY.md)
- [架构](docs/ARCHITECTURE.md) · [API](docs/API.md) · [数据库](docs/DATABASE.md) · [AI 设计](docs/AI.md) · [产品行为](docs/PRODUCT.md)
- [验证与预览](docs/LEARNING_VALIDATION.md) · [与 AnkiDroid 对比](docs/ANKIDROID_COMPARISON.md) · [多端布局](docs/RESPONSIVE.md)

### 开发与测试

后端 Go 1.26、前端 Node.js 24。完整 CI 检查 Go、Web、浏览器与 Docker Compose 集成：

```bash
bash scripts/test-install.sh
cd services/api && go test ./...
cd ../../apps/web && npm ci && npm test && npm run build
```

需要真实 PostgreSQL 的集成测试使用 `TEST_DATABASE_URL`；不配置时对应数据库测试会跳过。可选 Codex CLI 入库不是正常安装所必需，配置步骤见 [Learning 文档](docs/LEARNING.md#可选-codex-cli-入库)。

> 不需要预先购买 AI 模型即可启动页面、设置账号与查看示例；要运行 AI 讲解、PDF 识别或自然语音，必须另外接入有效模型。项目没有应用级调用次数或对话次数额度，但供应商可能计费。
