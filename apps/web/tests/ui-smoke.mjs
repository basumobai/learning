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
    feedback: submitted ? { score: 0, max_score: 2, needs_ai: false } : null,
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
  else if (p === "/learning/catalog")
    data = {
      region: "广东",
      subjects: [
        { id: "english", types: [{ id: "reading", name: "阅读理解" }] },
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
  } else if (p.endsWith("/explanation")) data = { id: "job1" };
  else if (p === "/learning/jobs/job1")
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
  else if (p === "/admin/config")
    data = {
      models: [
        { id: 1, name: "视觉与推理", model_id: "vision-test", kind: "chat" },
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
await page.getByRole("button", { name: "开始练习", exact: true }).click();
await page.getByRole("heading", { name: question.content.text }).waitFor();
assert.equal(await page.locator(".timeline-player").count(), 0);
checks++;
await page.locator(".question-choices label").nth(1).click();
await page.getByRole("button", { name: "提交并看讲解" }).click();
await page.locator(".highlight").waitFor();
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
