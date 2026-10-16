import { mathSegments } from "../../lib/math";
import "katex/dist/katex.min.css";
export function MathText({ text }: { text: string }) {
  return (
    <span className="math-text">
      {mathSegments(text).map((s, i) =>
        s.html ? (
          <span
            className={s.display ? "math-display" : undefined}
            key={i}
            dangerouslySetInnerHTML={{ __html: s.html }}
          />
        ) : (
          <span key={i}>{s.text}</span>
        ),
      )}
    </span>
  );
}
