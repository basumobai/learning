"use client";
import { PersonalAPI } from "../components/PersonalAPI";
import { useEffect, useState } from "react";
import { api, type Preferences } from "../../lib/api";
import { Navigation, Loading, useProtected } from "../providers";
import { AudioOutput, useSpeech, useVoice, VoicePicker } from "../speech";

export default function Settings() {
  const auth = useProtected();
  const voice = useVoice();
  const speech = useSpeech();
  const [prefs, setPrefs] = useState<Preferences | null>(null);
  const [current, setCurrent] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [code, setCode] = useState("");
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  useEffect(() => {
    if (auth.allowed && !auth.user?.must_change_password)
      api<Preferences>("/preferences")
        .then(setPrefs)
        .catch((e) => setError(e.message));
  }, [auth.allowed, auth.user?.must_change_password]);
  async function run(work: () => Promise<void>) {
    if (busy) return;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await work();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  async function changePassword(e: React.FormEvent) {
    e.preventDefault();
    if (password !== confirmation) {
      setError("两次输入的新密码不一致");
      return;
    }
    run(async () => {
      await api("/auth/password", {
        current_password: current,
        new_password: password,
      });
      setCurrent("");
      setPassword("");
      setConfirmation("");
      window.location.assign("/login");
    });
  }
  async function mfa(action: string) {
    run(async () => {
      const data = await api<{ secret?: string }>("/auth/mfa", {
        action,
        password: current,
        code,
      });
      if (data.secret) setSecret(data.secret);
      else {
        setSecret("");
        setCode("");
        setCurrent("");
        await auth.refresh();
        setNotice(
          action === "confirm"
            ? "两步验证已开启，其他设备已退出。"
            : "两步验证已关闭，其他设备已退出。",
        );
      }
    });
  }
  async function exportData() {
    run(async () => {
      const data = await api("/account/export");
      const blob = new Blob([JSON.stringify(data, null, 2)], {
        type: "application/json",
      });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = `els-learning-${new Date().toISOString().slice(0, 10)}.json`;
      anchor.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    });
  }
  if (!auth.allowed) return <Loading />;
  return (
    <main className="shell">
      <Navigation />
      <section className="intro" id="page-content" tabIndex={-1}>
        <span className="eyebrow">YOUR PACE · YOUR ACCOUNT</span>
        <h1>学得稳，也用得安心。</h1>
        <p>账户：{auth.user?.username} · <a href="#security">修改我的密码</a> · <a href="#security">两步验证与设备登录</a></p>
      </section>
      {auth.user?.must_change_password && (
        <p className="notice" role="status">
          首次登录或管理员重置后，请先修改初始密码，再进入学习。
        </p>
      )}
      {prefs && (
        <section className="panel glass">
          <h2>复习节奏</h2>
          <form
            className="form-stack"
            onSubmit={(e) => {
              e.preventDefault();
              run(async () => {
                const next = {
                  ...prefs,
                  tts_model_id: voice.model?.id || null,
                  voice: voice.voice || prefs.voice,
                };
                await api("/preferences", next);
                setPrefs(next);
                setNotice("复习设置已保存，新的评价将使用目标记忆率。");
              });
            }}
          >
            <div className="form-grid">
              <label>
                每日新词预算
                <input
                  type="number"
                  min={0}
                  max={100}
                  value={prefs.daily_new}
                  onChange={(e) =>
                    setPrefs({ ...prefs, daily_new: Number(e.target.value) })
                  }
                />
              </label>
              <label>
                目标记忆率
                <select
                  value={prefs.retention}
                  onChange={(e) =>
                    setPrefs({ ...prefs, retention: Number(e.target.value) })
                  }
                >
                  {[0.8, 0.85, 0.9, 0.93, 0.95, 0.97].map((n) => (
                    <option key={n} value={n}>
                      {Math.round(n * 100)}%
                    </option>
                  ))}
                </select>
              </label>
              <label>
                学习日时区
                <select
                  value={prefs.timezone}
                  onChange={(e) =>
                    setPrefs({ ...prefs, timezone: e.target.value })
                  }
                >
                  {[
                    "Asia/Shanghai",
                    "Asia/Singapore",
                    "Asia/Hong_Kong",
                    "Asia/Tokyo",
                    "Europe/London",
                    "America/New_York",
                    "UTC",
                  ].map((t) => (
                    <option key={t}>{t}</option>
                  ))}
                </select>
              </label>
            </div>
            <p className="setting-note">
              预算是全天累计的新词数量；到期题优先。设为 0
              可专心复习。暂停词条可在词库中管理。
            </p>
            <button className="primary" disabled={busy}>
              保存复习设置
            </button>
          </form>
        </section>
      )}
      {!auth.user?.must_change_password && (
        <section className="panel glass">
          <h2>自然语音</h2>
          <VoicePicker config={voice} />
          <button
            className="quiet"
            disabled={!voice.model || speech.busy}
            onClick={() =>
              speech.play("/ai/speech", {
                ...voice.body,
                input:
                  "Learning a little every day helps you understand the world.",
              })
            }
          >
            试听当前音色
          </button>
          <AudioOutput player={speech} />
          <p className="setting-note">
            音色取决于管理员配置的模型，可选择喜欢的声音与播放速度。
          </p>
        </section>
      )}
      <section className="panel glass" id="security">
        <h2>密码与登录安全</h2>
        <form className="form-stack" onSubmit={changePassword}>
          <label>
            当前密码
            <input
              type="password"
              autoComplete="current-password"
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
              required
              maxLength={128}
            />
          </label>
          <div className="form-grid">
            <label>
              新密码（15–128 个字符）
              <input
                type="password"
                autoComplete="new-password"
                minLength={15}
                maxLength={128}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
            </label>
            <label>
              再输入一次新密码
              <input
                type="password"
                autoComplete="new-password"
                value={confirmation}
                onChange={(e) => setConfirmation(e.target.value)}
                required
                maxLength={128}
              />
            </label>
          </div>
          <p className="setting-note">
            可使用容易记住的长短语。更改密码后，所有设备都需要重新登录。
          </p>
          <button className="primary" disabled={busy}>
            更改密码并重新登录
          </button>
        </form>
        {!auth.user?.must_change_password && (
          <div className="security-actions">
            <h3>两步验证 · {auth.user?.mfa_enabled ? "已开启" : "未开启"}</h3>
            <p>上方填写当前密码，再用验证器绑定或关闭两步验证。</p>
            {secret && (
              <>
                <p>
                  在验证器中手动添加账户
                  ELS，输入以下密钥。十分钟内填写验证码完成绑定。
                </p>
                <code className="enrollment-secret">{secret}</code>
              </>
            )}
            <label>
              验证器六位验证码
              <input
                value={code}
                onChange={(e) => setCode(e.target.value.replace(/\D/g, ""))}
                inputMode="numeric"
                autoComplete="one-time-code"
                maxLength={6}
              />
            </label>
            <div className="actions">
              {auth.user?.mfa_enabled ? (
                <button
                  className="quiet"
                  disabled={busy || !current || code.length !== 6}
                  onClick={() => mfa("disable")}
                >
                  关闭两步验证
                </button>
              ) : (
                <>
                  <button
                    className="quiet"
                    disabled={busy || !current}
                    onClick={() => mfa("start")}
                  >
                    开始绑定验证器
                  </button>
                  {secret && (
                    <button
                      className="primary"
                      disabled={busy || code.length !== 6}
                      onClick={() => mfa("confirm")}
                    >
                      确认绑定
                    </button>
                  )}
                </>
              )}
              <button
                className="quiet"
                disabled={busy}
                onClick={() =>
                  run(async () => {
                    await api("/auth/revoke-sessions", {});
                    setNotice("其他设备已退出登录。");
                  })
                }
              >
                退出其他设备
              </button>
            </div>
            <p className="setting-note">
              保管好验证器。丢失时由管理员重置密码与两步验证；系统不会通过学习对话处理账户凭据。
            </p>
          </div>
        )}
      </section>
      {!auth.user?.must_change_password && (
        <section className="panel glass">
          <h2>我的学习数据</h2>
          <p>导出自己的复习记录、设置、辅导对话和训练结果，便于留存。</p>
          <button className="quiet" disabled={busy} onClick={exportData}>
            导出学习记录
          </button>
        </section>
      )}
      {notice && (
        <p className="notice" role="status">
          {notice}
        </p>
      )}
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      {!auth.user?.must_change_password && <PersonalAPI />}
    </main>
  );
}
