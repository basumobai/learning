# API v1

Next.js 同源 `/api/v1` 转发到 Go。除 GET /health 和 POST /auth/login 外均需会话 Cookie。写入携带 `X-CSRF-Token`，无 Origin 的程序客户端还需 `X-ELS-Client:web`；Origin 必须匹配 APP_ORIGIN。

## 身份、账户、内容

| 接口 | 请求与行为 |
|---|---|
| POST /auth/login | {username,password,code?}；HttpOnly 会话 Cookie，统一认证失败 |
| GET /auth/me | {user,csrf_token}，不返回凭据 |
| POST /auth/logout | {}，撤销当前会话 |
| POST /auth/password | {current_password,new_password}；撤销全部会话，必须重新登录 |
| POST /auth/revoke-sessions | {}，退出其他设备 |
| POST /auth/mfa | {action:start/confirm/disable,password,code?}；绑定 start 返回十分钟待验证密钥 |
| POST /preferences/voice | {model_id,voice}，仅修改语音偏好，保留复习设置 |
| GET/POST /preferences | {daily_new,retention,timezone,tts_model_id,voice} |
| GET /account/export | 本人 JSON 学习数据，无凭据与他人记录，未交卷听力仅摘要 |
| GET /vocabulary?q=&page= | 50 条分页词条，音标/词性/中文/个人暂停/未独立记住次数 |
| POST /vocabulary/suspend | {sense_id,suspended}，仅本人的暂停状态 |
| GET /admin/config | 管理配置与最近审计；不返回 API Key/password_hash |
| POST /admin/providers | {id?,name,base_url,api_key,enabled,admin_password}；更新时空 Key 保留旧值 |
| POST /admin/models | {id?,provider_id,model_id,name,kind,voices,instructions,json_mode,enabled,admin_password} |
| POST /admin/groups | {id?,name,model_ids,admin_password}；只配置授权，无调用额度 |
| POST /admin/users | {id?,username,display_name,role,group_id,disabled,password?,clear_mfa?,admin_password} |
| POST /admin/vocabulary/preview | {text}，返回有效词条、逐行错误和文件内重复数 |
| POST /admin/vocabulary/import | {text,request_id,overwrite_phonetic,admin_password}，全部校验后事务写入 |

新用户初始改密前仅可 me/password/logout；管理写入需再次验证管理员密码。权限变更每次请求取最新组。角色/停用/改密撤销会话。

## 词汇复习

| 接口 | 行为 |
|---|---|
| GET /dashboard | 本人今日/分能力计数、new_today/new_remaining/preferences |
| POST /study/sessions | {mode,types,limit,new_limit}；创建或恢复本人活动练习 |
| GET /study/sessions/active | {session:null 或完整记录} |
| GET /study/sessions/{id} | 只有本人可读，含题型/顺序/已提交反馈与待评级预估 |
| POST /study/sessions/{id}/answers | {question_id,request_id,answer,response_time_ms,used_hint}，仅判分 |
| POST /study/sessions/{id}/review | {question_id,request_id,rating,resolved_result?}，确认后调度 |
| POST /study/sessions/{id}/undo | {request_id}，恢复最近一次本人的原始状态 |
| POST /study/sessions/{id}/questions/{question}/speech | {model_id,voice}；目标文本由服务端取，听音题交卷前可听 |

mode 为 scheduled/practice；types 可含 recognition/meaning_recall/spelling/listening_spelling，不重复。默认三种文本题；听音题要求组内有启用 TTS 模型。limit 默认 20、1–100；new_limit 默认 10、0–50，本轮上限同时受全天新词预算约束。

答后结果不等于完成，rating 非空才完成。expected/phonetic/pos/example 仅交卷后返回；中译英题交卷前禁止通过题目音频接口听答案。错误或提示只可评级 1；pending 需 resolved_result:correct/wrong。旧无快照记录拒绝撤销。答案/评价/撤销/导入均有幂等 request_id，冲突 409。

## AI 与语音

| 接口 | 请求与行为 |
|---|---|
| GET /ai/models | 本人组允许且启用的模型与音色 |
| GET/POST /ai/conversations | 列表 / {title} 创建，不设置对话数量额度 |
| GET /ai/conversations/{id} | 本人完整消息历史 |
| POST /ai/conversations/{id}/messages | {model_id,request_id,content}；服务端模型授权、历史上下文、持久化、幂等回复 |
| POST /ai/training | {model_id,request_id,kind:sentence/listening,text,level}；生成并保存 |
| GET /ai/training | 本人训练摘要 |
| GET /ai/training/{id} | 恢复；未交卷听力无原文/答案 |
| POST /ai/training/{id}/answers | {request_id,answers:[0–3]}；首次提交后返回原文、译文、分数、解析与精确依据 |
| POST /ai/training/{id}/speech | {model_id,voice}；服务端合成听力原文，返回 MP3 |
| POST /ai/speech | {model_id,voice,input}；已授权 TTS 模型，返回 MP3 |

