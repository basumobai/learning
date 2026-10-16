import assert from "node:assert/strict";
import { chromium } from "playwright";
import fs from "node:fs/promises";
const base = process.env.UI_BASE || "http://127.0.0.1:3007";
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || undefined,
  args: ["--no-sandbox"],
});
const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
  }),
  page = await context.newPage();
const failures = [];
page.on("pageerror", (e) => failures.push(e.message));
let submitted = false,
  draft = {},
  difficulty = "normal",
  round = 0;
const explanationRequests = [];
let selectedModels = { model_id: 1, reviewer_ids: [1, 2], lead_model_id: 3 };
let savedAdminGroup = null;
const question = {
  id: "q1",
  subject_id: "english",
  kind: "reading",
  content: {
    text: "Choose the subject of the main clause.",
    passage: "The book that you lent me helped me.",
    parts: [
      {
        id: "1",
        text: "What helped me?",
        points: 2,
        choices: [
          { id: "A", text: "The book" },
          { id: "B", text: "you" },
        ],
      },
    ],
  },
  assets: [],
  solution: {
    answers: { 1: "A" },
    explanation: "The book是主句主语；that引导定语从句。",
    rubric: "找主干。",
  },
};
const timeline = {
  version: 1,
  text: question.content.passage,
  spans: [
    { id: "s1", start: 0, end: 8, role: "主语", tone: "yellow" },
    { id: "s2", start: 9, end: 25, role: "定语从句", tone: "purple" },
  ],
  steps: [
    {
      id: "one",
      title: "先找主语",
      explanation: "这本书帮助了我，The book是主语。",
      span_ids: ["s1"],
      asset_ids: [],
    },
    {
      id: "two",
      title: "再看从句",
      explanation: "that you lent me修饰book。",
      span_ids: ["s2"],
      asset_ids: [],
    },
  ],
  summary: "先主干，再修饰。",
};
function attempt() {
  return {
    id: "attempt1",
    question,
    server_time: new Date().toISOString(),
    deadline: null,
    submitted_at: submitted ? new Date().toISOString() : null,
    answers: submitted ? draft : null,
    draft,
    feedback: submitted
      ? { score: draft["1"] === "A" ? 2 : 0, max_score: 2, needs_ai: false }
      : null,
    explanation_needed: submitted && draft["1"] !== "A",
    jobs: [],
  };
}
await context.route("**/api/v1/**", async (route) => {
  const p = new URL(route.request().url()).pathname.replace("/api/v1", "");
  const body =
    route.request().method() === "POST" ? route.request().postDataJSON() : {};
  let data = {};
  if (p === "/auth/me")
    data = {
      user: {
        id: "test",
        display_name: "学习者",
        username: "tester",
        role: "admin",
        must_change_password: false,
      },
      csrf_token: "test",
    };
  else if (p === "/learning/model-options")
    data = {
      group: { id: 1, name: "进阶学习组" },
      models: [
        { id: 1, name: "讲解模型", provider: "学校", model_id: "one" },
        { id: 2, name: "推理模型", provider: "学校", model_id: "two" },
        { id: 3, name: "主审模型", provider: "学校", model_id: "three" },
      ],
      selection: selectedModels,
      personal_enabled: false,
    };
  else if (p === "/learning/model-preferences") {
    selectedModels = body;
    data = { saved: true };
  } else if (p === "/learning/catalog")
    data = {
      region: "广东",
      subjects: [
        { id: "english", types: [{ id: "reading", name: "阅读理解" }] },
        { id: "chinese", types: [{ id: "essay", name: "作文" }] },
      ],
      chapters: [
        {
          id: "unit1",
          subject_id: "english",
          title: "Unit 1 Relationships",
          textbook: "北师大版2019 选择性必修一",
          term: "高二上",
          question_count: 12,
        },
      ],
    };
  else if (p === "/learning/attempts") {
    submitted = false;
    draft = {};
    data = attempt();
  } else if (p.endsWith("/draft")) {
    draft = body.answers;
    data = { saved: true };
  } else if (p.endsWith("/submit")) {
    submitted = true;
    draft = body.answers;
    data = attempt();
  } else if (p.endsWith("/explanation")) {
    assert.equal(submitted, true, "AI requested before submitting");
    explanationRequests.push(body);
    data = { id: "job1" };
  } else if (p === "/learning/jobs/job1")
    data = {
      id: "job1",
      kind: "explanation",
      status: "completed",
      result: submitted ? timeline : null,
      error: "",
    };
  else if (p.endsWith("/chat"))
    data = { answer: "先看helped的主语，再暂时遮住that从句。" };
  else if (p === "/learning/attempts/attempt1") data = attempt();
  else if (p === "/learning/words/next") {
    round++;
    difficulty = body.difficulty;
    data = {
      id: "r" + round,
      prompt: "adapt",
      direction: body.direction,
      difficulty,
      choices: ["适应", "调整", "改编", "适合"].map((text, i) => ({
        id: "ABCD"[i],
        text,
        ...(difficulty === "despair" ? { pos: "v." } : {}),
      })),
      server_time: new Date().toISOString(),
      deadline: new Date(
        Date.now() + (difficulty === "normal" ? 6000 : 3000),
      ).toISOString(),
      answered_at: null,
    };
  } else if (p.startsWith("/learning/words/") && p.endsWith("/answer"))
    data = {
      id: "r" + round,
      prompt: "adapt",
      direction: "en_zh",
      difficulty,
      choices: ["适应", "调整", "改编", "适合"].map((text, i) => ({
        id: "ABCD"[i],
        text,
      })),
      server_time: new Date().toISOString(),
      deadline: new Date().toISOString(),
      answered_at: new Date().toISOString(),
      correct: body.picked === "A",
      correct_id: "A",
      picked: body.picked,
    };
  else if (p === "/ai/models")
    data = { models: [{ id: 1, name: "讲解模型", kind: "chat", voices: [] }] };
  else if (p === "/preferences")
    data = { tts_model_id: 0, voice: "", timezone: "Asia/Shanghai" };
  else if (p === "/ai/conversations") data = { conversations: [] };
  else if (p === "/ai/training")
    data =
      route.request().method() === "POST"
        ? {
            id: "sentence1",
            kind: "sentence",
            data: {
              text: question.content.passage,
              analysis: {
                translation: "你借我的书帮助了我。",
                main_clause: "The book helped me.",
                chunks: [
                  { text: "The book", role: "主语", explanation: "书是主语" },
                ],
                grammar: ["that引导定语从句"],
                paraphrase: "This book helped.",
                check: {
                  question: "主语是什么？",
                  answer: "The book",
                  explanation: "书帮助了我。",
                },
              },
            },
          }
        : { training: [] };
  else if (p === "/admin/groups") {
    savedAdminGroup = body;
    data = { id: 1 };
  } else if (p === "/admin/config")
    data = {
      encryption_ready: true,
      providers: [
        { id: "school", name: "学校模型", enabled: true, base_url: "https://example.com", has_key: true },
        { id: "voice", name: "语音服务", enabled: true, base_url: "https://example.com", has_key: true },
      ],
      groups: [{ id: 1, name: "进阶组", model_ids: [1, 3] }],
      users: [],
      audit: [],
      models: [
        { id: 1, name: "视觉与推理", model_id: "vision-test", kind: "chat", provider_id: "school", enabled: true, voices: [], json_mode: false, instructions: "" },
        { id: 2, name: "英语评审", model_id: "essay-test", kind: "chat", provider_id: "school", enabled: true, voices: [], json_mode: false, instructions: "" },
        { id: 3, name: "合成语音", model_id: "tts-test", kind: "tts", provider_id: "voice", enabled: true, voices: ["clear"], json_mode: false, instructions: "" },
        { id: 4, name: "旧版停用", model_id: "old-test", kind: "chat", provider_id: "voice", enabled: false, voices: [], json_mode: false, instructions: "" },
      ],
    };
  else if (p === "/admin/learning/tasks")
    data = {
      tasks: [
        {
          task: "explanation",
          prompt: "逐步解释当前题目。",
          backend: "api",
          codex_model: "",
          model_ids: [1],
          lead_model_id: null,
          revision: 1,
        },
      ],
      defaults: {
        explanation: { name: "动态示例", prompt: "逐步解释当前题目。" },
      },
    };
  else if (p === "/admin/learning/documents") data = { documents: [] };
  else if (p === "/admin/learning/word-sets") data = { sets: [] };
  else if (p === "/admin/learning/questions") data = { questions: [] };
  else if (p === "/admin/learning/cloud") data = { connections: [] };
  await route.fulfill({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(data),
  });
});
let checks = 0;
async function noOverflow() {
  assert.ok(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    ),
    `overflow ${page.url()}`,
  );
  checks++;
}
await fs.mkdir("../../docs/previews", { recursive: true });
for (const width of [320, 390, 768, 1440]) {
  await page.setViewportSize({ width, height: 1000 });
  await page.goto(base);
  await page.locator(".subject-card").first().waitFor();
  assert.equal(await page.locator(".subject-card").count(), 6);
  checks++;
  await noOverflow();
  if (width === 1440)
    await page.screenshot({
      path: "../../docs/previews/learning-desktop.png",
      fullPage: true,
    });
  if (width === 390)
    await page.screenshot({
      path: "../../docs/previews/learning-mobile.png",
      fullPage: true,
    });
}
await page.setViewportSize({ width: 390, height: 844 });
await page.goto(base + "/practice?subject=english");
await page.getByText("进阶学习组", { exact: true }).waitFor();
await page.getByRole("combobox", { name: /助学模型/ }).selectOption("2");
await page.getByText("模型选择已保存", { exact: true }).waitFor();
assert.equal(selectedModels.model_id, 2);
checks++;
await page.setViewportSize({ width: 1440, height: 1000 });
await noOverflow();
await page.screenshot({
  path: "../../docs/previews/learning-practice-desktop.png",
  fullPage: true,
});
await page.setViewportSize({ width: 390, height: 844 });
await page.getByRole("button", { name: "开始练习", exact: true }).click();
await page.getByRole("heading", { name: question.content.text }).waitFor();
assert.equal(
  explanationRequests.length,
  0,
  "opening a question generated an explanation",
);
checks++;
assert.equal(await page.locator(".timeline-player").count(), 0);
checks++;
await page.locator(".question-choices label").nth(1).click();
await page.getByRole("button", { name: "提交作答" }).click();
await page.locator(".highlight").waitFor();
assert.equal(await page.locator(".correct-choice").count(), 1);
assert.equal(await page.locator(".incorrect-choice").count(), 1);
checks++;
assert.equal(explanationRequests.length, 1);
assert.equal(explanationRequests[0].reason, "incorrect");
assert.equal(explanationRequests[0].model_selection.model_id, 2);
checks++;
assert.equal(await page.locator(".annotation-role").innerText(), "主语");
checks++;
await page.getByRole("button", { name: "下一步", exact: true }).click();
assert.equal(await page.locator(".annotation-role").innerText(), "定语从句");
checks++;
await page.getByLabel("对当前步骤的疑问").fill("为什么不是主句？");
await page.getByRole("button", { name: "问问 AI" }).click();
await page
  .getByText("先看helped的主语，再暂时遮住that从句。", { exact: false })
  .waitFor();
