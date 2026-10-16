"use client";
import Link from "next/link";
import { useEffect, useState } from "react";
import { api } from "../../lib/api";

export default function Register() {
  const [open, setOpen] = useState<boolean | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  async function load() {
    try {
      setOpen(
        (await api<{ registration_open: boolean }>("/auth/options"))
          .registration_open,
      );
      setError("");
    } catch (e) {
      setError((e as Error).message);
    }
  }
  useEffect(() => {
    void load();
  }, []);
  async function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const d = new FormData(e.currentTarget);
    if (d.get("password") !== d.get("confirmation")) {
      setError("两次密码不一致");
      return;
    }
    setBusy(true);
    setError("");
    try {
      await api("/auth/register", {
        username: d.get("username"),
        display_name: d.get("display_name"),
        password: d.get("password"),
      });
      setDone(true);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <main className="shell">
      <section className="panel glass">
        <span className="eyebrow">LEARNING · 创建账户</span>
        <h1>开启自己的学习空间</h1>
        {done ? (
          <p role="status">
            注册成功。<Link href="/login">立即登录</Link>
          </p>
        ) : open ? (
          <form className="form-stack" onSubmit={submit}>
            <p>注册后自动加入本站默认学习身份组，无需管理员逐个分配账号。</p>
            <label>
              用户名
              <input
                name="username"
                autoComplete="username"
                minLength={3}
                maxLength={40}
                pattern="[a-z0-9][a-z0-9_.\-]{2,39}"
                required
                placeholder="3–40位小写字母、数字或_.-"
              />
            </label>
            <label>
              显示名称
              <input
                name="display_name"
                autoComplete="nickname"
                maxLength={80}
                required
              />
            </label>
            <label>
              密码（15–128个字符）
              <input
                name="password"
                type="password"
                autoComplete="new-password"
                minLength={15}
                maxLength={128}
                required
              />
            </label>
            <label>
              确认密码
              <input
                name="confirmation"
                type="password"
                autoComplete="new-password"
                minLength={15}
                maxLength={128}
                required
              />
            </label>
            <button className="primary" disabled={busy}>
              {busy ? "正在注册…" : "创建我的账户"}
            </button>
          </form>
        ) : (
          <p>
            {open === false
              ? "本站暂未开放自主注册，请联系管理员。"
              : "正在检查注册设置…"}
          </p>
        )}
        {error && (
          <div role="alert" className="error">
            {error}
            <button className="quiet" onClick={() => void load()}>
              重新检查
            </button>
          </div>
        )}
        <p>
          <Link href="/login">返回登录</Link>
        </p>
      </section>
    </main>
  );
}
