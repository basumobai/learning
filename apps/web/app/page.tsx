"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { Icon } from "./icons";
import { api, APIError } from "../lib/api";
import { useProtected, Navigation, Loading } from "./providers";
import { useVoice, VoicePicker, useSpeech, AudioOutput } from "./speech";
import {
  canRate,
  currentQuestion,
  intervalLabel,
  needsRetry,
  parsePending,
  type Kind,
  type Pending,
  type Result,
  type Session,
} from "../lib/study";

type Dashboard = {
  new_today: number;
  new_remaining: number;
  display_name: string;
  due: number;
  studied: number;
  demo_words: number;
  today: {
    reviewed: number;
    unassisted_correct: number;
    again: number;
    awaiting_rating: number;
    used_hint: number;
  };
  dimensions: {
    type: Kind;
    due: number;
    cards: number;
    attempts: number;
    unassisted_correct: number;
  }[];
};
const labels: Record<Kind, string> = {
  recognition: "英译中 · 选择",
  meaning_recall: "英译中 · 默写释义",
  spelling: "中译英 · 拼写",
  listening_spelling: "听音 · 拼写",
};
const ratings = ["重学", "困难", "记住了", "很轻松"];
const storagePrefix = "els-pending-v3:";
function stored(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}
function remember(key: string, value: Pending | null) {
  try {
    if (value) localStorage.setItem(key, JSON.stringify(value));
    else localStorage.removeItem(key);
  } catch {
    /* Server progress still works when local storage is unavailable. */
  }
}
function requestID(): string {
  if (typeof crypto.randomUUID === "function") return crypto.randomUUID();
  const values = crypto.getRandomValues(new Uint8Array(16));
  return Array.from(values, (v) => v.toString(16).padStart(2, "0")).join("");
}