checks++;
await noOverflow();
await page.screenshot({
  path: "../../docs/previews/learning-explanation-mobile.png",
  fullPage: true,
});
// A correct answer uses no AI until the student explicitly asks for help.
await page.goto(base + "/practice?subject=english");
await page.getByRole("button", { name: "开始练习", exact: true }).click();
await page.locator(".question-choices label").first().click();
await page.getByRole("button", { name: "提交作答", exact: true }).click();
await page
  .getByRole("button", { name: "没理解，动态讲解", exact: true })
  .waitFor();
assert.equal(explanationRequests.length, 1);
assert.equal(await page.locator(".timeline-player").count(), 0);
checks++;
await page
  .getByRole("button", { name: "没理解，动态讲解", exact: true })
  .click();
await page.locator(".timeline-player").waitFor();
assert.equal(explanationRequests.length, 2);
assert.equal(explanationRequests[1].reason, "self_report");
checks++;
// The essay selector offers only identity-group models and supports independent reviewers.
await page.goto(base + "/practice?subject=chinese");
await page
  .getByRole("group", { name: "作文独立评审 · 选择2至5个", exact: true })
  .waitFor();
await page.getByRole("checkbox", { name: "推理模型", exact: true }).uncheck();
await page.getByRole("checkbox", { name: "主审模型", exact: true }).check();
await page
  .getByRole("combobox", { name: "主审模型", exact: true })
  .selectOption("2");
