# 安装、更新、恢复与 HTTPS 排障

需要 Docker Engine、Compose v2、Git、Python 3、OpenSSL、curl，且当前用户可执行 `docker info`。主机不需安装 Go、Node、PostgreSQL。Web/API仅绑定本机，PostgreSQL/Redis不公开端口。

## 安装与部署模式

```bash
git clone https://github.com/basumobai/learning.git
cd learning
bash scripts/install.sh
```

首次交互选择主机端口，默认Web3000、API8080，范围1024–65535，拒绝其他业务占用的端口。首次使用正式域名：

```bash
WEB_PORT=9090 API_PORT=9009 bash scripts/install.sh --origin https://www.sumobai.com
```

| 模式 | 行为 |
| --- | --- |
| 默认，未发现配置或旧卷 | 生成随机密码/加密密钥，创建权限0600的 `.env`，启动并检查健康 |
| 默认，已有 `.env` | 保留全部配置、密钥与账号，验证当前项目端口、真实数据库认证后重启 |
| `--update` | 必须已有 `.env`；验证后构建、应用增量迁移、启动 |
| `--recover` | 必须恢复原配置；先检查数据库认证，不自动修改数据库密码 |
| `--config-only` | 生成或验证配置，不启动；生成前仍需Docker检查旧卷 |

初始管理员用户名 `admin`，密码仅首次生成时显示。`ELS_ADMIN_PASSWORD` 只用于首次创建管理员，不重置已有账号。自己的密码在“设置 → 密码与登录安全”。

已有部署只编辑需修改的字段，不重新复制示例配置、不轮换 `APP_ENCRYPTION_KEY`。`--origin` 与既有配置不一致时停止，要求人工核对；安装器无法凭空推断Nginx面板里的域名。

## 正式域名与 Nginx / OpenResty

本次部署目录 `/www/wwwroot/learning/learning`，配置应为：

```dotenv
APP_ORIGIN=https://www.sumobai.com
WEB_PORT=9090
API_PORT=9009
```

保留原数据库密码、管理员引导密码与加密密钥。修改域名后运行：

```bash
bash scripts/install.sh --recover --origin https://www.sumobai.com
```

Nginx/OpenResty HTTPS站点示例（证书路径替换为面板生成的实际路径）：

```nginx
server {
    listen 443 ssl;
    server_name www.sumobai.com;
    ssl_certificate /path/to/fullchain.pem;
    ssl_certificate_key /path/to/privkey.pem;
    client_max_body_size 70m;
    location / {
        proxy_pass http://127.0.0.1:9090;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_read_timeout 180s;
        proxy_send_timeout 180s;
    }
}
```

全部请求（包括 `/api/v1/`）转发到Web9090，再由Web转发到内部API8080，无需把公网API另行指向9009。若Nginx自身在容器内，`127.0.0.1` 指向Nginx容器，应改为能访问宿主机Web端口的受控网络地址。

80端口可另设301跳转HTTPS。检查和重载使用面板或该实例对应的Nginx命令，不关闭其他站点。

来源校验使用配置中的唯一公开源地址，并保留浏览器 `Origin`。Host和转发头用于路由，不扩大允许的来源范围；后端不根据任意转发头决定Cookie的Secure属性。HTTPS使用 `__Host-`、Secure、HttpOnly、SameSite=Strict会话Cookie，写操作继续需要CSRF Token。不配置通配CORS、不关闭来源验证。

## 数据库密码冲突与缺失配置

`pg_isready` 只能证明数据库进程可连接，不能验证密码。官方镜像的容器内本机TCP也可能免密；安装器用 `.env` 密码经Compose网络地址 `postgres` 执行 `SELECT 1`，避免本机信任规则造成误判。失败即停止，不重建卷、不自动执行 `ALTER ROLE`。

**旧卷存在但 `.env` 丢失：** 从原部署或加密备份恢复完整 `.env`，尤其原 `APP_ENCRYPTION_KEY`。不能生成替代密钥来修复旧库；原加密密钥丢失后，已有API Key/TOTP密文不能直接恢复，需先备份，再规划人工恢复。

