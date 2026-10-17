# Learning

面向广东高中学习的 Web 平台，覆盖 **语文、数学、英语、物理、化学、生物**：题库/PDF 入库、练习与按需 AI 讲解、作文多模型评审、英语词汇 FSRS 复习、听力和限时四选一。

- **先安装，再导入资料**：项目仅附开发示例词，不包含完整教材、试卷或预置收费模型。
- **当前提供 Web**：兼容手机、平板与桌面浏览器；暂不提供 Android 安装包。
- **AI 模型由管理员接入**：兼容 OpenAI 格式的公开 HTTPS API（如 OpenRouter）；实际调用由所用服务商计费。

## 🚀 三步启动

准备 **Docker Engine + Docker Compose v2**，并安装 **Git、Python 3、OpenSSL、curl**。可参考 [Docker 官方安装说明](https://docs.docker.com/engine/install/)；无需在主机单独安装 Go、Node 或 PostgreSQL。

```bash
git clone https://github.com/basumobai/learning.git
cd learning
bash scripts/install.sh
```

首次安装会交互询问 Web 和 API 的主机端口，并检查端口是否被占用；默认候选值是 3000 和 8080，遇到冲突可输入其他空闲端口。已有部署保留 .env；未配置端口时沿用 3000/8080，可手动添加 `WEB_PORT`、`API_PORT`。端口支持 1024–65535。无交互终端时使用默认值，也可在首次运行前设置 `WEB_PORT=3001 API_PORT=8081 bash scripts/install.sh`。不要停止占用端口的其他服务。

首次运行会自动生成 `.env` 中的数据库密码、管理员初始密码与 32 字节加密密钥，构建并启动容器，检查 Web/API 是否就绪。**请保存终端首次显示的管理员密码**，首次登录必须修改。已存在的 `.env` 与数据卷不会被安装器重置。

正式域名和自定义端口可在首次安装时一并指定：

```bash
WEB_PORT=9090 API_PORT=9009 bash scripts/install.sh --origin https://www.sumobai.com
```

已有部署只编辑 `.env` 中对应域名/端口，保留密码与加密密钥。安装器先检查旧卷，再用配置密码实际连接数据库；**旧卷存在但 `.env` 丢失时停止并要求恢复原配置，不生成替代密钥**。Web 等API健康后启动。

本机打开 `http://localhost:<WEB_PORT>`，使用 `admin` 和首次生成的密码登录。

> **VPS 默认只在本机监听端口，不会直接公网开放。** 若通过自己的电脑访问 VPS，可运行 `ssh -L <WEB_PORT>:127.0.0.1:<WEB_PORT> 用户名@服务器地址`，再在本机浏览器打开 `http://localhost:<WEB_PORT>`。正式使用域名时，在 HTTPS 反向代理前端转发到服务器 `127.0.0.1:<WEB_PORT>`，并将 `.env` 里的 `APP_ORIGIN` 改成实际的 `https://...` 站点源地址。

### 安装后做什么

1. 进入 **管理 → 服务商与模型**，配置服务商并获取模型目录，可视化多选聊天/TTS模型；不支持发现的服务商可手动添加。
2. 在 **身份组** 中授权模型，在 **管理 → 账户 → 自主注册** 选择新用户默认身份组。用户从登录页自行注册，也可关闭注册并由管理员创建。
3. 进入 **管理员设置 → 资源管理** `/admin/resources`：原件进入A区，轻量内容分类进入B区，深度解析与审核在C区，发布后进入资源库和现有题库。独立语法/知识点专题不必绑定教材Unit。英语词库仍在原管理页导入。

自己的密码在 **设置 → 密码与登录安全**，也可点击顶部姓名直达；管理员重置他人密码在 **管理 → 账户**。

服务器 PDF 放在 **`data/pdf/`**，也可在 `.env` 用 `LEARNING_PDF_DIR` 指定其他只读挂载根目录。资源工作台支持单PDF上传、相对子目录扫描、批量标签和可恢复任务；旧双PDF/WebDAV流程保留在 `/admin/learning`。

网盘导入当前支持**已授权OpenList分享挂载目录**，只下载管理员选中的PDF；尚不支持凭任意夸克/百度链接直接解析。模型/网盘凭据不交给AI。详细配置、限制和数据迁移见 [v2操作与升级](docs/v2/OPERATIONS.md)。

## 常用运维命令

在仓库目录执行：

| 操作 | 命令 |
| --- | --- |
| 查看状态 | `docker compose ps` |
| 查看最新日志 | `docker compose logs --tail=100` |
| 持续查看日志 | `docker compose logs -f --tail=100` |
| 重启 | `docker compose restart` |
| 拉取并无缓存更新前端 | `git pull --ff-only && bash scripts/install.sh --update` |
| 修复配置后恢复 | `bash scripts/install.sh --recover` |
| 停止（保留数据库） | `docker compose down` |
| 只生成配置，不启动 | `bash scripts/install.sh --config-only` |

**更新行为：** `--update` 会无缓存重建 Web 前端、更新 Compose 服务、强制重新创建 Web 容器，最后打印 Git 修订号与 Next.js 构建 ID。新版后台 HTML 使用不缓存策略，浏览器会重新获取最新页面；带哈希的 JS/CSS 文件可以继续安全缓存。脚本**不会**清除数据库、Redis、用户凭据、历史资料或其他 Docker 项目的缓存。首次更新可能较慢，因为重新执行前端依赖安装。外部 Cloudflare / CDN 缓存不受本机脚本控制，若仍显示旧版，需要检查反向代理指向并按需从 CDN 控制台清除缓存。

**管理员入口：** 登录后点击顶部「管理」，左侧「资源处理工作台」可进入 A/B/C 区；「AI 服务商与模型」可获取模型目录并多选。原误输入路径 `/admin/resource` 会跳转到 `/admin/resources`。手机和平板下管理员分类导航改为可横向滚动的分组入口。

**不要执行 `docker compose down -v`**，除非确认要销毁本项目数据库卷。更新前请备份数据库与 `.env`；`APP_ENCRYPTION_KEY` 不能随意更换，否则已经加密的 API Key 和两步验证密钥将无法解密。安装器不会重设已有管理员密码。

安装问题可先看 `docker compose ps` 和 `docker compose logs --tail=100`。数据库备份、安全配置、HTTPS 部署与常见错误详见 [部署及排障](docs/DEPLOY.md)。

## 项目功能与文档

| 功能 | 入口 |
| --- | --- |
| 六科练习与动态讲解 | `/` → 选择科目、开始练习 |
| 词汇复习 | `/english` |
| 限时四选一 | `/challenge` |
| AI 助学 / 句子拆解 / 听力 | `/assistant` |
| 广东听说 A朗读 / B三问五答 / C复述 | `/assistant#listening` |
| 自主注册 / 自己的密码 | `/register` / `/settings#security` |
| 服务商、模型、身份组与用户 | `/admin` |
| A/B/C资源管理、扫描、分类与审核 | `/admin/resources` |
| 正式学习资源 | `/resources` |
| 任务、Worker状态与恢复 | `/admin/resources/jobs` |
| 旧双PDF与多学科兼容配置 | `/admin/learning` |

- [v2操作与升级](docs/v2/OPERATIONS.md) · [审计与设计](docs/v2/P0_AUDIT.md) · [分阶段验证](docs/v2/VALIDATION.md)
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
