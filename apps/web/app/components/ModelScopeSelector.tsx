"use client";

import { useMemo, useState } from "react";
import { modelsInScope, switchModelScope, toggleOneModel, type ScopeModel, type ScopeFilters } from "../../lib/model-scopes";

type ProviderOption = { id: string; name: string };
type Props = {
  models: ScopeModel[];
  selected: number[];
  onChange: (ids: number[]) => void;
  providers?: ProviderOption[];
  maxSelected?: number;
  label?: string;
};

export function ModelScopeSelector({
  models, selected, onChange, providers = [], maxSelected, label = "模型授权",
}: Props) {
  const [filters, setFilters] = useState<ScopeFilters>({
    query: "", kind: "all", provider: "all", selectedOnly: false,
  });
  const providerOptions = useMemo(() => {
    const names = new Map(providers.map((provider) => [provider.id, provider.name]));
    for (const model of models) {
      const id = model.provider_id || model.provider || "";
      if (id && !names.has(id)) names.set(id, model.provider || id);
    }
    return [...names].map(([id, name]) => ({ id, name }));
  }, [models, providers]);
  const visible = useMemo(() => modelsInScope(models, selected, filters), [models, selected, filters]);
  const activeVisible = visible.filter((model) => model.enabled !== false);
  const selectedSet = new Set(selected);
  const inScopeSelected = visible.filter((model) => selectedSet.has(model.id)).length;
  const allActiveSelected = activeVisible.length > 0 && activeVisible.every((model) => selectedSet.has(model.id));
  const canAdd = maxSelected === undefined || selected.length < maxSelected;
  function applyScope(enable: boolean, scope = visible) {
    onChange(switchModelScope(selected, scope, enable, maxSelected));
  }
  return (
    <div className="model-scope-selector" aria-label={label}>
      <div className="model-scope-summary" role="status" aria-live="polite">
        <strong>已选 {selected.length} 个</strong>
        <span>当前范围 {inScopeSelected} / {visible.length} 个</span>
        {maxSelected !== undefined && <span>最多 {maxSelected} 个</span>}
      </div>
      <div className="model-scope-controls">
        <label>
          搜索模型
          <input
            type="search"
            value={filters.query}
            onChange={(event) => setFilters((prev) => ({ ...prev, query: event.target.value }))}
            placeholder="名称、模型 ID 或服务商"
          />
        </label>
        <label>
          模型类别
          <select
            value={filters.kind}
            onChange={(event) => setFilters((prev) => ({ ...prev, kind: event.target.value as ScopeFilters["kind"] }))}
          >
            <option value="all">全部类别</option>
            <option value="chat">聊天 / AI 助学</option>
            <option value="tts">语音 / TTS</option>
          </select>
        </label>
        <label>
          服务商范围
          <select
            value={filters.provider}
            onChange={(event) => setFilters((prev) => ({ ...prev, provider: event.target.value }))}
          >
            <option value="all">所有服务商</option>
            {providerOptions.map((provider) => (
              <option value={provider.id} key={provider.id}>{provider.name}</option>
            ))}
          </select>
        </label>
      </div>
      <div className="model-scope-toolbar">
        <label className="check-label model-scope-only">
          <input
            type="checkbox"
            checked={filters.selectedOnly}
            onChange={(event) => setFilters((prev) => ({ ...prev, selectedOnly: event.target.checked }))}
          />
          仅看已选
        </label>
        <button
          type="button"
          className="quiet model-scope-master"
          aria-pressed={allActiveSelected}
          disabled={!activeVisible.length || (!allActiveSelected && !canAdd)}
          onClick={() => applyScope(!allActiveSelected)}
          title="只改变当前筛选范围，其他模型保持原样"
        >
          {allActiveSelected ? "关闭当前范围" : "开启当前范围"}
        </button>
        <button type="button" className="quiet" onClick={() => applyScope(true, models)} disabled={!canAdd || !models.some((m) => m.enabled !== false)}>
          全部可用模型
        </button>
        <button type="button" className="quiet" onClick={() => onChange([])} disabled={!selected.length}>
          全部清空
        </button>
      </div>
      <p className="setting-note">
        总开关只影响当前搜索、类别和服务商范围；调整筛选不会丢失范围外的选择。保存后生效。
      </p>
      {visible.length ? (
        <div className="model-scope-grid">
          {visible.map((model) => {
            const checked = selectedSet.has(model.id);
            const disabled = model.enabled === false;
            return (
              <label className={"model-scope-item" + (checked ? " is-selected" : "")} key={model.id}>
                <input
                  type="checkbox"
                  checked={checked}
                  disabled={!checked && (disabled || !canAdd)}
                  onChange={(event) => onChange(toggleOneModel(selected, model, event.target.checked, maxSelected))}
                />
                <span>
                  <strong>{model.name}</strong>
                  <small>{model.model_id}</small>
                  <small>{model.kind === "asr" ? "转写" : model.kind === "tts" ? "语音" : "聊天"}{disabled ? " · 已停用（仅可撤销）" : ""}</small>
                </span>
              </label>
            );
          })}
        </div>
      ) : (
        <p className="empty-state">当前范围没有匹配模型。可以更换筛选条件，或先添加模型。</p>
      )}
    </div>
  );
}
