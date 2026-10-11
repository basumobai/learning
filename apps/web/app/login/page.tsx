"use client";
import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api } from "../../lib/api";
import { useAuth } from "../providers";

export default function Login() {
  const auth = useAuth();
  const router = useRouter();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (auth.user)
      router.replace(auth.user.must_change_password ? "/settings" : "/");
  }, [auth.user, router]);
  async function login(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api("/auth/login", { username, password, code });
      setPassword("");
      setCode("");
      await auth.refresh();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <main className="shell login-shell">
      <div className="brand">
        <span className="mark">E</span>ELS · 英语助学
      </div>
      <section className="panel glass">
        <span className="eyebrow">LEARN WITH UNDERSTANDING</span>
        <h1>从读懂一句话开始。</h1>
        <p>词汇复习、听音拼写与 AI 辅导，把零散知识连起来。</p>
        <form className="form-stack" onSubmit={login}>
          <label>
            用户名
            <input
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoComplete="username"
              required
              maxLength={40}
            />
          </label>
          <label>
            密码
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="current-password"
              required
              maxLength={128}
            />
          </label>
          <label>
            两步验证码（已开启时填写）
            <input
              value={code}
              onChange={(e) => setCode(e.target.value.replace(/\D/g, ""))}
              inputMode="numeric"
              autoComplete="one-time-code"
              maxLength={6}
            />
          </label>
          <button className="primary" disabled={busy}>
            {busy ? "登录中…" : "进入学习"}
          </button>
        </form>
        {error && (
          <p role="alert" className="error">
            {error}
          </p>
        )}
        <p className="setting-note">
          账户由管理员创建。忘记密码或丢失验证器时，请联系管理员重置。
        </p>
      </section>
    </main>
  );
}
