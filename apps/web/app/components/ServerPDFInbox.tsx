"use client";
import { useEffect, useState } from "react";
import { api } from "../../lib/api";
export function ServerPDFInbox({
  password,
  onImported,
}: {
  password: string;
  onImported: () => Promise<void>;
}) {
  const [files, setFiles] = useState<
    { name: string; size: number; importable: boolean }[]
  >([]);
  const [directory, setDirectory] = useState("./data/pdf");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  async function scan() {
    try {
      const d = await api<{ files: typeof files; host_directory: string }>(
        "/admin/learning/local-files",
      );
      if (!Array.isArray(d.files))
        throw new Error("资料目录暂时不可读，请稍后刷新。");
      setFiles(d.files);
      setDirectory(d.host_directory);
      setMessage(
        d.files.length
          ? "目录已刷新，选择题目与答案文件。"
          : "目录内还没有PDF，请放入文件后刷新。",
      );
    } catch (e) {
      setMessage((e as Error).message);
    }
  }
  useEffect(() => {
    void scan();
  }, []);
  async function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const data = Object.fromEntries(new FormData(e.currentTarget).entries());
    setBusy(true);
    try {
      await api("/admin/learning/local-import", {
        ...data,

      });
      await onImported();
      setMessage(
        "PDF 已接收：文本提取 → 扫描页OCR → 学科与教材归类。请在下方选择资料核对版本与册次。",
      );
    } catch (e) {
      setMessage((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="panel glass">
      <h2>从服务器目录导入 PDF</h2>
      <p>
        把 PDF 放入部署目录下的 <code>data/pdf/</code>。你的部署对应{" "}
        <code>/www/wwwroot/learning/learning/data/pdf/</code>。可在配置中用
        LEARNING_PDF_DIR 指定其他目录。
      </p>
      <p>
        当前挂载来源：<code>{directory}</code>
        。只读取文件，导入后保存快照；源文件更新后需重新导入。
      </p>
      <button
        type="button"
        className="quiet"
        onClick={() => void scan()}
        disabled={busy}
      >
        刷新目录
      </button>
      <form className="form-stack" onSubmit={submit}>
        <div className="form-grid">
          <label>
            资料标题
            <input name="title" required maxLength={200} />
          </label>
          <label>
            科目
            <select name="subject_id">
              <option value="">自动识别学科与教材（推荐）</option>
              {[
                ["english", "英语"],
                ["chinese", "语文"],
                ["math", "数学"],
                ["physics", "物理"],
                ["chemistry", "化学"],
                ["biology", "生物"],
              ].map(([id, name]) => (
                <option key={id} value={id}>
                  {name}
                </option>
              ))}
            </select>
          </label>
          <label>
            教材／册次
            <input
              name="textbook"
              placeholder="自动识别可留空；英语固定北师大版2019"
            />
          </label>
          <label>
            学期
            <input name="term" placeholder="高二上" />
          </label>
          {[
            ["question_path", "题目 PDF"],
            ["answer_path", "答案 PDF"],
          ].map(([key, label]) => (
            <label key={key}>
              {label}
              <select name={key} aria-label={label} required>
                <option value="">选择文件</option>
                {files.map((f) => (
                  <option key={f.name} value={f.name} disabled={!f.importable}>
                    {f.name} · {(f.size / 1024 / 1024).toFixed(1)}MB
                    {!f.importable ? "（超过32MB）" : ""}
                  </option>
                ))}
              </select>
            </label>
          ))}
        </div>
        <p className="setting-note">
          答案在同一 PDF
          时，可两项选择同一文件，再核对题目和答案物理页码。扫描件请先为“OCR”任务配置视觉模型。
        </p>
        <button
          className="primary"
          disabled={busy || !files.length}
        >
          {busy ? "正在导入…" : "导入所选 PDF"}
        </button>
      </form>
      <p role="status">{message}</p>
    </section>
  );
}
export function DocumentOCR({
  id,
  password,
}: {
  id: string;
  password: string;
}) {
  const [pages, setPages] = useState<
    {
      kind: string;
      page: number;
      text: string;
      source: string;
      notes: string[] | null;
    }[]
  >([]);
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  async function load() {
    try {
      const d = await api<{ pages: typeof pages }>(
        `/admin/learning/documents/${id}/ocr`,
      );
      setPages(d.pages);
      setMessage("");
    } catch (e) {
      setMessage((e as Error).message);
    }
  }
  useEffect(() => {
    void load();
  }, [id]);
  return (
    <details className="panel">
      <summary>
        OCR 识别与逐页核对 · {pages.filter((p) => p.source === "ocr").length}
        页已OCR · {pages.filter((p) => p.source === "pending").length}页待识别
      </summary>
      <p>
        原生文本直接提取，扫描页由独立OCR模型识别。目录识别任务会自动处理待识别页；也可单独补跑，完成后重试失败的目录任务。
      </p>
      <button className="quiet" type="button" onClick={() => void load()}>
        刷新文字预览
      </button>
      <button
        type="button"
        className="quiet"
        disabled={busy}
        onClick={async () => {
          setBusy(true);
          try {
            await api(`/admin/learning/documents/${id}/ocr`, {

            });
            setMessage("OCR已排队，已完成页会保留，稍后刷新预览。");
          } catch (e) {
            setMessage((e as Error).message);
          } finally {
            setBusy(false);
          }
        }}
      >
        识别待处理扫描页
      </button>
      {message && <p role="status">{message}</p>}
      {pages.map((p) => (
        <details key={`${p.kind}-${p.page}`}>
          <summary>
            {p.kind === "question" ? "题目" : "答案"} · 第{p.page}页 ·{" "}
            {p.source === "ocr"
              ? "OCR已识别"
              : p.source === "pending"
                ? "待OCR"
                : "原生文本"}
          </summary>
          <a
            href={`/api/v1/admin/learning/documents/${id}/pages/${p.kind}/${p.page}`}
            target="_blank"
            rel="noreferrer"
          >
            打开原页核对
          </a>
          <pre className="preserve-text">{p.text || "未提取到文字"}</pre>
          {p.notes?.map((n, i) => (
            <p className="notice" key={i}>
              {n}
            </p>
          ))}
        </details>
      ))}
    </details>
  );
}
