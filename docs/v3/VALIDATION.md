# v3.1 验证记录

验证日期：2026-10-17。基线：main@de6f5ad8325412027eca9b037113f4ebfdf32d76。用户本次请求为实施授权；压缩包四份S0原稿逐字保留，原稿中的“未执行”属于当时审计事实，本文件记录后续实际实施结果。

代码采用S1→S8独立依赖PR，不合并#13/#15，不连接或部署生产。新功能开关默认false，须按 OPERATIONS.md 审核、隔离验收后逐项启用。

## 完整GitHub CI

下表为各阶段实际测试过的代码提交；本记录后续仅文档更改。每次完整CI均包含installer、API、Web、integration四个成功job。

| 阶段 | PR | 已测试代码提交 | 完整CI |
| --- | --- | --- | --- |
| S1 | [#16](https://github.com/basumobai/learning/pull/16) | `0b44cd44c6f5` | [通过](https://github.com/basumobai/learning/actions/runs/39016397672) |
| S2 | [#17](https://github.com/basumobai/learning/pull/17) | `9ec8a83503b5` | [通过](https://github.com/basumobai/learning/actions/runs/39016562897) |
| S3 | [#18](https://github.com/basumobai/learning/pull/18) | `99f00bce047c` | [通过](https://github.com/basumobai/learning/actions/runs/39016987233) |
| S4 | [#19](https://github.com/basumobai/learning/pull/19) | `c583d5a2532d` | [通过](https://github.com/basumobai/learning/actions/runs/39017063419) |
| S5 | [#20](https://github.com/basumobai/learning/pull/20) | `776b58fa083a` | [通过](https://github.com/basumobai/learning/actions/runs/39017322805) |
| S6 | [#21](https://github.com/basumobai/learning/pull/21) | `1299091cb93e` | [通过](https://github.com/basumobai/learning/actions/runs/39017687534) |
| S7 | [#22](https://github.com/basumobai/learning/pull/22) | `9badac2aa358` | [通过](https://github.com/basumobai/learning/actions/runs/39018193990) |
| S8 | [#23](https://github.com/basumobai/learning/pull/23) | `dd4f7d06bbaf` | [通过](https://github.com/basumobai/learning/actions/runs/39019469408) |

S8完整运行 39019469408 的实际证据：

- PostgreSQL17：`go test -race -count=1 -v ./...`、Go vet/build通过。数据库用例实际执行，覆盖增量迁移、并发幂等、不可变原题/评分、用户隔离、提示/复答/迁移分离、输入失误更正、前置推荐/重放/循环拒绝、草稿CAS、考试截止、RAG引用/越权、人工评分与质量门槛、mock ASR等。
- Web：22项单元、生产构建通过；Chromium mock-API执行34项学习UI、652项设备检查（10种视口、320–2560宽度）、49项注册/PDF/OCR/广东听说/恢复流程、104项资源UI检查；弱网测试验证冻结原载荷跨刷新、同键重放和确认成功后清理私有草稿。
- Installer：生成密钥、权限、重复执行等静态检查通过；一次性完整安装验收覆盖初装、重复安装、更新、凭据冲突、恢复、缺配置、账号与AI密文保留和五服务健康。
- Docker Compose：完整栈冒烟、词汇FSRS/反馈/重试/撤销、资源去重/独立Worker/原件与PDF代理通过。实际备份使用同一数据库快照，恢复到新建随机数据库，核对账号、题目修订、原答、FSRS、学习证据、资源/录音、PDF归档和原加密密钥；成功后删除一次性副本。没有对生产库执行恢复。
- 本地Go 1.26.0 `go test ./...`、`go vet ./...`，Node24单元/TypeScript检查通过；本地未配置PostgreSQL，数据库用例跳过，数据库结论以以上真实CI为准。

## 已修复的回归

S3首轮发现旧浏览器mock缺失新DTO，补兼容与夹具后通过；S6发现JSX闭合错误，修复后通过。S8首轮发现新页面导航激活状态与Docker目录夹具权限问题，随后发现注册成功后不应执行登录会话检查；均修复并在上述完整CI重跑通过。失败运行未作为通过证据。

S8集成复查同时修正：同一次作答多个小题按一次独立证据聚合；新评分规则必须与原题参考答案一致；模型供应商配置变化使旧质量审批失效；推荐实际使用审核前置关系、学生确认错因、难度/时间与科目题型分布，并记录对应修订和策略版本。

## 外部与生产验收边界

以下不是自动测试通过项，也不构成上线批准：

| 项目 | 实际状态 |
| --- | --- |
| 北师大2019具体Unit、六科教材知识映射 | 未安装无授权材料；需合法材料及人工逐条核验 |
| 广东具体年份/卷型/分值与正式对齐 | 接口与合成规则已测；真实年份来源仍需人工核验 |
| OpenList/夸克/百度等真实网盘 | 未使用真实授权样本，不能以mock冒充通过 |
| 付费AI/ASR教学准确度、真实音频、TTFT、费用 | 未调用付费模型；mock仅验协议/权限/重放，教学效果与供应商账单未验收 |
| 专业发音评分、一般符号代数等价 | 未启用；ASR只提供待核对转写，数学仅支持受限有理数/分数确定性等价 |
| 生产备份恢复与真实业务容量 | 未连接生产；只在一次性完整栈验证备份恢复，不宣称已完成生产恢复或容量验收 |

没有复制受限教材或外部源码，未新增Go/npm依赖，未替权利人增加全仓LICENSE。来源详见 SOURCE_AND_LICENSE_NOTES.md。
