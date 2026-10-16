import type { CatalogSubject } from "../../lib/curriculum";

export function ExamStructure({ subject }: { subject?: CatalogSubject }) {
  if (!subject?.exam) return null;
  const exam = subject.exam;
  return (
    <details className="panel" open>
      <summary>广东高考题型框架 · {exam.paper}</summary>
      <p>
        {exam.policy_year}年政策依据 · {exam.minutes}分钟 · 科目满分
        {exam.points}分
      </p>
      <div className="form-grid">
        {exam.sections.map((section) => (
          <article key={section.name}>
            <h3>{section.name}</h3>
            <p>
              {section.kinds
                .map((id) => subject.types.find((t) => t.id === id)?.name || id)
                .join("、")}
            </p>
            <p className="setting-note">{section.note}</p>
          </article>
        ))}
      </div>
      <p className="setting-note">{exam.note}</p>
      {exam.sources.slice(0, 1).map((url) => (
        <a href={url} key={url} target="_blank" rel="noreferrer">
          查看考试政策依据
        </a>
      ))}
    </details>
  );
}
