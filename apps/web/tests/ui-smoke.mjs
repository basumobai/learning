import assert from "node:assert/strict";
import { chromium } from "playwright";
import fs from "node:fs/promises";
import { installMockAPI, question } from "./mock-api.mjs";
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
const fixtures = await installMockAPI(context);
const { explanationRequests } = fixtures;
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
await page.getByRole("button", { name: "模型与身份组", exact: false }).click();
await page.getByText("进阶学习组", { exact: true }).waitFor();
await page.getByRole("combobox", { name: /助学模型/ }).selectOption("2");
await page.getByText("模型选择已保存", { exact: true }).waitFor();
assert.equal(fixtures.selection.model_id, 2);
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
await page.getByRole("button", { name: "模型与身份组", exact: false }).click();
await page
  .getByRole("group", { name: "作文独立评审 · 选择2至5个", exact: true })
  .waitFor();
await page.getByRole("checkbox", { name: "推理模型", exact: true }).uncheck();
await page.getByRole("checkbox", { name: "主审模型", exact: true }).check();
await page
  .getByRole("combobox", { name: "主审模型", exact: true })
  .selectOption("2");
await page.getByText("模型选择已保存", { exact: true }).waitFor();
assert.deepEqual(fixtures.selection.reviewer_ids, [1, 3]);
assert.equal(fixtures.selection.lead_model_id, 2);
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
await page.getByRole("button", { name: "开始独立拆句", exact: true }).click();
await page.getByLabel("我的翻译").fill("你借给我的书帮助了我。");
assert.equal(await page.locator(".sentence-analysis").count(), 0);
await page.getByRole("button", { name: "提交尝试并看反馈", exact: true }).click();
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
