# Learning v2：资源处理与升级

当前实现以仓库原有 Next.js / TypeScript、Go `net/http`、PostgreSQL、Redis 和 Compose 为基础。没有切换到 Gin，也没有替换现有单词复习、教学和练习系统。

## 管理入口

| 入口 | 用途 |
| --- | --- |
| `/admin` | 服务商自动发现模型、模型启停、身份组、用户与安全 |
| `/admin/learning#tasks` | 任务候选池、顺序、备用模型、时间/token/费用策略 |
| `/admin/resources` | 上传、扫描、A/B/C/正式库、筛选、批量专题、分类修正 |
| `/admin/resources/shares` | 授权 OpenList 分享目录与选择性导入 |
| `/admin/resources/{id}` | 原文页图/PDF与解析对照、修正、退回、发布 |
| `/admin/resources/config` | 处理预算、真实教材目录、知识点、考试规则修订 |
| `/admin/resources/jobs` | 任务记录、暂停/取消/恢复、Worker与Redis状态、旧原件迁移 |
| `/resources` | 学生浏览已审核正式内容；题目继续进入现有学科练习 |
| `/admin/learning` | 保留旧双PDF、WebDAV、Codex及词汇入库兼容入口 |

普通模型、标签、配置和审核操作使用当前管理员会话与服务端角色/CSRF校验。修改账户等敏感操作保留重新认证。新建初始管理员首次登录必须修改密码；忘记密码使用 [部署文档](../DEPLOY.md) 的交互恢复命令。

## 首次配置顺序

1. 保存 AI 服务商后打开模型目录；选择模型，再给身份组授权。目录没有提供的模态、上下文、价格保留为未知。手动添加仍可作为备用。
2. 按任务分配模型：`classify_document` 为 A→B，`ocr` 理解扫描页，`deep_parse` 为教材/混合单元，`extract_questions` 为专题/试卷题目，`extract_answers` 为原参考答案，`knowledge_classify` 可选独立知识点映射，`share_prefilter` 只处理网盘元数据。其他教学/作文任务保留独立候选池。
3. 教材和知识点配置只录入可核验内容。英语教材目标为北师大版2019；独立专题和试卷不必绑定版本、册次或Unit。
4. 文件进入A区后执行轻量识别。只有少量代表页会被读取；证据不够时留A区待确认。达到阈值的分类进入B区；管理员也可以人工确认。
5. B区选择深度解析进入C区。完整解析分批读取、按需OCR、保存章节/正文/题组和原始答案依据。没有答案或分值的结果仍可保留待审。
6. C区对照原文修正，再提交审核发布。后台校验失败不会改变正式内容；任务详情显示原因。真题还需人工核验来源及适用年份的考试规则。

## 阶段、原件与版本

- 阶段在数据库中保存，学科、资源类型、专题、知识点和教材章节独立关联。修改标签不移动或覆盖PDF。
- 原件使用SHA-256寻址的不可变blob；相同内容复用资源，保留不同来源。双PDF与单PDF具有不同的资源组合键，不假装没有答案的文件具备答案。
- 深度解析有独立版本、页面/题目/知识点/答案检查点。暂停、超时和重启之后恢复已完成步骤；资源或解析配置改变时要求新建任务。AI分类同样推进资源版本，旧解析/发布随之失效；同一资源的后台处理按租约串行执行。
- 正式内容使用发布快照。可以逐批发布；显式替换旧正式版本前，当前解析的其他单元必须已审核或退回。
- 已开始的练习按原题版本和原图片评分。迁移无法恢复之前已被覆盖且从未保存的历史题目，不伪造这些版本。
- 页面图片由后台深度解析生成，手机可直接查看与放大；PDF浏览器和原件链接保留。原件支持范围请求，同站PDF对照允许嵌入，其他页面仍禁止被第三方嵌入。

## Worker、预算与恢复

Compose包含 `postgres`、`redis`、`api`、`worker`、`web` 五个常驻服务。API只处理短请求；扫描、下载、OCR、分析、发布图片处理均在独立Worker执行。开发时可显式设置 `LEARNING_EMBEDDED_WORKER=true` 使用兼容嵌入模式。

PostgreSQL记录任务、租约、代次、控制状态和通知outbox，Redis Stream负责通知唤醒。Redis不可用时数据库轮询仍可领取任务；失去租约、取消或资源版本变化的旧Worker无法提交中间结果。默认租约90秒，10秒续约。资源并发可配置1–8，领取受数据库锁协调；AI有必要的并发保护，没有每日调用次数配额。

默认A→B最多6页、10000字符、2页OCR、90秒；深度解析默认最多800页、30分钟。当前只处理PDF，Word/PPT/独立图片导入尚未实现。文件每份最大32MiB，目录扫描每次5000项/8层；大批资料应分目录导入。网盘目录最多100个目录、5000文件，人工批准每批1–100个PDF，流量上限2GiB，限速64KiB/s–20MiB/s。

累计token和已知费用采用保守预留，包含失败、备用调用和重试，持久化后不会因重启重置。预留不等于实际账单。美元上限只在价格来源、单位和更新时间可核验时执行；当前无法验证图片计费上限时会停止调用，请调整预算或处理方案，不承诺虚假的费用上限。可核验价格需24小时内刷新。新资源流水线使用OpenAI兼容API执行这些限制，Codex保留在旧兼容入口。

