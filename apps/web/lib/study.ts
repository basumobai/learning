export type Result = 'correct' | 'wrong' | 'pending';
export type Kind = 'recognition' | 'meaning_recall' | 'spelling';
export type Question = {
  id: number; type: Kind; prompt: string; hint_text: string; choices?: string[];
  position: number; result: Result | null; rating: number | null; answer?: string;
  expected?: string; example?: string; used_hint: boolean;
  intervals?: Record<string, string>; next_review_at?: string;
};
export type Session = { id: number; mode: 'scheduled' | 'practice'; questions: Question[] };
export type Pending = {
  action: 'answers' | 'review' | 'undo'; sessionId: number; questionId: number;
  body: Record<string, string | number | boolean>;
};

export function currentQuestion(session: Session | null): Question | undefined {
  return session?.questions.find(q => q.rating === null);
}

export function canRate(question: Question, rating: number, resolution: Result | ''): boolean {
  const result = question.result === 'pending' ? resolution : question.result;
  return (result === 'correct' || result === 'wrong') && rating >= 1 && rating <= 4
    && (!(result === 'wrong' || question.used_hint) || rating === 1);
}

export function intervalLabel(date?: string, now = Date.now()): string {
  if (!date) return '';
  const ms = Date.parse(date) - now;
  if (!Number.isFinite(ms)) return '';
  if (ms < 60_000) return '不到 1 分钟';
  if (ms < 3_600_000) return `约 ${Math.round(ms / 60_000)} 分钟`;
  if (ms < 86_400_000) return `约 ${Math.round(ms / 3_600_000)} 小时`;
  return `约 ${Math.round(ms / 86_400_000)} 天`;
}

export function parsePending(raw: string | null): Pending | null {
  if (!raw) return null;
  try {
    const value = JSON.parse(raw);
    if (!value || !['answers', 'review', 'undo'].includes(value.action)
      || !Number.isSafeInteger(value.sessionId) || value.sessionId < 1
      || !Number.isSafeInteger(value.questionId) || value.questionId < 1
      || !value.body || typeof value.body.request_id !== 'string'
      || !/^[A-Za-z0-9_-]{8,128}$/.test(value.body.request_id)) return null;
    if (value.action !== 'undo' && value.body.question_id !== value.questionId) return null;
    if (value.action === 'answers' && (typeof value.body.answer !== 'string'
      || !Number.isInteger(value.body.response_time_ms) || value.body.response_time_ms < 0
      || value.body.response_time_ms > 3600000 || typeof value.body.used_hint !== 'boolean')) return null;
    if (value.action === 'review' && (!Number.isInteger(value.body.rating) || value.body.rating < 1 || value.body.rating > 4)) return null;
    return value as Pending;
  } catch { return null; }
}

// A lost response may already be committed. Reconcile against server state
// without discarding an unacknowledged answer when the device is offline.
export function needsRetry(pending: Pending, session: Session | null): boolean {
  if (!session || session.id !== pending.sessionId) return pending.action === 'undo';
  const question = session.questions.find(q => q.id === pending.questionId);
  if (!question) return false;
  if (pending.action === 'answers') return question.result === null;
  if (pending.action === 'review') return question.result !== null && question.rating === null;
  return question.result !== null;
}
