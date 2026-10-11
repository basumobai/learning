import test from 'node:test';
import assert from 'node:assert/strict';
import { canRate, currentQuestion, intervalLabel, needsRetry, parsePending } from '../lib/study.ts';

const question = { id: 11, type: 'spelling', prompt: '必要的', result: 'correct', rating: null, used_hint: false };
const session = { id: 1, mode: 'scheduled', questions: [question, { ...question, id: 12, result: null }] };
test('a submitted answer stays current until memory is rated', () => {
  assert.equal(currentQuestion(session).id, 11);
  assert.equal(currentQuestion({ ...session, questions: [{ ...question, rating: 3 }, session.questions[1]] }).id, 12);
});
test('wrong and hinted answers require Again; pending requires self-check', () => {
  assert.equal(canRate(question, 3, ''), true);
  assert.equal(canRate({ ...question, used_hint: true }, 3, ''), false);
  assert.equal(canRate({ ...question, result: 'wrong' }, 2, ''), false);
  assert.equal(canRate({ ...question, result: 'wrong' }, 1, ''), true);
  assert.equal(canRate({ ...question, result: 'pending' }, 3, ''), false);
  assert.equal(canRate({ ...question, result: 'pending' }, 3, 'correct'), true);
  assert.equal(canRate({ ...question, result: 'pending' }, 3, 'wrong'), false);
});
test('lost responses reconcile with server progress without repeating committed work', () => {
  const request = { sessionId: 1, questionId: 11, action: 'answers', body: { request_id: 'retry-0001' } };
  assert.equal(needsRetry(request, session), false);
  assert.equal(needsRetry({ ...request, action: 'review' }, session), true);
  assert.equal(needsRetry({ ...request, action: 'review' }, { ...session, questions: [{ ...question, rating: 3 }] }), false);
  assert.equal(needsRetry({ ...request, action: 'undo' }, session), true);
  assert.equal(needsRetry({ ...request, action: 'undo' }, { ...session, questions: [{ ...question, result: null }] }), false);
});
test('malformed browser storage is ignored', () => {
  for (const raw of [null, '{', 'null', '{}', JSON.stringify({ action: 'delete', sessionId: 1, questionId: 1 })]) assert.equal(parsePending(raw), null);
  const request = { sessionId: 1, questionId: 11, action: 'review', body: { question_id: 11, request_id: 'retry-0001', rating: 3 } };
  assert.deepEqual(parsePending(JSON.stringify(request)), request);
});
test('interval labels reflect minutes and days', () => {
  const now = Date.parse('2026-10-11T00:00:00Z');
  assert.equal(intervalLabel('2026-10-11T00:10:00Z', now), '约 10 分钟');
  assert.equal(intervalLabel('2026-10-14T00:00:00Z', now), '约 3 天');
  assert.equal(intervalLabel('invalid', now), '');
});