网盘失败重试同样消耗批准流量。已完整导入的文件会跳过；中断的未验证文件不入库。流量不足时必须重新批准，不擅自扩大下载范围。

## 网盘接入边界

**已实现的是授权OpenList挂载目录适配器，未实现任意夸克/百度分享链接的原生直读。**

先在管理员合法授权的OpenList实例完成分享访问和挂载，再在Learning配置公开HTTPS基础地址、授权Token和允许根目录，提交原分享链接及其挂载目录。分享提取码加密保存；实际分享鉴权由已授权的OpenList挂载完成。无法挂载的分享不能在Learning中自动下载。

建议启用OpenList下载代理；默认仅允许同站下载，直链主机需明确列入白名单。下载HTTP客户端检查公开地址并拒绝重定向，不转发OpenList管理员Token到下载主机。百度需要的下载UA按驱动限制设置。Cookie和云盘账户凭据留在OpenList；Learning的Token与提取码不会送入AI。

AI只得到文件ID、相对路径/文件名和大小；建议不会自动批准下载。模糊文件默认等待人工选择。刷新目录生成新快照，旧选择不能在新快照中直接使用。

OpenList百度分享插件仍为测试版本，其可用性不等于普通百度账户挂载。真实夸克、百度账号、提取码、会员/流量约束及具体驱动版本须部署方联调，未宣称全部分享兼容。来源见 [验证记录](VALIDATION.md)。

## 升级与数据迁移

1. 备份数据库、`.env`和原PDF目录。保留 `APP_ENCRYPTION_KEY` 及数据库密码，按原安装流程更新独立测试环境。
2. API执行0008–0015增量迁移；旧表、资源ID、接口、PDF字段、词汇和学习记录保留。题目快照只依据可证实的当前数据建立。
3. 确认五服务健康及后台Worker心跳。旧PDF在迁移前仍可读取；大规模重复导入前，先在任务页执行“迁移旧入库原件”。
4. 每批最多迁移1000个旧资源，可重复执行。共享blob去重；原字段、旧ID与历史关联保留，已有重复ID仅标记 `duplicate_of`，可通过筛选显示，不合并或删除旧记录。
5. 在测试环境核对模型授权、现有练习、原件哈希、阶段数量、历史题目、任务暂停与重启恢复，再决定生产升级。本开发未部署生产，也未合并main。

直接退回旧应用镜像时保留增量表和新Worker停止状态，先验证旧版本能读取当前数据。数据库恢复以经过演练的备份为准，不执行破坏性自动down迁移。

## 接口约定

新增管理接口位于 `/api/v1/admin/`，沿用现有会话、CSRF和管理员权限。常用接口：

| 接口 | 用途 |
| --- | --- |
| `GET /admin/ai/providers/{id}/models` | 读取服务商模型目录 |
| `POST /admin/ai/providers/{id}/models/sync` | 同步服务商模型目录 |
| `PUT /admin/ai/model-selections`、`PATCH /admin/ai/models` | 批量选用/启停 |
| `PUT /admin/ai/task-routes` | 候选池与路由策略 |
| `GET /admin/resources`、`POST /admin/resources/uploads` | 分页查询/单PDF上传 |
| `POST /admin/resources/scans` | 配置根目录下的增量扫描 |
| `POST /admin/resources/{id}/classify`、`/deep-analyze` | 版本检查后排队 |
| `PATCH /admin/resources/{id}/tags`、`POST /admin/resources/tags/batch` | 多维标签与批量专题 |
| `GET /admin/resources/{id}/analysis` | 解析版本与分页内容单元 |
| `PATCH /admin/resources/{id}/units/{unit}` | 人工修正及单元版本检查 |
| `POST /admin/resources/{id}/approve`、`/return` | 排队校验发布/退回 |
| `GET /admin/resource-jobs`、`GET /admin/resources/jobs/{id}/status` | 任务记录/状态 |
| `POST /admin/resources/jobs/{id}/{action}` | pause/resume/cancel/retry |
| `GET /admin/runtime` | Worker/Redis/数据库恢复状态 |
| `POST /admin/resources/shares/inspect` | 识别链接并排队读取授权目录 |
| `GET /admin/resources/shares/{share}/files` | 当前目录快照 |
| `POST /admin/resources/shares/{share}/analyze`、`/imports` | 元数据预筛选/批准所选下载 |
| `GET /resources/library[ /{id} ]` | 学生正式资源，隐藏题目参考答案 |

变更资源、单元、任务、配置和规则需要携带对应revision；409时刷新后重新选择，不能自动覆盖他人的新版本。

## 验收边界

自动化覆盖合成教材、独立语法、多专题、物理、错误文件名、真题真实性防误判、选择性下载及超时/重启/重复执行。合成PDF和受控模型/OpenList响应验证程序约束，不能证明真实模型准确率。

尚未完成的外部验收：真实北师大2019教材及真实2025广东试卷逐项核验、真实扫描OCR/复杂公式跨页准确率、真实AI计费、真实网盘授权与驱动兼容。未预置完整版权教材、官方题库或已核验的全科题量规则。没有自动发布策略；当前必须人工审核。
