"use client";
import { useEffect, useState } from "react";
import { api, requestID, type User } from "../../lib/api";
import { Loading, Navigation, useProtected } from "../providers";
import { AdminNavigation } from "../components/AdminNavigation";
import { ModelScopeSelector } from "../components/ModelScopeSelector";
import { ProviderModelCatalog } from "../components/ProviderModelCatalog";
import { RegistrationPolicy } from "../components/RegistrationPolicy";

type Provider = {
  id: string;
  name: string;
  base_url: string;
  enabled: boolean;
  has_key: boolean;
};
type ConfigModel = {
  id: number;
  provider_id: string;
  model_id: string;
  name: string;
  kind: "chat" | "tts";
  voices: string[];
  instructions: string;
  json_mode: boolean;
  enabled: boolean;
};
type Group = { id: number; name: string; model_ids: number[]; revision?: number };
type Config = {
  encryption_ready: boolean;
  providers: Provider[];
  models: ConfigModel[];
  groups: Group[];
  users: (User & { disabled: boolean })[];
  audit: {
    id: number;
    actor: string;
    action: string;
    target: string;
    created_at: string;
  }[];
};
type Preview = {
  entries: {
    word: string;
    phonetic: string;
    pos: string;
    chinese: string;
    row: number;
  }[];
  errors: { row: number; message: string }[];
  duplicates: number;
};
const emptyProvider = {
  id: "",
  name: "OpenRouter",
  base_url: "https://openrouter.ai/api/v1",
  api_key: "",
  enabled: true,
};
const emptyModel: ConfigModel = {
  id: 0,
  provider_id: "",
  model_id: "",
  name: "",
  kind: "chat",
  voices: [],
  instructions: "",
  json_mode: false,
  enabled: true,
};
const emptyUser = {
  id: "",
  username: "",
  display_name: "",
  role: "learner" as "admin" | "learner",
  group_id: 1,
  disabled: false,
  password: "",
  clear_mfa: false,
};

