"use client";
import { useEffect, useState } from "react";
import { api, requestID } from "../../lib/api";
type Candidate = {
  id: string;
  type_id: string;
  question_quote: string;
  answer_quote: string;
  uncertainty: string;
};
type Event = { version: number; action: string; type_id: string; note: string };
type Data = {
  types: { id: string; name: string }[];
  candidates: Candidate[];
  events: Event[];
};
export function MistakePanel({
  attemptId,
  subject,
}: {
  attemptId: string;
  subject?: string;
}) {
  const [data, setData] = useState<Data | null>(null),
    [type, setType] = useState(""),
    [note, setNote] = useState(""),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [notice, setNotice] = useState("");
  const load = () =>
    api<Data>(`/learning/attempts/${attemptId}/mistakes`).then((d) => {
      if (
        !Array.isArray(d.types) ||
        !Array.isArray(d.events) ||
        !Array.isArray(d.candidates)
      )
        throw new Error("错因记录暂不可用，请稍后刷新");
      setData(d);
      setType((v) =>
        d.types.some((t) => t.id === v) ? v : d.types[0]?.id || "",
      );
    });
  useEffect(() => {
    setData(null);
    setError("");
    void load().catch((e) => setError(e.message));
  }, [attemptId]);
  async function record(action: string, candidate?: Candidate) {
    setBusy(true);
    setError("");
    try {
      await api(`/learning/attempts/${attemptId}/mistakes`, {
        request_id: requestID(),
        expected_version: data?.events.at(-1)?.version || 0,
        action,
        type_id: candidate?.type_id || type,
        candidate_id: candidate?.id || "",
        note,
      });
      await load();
      setNotice(
        action === "understood"
          ? "已记录理解；新题表现会另外记录。"
          : "错因已保存，可随时更正。",
      );
    } catch (e) {
      setError((e as Error).message);
      void load();
    } finally {
      setBusy(false);
    }
  }
  async function train(kind: "retry" | "transfer") {
    setBusy(true);
    setError("");
    try {
      const out = await api<{ id: string }>(
        `/learning/attempts/${attemptId}/${kind}`,
        { request_id: requestID() },
      );
      window.location.assign(`/practice?attempt=${encodeURIComponent(out.id)}`);
    } catch (e) {
      setError((e as Error).message);
      setBusy(false);
    }
  }
  return (
    <section className="panel glass evidence-panel">
      <h3>这次卡在哪里？</h3>
      {subject === "english" && (
        <a href={`/assistant?source_attempt=${attemptId}#sentence`}>
          从本题原句练主干与修饰
        </a>
      )}
      <p className="setting-note">
        AI 候选需要你确认。原题做对与新题验证分别记录。
      </p>
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
      {notice && <p role="status">{notice}</p>}
      {data?.candidates.map((c) => (
        <div className="evidence-card" key={c.id}>
          <strong>{data.types.find((t) => t.id === c.type_id)?.name}</strong>
          <blockquote>{c.question_quote}</blockquote>
          <p>你的作答：{c.answer_quote || "未作答"}</p>
          <p>{c.uncertainty}</p>
          <button disabled={busy} onClick={() => void record("confirm", c)}>
            符合我的情况
          </button>
        </div>
      ))}
      {data && (
        <>
          <label>
            选择或更正错因
            <select value={type} onChange={(e) => setType(e.target.value)}>
              {data.types.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            补充说明（选填）
            <input
              maxLength={1000}
              value={note}
              onChange={(e) => setNote(e.target.value)}
            />
          </label>
          <div className="button-row">
            <button
              disabled={busy || !type}
              onClick={() => void record("correct")}
            >
              记录 / 更正
            </button>
            <button
              disabled={busy || !type}
              onClick={() => void record("unsure")}
            >
              暂时不确定
            </button>
            <button
              disabled={busy || !type}
              onClick={() => void record("understood")}
            >
              我已理解
            </button>
          </div>
          {data.events.length > 0 && (
            <details>
              <summary>查看更正记录（{data.events.length}）</summary>
              {data.events.map((e) => (
                <p key={e.version}>
                  {e.version}.{" "}
                  {data.types.find((t) => t.id === e.type_id)?.name} ·{" "}
                  {e.action} {e.note}
                </p>
              ))}
            </details>
          )}
        </>
      )}
      <div className="button-row">
        <button disabled={busy} onClick={() => void train("retry")}>
          原题再练
        </button>
        <button
          className="primary"
          disabled={busy}
          onClick={() => void train("transfer")}
        >
          用新题验证
        </button>
      </div>
    </section>
  );
}
