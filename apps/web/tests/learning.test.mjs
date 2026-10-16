import test from "node:test";
import assert from "node:assert/strict";
import {
  hanCount,
  timeLabel,
  activeSegments,
  sentenceTimeline,
} from "../lib/learning.ts";
test("Han count excludes symbols and English; timer clamps expiration", () => {
  assert.equal(hanCount("你好，123a😀世 界"), 4);
  assert.equal(timeLabel(-1), "00:00");
  assert.equal(timeLabel(1800), "30:00");
});
test("sentence spans use code points and disambiguate repeated chunks", () => {
  const text = "😀 I know I can.";
  const t = sentenceTimeline(
    text,
    [
      { text: "I", role: "主语", explanation: "谁知道" },
      { text: "I", role: "从句主语", explanation: "谁可以" },
    ],
    "主干",
  );
  assert.equal(t.spans[0].start, 2);
  assert.equal(t.spans[1].start, 9);
  assert.equal(
    activeSegments(t, 1)
      .map((s) => s.text)
      .join(""),
    text,
  );
  assert.equal(
    activeSegments(t, 1)
      .filter((s) => s.marks.length)
      .map((s) => s.text)
      .join(""),
    "I",
  );
});
test("overlapping annotations preserve the exact original text", () => {
  const t = {
    text: "a😀bc",
    spans: [
      { id: "a", start: 0, end: 3 },
      { id: "b", start: 1, end: 4 },
    ],
    steps: [{ span_ids: ["a", "b"] }],
  };
  const parts = activeSegments(t, 0);
  assert.equal(parts.map((p) => p.text).join(""), t.text);
  assert.equal(parts.find((p) => p.text === "😀b").marks.length, 2);
});
