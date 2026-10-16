# Learning v3.1：开源来源、MIT 兼容与复用审计台账

> 核验日期：2026-10-17；当前用途：为 [LEARNING_V3_1_PLAN.md](LEARNING_V3_1_PLAN.md) 标记参考来源和法律/工程边界。**本 PR 只新增设计文档，不复制外部代码、UI 图片、品牌素材或模型数据。**
>
> 本文为工程合规流程，不构成法律意见；实际复用要以目标文件、目标提交、完整许可证及依赖链的核验结果为准。

## 1. 先澄清“符合 MIT”的含义

1. 目前 Learning 的 main 仓库根目录经检查**没有 LICENSE**，所以不能声称当前全部仓库内容已采用 MIT。公开源代码也不等于授予自由复制、改编、再发行权。
2. 用户希望未来项目保持 MIT 许可的宽松复用能力：**Learning 自己创作、拥有足够授权的原创代码可以采用 MIT**；第三方代码继续保留各自许可证与版权声明，不能将外部作品直接改标 MIT。
3. **MIT 依赖可纳入 MIT 项目，不等于能移除其 MIT 声明**。复制 MIT 源码的全部或重要部分时，必须保留完整许可文本与原版权信息；需要记录何处引用、是否修改和引用的提交。
4. 某些 Apache-2.0 组件在满足原许可义务的前提下可以与 MIT 项目共同分发，但**它们本身不会变成 MIT**。为满足本轮“来源尽可能 MIT”的诉求，暂不复制 Apache-2.0 来源代码，仅参考设计。
5. AGPL-3.0、GPL-3.0 或自定义非商用许可证代码，不得仅靠“标注来源”就复制进 MIT 原创模块。尤其 AGPL 涉及通过网络提供修改后软件时的对应源码义务，必须另行合规评估。**参考抽象产品需求/通常的交互模式，不等于复制代码**；实现时应独立设计，不搬运独创性代码、UI、素材或详细表达。
6. 新增 MIT LICENSE 应在版权拥有者确认、已有贡献与权利审计之后，单独通过一个许可证治理 PR 完成；不得把文档 PR 当作已获得所有贡献者重新许可的授权。
7. 课程教材、试卷、录音、图片、字体、用户上传资料、TTS 模型权重与 API 服务条款**不因程序采用 MIT 而自动具有 MIT 许可**，须分别处理。

## 2. 来源台账（截至核验日）

