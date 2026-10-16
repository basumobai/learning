"use client";
import { useRef, useState } from "react";
export function LearningFigure({
  src,
  alt,
  active = false,
}: {
  src: string;
  alt: string;
  active?: boolean;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const [zoom, setZoom] = useState(1);
  return (
    <figure className={active ? "active-figure" : "question-figure"}>
      <button
        ref={trigger}
        type="button"
        className="figure-open"
        aria-label={`放大查看：${alt}`}
        onClick={() => {
          setZoom(1);
          dialog.current?.showModal();
        }}
      >
        <img src={src} alt={alt} />
        <span>点按放大</span>
      </button>
      <figcaption>{alt}</figcaption>
      <dialog
        ref={dialog}
        className="figure-dialog"
        aria-label={`插图：${alt}`}
        onClose={() => trigger.current?.focus({ preventScroll: true })}
      >
        <div className="figure-toolbar">
          <strong>{alt}</strong>
          <label>
            放大倍率
            <input
              type="range"
              min={1}
              max={3}
              step={0.25}
              value={zoom}
              onChange={(e) => setZoom(Number(e.target.value))}
            />
            <output>{Math.round(zoom * 100)}%</output>
          </label>
          <button
            type="button"
            className="quiet"
            onClick={() => dialog.current?.close()}
          >
            关闭插图
          </button>
        </div>
        <div
          className="figure-canvas"
          tabIndex={0}
          role="region"
          aria-label="插图，可滚动查看"
        >
          <img src={src} alt={alt} style={{ width: `${zoom * 100}%` }} />
        </div>
      </dialog>
    </figure>
  );
}
