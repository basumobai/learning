'use client';

import { useEffect, useRef, useState } from 'react';
import { canRate, currentQuestion, intervalLabel, needsRetry, parsePending, type Kind, type Pending, type Result, type Session } from '../lib/study';

type Dashboard = {
  display_name: string; due: number; studied: number; demo_words: number;
  today: { reviewed: number; unassisted_correct: number; again: number; awaiting_rating: number; used_hint: number };
  dimensions: { type: Kind; due: number; cards: number; attempts: number; unassisted_correct: number }[];
};
class APIError extends Error { constructor(message: string, public status: number) { super(message); } }
async function api<T>(path: string, body?: object): Promise<T> {
  const response = await fetch(`/api/v1${path}`, body ? { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) } : { cache: 'no-store' });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new APIError(payload.error || '请求失败，请稍后重试', response.status);
  return payload as T;
}
const labels: Record<Kind, string> = { recognition: '英译中 · 选择', meaning_recall: '英译中 · 默写释义', spelling: '中译英 · 拼写' };
const ratings = ['重学', '困难', '记住了', '很轻松'];
const storageKey = 'els-pending-v2';
function stored(key: string): string | null { try { return localStorage.getItem(key); } catch { return null; } }
function remember(value: Pending | null) { try { if (value) localStorage.setItem(storageKey, JSON.stringify(value)); else localStorage.removeItem(storageKey); } catch { /* Server progress still works when local storage is unavailable. */ } }
function requestID(): string {
  if (typeof crypto.randomUUID === 'function') return crypto.randomUUID();
  const values = crypto.getRandomValues(new Uint8Array(16));
  return Array.from(values, v => v.toString(16).padStart(2, '0')).join('');
}

