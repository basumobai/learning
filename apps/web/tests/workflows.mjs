import assert from "node:assert/strict";
import { chromium } from "playwright";
import { installMockAPI } from "./mock-api.mjs";

const base = process.env.UI_BASE || "http://127.0.0.1:3007";
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || undefined,
  args: ["--no-sandbox"],
});
let checks = 0;
async function noOverflow(page) {
  assert.ok(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    ),
    `overflow at ${page.url()}`,
  );
  checks++;
}
try {
  const guest = await browser.newContext({
    viewport: { width: 390, height: 844 },
  });
  await installMockAPI(guest, { signedOut: true });
  let registrations = [];
  await guest.route("**/api/v1/auth/register", async (route) => {
    registrations.push(route.request().postDataJSON());
    await route.fulfill({ status: 201, json: { ok: true } });
  });
  const g = await guest.newPage();
  await g.goto(base + "/register");
  await g.getByLabel("用户名", { exact: true }).fill("new-student");
  await g.getByLabel("显示名称", { exact: true }).fill("新同学");
  await g
    .getByLabel("密码（15–128个字符）", { exact: true })
    .fill("new-student-password");
  await g.getByLabel("确认密码", { exact: true }).fill("different-password");
  await g.getByRole("button", { name: "创建我的账户" }).click();
  await g.getByRole("alert").filter({ hasText: "两次密码不一致" }).waitFor();
  assert.equal(registrations.length, 0);
  checks++;
  await g.getByLabel("确认密码", { exact: true }).fill("new-student-password");
  await g.getByRole("button", { name: "创建我的账户" }).click();
  await g.getByRole("link", { name: "立即登录" }).waitFor();
  assert.deepEqual(Object.keys(registrations[0]).sort(), [
    "display_name",
    "password",
    "username",
  ]);
  checks++;
  await noOverflow(g);
  await guest.route("**/api/v1/auth/options", (route) =>
    route.fulfill({ json: { registration_open: false } }),
  );
  await g.reload();
  await g
    .getByText("本站暂未开放自主注册，请联系管理员。", { exact: true })
    .waitFor();
  checks++;
  await guest.close();

  for (const width of [390, 768, 1440]) {
    const ctx = await browser.newContext({ viewport: { width, height: 1000 } });
    // Recorder lifecycle uses a deterministic browser stub, not real microphone audio.
    await ctx.addInitScript(() => {
      window.__mediaCalls = 0;
      window.__tracksStopped = 0;
      Object.defineProperty(navigator, "mediaDevices", {
        configurable: true,
        value: {
          getUserMedia: async () => {
            window.__mediaCalls++;
            return {
              getTracks: () => [{ stop: () => window.__tracksStopped++ }],
            };
          },
        },
      });
      window.MediaRecorder = class {
        state = "inactive";
        mimeType = "audio/mp4";
        ondataavailable = null;
        onstop = null;
        start() {
          this.state = "recording";
        }
        stop() {
          this.state = "inactive";
          this.ondataavailable?.({
            data: new Blob(["test audio"], { type: this.mimeType }),
          });
          this.onstop?.();
        }
      };
    });
    const mock = await installMockAPI(ctx, { curriculum: true });
    const page = await ctx.newPage();
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    let imported = null;
    let confirmedScope = null;
    await ctx.route("**/api/v1/admin/learning/local-files", (route) =>
      route.fulfill({
        json: {
          host_directory: "/www/wwwroot/learning/learning/data/pdf",
          files: [
            { name: "题目.pdf", size: 1024, importable: true },
            { name: "答案.pdf", size: 2048, importable: true },
          ],
        },
      }),
    );
    await ctx.route("**/api/v1/admin/learning/local-import", async (route) => {
      imported = route.request().postDataJSON();
      await route.fulfill({
        status: 202,
        json: { id: "inbox-doc", job_id: "identify" },
      });
    });
    await ctx.route("**/api/v1/admin/learning/documents", (route) =>
      route.fulfill({
        json: {
          documents: [
            {
              id: "inbox-doc",
              title: "目录中的资料",
              textbook: "北师大版",
              term: "高二上",
              scope_confirmed: !!confirmedScope,
              recognition: {
                subject_id: "english",
                edition: "北师大版2019",
                volume: "选择性必修第一册",
                source_kind: "exercise",
                confidence: "medium",
                evidence: [{ page: 1, quote: "Unit 1 Relationships" }],
                notes: "核对封面与目录。",
              },
              outline: { chapters: [] },
              jobs: [],
            },
          ],
        },
      }),
    );
    await ctx.route(
      "**/api/v1/admin/learning/documents/inbox-doc/scope",
      async (route) => {
        confirmedScope = route.request().postDataJSON();
        await route.fulfill({
          status: 202,
          json: { job_id: "outline-confirmed" },
        });
      },
    );
    await ctx.route(
      "**/api/v1/admin/learning/documents/inbox-doc/ocr",
      (route) =>
        route.fulfill({
          json: {
            pages: [
              {
                kind: "question",
                page: 1,
                text: "Question 1",
                source: "native",
                notes: [],
              },
              {
                kind: "answer",
                page: 1,
                text: "1. A",
                source: "ocr",
                notes: ["请核对公式"],
              },
            ],
          },
        }),
    );
    await page.goto(base + "/admin/learning");
    assert.equal(await page.getByLabel("当前管理员密码", { exact: true }).count(), 0);
    checks++;
    await page.getByLabel("资料标题", { exact: true }).fill("服务器练习册");
    const inbox = page
      .getByRole("heading", { name: "从服务器目录导入 PDF" })
      .locator("..");
    await inbox
      .getByLabel("题目 PDF", { exact: true })
      .selectOption("题目.pdf");
    await inbox
      .getByLabel("答案 PDF", { exact: true })
      .selectOption("答案.pdf");
    await inbox.getByRole("button", { name: "导入所选 PDF" }).click();
    await inbox.getByText(/PDF 已接收/).waitFor();
    assert.equal(imported.question_path, "题目.pdf");
    assert.equal(imported.answer_path, "答案.pdf");
    assert.equal(
      imported.subject_id,
      "",
      "default import must identify instead of guessing a subject",
    );
    checks++;
    await page
      .getByRole("button", { name: "目录中的资料", exact: true })
      .click();
    const scope = page.getByRole("region", { name: "PDF教材归类确认" });
    await scope.getByLabel("确认教材版本", { exact: true }).waitFor();
    await page.waitForFunction(
      () =>
        !!document.querySelector(
          '[aria-label="PDF教材归类确认"] input:disabled',
        ),
    );
    assert.equal(
      await scope.getByLabel("确认教材版本", { exact: true }).isDisabled(),
      true,
    );
    assert.equal(
      await scope.getByLabel("确认教材册次", { exact: true }).inputValue(),
      "选择性必修第一册",
    );
    checks++;
    await scope.getByRole("checkbox").check();
    await scope.getByRole("button", { name: "确认范围并识别章节" }).click();
    await scope.waitFor({ state: "detached" });
    assert.equal(confirmedScope.edition, "北师大版2019");
    assert.equal(confirmedScope.subject_id, "english");
    checks++;
    await page.getByText(/OCR 识别与逐页核对/).click();
    await page.getByText(/答案 · 第1页 · OCR已识别/).click();
    await page.getByText("请核对公式", { exact: true }).waitFor();
    checks++;
    await noOverflow(page);
    await page.goto(base + "/practice?subject=english");
    await page.getByText("广东高考题型框架", { exact: false }).waitFor();
    await page
      .getByLabel("教材版本", { exact: true })
      .selectOption("北师大版2019");
    await page
      .getByLabel("教材册次", { exact: true })
      .selectOption("选择性必修第一册");
    await page.getByRole("button", { name: "开始练习", exact: true }).click();
    await page
      .getByRole("heading", { name: "Choose the subject of the main clause." })
      .waitFor();
    const start = mock.requests.findLast(
      (r) => r.path === "/learning/attempts",
    );
    assert.equal(start.body.edition, "北师大版2019");
    assert.equal(start.body.volume, "选择性必修第一册");
    checks++;
    const tasks = Array.from({ length: 8 }, (_, i) => ({
      index: i,
      kind: i < 3 ? "ask" : "answer",
      cue: i < 3 ? `中文提问提示${i + 1}` : "听问题后用英语回答",
    }));
    const lesson = {
      id: "gd-b",
      kind: "listening",
      format: "guangdong_b",
      title: "广东三问五答",
      scenario: "你和同学讨论活动安排",
      keywords: [],
      speaking_tasks: tasks,
    };
    let generated = null,
      submitted = null;
    await ctx.route("**/api/v1/ai/training", async (route) => {
      if (route.request().method() !== "POST") return route.fallback();
      generated = route.request().postDataJSON();
      await route.fulfill({ json: lesson });
    });
    await ctx.route("**/api/v1/ai/training/gd-b/answers", async (route) => {
      submitted = route.request().postDataJSON();
      await route.fulfill({
        json: {
          ...lesson,
          submitted: true,
          responses: submitted.responses,
          notes: submitted.notes,
          transcript: "The activity begins at three.",
          translation: "活动三点开始。",
          reference_tasks: tasks.map(() => ({
            reference: "At three.",
            question: "When?",
            reply: "At three.",
            points: ["时间信息"],
            evidence: "at three",
          })),
        },
      });
    });
    await ctx.route("**/api/v1/ai/training/gd-b/feedback", (route) =>
      route.fulfill({
        json: {
          summary: "先补齐时间信息。",
          items: tasks.map((_, i) => ({
            index: i,
            assessment: "问句结构需完整。",
            suggestion: "When does it start?",
          })),
        },
      }),
    );
    await page.goto(base + "/assistant#listening");
    await page
      .getByLabel("训练题型", { exact: true })
      .selectOption("guangdong_b");
    await page
      .getByLabel("训练重点", { exact: true })
      .selectOption("记笔记与信息保持");
    await page.getByRole("button", { name: "生成新的听力练习" }).click();
    await page.getByRole("heading", { name: "广东三问五答" }).waitFor();
    assert.equal(generated.format, "guangdong_b");
    assert.equal(generated.focus, "记笔记与信息保持");
    checks++;
    await page
      .getByRole("button", { name: "录制这一项作答", exact: true })
      .click();
    await page.getByRole("button", { name: "结束录音", exact: true }).click();
    await page
      .getByRole("link", { name: "下载本项录音", exact: true })
      .waitFor();
    assert.match(
      await page
        .getByRole("link", { name: "下载本项录音", exact: true })
        .getAttribute("download"),
      /\.m4a$/,
    );
    assert.equal(await page.evaluate(() => window.__mediaCalls), 1);
    assert.ok((await page.evaluate(() => window.__tracksStopped)) > 0);
    checks++;
    await page.getByLabel("听力笔记", { exact: true }).fill("Fri 3pm");
    await page
      .getByLabel("我的英文作答／录音转写", { exact: true })
      .fill("When does it start?");
    await page.getByRole("button", { name: "开始准备计时（20秒）" }).click();
    await page.getByRole("timer").waitFor();
    checks++;
    await page.getByRole("button", { name: "答1", exact: true }).click();
    await page
      .getByLabel("我的英文作答／录音转写", { exact: true })
      .fill("At three.");
    await page
      .getByRole("button", { name: "提交本组作答，核对参考表达" })
      .click();
    await page.getByRole("heading", { name: "本项参考" }).waitFor();
    assert.equal(submitted.responses.length, 8);
    assert.equal(submitted.notes, "Fri 3pm");
    checks++;
    await page.getByRole("button", { name: "AI逐项评价我的作答文字" }).click();
    await page.getByText("先补齐时间信息。", { exact: true }).waitFor();
    checks++;
    await noOverflow(page);
    assert.deepEqual(errors, []);
    checks++;
    await ctx.close();
  }
  const ctx = await browser.newContext();
  await installMockAPI(ctx);
  let unavailable = true;
  let probes = 0;
  await ctx.route("**/api/v1/auth/me", (route) => {
    probes++;
    return unavailable
      ? route.fulfill({ status: 503, json: { error: "学习服务暂时不可用" } })
      : route.fallback();
  });
  const p = await ctx.newPage();
  await p.clock.install();
  await p.goto(base);
  await p.getByRole("heading", { name: "暂时无法连接学习服务" }).waitFor();
  unavailable = false;
  await p.clock.fastForward(15_000);
  await p.locator(".subject-card").first().waitFor();
  checks++;
  unavailable = true;
  probes = 0;
  await p.reload();
  await p.getByRole("heading", { name: "暂时无法连接学习服务" }).waitFor();
  for (const delay of [15_000, 30_000, 60_000]) {
    const response = p.waitForResponse((r) =>
      r.url().endsWith("/api/v1/auth/me"),
    );
    await p.clock.fastForward(delay);
    await (await response).finished();
    // Allow the failed fetch and React effect cleanup to finish before advancing time.
    await p.waitForTimeout(100);
  }
  assert.equal(probes, 4);
  await p.clock.fastForward(300_000);
  await p.waitForTimeout(100);
  assert.equal(
    probes,
    4,
    "automatic retries must stop after the finite budget",
  );
  assert.equal(
    new URL(p.url()).pathname,
    "/",
    "outage must not redirect to login",
  );
  checks++;
  unavailable = false;
  await p.getByRole("button", { name: "重新连接", exact: true }).click();
  await p.locator(".subject-card").first().waitFor();
  checks++;
  await ctx.close();
  console.log(
    `New workflow browser checks passed: ${checks} (registration, PDF/OCR, Guangdong speaking, recovery).`,
  );
} finally {
  await browser.close();
}
