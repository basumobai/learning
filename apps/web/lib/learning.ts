export const subjects = [
  {
    id: "chinese",
    name: "语文",
    symbol: "文",
    note: "阅读、表达与思考",
    tone: "pink",
  },
  {
    id: "math",
    name: "数学",
    symbol: "π",
    note: "让每一步推导清晰",
    tone: "blue",
  },
  {
    id: "english",
    name: "英语",
    symbol: "Aa",
    note: "词汇、语篇与听说",
    tone: "purple",
  },
  {
    id: "physics",
    name: "物理",
    symbol: "F",
    note: "从现象理解规律",
    tone: "green",
  },
  {
    id: "chemistry",
    name: "化学",
    symbol: "H₂",
    note: "看懂变化的本质",
    tone: "yellow",
  },
  {
    id: "biology",
    name: "生物",
    symbol: "生",
    note: "理解生命的联系",
    tone: "green",
  },
] as const;
export type Choice = { id: string; text: string; pos?: string };
export type Part = {
  id: string;
  text: string;
  choices?: Choice[];
  points: number;
};
export type QuestionContent = { text: string; passage?: string; parts: Part[] };
export type Timeline = {
  version: number;
  text: string;
  spans: {
    id: string;
    start: number;
    end: number;
    role: string;
    tone: string;
  }[];
  steps: {
    id: string;
    title: string;
    explanation: string;
    span_ids: string[];
    asset_ids: string[];
  }[];
  summary: string;
};
export type Job = {
  id: string;
  kind: string;
  status: "queued" | "running" | "failed" | "completed";
  error: string;
  result?: Timeline | null;
};
export function hanCount(text: string) {
  return Array.from(text).filter((c) => /\p{Script=Han}/u.test(c)).length;
}
export function timeLabel(seconds: number) {
  const s = Math.max(0, Math.ceil(seconds));
  return `${Math.floor(s / 60)
    .toString()
    .padStart(2, "0")}:${(s % 60).toString().padStart(2, "0")}`;
}
export function activeSegments(timeline: Timeline, stepIndex: number) {
  const chars = Array.from(timeline.text),
    step = timeline.steps[stepIndex];
  const spans = timeline.spans.filter((s) => step?.span_ids.includes(s.id));
  const boundaries = Array.from(
    new Set([0, chars.length, ...spans.flatMap((s) => [s.start, s.end])]),
  ).sort((a, b) => a - b);
  return boundaries.slice(0, -1).map((start, i) => {
    const end = boundaries[i + 1],
      marks = spans.filter((s) => s.start <= start && s.end >= end);
    return { start, text: chars.slice(start, end).join(""), marks };
  });
}
export function sentenceTimeline(
  text: string,
  chunks: { text: string; role: string; explanation: string }[],
  summary: string,
): Timeline {
  const source = Array.from(text);
  let cursor = 0;
  const spans: Timeline["spans"] = [],
    steps: Timeline["steps"] = [];
  chunks.forEach((c, i) => {
    const part = Array.from(c.text);
    let start = -1;
    for (let j = cursor; j <= source.length - part.length; j++) {
      if (source.slice(j, j + part.length).join("") === c.text) {
        start = j;
        break;
      }
    }
    if (start < 0) {
      for (let j = 0; j <= source.length - part.length; j++) {
        if (source.slice(j, j + part.length).join("") === c.text) {
          start = j;
          break;
        }
      }
    }
    if (start < 0) return;
    cursor = start + part.length;
    const id = `chunk-${i}`;
    spans.push({
      id,
      start,
      end: cursor,
      role: c.role,
      tone: ["yellow", "blue", "green", "pink", "purple"][i % 5],
    });
    steps.push({
      id: `step-${i}`,
      title: c.role,
      explanation: c.explanation,
      span_ids: [id],
      asset_ids: [],
    });
  });
  return { version: 1, text, spans, steps, summary };
}
