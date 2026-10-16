"use client";
import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api } from "../../lib/api";
import { useAuth } from "../providers";
import { Icon } from "../icons";

export default function Login() {
  const auth = useAuth();
  const router = useRouter();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [showPassword, setShowPassword] = useState(false);
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
      <section className="login-story" aria-label="关于 Learning">
        <div className="brand">
          <span className="mark">
            <Icon name="book" />
          </span>
          <span>
            Learning<span className="brand-detail">学习平台</span>
          </span>
        </div>
        <span className="eyebrow">A LITTLE EVERY DAY</span>
        <h2>
          从记住一个词，
          <br />
          到读懂整个世界。
        </h2>
        <p>
          把零散的知识连起来。复习、理解、表达，
          <br className="desktop-break" />
          每一步都有自己的节奏。
        </p>
        <div className="login-features">
          <div>
            <span className="feature-icon blue">
              <Icon name="book" />
            </span>
            <span>
              <strong>词汇复习</strong>
              <small>先回忆，再核对，稳稳记住。</small>
            </span>
          </div>
          <div>
            <span className="feature-icon purple">
              <Icon name="sparkles" />
            </span>
            <span>
              <strong>AI 助学</strong>
              <small>拆解长句，把问题问明白。</small>
            </span>
          </div>
          <div>
            <span className="feature-icon green">
              <Icon name="headphones" />
            </span>
            <span>
              <strong>听力训练</strong>
              <small>听清信息，练习理解与记忆。</small>
            </span>
          </div>
        </div>
      </section>
      <section className="panel glass login-panel">
        <span className="login-badge">
          <Icon name="shield" />
          你的专属学习空间
        </span>
        <h1>欢迎回来。</h1>
        <p>登录账户，继续今天的进步。</p>
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
          <div className="form-field">
            <label htmlFor="login-password">密码</label>
            <div className="password-field">
              <input
                id="login-password"
                type={showPassword ? "text" : "password"}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password"
                required
                maxLength={128}
              />
              <button
                type="button"
                className="password-toggle"
                aria-label={showPassword ? "隐藏密码" : "显示密码"}
                aria-pressed={showPassword}
                onClick={() => setShowPassword(!showPassword)}
              >
                <Icon name={showPassword ? "eye-off" : "eye"} />
              </button>
            </div>
          </div>
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
            <Icon name="arrow" />
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