**配置存在但密码不匹配：** 优先恢复与数据库匹配的原密码。若确定需人工同步，先备份数据库和原配置，在自己管理的服务器进入交互终端：

```bash
docker compose exec postgres psql -U els -d els
```

在psql中执行 `\password els`，按提示输入与 `.env` 对应的新密码，再 `\q` 退出。不要把密码拼进SQL命令行、聊天或日志。然后运行 `bash scripts/install.sh --recover`。禁用本地信任认证的实例需按自己的数据库管理员认证策略恢复。

**目录或项目名变动：** 安装器检查 `postgres_data` 标签与同名后缀卷。若当前项目将使用不同卷，则停止。用 `docker volume ls --filter label=com.docker.compose.volume=postgres_data` 核对元数据，恢复原目录或在 `.env` 设置原 `COMPOSE_PROJECT_NAME`；不要删除旧卷绕过检查。

配置检查使用 `docker compose config --quiet`，不要公开完整配置输出。

## 更新、备份与健康检查

在仓库根目录备份数据库：

```bash
umask 077
docker compose exec -T postgres pg_dump -U els -d els --format=custom > els-backup.dump
```

单独安全备份完整 `.env` 和PDF原始资料目录，不提交到Git。数据库内含账号、AI密文、PDF快照与练习历史。定期在隔离实例验证备份可恢复性。

```bash
git pull --ff-only
bash scripts/install.sh --update
docker compose ps
```

更新使用增量迁移，不重建卷/账号或替换密钥。停止使用 `docker compose down`，**不要加 `-v`**。

核心服务都有健康检查与 `unless-stopped` 策略。Web等待API健康，API等待数据库和Redis健康。API暂时不可用时Web显示中文503提示，不暴露内部错误、不自动重放写请求；后续请求可重新连接。读取账号失败时，依次等待15、30、60秒自动重连，最多3次；期间不跳转登录页，超过次数仍保留手动重连入口。健康状态本身不会重启仍存活的进程，API进程退出才由Docker策略重启。

```bash
curl -fsS http://127.0.0.1:9009/api/v1/health
curl -fsS http://127.0.0.1:9090/api/v1/health
docker compose logs api postgres --tail=100
```

## 常见问题

| 症状 | 恢复 |
| --- | --- |
| 缺失配置且发现旧卷 | 恢复完整配置和项目名，禁止重新初始化 |
| `28P01` / 数据库认证失败 | 按上文安全同步，不删除卷 |
| 端口占用 | 修改对应端口；同一Compose项目的原映射可复用 |
| 来源与配置不一致 | 对照浏览器scheme/域名/端口与APP_ORIGIN，再检查反代头 |
| 页面存在但API不可用 | 查看四服务健康，先定位数据库/API，再重试Web |
| PDF目录不可读 | 创建 `data/pdf/` 并允许容器读取；自定义挂载后重建API |
| OCR/提取失败 | 配置对应任务模型，查看逐页结果和失败任务；已完成OCR页保留 |
| 录音不可用 | 使用HTTPS并允许麦克风，也可填写英文作答 |

安装器隔离测试：`bash scripts/test-install.sh`。完整CI包括PostgreSQL17、Go race、Web构建/浏览器和Compose集成。验证记录见 [DEPLOYMENT_VALIDATION](DEPLOYMENT_VALIDATION.md)。

## 管理员忘记密码

有另一名管理员时，在账户管理中重新认证后重置。只有服务器操作者可以在交互终端运行：

```bash
docker compose exec -it api /app/els admin-reset-password --username admin
```

密码不回显，需再次输入并确认 `RESET admin`；命令撤销所有会话、要求首次改密并记录审计。丢失验证器时明确加 `--clear-mfa`。不会启用已停用账号，不修改 `.env`、数据库密码、文件或 APP_ENCRYPTION_KEY。更改环境里的初始密码不能恢复已有账号。