export default function Home() {
  const auth = useProtected();
  const voice = useVoice();
  const speech = useSpeech();
  const storageKey = storagePrefix + (auth.user?.id || "signed-out");
  const [dashboard, setDashboard] = useState<Dashboard | null>(null);
  const [session, setSession] = useState<Session | null>(null);
  const [paused, setPaused] = useState(false);
  const [answer, setAnswer] = useState("");
  const [busy, setBusy] = useState(false);
  const [ready, setReady] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [hint, setHint] = useState(false);
  const [resolution, setResolution] = useState<Result | "">("");
  const [mode, setMode] = useState<"scheduled" | "practice">("scheduled");
  const [types, setTypes] = useState<Kind[]>(["spelling"]);
  const [limit, setLimit] = useState(20);
  const [newLimit, setNewLimit] = useState(10);
  const [pending, setPending] = useState<Pending | null>(null);
  const [online, setOnline] = useState(true);
  const started = useRef(Date.now());
  const lock = useRef(false);
  const pendingRef = useRef<Pending | null>(null);
  const input = useRef<HTMLInputElement>(null);
  const ratingFocus = useRef<HTMLHeadingElement>(null);
  const current = currentQuestion(session);
  const completed =
    session?.questions.filter((q) => q.rating !== null).length || 0;
  const complete = !!session && !current;
  const reviewing = !!current && current.result !== null;

  function track(value: Pending | null) {
    pendingRef.current = value;
    setPending(value);
    remember(storageKey, value);
  }
  async function load(id: number) {
    const loaded = await api<Session>(`/study/sessions/${id}`);
    setSession(loaded);
    return loaded;
  }
  async function synchronize() {
    if (lock.current) return;
    lock.current = true;
    setBusy(true);
    setError("");
    try {
      const [data, active] = await Promise.all([
        api<Dashboard>("/dashboard"),
        api<{ session: Session | null }>("/study/sessions/active"),
      ]);
      setDashboard(data);
      const saved = pendingRef.current || parsePending(stored(storageKey));
      let loaded = active.session;
      if (saved && (!loaded || loaded.id !== saved.sessionId)) {
        try {
          loaded = await api<Session>(`/study/sessions/${saved.sessionId}`);
        } catch (e) {
          if (!(e instanceof APIError && e.status === 404)) throw e;
        }
      } else if (!loaded && session)
        loaded = await api<Session>(`/study/sessions/${session.id}`);
      setSession(loaded);
      if (saved) track(needsRetry(saved, loaded) ? saved : null);
      setReady(true);
    } catch (e) {
      setError((e as Error).message || "暂时无法连接，请检查网络后重试。");
    } finally {
      lock.current = false;
      setBusy(false);
    }
  }

  useEffect(() => {
    if (!auth.allowed) return;
    track(parsePending(stored(storageKey)));
    synchronize();
    const update = () => setOnline(navigator.onLine);
    update();
    window.addEventListener("online", update);
    window.addEventListener("offline", update);
    return () => {
      window.removeEventListener("online", update);
      window.removeEventListener("offline", update);
    };
    // Initial server restore is authoritative, including the feedback stage.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [auth.allowed, storageKey]);
  useEffect(() => {
    speech.stop();
    setAnswer("");
    setHint(false);
    setResolution("");
    started.current = Date.now();
    if (current && !reviewing && !paused) input.current?.focus();
    if (reviewing && !paused) ratingFocus.current?.focus();
  }, [current?.id, reviewing, paused]);

  async function start() {
    if (lock.current || !types.length || !ready || pendingRef.current) return;
    lock.current = true;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const created = await api<{ id: number | null; message?: string }>(
        "/study/sessions",
        { mode, types, limit, new_limit: newLimit },
      );
      if (created.id) {
        await load(created.id);
        setPaused(false);
      } else {
        setSession(null);
        setNotice(created.message || "当前没有符合条件的题目。");
      }
    } catch (e) {
      setError((e as Error).message);
    } finally {
      lock.current = false;
      setBusy(false);
    }
  }

  async function perform(request: Pending) {
    if (lock.current) return;
    lock.current = true;
    setBusy(true);
    setError("");
    track(request);
    try {
      await api(
        `/study/sessions/${request.sessionId}/${request.action}`,
        request.body,
      );
      await load(request.sessionId);
      setReady(true);
      track(null);
      api<Dashboard>("/dashboard")
        .then(setDashboard)
        .catch(() => {});
    } catch (e) {
      setError(
        (e as Error).message ||
          "提交尚未确认。恢复网络后可重试，进度不会自动跳过。",
      );
    } finally {
      lock.current = false;
      setBusy(false);
    }
  }
  function submit(value: string) {
    if (
      !session ||
      !current ||
      reviewing ||
      pendingRef.current ||
      !value.trim()
    )
      return;
    perform({
      action: "answers",
      sessionId: session.id,
      questionId: current.id,
      body: {
        question_id: current.id,
        request_id: requestID(),
        answer: value,
        response_time_ms: Math.max(
          0,
          Math.min(Date.now() - started.current, 3600000),
        ),
        used_hint: hint,
      },
    });
  }
  function rate(rating: number) {
    if (
      !session ||
      !current ||
      pendingRef.current ||
      !canRate(current, rating, resolution)
    )
      return;
    perform({
      action: "review",
      sessionId: session.id,
      questionId: current.id,
      body: {
        question_id: current.id,
        request_id: requestID(),
        rating,
        ...(current.result === "pending"
          ? { resolved_result: resolution }
          : {}),
      },
    });
  }
  function undo() {
    if (!session || pendingRef.current || (!reviewing && !completed)) return;
    const target = reviewing
      ? current
      : [...session.questions].reverse().find((q) => q.rating !== null);
    if (!target) return;
    perform({
      action: "undo",
      sessionId: session.id,
      questionId: target.id,
      body: { request_id: requestID() },
    });
  }
  function speak() {
    if (!current || !session || !voice.model) return;
    speech.play(
      `/study/sessions/${session.id}/questions/${current.id}/speech`,
      voice.body,
    );
  }
  useEffect(() => {
    const keys = (event: KeyboardEvent) => {
      if (
        event.repeat ||
        event.isComposing ||
        event.ctrlKey ||
        event.metaKey ||
        event.altKey ||
        busy ||
        paused ||
        pending ||
        !current
      )
        return;
      if (
        (event.target as HTMLElement)?.closest(
          "input, textarea, select, button",
        )
      )
        return;
      if (
        reviewing &&
        /^[1-4]$/.test(event.key) &&
        canRate(current, Number(event.key), resolution)
      ) {
        event.preventDefault();
        rate(Number(event.key));
      } else if (!reviewing && current.choices && /^[1-4]$/.test(event.key)) {
        const choice = current.choices[Number(event.key) - 1];
        if (choice) {
          event.preventDefault();
          submit(choice);
        }
      }
    };
    window.addEventListener("keydown", keys);
    return () => window.removeEventListener("keydown", keys);
  });

  if (!auth.allowed) return <Loading />;
  return (
    <main className="shell">
      <Navigation />
      <section className="intro home-intro" id="page-content" tabIndex={-1}>
        <div>
          <span className="eyebrow">
            <span className="eyebrow-dot" aria-hidden="true" /> 你好，
            {dashboard?.display_name || auth.user?.display_name}
          </span>
          <h1>
            今天，也进步一点<span className="title-dot">。</span>
          </h1>
          <p>从一个单词，到读懂一句话。按自己的节奏，慢慢积累。</p>
        </div>
        {(!current || paused) && (
          <div className="hero-start">
            <button
              className="primary"
              disabled={!ready || busy || !types.length || !!pending}
              onClick={paused && current ? () => setPaused(false) : start}
            >
              {busy
                ? "正在准备…"
                : paused && current
                  ? "继续当前练习"
                  : "开始今日学习"}
              <Icon name="arrow" />
            </button>
            <span>沿用下方的练习设置</span>
          </div>
        )}
      </section>
      {dashboard && (
        <div className="stats">
          <div className="stat glass">
            <span className="stat-label">
              <span className="stat-icon amber">
                <Icon name="clock" />
              </span>
              待复习能力项
            </span>
            <strong>
              {dashboard.due}
              <small>项</small>
            </strong>
            <span className="stat-note">同词的不同题型分日练习</span>
          </div>
          <div className="stat glass">
            <span className="stat-label">
              <span className="stat-icon green">
                <Icon name="check" />
              </span>
              今日已完成
            </span>
            <strong>
              {dashboard.today.reviewed}
              <small>题</small>
            </strong>
            <span className="stat-note">
              独立记住 {dashboard.today.unassisted_correct} · 重学{" "}
              {dashboard.today.again}
            </span>
          </div>
          <div className="stat glass">
            <span className="stat-label">
              <span className="stat-icon purple">
                <Icon name="library" />
              </span>
              已练习单词
            </span>
            <strong>
              {dashboard.studied}
              <small>个</small>
            </strong>
            <span className="stat-note">
              词库共 {dashboard.demo_words} 个单词
            </span>
          </div>
        </div>
      )}
      {!online && (
        <p className="connection" role="status">
          网络已断开。当前题目仍保留，联网后可确认提交。
        </p>
      )}
      <div className="study-layout">
        <section className="study" aria-label="词汇练习" aria-busy={busy}>
          {!ready && (
            <div className="empty">
              <h2>恢复学习进度…</h2>
              <p>正在读取服务器上的练习记录。</p>
            </div>
          )}
          {ready && (!session || paused || complete) && (
            <div className="empty">
              <div className="step">
                <span className="step-index">{complete ? "✓" : "01"}</span>
                {complete ? "本轮完成" : paused ? "进度已保留" : "开始一轮"}
              </div>
              <h2>
                {complete
                  ? "把这一轮留在记忆里"
                  : paused
                    ? "准备好，再继续"
                    : "开启今天的学习"}
              </h2>
              {complete && (
                <>
                  <p>
                    完成 {completed} 道题。
                    {session.mode === "practice"
                      ? "自由练习已记录，不改变正式复习时间。"
                      : "下一次复习已按记忆评价安排。"}
                  </p>
                  <div className="summary">
                    <span>
                      独立记住{" "}
                      {
                        session.questions.filter(
                          (q) =>
                            q.result === "correct" &&
                            !q.used_hint &&
                            (q.rating || 0) > 1,
                        ).length
                      }
                    </span>
                    <span>
                      重学{" "}
                      {session.questions.filter((q) => q.rating === 1).length}
                    </span>
                  </div>
                </>
              )}
              {paused && current && session && (
                <p>
                  当前练习还有 {session.questions.length - completed}{" "}
                  道题。题型和顺序沿用本轮设置。
                </p>
              )}
              {(!paused || !current) && (
                <div className="settings">
                  <fieldset>
                    <legend>练习方式</legend>
                    <div className="segments">
                      <button
                        aria-pressed={mode === "scheduled"}
                        onClick={() => setMode("scheduled")}
                      >
                        按计划学习
                      </button>
                      <button
                        aria-pressed={mode === "practice"}
                        onClick={() => setMode("practice")}
                      >
                        自由练习
                      </button>
                    </div>
                    <p className="setting-note">
                      {mode === "scheduled"
                        ? "到期优先，新题随机；同一词义每天只练一种题型。"
                        : "随机抽题，可提前练习；不改变 FSRS 复习状态。"}
                    </p>
                  </fieldset>
                  <fieldset>
                    <legend>
                      想练哪些能力？<span className="legend-note">可多选</span>
                    </legend>
                    <div className="type-options">
                      {(Object.keys(labels) as Kind[]).map((kind) => (
                        <label key={kind}>
                          <input
                            type="checkbox"
                            checked={types.includes(kind)}
                            disabled={
                              kind === "listening_spelling" && !voice.model
                            }
                            onChange={() =>
                              setTypes(
                                types.includes(kind)
                                  ? types.filter((t) => t !== kind)
                                  : [...types, kind],
                              )
                            }
                          />
                          <span>{labels[kind]}</span>
                        </label>
                      ))}
                    </div>
                    <p className="setting-note">
                      多选时每个词义只抽一种题型，避免答案互相提示。
                    </p>
                  </fieldset>
                  <div className="limit-options">
                    <label>
                      本轮最多题数
                      <select
                        value={limit}
                        onChange={(e) => setLimit(Number(e.target.value))}
                      >
                        {[5, 10, 20, 50].map((n) => (
                          <option key={n} value={n}>
                            {n} 题
                          </option>
                        ))}
                      </select>
                    </label>
                    {mode === "scheduled" && (
                      <label>
                        本轮新能力项上限
                        <select
                          value={newLimit}
                          onChange={(e) => setNewLimit(Number(e.target.value))}
                        >
                          {[0, 5, 10, 20].map((n) => (
                            <option key={n} value={n}>
                              {n === 0 ? "只复习" : `${n} 项`}
                            </option>
                          ))}
                        </select>
                      </label>
                    )}
                  </div>
                </div>
              )}
              <div className="actions">
                <button
                  className="primary"
                  onClick={paused && current ? () => setPaused(false) : start}
                  disabled={busy || !types.length || !!pending}
                >
                  {busy
                    ? "正在准备…"
                    : paused && current
                      ? "继续当前练习"
                      : complete
                        ? "开始下一轮"
                        : "开始学习"}
                  <Icon name="arrow" />
                </button>
                {complete && (
                  <button
                    className="quiet"
                    onClick={undo}
                    disabled={busy || !!pending}
                  >
                    撤销上一题
                  </button>
                )}
              </div>
              <p className="setting-note">
                先回忆，再核对答案。每天一点点，比一次学很多更容易坚持。
              </p>
            </div>
          )}
          {ready && session && current && !paused && (
            <div className="question" key={current.id}>
              <div className="question-top">
                <span className="step">{labels[current.type]}</span>
                <span>
                  {completed + 1} / {session.questions.length}
                </span>
              </div>
              <div
                className="meter"
                role="progressbar"
                aria-label="已完成题数"
                aria-valuemin={0}
                aria-valuemax={session.questions.length}
                aria-valuenow={completed}
              >
                <span
                  style={{
                    width: `${(completed / session.questions.length) * 100}%`,
                  }}
                />
              </div>
              <p className="instruction">
                {reviewing
                  ? "先核对答案，再告诉自己：这一次真的记住了吗？"
                  : current.type === "listening_spelling"
                    ? "先听发音，再写出英文单词；可以重复播放。"
                    : current.type === "spelling"
                      ? "根据中文写出英文单词"
                      : current.type === "recognition"
                        ? "选择最合适的中文释义"
                        : "主动写出中文意思"}
              </p>
              <h2
                className={
                  current.type === "spelling" ? "prompt chinese" : "prompt"
                }
              >
                {current.prompt}
              </h2>
              {!reviewing && current.type === "listening_spelling" && (
                <>
                  <VoicePicker config={voice} />
                  <button
                    className="primary"
                    onClick={speak}
                    disabled={!voice.model || speech.busy}
                  >
                    {speech.busy ? "准备发音…" : "播放单词"}
                  </button>
                  <AudioOutput player={speech} />
                </>
              )}
              {!reviewing &&
                (current.choices ? (
                  <div className="choices">
                    {current.choices.map((choice, i) => (
                      <button
                        key={choice}
                        disabled={busy || !!pending}
                        onClick={() => submit(choice)}
                      >
                        <span>{i + 1}</span>
                        {choice}
                      </button>
                    ))}
                  </div>
                ) : (
                  <form
                    onSubmit={(e) => {
                      e.preventDefault();
                      submit(answer);
                    }}
                  >
                    <label htmlFor="answer">你的答案</label>
                    <input
                      ref={input}
                      id="answer"
                      autoComplete="off"
                      autoCapitalize="off"
                      spellCheck={false}
                      disabled={busy || !!pending}
                      value={answer}
                      onChange={(e) => setAnswer(e.target.value)}
                      placeholder={
                        ["spelling", "listening_spelling"].includes(
                          current.type,
                        )
                          ? "输入英文单词"
                          : "输入中文释义"
                      }
                    />
                    <div className="actions">
                      <button
                        type="button"
                        className="quiet"
                        onClick={() => setHint(true)}
                        disabled={hint || busy || !!pending}
                      >
                        {hint ? "已使用提示" : "使用提示"}
                      </button>
                      <button
                        className="primary"
                        disabled={busy || !!pending || !answer.trim()}
                      >
                        {busy ? "提交中…" : "核对答案"}
                      </button>
                    </div>
                  </form>
                ))}
              {!reviewing && hint && (
                <p className="hint">
                  提示：{current.hint_text}。这次将按「重学」记录。
                </p>
              )}
              {reviewing && (
                <div className="review-panel">
                  <h3
                    ref={ratingFocus}
                    tabIndex={-1}
                    className={`result-title ${current.result}`}
                  >
                    {current.result === "correct"
                      ? "答案正确"
                      : current.result === "wrong"
                        ? "再记一次，会更牢"
                        : "这条释义需要你核对"}
                  </h3>
                  <dl className="answer-comparison">
                    <div>
                      <dt>你的答案</dt>
                      <dd>{current.answer || "未填写"}</dd>
                    </div>
                    <div>
                      <dt>参考答案</dt>
                      <dd>{current.expected}</dd>
                    </div>
                  </dl>
                  {current.phonetic && (
                    <p className="example">
                      {current.phonetic} · {current.pos}
                    </p>
                  )}
                  {current.example && (
                    <p className="example" lang="en">
                      {current.example}
                    </p>
                  )}
                  <VoicePicker config={voice} />
                  <button
                    className="quiet pronunciation"
                    onClick={speak}
                    disabled={!voice.model || speech.busy}
                  >
                    听自然发音
                  </button>
                  <AudioOutput player={speech} />
                  {current.result === "pending" && (
                    <fieldset className="resolution">
                      <legend>
                        未命中词库别名，请自行核对；本次标记为自评
                      </legend>
                      <div className="segments">
                        <button
                          aria-pressed={resolution === "correct"}
                          onClick={() => setResolution("correct")}
                          disabled={busy || !!pending}
                        >
                          我的释义正确
                        </button>
                        <button
                          aria-pressed={resolution === "wrong"}
                          onClick={() => setResolution("wrong")}
                          disabled={busy || !!pending}
                        >
                          没有记住 / 释义不对
                        </button>
                      </div>
                    </fieldset>
                  )}
                  {(current.used_hint ||
                    current.result === "wrong" ||
                    resolution === "wrong") && (
                    <p className="hint">
                      {current.used_hint
                        ? "这次借助了提示"
                        : "这次没有独立回忆出来"}
                      ，请选择「重学」。
                    </p>
                  )}
                  <p className="rating-instruction">
                    {session.mode === "practice"
                      ? "本次只记录评价，不改变正式复习时间。"
                      : "按真实记忆选择；忘记了就选重学。时间是当前预估。"}
                  </p>
                  <div className="rating-buttons">
                    {ratings.map((label, i) => (
                      <button
                        key={label}
                        className={`rating rating-${i + 1}`}
                        disabled={
                          busy ||
                          !!pending ||
                          !canRate(current, i + 1, resolution)
                        }
                        onClick={() => rate(i + 1)}
                      >
                        <strong>{label}</strong>
                        <small>
                          {session.mode === "scheduled"
                            ? intervalLabel(current.intervals?.[String(i + 1)])
                            : "仅练习"}
                        </small>
                        <kbd>{i + 1}</kbd>
                      </button>
                    ))}
                  </div>
                </div>
              )}
              <div className="study-toolbar">
                <button
                  className="quiet"
                  onClick={() => setPaused(true)}
                  disabled={busy}
                >
                  返回概览
                </button>
                <button
                  className="quiet"
                  onClick={undo}
                  disabled={busy || !!pending || (!reviewing && !completed)}
                >
                  {reviewing ? "重新作答" : "撤销上一题"}
                </button>
              </div>
            </div>
          )}
          {pending && (
            <div className="retry-panel" role="status">
              <p>有一项提交尚未确认。重试会沿用原请求，避免重复记录。</p>
              <button
                className="primary"
                disabled={busy}
                onClick={() => perform(pending)}
              >
                {busy
                  ? "确认中…"
                  : pending.action === "answers"
                    ? "重试答案提交"
                    : pending.action === "review"
                      ? "重试记忆评价"
                      : "重试撤销"}
              </button>
            </div>
          )}
          {notice && (
            <p className="notice" role="status">
              {notice}
            </p>
          )}
          {error && (
            <div className="error" role="alert">
              <p>{error}</p>
              <button className="quiet" onClick={synchronize} disabled={busy}>
                同步服务器进度
              </button>
            </div>
          )}
        </section>
        <aside className="study-aside" aria-label="学习概览">
          {dashboard && (
            <section className="panel glass budget-panel">
              <div className="aside-heading">
                <span className="stat-icon blue">
                  <Icon name="book" />
                </span>
                <h2>今日新词</h2>
              </div>
              <div className="budget-number">
                <strong>{dashboard.new_remaining}</strong>
                <span>个新词可学</span>
              </div>
              <progress
                value={dashboard.new_today}
                max={Math.max(1, dashboard.new_today + dashboard.new_remaining)}
                aria-label="今日新词预算使用情况"
              />
              <p className="setting-note">
                今天已学 {dashboard.new_today}{" "}
                个新词。到期复习不受新词预算影响。
              </p>
              <Link className="text-link" href="/settings">
                调整学习节奏 <Icon name="arrow" />
              </Link>
            </section>
          )}
          {dashboard && dashboard.dimensions.length > 0 && (
            <section
              className="dimension-panel glass"
              aria-label="分能力学习记录"
            >
              <div className="aside-heading">
                <span className="stat-icon purple">
                  <Icon name="library" />
                </span>
                <h2>我的学习能力</h2>
              </div>
              <div className="dimension-list">
                {dashboard.dimensions.map((d) => (
                  <div key={d.type}>
                    <strong>{labels[d.type]}</strong>
                    <span>
                      到期 {d.due} · 练过 {d.cards} 项
                    </span>
                  </div>
                ))}
              </div>
              <p className="setting-note">
                认识、释义、拼写与听音，分别安排复习。
              </p>
            </section>
          )}
        </aside>
      </div>
      <section className="explore" aria-labelledby="explore-title">
        <div className="section-heading">
          <h2 id="explore-title">从记住，到会用</h2>
          <span>换一种方式，理解更深一点。</span>
        </div>
        <div className="learning-links">
          <Link className="learning-link" href="/assistant#chat">
            <span className="feature-icon blue">
              <Icon name="sparkles" />
            </span>
            <div>
              <h3>AI 助学</h3>
              <p>带着问题，和老师聊一聊。</p>
            </div>
            <Icon name="arrow" />
          </Link>
          <Link className="learning-link" href="/assistant#sentence">
            <span className="feature-icon purple">
              <Icon name="text" />
            </span>
            <div>
              <h3>句子拆解</h3>
              <p>找到主干，读懂复杂句子。</p>
            </div>
            <Icon name="arrow" />
          </Link>
          <Link className="learning-link" href="/assistant#listening">
            <span className="feature-icon green">
              <Icon name="headphones" />
            </span>
            <div>
              <h3>听力训练</h3>
              <p>听清信息，也记住信息。</p>
            </div>
            <Icon name="arrow" />
          </Link>
        </div>
      </section>
      <footer>ELS · 每一次理解，都是进步。</footer>
    </main>
  );
}