| 编号 | 项目与原始仓库 | 核验许可证 | 本计划引用的思路 | 代码复用状态 |
| --- | --- | --- | --- | --- |
| R01 | [lfnovo/open-notebook](https://github.com/lfnovo/open-notebook) | [MIT](https://github.com/lfnovo/open-notebook/blob/main/LICENSE) | 多服务商模型选择、配置测试、可检索资料、原文引用、知识对话与上下文维护 | **仅设计参考**；真正复制须逐文件登记 |
| R02 | [learningequality/kolibri](https://github.com/learningequality/kolibri) | [MIT](https://github.com/learningequality/kolibri/blob/develop/LICENSE) | 弱网/离线教学、学习资料的可靠访问与学习恢复 | **仅设计参考**；暂不引入其庞大 Python 系统 |
| R03 | [open-spaced-repetition/fsrs4anki](https://github.com/open-spaced-repetition/fsrs4anki) | [MIT](https://github.com/open-spaced-repetition/fsrs4anki/blob/main/LICENSE) | 复习调度、复习日志、对遗忘风险的长期测量和验证 | **仅算法/教学参考**；不复制调度实现 |
| R04 | [open-spaced-repetition/go-fsrs](https://github.com/open-spaced-repetition/go-fsrs) | [MIT](https://github.com/open-spaced-repetition/go-fsrs/blob/main/LICENSE) | 官方 Go FSRS v6 调度、历史重放与撤销接口 | **仓库已经通过依赖使用 go-fsrs/v4 v4.0.0**；保留许可证与 SBOM 记录，不重复手写 |
| R05 | [open-spaced-repetition/ts-fsrs](https://github.com/open-spaced-repetition/ts-fsrs) | [MIT](https://github.com/open-spaced-repetition/ts-fsrs/blob/main/LICENSE) | 复习评级预览、浏览器学习流程的设计 | **仅设计参考**；不让前端独立计算正式 FSRS 真值 |
| R06 | [learnhouse/learnhouse](https://github.com/learnhouse/learnhouse) | [AGPL-3.0](https://github.com/learnhouse/learnhouse/blob/dev/LICENSE) | 课程编辑、课内 AI、用户组、教学分析 | **只参考抽象功能**；不得直接复制源码、组件或视觉素材 |
| R07 | [frappe/lms](https://github.com/frappe/lms) | [AGPL-3.0](https://github.com/frappe/lms/blob/develop/license.txt) | 课程—章节—课时组织、测验/主观题流程 | **只参考抽象设计**，独立实现 |
| R08 | [khoj-ai/khoj](https://github.com/khoj-ai/khoj) | [AGPL-3.0](https://github.com/khoj-ai/khoj/blob/master/LICENSE) | 文档语义检索、个人上下文与 AI 问答 | **只参考产品方向**，独立实现 |
| R09 | [oppia/oppia](https://github.com/oppia/oppia) | [Apache-2.0](https://github.com/oppia/oppia/blob/develop/LICENSE) | 分支教学、通过学生回应决定下一步讲解 | 本阶段**只参考交互理念**，不复制代码 |
| R10a | [ankitects/anki](https://github.com/ankitects/anki) | GitHub 未给出统一 MIT 标识；以源文件授权为准 | 主动回忆、成熟复习操作 | **只参考产品体验** |
| R10b | [ankidroid/Anki-Android](https://github.com/ankidroid/Anki-Android) | [GPL-3.0](https://github.com/ankidroid/Anki-Android/blob/main/COPYING) | 安卓记忆体验与撤销流程 | **不复制代码或布局素材** |
| R11 | [CaviraOSS/PageLM](https://github.com/CaviraOSS/PageLM) | [PageLM Community License](https://github.com/CaviraOSS/PageLM/blob/main/LICENSE.md)，**非标准开源且含非商用、禁止再分发条款** | 文档 → 习题/闪卡/播客的产品构想 | **禁止复制代码/片段/UI/示例资源**；仅独立设计 |
| R12a | [classroomio/classroomio](https://github.com/classroomio/classroomio) | [AGPL-3.0](https://github.com/classroomio/classroomio) | AI 课程生成、学习任务和机构级后台 | 设计参考 |
| R12b | [moodle/moodle](https://github.com/moodle/moodle) | [GPL-3.0](https://github.com/moodle/moodle) | 成熟 LMS 测试、测验结构与教学管理 | 设计参考 |
| R12c | [openedx/openedx-platform](https://github.com/openedx/openedx-platform) | [AGPL-3.0](https://github.com/openedx/openedx-platform) | 课程、成绩与大规模教育流程 | 设计参考 |

**关于 R07 等 LICENSE 路径**：链接用于定位仓库许可证；个别仓库的文件名、默认分支将来可能变动。真正引入前按指定提交 SHA 重新核验，不能仅以目录或 README 的“开源”自述代替许可文本。R10a 必须进行细粒度组件确认，不得猜测为 MIT。

### 2.1 推荐直接研究的官方文档

- **模型发现/选择**：[Open Notebook README](https://github.com/lfnovo/open-notebook/blob/main/README.md)、[Open Notebook 模型设置说明](https://github.com/lfnovo/open-notebook/blob/main/docs/4-AI-PROVIDERS/index.md)。产品可参考“填写 API Key → 测试 → 选择可用模型 → 指定任务默认模型”；Learning 后端及 v2 模型数据仍是唯一真值。
- **课程内 AI 辅导**：[LearnHouse README](https://github.com/learnhouse/learnhouse/blob/dev/README.md)、[LearnHouse AI 配置文档](https://github.com/learnhouse/learnhouse/blob/dev/docs/content/self-hosting/configuration/ai-setup.mdx)。仅作为思路出处，不取用 AGPL 代码。
- **间隔复习**：[go-fsrs README](https://github.com/open-spaced-repetition/go-fsrs/blob/main/README.md)、[FSRS4Anki README](https://github.com/open-spaced-repetition/fsrs4anki/blob/main/README.md)、[ts-fsrs README](https://github.com/open-spaced-repetition/ts-fsrs/blob/main/README.md)。
- **离线能力**：[Kolibri README](https://github.com/learningequality/kolibri/blob/develop/README.md)。
- **互动学习设计**：[Oppia 仓库](https://github.com/oppia/oppia)。
- **课程结构**：[Frappe Learning README](https://github.com/frappe/lms/blob/develop/README.md)。

### 2.2 来源版本快照

下列短 SHA 为 2026-10-17 查询到的参照点，**不是复用时自动批准的合法版本**；实施前还需登记完整 40 位提交 SHA、涉及的精确文件与有效许可证。

| 来源 | 核验提交短 SHA |
| --- | --- |
| Learning/main | 6dbbe97ebbeb |
| Open Notebook | a0f6f3081ae0 |
| Kolibri | 3270adc2406a |
| FSRS4Anki | ff7c85cee735 |
| go-fsrs | 61159b9b3891 |
| ts-fsrs | c8ca282edc3f |
| LearnHouse | 2c65f6a05d75 |
| Frappe LMS | 82ad269653cf |
| Khoj | ae229ca894c0 |
| Oppia | 7132c1de45f2 |
| PageLM | a6646794661b |

## 3. 开源复用 PR 的强制登记模板

当一个 PR **实际引入**外部源码、样式、图片、示例数据、模型文件或直接改编的材料时，每一项都必须填写：

| 字段 | 填写要求 |
| --- | --- |
| Upstream URL | 原始仓库与具体文件的永久链接 |
| Upstream commit | **完整 40 位 SHA** 或固定发布版本标签 |
| Original author/copyright | 源文件头与许可证声明中的原作者、版权年份 |
| SPDX / LICENSE | 核验的 SPDX 标识、原许可证文本位置、例外条款 |
| Imported path | Learning 目标路径、实际复制/修改范围 |
| Modification notes | 哪些代码被修改、改动原因、是否含第三方/生成资源 |
| Reason for reuse | 为什么不能简单利用现有库、是否已有等效功能 |
| Dependency audit | 新增直接与传递依赖、SBOM 及不兼容问题 |
| Notice retention | 原 LICENSE、版权声明、需要的 NOTICE 是否已随发行保留 |
| Tests/security | 单元、集成、性能、权限及恶意内容检查 |
| Review signoff | 开发人员与许可审查结论，发现问题时阻止合并 |

如果**仅参考设计理念且全部代码自行独立实现**，记录来源编号与“design inspiration only / no code copied”即可，禁止在 commit 或 README 中把其误写成“基于某项目源码”。

建议新建并随实际引入变化更新 THIRD_PARTY_NOTICES.md 或 licenses/ 目录；**本次设计 PR 不创建虚构的已导入代码清单**。请记住 MIT 的核心义务是保留原版权声明与许可声明，而不是简单地添加一个 GitHub 链接。

## 4. 自有项目 MIT 化的条件与流程

**拟定独立 PR：license/mit-governance**（名称仅供建议，尚未创建）：

1. 先通过 git 历史和已有文件头审计所有贡献者的版权/授权及导入的第三方材料；明确哪些文件是原创、哪些不是、哪些权利无法确认。
2. 由有权授权的持有人决定自有部分的 MIT 许可证版权署名；添加仓库根目录 LICENSE（使用标准 MIT 文本），必要时添加 CONTRIBUTING.md 的入库许可证约定。
3. 为真正打包/修改的第三方代码保存其许可证声明；新增 THIRD_PARTY_NOTICES.md、锁定依赖版本及 SBOM 生成/CI 校验。不改变第三方原有许可证。
4. 审查字体、图标、媒体、教材/试卷、模型服务的独立版权和使用条款。教学资源应默认“管理员依法自行导入并审核”，不能因为软件 MIT 就公开再分发教材。
5. 许可证/引用检查未通过时，不对外宣称“全仓库纯 MIT”；必要时采用“**自有原创部分 MIT，第三方按各自许可证**”的准确表述。

## 5. MIT 与教学实现的架构保障

- 学习调度**沿用现有 MIT go-fsrs/v4**，仅将适合回忆的内容接入同一服务端适配层。不要把 AnkiDroid GPL 源码复制成看似自己的调度器。
- AI 文档系统从 Open Notebook 吸收“分步添加服务商/模型、检索来源引用、独立模型配置测试”的**设计经验**；沿用 Learning 的现有 Go/PostgreSQL 身份组和任务契约，不做整项目迁移。
- 教学资料 RAG 的检索分段、语义搜索、访问控制和引用页码必须为 Learning 自己建立**可验证的数据模型**；不能使用受 AGPL/自定义许可约束的源码作为实现捷径。
- 离线功能从 Kolibri 吸收“弱网优先、资源可用性明确、可恢复学习过程”的通用理念；以 Web/PWA 自行实现，并把答案保护作为第一约束。
- 分支式教学与课程分级参考 Oppia、Frappe、LearnHouse 的抽象操作流程；题型树、状态机和组件由 Learning 自主设计，UI 视觉需自有实现，不复制截图。

## 6. 验收清单：第三方能力正式落地前逐项检查

- [ ] 当天拉取 main 和 PR #11，确认 v2 是否已经实现目标能力。
- [ ] 识别此次新增功能是否确需第三方源代码，优先调用已有包与内部组件。
- [ ] 若使用外部源码，填写来源、完整提交 SHA、版权、许可、目标文件和修改说明。
- [ ] MIT 原文与原作者信息随代码保留；非 MIT 的材料未被误标 MIT。
- [ ] 不将 AGPL/GPL/PageLM 自定义许可证代码直接写入 MIT 原创模块。
- [ ] 检查新增依赖及传递依赖，确保最终发行包许可证声明完整。
- [ ] 完成无真实秘密的安全测试、功能回归、数据恢复和多端测试。
- [ ] PR 明确声明“设计参考 / 实际代码复用”的区分和当前的法律/工程限制。
- [ ] 教学内容、答案、考试录音与教材资源已经分别核验版权，不能靠程序 MIT 替代。
- [ ] 在没有证据时不声称某个项目已经实际完成、许可证已转换或已部署上线。

**本台账的原则：可以站在开源社区成熟经验的肩膀上，但必须把授权、归属、教学正确性和用户数据可靠性同样当成软件工程的一部分。**