await page.getByText("模型选择已保存", { exact: true }).waitFor();
assert.deepEqual(selectedModels.reviewer_ids, [1, 3]);
assert.equal(selectedModels.lead_model_id, 2);
checks++;
await noOverflow();
for (const level of ["普通", "绝望"]) {
  await page.goto(base + "/challenge");
  await page.getByRole("radio", { name: level, exact: false }).check();
  await page.getByRole("button", { name: "开始挑战", exact: true }).click();
  await page.locator(".challenge-clock strong").waitFor();
  assert.ok(
    Number(await page.locator(".challenge-clock strong").innerText()) <=
      (level === "普通" ? 6 : 3),
  );
  checks++;
  if (level === "绝望") {
    assert.equal(await page.locator(".challenge-options small").count(), 4);
    checks++;
  }
  await noOverflow();
  await page.getByRole("button", { name: "停止连做" }).click();
}
await page.goto(base + "/assistant#sentence");
await page.getByLabel("英语原句").fill(question.content.passage);
await page.getByRole("button", { name: "拆解这句话", exact: true }).click();
await page.locator(".sentence-analysis").waitFor();
assert.equal(await page.locator(".timeline-player").count(), 0);
checks++;
await page.getByRole("button", { name: "查看参考答案", exact: true }).click();
assert.equal(await page.locator(".timeline-player").count(), 0);
checks++;
await page
  .getByRole("button", { name: "没理解，动态演示", exact: true })
  .click();
