"use client";
import { useEffect, useState } from "react";
import { api } from "../../lib/api";
export function PersonalAPI() {
  const [form, setForm] = useState({
      base_url: "https://openrouter.ai/api/v1",
      model_id: "",
      api_key: "",
      enabled: true,
      reviewer_models: [] as string[],
    }),
    [reviewers, setReviewers] = useState(""),
    [hasKey, setHasKey] = useState(false),
    [notice, setNotice] = useState(""),
    [busy, setBusy] = useState(false);
  useEffect(() => {
    api<typeof form & { has_key: boolean }>("/learning/ai-settings")
      .then((d) => {
        if (d.has_key) {
          setForm({ ...d, api_key: "" });
          setHasKey(true);
          setReviewers((d.reviewer_models || []).join(", "));
        }
      })
      .catch((e) => setNotice(e.message));
  }, []);
  return (
    <section className="panel glass personal-api">
      <h2>自己的 AI 接口</h2>
      <p className="setting-note">
        用于多学科题目讲解、追问与评价。密钥加密保存在服务端，不返回浏览器。未选择个人接口时，使用权限组授权的任务模型。
      </p>
      <form
        onSubmit={async (e) => {
          e.preventDefault();
          if (busy) return;
          setBusy(true);
          setNotice("");
          try {
            await api("/learning/ai-settings", {
              base_url: form.base_url,
              model_id: form.model_id,
              api_key: form.api_key,
              enabled: form.enabled,
              reviewer_models: reviewers
                .split(",")
                .map((s) => s.trim())
                .filter(Boolean),
            });
            setForm((v) => ({ ...v, api_key: "" }));
            setHasKey(true);
            setNotice("个人接口已保存");
          } catch (e) {
            setNotice((e as Error).message);
          } finally {
            setBusy(false);
          }
        }}
      >
        <label>
          OpenAI 兼容基础地址
          <input
            type="url"
            required
            value={form.base_url}
            onChange={(e) =>
              setForm((v) => ({ ...v, base_url: e.target.value }))
            }
          />
        </label>
        <label>
          常用／作文主审模型标识
          <input
            required
            value={form.model_id}
            onChange={(e) =>
              setForm((v) => ({ ...v, model_id: e.target.value }))
            }
          />
        </label>
        <label>
          API Key{hasKey ? "（留空保留原密钥）" : ""}
          <input
            type="password"
            autoComplete="off"
            value={form.api_key}
            required={!hasKey}
            onChange={(e) =>
              setForm((v) => ({ ...v, api_key: e.target.value }))
            }
          />
        </label>
        <label>
          作文独立评审模型（可选，至少两个，以英文逗号分隔）
          <input
            value={reviewers}
            onChange={(e) => setReviewers(e.target.value)}
            placeholder="provider/model-a, provider/model-b"
          />
        </label>
        <label className="check-label">
          <input
            type="checkbox"
            checked={form.enabled}
            onChange={(e) =>
              setForm((v) => ({ ...v, enabled: e.target.checked }))
            }
          />
          启用个人接口
        </label>
        <button className="primary" disabled={busy}>
          保存个人接口
        </button>
        <p role="status">{notice}</p>
      </form>
    </section>
  );
}
