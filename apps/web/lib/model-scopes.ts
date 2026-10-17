export type ScopeModel = {
  id: number;
  name: string;
  model_id: string;
  kind: "chat" | "tts" | "asr";
  provider_id?: string;
  provider?: string;
  enabled?: boolean;
};
export type ScopeFilters = {
  query: string;
  kind: "all" | "chat" | "tts" | "asr";
  provider: string;
  selectedOnly: boolean;
};

export function modelsInScope(models: ScopeModel[], selected: number[], filters: ScopeFilters): ScopeModel[] {
  const query = filters.query.trim().toLocaleLowerCase();
  const chosen = new Set(selected);
  return models.filter((model) =>
    (filters.kind === "all" || model.kind === filters.kind) &&
    (filters.provider === "all" || (model.provider_id || model.provider || "") === filters.provider) &&
    (!filters.selectedOnly || chosen.has(model.id)) &&
    (!query || [model.name, model.model_id, model.provider_id || "", model.provider || ""].some((text) => text.toLocaleLowerCase().includes(query)))
  );
}

// Bulk switches only affect the current scope. Hidden selections are preserved,
// and disabled models can be revoked but cannot be newly granted.
export function switchModelScope(selected: number[], scope: ScopeModel[], enable: boolean, maxSelected = Infinity): number[] {
  const visible = new Set(scope.map((model) => model.id));
  if (!enable) return selected.filter((id) => !visible.has(id));
  const next = [...new Set(selected)];
  const selectedSet = new Set(next);
  for (const model of scope) {
    if (model.enabled === false || selectedSet.has(model.id) || next.length >= maxSelected) continue;
    next.push(model.id);
    selectedSet.add(model.id);
  }
  return next;
}

export function toggleOneModel(selected: number[], model: ScopeModel, enabled: boolean, maxSelected = Infinity): number[] {
  return switchModelScope(selected, [model], enabled, maxSelected);
}
