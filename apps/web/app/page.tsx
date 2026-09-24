'use client';

import { useEffect, useRef, useState } from 'react';

type Question = { id: number; type: 'recognition' | 'meaning_recall' | 'spelling'; prompt: string; hint_text: string; choices?: string[]; position: number; result: 'correct' | 'wrong' | 'pending' | null };
type Session = { id: number; questions: Question[] };
type Dashboard = { display_name: string; due: number; studied: number; demo_words: number };
type Grade = { result: 'correct' | 'wrong' | 'pending'; expected?: string; used_hint?: boolean };

async function api<T>(path: string, body?: object): Promise<T> {
  const response = await fetch(`/api/v1${path}`, body ? { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) } : { cache: 'no-store' });
  const payload = await response.json();
  if (!response.ok) throw new Error(payload.error || '请求失败，请稍后重试');
  return payload as T;
}

const labels = { recognition: '英译中 · 选择', meaning_recall: '英译中 · 默写释义', spelling: '中译英 · 拼写' };

export default function Home() {
  const [dashboard, setDashboard] = useState<Dashboard | null>(null);
  const [session, setSession] = useState<Session | null>(null);
  const [answer, setAnswer] = useState('');
  const [message, setMessage] = useState<Grade | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [hint, setHint] = useState(false);
  const started = useRef(Date.now());
  const pendingRequest = useRef<{ questionId: number; requestId: string; answer: string; ms: number; hint: boolean } | null>(null);

  useEffect(() => {
    api<Dashboard>('/dashboard').then(setDashboard).catch(e => setError(e.message));
    const saved = window.localStorage.getItem('els-session');
    if (saved && /^\d+$/.test(saved)) api<Session>(`/study/sessions/${saved}`).then(setSession).catch(() => window.localStorage.removeItem('els-session'));
  }, []);

  const current = session?.questions.find(q => q.result === null);
  const completed = session?.questions.filter(q => q.result !== null).length || 0;

  async function start() {
    setBusy(true); setError(''); setMessage(null); setHint(false);
    try {
      const created = await api<{ id: number }>('/study/sessions', {});
      const loaded = await api<Session>(`/study/sessions/${created.id}`);
      window.localStorage.setItem('els-session', String(created.id));
      setSession(loaded); setAnswer(''); started.current = Date.now();
    } catch (e) { setError((e as Error).message); }
    finally { setBusy(false); }
  }

  async function submit(value: string) {
    if (!session || !current || busy || !value.trim()) return;
    const next = pendingRequest.current?.questionId === current.id && pendingRequest.current.answer === value
      ? pendingRequest.current
      : { questionId: current.id, requestId: crypto.randomUUID(), answer: value, ms: Math.min(Date.now() - started.current, 3600000), hint };
    pendingRequest.current = next;
    setBusy(true); setError('');
    try {
      const grade = await api<Grade>(`/study/sessions/${session.id}/answers`, {
        question_id: current.id, request_id: next.requestId, answer: next.answer,
        response_time_ms: next.ms, used_hint: next.hint,
      });
      pendingRequest.current = null;
      setSession({ ...session, questions: session.questions.map(q => q.id === current.id ? { ...q, result: grade.result } : q) });
      setMessage(grade); setAnswer(''); setHint(false); started.current = Date.now();
      api<Dashboard>('/dashboard').then(setDashboard).catch(() => {});
    } catch (e) { setError((e as Error).message); }
    finally { setBusy(false); }
  }

  return <main className="shell">
    <header className="topbar glass"><div className="brand"><span className="mark">E</span><span>ELS<span className="brand-detail">英语学习</span></span></div><div className="topbar-right"><span className="mode-label">词汇训练</span><span className="edition">体验版</span></div></header>
    <section className="intro"><span className="eyebrow"><span className="eyebrow-dot" /> YOUR DAILY PRACTICE</span><h1>今天，学得更牢一点<span className="title-dot">.</span></h1><p>从认识，到回忆，再到拼写。每一步都单独记录。</p></section>
    {dashboard && <div className="stats"><div className="stat glass"><span className="stat-label">待复习能力项</span><strong>{dashboard.due}<small>项</small></strong><span className="stat-note">值得再看一眼</span></div><div className="stat glass"><span className="stat-label">已练习单词</span><strong>{dashboard.studied}<small>个</small></strong><span className="stat-note">一点点积累起来</span></div><div className="stat glass"><span className="stat-label">示例单词</span><strong>{dashboard.demo_words}<small>个</small></strong><span className="stat-note">当前体验词库</span></div></div>}
    <section className="study" aria-live="polite">
      {!session && <div className="empty"><div className="step"><span className="step-index">01</span> 今日练习</div><h2>先来一轮词汇练习</h2><p>15 道示例题，依次练习选择、释义和拼写。刷新页面也能继续当前进度。</p><button className="primary" onClick={start} disabled={busy}>{busy ? '正在准备…' : '开始学习'}<span aria-hidden="true">→</span></button></div>}
      {session && current && <div className="question" key={current.id}>
        <div className="question-top"><span className="step">{labels[current.type]}</span><span>{completed + 1} / {session.questions.length}</span></div>
        <div className="meter"><span style={{ width: `${(completed / session.questions.length) * 100}%` }} /></div>
        <p className="instruction">{current.type === 'spelling' ? '根据中文写出英文单词' : current.type === 'recognition' ? '选择最合适的中文释义' : '写出你理解的中文意思'}</p>
        <h2 className={current.type === 'spelling' ? 'prompt chinese' : 'prompt'}>{current.prompt}</h2>
        {current.choices ? <div className="choices">{current.choices.map((choice, i) => <button key={choice} disabled={busy} onClick={() => submit(choice)}><span>{String.fromCharCode(65 + i)}</span>{choice}</button>)}</div>
          : <form onSubmit={e => { e.preventDefault(); submit(answer); }}><label htmlFor="answer">你的答案</label><input id="answer" autoComplete="off" autoCapitalize="off" spellCheck={false} value={answer} onChange={e => setAnswer(e.target.value)} placeholder={current.type === 'spelling' ? '输入英文单词' : '输入中文释义'} /><div className="actions"><button type="button" className="quiet" onClick={() => setHint(true)} disabled={hint || busy}>{hint ? '已使用提示' : '使用提示'}</button><button className="primary" disabled={busy || !answer.trim()}>{busy ? '提交中…' : '提交答案'}</button></div></form>}
        {hint && <p className="hint">提示：{current.hint_text}。使用提示的答题会降低复习间隔。</p>}
      </div>}
      {session && !current && <div className="empty"><div className="step"><span className="step-index">✓</span> 本轮完成</div><h2>这一轮完成了</h2><p>已提交 {completed} 道题。待判答案暂不计入复习状态。</p><div className="summary"><span>答对 {session.questions.filter(q => q.result === 'correct').length}</span><span>需加强 {session.questions.filter(q => q.result === 'wrong').length}</span><span>待判 {session.questions.filter(q => q.result === 'pending').length}</span></div><button className="primary" onClick={start} disabled={busy}>再练一轮<span aria-hidden="true">→</span></button></div>}
      {message && <div className={`feedback ${message.result}`}>{message.result === 'correct' ? '回答正确' : message.result === 'wrong' ? '这题需要再练习' : '释义待判定，本题暂不影响复习'}{message.expected && <span>参考答案：{message.expected}</span>}</div>}
      {error && <p className="error" role="alert">{error}</p>}
    </section>
    <footer>学习记录按识别、释义回忆和拼写分别保存 · 本机开发体验</footer>
  </main>;
}