await page.locator(".timeline-player").waitFor();
checks++;
await page.goto(base + "/admin/learning");
await page
  .getByRole("button", { name: "AI 提示词与模型", exact: true })
  .click();
await page.getByRole("combobox").first().selectOption("explanation");
assert.equal(
  await page.locator("textarea").first().inputValue(),
  "逐步解释当前题目。",
);
checks++;
await noOverflow();
// Administrators can change a bounded grant scope without losing other categories.
for (const width of [320, 390, 768, 1024, 1440]) {
  await page.setViewportSize({ width, height: 900 });
  await page.goto(base + "/admin#groups");
  await page.getByRole("heading", { name: "身份组与可用模型" }).waitFor();
  await page.getByRole("button", { name: /进阶组/ }).click();
  await noOverflow();
  checks++;
}
await page.getByRole("combobox", { name: "服务商范围" }).selectOption("school");
await page.getByRole("combobox", { name: "模型类别" }).selectOption("chat");
await page.getByRole("button", { name: "开启当前范围" }).click();
assert.equal(await page.getByText("已选 3 个").count(), 1);
checks++;
await page.getByRole("combobox", { name: "服务商范围" }).selectOption("voice");
await page.getByRole("combobox", { name: "模型类别" }).selectOption("tts");
await page.getByRole("button", { name: "关闭当前范围" }).click();
assert.equal(await page.getByText("已选 2 个").count(), 1);
await page.getByRole("button", { name: "开启当前范围" }).click();
checks++;
await page.getByRole("combobox", { name: "模型类别" }).selectOption("chat");
assert.equal(await page.locator(".model-scope-grid input:disabled").count(), 1);
checks++;
await page.getByRole("combobox", { name: "模型类别" }).selectOption("all");
await page.getByRole("checkbox", { name: "仅看已选" }).check();
assert.equal(await page.locator(".model-scope-item").count(), 1);
checks++;
await page.getByRole("checkbox", { name: "仅看已选" }).uncheck();
await page.locator(".admin-confirm input[type=password]").fill("test-password");
await page.getByRole("button", { name: "保存身份组" }).click();
await page.getByText("权限组已保存", { exact: false }).waitFor();
assert.deepEqual([...savedAdminGroup.model_ids].sort((a, b) => a - b), [1, 2, 3]);
checks++;

await page.emulateMedia({ reducedMotion: "reduce", colorScheme: "dark" });
await page.goto(base);
await page.locator(".subject-card").first().waitFor();
await noOverflow();
assert.deepEqual(failures, []);
checks++;
await browser.close();
console.log(
  `Learning UI: ${checks} checks passed (mock API; no paid model calls).`,
);
