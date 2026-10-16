"use client";
import { useEffect } from "react";

export function ViewportFrame() {
  useEffect(() => {
    const root = document.documentElement;
    const viewport = window.visualViewport;
    let width = window.innerWidth;
    let baseline = window.innerHeight;
    let frame = 0;
    const update = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        const height = viewport?.height ?? window.innerHeight;
        if (Math.abs(window.innerWidth - width) > 80) {
          width = window.innerWidth;
          baseline = window.innerHeight;
        }
        const focused = document.activeElement;
        const editing =
          focused instanceof HTMLElement &&
          focused.matches(
            'textarea,input:not([type="checkbox"]):not([type="radio"]):not([type="range"]):not([type="file"]):not([type="button"]),[contenteditable="true"]',
          );
        if (!editing) baseline = Math.max(window.innerHeight, height);
        const obscured = Math.max(
          baseline - height,
          window.innerHeight - height - (viewport?.offsetTop ?? 0),
        );
        root.dataset.keyboardOpen = String(
          editing &&
            window.matchMedia("(any-pointer: coarse)").matches &&
            obscured > 140,
        );
        root.style.setProperty("--visual-height", `${height}px`);
      });
    };
    update();
    window.addEventListener("resize", update);
    viewport?.addEventListener("resize", update);
    viewport?.addEventListener("scroll", update);
    document.addEventListener("focusin", update);
    document.addEventListener("focusout", update);
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener("resize", update);
      viewport?.removeEventListener("resize", update);
      viewport?.removeEventListener("scroll", update);
      document.removeEventListener("focusin", update);
      document.removeEventListener("focusout", update);
      delete root.dataset.keyboardOpen;
      root.style.removeProperty("--visual-height");
    };
  }, []);
  return null;
}
