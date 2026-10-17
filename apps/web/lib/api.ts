import {adoptPrivateOwner,broadcastAccountChange} from "./offline";
export type User = {
  id: string;
  username: string;
  display_name: string;
  role: "admin" | "learner";
  group_id: number;
  must_change_password: boolean;
  mfa_enabled: boolean;
};
export type Model = {
  id: number;
  name: string;
  model_id: string;
  kind: "chat" | "tts" | "asr";
  voices: string[];
  provider: string;
};
export type Preferences = {
  daily_new: number;
  retention: number;
  timezone: string;
  tts_model_id: number | null;
  voice: string;
};
let csrf = "";
export class APIError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message);
  }
}
export function setCSRF(value: string) {
  csrf = value;
}
async function fetchAPI(path: string, init?: RequestInit) {
  try {
    return await fetch(path, init);
  } catch {
    throw new APIError("学习服务暂时不可用，请稍后点击重试。", 503);
  }
}
export async function authState(): Promise<{ user: User; csrf_token: string }> {
  const response = await fetchAPI("/api/v1/auth/me", { cache: "no-store" });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    csrf = "";
    throw new APIError(
      data.error ||
        (response.status === 401
          ? "请先登录"
          : "学习服务暂时不可用，请稍后重试。"),
      response.status,
    );
  }
  csrf = data.csrf_token;
  await adoptPrivateOwner(data.user.id);
  return data;
}
async function headers(
  authenticated: boolean,
): Promise<Record<string, string>> {
  if (authenticated && !csrf) await authState();
  return {
    "Content-Type": "application/json",
    "X-ELS-Client": "web",
    ...(authenticated ? { "X-CSRF-Token": csrf } : {}),
  };
}
export async function api<T>(path: string, body?: object, method: "POST" | "PUT" | "PATCH" | "DELETE" = "POST"): Promise<T> {
  const response = await fetchAPI(
    `/api/v1${path}`,
    body
      ? {
          method,
          headers: await headers(
            !["/auth/login", "/auth/register"].includes(path),
          ),
          body: JSON.stringify(body),
        }
      : { cache: "no-store" },
  );
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    if (
      response.status === 401 &&
      !["/auth/login", "/auth/register"].includes(path)
    ) {
      csrf = "";
      if (
        typeof window !== "undefined" &&
        window.location.pathname !== "/login"
      )
        window.location.assign("/login");
    }
    throw new APIError(data.error || "请求失败，请稍后重试", response.status);
  }
  if(path==="/auth/login"||path==="/auth/register"){const state=await authState();broadcastAccountChange(state.user.id)}
  return data as T;
}
export async function speechBlob(path: string, body: object): Promise<Blob> {
  const response = await fetchAPI(`/api/v1${path}`, {
    method: "POST",
    headers: await headers(true),
    body: JSON.stringify(body),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({}));
    throw new APIError(data.error || "语音暂不可用", response.status);
  }
  return response.blob();
}
export function requestID(): string {
  return crypto.randomUUID();
}

export async function upload<T>(path: string, body: FormData): Promise<T> {
  if (!csrf) await authState();
  const response = await fetchAPI(`/api/v1${path}`, {
    method: "POST",
    headers: { "X-ELS-Client": "web", "X-CSRF-Token": csrf },
    body,
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok)
    throw new APIError(data.error || "上传失败", response.status);
  return data as T;
}

// Best effort flush on navigation/backgrounding. Never put CSRF or answers in a URL.
export function saveBeforeLeave(path: string, body: object): boolean {
  if (!csrf) return false;
  const raw = JSON.stringify(body);
  // Browsers impose a shared 64 KiB keepalive limit; ordinary autosave handles larger drafts.
  if (new TextEncoder().encode(raw).byteLength > 60000) return false;
  void fetch(`/api/v1${path}`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-ELS-Client": "web",
      "X-CSRF-Token": csrf,
    },
    body: raw,
    keepalive: true,
  }).catch(() => {});
  return true;
}
