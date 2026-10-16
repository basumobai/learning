"use client";
import Link from "next/link";
import { useEffect, useState } from "react";
import { api, upload, type Model } from "../../../lib/api";
import { subjects } from "../../../lib/learning";
import { Loading, Navigation, useProtected } from "../../providers";
type Task = {
  task: string;
  prompt: string;
  backend: string;
  codex_model: string;
  model_ids: number[];
  lead_model_id: number | null;
  revision: number;
};
type Chapter = {
  key: string;
  title: string;
  parent: string;
  question_pages: number[];
  answer_pages: number[];
  note: string;
};
type Doc = {
  id: string;
  title: string;
  subject_id: string;
  textbook: string;
  term: string;
  outline: { chapters: Chapter[] } | null;
  jobs: { id: string; kind: string; status: string; error: string }[] | null;
};
type Draft = {
  id: string;
  document_id: string;
  status: string;
  revision: number;
  content: { text: string };
  solution: unknown;
  provenance: {
    original_answer: string;
    question_pages: number[];
    answer_pages: number[];
  };
  assets: { id: string; alt: string }[] | null;
};
type WordSet = {
  id: string;
  lemma: string;
  prompt: string;
  choices: { id: string; text: string; pos: string; similar_group: string }[];
  correct_id: string;
  rationale: string;
  difficulty: string;
  direction: string;
  approved: boolean;
};
type Cloud = {
  id: string;
  name: string;
  base_url: string;
  username: string;
  enabled: boolean;
};
export default function AdminLearning() {
  const auth = useProtected(true),
    [tab, setTab] = useState("documents"),
    [password, setPassword] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [documents, setDocuments] = useState<Doc[]>([]),
    [tasks, setTasks] = useState<Task[]>([]),
    [defaults, setDefaults] = useState<
      Record<string, { name: string; prompt: string }>
    >({}),
    [models, setModels] = useState<Model[]>([]),
    [task, setTask] = useState<Task | null>(null),
    [drafts, setDrafts] = useState<Draft[]>([]),
    [draft, setDraft] = useState<Draft | null>(null),
    [edit, setEdit] = useState(""),
    [doc, setDoc] = useState<Doc | null>(null),
    [outline, setOutline] = useState<Chapter[]>([]),
    [picked, setPicked] = useState<string[]>([]),
    [sets, setSets] = useState<WordSet[]>([]),
    [clouds, setClouds] = useState<Cloud[]>([]),
    [cloud, setCloud] = useState({
      id: "",
      name: "教学资料",
      base_url: "",
      username: "",
      secret: "",
      enabled: true,
    }),
    [source, setSource] = useState("upload"),
    [word, setWord] = useState({
      sense_id: 1,
      direction: "en_zh",
      difficulty: "normal",
    });
  async function load() {
    const [d, t, c, w, q, cc] = await Promise.all([
      api<{ documents: Doc[] }>("/admin/learning/documents"),
      api<{ tasks: Task[]; defaults: typeof defaults }>(
        "/admin/learning/tasks",
      ),
      api<{ models: Model[] }>("/admin/config"),
      api<{ sets: WordSet[] }>("/admin/learning/word-sets"),
      api<{ questions: Draft[] }>("/admin/learning/questions"),
      api<{ connections: Cloud[] }>("/admin/learning/cloud"),
    ]);
    setDocuments(d.documents);
    setTasks(t.tasks);
    setDefaults(t.defaults);
    setModels(c.models.filter((m) => m.kind === "chat"));
    setSets(w.sets);
    setDrafts(q.questions);
    setClouds(cc.connections);
  }
  useEffect(() => {
    if (auth.allowed) load().catch((e) => setError(e.message));
  }, [auth.allowed]);
  useEffect(() => {
    if (
      !documents.some((d) =>
        d.jobs?.some((j) => j.status === "queued" || j.status === "running"),
      )
    )
      return;
    const timer = setInterval(
      () => load().catch((e) => setError(e.message)),
      4000,
    );
    return () => clearInterval(timer);
  }, [documents]);
  async function run(work: () => Promise<void>) {
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
  if (!auth.allowed) return <Loading />;
  const pages = (id: string, kind: string, nums: number[]) => (
    <div className="source-page-links">
      {nums.map((p) => (
        <a
          key={p}
          href={`/api/v1/admin/learning/documents/${id}/pages/${kind}/${p}`}
          target="_blank"
          rel="noreferrer"
        >
          {kind === "question" ? "题目" : "答案"}p{p}
        </a>
      ))}
    </div>
  );
  return (
    <main className="shell">
      <Navigation />
      <div id="page-content" className="learning-admin">
        <header className="page-intro">
          <span className="eyebrow">LEARNING · 管理工作台</span>
          <h1>把资料，变成可理解的知识。</h1>
          <p>
            识别目录 → 选择小节 → 审核题目与答案 →
            发布。动态讲解在做到题时生成。
          </p>
        </header>
        <p>
          <Link href="/admin">账户、服务商、模型权限组与词库导入 →</Link>
        </p>
        <label className="admin-reauth">
          当前管理员密码
          <input
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </label>
        <div className="subject-tabs" role="group" aria-label="管理功能">
          {[
            ["documents", "资料入库"],
            ["questions", "题目审核"],
            ["tasks", "AI 提示词与模型"],
            ["words", "四选一选项集"],
            ["cloud", "云盘连接"],
          ].map(([id, name]) => (
            <button
              className={tab === id ? "primary" : "quiet"}
              onClick={() => setTab(id)}
              key={id}
            >
              {name}
            </button>
          ))}
          <button
            className="quiet"
            disabled={busy}
            onClick={() => void run(load)}
          >
            刷新
          </button>
        </div>
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        {notice && (
          <p role="status" className="notice preserve-text">
            {notice}
          </p>
        )}
        {tab === "tasks" && (
          <section className="panel glass">
            <h2>每个用途，单独配置。</h2>
            <p>
              学生调用平台模型仍受权限组控制。作文需要至少两个独立评审模型与一个主审。PDF识别请选择视觉模型。
            </p>
            <label>
              任务
              <select
                value={task?.task || ""}
                onChange={(e) =>
                  setTask(tasks.find((t) => t.task === e.target.value) || null)
                }
              >
                <option value="">选择任务</option>
                {tasks.map((t) => (
                  <option key={t.task} value={t.task}>
                    {defaults[t.task]?.name || t.task}
                  </option>
                ))}
              </select>
            </label>
            {task && (
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  void run(async () => {
                    await api("/admin/learning/tasks", {
                      ...task,
                      admin_password: password,
                    });
                    await load();
                    setTask(null);
                    setNotice("已保存，新生成使用新版本提示词");
                  });
                }}
              >
                <label>
                  提示词
                  <textarea
                    rows={12}
                    value={task.prompt}
                    maxLength={12000}
                    onChange={(e) =>
                      setTask({ ...task, prompt: e.target.value })
                    }
                  />
                </label>
                <button
                  type="button"
                  className="quiet"
                  onClick={() =>
                    setTask({ ...task, prompt: defaults[task.task].prompt })
                  }
                >
                  恢复默认提示词
                </button>
                {["outline", "extract"].includes(task.task) && (
                  <>
                    <label>
                      入库方式
                      <select
                        value={task.backend}
                        onChange={(e) =>
                          setTask({ ...task, backend: e.target.value })
                        }
                      >
                        <option value="api">OpenAI 兼容 API</option>
                        <option value="codex">独立 Codex CLI 服务</option>
                      </select>
                    </label>
                    {task.backend === "codex" && (
                      <label>
                        Codex 模型（可留空）
                        <input
                          value={task.codex_model}
                          onChange={(e) =>
                            setTask({ ...task, codex_model: e.target.value })
                          }
                        />
                      </label>
                    )}
                  </>
                )}
                {task.backend === "api" && task.task !== "essay_lead" && (
                  <fieldset className="chapter-picker">
                    <legend>
                      {task.task === "essay_reviewer"
                        ? "评审模型（2–5个）"
                        : "任务模型（按选择顺序使用第一个）"}
                    </legend>
                    {models.map((m) => (
                      <label key={m.id}>
                        <input
                          type="checkbox"
                          checked={task.model_ids.includes(m.id)}
                          onChange={(e) =>
                            setTask({
                              ...task,
                              model_ids: e.target.checked
                                ? [...task.model_ids, m.id]
                                : task.model_ids.filter((id) => id !== m.id),
                            })
                          }
                        />
                        <span>
                          {m.name}
                          <small>{m.model_id}</small>
                        </span>
                      </label>
                    ))}
                  </fieldset>
                )}
                {task.task === "essay_lead" && (
                  <p className="setting-note">
                    主审模型在“作文独立评审”任务中指定，此处编辑主审汇总提示词。
                  </p>
                )}
                {task.task === "essay_reviewer" && (
                  <label>
                    作文主审
                    <select
                      value={task.lead_model_id || ""}
                      onChange={(e) =>
                        setTask({
                          ...task,
                          lead_model_id: Number(e.target.value) || null,
                        })
                      }
                    >
                      <option value="">请选择</option>
                      {models.map((m) => (
                        <option value={m.id} key={m.id}>
                          {m.name}
                        </option>
                      ))}
                    </select>
                  </label>
                )}
                <button className="primary" disabled={busy || !password}>
                  保存配置
                </button>
              </form>
            )}
          </section>
        )}
        {tab === "documents" && (
          <>
            <section className="panel glass">
              <h2>题目与答案 PDF</h2>
              <p className="setting-note">
                每份最多32MB、200页。每个小节最多8页题目和8页答案。扫描版使用视觉识别。
              </p>
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  const data = new FormData(e.currentTarget);
                  void run(async () => {
                    data.set("admin_password", password);
                    if (source === "upload")
                      await upload("/admin/learning/documents", data);
                    else {
                      const v = Object.fromEntries(data.entries());
                      await api("/admin/learning/cloud/import", {
                        title: v.title,
                        subject_id: v.subject_id,
                        textbook: v.textbook,
                        term: v.term,
                        connection_id: v.connection_id,
                        question_path: v.question_path,
                        answer_path: v.answer_path,
                        admin_password: password,
                      });
                    }
                    await load();
                    setNotice("文件已接收，目录识别已排队。");
                  });
                }}
              >
                <div className="learning-filters">
                  <label>
                    标题
                    <input required name="title" placeholder="同步练习册" />
                  </label>
                  <label>
                    科目
                    <select name="subject_id">
                      {subjects.map((s) => (
                        <option key={s.id} value={s.id}>
                          {s.name}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label>
                    教材／册次
                    <input
                      name="textbook"
                      placeholder="北师大版2019 选择性必修一"
                    />
                  </label>
                  <label>
                    学期
                    <input name="term" placeholder="高二上" />
                  </label>
                </div>
                <label>
                  来源
                  <select
                    value={source}
                    onChange={(e) => setSource(e.target.value)}
                  >
                    <option value="upload">本地上传</option>
                    <option value="cloud">WebDAV 云盘</option>
                  </select>
                </label>
                {source === "upload" ? (
                  <div className="learning-filters">
                    <label>
                      题目 PDF
                      <input
                        type="file"
                        name="question_pdf"
                        accept=".pdf"
                        required
                      />
                    </label>
                    <label>
                      答案 PDF
                      <input
                        type="file"
                        name="answer_pdf"
                        accept=".pdf"
                        required
                      />
                    </label>
                  </div>
                ) : (
                  <>
                    <label>
                      云盘
                      <select required name="connection_id">
                        <option value="">选择连接</option>
                        {clouds
                          .filter((c) => c.enabled)
                          .map((c) => (
                            <option key={c.id} value={c.id}>
                              {c.name}
                            </option>
                          ))}
                      </select>
                    </label>
                    <label>
                      题目相对路径
                      <input
                        name="question_path"
                        required
                        placeholder="英语/题目.pdf"
                      />
                    </label>
                    <label>
                      答案相对路径
                      <input
                        name="answer_path"
                        required
                        placeholder="英语/答案.pdf"
                      />
                    </label>
                  </>
                )}
                <button className="primary" disabled={busy || !password}>
                  接收文件并识别目录
                </button>
              </form>
            </section>
            <section className="panel glass">
              <h2>选择小节</h2>
              <div className="document-list">
                {documents.map((d) => (
                  <article key={d.id}>
                    <button
                      className="text-button"
                      onClick={() => {
                        setDoc(d);
                        setOutline(d.outline?.chapters || []);
                        setPicked([]);
                      }}
                    >
                      {d.title}
                    </button>
                    <small>
                      {d.textbook} · {d.term}
                    </small>
                    {d.jobs?.map((j) => (
                      <p className="setting-note" key={j.id}>
                        {j.kind} · {j.status} {j.error}
                        {j.status === "failed" && (
                          <button
                            className="quiet"
                            onClick={() =>
                              void run(async () => {
                                await api(`/learning/jobs/${j.id}/retry`, {});
                                await load();
                              })
                            }
                          >
                            重试
                          </button>
                        )}
                      </p>
                    ))}
                  </article>
                ))}
              </div>
              {doc && (
                <>
                  <p>
                    核对物理页码（从1开始）。答案页不确定时，请人工补齐。目录完成后重新选择资料。
                  </p>
                  {outline.map((c, i) => (
                    <article className="outline-chapter" key={c.key}>
                      <label className="check-label">
                        <input
                          type="checkbox"
                          checked={picked.includes(c.key)}
                          onChange={(e) =>
                            setPicked((v) =>
                              e.target.checked
                                ? [...v, c.key]
                                : v.filter((k) => k !== c.key),
                            )
                          }
                        />
                        <span>
                          {c.parent} / {c.title}
                        </span>
                      </label>
                      <div className="learning-filters">
                        <label>
                          小节名称
                          <input
                            value={c.title}
                            onChange={(e) =>
                              setOutline((v) =>
                                v.map((x, j) =>
                                  i === j ? { ...x, title: e.target.value } : x,
                                ),
                              )
                            }
                          />
                        </label>
                        {(["question_pages", "answer_pages"] as const).map(
                          (key) => (
                            <label key={key}>
                              {key === "question_pages" ? "题目页" : "答案页"}
                              <input
                                defaultValue={c[key].join(",")}
                                onBlur={(e) =>
                                  setOutline((v) =>
                                    v.map((x, j) =>
                                      i === j
                                        ? {
                                            ...x,
                                            [key]: e.target.value
                                              .split(",")
                                              .map(Number)
                                              .filter((n) => n > 0),
                                          }
                                        : x,
                                    ),
                                  )
                                }
                              />
                            </label>
                          ),
                        )}
                      </div>
                      <p>{c.note}</p>
                      {pages(doc.id, "question", c.question_pages)}
                      {pages(doc.id, "answer", c.answer_pages)}
                    </article>
                  ))}
                  <button
                    className="quiet"
                    onClick={() =>
                      setOutline((v) => [
                        ...v,
                        {
                          key: `manual-${Date.now()}`,
                          title: "手动小节",
                          parent: "",
                          question_pages: [1],
                          answer_pages: [1],
                          note: "人工选择页码",
                        },
                      ])
                    }
                  >
                    补充小节
                  </button>
                  <button
                    className="primary"
                    disabled={busy || !password || !picked.length}
                    onClick={() =>
                      void run(async () => {
                        await api(
                          `/admin/learning/documents/${doc.id}/extract`,
                          {
                            chapters: outline.filter((c) =>
                              picked.includes(c.key),
                            ),
                            admin_password: password,
                          },
                        );
                        await load();
                        setNotice("选中的小节已排队提取，只生成题目草稿。");
                      })
                    }
                  >
                    识别选中小节
                  </button>
                </>
              )}
            </section>
          </>
        )}
        {tab === "questions" && (
          <section className="panel glass">
            <h2>题干、插图与答案，一起核对。</h2>
            <label>
              选择题目
              <select
                value={draft?.id || ""}
                onChange={(e) => {
                  const q = drafts.find((d) => d.id === e.target.value);
                  if (q) {
                    setDraft(q);
                    setEdit(
                      JSON.stringify(
                        { content: q.content, solution: q.solution },
                        null,
                        2,
                      ),
                    );
                  }
                }}
              >
                <option value="">请选择</option>
                {drafts.map((q) => (
                  <option key={q.id} value={q.id}>
                    [{q.status}] {q.content.text.slice(0, 70)}
                  </option>
                ))}
              </select>
            </label>
            {draft && (
              <>
                <h3>{draft.content.text}</h3>
                {pages(
                  draft.document_id,
                  "question",
                  draft.provenance.question_pages,
                )}
                {pages(
                  draft.document_id,
                  "answer",
                  draft.provenance.answer_pages,
                )}
                <p>原始答案：{draft.provenance.original_answer}</p>
                {draft.assets?.map((a) => (
                  <figure className="question-figure" key={a.id}>
                    <img alt={a.alt} src={`/api/v1/learning/assets/${a.id}`} />
                    <figcaption>{a.alt}</figcaption>
                  </figure>
                ))}
                <label>
                  修订题目／解析数据
                  <textarea
                    className="json-editor"
                    rows={24}
                    value={edit}
                    onChange={(e) => setEdit(e.target.value)}
                    spellCheck={false}
                  />
                </label>
                <div className="question-submit">
                  {["draft", "published"].map((status) => (
                    <button
                      className={status === "published" ? "primary" : "quiet"}
                      disabled={busy || !password}
                      key={status}
                      onClick={() =>
                        void run(async () => {
                          const data = JSON.parse(edit);
                          await api(`/admin/learning/questions/${draft.id}`, {
                            content: data.content,
                            solution: data.solution,
                            revision: draft.revision,
                            status,
                            admin_password: password,
                          });
                          await load();
                          setDraft(null);
                          setNotice(
                            status === "published"
                              ? "题目已发布"
                              : "草稿已保存",
                          );
                        })
                      }
                    >
                      {status === "published" ? "审核通过并发布" : "保存草稿"}
                    </button>
                  ))}
                </div>
              </>
            )}
          </section>
        )}
        {tab === "words" && (
          <>
            <section className="panel glass">
              <h2>准备极速四选一</h2>
              <p>
                近似组用于结构校验；选项的语义相似程度和答案唯一性需人工审核。词义编号可在词库查看。
              </p>
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  void run(async () => {
                    const job = await api<{ id: string }>(
                      "/admin/learning/word-sets",
                      word,
                    );
                    setNotice(
                      `生成任务已排队：${job.id}。稍后刷新查看待审核选项。`,
                    );
                  });
                }}
              >
                <div className="learning-filters">
                  <label>
                    词义编号
                    <input
                      type="number"
                      min={1}
                      value={word.sense_id}
                      onChange={(e) =>
                        setWord({ ...word, sense_id: Number(e.target.value) })
                      }
                    />
                  </label>
                  <label>
                    方向
                    <select
                      value={word.direction}
                      onChange={(e) =>
                        setWord({ ...word, direction: e.target.value })
                      }
                    >
                      <option value="en_zh">英选中</option>
                      <option value="zh_en">中选英</option>
                    </select>
                  </label>
                  <label>
                    难度
                    <select
                      value={word.difficulty}
                      onChange={(e) =>
                        setWord({ ...word, difficulty: e.target.value })
                      }
                    >
                      <option value="normal">普通 · 6秒</option>
                      <option value="hard">困难 · 3秒</option>
                      <option value="despair">绝望 · 3秒，带词性</option>
                    </select>
                  </label>
                </div>
                <button className="primary" disabled={busy}>
                  生成选项集
                </button>
              </form>
            </section>
            <section className="panel glass">
              <h2>审核选项</h2>
              {sets.map((s) => (
                <article className="word-set-review" key={s.id}>
                  <h3>
                    {s.lemma} · {s.difficulty} · {s.direction}
                  </h3>
                  <p>{s.prompt}</p>
                  <ul>
                    {s.choices.map((c) => (
                      <li key={c.id}>
                        {c.id === s.correct_id ? "✓ " : ""}
                        {c.id}. {c.pos} {c.text}（近似组 {c.similar_group}）
                      </li>
                    ))}
                  </ul>
                  <p>{s.rationale}</p>
                  <button
                    className="quiet"
                    disabled={busy || !password}
                    onClick={() =>
                      void run(async () => {
                        await api(`/admin/learning/word-sets/${s.id}/approve`, {
                          approved: !s.approved,
                          admin_password: password,
                        });
                        await load();
                      })
                    }
                  >
                    {s.approved ? "撤回选项集" : "确认答案唯一并批准"}
                  </button>
                </article>
              ))}
            </section>
          </>
        )}
        {tab === "cloud" && (
          <section className="panel glass">
            <h2>教学资料云盘</h2>
            <p>
              支持公开HTTPS
              WebDAV接口与兼容网盘网关。连接仅供管理员读取；Codex只接收本次题目的页图和文字。
            </p>
            <label>
              选择连接
              <select
                value={cloud.id}
                onChange={(e) => {
                  const c = clouds.find((c) => c.id === e.target.value);
                  setCloud(
                    c
                      ? { ...c, secret: "" }
                      : {
                          id: "",
                          name: "教学资料",
                          base_url: "",
                          username: "",
                          secret: "",
                          enabled: true,
                        },
                  );
                }}
              >
                <option value="">新建</option>
                {clouds.map((c) => (
                  <option value={c.id} key={c.id}>
                    {c.name}
                  </option>
                ))}
              </select>
            </label>
            <form
              onSubmit={(e) => {
                e.preventDefault();
                void run(async () => {
                  await api("/admin/learning/cloud", {
                    ...cloud,
                    admin_password: password,
                  });
                  setCloud((v) => ({ ...v, secret: "" }));
                  await load();
                  setNotice("连接已保存");
                });
              }}
            >
              {(["name", "base_url", "username", "secret"] as const).map(
                (key) => (
                  <label key={key}>
                    {
                      {
                        name: "名称",
                        base_url: "WebDAV资料根目录URL",
                        username: "用户名",
                        secret: "访问密码／应用令牌（编辑时留空保留）",
                      }[key]
                    }
                    <input
                      type={
                        key === "secret"
                          ? "password"
                          : key === "base_url"
                            ? "url"
                            : "text"
                      }
                      autoComplete="off"
                      value={cloud[key]}
                      onChange={(e) =>
                        setCloud({ ...cloud, [key]: e.target.value })
                      }
                      required={
                        key === "name" ||
                        key === "base_url" ||
                        (key === "secret" && !cloud.id)
                      }
                    />
                  </label>
                ),
              )}
              <label className="check-label">
                <input
                  type="checkbox"
                  checked={cloud.enabled}
                  onChange={(e) =>
                    setCloud({ ...cloud, enabled: e.target.checked })
                  }
                />
                启用连接
              </label>
              <button className="primary" disabled={busy || !password}>
                保存云盘
              </button>
              {cloud.id && (
                <button
                  type="button"
                  className="quiet"
                  disabled={busy}
                  onClick={() =>
                    void run(async () => {
                      const data = await api("/admin/learning/cloud/list", {
                        id: cloud.id,
                        path: "",
                      });
                      setNotice(JSON.stringify(data, null, 2));
                    })
                  }
                >
                  读取根目录
                </button>
              )}
            </form>
            <h3>Codex CLI 入库服务</h3>
            <p>
              按仓库 docs/LEARNING.md
              配置独立服务并登录后，将目录识别、题目入库任务切换为 Codex
              CLI。CLI登录资料留在独立服务内。
            </p>
          </section>
        )}
      </div>
    </main>
  );
}
