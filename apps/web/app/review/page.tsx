"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { api, requestID } from "../../lib/api";
import { Loading, Navigation, useProtected } from "../providers";
type Item = {
  id: string;
  front: string;
  version: number;
  due: string;
  subject_id: string;
};
export default function Review() {
  const auth = useProtected();
  const [items, setItems] = useState<Item[]>([]),
    [item, setItem] = useState<Item | null>(null),
    [back, setBack] = useState(""),
    [intervals, setIntervals] = useState<Record<string, string>>({}),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [last, setLast] = useState<{ item: string; event: string } | null>(null),
    [hint, setHint] = useState(false);
  const load = () =>
    api<{ items: Item[] }>("/learning/review-items").then((d) =>
      setItems(d.items),
    );
  useEffect(() => {
    if (auth.allowed) void load().catch((e) => setError(e.message));
  }, [auth.allowed]);
  async function act(fn: () => Promise<void>) {
    setBusy(true);
    setError("");
    try {
      await fn();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  if (!auth.allowed) return <Loading />;
  return (
    <main className="shell">
      <Navigation />
      <div id="page-content" className="learning-home" tabIndex={-1}>
        <header className="page-intro">
          <h1>先回忆，再看答案。</h1>
          <p>自建卡用于概念、公式条件和短语。推理能力通过新题验证。</p>
          <Link href="/english">词汇 FSRS 复习</Link> ·{" "}
          <Link href="/progress">学习证据</Link>
        </header>
        {error && (
          <p role="alert" className="error">
            {error}
          </p>
        )}
        {item ? (
          <section className="panel glass">
            <h2>{item.front}</h2>
            {back ? (
              <>
                <p className="recall-answer">{back}</p>
                <label>
                  <input
                    type="checkbox"
                    checked={hint}
                    onChange={(e) => setHint(e.target.checked)}
                  />{" "}
                  回忆时用了提示
                </label>
                <div className="button-row">
                  {["重学", "困难", "记住了", "很轻松"].map((label, i) => (
                    <button
                      key={label}
                      disabled={busy || (hint && i !== 0)}
                      onClick={() =>
                        void act(async () => {
                          const d = await api<{ event_id: string }>(
                            `/learning/review-items/${item.id}/rate`,
                            {
                              request_id: requestID(),
                              version: item.version,
                              rating: i + 1,
                              result: i === 0 ? "wrong" : "correct",
                              used_hint: hint,
                            },
                          );
                          setLast({ item: item.id, event: d.event_id });
                          setItem(null);
                          setBack("");
                          await load();
                        })
                      }
                    >
                      {label}
                      <small>
                        {intervals[i + 1]
                          ? new Date(intervals[i + 1]).toLocaleString()
                          : ""}
                      </small>
                    </button>
                  ))}
                </div>
              </>
            ) : (
              <button
                className="primary"
                disabled={busy}
                onClick={() =>
                  void act(async () => {
                    const d = await api<{
                      back: string;
                      intervals: Record<string, string>;
                      version: number;
                    }>(`/learning/review-items/${item.id}/reveal`, {});
                    setBack(d.back);
                    setIntervals(d.intervals);
                    setItem({ ...item, version: d.version });
                  })
                }
              >
                已经尝试，查看答案
              </button>
            )}
            <button
              onClick={() => {
                setItem(null);
                setBack("");
              }}
            >
              暂时跳过
            </button>
          </section>
        ) : (
          <section className="panel glass">
            <h2>主动回忆卡</h2>
            {items.length ? (
              items.map((i) => (
                <article key={i.id} className="evidence-card">
                  <h3>{i.front}</h3>
                  <p>下次复习 {new Date(i.due).toLocaleString()}</p>
                  <button
                    disabled={busy}
                    onClick={() => {
                      setItem(i);
                      setBack("");
                      setHint(false);
                    }}
                  >
                    开始回忆
                  </button>
                </article>
              ))
            ) : (
              <p>交卷后可将简短概念加入回忆卡。</p>
            )}
          </section>
        )}
        {last && (
          <button
            disabled={busy}
            onClick={() =>
              void act(async () => {
                await api(`/learning/review-items/${last.item}/undo`, {
                  request_id: requestID(),
                  event_id: last.event,
                });
                setLast(null);
                await load();
              })
            }
          >
            撤销上一次复习
          </button>
        )}
      </div>
    </main>
  );
}
