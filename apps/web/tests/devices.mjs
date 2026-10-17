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
const failures = [];
let debugPage;
let checks = 0;
const configurations = [
  { name: "small-phone", width: 320, height: 640, touch: true, mobile: true },
  { name: "phone", width: 390, height: 844, touch: true, mobile: true },
  {
    name: "phone-landscape",
    width: 844,
    height: 390,
    touch: true,
    mobile: true,
  },
  { name: "tablet-portrait", width: 834, height: 1112, touch: true },
  { name: "tablet-landscape", width: 1194, height: 834, touch: true },
  { name: "tablet-split", width: 600, height: 900, touch: true },
  { name: "navigation-boundary", width: 701, height: 900, touch: true },
  { name: "desktop", width: 1440, height: 1000, touch: false },
  { name: "wide-desktop", width: 1920, height: 1080, touch: false },
  { name: "large-display", width: 2560, height: 1440, touch: false },
];
const pages = [
  "/",
  "/practice?subject=english",
  "/challenge",
  "/english",
  "/assistant#chat",
  "/assistant#sentence",
  "/vocabulary",
  "/settings",
  "/progress",
  "/review",
  "/exams",
  "/admin/engine",
  "/admin",
  "/admin/learning",
];
async function layout(page, label) {
  const metrics = await page.evaluate(() => {
    const nav = document.querySelector('nav[aria-label="主要导航"]');
    const links = Array.from(nav?.querySelectorAll("a") || []).map((a) => {
      const r = a.getBoundingClientRect();
      return { x: r.x, right: r.right, width: r.width, height: r.height };
    });
    const fields = Array.from(
      document.querySelectorAll(
        'input:not([type="hidden"]):not([type="file"]):not([type="range"]),select,textarea',
      ),
    )
      .filter(
        (e) =>
          e.getBoundingClientRect().width &&
          getComputedStyle(e).display !== "none",
      )
      .map((e) => ({
        font: parseFloat(getComputedStyle(e).fontSize),
        width: e.getBoundingClientRect().width,
      }));
    return {
      width: innerWidth,
      overflow: document.documentElement.scrollWidth > innerWidth + 1,
      links,
      fields,
      current: nav?.querySelectorAll('[aria-current="page"]').length ?? 0,
    };
  });
  assert.equal(metrics.overflow, false, `${label}: horizontal overflow`);
  checks++;
  assert.equal(metrics.current, 1, `${label}: one active navigation section`);
  checks++;
  assert.ok(
    metrics.links.every(
      (r) =>
        r.width >= 43 &&
        r.height >= 43 &&
        r.x >= -1 &&
        r.right <= metrics.width + 1,
    ),
    `${label}: navigation touch targets`,
  );
  checks++;
  assert.ok(
    metrics.fields.every((f) => f.font >= 16 || f.width < 25),
    `${label}: readable form controls`,
  );
  checks++;
}
async function press(locator, touch) {
  if (touch) await locator.tap();
  else await locator.click();
}
async function contextFor(config, options = {}) {
  const context = await browser.newContext({
    viewport: { width: config.width, height: config.height },
    hasTouch: config.touch,
    isMobile: !!config.mobile,
    deviceScaleFactor: 1,
  });
  context.setDefaultTimeout(8000);
  const fixture = await installMockAPI(context, options);
  const page = await context.newPage();
  debugPage = page;
  page.on("pageerror", (e) => failures.push(`${config.name}: ${e.message}`));
  return { context, fixture, page };
}
await fs.mkdir("../../docs/previews", { recursive: true });
try {
  for (const config of configurations) {
    const options = { figures: true, longContent: true };
    const { context, fixture, page } = await contextFor(config, options);
    for (const path of pages) {
      await page.goto(base + path);
      await page.locator("h1").waitFor();
      await page.locator('nav[aria-label="主要导航"] a').last().waitFor();
      await layout(page, `${config.name} ${path}`);
    }
    // Typing focus should stay with the user on a touch screen; desktop retains automatic focus.
    await page.goto(base + "/english");
    await page
      .getByRole("button", { name: "开始今日学习", exact: true })
      .waitFor();
    await press(
      page.getByRole("button", { name: "开始今日学习", exact: true }),
      config.touch,
    );
    await page.locator(".question input").waitFor();
    const focused = await page
      .locator(".question input")
      .evaluate((e) => document.activeElement === e);
    assert.equal(
      focused,
      !config.touch,
      `${config.name}: auto keyboard policy`,
    );
    checks++;
    await page.locator(".question input").fill("adapt");
    await page.locator(".question input").press("Enter");
    await page.locator(".rating-buttons").waitFor();
    checks++;
    // The entire timed choice set must be reachable as soon as the round starts.
    await page.goto(base + "/challenge");
    await press(
      page.getByRole("button", { name: "开始挑战", exact: true }),
      config.touch,
    );
    await page.locator(".challenge-options button").last().waitFor();
    await page.waitForFunction(() =>
      document.activeElement?.matches(".challenge-question h2"),
    );
    const roundBounds = await page.evaluate(() => {
      const nav = document.querySelector('nav[aria-label="主要导航"]');
      const bottom =
        nav && getComputedStyle(nav).position === "fixed"
          ? nav.getBoundingClientRect().top
          : innerHeight;
      return {
        bottom,
        choices: Array.from(
          document.querySelectorAll(".challenge-options button"),
        ).map((e) => {
          const r = e.getBoundingClientRect();
          return { top: r.top, bottom: r.bottom, height: r.height };
        }),
      };
    });
    assert.ok(
      roundBounds.choices.every(
        (r) =>
          r.top >= 0 && r.bottom <= roundBounds.bottom + 1 && r.height >= 44,
      ),
      `${config.name}: timed options should fit before the dock`,
    );
    checks++;
    if (config.name === "phone-landscape")
      await page.screenshot({
        path: "../../docs/previews/learning-challenge-landscape.jpg",
        fullPage: false,
        type: "jpeg",
        quality: 85,
        animations: "disabled",
      });
    await press(
      page.getByRole("button", { name: "停止连做", exact: true }),
      config.touch,
    );
    assert.equal(
      await page.locator(".challenge-clock strong").innerText(),
      "已停止",
    );
    checks++;
    // Settings keep the account section first and personal API below the core settings.
    await page.goto(base + "/settings");
    assert.ok(
      await page
        .locator("#page-content")
        .evaluate(
          (e) =>
            !!(
              e.compareDocumentPosition(
                document.querySelector(".personal-api"),
              ) & Node.DOCUMENT_POSITION_FOLLOWING
            ),
        ),
    );
    checks++;
    options.signedOut = true;
    await page.goto(base + "/login");
    await page.getByRole("button", { name: "进入学习", exact: true }).waitFor();
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      ),
      false,
      `${config.name}: login overflow`,
    );
    checks++;
    await context.close();
  }
  // A single mounted practice survives rotation and tablet split-window resizing.
  const config = {
    name: "touch-flow",
    width: 390,
    height: 844,
    touch: true,
    mobile: true,
  };
  const { context, fixture, page } = await contextFor(config, {
    figures: true,
  });
  await page.goto(base + "/practice?subject=english");
  const modelToggle = page.getByRole("button", { name: /模型与身份组/ });
  assert.equal(await modelToggle.getAttribute("aria-expanded"), "false");
  checks++;
  await modelToggle.tap();
  await page.getByRole("combobox", { name: /助学模型/ }).selectOption("2");
  await page.setViewportSize({ width: 844, height: 390 });
  assert.equal(await modelToggle.getAttribute("aria-expanded"), "true");
  assert.equal(
    await page.getByRole("combobox", { name: /助学模型/ }).inputValue(),
    "2",
  );
  checks++;
  await page.setViewportSize({ width: 600, height: 900 });
  assert.equal(
    await page.getByRole("combobox", { name: /助学模型/ }).inputValue(),
    "2",
  );
  checks++;
  await page.getByRole("button", { name: "开始练习", exact: true }).tap();
  await page.locator(".question-choices label").nth(1).tap();
  await page.setViewportSize({ width: 1194, height: 834 });
  assert.equal(
    await page.locator(".question-choices input").nth(1).isChecked(),
    true,
  );
  checks++;
  assert.equal(fixture.explanationRequests.length, 0);
  checks++;
  // Modal figures support tap, keyboard zoom, scroll, Escape and focus return.
  const image = page.getByRole("button", {
    name: "放大查看：三角形示意图",
    exact: true,
  });
  await image.tap();
  await page.getByRole("dialog").waitFor();
  const zoom = page.getByRole("slider", { name: /放大倍率/ });
  await zoom.focus();
  await zoom.press("End");
  assert.equal(await zoom.inputValue(), "3");
  assert.ok(
    await page
      .locator(".figure-canvas")
      .evaluate((e) => e.scrollWidth > e.clientWidth),
  );
  checks++;
  await page.keyboard.press("Escape");
  assert.equal(await page.getByRole("dialog").count(), 0);
  assert.equal(await image.evaluate((e) => e === document.activeElement), true);
  checks++;
  await page.getByRole("button", { name: "提交作答", exact: true }).tap();
  await page.locator(".timeline-player").waitFor();
  assert.equal(fixture.explanationRequests.length, 1);
  checks++;
  await page.setViewportSize({ width: 834, height: 1112 });
  await page.getByRole("link", { name: "查看动态讲解", exact: true }).tap();
  await page.getByRole("button", { name: "下一步", exact: true }).tap();
  assert.equal(await page.locator(".annotation-role").innerText(), "定语从句");
  checks++;
  await layout(page, "tablet review");
  await page.screenshot({
    path: "../../docs/previews/learning-tablet-review.jpg",
    fullPage: true,
    type: "jpeg",
    quality: 85,
    animations: "disabled",
  });
  // A hidden tab stops automatic rounds without changing the server's original deadline.
  await page.goto(base + "/challenge");
  await page.getByRole("button", { name: "开始挑战", exact: true }).tap();
  await page.locator(".challenge-options button").last().waitFor();
  const nextBefore = fixture.requests.filter(
    (r) => r.path === "/learning/words/next",
  ).length;
  await page.evaluate(() => {
    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      value: "hidden",
    });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await page.waitForFunction(
    () =>
      document.querySelector(".challenge-clock strong")?.textContent ===
      "已停止",
  );
  assert.equal(
    fixture.requests.filter((r) => r.path === "/learning/words/next").length,
    nextBefore,
  );
  checks++;
  await context.close();
  // Simulate the browser viewport contract used by Android/iPad keyboards.
  const keyboard = await browser.newContext({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    isMobile: true,
  });
  keyboard.setDefaultTimeout(8000);
  await keyboard.addInitScript(() => {
    let height = innerHeight;
    const v = new EventTarget();
    Object.defineProperties(v, {
      height: { get: () => height },
      width: { get: () => innerWidth },
      offsetTop: { value: 0 },
      offsetLeft: { value: 0 },
      scale: { value: 1 },
    });
    Object.defineProperty(window, "visualViewport", {
      configurable: true,
      value: v,
    });
    window.__keyboardHeight = (value) => {
      height = value;
      v.dispatchEvent(new Event("resize"));
    };
  });
  const kfixture = await installMockAPI(keyboard);
  const kpage = await keyboard.newPage();
  kpage.on("pageerror", (e) => failures.push(e.message));
  await kpage.goto(base + "/practice?subject=chinese");
  await kpage
    .getByRole("combobox", { name: "题型", exact: true })
    .selectOption("modern_reading");
  await kpage.getByRole("button", { name: "开始练习", exact: true }).tap();
  const writing = kpage.getByRole("textbox", {
    name: "第1题作答",
    exact: true,
  });
  await writing.fill("学习与思考相互促进，不能偏废。");
  await kpage.evaluate(() => window.__keyboardHeight(430));
  await kpage.setViewportSize({ width: 390, height: 430 });
  await kpage.waitForFunction(
    () => document.documentElement.dataset.keyboardOpen === "true",
  );
  assert.equal(
    await kpage.locator('nav[aria-label="主要导航"]').isVisible(),
    false,
  );
  checks++;
  await kpage
    .getByRole("button", { name: "提交作答", exact: true })
    .scrollIntoViewIfNeeded();
  assert.ok(
    await kpage
      .getByRole("button", { name: "提交作答", exact: true })
      .evaluate((e) => e.getBoundingClientRect().bottom <= innerHeight),
  );
  checks++;
  await kpage.evaluate(() => window.__keyboardHeight(844));
  await kpage.setViewportSize({ width: 390, height: 844 });
  await writing.blur();
  await kpage.waitForFunction(
    () => document.documentElement.dataset.keyboardOpen === "false",
  );
  assert.equal(
    await kpage.locator('nav[aria-label="主要导航"]').isVisible(),
    true,
  );
  checks++;
  await writing.fill("切换页面前保留的作答草稿");
  await kpage
    .getByRole("navigation", { name: "主要导航" })
    .getByRole("link", { name: "词库", exact: true })
    .tap();
  await kpage
    .getByRole("heading", { name: "自己的节奏，自己的词汇。", exact: true })
    .waitFor();
  await kpage.waitForFunction(() => location.pathname === "/vocabulary");
  assert.ok(
    kfixture.requests.some(
      (r) =>
        r.path.endsWith("/draft") &&
        r.body.answers?.["1"] === "切换页面前保留的作答草稿" &&
        r.headers["x-csrf-token"] === "test",
    ),
  );
  checks++;
  assert.equal(kfixture.explanationRequests.length, 0);
  checks++;
  await keyboard.close();
  // Keyboard-only navigation and administration remain usable with no touch surface.
  const desktop = await contextFor({
    name: "keyboard",
    width: 1440,
    height: 900,
    touch: false,
  });
  await desktop.page.goto(base + "/admin#audit");
  await desktop.page
    .getByRole("region", { name: "数据表格，可横向滚动", exact: true })
    .waitFor();
  const table = desktop.page.getByRole("region", {
    name: "数据表格，可横向滚动",
    exact: true,
  });
  await table.focus();
  assert.equal(await table.evaluate((e) => document.activeElement === e), true);
  checks++;
  await desktop.page.goto(base + "/practice?subject=english");
  await desktop.page.getByRole("navigation", { name: "主要导航" }).waitFor();
  await desktop.page.getByRole("navigation", { name: "主要导航" }).waitFor();
  await desktop.page.keyboard.press("Tab");
  assert.equal(
    await desktop.page.evaluate(() =>
      document.activeElement?.matches(".skip-link"),
    ),
    true,
  );
  await desktop.page.keyboard.press("Enter");
  assert.ok(
    await desktop.page.evaluate(
      () =>
        document.activeElement?.id === "page-content" ||
        document.activeElement?.closest("#page-content"),
    ),
  );
  checks++;
  await desktop.page.setViewportSize({ width: 720, height: 600 });
  await layout(desktop.page, "desktop narrow window");
  await desktop.page.setViewportSize({ width: 1440, height: 900 });
  await desktop.page.goto(base + "/challenge");
  await desktop.page
    .getByRole("button", { name: "开始挑战", exact: true })
    .click();
  await desktop.page.waitForFunction(() =>
    document.activeElement?.matches(".challenge-question h2"),
  );
  await desktop.page.keyboard.press("1");
  await desktop.page.getByText("答对了。", { exact: true }).waitFor();
  assert.ok(
    desktop.fixture.requests.some(
      (r) => r.path.endsWith("/answer") && r.body.picked === "A",
    ),
  );
  checks++;
  await desktop.page
    .getByRole("button", { name: "停止连做", exact: true })
    .click();
  await desktop.context.close();
  const learner = await contextFor(
    { name: "learner", width: 390, height: 844, touch: true, mobile: true },
    { role: "learner" },
  );
  await learner.page.goto(base + "/");
  await learner.page.getByRole("navigation", { name: "主要导航" }).waitFor();
  assert.equal(
    await learner.page
      .getByRole("navigation", { name: "主要导航" })
      .getByRole("link")
      .count(),
    5,
  );
  checks++;
  assert.equal(await learner.page.getByRole("navigation", {name:"主要导航"}).getByRole("link", {name:"资源",exact:true}).count(),1);
  assert.equal(await learner.page.getByRole("navigation", {name:"主要导航"}).getByRole("link", {name:"管理",exact:true}).count(),0);
  checks+=2;
  await learner.context.close();
  const firstLogin = await contextFor(
    { name: "first-login", width: 834, height: 1112, touch: true },
    { role: "learner", mustChange: true },
  );
  await firstLogin.page.goto(base + "/settings");
  await firstLogin.page
    .getByText("首次登录或管理员重置后，请先修改初始密码，再进入学习。", {
      exact: true,
    })
    .waitFor();
  assert.equal(await firstLogin.page.locator(".personal-api").count(), 0);
  checks++;
  assert.equal(
    await firstLogin.page
      .getByRole("navigation", { name: "主要导航" })
      .getByRole("link")
      .count(),
    1,
  );
  checks++;
  assert.equal(
    firstLogin.fixture.requests.some((r) => r.path === "/learning/ai-settings"),
    false,
  );
  checks++;
  await firstLogin.context.close();
  assert.deepEqual(failures, []);
  console.log(
    `Device UI: ${checks} checks passed across 8 viewports, touch/keyboard, rotation, viewport keyboard and draft navigation (mock API).`,
  );
} catch (error) {
  if (debugPage && !debugPage.isClosed())
    await debugPage
      .screenshot({ path: "/tmp/learning-device-failure.png", fullPage: true })
      .catch(() => {});
  throw error;
} finally {
  await browser.close();
}
