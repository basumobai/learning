"use client";
import { AdaptivePanel } from "../components/AdaptivePanel";
import { useMemo } from "react";
import { TimelinePlayer } from "../components/TimelinePlayer";
import { sentenceTimeline } from "../../lib/learning";
import { useEffect, useRef, useState } from "react";
import { api, requestID } from "../../lib/api";
import { parsePendingAI, type PendingAI } from "../../lib/ai-state";
import { useProtected, Navigation, Loading } from "../providers";
import { useVoice, VoicePicker, useSpeech, AudioOutput } from "../speech";
import { Icon, type IconName } from "../icons";
import {
  GuangdongPractice,
  type SpeakingLesson,
} from "../components/GuangdongPractice";

type Conversation = { id: string; title: string; updated_at: string };
type Message = {
  id: number;
  role: "user" | "assistant";
  content: string;
  model_id: number;
};
type TrainingSummary = {
  id: string;
  kind: "sentence" | "listening";
  title: string;
};
type Analysis = {
  translation: string;
  main_clause: string;
  chunks: { text: string; role: string; explanation: string }[];
  grammar: string[];
  paraphrase: string;
  check: { question: string; answer: string; explanation: string };
};
type Training = Partial<Omit<SpeakingLesson, "kind">> & {
  id: string;
  kind: "sentence" | "listening";
  title?: string;
  data?: { text: string; analysis: Analysis };
  questions?: { index: number; question: string; choices: string[] }[];
  feedback?: {
    picked: number;
    answer: number;
    correct: boolean;
    explanation: string;
    evidence: string;
  }[];
  score?: number;
  transcript?: string;
  translation?: string;
};

