"use client";
import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { api, requestID } from "../../lib/api";
import { Loading, Navigation, useProtected } from "../providers";
type Round = {
  id: string;
  prompt: string;
  direction: string;
  difficulty: string;
  choices: { id: string; text: string; pos?: string }[];
  deadline: string;
  server_time: string;
  answered_at: string | null;
  correct?: boolean;
  correct_id?: string;
  picked?: string;
};
export default function Challenge() {
  const auth = useProtected();
  const [direction, setDirection] = useState("en_zh"),
    [difficulty, setDifficulty] = useState("normal"),
    [round, setRound] = useState<Round | null>(null),
    [running, setRunning] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [left, setLeft] = useState(6),
    [score, setScore] = useState({ correct: 0, total: 0 });
  const roundRef = useRef(round),
    runningRef = useRef(running),
    busyRef = useRef(false),
    offset = useRef(0),
    pendingNext = useRef(""),
    answered = useRef(new Set<string>()),
    nextTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  roundRef.current = round;
  runningRef.current = running;
  const next = useCallback(async () => {
    if (busyRef.current) return;
    busyRef.current = true;
    setBusy(true);
    setError("");
    try {
      if (!pendingNext.current) pendingNext.current = requestID();
      const next = await api<Round>("/learning/words/next", {
        direction,
        difficulty,
        request_id: pendingNext.current,
      });
      pendingNext.current = "";
      offset.current = Date.parse(next.server_time) - Date.now();
      roundRef.current = next;
      setRound(next);
      setLeft(
        Math.max(
          0,
          (Date.parse(next.deadline) - Date.now() - offset.current) / 1000,
        ),
      );
    } catch (e) {
      setError((e as Error).message);
      setRunning(false);
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  }, [direction, difficulty]);
  const answer = useCallback(
    async (picked: string) => {
      const r = roundRef.current;
      if (!r || r.answered_at || busyRef.current) return;
      busyRef.current = true;
      setBusy(true);
      setError("");
      try {
        const result = await api<Round>(`/learning/words/${r.id}/answer`, {
          picked,
        });
        setRound(result);
        roundRef.current = result;
        if (!answered.current.has(r.id)) {
          answered.current.add(r.id);
          setScore((v) => ({
            correct: v.correct + Number(!!result.correct),
            total: v.total + 1,
          }));
        }
        if (runningRef.current)
          nextTimer.current = setTimeout(() => {
            if (runningRef.current) void next();
          }, 500);
      } catch (e) {
        setError((e as Error).message);
        setRunning(false);
      } finally {
        busyRef.current = false;
        setBusy(false);
      }
    },
    [next],
  );
  useEffect(
    () => () => {
      if (nextTimer.current) clearTimeout(nextTimer.current);
    },
    [],
  );
  useEffect(() => {
    if (!round || round.answered_at || !running) return;
    const tick = () => {
      const remain =
        (Date.parse(round.deadline) - Date.now() - offset.current) / 1000;
      setLeft(Math.max(0, remain));
      if (remain <= 0 && !busyRef.current) void answer("");
    };
    tick();
    const timer = setInterval(tick, 50);
    return () => clearInterval(timer);
  }, [round?.id, round?.answered_at, running, answer]);
  useEffect(() => {
    function key(e: KeyboardEvent) {
      if (
        !running ||
        busy ||
        e.repeat ||
        e.ctrlKey ||
        e.metaKey ||
        e.altKey ||
        e.target instanceof HTMLInputElement ||
        e.target instanceof HTMLSelectElement
      )
        return;
      const i = Number(e.key) - 1;
      if (i >= 0 && i < 4 && round?.choices[i]) {
        e.preventDefault();
        void answer(round.choices[i].id);
      }
    }
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  }, [running, busy, round, answer]);
  if (!auth.allowed) return <Loading />;
  const limit = (round?.difficulty || difficulty) === "normal" ? 6 : 3;
  return (
    <main className="shell">
      <Navigation />
      <div id="page-content" className="challenge-layout">
        <header className="page-intro">
          <span className="eyebrow">ENGLISH · QUICK MATCH</span>
          <h1>一眼辨词，一刻作答。</h1>
          <p>四个选项，同一段倒计时。选项位置每题随机。</p>
        </header>
        <section className="panel glass challenge-settings">
          <label>
            方向
            <select
              value={direction}
              disabled={running || busy}
              onChange={(e) => setDirection(e.target.value)}
            >
              <option value="en_zh">看英文，选中文</option>
              <option value="zh_en">看中文，选英文</option>
            </select>
          </label>
          <fieldset className="difficulty-picker">
            <legend>选择难度</legend>
            {[
              ["normal", "普通", "6秒 · 四项差异明显"],
              ["hard", "困难", "3秒 · 两项近似，正确项在其中"],
              ["despair", "绝望", "3秒 · 四项近似，附词性"],
            ].map(([id, name, note]) => (
              <label key={id} className={difficulty === id ? "selected" : ""}>
                <input
                  type="radio"
                  name="difficulty"
                  value={id}
                  checked={difficulty === id}
                  disabled={running || busy}
                  onChange={() => setDifficulty(id)}
                />
                <strong>{name}</strong>
                <span>{note}</span>
              </label>
            ))}
          </fieldset>
          <div className="question-submit">
            <button
              className="primary"
              disabled={running || busy}
              onClick={() => {
                setRunning(true);
                runningRef.current = true;
                void next();
              }}
            >
              {busy
                ? "正在准备…"
                : round && !round.answered_at
                  ? "继续挑战"
                  : "开始挑战"}
            </button>
            {running && (
              <button
                className="quiet"
                onClick={() => {
                  setRunning(false);
                  runningRef.current = false;
                  if (nextTimer.current) clearTimeout(nextTimer.current);
                }}
              >
                停止连做
              </button>
            )}
            <span className="challenge-score">
              本轮 {score.correct}/{score.total} 正确
            </span>
          </div>
        </section>
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        {round && (
          <section
            className={`panel glass challenge-question difficulty-${round.difficulty}`}
          >
            <div className="challenge-clock" aria-label="剩余秒数">
              <strong>{round.answered_at ? "—" : left.toFixed(1)}</strong>
              <span>秒</span>
            </div>
            <div
              className="challenge-progress"
              role="progressbar"
              aria-label="作答剩余时间"
              aria-valuemin={0}
              aria-valuemax={limit}
              aria-valuenow={Number(left.toFixed(1))}
            >
              <span
                style={{ transform: `scaleX(${Math.min(1, left / limit)})` }}
              />
            </div>
            <h2>{round.prompt}</h2>
            <div className="challenge-options">
              {round.choices.map((c, i) => (
                <button
                  key={c.id}
                  className={`${round.answered_at && c.id === round.correct_id ? "choice-correct" : ""} ${round.answered_at && c.id === round.picked && !round.correct ? "choice-wrong" : ""}`}
                  disabled={busy || !!round.answered_at || !running}
                  onClick={() => void answer(c.id)}
                >
                  <kbd>{i + 1}</kbd>
                  <span>
                    {c.pos && <small>{c.pos}</small>}
                    {c.text}
                  </span>
                  {round.answered_at && c.id === round.correct_id && (
                    <b aria-label="正确答案">✓</b>
                  )}
                </button>
              ))}
            </div>
            {round.answered_at && (
              <p className="challenge-verdict" role="status">
                {round.correct
                  ? "答对了。"
                  : round.picked
                    ? "这次选错了，记住标出的答案。"
                    : "时间到了，记住标出的答案。"}
              </p>
            )}
          </section>
        )}
        <p className="setting-note">
          支持键盘
          1–4。计时以服务端为准，切换页面不会延长时间。近似选项由管理员审核，只有一个正确答案。
        </p>
        <p>
          <Link href="/english">返回词汇复习 →</Link>
        </p>
      </div>
    </main>
  );
}
