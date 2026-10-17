"use client";
import Link from "next/link";
import { useEffect, useState } from "react";
import { api } from "../lib/api";
import { subjects } from "../lib/learning";
import { Loading, Navigation, useProtected } from "./providers";
export default function LearningHome() {
  const auth = useProtected();
  const [counts, setCounts] = useState<Record<string, number>>({});
  useEffect(() => {
    if (auth.allowed)
      api<{ chapters: { subject_id: string; question_count: number }[] }>(
        "/learning/catalog",
      )
        .then((d) => {
          const next: Record<string, number> = {};
          for (const c of d.chapters)
            next[c.subject_id] =
              (next[c.subject_id] || 0) + Number(c.question_count);
          setCounts(next);
        })
        .catch(() => {});
  }, [auth.allowed]);
  if (!auth.allowed) return <Loading />;
  return (
    <main className="shell">
      <Navigation />
      <div id="page-content" tabIndex={-1} className="learning-home">
        <header className="learning-hero">
          <span className="eyebrow">LEARNING · 广东高中</span>
          <h1>
            从好奇开始，
            <br />
            把知识学明白。
          </h1>
          <p>选择今天想学的科目。练一道题，弄懂每一步。</p>
        </header>
        <section className="subject-grid" aria-label="选择学习科目">
          {subjects.map((s) => (
            <Link
              className={`subject-card subject-${s.tone}`}
              href={`/practice?subject=${s.id}`}
              key={s.id}
            >
              <span className="subject-symbol" aria-hidden="true">
                {s.symbol}
              </span>
              <div>
                <h2>{s.name}</h2>
                <p>{s.note}</p>
              </div>
              <span className="subject-count">
                {counts[s.id] || 0} 道已审核题目{" "}
                <span aria-hidden="true">↗</span>
              </span>
            </Link>
          ))}
        </section>
        <section className="learning-shortcuts">
 <Link className="panel glass" href="/exams"><span className="eyebrow">按年份与规则</span><h2>整卷模拟</h2><p>服务端计时、恢复草稿与考后复盘。</p><span>查看可用规则 →</span></Link>
 <Link className="panel glass" href="/progress"><span className="eyebrow">今天与下一步</span><h2>计划、错题与学习证据</h2><p>独立首答、提示和新题验证分别回看。</p><span>查看下一步 →</span></Link>
          <Link className="panel glass" href="/english">
            <span className="eyebrow">ENGLISH · 每日积累</span>
            <h2>单词，记得更牢。</h2>
            <p>继续词汇复习、拼写与听音训练。</p>
            <span>开始复习 →</span>
          </Link>
          <Link className="panel glass" href="/challenge">
            <span className="eyebrow">6 秒 / 3 秒</span>
            <h2>极速四选一</h2>
            <p>英选中、中选英。普通、困难、绝望。</p>
            <span>进入挑战 →</span>
          </Link>
        </section>
        {auth.user?.role === "admin" && (
          <p className="setting-note">
            准备教学内容：
            <Link href="/admin/learning">导入 PDF、审核题目、配置 AI</Link>
          </p>
        )}
      </div>
    </main>
  );
}
