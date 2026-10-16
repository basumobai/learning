# AI 助学与自然语音

## 配置与权限

使用 OpenAI Chat Completions 和 Audio Speech 请求格式，基础 URL 可配置；OpenRouter 为 `https://openrouter.ai/api/v1`。服务端发送 Bearer API Key，浏览器只得到已授权模型的显示信息。聊天模型和 TTS 分别配置并分组授权，不允许客户端自行指定未注册模型、备用模型或远程地址。没有本地调用次数、对话数量或业务用量上限；供应商余额、上下文窗口、外部服务的繁忙响应仍由供应商决定。

管理页支持服务商启停/密钥更新、模型标识/用途/音色/朗读风格、权限组和用户分组。JSON mode 可选，兼容不支持 response_format 的文本模型。语音可走同一 OpenRouter 服务商，也可给独立 OpenAI 兼容服务商单独配置语音模型。

## 学习能力

- 辅导对话：面向高中基础薄弱的学生，鼓励定位词汇/句法/信息保持困难、分步解释与练习；完整对话保存在本人账户，不主动截断历史或设置轮数额度。模型自己的上下文窗口耗尽会返回服务错误，需要用户选择新对话或合适模型。
- 句子拆解：中文译文、主干、精确原文片段及语法作用、简单英语改写和理解检查。服务端要求 JSON 字段完整，片段必须真实出现在输入句子中；输出不是可信的教材语法标注，仍需学生核对。
- 听力训练：A2/B1/B2、主题、训练重点，以及广东A朗读/B三问五答/C复述或基础四选一。B验证3问5答顺序、数量及原文/计算机回答依据。交卷前不返回B/C原文、参考表达和译文，A提供必要朗读稿。作答按账户隔离，提交后不可覆盖，相同请求可恢复。录音仅本机回放，AI评价手动转写文本。PDF的OCR、目录、题干、答案与配对拆成独立任务，详见 [流程与任务表](IMPORT_AND_LISTENING.md)。
- 听音拼写：从导入词库产生听音题，服务端取得目标词合成语音；词形、音标和例句只在答后反馈显示。listening_spelling 有独立 FSRS 记忆状态。

输入和生成内容都按普通文本渲染，不执行 HTML 或模型代码。AI 不拥有数据库/账户管理工具，不处理权限和秘密，不自动把生成内容归入教材。生成的训练结构无效时明确失败，不靠随机填补答案。

## TTS

`POST /audio/speech` 使用 model、input、voice、response_format:mp3，模型支持时发送配置的 instructions。OpenRouter 文档提供 `openai/gpt-4o-mini-tts-2025-12-15` 等模型；OpenAI 文档建议 Marin/Cedar。模型与音色可自由更换，语音试听可在账户设置使用。语速通过 Web 播放器控制，避免给不支持 speed 的供应商发送该参数。页面明确标识 AI 生成语音，不使用浏览器 speechSynthesis 回退。

音频按用户和完整语音参数缓存，重复播放不必重复生成。单条请求文本及响应字节有技术边界，避免异常输入耗尽资源；不构成每日调用次数或会话额度。密钥和原文只按本次用户选择发给该供应商，不向额外分析服务发送数据。

## 判分边界与验证

词汇仍采用主释义/维护别名精确判分，未匹配且非空的自由中文释义为 pending，由学生核对自评。AI 助学不自动改写词库别名或影响正式判分，initial_result 与 resolved_result 分别保留。

测试通过模拟供应商验证请求模型、服务端认证、结构校验、重试、权限撤销与音频缓存，不调用真实付费模型。真实音色质量和供应商可用性需配置有效 Key 后试听，不能把模拟音频测试描述为真实声音试听完成。

参考：[OpenRouter API](https://openrouter.ai/docs/api_reference/overview)、[OpenRouter TTS](https://openrouter.ai/docs/guides/overview/multimodal/tts)、[OpenAI TTS](https://developers.openai.com/api/docs/guides/text-to-speech)。
