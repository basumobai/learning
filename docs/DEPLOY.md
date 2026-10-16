# 部署、升级与排障

Learning 默认采用 **Docker Compose + PostgreSQL + Redis + Go API + Next.js Web**。安装器只负责配置并启动本仓库的容器；不会替你安装 Docker、修改系统防火墙、打开公网端口、清理数据库或部署 HTTPS。

## 一、快速安装

适用：Linux VPS / 开发机，已安装 Docker Engine、Docker Compose v2、Git、OpenSSL 与 curl，且当前用户可以执行 `docker info`。

```bash
git clone https://github.com/basumobai/learning.git
cd learning
bash scripts/install.sh
```

首次安装交互选择 Web/API 主机端口（默认 3000/8080，范围 1024–65535），遇到冲突可重新输入。非交互安装可使用 `WEB_PORT=3001 API_PORT=8081 bash scripts/install.sh`。只生成配置时不检查端口占用，实际启动前再检查。

安装器第一次运行时生成 `.env`（权限 0600），包括独立随机数据库密码、管理员初始密码、32 字节 Base64 加密密钥；再运行 `docker compose config --quiet`、`docker compose up --build -d`，等待 Web 和数据库健康接口返回成功。

管理员用户名默认 `admin`。密码首次创建时在终端显示一次，请保存并在登录后修改；**之后运行安装脚本不覆盖 `.env`，也不再次显示密码**。已有账号数据不会被环境变量中的初始密码覆盖。

若需要只准备配置、不启动容器：

```bash
bash scripts/install.sh --config-only
```

安装器不会自动修复已经存在但缺少字段或密钥不合规的 `.env`，而会提示需要修正的配置，以免意外丢失既有加密数据。手工编辑请参考 [`.env.example`](../.env.example)。使用 `openssl rand -base64 32` 生成加密密钥，已有加密数据时 **绝对不能直接重新生成替换**。

## 二、访问方式与 HTTPS

主机端口由 .env 的 `WEB_PORT`、`API_PORT` 控制；旧配置未填写时沿用 3000/8080，不改写既有密钥。改端口时也要同步修改 `APP_ORIGIN`（或保持实际域名）。

Compose 默认仅将 Web `3000` 和 API `8080` 映射到 `127.0.0.1`，数据库和 Redis 不对外映射。

- 服务器本机：浏览器打开 `http://localhost:<WEB_PORT>`。
- 从电脑连接远程 VPS：在你的电脑运行 `ssh -L <WEB_PORT>:127.0.0.1:<WEB_PORT> 用户名@服务器地址`，保持 SSH 会话，然后访问本机 `http://localhost:<WEB_PORT>`。
- 使用域名：在服务器的 Nginx/Caddy 等 HTTPS 反向代理中，将外部站点转发到 `http://127.0.0.1:<WEB_PORT>`。将 `.env` 的 `APP_ORIGIN` 设为浏览器访问的完整 HTTPS 源地址，例如 `https://learn.example.com`（不可附加路径），随后 `docker compose up -d` 使配置生效。保持 3000、8080、PostgreSQL、Redis 的容器端口不公开。

浏览器请求经 Next.js 同源转发到内部 API，无需另行公开 API。上传 PDF 时，反向代理的请求体上限须覆盖题目与答案文件合计大小，建议至少 70MB，并酌情设置上传超时。只在有 HTTPS 证书、实际域名及反代正确配置后才开放公网访问。

## 三、管理员首次配置

登录后进入：

1. `/admin` → **服务商与模型**：输入兼容 OpenAI 协议的 HTTPS URL（如 `https://openrouter.ai/api/v1`）与 API Key，添加聊天/视觉/TTS 模型。
2. `/admin#groups` → **身份组**：可以按模型类别、服务商、名称批量开启模型权限，再到 **账户** 创建用户、分配身份组。
3. `/admin/learning` → **AI 提示词与模型**：按题目提取、评分、讲解、作文等任务配置模型；上传题目/答案 PDF 时先检查并审核草稿后发布。
4. **词库导入**：可粘贴或上传含单词、音标、词性、中文释义的 CSV/TSV/TXT，预览后确认。

不配置第三方 AI Key 也能启动站点，但调用 AI 与 TTS 前需要有效模型。平台不限制调用次数，但第三方服务商可能计费。

## 四、日常运维与备份

```bash
docker compose ps
docker compose logs --tail=100
docker compose logs -f --tail=100
docker compose restart
```

更新前先备份（命令在仓库根目录执行）：

```bash
docker compose exec -T postgres pg_dump -U els -d els --format=custom > els-backup.dump
```

同时**单独安全备份 `.env`，尤其是 `APP_ENCRYPTION_KEY`**。数据库备份应妥善加密存放并定期验证可恢复性；不要上传到公开仓库。恢复需在隔离环境演练后执行，不推荐直接覆盖正在运行的数据库。

更新已部署的仓库：

```bash
git pull --ff-only
docker compose up --build -d
```

上述操作保留数据库卷和现有 `.env`，API 启动时自动执行尚未应用的数据库迁移。旧库升级仍建议先备份。停止容器使用 `docker compose down`；**不要添加 `-v`**，否则会删除本项目数据库卷。

## 五、常见问题

| 症状 | 优先检查 |
| --- | --- |
| `docker: command not found` | 安装 Docker Engine，确认 `docker compose version` 为 v2 |
| `Cannot connect to the Docker daemon` | Docker 服务状态以及当前用户是否有 Docker 权限 |
| 端口 `3000` / `8080` 被占用 | `docker compose ps` 与宿主端口监听状况，勿直接关闭其他业务 |
| `.env` 缺失、密码过短或密钥无效 | 只修改报错项；已有加密记录时禁止替换原密钥 |
| 页面打不开 | `docker compose ps`，然后分别检查 Web 和 API 日志 |
| API 健康检查失败 | `docker compose logs api postgres --tail=100`，核查数据库密码与迁移错误 |
| 登录或 CSRF 异常 | 检查 `APP_ORIGIN` 与浏览器访问源是否完全相同；反代是否使用 HTTPS |
| 模型无权限 | 模型及服务商是否启用、账号所属身份组是否授权 |
| 运行脚本后已有管理员密码没变 | 正常行为：初始密码只用于首次引导，不用于重置老账号 |

健康接口（替换为实际 API_PORT）：`curl -fsS http://127.0.0.1:<API_PORT>/api/v1/health`。

安装脚本的隔离测试（无需 Docker 或真实 API Key）：

```bash
bash scripts/test-install.sh
```

完整安全细节见 [SECURITY](SECURITY.md)；应用与多学科学习流程见 [LEARNING](LEARNING.md)。
