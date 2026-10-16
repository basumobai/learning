// Original learning content and deterministic API responses; no real model calls.
import assert from "node:assert/strict";
export const question = {
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
export const timeline = {
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
export async function installMockAPI(context, options = {}) {
  let submitted = false,
    draft = {},
    difficulty = "normal",
    round = 0;
  const explanationRequests = [];
  let selectedModels = { model_id: 1, reviewer_ids: [1, 2], lead_model_id: 3 };
  const requests = [];
  let activeQuestion = question,
    study = null,
    suspended = false;
  function attempt() {
    return {
      id: "attempt1",
      question: activeQuestion,
      server_time: new Date().toISOString(),
      deadline: null,
      submitted_at: submitted ? new Date().toISOString() : null,
      answers: submitted ? draft : null,
      draft,
      feedback: submitted
        ? { score: draft["1"] === "A" ? 2 : 0, max_score: 2, needs_ai: false }
        : null,
      explanation_needed:
        submitted && activeQuestion.kind === "reading" && draft["1"] !== "A",
      jobs: [],
    };
  }
  await context.route("**/api/v1/**", async (route) => {
    const p = new URL(route.request().url()).pathname.replace("/api/v1", "");
    const body =
      route.request().method() === "POST" ? route.request().postDataJSON() : {};
    let data = {};
    requests.push({
      path: p,
      body,
      method: route.request().method(),
      headers: route.request().headers(),
    });
    if (p === "/auth/me" && options.signedOut) {
      await route.fulfill({
        status: 401,
        contentType: "application/json",
        body: JSON.stringify({ error: "请登录" }),
      });
      return;
    }
    if (p.startsWith("/learning/assets/")) {
      await route.fulfill({
        status: 200,
        contentType: "image/svg+xml",
        body: '<svg xmlns="http://www.w3.org/2000/svg" width="1000" height="600"><rect width="1000" height="600" fill="white"/><path d="M100 500L500 100L900 500Z" fill="#e6f2ff" stroke="#0066cc" stroke-width="5"/><text x="480" y="70" font-size="40">A</text><text x="50" y="530" font-size="40">B</text><text x="920" y="530" font-size="40">C</text></svg>',
      });
      return;
    }
    if (p === "/auth/me")
      data = {
        user: {
          id: "test",
          display_name: "学习者",
          username: "tester",
          role: options.role || "admin",
          must_change_password: !!options.mustChange,
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
          {
            id: "chinese",
            types: [
              { id: "essay", name: "作文" },
              { id: "modern_reading", name: "现代文阅读" },
            ],
          },
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
          {
            id: "chinese1",
            subject_id: "chinese",
            title: "学习与思考",
            textbook: "必修上",
            term: "高一上",
            question_count: 3,
          },
        ],
      };
    else if (p === "/learning/attempts") {
      submitted = false;
      draft = {};
      activeQuestion =
        body.subject_id === "chinese"
          ? {
              ...question,
              subject_id: "chinese",
              kind: body.kind,
              content: {
                text: "阅读材料并作答。",
                passage: "学而不思则罔，思而不学则殆。",
                parts: [{ id: "1", text: "说明学习与思考的关系。", points: 6 }],
              },
            }
          : {
              ...question,
              assets: options.figures
                ? [{ id: "figure1", alt: "三角形示意图" }]
                : [],
            };
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
      data = {
        models: [{ id: 1, name: "讲解模型", kind: "chat", voices: [] }],
      };
    else if (p === "/preferences")
      data = {
        daily_new: 10,
        retention: 0.9,
        tts_model_id: 0,
        voice: "",
        timezone: "Asia/Shanghai",
      };
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
    else if (p === "/admin/config")
      data = {
        encryption_ready: true,
        providers: [
          {
            id: "school",
            name: "学校接口",
            base_url: "https://example.test/v1",
            enabled: true,
            has_key: true,
          },
        ],
        models: [
          {
            id: 1,
            provider_id: "school",
            name: "视觉与推理",
            model_id: "vision-test",
            kind: "chat",
            enabled: true,
            voices: [],
            instructions: "",
            json_mode: true,
          },
        ],
        groups: [{ id: 1, name: "基础学习", model_ids: [1] }],
        users: [
          {
            id: "test",
            username: "tester",
            display_name: "学习者",
            role: "learner",
            group_id: 1,
            disabled: false,
          },
        ],
        audit: [
          {
            id: 1,
            actor: "管理员",
            action: "save_learning_task",
            target: "explanation",
            created_at: "2026-10-16T08:00:00Z",
          },
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
    else if (p.startsWith("/vocabulary/suspend")) {
      suspended = body.suspended;
      data = { saved: true };
    } else if (p.startsWith("/vocabulary"))
      data = {
        entries: [
          {
            id: 1,
            word: options.longContent ? "electroencephalographically" : "adapt",
            phonetic: "/əˈdæpt/",
            pos: "v.",
            chinese: "适应；调整；改编",
            suspended,
            failed_recalls: 2,
          },
        ],
      };
    else if (p === "/dashboard")
      data = {
        new_today: 0,
        new_remaining: 10,
        display_name: "学习者",
        due: 3,
        studied: 0,
        demo_words: 5,
        today: {
          reviewed: 0,
          unassisted_correct: 0,
          again: 0,
          awaiting_rating: 0,
          used_hint: 0,
        },
        dimensions: [],
      };
    else if (p === "/study/sessions/active") data = { session: study };
    else if (p === "/study/sessions") {
      study = {
        id: 1,
        mode: body.mode,
        questions: [
          {
            id: 1,
            type: "spelling",
            prompt: "适应；调整；改编",
            hint_text: "a开头",
            position: 1,
            result: null,
            rating: null,
            used_hint: false,
          },
        ],
      };
      data = { id: 1 };
    } else if (p === "/study/sessions/1") data = study;
    else if (p === "/study/sessions/1/answers") {
      Object.assign(study.questions[0], {
        result: body.answer === "adapt" ? "correct" : "wrong",
        answer: body.answer,
        expected: "adapt",
        phonetic: "/əˈdæpt/",
        pos: "v.",
        example: "Adapt to a new school.",
        intervals: {
          1: new Date(Date.now() + 60000).toISOString(),
          2: new Date(Date.now() + 3600000).toISOString(),
          3: new Date(Date.now() + 86400000).toISOString(),
          4: new Date(Date.now() + 172800000).toISOString(),
        },
      });
      data = { saved: true };
    } else if (p === "/study/sessions/1/review") {
      study.questions[0].rating = body.rating;
      data = { saved: true };
    } else if (p === "/learning/ai-settings")
      data = {
        has_key: false,
        enabled: false,
        encryption_ready: true,
        reviewer_models: [],
      };
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(data),
    });
  });
  return {
    requests,
    explanationRequests,
    get selection() {
      return selectedModels;
    },
    get draft() {
      return draft;
    },
    get submitted() {
      return submitted;
    },
  };
}
