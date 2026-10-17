export type PendingSubmission = {
  client_request_id: string;
  answers: Record<string, string>;
  personal: boolean;
  model_selection?: {
    model_id: number;
    reviewer_ids: number[];
    lead_model_id: number;
  };
};
export type DraftCopy = {
  user_id: string;
  attempt_id: string;
  base_version: number;
  version: number;
  answers: Record<string, string>;
  pending_submission?: PendingSubmission;
};
const prefix = "learning-draft-v1:";
function object(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object" && !Array.isArray(value);
}
function answers(value: unknown): value is Record<string, string> {
  return (
    object(value) &&
    Object.keys(value).length <= 30 &&
    Object.entries(value).every(
      ([k, v]) =>
        k.length > 0 &&
        k.length <= 40 &&
        typeof v === "string" &&
        Array.from(v).length <= 12000,
    )
  );
}
export function parseDraft(
  raw: string | null,
  user: string,
  attempt: string,
): DraftCopy | null {
  if (!raw || raw.length > 160000) return null;
  try {
    const d = JSON.parse(raw);
    if (
      !object(d) ||
      Object.keys(d).some(
        (k) =>
          ![
            "user_id",
            "attempt_id",
            "base_version",
            "version",
            "answers",
            "pending_submission",
          ].includes(k),
      ) ||
      d.user_id !== user ||
      d.attempt_id !== attempt ||
      !Number.isSafeInteger(d.version) ||
      Number(d.version) < 1 ||
      !Number.isSafeInteger(d.base_version) ||
      Number(d.base_version) < 0 ||
      !answers(d.answers)
    )
      return null;
    const p = d.pending_submission;
    if (p !== undefined) {
      if (
        !object(p) ||
        Object.keys(p).some(
          (k) =>
            ![
              "client_request_id",
              "answers",
              "personal",
              "model_selection",
            ].includes(k),
        ) ||
        typeof p.client_request_id !== "string" ||
        !/^[A-Za-z0-9_-]{8,128}$/.test(p.client_request_id) ||
        !answers(p.answers) ||
        typeof p.personal !== "boolean"
      )
        return null;
      const m = p.model_selection;
      if (
        m !== undefined &&
        (!object(m) ||
          Object.keys(m).some(
            (k) => !["model_id", "reviewer_ids", "lead_model_id"].includes(k),
          ) ||
          !Number.isSafeInteger(m.model_id) ||
          !Number.isSafeInteger(m.lead_model_id) ||
          !Array.isArray(m.reviewer_ids) ||
          m.reviewer_ids.length > 5 ||
          m.reviewer_ids.some((i) => !Number.isSafeInteger(i) || i < 1))
      )
        return null;
    }
    return d as DraftCopy;
  } catch {
    return null;
  }
}
export function draftKey(user: string, attempt: string) {
  return prefix + encodeURIComponent(user) + ":" + encodeURIComponent(attempt);
}
export function readDraft(
  user: string,
  attempt: string,
  storage: Pick<Storage, "getItem"> = localStorage,
) {
  try {
    return parseDraft(storage.getItem(draftKey(user, attempt)), user, attempt);
  } catch {
    return null;
  }
}
export function writeDraft(
  draft: DraftCopy,
  storage: Pick<Storage, "setItem"> = localStorage,
) {
  try {
    const raw = JSON.stringify(draft);
    if (!parseDraft(raw, draft.user_id, draft.attempt_id)) return false;
    storage.setItem(draftKey(draft.user_id, draft.attempt_id), raw);
    return true;
  } catch {
    return false;
  }
}
export function removeDraft(user: string, attempt: string) {
  try {
    localStorage.removeItem(draftKey(user, attempt));
  } catch {}
}
export async function clearPrivateData() {
  if (typeof window === "undefined") return;
  for (const storage of [localStorage, sessionStorage]) {
    try {
      for (const key of Object.keys(storage)) {
        if (
          key.startsWith("els-") ||
          key.startsWith("learning-draft-") ||
          key.startsWith("learning-private-")
        )
          storage.removeItem(key);
      }
    } catch {}
  }
  if ("caches" in window) {
    for (const name of await caches.keys()) {
      if (name.startsWith("learning-private-")) await caches.delete(name);
    }
  }
}
export function broadcastAccountChange(user?: string) {
  if (typeof window === "undefined") return;
  try {
    const channel = new BroadcastChannel("learning-account-v1");
    channel.postMessage({ type: "account_changed", user_id: user });
    channel.close();
  } catch {}
}
export function sameAnswers(
  a: Record<string, string>,
  b: Record<string, string>,
) {
  const keys = Object.keys(a);
  return (
    keys.length === Object.keys(b).length && keys.every((k) => a[k] === b[k])
  );
}
export async function adoptPrivateOwner(user: string) {
  if (typeof window === "undefined") return;
  try {
    const previous = localStorage.getItem("learning-private-owner");
    if (previous && previous !== user) await clearPrivateData();
    localStorage.setItem("learning-private-owner", user);
  } catch {}
}
