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
  kind: "chat" | "tts";
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
export async function authState(): Promise<{ user: User; csrf_token: string }> {
  const response = await fetch("/api/v1/auth/me", { cache: "no-store" });
  const data = await response.json();
  if (!response.ok) {
    csrf = "";
    throw new APIError(data.error || "请先登录", response.status);
  }
  csrf = data.csrf_token;
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
export async function api<T>(path: string, body?: object): Promise<T> {
  const response = await fetch(
    `/api/v1${path}`,
    body
      ? {
          method: "POST",
          headers: await headers(path !== "/auth/login"),
          body: JSON.stringify(body),
        }
      : { cache: "no-store" },
  );
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    if (response.status === 401 && path !== "/auth/login") {
      csrf = "";
      if (
        typeof window !== "undefined" &&
        window.location.pathname !== "/login"
      )
        window.location.assign("/login");
    }
    throw new APIError(data.error || "请求失败，请稍后重试", response.status);
  }
  return data as T;
}
export async function speechBlob(path: string, body: object): Promise<Blob> {
  const response = await fetch(`/api/v1${path}`, {
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
  const response = await fetch(`/api/v1${path}`, {
    method: "POST",
    headers: { "X-ELS-Client": "web", "X-CSRF-Token": csrf },
    body,
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok)
    throw new APIError(data.error || "上传失败", response.status);
  return data as T;
}
