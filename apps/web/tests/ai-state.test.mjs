import test from "node:test";
import assert from "node:assert/strict";
import { parsePendingAI } from "../lib/ai-state.ts";
test("AI retries restore only supported learning requests", () => {
  const p = {
    path: "/ai/conversations/conversation_0001/messages",
    body: {
      request_id: "retry-chat-0001",
      model_id: 1,
      content: "Explain this sentence",
    },
  };
  assert.deepEqual(parsePendingAI(JSON.stringify(p)), p);
  for (const value of [
    null,
    "{",
    "null",
    JSON.stringify({ ...p, path: "/admin/users" }),
    JSON.stringify({ ...p, body: { ...p.body, request_id: "bad" } }),
    JSON.stringify({
      path: "/ai/training/session_0001/answers",
      body: { request_id: "answer-0001", answers: [4] },
    }),
  ])
    assert.equal(parsePendingAI(value), null);
  const listening = {
    path: "/ai/training/session_0001/answers",
    body: { request_id: "answer-0001", answers: [0, 1, 2] },
  };
  assert.deepEqual(parsePendingAI(JSON.stringify(listening)), listening);
});
