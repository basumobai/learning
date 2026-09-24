# ELS · 广东高中英语智能学习平台

当前是**第一阶段可运行样板**：少量原创示例词条、三种题型、答题日志、按能力维度的简易复习时间和手机优先的学习页面。没有接入教材词库、账户、AI 或正式 FSRS；详见 `docs/PRODUCT.md`。

## 本地启动

安装 Docker 与 Docker Compose 后，在项目根目录运行：

```bash
docker compose up --build
```

访问 `http://localhost:3000`。首次构建需要访问容器镜像仓库和语言包仓库。PostgreSQL 数据存储在 Compose 卷中。示例用户会由迁移文件创建；本项目**仅供本机开发**，没有身份验证，请勿公开部署。

服务：Next.js 前端 3000、Go API 8080（只监听本机）、PostgreSQL、Redis。`/api/v1/health` 用于检查 API 和数据库。页面通过 Next.js 的 `/api/v1/*` 转发访问 API。

## 开发顺序

1. 把示例词条替换成来源明确、许可清晰、经过校对的词库；建立教材版本与 Unit 关联。
2. 增加账户认证、用户数据隔离、速率限制，再考虑对外开放。
3. 将 `review.go` 的临时复习规则替换为经验证的 FSRS 适配层，并迁移现有状态。
4. 加入后台 AI 判分、超时处理和人工纠错入口；待判答案确认后再更新复习。

## 验证

有 Go 和 Node 的开发机可运行：`cd services/api && go test ./...`，`cd apps/web && npm ci && npm run build`。完整集成需执行 `docker compose up --build`。当前执行环境缺少 Go 与 Docker，未运行这两项验证。
