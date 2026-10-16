"use client";
import { MathText } from "./MathText";
import { useEffect, useState } from "react";
import { activeSegments, type Timeline } from "../../lib/learning";
export function TimelinePlayer({
  timeline,
  assets = [],
  onStep,
}: {
  timeline: Timeline;
  assets?: { id: string; alt: string }[];
  onStep?: (id: string) => void;
}) {
  const [index, setIndex] = useState(0),
    [playing, setPlaying] = useState(false);
  const step = timeline.steps[index];
  useEffect(() => {
    setIndex(0);
    setPlaying(false);
  }, [timeline]);
  useEffect(() => {
    if (step) onStep?.(step.id);
  }, [step, onStep]);
  useEffect(() => {
    if (!playing) return;
    const timer = setInterval(
      () =>
        setIndex((i) => {
          if (i >= timeline.steps.length - 1) {
            setPlaying(false);
            return i;
          }
          return i + 1;
        }),
      4000,
    );
    return () => clearInterval(timer);
  }, [playing, timeline.steps.length]);
  if (!step) return null;
  return (
    <section className="timeline-player" aria-label="逐步动态讲解">
      <div className="timeline-toolbar">
        <div>
          <span className="eyebrow">
            拆开来看 · {index + 1}/{timeline.steps.length}
          </span>
          <h3>{step.title}</h3>
        </div>
        <button
          className="quiet"
          onClick={() => setPlaying((v) => !v)}
          aria-pressed={playing}
        >
          {playing ? "暂停播放" : "逐步播放"}
        </button>
      </div>
      <div className="timeline-source" aria-label="当前步骤的原文标注">
        {activeSegments(timeline, index).map((s) =>
          s.marks.length ? (
            <mark
              key={s.start}
              className={`highlight tone-${s.marks[0].tone}`}
              title={s.marks.map((m) => m.role).join("、")}
            >
              <span className="annotation-role">
                {s.marks.map((m) => m.role).join(" / ")}
              </span>
              <MathText text={s.text} />
            </mark>
          ) : (
            <span key={s.start}>
              <MathText text={s.text} />
            </span>
          ),
        )}
      </div>
      <div className="timeline-explanation" key={step.id}>
        <p>
          <MathText text={step.explanation} />
        </p>
        {assets
          .filter((a) => step.asset_ids.includes(a.id))
          .map((a) => (
            <figure className="active-figure" key={a.id}>
              <img src={`/api/v1/learning/assets/${a.id}`} alt={a.alt} />
              <figcaption>{a.alt}</figcaption>
            </figure>
          ))}
      </div>
      <div className="timeline-controls">
        <button
          className="quiet"
          onClick={() => {
            setPlaying(false);
            setIndex((i) => i - 1);
          }}
          disabled={index === 0}
        >
          上一步
        </button>
        <label className="timeline-scrubber">
          <span className="sr-only">选择讲解步骤</span>
          <input
            type="range"
            min={0}
            max={timeline.steps.length - 1}
            value={index}
            onChange={(e) => {
              setPlaying(false);
              setIndex(Number(e.target.value));
            }}
          />
        </label>
        <button
          className="quiet"
          onClick={() => {
            setPlaying(false);
            setIndex((i) => i + 1);
          }}
          disabled={index === timeline.steps.length - 1}
        >
          下一步
        </button>
      </div>
      <details>
        <summary>全部步骤与总结</summary>
        {timeline.steps.map((s, i) => (
          <p key={s.id}>
            <button
              className="text-button"
              onClick={() => {
                setIndex(i);
                setPlaying(false);
              }}
            >
              {i + 1}. {s.title}
            </button>
            ：<MathText text={s.explanation} />
          </p>
        ))}
        <p>
          <MathText text={timeline.summary} />
        </p>
      </details>
    </section>
  );
}
