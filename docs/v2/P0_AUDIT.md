# Learning v2.0：P0 审计报告

审计日期：2026-10-17（UTC+8）。基线：`main@6dbbe97ebbeb9a6468e670753b6c0bfd4a13554d`。

本次交付仅为 P0：代码审计、技术设计、迁移方案和实施验收计划。P1–P8 尚未实施，本文不代表这些能力已经可用。未连接生产服务器、生产数据库或真实 AI/网盘账号。

- [架构与数据迁移设计](ARCHITECTURE.md)
- [实施顺序、测试与交付门槛](IMPLEMENTATION_PLAN.md)
- [基线提交](https://github.com/basumobai/learning/commit/6dbbe97ebbeb9a6468e670753b6c0bfd4a13554d)

## 1. 结论与范围

现有平台已经有可复用的账户安全、模型权限组、任务提示词、PDF OCR/题目答案配对、草稿审核、六科练习与多端基础。主要问题在于这些模块围绕“成对 PDF → 教材章节 → 题目”连接，尚不适合管理通用高中资源。

推荐保留 Next.js、Go、PostgreSQL、Redis 和 Compose，以现有表和授权逻辑为基础改造。先补模型目录与路由，再重构管理入口和认证交互，接着建立独立 Worker 与资源状态机。A→B 与 B→C 必须成为不同任务，不能把现有识别函数直接改名为轻量分类。

本次逐项检查：仓库及分支、全部 PR 元数据、0001–0007 迁移、Go 路由与认证、服务商/模型/身份组、任务调度、PDF 本地与 WebDAV 导入、OCR/归类/目录/提取/审核、六科题型、后台与共享前端组件、部署脚本和现有测试。未发现仓库内 `AGENTS.md`。这是源码与隔离基线检查，不是对线上数据或真实服务可用性的认证。

## 2. 当前技术基线与 PR

| 项目 | 实际代码 | 依据 |
| --- | --- | --- |
| 前端 | Next.js 16.3.6；React 声明 `^19.0.0`；TypeScript 声明 `^5.9.0` | `apps/web/package.json`；安装以 lockfile 为准 |
| 后端 | Go 1.26.0；标准库 `net/http` / `http.ServeMux`，没有 Gin | `services/api/go.mod`、`cmd/api/main.go`、`routes.go` |
| 数据库 | PostgreSQL 17，pgx/v5 5.9.2；SQL 事务迁移 | Compose、go.mod、`migrate()` |
| Redis | Redis 7 容器已配置；关闭 RDB/AOF，未挂持久化卷 | `docker-compose.yml` |
| 实际任务队列 | PostgreSQL `learning_jobs` + API 进程内两个 goroutine | `learning_jobs.go` |
| PDF | Poppler；原 PDF、页图和插图存 PostgreSQL `bytea` | API Dockerfile、`learning_pdf.go`、迁移 0004 |
| 本地资料目录 | `LEARNING_PDF_DIR` → `/data/pdf:ro`；只读挂载可配置 | Compose、`learning_local.go` |
| AI | OpenAI 兼容 Chat Completions、TTS；可选 Codex runner | `provider.go`、`learning_ai.go`、`docker-compose.codex.yml` |

任务书中的“继续使用 Gin”与仓库不一致。本轮建议继续现有 `net/http`，逐步抽取服务层；为功能升级同时更换 HTTP 框架会扩大权限、Cookie、CSRF 和兼容测试范围，没有必要收益。

已核实 PR #1–#10 全部合并；审计时开放 PR 为 0。分支列表仍保留旧开发分支，部分采用 squash 合并，不能仅凭 Git 祖先关系认定它们有待移植代码。

| PR | 已合并内容 | 对本轮的影响 |
| --- | --- | --- |
| #1 | 账户、身份组、AI 与原单词学习 | 复用认证、密钥加密、FSRS 与模型授权 |
| #2 | Apple 风格前端 | 复用基础视觉与导航 |
| #3 | 六科题库、PDF 配对、动态讲解 | 复用内容结构与提取器，改变资源流程 |
| #4 | 按需讲解、身份组内选模型 | 保留答错/主动求助触发规则 |
| #5 | 模型范围总开关与批量授权 | 扩展现有选择器，不再造一套 |
| #6、#8、#9 | 安装、仓库名文档、端口设置 | 保留升级路径、端口和原环境密钥 |
| #7 | 多设备学习操作 | 保留设备回归，补后台资源场景 |
| #10 | 部署修复、注册、PDF 归类、广东听说 | 本次最新基线，不能遗漏或按旧版本重写 |

## 3. 功能覆盖与差距

| 能力 | 当前状态 | 本轮处理 |
| --- | --- | --- |
| 服务商与加密 API Key | 已有；保存不验证真实连接 | 增加独立连接测试和模型目录同步 |
| 自动获取模型 | 未实现；没有服务端 GET 上游 `/models` 的发现流程 | 新增目录适配器，保留手动备用 |
| 模型多选/按服务商与类别授权 | 已有 `ModelScopeSelector`、`group_models` | 复用并扩展排序、模态、价格与来源 |
| 批量启用/禁用模型 | 当前逐项保存；授权勾选不等于模型启停 | 新增原子批量操作与影响预览 |
| AI 任务配置 | 已有独立提示词、模型数组、版本和作文评审/主审 | 增加有序候选、失败策略、能力/预算校验 |
| 首选失败后备用 | 未实现；非作文任务把数组裁为第一项 | 显式实现，不能把并行评审误作备用池 |
| 身份组权限 | 平台模型有服务端授权校验，学生选择前后核验 | 统一所有执行入口及降级路径 |
| A/B/C 区 | 未实现；只有识别建议、范围确认、目录与草稿 | 新增阶段状态机与独立分类/审核状态 |
| 单份原始 PDF | 现有流程要求题目/答案成对；可重复选择同一 PDF | 单文件为默认，答案附件可选 |
| 服务器目录扫描 | 单层列出最多 500 个 PDF，手动选择成对导入 | 增量扫描、去重、任务化；保留只读目录 |
| 内容归类 | 有学科/版本/册次/简类和逐字依据校验 | 改造为有预算的多维轻量分类 |
| OCR | 已有逐页 OCR 和已完成页保留 | 分离抽样 OCR 和深度 OCR，加入质量/版本键 |
| 教材外专题、知识点 | 没有独立专题/知识点关联表 | 新增最小术语与多对多关系，教材不再必填 |
| 深度解析 | 目录→题干→答案→配对；同一题组保留材料/小题/图 | 复用分步提取，按资源类型分策略 |
| 题库审核 | 单题 JSON 编辑、草稿/发布、修订号和账户审计 | 增加可视编辑、批量审核、逐项来源和审校记录 |
| 真题真实性 | 文件名/模型建议不能证明来源真实 | 保存来源核对状态与证据，不自动认证“官方真题” |
| 云盘 | 已有加密 WebDAV、目录读取、成对 PDF 同步下载 | 保留为单独适配器；新增分享链接适配 |
| 夸克/百度分享 | 未实现 | P5 先验证授权目录、选择性下载，再接 A 区 |
| 后台任务恢复 | 有去重键、领取锁、超时回收、generation | 不足以满足独立进程、暂停、取消、自动重试、心跳 |
| 多端 | 已有移动/平板/桌面、键盘和触控回归 | 新模型目录、资源批量与对照审核仍需补测 |

## 4. 必须先解决的具体问题

以下为源码路径确认的行为；未在本轮调用真实付费 AI 或网盘复现。严重级别针对 v2 要求与运行风险，不表示已发生生产数据事故。

| 编号 | 优先级 | 代码证据与实际影响 | 设计去向 |
| --- | --- | --- | --- |
| F01 | 高 | `identifyPDF()` 首先调用 `recognizePDF()`，后者读取全部 pending 页，包含题目与答案。轻量归类前就可能全文 OCR | P4 独立抽样器，禁止调用全文入口 |
| F02 | 高 | `identifyPDF()` 对题目所有页组装文本：前四页各最多 4000 字符，其余每页 500；无总 token/费用预算 | P4 页数、字符、token、图片、耗时共同限额 |
| F03 | 高 | `uploadLearningDocument()`、本地/WebDAV 导入均强制两份 PDF；数据库两列均 NOT NULL；提取要求每节 1–8 页题目和答案 | P3 单源文件+可选答案；P6 允许缺答案草稿 |
| F04 | 高 | `confirmDocumentScope()` 调用 `ValidateCurriculum()`，所有资源必须有版本/册次；英语必须北师大；`outlinePDF()` 始终要求教材章/Unit/Lesson | P3/P4 先区分资源类型，教材字段可空 |
| F05 | 高 | `learning_questions.chapter_id` NOT NULL；练习选择 INNER JOIN 章节；没有知识点多对多。仅放开列约束会让无章节题目在查询中消失 | P7 同时改约束、查询、前端筛选和练习契约 |
| F06 | 高 | `taskModels()` 非作文直接 `ids=ids[:1]`，没有回退；`completion()`/`learningCompletion()` 不记录 usage、没有输出 token 预算 | P1 统一路由与调用记录，P4/P6 预算接入 |
| F07 | 高 | Worker 在 API 内启动；固定两个工作协程；Redis 无客户端依赖/URL，未参与队列 | P3 独立进程，数据库真值+Redis 通知调度 |
| F08 | 高 | 任务 10 分钟超时、12 分钟回收；无租约心跳。generation 仅保护最终任务行，OCR/目录/题库中途写入未统一绑定 generation 或解析版本 | P3 所有副作用写入前校验租约与资源版本 |
| F09 | 高 | `extractChapterStages()` 要求所有题目完整配对答案，`ValidateQuestion()` 对草稿也要求答案/解析齐全 | P6 拆开提取合法性和发布完整性；不确定留空审核 |
| F10 | 高 | `bootstrap()` INSERT 不设置 must_change_password；该列默认 false。管理员新建/重置他人账号才明确设 true | P2 修复首次初始化；不得批量强制已正常改密用户 |
| F11 | 中 | `adminPassword()` 被服务商、模型、组、词库、普通任务/资源管理等重复调用；两个管理页常驻密码输入，传到子组件 | P2 会话+CSRF+角色授权；仅高风险按动作重新认证 |
| F12 | 中 | `source_fingerprint` 包含标题、学科、教材、学期和两个文件哈希；重命名同内容可再次建档，不区分内容去重与来源去重 | P3 内容哈希、资源版本、来源记录分开 |
| F13 | 中 | 分类仅 textbook/exercise/exam/unknown；high/medium/low 为模型自述；未存字段级置信度、覆盖率和规则版本 | P4 证据、覆盖范围、分类修订；不转成虚假概率 |
| F14 | 中 | 现有 WebDAV 下载在 HTTP 请求内逐份读取，单份 32MB；无分享预筛选、断点/流量控制、下载取消 | P5 独立下载 Worker，显式批准的文件集合 |
| F15 | 中 | 资料/草稿列表最多 100 条；目录最多 500 条；没有分页后的全库筛选和跨页批量语义 | P3/P8 服务端分页、聚合筛选与范围快照 |
| F16 | 中 | 管理数据分 `/admin` 和 `/admin/learning`；后台审核主要是 JSON 文本编辑、原页另开标签 | P2 统一管理布局；P7/P8 PDF 对照、表单编辑 |
| F17 | 中 | 题型与 `PolicyYear:2026` 写在 Go switch；来源 URL 不等于每学科每题型都被该文件证明 | P7 规则版本化、来源到字段级、未核实配额留空 |
| F18 | 中 | 只有其他管理员网页重置途径；环境初始密码不重置已有账号，未提供完备的单管理员交互恢复命令 | P2 受控服务器 CLI，撤销会话并写审计 |
| F19 | 中 | 现有 personal API 允许普通用户配置任意模型字符串，与“只能使用组授权模型”的新规则存在产品边界差异 | P1 明确执行策略，保留配置，不作为隐式绕过或回退 |
| F20 | 中 | 管理资料为管理员共享列表，相关任务却按创建者 owner 查询；跨管理员查看/接手不完整 | P3 资源管理域授权与学生私有任务分别处理 |
| F21 | 中 | `saveLearningQuestion()` 原地增加 revision；`loadAttempt()` 只 JOIN 当前题目版本，旧版本练习会提示重新选题 | P7 不可变题目修订快照，保留历史作答可读性 |

关键源码入口：

- [模型保存与重复认证](../../services/api/cmd/api/admin.go)、[安全 HTTP 客户端与身份组校验](../../services/api/cmd/api/provider.go)
- [任务池与模型调用](../../services/api/cmd/api/learning_ai.go)、[现有模型选择器](../../apps/web/app/components/ModelScopeSelector.tsx)
- [轻量归类的实际调用链](../../services/api/cmd/api/learning_classification.go)、[全文 OCR](../../services/api/cmd/api/learning_ocr.go)
- [PDF 成对导入/目录/题目审核](../../services/api/cmd/api/learning_pdf.go)、[答案配对](../../services/api/cmd/api/learning_extract.go)
- [任务领取与恢复](../../services/api/cmd/api/learning_jobs.go)、[本地目录](../../services/api/cmd/api/learning_local.go)、[WebDAV](../../services/api/cmd/api/learning_cloud.go)
- [认证与首次初始化](../../services/api/cmd/api/auth.go)、[题库与练习查询](../../services/api/cmd/api/learning.go)
- [题型/教材规则](../../services/api/internal/platform/curriculum.go)、[题目校验](../../services/api/internal/platform/content.go)、[数据库基线](../../migrations/0004_learning_platform.sql)

## 5. 外部能力核实与边界

核实时间同审计日期。以下只说明设计依据，不表示已完成真实账号联调。

| 依赖 | 已核实 | 决策 |
| --- | --- | --- |
| OpenAI 兼容模型列表 | 官方有 GET 模型列表；通用兼容方未必返回上下文/价格 | 优先 `<base>/models`；缺字段显示未知，404/405 可进入手动备用 |
| OpenRouter | 官方目录含 ID、上下文、模态、价格等；模型端点详情另有供应商/性能信息 | 列表快照与实际可调用测试分开；保存来源/时间/单位 |
| OpenList 百度 | 文档有授权令牌和官方下载方式 | 可以评估已有授权存储；不能据此承诺任意分享链接导入 |
| OpenList 夸克 | 文档明确旧逆向接口不主动维护，传输有本地代理限制 | 不作为无条件可用依赖；P5 必须单独验证分享目录/选中文件链路 |
| 广东 2026 招生规定 | 语数外全国卷、广东选择性考试与英语单独听说、计分规则有官方依据 | 足以证明考试类别/时间/总分，不能证明具体题目配额、未来年份或某 PDF 真伪 |
| 北师大教材 | 出版方存在电子教材入口 | 本轮未逐册核验全目录，不预装猜测 Unit；P7 逐册据合法原件录入 |

特别区分：OpenList 项目/文档仍有活动，不代表每个驱动都受到主动维护；“挂载自己的网盘”“读取他人的分享目录”“选择性下载 PDF”是不同能力。未使用真实夸克/百度分享链接或凭据，P5 仍有外部依赖风险。

来源：

1. [OpenAI 模型列表](https://developers.openai.com/api/reference/resources/models/methods/list)
2. [OpenRouter 模型目录](https://openrouter.ai/docs/api/api-reference/models/list-all-models-and-their-properties)、[模型供应商端点](https://openrouter.ai/docs/api/api-reference/endpoints/list-all-endpoints-for-a-model)
3. [OpenList 百度驱动](https://doc.oplist.org/guide/drivers/baidu)、[夸克驱动及维护说明](https://doc.oplist.org/guide/drivers/quark)（所见页面标注 2026-10-06 构建；未据此认定最新发行版）
4. [广东省 2026 招生工作规定，教育部门公开 PDF](https://szeb.sz.gov.cn/attachment/1/1713/1713627/12781791.pdf)（PDF 物理页 3–5）
5. [北师大出版方电子资料入口](https://www.100875.com.cn/show/downloadAnnouncement.html)
6. [Redis XAUTOCLAIM](https://redis.io/docs/latest/commands/xautoclaim/)（仅作调度恢复机制依据）

## 6. 本轮验证结果

| 验证 | 本轮结果 | 覆盖边界 |
| --- | --- | --- |
| `npm ci --ignore-scripts --no-audit --no-fund` | 成功 | 依 lockfile 安装，未改依赖声明 |
| `npm test` | 17/17 通过 | 原有状态、模型选择范围、代理与公式安全等 |
| `npm run build` | 通过，包含 TypeScript 检查 | Next.js 16.3.6 生产构建；不是线上页面验收 |
| `bash scripts/test-install.sh` | Shell 基础检查和 15 项 Python 隔离回归通过 | Docker 为模拟，未运行真实 Compose 安装 |
| `python3 -m unittest discover -s services/codex-runner -p 'test_*.py' -v` | 2/2 通过 | runner 输入限制；未调用真实 Codex |
| Go/race/真实 PostgreSQL/Compose/浏览器 | 本轮未本地重跑 | 当前环境无 Go、Docker；已有 CI 另列，不能混为本轮执行 |
| 真实 PDF/AI/网盘八场景 | 尚未执行 | 当前只有原项目小型测试 PDF 和模拟供应商；不能冒充用户给定教材或 2025 真题 |

已直接查询基线提交对应 [GitHub Actions #38940786244](https://github.com/basumobai/learning/actions/runs/38940786244)：installer、api、web、integration 四个 Job 全部 success。工作流包含 Go race/数据库测试、vet/build、浏览器模拟 API 和一次性 Compose 冒烟；这是远程 CI 证据，不等于真实 AI、真实网盘或生产部署验证。

## 7. P0 决策与下一阶段边界

P0 已形成可实施方案：复用现有数据和安全基础，资源阶段、分类、解析修订、审核分别建模；现有任务表扩展后由独立 Worker 执行；PostgreSQL 保留任务真值，Redis 只负责可重建的调度通知。首版沿用数据库原件存储，避免同时引入第二套必须协调备份的文件系统。

P1 从模型发现/目录/批量选择和统一授权路由开始。P3 的资料与任务基础通过恢复测试后，P4 才替换归类链路；P5 网盘能力必须经过授权样例验证；P6/P7 才开放深度解析与审核发布。所有新增能力默认标注阶段和开关，未通过相应测试不得宣称已完成。
