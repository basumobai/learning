"use client";
import { useEffect, useId, useState } from "react";

// Keep the same children mounted when the viewport or disclosure changes.
export function AdaptivePanel({
  title,
  children,
  wideAt = 1000,
}: {
  title: string;
  children: React.ReactNode;
  wideAt?: number;
}) {
  const id = useId();
  const [wide, setWide] = useState(false);
  const [choice, setChoice] = useState<boolean | null>(null);
  useEffect(() => {
    const media = window.matchMedia(`(min-width: ${wideAt}px)`);
    const update = () => setWide(media.matches);
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, [wideAt]);
  const expanded = choice ?? wide;
  return (
    <div className="adaptive-panel">
      <button
        type="button"
        className="adaptive-toggle"
        aria-expanded={expanded}
        aria-controls={id}
        onClick={() => setChoice(!expanded)}
      >
        <span>{title}</span>
        <span aria-hidden="true">{expanded ? "−" : "＋"}</span>
      </button>
      <div id={id} hidden={!expanded} className="adaptive-content">
        {children}
      </div>
    </div>
  );
}
