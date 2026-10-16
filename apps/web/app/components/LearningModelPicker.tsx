"use client";
import Link from "next/link";
export type ModelSelection = {
  model_id: number;
  reviewer_ids: number[];
  lead_model_id: number;
};
export type ModelOptions = {
  group: { id: number; name: string };
  models: { id: number; name: string; model_id: string; provider: string }[];
  selection: ModelSelection;
  personal_enabled: boolean;
};
export function LearningModelPicker({
  options,
  value,
  onChange,
  personal,
  onPersonal,
  essay,
  status,
}: {
  options: ModelOptions | null;
  value: ModelSelection;
  onChange: (value: ModelSelection) => void;
  personal: boolean;
  onPersonal: (value: boolean) => void;
  essay: boolean;
  status: string;
}) {
  return (
    <section className="learning-model-picker" aria-label="练习模型选择">
      <div className="panel-heading">
        <h3>本次 AI 助学</h3>
        <span className="access-badge">{options?.group.name || "身份组"}</span>
      </div>
      <p className="setting-note">
        模型由管理员提供，按你的身份组授权。只在交卷评价、答错讲解或主动求助时使用。
      </p>
      {options ? (
        <>
          {options.models.length ? (
            <label>
              助学模型
              <select
                value={value.model_id}
                onChange={(e) =>
                  onChange({ ...value, model_id: Number(e.target.value) })
                }
                disabled={personal}
              >
                {options.models.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.name} · {m.provider}
                  </option>
                ))}
              </select>
              <small>用于动态讲解、步骤追问和非作文评价。</small>
            </label>
          ) : (
            <p className="empty-state">
              身份组还没有可用模型。请管理员在“身份组”中分配；你仍可作答和查看参考解析。
            </p>
          )}
          {essay && (
            <fieldset className="reviewer-picker" disabled={personal}>
              <legend>作文独立评审 · 选择2至5个</legend>
              {options.models.map((m) => (
                <label className="check-label" key={m.id}>
                  <input
                    type="checkbox"
                    checked={value.reviewer_ids.includes(m.id)}
                    disabled={
                      !value.reviewer_ids.includes(m.id) &&
                      value.reviewer_ids.length >= 5
                    }
                    onChange={(e) =>
                      onChange({
                        ...value,
                        reviewer_ids: e.target.checked
                          ? [...value.reviewer_ids, m.id]
                          : value.reviewer_ids.filter((id) => id !== m.id),
                      })
                    }
                  />
                  <span>{m.name}</span>
                </label>
              ))}
              <label>
                主审模型
                <select
                  value={value.lead_model_id}
                  onChange={(e) =>
                    onChange({
                      ...value,
                      lead_model_id: Number(e.target.value),
                    })
                  }
                >
                  <option value={0}>选择主审</option>
                  {options.models.map((m) => (
                    <option key={m.id} value={m.id}>
                      {m.name}
                    </option>
                  ))}
                </select>
              </label>
              <small>独立评审并行思考，主审结合证据处理分歧。</small>
            </fieldset>
          )}
          {options.personal_enabled ? (
            <label className="check-label">
              <input
                type="checkbox"
                checked={personal}
                onChange={(e) => onPersonal(e.target.checked)}
              />
              使用我已配置的个人 API
            </label>
          ) : (
            <p className="setting-note">
              <Link href="/settings">也可配置自己的兼容 API</Link>
            </p>
          )}
          {personal && (
            <p className="setting-note">
              使用设置页中的个人模型与作文评审组合。
            </p>
          )}
          <small role="status">{status}</small>
        </>
      ) : (
        <p role="status">正在读取身份组模型…</p>
      )}
    </section>
  );
}
