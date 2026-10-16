"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { api } from "../../lib/api";
import { useProtected, Loading, Navigation } from "../providers";
import { Icon } from "../icons";
type Entry = {
  id: number;
  word: string;
  phonetic: string;
  pos: string;
  chinese: string;
  suspended: boolean;
  failed_recalls: number;
};
export default function Vocabulary() {
  const auth = useProtected();
  const [entries, setEntries] = useState<Entry[]>([]);
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function load() {
    setBusy(true);
    try {
      const d = await api<{ entries: Entry[] }>(
        `/vocabulary?q=${encodeURIComponent(search)}&page=${page}`,
      );
      setEntries(d.entries);
      setError("");
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  useEffect(() => {
    if (auth.allowed) load();
  }, [auth.allowed, search, page]);
  async function suspend(entry: Entry) {
    setBusy(true);
    try {
      await api("/vocabulary/suspend", {
        sense_id: entry.id,
        suspended: !entry.suspended,
      });
      await load();
    } catch (e) {
      setError((e as Error).message);
      setBusy(false);
    }
  }
  if (!auth.allowed) return <Loading />;
  return (
    <main className="shell">
      <Navigation />
      <section className="intro" id="page-content" tabIndex={-1}>
        <span className="eyebrow">WORDS IN CONTEXT</span>
        <h1>自己的节奏，自己的词汇。</h1>
        <p>反复忘记的词先停一下，理解之后再恢复。暂停仅影响你的新一轮练习。</p>
      </section>
      <section className="panel glass">
        <form
          className="search-form"
          onSubmit={(e) => {
            e.preventDefault();
            setSearch(query);
            setPage(1);
          }}
        >
          <label className="sr-only" htmlFor="word-search">
            搜索单词或中文
          </label>
          <input
            id="word-search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="搜索单词或中文释义"
            maxLength={100}
          />
          <button className="primary" disabled={busy}>
            搜索
          </button>
        </form>
        {auth.user?.role === "admin" && (
          <p className="setting-note">
            <Link href="/admin">前往管理页导入四字段词库 →</Link>
          </p>
        )}
        <div className="word-list" aria-busy={busy}>
          {entries.map((entry) => (
            <article
              key={entry.id}
              className={entry.suspended ? "suspended" : ""}
            >
              <div>
                <h2 lang="en">
                  {entry.word} <small>{entry.phonetic}</small>
                  {auth.user?.role === "admin" && (
                    <small> · 词义 #{entry.id}</small>
                  )}
                </h2>
                <p>
                  {entry.pos} · {entry.chinese}
                </p>
                <span className="word-status">
                  {entry.suspended ? "已暂停" : "参与学习"}
                </span>
                <span className="setting-note">
                  {entry.failed_recalls > 0
                    ? ` · 未独立记住 ${entry.failed_recalls} 次`
                    : ""}
                  {entry.failed_recalls >= 8
                    ? " · 建议先用 AI 拆解例句，再继续复习"
                    : ""}
                </span>
              </div>
              <button
                className="quiet"
                disabled={busy}
                onClick={() => suspend(entry)}
              >
                {entry.suspended ? "恢复学习" : "暂停词条"}
              </button>
            </article>
          ))}
        </div>
        {!entries.length && (
          <div className="empty-state" role="status">
            <span className="feature-icon blue">
              <Icon name="library" />
            </span>
            <h2>{busy ? "正在读取词库…" : "暂时没有找到词条"}</h2>
            <p>
              {busy
                ? "你的词汇正在路上。"
                : search
                  ? "试试其他英文单词或中文释义。"
                  : "教材词汇由管理员导入，导入后就可以开始学习。"}
            </p>
          </div>
        )}
        <div className="actions pagination" aria-label="词库分页">
          <button
            className="quiet"
            disabled={page === 1 || busy}
            onClick={() => setPage(page - 1)}
          >
            上一页
          </button>
          <span>第 {page} 页</span>
          <button
            className="quiet"
            disabled={entries.length < 50 || busy}
            onClick={() => setPage(page + 1)}
          >
            下一页
          </button>
        </div>
      </section>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
    </main>
  );
}
