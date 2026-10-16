"use client";
import { useEffect, useRef, useState } from "react";
import { api } from "../../lib/api";
import { subjects } from "../../lib/learning";
import type { CatalogSubject, Recognition } from "../../lib/curriculum";

export function DocumentScope({
  id,
  recognition,
  password,
  pending,
  onConfirmed,
}: {
  id: string;
  recognition?: Recognition | null;
  password: string;
  pending: boolean;
  onConfirmed: () => Promise<void>;
}) {
  const [catalog, setCatalog] = useState<CatalogSubject[]>([]);
  const [subject, setSubject] = useState("");
  const [edition, setEdition] = useState("");
  const [volume, setVolume] = useState("");
  const [term, setTerm] = useState("");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const touched = useRef(false);
  useEffect(() => {
    api<{ subjects: CatalogSubject[] }>("/learning/catalog")
      .then((c) => {
        if (Array.isArray(c.subjects)) setCatalog(c.subjects);
      })
      .catch((e) => setMessage(e.message));
  }, []);
  useEffect(() => {
    if (!recognition || touched.current) return;
    if (subjects.some((s) => s.id === recognition.subject_id))
      setSubject(recognition.subject_id);
    setEdition(recognition.edition);
    setVolume(recognition.volume);
  }, [recognition]);
  const profile = catalog.find((s) => s.id === subject)?.curriculum;
  const confirmedEdition = profile?.edition_locked ? profile.edition : edition;
  async function confirm(e: React.FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setMessage("");
    try {
      await api(`/admin/learning/documents/${id}/scope`, {
        subject_id: subject,
        edition: confirmedEdition,
        volume,
        term,
        admin_password: password,
      });
      await onConfirmed();
    } catch (e) {
      setMessage((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="panel" aria-label="PDF教材归类确认">
      <h3>确认 AI 识别的教材范围</h3>
      <p>
        识别结果只是建议。确认学科、版本与册次后，才开始识别教材章节；题目仍需审核发布。混合学科建议先拆分PDF，跨册可选综合复习。
      </p>
      {recognition ? (
        <>
          <p>
            建议：
            {subjects.find((s) => s.id === recognition.subject_id)?.name ||
              (recognition.subject_id === "mixed"
                ? "跨学科资料"
                : "学科待确认")}{" "}
            · {recognition.edition || "版本不确定"} ·{" "}
            {recognition.volume || "册次不确定"}
          </p>
          <p>
            识别把握：
            {{ high: "较高", medium: "一般", low: "较低" }[
              recognition.confidence
            ] || "待核对"}
            （模型自述，仍需核对）
          </p>
          <p>{recognition.notes}</p>
          {recognition.edition_conflict && (
            <p className="error" role="alert">
              英语教材版本冲突：不能把其他版本直接标成北师大版2019，请核对原PDF。
            </p>
          )}
          {recognition.evidence?.map((e, i) => (
            <p key={i}>
              <a
                target="_blank"
                rel="noreferrer"
                href={`/api/v1/admin/learning/documents/${id}/pages/question/${e.page}`}
              >
                原PDF第{e.page}页
              </a>
              ：{e.quote}
            </p>
          ))}
        </>
      ) : (
        <p>
          {pending
            ? "正在读取PDF并识别学科与教材…"
            : "暂无可用识别建议，可核对封面与目录后人工确认。"}
        </p>
      )}
      <form onSubmit={confirm} className="form-stack">
        <div className="form-grid">
          <label>
            确认学科
            <select
              required
              value={subject}
              onChange={(e) => {
                touched.current = true;
                setSubject(e.target.value);
                setEdition("");
                setVolume("");
              }}
            >
              <option value="">请选择学科</option>
              {subjects.map((s) => (
                <option value={s.id} key={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            确认教材版本
            <input
              required
              value={confirmedEdition}
              disabled={!!profile?.edition_locked}
              maxLength={80}
              placeholder="如人教A版2019、粤教版2019；不确定请核对封面"
              onChange={(e) => {
                touched.current = true;
                setEdition(e.target.value);
              }}
            />
          </label>
          <label>
            确认教材册次
            {subject === "english" ? (
              <select
                required
                value={volume}
                onChange={(e) => {
                  touched.current = true;
                  setVolume(e.target.value);
                }}
              >
                <option value="">请选择册次</option>
                {profile?.volumes.map((v) => (
                  <option key={v}>{v}</option>
                ))}
              </select>
            ) : (
              <>
                <input
                  required
                  list={`volumes-${id}`}
                  maxLength={80}
                  value={volume}
                  onChange={(e) => {
                    touched.current = true;
                    setVolume(e.target.value);
                  }}
                  placeholder="按封面填写；跨册用综合复习"
                />
                <datalist id={`volumes-${id}`}>
                  {profile?.volumes.map((v) => (
                    <option key={v} value={v} />
                  ))}
                </datalist>
              </>
            )}
          </label>
          <label>
            学期（可选）
            <input
              value={term}
              onChange={(e) => setTerm(e.target.value)}
              maxLength={100}
              placeholder="册次与学期分别保存"
            />
          </label>
        </div>
        {profile && <p className="setting-note">{profile.note}</p>}
        <label className="check-label">
          <input type="checkbox" required />
          <span>我已核对封面、版本和册次，确认当前资料的归属。</span>
        </label>
        <button
          className="primary"
          disabled={
            busy ||
            pending ||
            !password ||
            !profile ||
            (!!recognition?.edition_conflict && subject === "english")
          }
        >
          确认范围并识别章节
        </button>
        {message && (
          <p role="alert" className="error">
            {message}
          </p>
        )}
      </form>
    </section>
  );
}
