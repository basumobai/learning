"use client";
import { useEffect, useRef, useState } from "react";
import { api, requestID, upload, type Model } from "../../lib/api";
export function SpeechTranscription({
  trainingId,
  taskIndex,
  onTranscript,
  recordingURL,
}: {
  trainingId: string;
  taskIndex: number;
  onTranscript: (text: string) => void;
  recordingURL?: string;
}) {
  const [models, setModels] = useState<Model[]>([]),
    [model, setModel] = useState(0),
    [file, setFile] = useState<File | null>(null),
    [busy, setBusy] = useState(false),
    [status, setStatus] = useState("");
  const key = useRef(requestID());
  useEffect(() => {
    void api<{ models: Model[] }>("/ai/models")
      .then((d) => {
        const ms = d.models.filter((m) => m.kind === "asr");
        setModels(ms);
        setModel(ms[0]?.id || 0);
      })
      .catch(() => {});
  }, []);
  useEffect(() => {
    key.current = requestID();
    setFile(null);
    setStatus("");
  }, [taskIndex, recordingURL]);
  return (
    <details className="evidence-card">
      <summary>保存录音并自动转写（可选）</summary>
      <p>录音上传后仅本人可访问。转写文本需要自己核对，不作为官方发音评分。</p>
      <label>
        ASR模型
        <select
          value={model}
          onChange={(e) => setModel(Number(e.target.value))}
        >
          {models.map((m) => (
            <option key={m.id} value={m.id}>
              {m.name}
            </option>
          ))}
        </select>
      </label>
      {!models.length && (
        <p>管理员尚未授权ASR模型，仍可录音回放、下载或手动填写。</p>
      )}
      <label>
        或选择音频（小于20MB）
        <input
          type="file"
          accept="audio/*"
          onChange={(e) => {
            setFile(e.target.files?.[0] || null);
            key.current = requestID();
          }}
        />
      </label>
      <button
        disabled={busy || !model || (!file && !recordingURL)}
        onClick={async () => {
          setBusy(true);
          setStatus("");
          try {
            const audio =
              file ||
              new File(
                [await (await fetch(recordingURL!)).blob()],
                "recording.webm",
              );
            const form = new FormData();
            form.append("file", audio);
            form.append("training_id", trainingId);
            form.append("task_index", String(taskIndex));
            form.append("request_id", key.current);
            const d = await upload<{ id: string }>(
              "/learning/recordings",
              form,
            );
            const result = await api<{ text: string; note: string }>(
              `/learning/recordings/${d.id}/transcribe`,
              { request_id: `asr-${key.current}`, model_id: model },
            );
            onTranscript(result.text);
            setStatus(result.note);
          } catch (e) {
            setStatus((e as Error).message);
          } finally {
            setBusy(false);
          }
        }}
      >
        {busy ? "正在保存并转写…" : "保存并转写到我的作答"}
      </button>
      <p role="status">{status}</p>
    </details>
  );
}
