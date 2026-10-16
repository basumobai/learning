"use client";
import { useEffect, useState } from "react";
import { api } from "../../lib/api";
export function RegistrationPolicy({
  password,
  groups,
}: {
  password: string;
  groups: { id: number; name: string }[];
}) {
  const [policy, setPolicy] = useState<{
    registration_open: boolean;
    default_group_id: number;
  } | null>(null);
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    api<{ registration_open: boolean; default_group_id: number }>(
      "/admin/registration",
    )
      .then(setPolicy)
      .catch((e) => setMessage(e.message));
  }, []);
  return (
    <fieldset>
      <legend>自主注册</legend>
      <p>
        用户从登录页自行注册；管理员设置默认身份组，也可随时关闭注册。
        <a href="/settings#security">修改我自己的密码 →</a>
      </p>
      {policy && (
        <>
          <label className="check-label">
            <input
              type="checkbox"
              checked={policy.registration_open}
              onChange={(e) =>
                setPolicy({ ...policy, registration_open: e.target.checked })
              }
            />
            开放用户自主注册
          </label>
          <label>
            新用户默认身份组
            <select
              value={policy.default_group_id}
              onChange={(e) =>
                setPolicy({
                  ...policy,
                  default_group_id: Number(e.target.value),
                })
              }
            >
              {groups.map((g) => (
                <option key={g.id} value={g.id}>
                  {g.name}
                </option>
              ))}
            </select>
          </label>
          <button
            type="button"
            className="quiet"
            disabled={busy}
            onClick={async () => {
              setBusy(true);
              try {
                await api("/admin/registration", {
                  ...policy,

                });
                setMessage("注册设置已保存，仅影响此后注册的用户。");
              } catch (e) {
                setMessage((e as Error).message);
              } finally {
                setBusy(false);
              }
            }}
          >
            保存注册设置
          </button>
        </>
      )}
      {message && <p role="status">{message}</p>}
    </fieldset>
  );
}