export default function Assistant() {
  const auth = useProtected();
  const voice = useVoice();
  const speech = useSpeech();
  const [tab, setTab] = useState<"chat" | "sentence" | "listening">("chat");
  const [modelID, setModelID] = useState(0);
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [conversation, setConversation] = useState("");
  const [messages, setMessages] = useState<Message[]>([]);
  const [history, setHistory] = useState<TrainingSummary[]>([]);
  const [lesson, setLesson] = useState<Training | null>(null);
  const [content, setContent] = useState("");
  const [sentence, setSentence] = useState("");
  const [topic, setTopic] = useState("校园生活与一次时间安排");
  const [level, setLevel] = useState("A2");
  const [format, setFormat] = useState("guangdong_b");
  const [focus, setFocus] = useState("记笔记与信息保持");
  const [answers, setAnswers] = useState<number[]>([]);
  const [showCheck, setShowCheck] = useState(false);
  const [showSentenceDemo, setShowSentenceDemo] = useState(false);
  useEffect(() => setShowSentenceDemo(false), [lesson?.id]);
  const [pending, setPending] = useState<PendingAI | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const lock = useRef(false);
  const chatModels = voice.models.filter((m) => m.kind === "chat");
  const selectedModel =
    chatModels.find((m) => m.id === modelID) || chatModels[0];
  const storageKey = `els-ai-pending-v1:${auth.user?.id || ""}`;
  const selectedKey = `els-ai-current-v1:${auth.user?.id || ""}`;
  const requestedTab = useRef<"chat" | "sentence" | "listening" | null>(null);
  useEffect(() => {
    const update = () => {
      const value = window.location.hash.slice(1);
      if (value === "chat" || value === "sentence" || value === "listening") {
        requestedTab.current = value;
        setTab(value);
        speech.stop();
        setShowCheck(false);
      }
    };
    update();
    window.addEventListener("hashchange", update);
    return () => window.removeEventListener("hashchange", update);
    // The speech player is kept in refs; hash navigation must not restart audio.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  function remember(value: PendingAI | null) {
    setPending(value);
    try {
      if (value) sessionStorage.setItem(storageKey, JSON.stringify(value));
      else sessionStorage.removeItem(storageKey);
    } catch {}
  }
  function current(id: string, kind: string) {
    try {
      sessionStorage.setItem(selectedKey, JSON.stringify({ id, kind }));
    } catch {}
  }
  async function histories() {
    const [c, h] = await Promise.all([
      api<{ conversations: Conversation[] }>("/ai/conversations"),
      api<{ training: TrainingSummary[] }>("/ai/training"),
    ]);
    setConversations(c.conversations);
    setHistory(h.training);
    return { c: c.conversations, h: h.training };
  }
  async function loadChat(id: string) {
    const d = await api<{ messages: Message[] }>(`/ai/conversations/${id}`);
    setConversation(id);
    setMessages(d.messages);
    current(id, "chat");
  }
  async function loadTraining(id: string, restoring = false) {
    const d = await api<Training>(`/ai/training/${id}`);
    setLesson(d);
    setAnswers(d.questions?.map(() => -1) || []);
    setShowCheck(false);
    if (restoring) setTab(requestedTab.current || d.kind);
    else switchTab(d.kind);
    current(id, d.kind);
    speech.stop();
  }
  useEffect(() => {
    if (!auth.allowed) return;
    let live = true;
    async function restore() {
      try {
        const h = await histories();
        let saved: { id: string; kind: string } | null = null;
        let job: PendingAI | null = null;
        try {
          job = parsePendingAI(sessionStorage.getItem(storageKey));
          saved = JSON.parse(sessionStorage.getItem(selectedKey) || "null");
        } catch {}
        if (!live) return;
        remember(job);
        if (job?.path.includes("/conversations/")) {
          const id = job.path.split("/")[3];
          await loadChat(id);
          setTab(requestedTab.current || "chat");
          setContent(String(job.body.content || ""));
        } else if (
          saved?.kind === "chat" &&
          h.c.some((c) => c.id === saved!.id)
        )
          await loadChat(saved.id);
        else if (saved && h.h.some((t) => t.id === saved!.id))
          await loadTraining(saved.id, true);
        else if (h.c[0]) await loadChat(h.c[0].id);
      } catch (e) {
        if (live) setError((e as Error).message);
      }
    }
    restore();
    return () => {
      live = false;
    };
  }, [auth.allowed, storageKey]);
  async function request(job: PendingAI) {
    if (lock.current) return;
    lock.current = true;
    setBusy(true);
    setError("");
    remember(job);
    try {
      const result = await api<Training>(job.path, job.body);
      if (job.path.endsWith("/messages")) {
        const id = job.path.split("/")[3];
        await loadChat(id);
        setContent("");
      } else {
        setLesson(result);
        setAnswers(result.questions?.map(() => -1) || []);
        setShowCheck(false);
        setTab(result.kind);
        current(result.id, result.kind);
        speech.stop();
      }
      await histories();
      remember(null);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      lock.current = false;
      setBusy(false);
    }
  }
  async function newChat() {
    if (busy || pending) return;
    setBusy(true);
    setError("");
    try {
      const d = await api<{ id: string }>("/ai/conversations", {
        title: "英语助学",
      });
      setConversation(d.id);
      setMessages([]);
      setTab("chat");
      current(d.id, "chat");
      await histories();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  async function send(e: React.FormEvent) {
    e.preventDefault();
    if (busy || pending || !selectedModel || !content.trim()) return;
    let id = conversation;
    if (!id) {
      setBusy(true);
      try {
        const d = await api<{ id: string }>("/ai/conversations", {
          title: content.slice(0, 40),
        });
        id = d.id;
        setConversation(id);
      } catch (e) {
        setError((e as Error).message);
        setBusy(false);
        return;
      }
      setBusy(false);
    }
    request({
      path: `/ai/conversations/${id}/messages`,
      body: { model_id: selectedModel.id, request_id: requestID(), content },
    });
  }
  function generate(e: React.FormEvent) {
    e.preventDefault();
    if (!selectedModel || pending || busy) return;
    request({
      path: "/ai/training",
      body: {
        kind: tab,
        model_id: selectedModel.id,
        request_id: requestID(),
        text: tab === "sentence" ? sentence : topic,
        level: tab === "listening" ? level : "",
        ...(tab === "listening" ? { format, focus } : {}),
      },
    });
  }
  function switchTab(next: "chat" | "sentence" | "listening") {
    requestedTab.current = next;
    window.history.replaceState(window.history.state, "", `#${next}`);
    setTab(next);
    speech.stop();
    setShowCheck(false);
  }
  const analysis = lesson?.kind === "sentence" ? lesson.data?.analysis : null;
  const dynamicSentence = useMemo(
    () =>
      analysis && lesson?.data
        ? sentenceTimeline(
            lesson.data.text,
            analysis.chunks,
            analysis.main_clause,
          )
        : null,
    [analysis, lesson?.data],
  );
  if (!auth.allowed) return <Loading />;
  return (
    <main className="shell">
      <Navigation />
      <section className="intro" id="page-content" tabIndex={-1}>
        <span className="eyebrow">UNDERSTAND · LISTEN · EXPRESS</span>
        <h1>让“认识”，走到“读懂”。</h1>
        <p>
          把难句拆成能理解的片段，把听力变成有依据的练习。AI
          的讲解与材料可供核对和追问。
        </p>
      </section>
      <div className="segments assistant-tabs" aria-label="助学方式">
        {[
          ["chat", "辅导对话", "sparkles"],
          ["sentence", "句子拆解", "text"],
          ["listening", "听力训练", "headphones"],
        ].map(([id, label, icon]) => (
          <button
            key={id}
            aria-pressed={tab === id}
            onClick={() => switchTab(id as typeof tab)}
          >
            <Icon name={icon as IconName} />
            {label}
          </button>
        ))}
      </div>
      <section className="panel glass model-picker">
        <label>
          当前助学模型
          <select
            value={selectedModel?.id || ""}
            onChange={(e) => setModelID(Number(e.target.value))}
            disabled={busy || !!pending}
          >
            {!chatModels.length && <option value="">尚未授权模型</option>}
            {chatModels.map((m) => (
              <option value={m.id} key={m.id}>
                {m.name}
              </option>
            ))}
          </select>
        </label>
        <p className="setting-note">选择适合当前问题的助学模型。</p>
      </section>
      {tab === "chat" && (
        <section className="assistant-grid">
          <aside className="panel glass">
            <AdaptivePanel
              title={`对话记录 · ${conversations.length}`}
              wideAt={1100}
            >
              <div className="panel-heading">
                <h2>我的对话</h2>
                <button
                  className="quiet"
                  onClick={newChat}
                  disabled={busy || !!pending}
                >
                  新建
                </button>
              </div>
              <div className="history-list">
                {conversations.map((c) => (
                  <button
                    className="quiet"
                    key={c.id}
                    aria-pressed={conversation === c.id}
                    disabled={busy || !!pending}
                    onClick={() =>
                      loadChat(c.id).catch((e) => setError(e.message))
                    }
                  >
                    {c.title}
                  </button>
                ))}
              </div>
            </AdaptivePanel>
          </aside>
          <div className="panel glass chat-panel">
            <div className="messages" aria-live="polite">
              {!messages.length && (
                <div className="empty-chat">
                  <h2>哪里卡住了？</h2>
                  <p>
                    可以贴一句读不懂的英语，或说明：“每个单词都认识，但整句读不懂。”
                  </p>
                </div>
              )}
              {messages.map((m) => (
                <article className={`message ${m.role}`} key={m.id}>
                  <strong>{m.role === "user" ? "你" : "AI 助学老师"}</strong>
                  <div>{m.content}</div>
                </article>
              ))}
            </div>
            <form className="form-stack" onSubmit={send}>
              <label>
                你的问题
                <textarea
                  value={content}
                  onChange={(e) => setContent(e.target.value)}
                  rows={4}
                  maxLength={12000}
                  placeholder="贴句子、提问题，或描述学习困难…"
                  disabled={busy || !!pending}
                  required
                />
              </label>
              <button
                className="primary"
                disabled={
                  busy || !!pending || !selectedModel || !content.trim()
                }
              >
                {busy ? "老师正在整理思路…" : "发送问题"}
              </button>
            </form>
          </div>
        </section>
      )}
      {tab === "sentence" && (
        <>
          <section className="panel glass">
            <h2>把长句拆开看</h2>
            <form className="form-stack" onSubmit={generate}>
              <label>
                英语原句
                <textarea
                  value={sentence}
                  onChange={(e) => setSentence(e.target.value)}
                  rows={4}
                  maxLength={4000}
                  placeholder="例如：The book that you lent me yesterday helped me understand the problem."
                  required
                  disabled={busy || !!pending}
                />
              </label>
              <button
                className="primary"
                disabled={
                  busy || !!pending || !selectedModel || !sentence.trim()
                }
              >
                {busy ? "正在分析句子…" : "拆解这句话"}
              </button>
            </form>
          </section>
          {analysis && lesson?.data && (
            <section className="panel glass sentence-analysis">
              <h2 lang="en">{lesson.data.text}</h2>
              <p>{analysis.translation}</p>
              <div className="main-clause">
                <span>先找主干</span>
                <strong>{analysis.main_clause}</strong>
              </div>
              <div className="sentence-chunks">
                {analysis.chunks.map((c, i) => (
                  <article key={i}>
                    <span>{c.role}</span>
                    <h3 lang="en">{c.text}</h3>
                    <p>{c.explanation}</p>
                  </article>
                ))}
              </div>
              <h3>连接与语法</h3>
              <ul>
                {analysis.grammar.map((g, i) => (
                  <li key={i}>{g}</li>
                ))}
              </ul>
              <h3>换成更容易的英语</h3>
              <p lang="en">{analysis.paraphrase}</p>
              <VoicePicker config={voice} />
              <button
                className="quiet"
                disabled={!voice.model || speech.busy}
                onClick={() =>
                  speech.play("/ai/speech", {
                    ...voice.body,
                    input: lesson.data!.text,
                  })
                }
              >
                听原句
              </button>
              <AudioOutput player={speech} />
              <div className="understanding-check">
                <h3>确认自己读懂了</h3>
                <p>{analysis.check.question}</p>
                <button
                  className="quiet"
                  onClick={() => setShowCheck(!showCheck)}
                >
                  {showCheck ? "收起答案" : "查看参考答案"}
                </button>
                {showCheck && (
                  <p>
                    {analysis.check.answer}
                    <br />
                    {analysis.check.explanation}
                  </p>
                )}
              </div>
              {showCheck && dynamicSentence && (
                <button
                  className="quiet"
                  onClick={() => setShowSentenceDemo((v) => !v)}
                >
                  {showSentenceDemo ? "收起动态演示" : "没理解，动态演示"}
                </button>
              )}
              {showCheck && showSentenceDemo && dynamicSentence && (
                <div>
                  <h3>答错或没理解？跟着标注重新看。</h3>
                  <TimelinePlayer timeline={dynamicSentence} />
                </div>
              )}
              <p className="setting-note">
                AI 生成的讲解，遇到疑点可以复制到辅导对话继续核对。
              </p>
            </section>
          )}
        </>
      )}
      {tab === "listening" && (
        <>
          <section className="panel glass">
            <h2>听清信息，也记住信息</h2>
            <form className="form-stack" onSubmit={generate}>
              <div className="form-grid">
                <label>
                  训练题型
                  <select
                    value={format}
                    onChange={(e) => setFormat(e.target.value)}
                    disabled={busy || !!pending}
                  >
                    <option value="guangdong_a">广东 Part A · 模仿朗读</option>
                    <option value="guangdong_b">广东 Part B · 三问五答</option>
                    <option value="guangdong_c">广东 Part C · 故事复述</option>
                    <option value="choice">基础听力 · 四选一</option>
                  </select>
                </label>
                <label>
                  训练重点
                  <select
                    value={focus}
                    onChange={(e) => setFocus(e.target.value)}
                    disabled={busy || !!pending}
                  >
                    {[
                      "记笔记与信息保持",
                      "时间数字与地点变更",
                      "英语问句结构",
                      "故事主线与转折",
                      "意群停顿与连读",
                    ].map((x) => (
                      <option key={x}>{x}</option>
                    ))}
                  </select>
                </label>
                <label>
                  听力主题
                  <input
                    value={topic}
                    onChange={(e) => setTopic(e.target.value)}
                    maxLength={200}
                    required
                    disabled={busy || !!pending}
                  />
                </label>
                <label>
                  难度
                  <select
                    value={level}
                    onChange={(e) => setLevel(e.target.value)}
                    disabled={busy || !!pending}
                  >
                    <option value="A2">A2 · 先听懂日常信息</option>
                    <option value="B1">B1 · 高中基础训练</option>
                    <option value="B2">B2 · 挑战复杂信息</option>
                  </select>
                </label>
              </div>
              <button
                className="primary"
                disabled={busy || !!pending || !selectedModel || !topic.trim()}
              >
                {busy ? "正在编写练习…" : "生成新的听力练习"}
              </button>
            </form>
          </section>
          {lesson?.kind === "listening" && lesson.speaking_tasks && (
            <GuangdongPractice
              key={lesson.id}
              lesson={lesson as SpeakingLesson}
              modelID={selectedModel?.id || 0}
              onUpdate={(d) => setLesson(d as Training)}
            />
          )}
          {lesson?.kind === "listening" && !lesson.speaking_tasks && (
            <section className="panel glass listening-lesson">
              <h2>{lesson.title}</h2>
              <p>
                先浏览问题，再听材料。可以重复播放或放慢速度；交卷后核对原文与答案依据。
              </p>
              <VoicePicker config={voice} />
              <button
                className="primary"
                disabled={!voice.model || speech.busy}
                onClick={() =>
                  speech.play(`/ai/training/${lesson.id}/speech`, voice.body)
                }
              >
                播放听力材料
              </button>
              <AudioOutput player={speech} />
              <div className="listening-questions">
                {lesson.questions?.map((q, i) => (
                  <fieldset key={i}>
                    <legend>
                      {i + 1}. {q.question}
                    </legend>
                    <div className="choices">
                      {q.choices.map((choice, j) => (
                        <button
                          key={j}
                          disabled={!!lesson.feedback || busy || !!pending}
                          aria-pressed={
                            answers[i] === j ||
                            lesson.feedback?.[i].picked === j
                          }
                          onClick={() =>
                            setAnswers(answers.map((a, k) => (k === i ? j : a)))
                          }
                        >
                          <span>{"ABCD"[j]}</span>
                          {choice}
                        </button>
                      ))}
                    </div>
                    {lesson.feedback && (
                      <div
                        className={
                          lesson.feedback[i].correct
                            ? "feedback"
                            : "feedback wrong"
                        }
                      >
                        <strong>
                          {lesson.feedback[i].correct
                            ? "答对了"
                            : `正确答案：${"ABCD"[lesson.feedback[i].answer]}`}
                        </strong>
                        <p>{lesson.feedback[i].explanation}</p>
                        <p className="evidence" lang="en">
                          原文依据：{lesson.feedback[i].evidence}
                        </p>
                      </div>
                    )}
                  </fieldset>
                ))}
              </div>
              {!lesson.feedback ? (
                <button
                  className="primary"
                  disabled={
                    busy ||
                    !!pending ||
                    answers.some((a) => a < 0) ||
                    !answers.length
                  }
                  onClick={() =>
                    request({
                      path: `/ai/training/${lesson.id}/answers`,
                      body: { request_id: requestID(), answers },
                    })
                  }
                >
                  提交并查看解析
                </button>
              ) : (
                <div className="transcript">
                  <h3>
                    本次 {lesson.score} / {lesson.questions?.length} ·
                    原文与译文
                  </h3>
                  <p lang="en">{lesson.transcript}</p>
                  <p>{lesson.translation}</p>
                </div>
              )}
              <p className="setting-note">
                原创 AI 练习与 AI 生成语音。先练关键词与转折，再逐步提高难度。
              </p>
            </section>
          )}
        </>
      )}
      {tab !== "chat" && (
        <section className="panel glass">
          <h2>保存的训练</h2>
          <div className="history-list">
            {history
              .filter((t) => t.kind === tab)
              .map((t) => (
                <button
                  className="quiet"
                  key={t.id}
                  disabled={busy || !!pending}
                  onClick={() =>
                    loadTraining(t.id).catch((e) => setError(e.message))
                  }
                >
                  {t.title}
                </button>
              ))}
          </div>
          {!history.some((t) => t.kind === tab) && (
            <p className="setting-note">
              生成的练习会保存在账户中，可在这里恢复。
            </p>
          )}
        </section>
      )}
      {pending && (
        <div className="notice" role="status">
          <p>有一项请求尚未确认。重试会使用原编号，已完成的结果会直接恢复。</p>
          <div className="actions">
            <button
              className="primary"
              disabled={busy}
              onClick={() => request(pending)}
            >
              恢复或重试
            </button>
            <button
              className="quiet"
              disabled={busy}
              onClick={() => {
                remember(null);
                histories().catch((e) => setError(e.message));
              }}
            >
              查看历史后清除待确认项
            </button>
          </div>
        </div>
      )}
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      {!chatModels.length && (
        <p className="notice">
          助学模型需要由管理员配置接口并授权到你的权限组。
        </p>
      )}
    </main>
  );
}