句子 text 最多 4000 字符；听力主题最多 200，level 为 A2/B1/B2；聊天单条最多 12000；语音最多 4000。这些以及请求/响应字节边界用于安全及供应商格式兼容，没有调用次数或对话轮数额度。聊天不发送客户端自选 system/tool 角色或模型 fallback。训练字段与连续原文依据必须校验通过，否则 502。

request_id 为 8–128 位字母/数字/下划线/连字符。AI 同一编号相同 payload 返回已缓存结果，不同 payload 冲突；同一对话已有运行请求时返回 409。崩溃预留三分钟后可重试。所有正文只允许一个 JSON 值和已定义字段，未知字段拒绝。401 未登录/会话失效，403 未授权/CSRF/需改密，404 不存在或非本人，502 供应商/生成结构错误。供应商原始错误和密钥不回显。

## Learning 多学科 API

完整行为见 [LEARNING](LEARNING.md)。沿用登录Cookie、CSRF与管理员角色校验。

| 路径（均以 `/api/v1` 开头） | 用途 |
| --- | --- |
| GET `/learning/catalog` | 六科、广东题型、教材/学期/章节与已发布题数 |
| POST `/learning/attempts` | 按科目、题型、多章节范围开始练习 |
| GET `/learning/attempts/{id}` | 本人练习、截止时间、草稿和交卷后反馈 |
| POST `/learning/attempts/{id}/draft`、`/submit` | 截止前保存；交卷原子写入作答及主观评分任务 |
| POST `/learning/attempts/{id}/explanation`、`/chat` | 仅交卷后答错或主动求助时排队；按当前步骤追问 |
| GET `/learning/jobs/{id}`、POST `/learning/jobs/{id}/retry` | 本人后台任务状态及失败重试 |
| GET `/learning/assets/{id}` | 本人已开始练习的已发布题插图；管理员可预览草稿 |
| GET `/learning/model-options` | 本人身份组、授权聊天模型、可用默认选择及已保存偏好 |
| POST `/learning/model-preferences` | `{model_id,reviewer_ids,lead_model_id}`；逐项验证本组授权 |
| GET/POST `/learning/ai-settings` | 本人API接口配置；Key不返回 |
| POST `/learning/text-import` | multipart `file`，读取UTF-8 TXT／DOCX作文正文 |
| POST `/learning/words/next`、`/learning/words/{id}/answer` | 4选1限时轮次，服务端判断截止 |
| GET/POST `/admin/learning/tasks` | 提示词、任务模型、作文主审、API/Codex后端 |
| GET/POST `/admin/learning/documents` | 资料列表／multipart题目与答案PDF上传 |
| POST `/admin/learning/documents/{id}/extract` | 核对页码后为每个选中小节独立排队 |
| GET `/admin/learning/documents/{id}/pages/{kind}/{page}` | question/answer物理页图 |
| GET `/admin/learning/questions`、POST `/admin/learning/questions/{id}` | 审核草稿、修订、发布，使用revision乐观锁 |
| GET/POST `/admin/learning/word-sets`、POST `/admin/learning/word-sets/{id}/approve` | 准备、审核与撤回限时选项集 |
| GET/POST `/admin/learning/cloud`、POST `/admin/learning/cloud/list`、`/import` | 管理员WebDAV配置、列目录、成对读取PDF |

`normal/hard/despair` 为难度；`en_zh/zh_en` 为方向。`request_id`用于开始练习、开始词汇轮次和AI追问的幂等；作答提交/词汇选择由服务端锁定第一次有效提交。不同内容不能通过重放覆盖首次结果。新增练习题组用 `content.parts` 保存小题，`solution.answers` 按小题ID映射，选择题答案为选项ID，多选以逗号分隔。

练习的 `/submit`、`/explanation`、`/chat` 可携带 `personal` 与 `model_selection:{model_id,reviewer_ids,lead_model_id}`。`model_id` 用于讲解、追问、普通主观评价；作文使用2–5个不同 `reviewer_ids` 和 `lead_model_id`。平台选择只能引用身份组授权的启用模型；个人接口使用设置页模型组合。提交作答不会因为模型不可用丢弃结果，评分错误保存在任务中。

`/explanation` 的 `reason` 为 `incorrect`（默认）或 `self_report`，前者要求已确认失分，后者是学生主动求助。两种都要求已交卷；未交卷403，未判错且非主动求助409。练习响应含 `explanation_needed`，前端只在它为true时自动请求。`jobs` 包含本人该次评分和讲解任务，刷新可恢复，不重新生成。不同作答、模型或提示词版本不共用讲解。

失败评分 `/jobs/{id}/retry` 可携带新的 `model_selection`/`personal`，重新验证并保存模型快照；讲解重试使用空对象保留原快照，更换讲解模型应再次请求 `/explanation`。
