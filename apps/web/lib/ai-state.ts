export type PendingAI = { path: string; body: Record<string, unknown> };
export function parsePendingAI(raw: string | null): PendingAI | null {
  if (!raw) return null;
  try {
    const p = JSON.parse(raw);
    if (
      !p ||
      typeof p.path !== "string" ||
      !/^\/ai\/(conversations\/[A-Za-z0-9_-]{8,64}\/messages|training|training\/[A-Za-z0-9_-]{8,64}\/answers)$/.test(
        p.path,
      ) ||
      !p.body ||
      typeof p.body.request_id !== "string" ||
      !/^[A-Za-z0-9_-]{8,128}$/.test(p.body.request_id)
    )
      return null;
    if (
      p.path.endsWith("/messages") &&
      (typeof p.body.content !== "string" ||
        !Number.isSafeInteger(p.body.model_id))
    )
      return null;
    if (
      p.path === "/ai/training" &&
      (!["sentence", "listening"].includes(p.body.kind) ||
        typeof p.body.text !== "string" ||
        !Number.isSafeInteger(p.body.model_id))
    )
      return null;
    if (
      p.path.endsWith("/answers") &&
      (!Array.isArray(p.body.answers) ||
        !p.body.answers.length ||
        p.body.answers.length > 5 ||
        p.body.answers.some(
          (v: unknown) =>
            !Number.isInteger(v) || Number(v) < 0 || Number(v) > 3,
        ))
    )
      return null;
    return p;
  } catch {
    return null;
  }
}
