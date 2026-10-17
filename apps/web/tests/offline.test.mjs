import assert from "node:assert/strict";
import test from "node:test";
import fs from "node:fs";
import vm from "node:vm";
import {
  parseDraft,
  writeDraft,
  readDraft,
  sameAnswers,
} from "../lib/offline.ts";
const draft = {
  user_id: "student-a",
  attempt_id: "attempt-a",
  base_version: 0,
  version: 2,
  answers: { 1: "B" },
  pending_submission: {
    client_request_id: "submit-key-1234",
    answers: { 1: "B" },
    personal: false,
    model_selection: { model_id: 1, reviewer_ids: [], lead_model_id: 0 },
  },
};
test("offline drafts and pending payloads are scoped to exact user and attempt", () => {
  const data = new Map();
  const storage = {
    getItem: (k) => data.get(k) || null,
    setItem: (k, v) => data.set(k, v),
  };
  assert.equal(writeDraft(draft, storage), true);
  assert.deepEqual(readDraft("student-a", "attempt-a", storage), draft);
  assert.equal(readDraft("student-b", "attempt-a", storage), null);
  assert.equal(
    parseDraft(JSON.stringify(draft), "student-a", "other-attempt"),
    null,
  );
});
test("draft cache refuses protected answers, credentials and malformed versions", () => {
  for (const bad of [
    { ...draft, solution: { answers: { 1: "A" } } },
    { ...draft, version: NaN },
    { ...draft, answers: { 1: 123 } },
    {
      ...draft,
      pending_submission: { ...draft.pending_submission, api_key: "secret" },
    },
    {
      ...draft,
      pending_submission: {
        ...draft.pending_submission,
        model_selection: {
          ...draft.pending_submission.model_selection,
          api_key: "secret",
        },
      },
    },
  ])
    assert.equal(
      parseDraft(JSON.stringify(bad), "student-a", "attempt-a"),
      null,
    );
});
test("answer equality ignores key order but never ignores changed values", () => {
  assert.equal(sameAnswers({ a: "1", b: "2" }, { b: "2", a: "1" }), true);
  assert.equal(sameAnswers({ a: "1" }, { a: "2" }), false);
});
test("service worker never intercepts API, audio or POST data and caches only public shell files", async () => {
  const handlers = {};
  let cached;
  const context = {
    self: {
      location: { origin: "https://learning.test" },
      addEventListener: (name, fn) => (handlers[name] = fn),
      skipWaiting: () => {},
      clients: { claim: async () => {} },
    },
    URL,
    Promise,
    caches: {
      open: async () => ({ addAll: async (files) => (cached = files) }),
      keys: async () => [],
      match: async () => null,
    },
    fetch: async () => ({ ok: true }),
  };
  vm.runInNewContext(
    fs.readFileSync(new URL("../public/sw.js", import.meta.url), "utf8"),
    context,
  );
  await new Promise((resolve, reject) =>
    handlers.install({ waitUntil: (p) => p.then(resolve, reject) }),
  );
  assert.deepEqual(Array.from(cached), ["/offline.html", "/icon.svg"]);
  for (const path of [
    "/api/v1/account/export",
    "/api/v1/learning/attempts/x",
    "/api/v1/learning/recordings/x/audio",
  ]) {
    let intercepted = false;
    handlers.fetch({
      request: {
        method: "GET",
        url: "https://learning.test" + path,
        mode: "cors",
      },
      respondWith: () => (intercepted = true),
    });
    assert.equal(intercepted, false);
  }
  let post = false;
  handlers.fetch({
    request: {
      method: "POST",
      url: "https://learning.test/api/v1/learning/attempts/x/submit",
      mode: "cors",
    },
    respondWith: () => (post = true),
  });
  assert.equal(post, false);
});