export default function Admin() {
  const auth = useProtected(true);
  const [config, setConfig] = useState<Config | null>(null);
  const [tab, setTab] = useState("providers");
  const [adminPassword, setAdminPassword] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [provider, setProvider] = useState(emptyProvider);
  const [catalogProvider,setCatalogProvider]=useState("");
  const [bulkModels,setBulkModels]=useState<number[]>([]);
  const [model, setModel] = useState(emptyModel);
  const [voices, setVoices] = useState("cedar, marin, coral");
  const [group, setGroup] = useState<Group>({ id: 0, name: "", model_ids: [] });
  const [user, setUser] = useState(emptyUser);
  const [text, setText] = useState("");
  const [preview, setPreview] = useState<Preview | null>(null);
  const [overwrite, setOverwrite] = useState(false);
  const [importRequest, setImportRequest] = useState("");
  async function load() {
    const data = await api<Config>("/admin/config");
    setConfig(data);
    return data;
  }
  useEffect(() => {
    if (auth.allowed) {
      const requested = window.location.hash.slice(1);
      if (
        ["providers", "groups", "users", "import", "audit"].includes(requested)
      )
        setTab(requested);
      load().catch((e) => setError(e.message));
    }
  }, [auth.allowed]);
  useEffect(()=>{const sync=()=>{const h=window.location.hash.slice(1);if(["providers","groups","users","import","audit"].includes(h))setTab(h);};window.addEventListener("hashchange",sync);return()=>window.removeEventListener("hashchange",sync);},[]);
  async function run(work: () => Promise<unknown>) {
    if (busy) return;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await work();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  async function save(path: string, body: object, message: string) {
    const saved = await api<{ id: string | number }>(path, {
      ...body,
      ...(path==="/admin/users"?{admin_password:adminPassword}:{}),
    });
    await load();
    setNotice(message);
    return saved;
  }
  function editText(value: string) {
    setText(value);
    setPreview(null);
    setImportRequest("");
  }
  if (!auth.allowed) return <Loading />;
  return (
    <main className="shell admin-root">
      <Navigation />
      <div className="admin-workspace-layout">
        <AdminNavigation />
        <div className="admin-workspace-content">
      {catalogProvider&&<ProviderModelCatalog providerId={catalogProvider} onClose={()=>setCatalogProvider("")} onSaved={load}/>}
      {tab==="providers"&&!!config?.models.length&&<section className="panel glass"><h2>批量启用与停用</h2><ModelScopeSelector models={config.models.map(m=>({...m,enabled:true}))} providers={config.providers} selected={bulkModels} onChange={setBulkModels} label="批量模型管理"/><div className="segments">{[true,false].map(enabled=><button key={String(enabled)} className="quiet" disabled={busy||!bulkModels.length} onClick={()=>void run(async()=>{await api("/admin/ai/models",{ids:bulkModels,enabled},"PATCH");await load();setBulkModels([]);})}>{enabled?"启用所选":"停用所选"}</button>)}</div></section>}
      <section className="intro" id="page-content" tabIndex={-1}>
        <span className="eyebrow">CONTENT · MODELS · ACCESS</span>
        <h1>把合适的资源，交给合适的人。</h1>
        <p>
          配置 OpenAI
          兼容接口，给用户分配可用模型。权限组只管理授权，不设置调用次数或对话额度。
        </p>
      </section>
      {tab==="users"&&<section className="panel glass admin-confirm">
        <label>
          管理员当前密码
          <input
            type="password"
            value={adminPassword}
            onChange={(e) => setAdminPassword(e.target.value)}
            autoComplete="current-password"
            maxLength={128}
          />
        </label>
        <p className="setting-note">
          创建或修改账号需要重新认证；普通资源、模型与授权操作使用已有登录会话。
        </p>
        {config && !config.encryption_ready && (
          <p className="hint">
            服务器尚未配置 APP_ENCRYPTION_KEY，配置 API Key
            和两步验证前需要完成这一项。
          </p>
        )}
      </section>}
      {tab === "providers" && (
        <>
          <section className="panel glass">
            <div className="panel-heading">
              <h2>OpenAI 兼容服务商</h2>
              <button
                className="quiet"
                onClick={() => setProvider(emptyProvider)}
              >
                添加服务商
              </button>
            </div>
            <div className="segments">
              {config?.providers.map((p) => (
                <button
                  key={p.id}
                  aria-pressed={provider.id === p.id}
                  onClick={() =>
                    setProvider({
                      id: p.id,
                      name: p.name,
                      base_url: p.base_url,
                      enabled: p.enabled,
                      api_key: "",
                    })
                  }
                >
                  {p.name}
                  {!p.enabled ? " · 停用" : ""}
                </button>
              ))}
            </div>
            <form
              className="form-stack"
              onSubmit={(e) => {
                e.preventDefault();
                run(async () => {
                  const saved = await save(
                    "/admin/providers",
                    provider,
                    "服务商已保存。请添加模型并分配到权限组。",
                  );
                  setProvider({
                    ...provider,
                    id: String(saved.id),
                    api_key: "",
                  });
                  setCatalogProvider(String(saved.id));
                });
              }}
            >
              <div className="form-grid">
                <label>
                  服务商名称
                  <input
                    value={provider.name}
                    onChange={(e) =>
                      setProvider({ ...provider, name: e.target.value })
                    }
                    required
                    maxLength={100}
                  />
                </label>
                <label>
                  基础接口地址
                  <input
                    type="url"
                    value={provider.base_url}
                    onChange={(e) =>
                      setProvider({ ...provider, base_url: e.target.value })
                    }
                    required
                    placeholder="https://openrouter.ai/api/v1"
                  />
                </label>
              </div>
              <label>
                API Key{provider.id ? "（留空保留现有密钥）" : ""}
                <input
                  type="password"
                  value={provider.api_key}
                  onChange={(e) =>
                    setProvider({ ...provider, api_key: e.target.value })
                  }
                  autoComplete="off"
                  required={!provider.id}
                />
              </label>
              <label className="check-label">
                <input
                  type="checkbox"
                  checked={provider.enabled}
                  onChange={(e) =>
                    setProvider({ ...provider, enabled: e.target.checked })
                  }
                />
                启用服务商
              </label>
              <button className="primary" disabled={busy}>保存服务商并选择模型</button>
              {provider.id&&<button type="button" className="quiet" onClick={()=>setCatalogProvider(provider.id)}>获取模型目录 / 选择模型</button>}
            </form>
          </section>
          <section className="panel glass">
            <div className="panel-heading">
              <h2>聊天与自然语音模型</h2>
              <button
                className="quiet"
                onClick={() => {
                  setModel(emptyModel);
                  setVoices("cedar, marin, coral");
                }}
              >
                添加模型
              </button>
            </div>
            <div className="segments">
              {config?.models.map((m) => (
                <button
                  key={m.id}
                  aria-pressed={m.id === model.id}
                  onClick={() => {
                    setModel(m);
                    setVoices(m.voices.join(", "));
                  }}
                >
                  {m.name} · {m.kind === "tts" ? "语音" : "聊天"}
                  {!m.enabled ? " · 停用" : ""}
                </button>
              ))}
            </div>
            <form
              className="form-stack"
              onSubmit={(e) => {
                e.preventDefault();
                run(() =>
                  save(
                    "/admin/models",
                    {
                      ...model,
                      voices:
                        model.kind === "tts"
                          ? voices
                              .split(",")
                              .map((v) => v.trim())
                              .filter(Boolean)
                          : [],
                    },
                    "模型已保存，还需要在权限组中勾选授权。",
                  ),
                );
              }}
            >
              <div className="form-grid">
                <label>
                  服务商
                  <select
                    value={model.provider_id}
                    onChange={(e) =>
                      setModel({ ...model, provider_id: e.target.value })
                    }
                    required
                  >
                    <option value="">请选择</option>
                    {config?.providers.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  用途
                  <select
                    value={model.kind}
                    onChange={(e) => {
                      const kind = e.target.value as "chat" | "tts";
                      setModel({
                        ...model,
                        kind,
                        ...(kind === "tts" && !model.id
                          ? {
                              model_id: "openai/gpt-4o-mini-tts-2025-12-15",
                              name: "自然英语发音",
                              instructions:
                                "Speak naturally and warmly in clear English with expressive conversational intonation. Read only the supplied text.",
                            }
                          : {}),
                      });
                    }}
                  >
                    {" "}
                    <option value="chat">聊天 / 句子 / 听力出题</option>
                    <option value="tts">自然语音 / 听音拼写</option>
                  </select>
                </label>
                <label>
                  模型显示名称
                  <input
                    value={model.name}
                    onChange={(e) =>
                      setModel({ ...model, name: e.target.value })
                    }
                    required
                  />
                </label>
                <label>
                  供应商模型标识
                  <input
                    value={model.model_id}
                    onChange={(e) =>
                      setModel({ ...model, model_id: e.target.value })
                    }
                    required
                    placeholder="使用供应商 Models 列表中的完整 ID"
                  />
                </label>
              </div>
              {model.kind === "tts" ? (
                <>
                  <label>
                    音色标识（英文逗号分隔）
                    <input
                      value={voices}
                      onChange={(e) => setVoices(e.target.value)}
                      required
                    />
                  </label>
                  <label>
                    朗读风格（模型支持时填写）
                    <textarea
                      rows={3}
                      value={model.instructions}
                      onChange={(e) =>
                        setModel({ ...model, instructions: e.target.value })
                      }
                      maxLength={1000}
                    />
                  </label>
                  <p className="setting-note">
                    OpenAI 新 TTS 可使用
                    Cedar、Marin；其他供应商需填写其支持的音色。不支持
                    instructions 参数时留空。
                  </p>
                </>
              ) : (
                <label className="check-label">
                  <input
                    type="checkbox"
                    checked={model.json_mode}
                    onChange={(e) =>
                      setModel({ ...model, json_mode: e.target.checked })
                    }
                  />
                  出题时发送 JSON mode 参数（模型支持时开启）
                </label>
              )}
              <label className="check-label">
                <input
                  type="checkbox"
                  checked={model.enabled}
                  onChange={(e) =>
                    setModel({ ...model, enabled: e.target.checked })
                  }
                />
                启用模型
              </label>
              <button className="primary" disabled={busy}>
                保存模型
              </button>
            </form>
          </section>
        </>
      )}
      {tab === "groups" && (
        <section className="panel glass">
          <div className="panel-heading">
            <h2>身份组与可用模型</h2>
            <button
              className="quiet"
              onClick={() => setGroup({ id: 0, name: "", model_ids: [] })}
            >
              新建身份组
            </button>
          </div>
          <p className="setting-note">
            先添加服务商和模型，再建立身份组并勾选模型，最后到“账户”为用户分配身份组。学生可在练习页选择本组模型；作文可多选评审。
          </p>
          <div className="segments">
            {config?.groups.map((g) => (
              <button
                key={g.id}
                aria-pressed={group.id === g.id}
                onClick={() => setGroup(g)}
              >
                {g.name} · {g.model_ids.length} 个模型
              </button>
            ))}
          </div>
          <form
            className="form-stack"
            onSubmit={(e) => {
              e.preventDefault();
              run(async () => {
                const saved = await save(
                  "/admin/groups",
                  group,
                  "权限组已保存，新请求立即按新授权检查。",
                );
                setGroup({ ...group, id: Number(saved.id) });
              });
            }}
          >
            <label>
              身份组名称
              <input
                value={group.name}
                onChange={(e) => setGroup({ ...group, name: e.target.value })}
                required
                maxLength={100}
              />
            </label>
<ModelScopeSelector models={config?.models||[]} providers={config?.providers||[]} selected={group.model_ids} onChange={ids=>setGroup({...group,model_ids:ids})} label="身份组模型授权"/>
            <p className="setting-note">
              学生只能选择组内启用的模型；撤销授权会阻止待执行任务与后续追问。管理员在学生练习区也遵循身份组，资料入库任务单独按管理员配置执行。没有调用次数额度。
            </p>
            <button className="primary" disabled={busy}>
              保存身份组
            </button>
          </form>
        </section>
      )}
      {tab === "users" && (
        <section className="panel glass">
          <RegistrationPolicy password={adminPassword} groups={config?.groups || []} />
          <div className="panel-heading">
            <h2>账户与授权</h2>
            <button className="quiet" onClick={() => setUser(emptyUser)}>
              创建账户
            </button>
          </div>
          <div className="segments">
            {config?.users.map((u) => (
              <button
                key={u.id}
                aria-pressed={user.id === u.id}
                onClick={() =>
                  setUser({
                    id: u.id,
                    username: u.username,
                    display_name: u.display_name,
                    role: u.role,
                    group_id: u.group_id,
                    disabled: u.disabled,
                    password: "",
                    clear_mfa: false,
                  })
                }
              >
                {u.display_name} · {u.username}
                {u.disabled ? " · 停用" : ""}
              </button>
            ))}
          </div>
          <form
            className="form-stack"
            onSubmit={(e) => {
              e.preventDefault();
              run(async () => {
                const saved = await save(
                  "/admin/users",
                  user,
                  "账户已保存。新建或重置密码后，用户首次登录必须改密。",
                );
                setUser({
                  ...user,
                  id: String(saved.id),
                  password: "",
                  clear_mfa: false,
                });
              });
            }}
          >
            <div className="form-grid">
              <label>
                用户名
                <input
                  value={user.username}
                  onChange={(e) =>
                    setUser({ ...user, username: e.target.value })
                  }
                  required
                  minLength={3}
                  maxLength={40}
                  pattern="[a-z0-9][a-z0-9_.\-]{2,39}"
                />
              </label>
              <label>
                显示名称
                <input
                  value={user.display_name}
                  onChange={(e) =>
                    setUser({ ...user, display_name: e.target.value })
                  }
                  required
                />
              </label>
              <label>
                账户角色
                <select
                  value={user.role}
                  onChange={(e) =>
                    setUser({
                      ...user,
                      role: e.target.value as "admin" | "learner",
                    })
                  }
                >
                  <option value="learner">学习用户</option>
                  <option value="admin">管理员</option>
                </select>
              </label>
              <label>
                模型权限组
                <select
                  value={user.group_id}
                  onChange={(e) =>
                    setUser({ ...user, group_id: Number(e.target.value) })
                  }
                >
                  {config?.groups.map((g) => (
                    <option key={g.id} value={g.id}>
                      {g.name}
                    </option>
                  ))}
                </select>
              </label>
            </div>
            <label>
              {user.id ? "重置密码（留空不重置）" : "初始密码"}
              <input
                type="password"
                autoComplete="new-password"
                value={user.password}
                onChange={(e) => setUser({ ...user, password: e.target.value })}
                required={!user.id}
                minLength={15}
                maxLength={128}
              />
            </label>
            <label className="check-label">
              <input
                type="checkbox"
                checked={user.disabled}
                onChange={(e) =>
                  setUser({ ...user, disabled: e.target.checked })
                }
              />
              停用账户并退出该账户所有设备
            </label>
            {user.id && (
              <label className="check-label">
                <input
                  type="checkbox"
                  checked={user.clear_mfa}
                  onChange={(e) =>
                    setUser({ ...user, clear_mfa: e.target.checked })
                  }
                />
                用户丢失验证器：同时重置两步验证（必须设置新密码）
              </label>
            )}
            <button className="primary" disabled={busy}>
              保存账户
            </button>
          </form>
        </section>
      )}
      {tab === "import" && (
        <section className="panel glass">
          <h2>教材词库 · 四字段导入</h2>
          <p>
            每行按顺序填写：单词、音标、词性、中文。支持 CSV、Tab
            分隔或下面的空格格式；教材内容由你提供。
          </p>
          <pre className="sample">
            {"adapt /əˈdæpt/ v. 适应\nnecessary /ˈnesəsəri/ adj. 必要的"}
          </pre>
          <label>
            选择 UTF-8 CSV / TSV / TXT 文件
            <input
              type="file"
              accept=".csv,.tsv,.txt"
              onChange={(e) => {
                const file = e.target.files?.[0];
                if (!file) return;
                if (file.size > 1500000) {
                  setError("文件过大，请分批导入。");
                  return;
                }
                file.text().then(editText);
              }}
            />
          </label>
          <label>
            粘贴词表
            <textarea
              value={text}
              onChange={(e) => editText(e.target.value)}
              rows={9}
              placeholder="word,phonetic,pos,chinese"
            />
          </label>
          <label className="check-label">
            <input
              type="checkbox"
              checked={overwrite}
              onChange={(e) => {
                setOverwrite(e.target.checked);
                setImportRequest("");
              }}
            />
            更新已有单词的音标（默认保留已有音标）
          </label>
          <div className="actions">
            <button
              className="quiet"
              disabled={busy || !text.trim()}
              onClick={() =>
                run(async () => {
                  const p = await api<Preview>("/admin/vocabulary/preview", {
                    text,
                  });
                  setPreview(p);
                  setImportRequest(requestID());
                })
              }
            >
              预览并校验
            </button>
            <button
              className="primary"
              disabled={
                busy || !adminPassword || !preview || !!preview.errors.length
              }
              onClick={() =>
                run(async () => {
                  const id = importRequest || requestID();
                  setImportRequest(id);
                  const result = await api<{ added: number; skipped: number }>(
                    "/admin/vocabulary/import",
                    {
                      text,
                      overwrite_phonetic: overwrite,
                      request_id: id,
                                          },
                  );
                  await load();
                  setNotice(
                    `已导入 ${result.added} 条，跳过 ${result.skipped} 条重复内容。`,
                  );
                })
              }
            >
              确认导入
            </button>
          </div>
          {preview && (
            <>
              <p>
                有效词条 {preview.entries.length} · 文件内重复{" "}
                {preview.duplicates} · 错误 {preview.errors.length}
              </p>
              {preview.errors.map((e, i) => (
                <p className="error" key={i}>
                  第 {e.row} 行：{e.message}
                </p>
              ))}
              <div
                className="table-scroll"
                tabIndex={0}
                role="region"
                aria-label="数据表格，可横向滚动"
              >
                <table>
                  <thead>
                    <tr>
                      <th>单词</th>
                      <th>音标</th>
                      <th>词性</th>
                      <th>中文</th>
                    </tr>
                  </thead>
                  <tbody>
                    {preview.entries.slice(0, 50).map((e) => (
                      <tr key={e.row}>
                        <td lang="en">{e.word}</td>
                        <td>{e.phonetic}</td>
                        <td>{e.pos}</td>
                        <td>{e.chinese}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {preview.entries.length > 50 && (
                <p className="setting-note">
                  预览显示前 50 条；确认导入会处理全部有效词条。
                </p>
              )}
            </>
          )}
        </section>
      )}
      {tab === "audit" && (
        <section className="panel glass">
          <h2>最近的管理与安全操作</h2>
          <div
            className="table-scroll"
            tabIndex={0}
            role="region"
            aria-label="数据表格，可横向滚动"
          >
            <table>
              <thead>
                <tr>
                  <th>时间</th>
                  <th>操作者</th>
                  <th>操作</th>
                  <th>对象</th>
                </tr>
              </thead>
              <tbody>
                {config?.audit.map((a) => (
                  <tr key={a.id}>
                    <td>{new Date(a.created_at).toLocaleString()}</td>
                    <td>{a.actor}</td>
                    <td>{a.action}</td>
                    <td>{a.target}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}
      {notice && (
        <p className="notice" role="status">
          {notice}
        </p>
      )}
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
        </div>
      </div>
    </main>
  );
}