export default function Home() {
  const [dashboard, setDashboard] = useState<Dashboard | null>(null);
  const [session, setSession] = useState<Session | null>(null);
  const [paused, setPaused] = useState(false);
  const [answer, setAnswer] = useState('');
  const [busy, setBusy] = useState(false);
  const [ready, setReady] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [hint, setHint] = useState(false);
  const [resolution, setResolution] = useState<Result | ''>('');
  const [mode, setMode] = useState<'scheduled' | 'practice'>('scheduled');
  const [types, setTypes] = useState<Kind[]>(['spelling']);
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
  const completed = session?.questions.filter(q => q.rating !== null).length || 0;
  const complete = !!session && !current;
  const reviewing = !!current && current.result !== null;

  function track(value: Pending | null) { pendingRef.current = value; setPending(value); remember(value); }
  async function load(id: number) {
    const loaded = await api<Session>(`/study/sessions/${id}`);
    setSession(loaded); return loaded;
  }
  async function synchronize() {
    if (lock.current) return;
    lock.current = true; setBusy(true); setError('');
    try {
      const [data, active] = await Promise.all([api<Dashboard>('/dashboard'), api<{ session: Session | null }>('/study/sessions/active')]);
      setDashboard(data);
      const saved = pendingRef.current || parsePending(stored(storageKey));
      let loaded = active.session;
      if (saved && (!loaded || loaded.id !== saved.sessionId)) {
        try { loaded = await api<Session>(`/study/sessions/${saved.sessionId}`); }
        catch (e) { if (!(e instanceof APIError && e.status === 404)) throw e; }
      } else if (!loaded && session) loaded = await api<Session>(`/study/sessions/${session.id}`);
      setSession(loaded);
      if (saved) track(needsRetry(saved, loaded) ? saved : null);
      setReady(true);
    } catch (e) { setError((e as Error).message || '暂时无法连接，请检查网络后重试。'); }
    finally { lock.current = false; setBusy(false); }
  }

  useEffect(() => {
    track(parsePending(stored(storageKey)));
    synchronize();
    const update = () => setOnline(navigator.onLine);
    update(); window.addEventListener('online', update); window.addEventListener('offline', update);
    return () => { window.removeEventListener('online', update); window.removeEventListener('offline', update); };
    // Initial server restore is authoritative, including the feedback stage.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  useEffect(() => {
    setAnswer(''); setHint(false); setResolution(''); started.current = Date.now();
    if (current && !reviewing && !paused) input.current?.focus();
    if (reviewing && !paused) ratingFocus.current?.focus();
  }, [current?.id, reviewing, paused]);

  async function start() {
    if (lock.current || !types.length || !ready || pendingRef.current) return;
    lock.current = true; setBusy(true); setError(''); setNotice('');
    try {
      const created = await api<{ id: number | null; message?: string }>('/study/sessions', { mode, types, limit, new_limit: newLimit });
      if (created.id) { await load(created.id); setPaused(false); }
      else { setSession(null); setNotice(created.message || '当前没有符合条件的题目。'); }
    } catch (e) { setError((e as Error).message); }
    finally { lock.current = false; setBusy(false); }
  }

  async function perform(request: Pending) {
    if (lock.current) return;
    lock.current = true; setBusy(true); setError(''); track(request);
    try {
      await api(`/study/sessions/${request.sessionId}/${request.action}`, request.body);
      await load(request.sessionId);
      setReady(true);
      track(null);
      api<Dashboard>('/dashboard').then(setDashboard).catch(() => {});
    } catch (e) { setError((e as Error).message || '提交尚未确认。恢复网络后可重试，进度不会自动跳过。'); }
    finally { lock.current = false; setBusy(false); }
  }
  function submit(value: string) {
    if (!session || !current || reviewing || pendingRef.current || !value.trim()) return;
    perform({ action: 'answers', sessionId: session.id, questionId: current.id, body: {
      question_id: current.id, request_id: requestID(), answer: value,
      response_time_ms: Math.max(0, Math.min(Date.now() - started.current, 3600000)), used_hint: hint,
    } });
  }
  function rate(rating: number) {
    if (!session || !current || pendingRef.current || !canRate(current, rating, resolution)) return;
    perform({ action: 'review', sessionId: session.id, questionId: current.id, body: {
      question_id: current.id, request_id: requestID(), rating,
      ...(current.result === 'pending' ? { resolved_result: resolution } : {}),
    } });
  }
  function undo() {
    if (!session || pendingRef.current || (!reviewing && !completed)) return;
    const target = reviewing ? current : [...session.questions].reverse().find(q => q.rating !== null);
    if (!target) return;
    perform({ action: 'undo', sessionId: session.id, questionId: target.id, body: { request_id: requestID() } });
  }
  function speak() {
    if (!current || !('speechSynthesis' in window)) return;
    const text = current.type === 'spelling' ? current.expected : current.prompt;
    if (!text) return;
    speechSynthesis.cancel(); const utterance = new SpeechSynthesisUtterance(text); utterance.lang = 'en-GB'; utterance.rate = 0.85; speechSynthesis.speak(utterance);
  }
  useEffect(() => {
    const keys = (event: KeyboardEvent) => {
      if (event.repeat || event.isComposing || event.ctrlKey || event.metaKey || event.altKey || busy || paused || pending || !current) return;
      if ((event.target as HTMLElement)?.closest('input, textarea, select, button')) return;
      if (reviewing && /^[1-4]$/.test(event.key) && canRate(current, Number(event.key), resolution)) { event.preventDefault(); rate(Number(event.key)); }
      else if (!reviewing && current.choices && /^[1-4]$/.test(event.key)) { const choice = current.choices[Number(event.key) - 1]; if (choice) { event.preventDefault(); submit(choice); } }
    };
    window.addEventListener('keydown', keys); return () => window.removeEventListener('keydown', keys);
  });

  return <main className="shell">
    <header className="topbar glass"><div className="brand"><span className="mark">E</span><span>ELS<span className="brand-detail">英语学习</span></span></div><div className="topbar-right"><span className="mode-label">FSRS 复习</span><span className="edition">示例版</span></div></header>
    <section className="intro"><span className="eyebrow"><span className="eyebrow-dot" /> RECALL · REVIEW · REMEMBER</span><h1>记住一个，再走一步<span className="title-dot">.</span></h1><p>先回忆，再核对。认识、释义和拼写，分别安排复习。</p></section>
    {dashboard && <div className="stats">
      <div className="stat glass"><span className="stat-label">到期能力项</span><strong>{dashboard.due}<small>项</small></strong><span className="stat-note">同词的不同题型分日练习</span></div>
      <div className="stat glass"><span className="stat-label">今日已完成</span><strong>{dashboard.today.reviewed}<small>题</small></strong><span className="stat-note">独立记住 {dashboard.today.unassisted_correct} · 重学 {dashboard.today.again}</span></div>
      <div className="stat glass"><span className="stat-label">已练习单词</span><strong>{dashboard.studied}<small>个</small></strong><span className="stat-note">词库共 {dashboard.demo_words} 个原创示例词</span></div>
    </div>}
    {!online && <p className="connection" role="status">网络已断开。当前题目仍保留，联网后可确认提交。</p>}
    <section className="study" aria-label="词汇练习">
      {!ready && <div className="empty"><h2>恢复学习进度…</h2><p>正在读取服务器上的练习记录。</p></div>}
      {ready && (!session || paused || complete) && <div className="empty">
        <div className="step"><span className="step-index">{complete ? '✓' : '01'}</span>{complete ? '本轮完成' : paused ? '进度已保留' : '开始一轮'}</div>
        <h2>{complete ? '把这一轮留在记忆里' : paused ? '准备好，再继续' : '今天想怎么练？'}</h2>
        {complete && <><p>完成 {completed} 道题。{session.mode === 'practice' ? '自由练习已记录，不改变正式复习时间。' : '下一次复习已按记忆评价安排。'}</p><div className="summary"><span>独立记住 {session.questions.filter(q => q.result === 'correct' && !q.used_hint && (q.rating || 0) > 1).length}</span><span>重学 {session.questions.filter(q => q.rating === 1).length}</span></div></>}
        {paused && current && session && <p>当前练习还有 {session.questions.length - completed} 道题。题型和顺序沿用本轮设置。</p>}
        {(!paused || !current) && <div className="settings">
          <fieldset><legend>练习方式</legend><div className="segments"><button aria-pressed={mode === 'scheduled'} onClick={() => setMode('scheduled')}>按计划学习</button><button aria-pressed={mode === 'practice'} onClick={() => setMode('practice')}>自由练习</button></div><p className="setting-note">{mode === 'scheduled' ? '到期优先，新题随机；同一词义每天只练一种题型。' : '随机抽题，可提前练习；不改变 FSRS 复习状态。'}</p></fieldset>
          <fieldset><legend>题型（可多选）</legend><div className="type-options">{(Object.keys(labels) as Kind[]).map(kind => <label key={kind}><input type="checkbox" checked={types.includes(kind)} onChange={() => setTypes(types.includes(kind) ? types.filter(t => t !== kind) : [...types, kind])} />{labels[kind]}</label>)}</div><p className="setting-note">多选时每个词义只抽一种题型，避免答案互相提示。</p></fieldset>
          <div className="limit-options"><label>本轮最多题数<select value={limit} onChange={e => setLimit(Number(e.target.value))}>{[5, 10, 20, 50].map(n => <option key={n} value={n}>{n} 题</option>)}</select></label>{mode === 'scheduled' && <label>本轮新能力项上限<select value={newLimit} onChange={e => setNewLimit(Number(e.target.value))}>{[0, 5, 10, 20].map(n => <option key={n} value={n}>{n === 0 ? '只复习' : `${n} 项`}</option>)}</select></label>}</div>
        </div>}
        <div className="actions"><button className="primary" onClick={paused && current ? () => setPaused(false) : start} disabled={busy || !types.length || !!pending}>{busy ? '正在准备…' : paused && current ? '继续当前练习' : complete ? '开始下一轮' : '开始学习'}<span aria-hidden="true">→</span></button>{complete && <button className="quiet" onClick={undo} disabled={busy || !!pending}>撤销上一题</button>}</div>
        <p className="setting-note">当前仍使用原创示例词，尚未接入北师大版教材。</p>
      </div>}
      {ready && session && current && !paused && <div className="question" key={current.id}>
        <div className="question-top"><span className="step">{labels[current.type]}</span><span>{completed + 1} / {session.questions.length}</span></div>
        <div className="meter" role="progressbar" aria-label="已完成题数" aria-valuemin={0} aria-valuemax={session.questions.length} aria-valuenow={completed}><span style={{ width: `${completed / session.questions.length * 100}%` }} /></div>
        <p className="instruction">{reviewing ? '先核对答案，再告诉自己：这一次真的记住了吗？' : current.type === 'spelling' ? '根据中文写出英文单词' : current.type === 'recognition' ? '选择最合适的中文释义' : '主动写出中文意思'}</p>
        <h2 className={current.type === 'spelling' ? 'prompt chinese' : 'prompt'}>{current.prompt}</h2>
        {!reviewing && (current.choices ? <div className="choices">{current.choices.map((choice, i) => <button key={choice} disabled={busy || !!pending} onClick={() => submit(choice)}><span>{i + 1}</span>{choice}</button>)}</div> : <form onSubmit={e => { e.preventDefault(); submit(answer); }}><label htmlFor="answer">你的答案</label><input ref={input} id="answer" autoComplete="off" autoCapitalize="off" spellCheck={false} disabled={busy || !!pending} value={answer} onChange={e => setAnswer(e.target.value)} placeholder={current.type === 'spelling' ? '输入英文单词' : '输入中文释义'} /><div className="actions"><button type="button" className="quiet" onClick={() => setHint(true)} disabled={hint || busy || !!pending}>{hint ? '已使用提示' : '使用提示'}</button><button className="primary" disabled={busy || !!pending || !answer.trim()}>{busy ? '提交中…' : '核对答案'}</button></div></form>)}
        {!reviewing && hint && <p className="hint">提示：{current.hint_text}。这次将按「重学」记录。</p>}
        {reviewing && <div className="review-panel">
          <h3 ref={ratingFocus} tabIndex={-1} className={`result-title ${current.result}`}>{current.result === 'correct' ? '答案正确' : current.result === 'wrong' ? '再记一次，会更牢' : '这条释义需要你核对'}</h3>
          <dl className="answer-comparison"><div><dt>你的答案</dt><dd>{current.answer || '未填写'}</dd></div><div><dt>参考答案</dt><dd>{current.expected}</dd></div></dl>
          {current.example && <p className="example" lang="en">{current.example}</p>}
          <button className="quiet pronunciation" onClick={speak}>听单词发音</button>
          {current.result === 'pending' && <fieldset className="resolution"><legend>未命中词库别名，请自行核对；本次标记为自评</legend><div className="segments"><button aria-pressed={resolution === 'correct'} onClick={() => setResolution('correct')} disabled={busy || !!pending}>我的释义正确</button><button aria-pressed={resolution === 'wrong'} onClick={() => setResolution('wrong')} disabled={busy || !!pending}>没有记住 / 释义不对</button></div></fieldset>}
          {(current.used_hint || current.result === 'wrong' || resolution === 'wrong') && <p className="hint">{current.used_hint ? '这次借助了提示' : '这次没有独立回忆出来'}，请选择「重学」。</p>}
          <p className="rating-instruction">{session.mode === 'practice' ? '本次只记录评价，不改变正式复习时间。' : '按真实记忆选择；忘记了就选重学。时间是当前预估。'}</p>
          <div className="rating-buttons">{ratings.map((label, i) => <button key={label} className={`rating rating-${i + 1}`} disabled={busy || !!pending || !canRate(current, i + 1, resolution)} onClick={() => rate(i + 1)}><strong>{label}</strong><small>{session.mode === 'scheduled' ? intervalLabel(current.intervals?.[String(i + 1)]) : '仅练习'}</small><kbd>{i + 1}</kbd></button>)}</div>
        </div>}
        <div className="study-toolbar"><button className="quiet" onClick={() => setPaused(true)} disabled={busy}>返回概览</button><button className="quiet" onClick={undo} disabled={busy || !!pending || (!reviewing && !completed)}>{reviewing ? '重新作答' : '撤销上一题'}</button></div>
      </div>}
      {pending && <div className="retry-panel" role="status"><p>有一项提交尚未确认。重试会沿用原请求，避免重复记录。</p><button className="primary" disabled={busy} onClick={() => perform(pending)}>{busy ? '确认中…' : pending.action === 'answers' ? '重试答案提交' : pending.action === 'review' ? '重试记忆评价' : '重试撤销'}</button></div>}
      {notice && <p className="notice" role="status">{notice}</p>}
      {error && <div className="error" role="alert"><p>{error}</p><button className="quiet" onClick={synchronize} disabled={busy}>同步服务器进度</button></div>}
    </section>
    {dashboard && dashboard.dimensions.length > 0 && <section className="dimension-panel glass" aria-label="分能力学习记录"><h2>认识，不等于会拼写</h2><div className="dimension-list">{dashboard.dimensions.map(d => <div key={d.type}><strong>{labels[d.type]}</strong><span>到期 {d.due} · 练过 {d.cards} 项</span></div>)}</div></section>}
    <footer>FSRS-6 · 独立能力记录 · 本机开发示例<br />发音由设备语音引擎提供；这是词汇训练，尚不包含句子分析和听力专项。</footer>
  </main>;
}
