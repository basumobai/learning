# ELS · 广东高中英语智能学习平台

面向高中英语的独立能力训练：英译中选择、主动释义、中译英拼写。现已接入 **FSRS-6**，支持到期复习、随机新题、题型筛选、答后评价、进度恢复和撤销。当前词库只有 **5 个原创示例词**，尚未接入北师大版教材或账户认证，仅供本机开发。

## 启动

安装 Docker 与 Docker Compose，在项目根目录运行：

```bash
docker compose up --build -d
```

打开 `http://localhost:3000`。首次构建需下载镜像和依赖，PostgreSQL 数据保存在 Compose 卷中。停止服务使用 `docker compose down`，保留数据；不要添加 `-v`，除非确实要删除本机开发数据。

前端 3000 和 API 8080 只绑定本机。`demo` 为共享体验身份，没有认证，请勿通过反向代理公开部署。可在 `.env` 中设置 `POSTGRES_PASSWORD`；配置示例见 `.env.example`。

## 使用

1. 选择按计划学习或自由练习，再选择题型；默认中译英拼写。
2. 提交答案后，先看参考答案和例句，再评价记忆：重学 / 困难 / 记住了 / 很轻松。
3. 答错或使用提示时必须重学；未匹配的中文释义由你核对，自评来源单独保留。
4. 按计划到期优先，同一词义的不同题型分日练习；自由练习不改变正式复习时间。
5. 刷新会恢复题目和答后反馈。请求未确认时沿用原请求重试；可重新作答或撤销最近一题。

## 验证

需要 Go 1.26、Node 24。分别在对应目录运行：

```bash
cd services/api
go test ./...
go vet ./...
```

```bash
cd apps/web
npm ci
npm test
npm run build
```

数据库集成测试需要 `TEST_DATABASE_URL` 指向可创建临时 schema 的测试 PostgreSQL；未设置时明确跳过。CI 用 PostgreSQL 17 执行数据库与并发测试，并启动完整 Compose 栈运行 `python3 scripts/smoke.py`。升级使用增量迁移，不重置原数据。

能力对比、审计依据和后续优先级：[AnkiDroid 对比与路线](docs/ANKIDROID_COMPARISON.md)。产品行为：[PRODUCT](docs/PRODUCT.md)，接口：[API](docs/API.md)，安全边界：[SECURITY](docs/SECURITY.md)。
