"use client";
import { MathText } from "../components/MathText";
import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { api, requestID, upload } from "../../lib/api";
import {
  subjects,
  hanCount,
  timeLabel,
  type Job,
  type QuestionContent,
  type Timeline,
} from "../../lib/learning";
import { Loading, Navigation, useProtected } from "../providers";
import { TimelinePlayer } from "../components/TimelinePlayer";
type Chapter = {
  id: string;
  subject_id: string;
  parent_id: string | null;
  title: string;
  textbook: string;
  term: string;
  question_count: number;
};
type Catalog = {
  subjects: {
    id: string;
    types: { id: string; name: string; objective: boolean }[];
  }[];
  chapters: Chapter[];
};
type Feedback = {
  score: number;
  max_score: number;
  needs_ai: boolean;
  ai_reference: boolean;
  incomplete?: boolean;
  han_count?: number;
  timed_out?: boolean;
  ai_grading?: {
    feedback: string;
    parts: { id: string; score: number; reason: string; mistake: string }[];
  };
  essay_review?: {
    score: number;
    reason: string;
    disagreement: string;
    suggestions: string[];
  };
  independent_reviews?: {
    score: number;
    reason: string;
    evidence: string[];
    suggestions: string[];
  }[];
};
type Attempt = {
  draft_version?: number;
  id: string;
  question: {
    id: string;
    subject_id: string;
    kind: string;
    content: QuestionContent;
    solution: {
      answers: Record<string, string>;
      explanation: string;
      rubric: string;
    };
    assets: { id: string; alt: string }[];
  };
  deadline: string | null;
  server_time: string;
  submitted_at: string | null;
  answers: Record<string, string> | null;
  draft: Record<string, string> | null;
  feedback: Feedback | null;
  jobs?: Job[];
};
export default function Practice() {
  const auth = useProtected();
  const [catalog, setCatalog] = useState<Catalog | null>(null),
    [subject, setSubject] = useState("english"),
    [kind, setKind] = useState(""),
    [textbook, setTextbook] = useState(""),
    [term, setTerm] = useState(""),
    [chapters, setChapters] = useState<string[]>([]),
    [attempt, setAttempt] = useState<Attempt | null>(null),
    [answers, setAnswers] = useState<Record<string, string>>({}),
    [job, setJob] = useState<Job | null>(null),
    [step, setStep] = useState(""),
    [question, setQuestion] = useState(""),
    [chat, setChat] = useState<{ question: string; answer: string }[]>([]),
    [personal, setPersonal] = useState(false),
    [error, setError] = useState(""),
    [explainError, setExplainError] = useState(""),
    [busy, setBusy] = useState(false),
    [seconds, setSeconds] = useState<number | null>(null),
    [saving, setSaving] = useState("");
  const offset = useRef(0),
    draftVersion = useRef(0),
    answersRef = useRef(answers),
    submitRef = useRef(false),
    dirty = useRef(false),
    attemptRef = useRef<Attempt | null>(null),
    prefetch = useRef(""),
    nextRequest = useRef("");
  answersRef.current = answers;
  attemptRef.current = attempt;
  function install(a: Attempt) {
    setSubject(a.question.subject_id);
    setKind(a.question.kind);
    setAttempt(a);
    draftVersion.current = a.draft_version || 0;
    setAnswers(a.answers || a.draft || {});
    offset.current = Date.parse(a.server_time) - Date.now();
    dirty.current = false;
    setSaving("");
  }
  useEffect(() => {
    if (!auth.allowed) return;
    const params = new URLSearchParams(window.location.search);
    const requested = params.get("subject");
    if (subjects.some((s) => s.id === requested)) setSubject(requested!);
    api<Catalog>("/learning/catalog")
      .then(setCatalog)
      .catch((e) => setError(e.message));
    const id = params.get("attempt");
    if (id)
      api<Attempt>(`/learning/attempts/${encodeURIComponent(id)}`)
        .then(install)
        .catch((e) => setError(e.message));
  }, [auth.allowed]);
  const types = catalog?.subjects.find((s) => s.id === subject)?.types || [];
  useEffect(() => {
    if (types.length && !types.some((t) => t.id === kind)) setKind(types[0].id);
  }, [types, kind]);
  useEffect(() => {
    setChapters([]);
    setTextbook("");
    setTerm("");
  }, [subject, kind]);
  const requestExplain = useCallback(
    async (a: Attempt, force = false) => {
      const key = `${a.id}:${personal}`;
      if (prefetch.current === key && !force) return;
      prefetch.current = key;
      setExplainError("");
      try {
        const result = await api<{ id: string }>(
          `/learning/attempts/${a.id}/explanation`,
          { personal },
        );
        setJob(await api<Job>(`/learning/jobs/${result.id}`));
      } catch (e) {
        setExplainError((e as Error).message);
      }
    },
    [personal],
  );
  useEffect(() => {
    if (attempt) void requestExplain(attempt);
  }, [attempt?.id, requestExplain]);
  useEffect(() => {
    if (
      !job ||
      job.status === "failed" ||
      (job.status === "completed" && (!attempt?.submitted_at || job.result))
    )
      return;
    let active = true;
    const timer = setInterval(
      () =>
        api<Job>(`/learning/jobs/${job.id}`)
          .then((j) => {
            if (active) setJob(j);
          })
          .catch((e) => {
            if (active) setExplainError(e.message);
          }),
      2000,
    );
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [job?.id, job?.status, !!job?.result, attempt?.submitted_at]);
  useEffect(() => {
    if (!attempt?.submitted_at || !attempt.feedback?.needs_ai) return;
    const timer = setInterval(
      () =>
        api<Attempt>(`/learning/attempts/${attempt.id}`)
          .then(setAttempt)
          .catch((e) => setError(e.message)),
      3000,
    );
    return () => clearInterval(timer);
  }, [attempt?.id, attempt?.submitted_at, attempt?.feedback?.needs_ai]);
  const saveDraft = useCallback(async () => {
    const a = attemptRef.current;
    if (!a || a.submitted_at || !dirty.current) return;
    const current = answersRef.current;
    try {
      await api(`/learning/attempts/${a.id}/draft`, {
        answers: current,
        version: draftVersion.current,
      });
      if (current === answersRef.current) dirty.current = false;
      setSaving("已保存");
    } catch (e) {
      setSaving((e as Error).message);
    }
  }, []);
  useEffect(() => {
    const timer = setInterval(() => void saveDraft(), 1500);
    return () => clearInterval(timer);
  }, [saveDraft]);
  const submit = useCallback(async () => {
    const a = attemptRef.current;
    if (!a || a.submitted_at || submitRef.current) return;
    submitRef.current = true;
    setBusy(true);
    setError("");
    try {
      if (!a.deadline || Date.now() + offset.current < Date.parse(a.deadline))
        await saveDraft();
      const next = await api<Attempt>(`/learning/attempts/${a.id}/submit`, {
        answers: answersRef.current,
        personal,
      });
      install(next);
      if (job?.id) setJob(await api<Job>(`/learning/jobs/${job.id}`));
    } catch (e) {
      setError((e as Error).message);
    } finally {
      submitRef.current = false;
      setBusy(false);
    }
  }, [personal, job?.id, saveDraft]);
  useEffect(() => {
    if (!attempt?.deadline || attempt.submitted_at) {
      setSeconds(null);
      return;
    }
    const tick = () => {
      const left =
        (Date.parse(attempt.deadline!) - Date.now() - offset.current) / 1000;
      setSeconds(Math.max(0, left));
      if (left <= 0) void submit();
    };
    tick();
    const timer = setInterval(tick, 300);
    return () => clearInterval(timer);
  }, [attempt?.id, attempt?.deadline, attempt?.submitted_at, submit]);
  async function start() {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      if (!nextRequest.current) nextRequest.current = requestID();
      const a = await api<Attempt>("/learning/attempts", {
        subject_id: subject,
        kind,
        chapter_ids: chapters,
        textbook,
        term,
        request_id: nextRequest.current,
      });
      nextRequest.current = "";
      install(a);
      setChat([]);
      setJob(null);
      window.history.replaceState(
        null,
        "",
        `/practice?subject=${subject}&attempt=${a.id}`,
      );
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  function answer(id: string, text: string) {
    draftVersion.current++;
    setAnswers((v) => ({ ...v, [id]: text }));
    dirty.current = true;
    setSaving("等待保存…");
  }
  async function ask() {
    if (!attempt || !job || !step || !question.trim() || busy) return;
    setBusy(true);
    setError("");
    const text = question;
    try {
      const result = await api<{ answer: string }>(
        `/learning/attempts/${attempt.id}/chat`,
        {
          job_id: job.id,
          step_id: step,
          content: text,
          request_id: requestID(),
          personal,
        },
      );
      setChat((v) => [...v, { question: text, answer: result.answer }]);
      setQuestion("");
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  async function importText(file: File) {
    setError("");
    try {
      const data = new FormData();
      data.append("file", file);
      const result = await upload<{ text: string }>(
        "/learning/text-import",
        data,
      );
      const id = attempt?.question.content.parts[0]?.id;
      if (id) answer(id, result.text);
    } catch (e) {
      setError((e as Error).message);
    }
  }
  if (!auth.allowed) return <Loading />;
  const current = subjects.find((s) => s.id === subject)!;
  const available =
    catalog?.chapters.filter(
      (c) =>
        c.subject_id === subject &&
        (!textbook || c.textbook === textbook) &&
        (!term || c.term === term),
    ) || [];
  const submitted = !!attempt?.submitted_at;
  const timeline = job?.result as Timeline | undefined;
  const essay =
    attempt?.question.subject_id === "chinese" &&
    attempt.question.kind === "essay";
  return (
    <main className="shell">
      <Navigation />
      <div id="page-content" className="practice-layout">
        <header className="page-intro">
          <span className="eyebrow">广东高中 · 专项练习</span>
          <h1>
            {attempt
              ? subjects.find((s) => s.id === attempt.question.subject_id)?.name
              : current.name}
            ，一步步学懂。
          </h1>
          <p>先选题型，再划定教材与章节范围。</p>
        </header>
        <div className="subject-tabs" role="group" aria-label="选择科目">
          {subjects.map((s) => (
            <button
              key={s.id}
              className={subject === s.id ? "primary" : "quiet"}
              disabled={!!attempt && !submitted}
              onClick={() => {
                setSubject(s.id);
                setAttempt(null);
                setJob(null);
              }}
            >
              {s.name}
            </button>
          ))}
        </div>
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        {!attempt && (
          <section className="panel glass filter-panel">
            <h2>今天练什么？</h2>
            <div className="learning-filters">
              <label>
                题型
                <select value={kind} onChange={(e) => setKind(e.target.value)}>
                  {types.map((t) => (
                    <option value={t.id} key={t.id}>
                      {t.name}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                教材范围
                <select
                  value={textbook}
                  onChange={(e) => {
                    setTextbook(e.target.value);
                    setChapters([]);
                  }}
                >
                  <option value="">全部教材</option>
                  {Array.from(
                    new Set(
                      catalog?.chapters
                        .filter((c) => c.subject_id === subject)
                        .map((c) => c.textbook)
                        .filter(Boolean),
                    ),
                  ).map((v) => (
                    <option key={v}>{v}</option>
                  ))}
                </select>
              </label>
              <label>
                学期
                <select
                  value={term}
                  onChange={(e) => {
                    setTerm(e.target.value);
                    setChapters([]);
                  }}
                >
                  <option value="">全部学期</option>
                  {Array.from(
                    new Set(
                      catalog?.chapters
                        .filter((c) => c.subject_id === subject)
                        .map((c) => c.term)
                        .filter(Boolean),
                    ),
                  ).map((v) => (
                    <option key={v}>{v}</option>
                  ))}
                </select>
              </label>
            </div>
            <fieldset className="chapter-picker">
              <legend>练习章节 · 可多选，未选则使用全部范围</legend>
              {available
                .filter((c) => Number(c.question_count) > 0)
                .map((c) => (
                  <label key={c.id}>
                    <input
                      type="checkbox"
                      checked={chapters.includes(c.id)}
                      onChange={(e) =>
                        setChapters((v) =>
                          e.target.checked
                            ? [...v, c.id]
                            : v.filter((id) => id !== c.id),
                        )
                      }
                    />
                    <span>
                      {c.title}
                      <small>
                        {c.textbook} · {c.question_count}题
                      </small>
                    </span>
                  </label>
                ))}
              {!available.some((c) => Number(c.question_count) > 0) && (
                <p className="empty-state">
                  这个范围还没有已发布题目。
                  {auth.user?.role === "admin" && (
                    <Link href="/admin/learning">去导入教学资料</Link>
                  )}
                </p>
              )}
            </fieldset>
            <label className="check-label">
              <input
                type="checkbox"
                checked={personal}
                onChange={(e) => setPersonal(e.target.checked)}
              />
              使用我的 API 生成讲解和评价（<Link href="/settings">配置</Link>）
            </label>
            {subject === "chinese" && (
              <p className="setting-note">
                平台训练：作文30分钟，有效汉字超过850；阅读30分钟。上传Word或TXT也计入训练时间。
              </p>
            )}
            <button
              className="primary"
              disabled={busy || !kind}
              onClick={start}
            >
              {busy ? "正在选题…" : "开始练习"}
            </button>
            {subject === "english" && (
              <p className="section-links">
                <Link href="/english">词汇复习</Link> ·{" "}
                <Link href="/challenge">极速四选一</Link> ·{" "}
                <Link href="/assistant">长句与听力</Link>
              </p>
            )}
          </section>
        )}
        {attempt && (
          <>
            <section className="panel glass question-panel">
              <div className="question-heading">
                <span className="eyebrow">
                  {submitted ? "练习反馈" : "专注这一题"}
                </span>
                {seconds !== null && (
                  <strong
                    className={`practice-timer ${seconds < 60 ? "urgent" : ""}`}
                    role="timer"
                    aria-label="剩余时间"
                  >
                    {timeLabel(seconds)}
                  </strong>
                )}
              </div>
              <h2>
                <MathText text={attempt.question.content.text} />
              </h2>
              {attempt.question.content.passage && (
                <div className="reading-passage">
                  <MathText text={attempt.question.content.passage} />
                </div>
              )}
              {attempt.question.assets.map((a) => (
                <figure className="question-figure" key={a.id}>
                  <img src={`/api/v1/learning/assets/${a.id}`} alt={a.alt} />
                  <figcaption>{a.alt}</figcaption>
                </figure>
              ))}
              {essay && !submitted && (
                <label className="file-button">
                  导入 .docx / .txt
                  <input
                    type="file"
                    accept=".docx,.txt"
                    onChange={(e) => {
                      if (e.target.files?.[0])
                        void importText(e.target.files[0]);
                      e.target.value = "";
                    }}
                  />
                </label>
              )}
              {attempt.question.content.parts.map((p) => (
                <fieldset className="question-part" key={p.id}>
                  <legend>
                    {p.id}. <MathText text={p.text} />{" "}
                    <small>{p.points}分</small>
                  </legend>
                  {p.choices?.length ? (
                    <div className="question-choices">
                      {p.choices.map((c) => (
                        <label
                          key={c.id}
                          className={
                            (answers[p.id] || "").split(",").includes(c.id)
                              ? "picked"
                              : ""
                          }
                        >
                          <input
                            type={
                              attempt.question.kind === "multiple_choice"
                                ? "checkbox"
                                : "radio"
                            }
                            name={`part-${p.id}`}
                            disabled={submitted || busy}
                            checked={(answers[p.id] || "")
                              .split(",")
                              .includes(c.id)}
                            onChange={(e) => {
                              if (attempt.question.kind === "multiple_choice") {
                                const v = (answers[p.id] || "")
                                  .split(",")
                                  .filter(Boolean)
                                  .filter((id) => id !== c.id);
                                if (e.target.checked) v.push(c.id);
                                answer(p.id, v.sort().join(","));
                              } else answer(p.id, c.id);
                            }}
                          />
                          <span>
                            <b>{c.id}</b> <MathText text={c.text} />
                          </span>
                        </label>
                      ))}
                    </div>
                  ) : (
                    <textarea
                      aria-label={`第${p.id}题作答`}
                      rows={essay ? 18 : 5}
                      value={answers[p.id] || ""}
                      disabled={submitted || busy}
                      maxLength={12000}
                      onChange={(e) => answer(p.id, e.target.value)}
                      placeholder="写下你的思路与答案…"
                    />
                  )}
                  {submitted && (
                    <p className="reference-answer">
                      参考答案：
                      <MathText
                        text={attempt.question.solution.answers[p.id]}
                      />
                    </p>
                  )}
                </fieldset>
              ))}
              {essay && (
                <p className="word-counter">
                  有效汉字 {hanCount(Object.values(answers).join(""))} / 至少851{" "}
                  <span>标点、数字、字母不计入</span>
                </p>
              )}
              {!submitted ? (
                <div className="question-submit">
                  <button
                    className="primary"
                    disabled={busy}
                    onClick={() => void submit()}
                  >
                    {busy ? "正在交卷…" : "提交并看讲解"}
                  </button>
                  <span className="setting-note" role="status">
                    {saving}
                  </span>
                </div>
              ) : (
                <>
                  <div className="learning-feedback">
                    <h3>
                      {attempt.feedback?.incomplete
                        ? "作文未达到训练字数"
                        : attempt.feedback?.needs_ai
                          ? "已交卷，正在等待 AI 评价"
                          : `参考得分 ${attempt.feedback?.score ?? 0} / ${attempt.feedback?.max_score ?? 0}`}
                    </h3>
                    {attempt.feedback?.timed_out && (
                      <p>训练时间已结束，使用截止前保存的作答。</p>
                    )}
                    {attempt.feedback?.ai_reference && (
                      <p className="setting-note">
                        AI 训练参考评分，请结合教师意见。
                      </p>
                    )}
                    {attempt.feedback?.ai_grading && (
                      <>
                        <p>{attempt.feedback.ai_grading.feedback}</p>
                        {attempt.feedback.ai_grading.parts.map((p) => (
                          <p key={p.id}>
                            {p.id}：{p.score}分 · {p.reason}
                            <br />
                            错因：{p.mistake}
                          </p>
                        ))}
                      </>
                    )}
                    {attempt.feedback?.essay_review && (
                      <>
                        <p>{attempt.feedback.essay_review.reason}</p>
                        <p>{attempt.feedback.essay_review.disagreement}</p>
                        <ul>
                          {attempt.feedback.essay_review.suggestions.map(
                            (s, i) => (
                              <li key={i}>{s}</li>
                            ),
                          )}
                        </ul>
                        <details>
                          <summary>查看独立评审</summary>
                          {attempt.feedback.independent_reviews?.map((v, i) => (
                            <article key={i}>
                              <h4>
                                评审{i + 1} · {v.score}分
                              </h4>
                              <p>{v.reason}</p>
                              {v.evidence.map((s, j) => (
                                <blockquote key={j}>{s}</blockquote>
                              ))}
                              <ul>
                                {v.suggestions.map((s, j) => (
                                  <li key={j}>{s}</li>
                                ))}
                              </ul>
                            </article>
                          ))}
                        </details>
                      </>
                    )}
                    {attempt.jobs
                      ?.filter((j) => j.status === "failed")
                      .map((j) => (
                        <p key={j.id}>
                          {j.error}
                          <button
                            className="quiet"
                            onClick={() =>
                              api(`/learning/jobs/${j.id}/retry`, {})
                                .then(() =>
                                  api<Attempt>(
                                    `/learning/attempts/${attempt.id}`,
                                  ),
                                )
                                .then(setAttempt)
                                .catch((e) => setError(e.message))
                            }
                          >
                            重试评价
                          </button>
                        </p>
                      ))}
                  </div>
                  <details open>
                    <summary>参考解析</summary>
                    <p className="preserve-text">
                      <MathText text={attempt.question.solution.explanation} />
                    </p>
                    <p>
                      <MathText text={attempt.question.solution.rubric} />
                    </p>
                  </details>
                  <button
                    className="primary"
                    onClick={() => {
                      setAttempt(null);
                      setJob(null);
                      prefetch.current = "";
                      window.history.replaceState(
                        null,
                        "",
                        `/practice?subject=${subject}`,
                      );
                    }}
                  >
                    继续选题
                  </button>
                </>
              )}
            </section>
            {submitted && (
              <section className="panel glass dynamic-panel">
                <h2>让思路动起来。</h2>
                {timeline ? (
                  <>
                    <TimelinePlayer
                      timeline={timeline}
                      assets={attempt.question.assets}
                      onStep={setStep}
                    />
                    <section className="embedded-tutor">
                      <h3>这一步，还没理解？</h3>
                      <p className="setting-note">
                        只围绕当前题目和选中的步骤追问。
                      </p>
                      {chat.map((v, i) => (
                        <div key={i} className="step-dialogue">
                          <p>
                            <strong>你：</strong>
                            {v.question}
                          </p>
                          <p className="preserve-text">
                            <strong>AI：</strong>
                            {v.answer}
                          </p>
                        </div>
                      ))}
                      <form
                        onSubmit={(e) => {
                          e.preventDefault();
                          void ask();
                        }}
                      >
                        <label>
                          对当前步骤的疑问
                          <textarea
                            value={question}
                            onChange={(e) => setQuestion(e.target.value)}
                            maxLength={2000}
                            rows={3}
                            placeholder="例如：为什么这部分是定语，而不是主句？"
                          />
                        </label>
                        <button
                          className="primary"
                          disabled={busy || !question.trim() || !step}
                        >
                          问问 AI
                        </button>
                      </form>
                    </section>
                  </>
                ) : (
                  <>
                    <p role="status">
                      {job?.status === "failed"
                        ? job.error
                        : explainError || "正在后台生成这道题的动态讲解…"}
                    </p>
                    <button
                      className="quiet"
                      disabled={busy}
                      onClick={() =>
                        job?.status === "failed"
                          ? api(`/learning/jobs/${job.id}/retry`, {})
                              .then(() => setJob({ ...job, status: "queued" }))
                              .catch((e) => setExplainError(e.message))
                          : void requestExplain(attempt, true)
                      }
                    >
                      重试生成
                    </button>
                  </>
                )}
              </section>
            )}
          </>
        )}
      </div>
    </main>
  );
}
