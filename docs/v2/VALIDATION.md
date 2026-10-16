# Learning v2 implementation validation

This branch implements P1–P8 on top of the P0 design. Results below distinguish
local checks, CI with PostgreSQL/Compose, and integrations requiring real accounts.

## P1 — model discovery and task routing

- Added bounded authenticated GET discovery, same-origin pagination, catalog
  snapshots, metadata provenance, explicit selection and bulk state operations.
- Connected the shared scope selector to the real identity-group and task screens;
  saving a provider opens discovery. Manual model entry and TTS remain available.
- Added explicit ordered fallbacks, finite attempts/time/input/output budgets,
  per-call usage records and transient concurrency protection. Legacy task pools
  retain fallback disabled until the administrator enables it.
- Local: Go 1.26 `go test -race -count=1 ./...` passed (database-dependent cases
  skipped without TEST_DATABASE_URL); Web 17 tests and production build passed.
- New tests cover metadata absence, authenticated discovery, unsafe pagination,
  bounded attempts/concurrency and a PostgreSQL-backed revoked fallback scenario.
- Real supplier credentials and throughput/voice quality were not tested. CI must
  execute database integration and migration checks before review readiness.

## P2 — administration and account safety

- Ordinary model/resource/vocabulary/registration operations now use the current
  administrator session, CSRF and fresh role check. Account mutations retain
  password reauthentication; existing password JSON fields remain accepted for
  old clients without being required for routine changes.
- Shared administrator navigation and deep-link handling preserve existing URLs.
- Bootstrap now requires initial password change. Server-only interactive reset
  hides password input, confirms the target, revokes sessions, requires change and
  audits recovery; disabled accounts remain disabled.
- Added group edit revisions and regression tests for routine management without
  a password, CSRF/learner rejection, recovery and initial password gating.
- Local Go race/unit and Next production build passed. Database regressions are
  included in CI; no production account was modified.

P1 CI with PostgreSQL 17, race tests, browser mocks and complete Compose passed: https://github.com/basumobai/learning/actions/runs/38948876823 .

## P3：资源状态、原件与任务

- 单PDF和旧双PDF入口共享内容哈希去重；原件使用不可变blob，来源独立保留；旧PDF字段仍可读取。
- 新工作台提供A/B/C/正式库阶段、筛选、分页、上传、增量扫描、人工多维分类与任务控制。独立专题无需教材Unit。
- Worker独立Compose服务；PostgreSQL租约/代次与Redis通知配合。Redis不可用时数据库轮询恢复；暂停、取消和旧代次写入校验覆盖中间写入。
- 新增数据库测试：去重/原件不可改删、无教材专题、权限与版本冲突、暂停恢复、过期租约重领、重复扫描。
- 本地Go编译/无数据库测试、前端17项单元测试及Next生产构建通过。数据库与浏览器结果以本阶段远程CI为准。
- P2远程CI：API数据库、安装器和Compose通过；浏览器脚本遗留重复密码框断言已在P3修正。

## P4：有界内容抽样与多维分类

- A→B使用pdfinfo和指定物理页的pdftotext，不调用旧preparePDF/全文OCR。覆盖封面、前页、中部及尾部；默认最多6页、10000字符、2页OCR、90秒。
- 正文规则优先；不能确认时调用独立分类任务。文件名不决定学科和真题真实性。引用须可在抽样正文逐字核实；未知或低置信度留A区。
- 结果包含资源类型、教材同步状态、专题、多知识点、可能章节及考试元数据。独立专题不绑定Unit；真题真实性始终待人工核验。
- 配置支持版本冲突检测、抽样/OCR/token/时间/费用/重试预算。累计预留包含失败与降级调用；未知费用不会冒充可验证美元上限。
- 新测试覆盖代表页上限、正文覆盖错误文件名、英语混合专题、物理专题、引用造假、累计预算、缓存及未触发全文准备。
- 本地Go测试及Next生产构建通过；P3远程发现的Go ServeMux路径冲突和旧分类界面禁用条件已修复，新增无数据库路由注册测试防回归。

## P5：选择性网盘导入

- 复用learning_cloud_connections的加密凭据，新增OpenList适配类型，保留WebDAV兼容。
- 夸克/百度链接识别、目录快照、元数据AI预筛选、显式文件批准、大小/版本/hash核验、流量预留/限速、任务控制及原件进入A区已实现。
- 原生任意分享链接解析**未实现**。当前需管理员在合法授权的OpenList实例完成分享挂载，再指定目录；百度分享插件仍属测试版本，不宣称兼容所有分享。无Cookie、API Key或提取码进入AI输入。
- 下载默认仅允许同源代理，其他下载主机需显式白名单；安全HTTP传输拒绝内网与重定向；响应中的HTML header不作为HTTP凭据转发。
- 新测试使用受控OpenList响应，验证仅取已选PDF、快照冲突、越界/SSRF、保密和导入A区；未使用真实网盘账号，真实授权及驱动兼容待验收。
- P4远程浏览器和完整Compose通过；遗留交互任务重试时间与安装器四服务假设已修正为立即重试及五服务验收。

### OpenList核查来源（2026-10-16）

- 官方API说明：https://doc.oplist.org/api/apidocs
- 目录/文件接口实际源码：https://github.com/OpenListTeam/OpenList/blob/main/server/handles/fsread.go
- 百度授权：https://doc.oplist.org/guide/drivers/baidu
- 夸克授权：https://doc.oplist.org/guide/drivers/quark
- 百度分享测试插件：https://github.com/OpenListTeam/openlist-baidushare-plugin

## P6：差异化深度解析与断点恢复

- 根据教材/专题/试卷/混合类型选择解析策略；完整逐页读取采用两页小批次及一页续接上下文，扫描页按需OCR，保留图表bbox、共用阅读材料和多小题。
- 正文/题目与原始参考答案使用独立任务；不从题干生成答案。跨页答案仅在原题号与学科唯一匹配时关联，缺失或多义匹配保留待审。
- 新增逻辑内容单元和解析版本。原PDF不拆改，页码和逐字引用可核对；插图需在有效页码及归一化坐标范围内。
- 页识别、题目提取和完整批次分别保存检查点。任务重试不会重新提取已完成题目，部分结果可先查看。
- 累计token/费用预留持久化到任务，重启/重试不会重置；资源并发数通过数据库串行领取及配置控制。
- 对照工作台适配移动端切换原文/结果及桌面并排，支持查看不同解析版本和任务恢复。
- 新测试：四种策略、未知答案/题型草稿可保存但不可发布、伪造证据拒绝、参考答案接口超时后的断点恢复和无提前发布。
- 本地Go及Next构建通过。真实教材、扫描件、付费模型的准确率和成本尚需真实样本联调，合成夹具不能替代该验收。
