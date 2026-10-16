# 部署与学习流程验证记录

基于main `e6a6952`，PR #9已合并，审计时没有开放PR。独立修复分支 `fix/safe-deployment-and-learning-workflows`，见 [PR #10](https://github.com/basumobai/learning/pull/10)。未操作生产服务器、生产数据库或真实 `.env`，不合并main。

## 自动验证范围

| 检查 | 验证内容 |
| --- | --- |
| 安装器隔离回归 | 首次生成、权限、重复安装、旧卷缺失配置、认证冲突、项目名变化、自定义端口/端口冲突、域名不一致与非法来源；验证不输出数据库密码或加密密钥 |
| 真实安装器验收 | GitHub Actions一次性独立项目，实际执行首次安装、重复安装、升级、密码冲突/恢复、缺失配置保护；对比账号密码摘要和AI密文，等待四服务全部健康 |
| Go与PostgreSQL17 | 迁移、账号/密文保留、HTTPS配置登录、Secure Cookie、CSRF、拒绝非法Origin/转发头绕过、自助注册/默认身份组、权限隔离、OCR进度、分阶段PDF与跨小节重复题号、听说任务结构/交卷隔离/反馈幂等和撤权 |
| Web单元与构建 | 安全503代理、保留Origin/CSRF/Cookie、单次请求失败后恢复；现有学习/模型选择回归，TypeScript与Next.js生产构建 |
| 浏览器 | 原有UI与多设备套件；新增注册、服务器目录导入、OCR预览、广东B问答、笔记/计时/交卷/评价及账号故障重连，覆盖390/768/1440px |
| Compose集成 | PostgreSQL17、Redis7、API、Web健康依赖；完整栈登录、复习、幂等、撤销与页面冒烟 |

本地安装器、Go（无数据库时对应集成测试跳过）、Web单元与构建已执行。真实PostgreSQL17与Docker由GitHub Actions的隔离环境执行，不以跳过的本地测试冒充数据库验证。

首轮CI：安装器、API/race、Compose均通过；Web浏览器发现新增目录面板缺少模拟API数据导致渲染失败，已补全模拟场景并加上无效响应保护。最终状态以PR最新提交的四项check为准。

## 复核命令

```bash
bash scripts/test-install.sh
cd services/api
go test -race -count=1 ./...
go vet ./...
cd ../../apps/web
npm ci
npm test
npm run build
```

数据库集成需要 `TEST_DATABASE_URL` 指向一次性测试库；测试仅创建独立schema。启动测试Web后用 `npm run test:ui` 执行浏览器套件。

## 实际边界

- HTTPS测试使用正式域名作为配置和Origin，验证后端会话、Cookie与拒绝非法请求；未操作真实域名站点，生产登录仍需部署后验收。
- AI/OCR/TTS使用模拟供应商验证协议、权限与流程，没有消耗真实模型余额，也不宣称已验证扫描件识别准确率或真实音色质量。
- 浏览器录音仅本机回放，未接入ASR或专业发音评分；AI评价手动转写文本，不生成官方听说分。
- 视口由浏览器模拟，尚未在实体HyperOS平板/手机或电视上做实机验证。
