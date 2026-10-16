import test from "node:test";
import assert from "node:assert/strict";
import { modelsInScope, switchModelScope, toggleOneModel } from "../lib/model-scopes.ts";

const models = [
  { id: 1, name: "Quick", model_id: "vendor/fast", provider_id: "a", kind: "chat", enabled: true },
  { id: 2, name: "Deep", model_id: "vendor/deep", provider_id: "a", kind: "chat", enabled: true },
  { id: 3, name: "Voice", model_id: "vendor/tts", provider_id: "b", kind: "tts", enabled: true },
  { id: 4, name: "Old", model_id: "vendor/old", provider_id: "b", kind: "chat", enabled: false },
];
const filters = { query: "", kind: "chat", provider: "a", selectedOnly: false };
test("filters by multiple intersecting categories, provider, and search", () => {
  assert.deepEqual(modelsInScope(models, [], filters).map((m) => m.id), [1, 2]);
  assert.deepEqual(modelsInScope(models, [], { ...filters, query: "DEEP" }).map((m) => m.id), [2]);
  assert.deepEqual(modelsInScope(models, [2], { ...filters, selectedOnly: true }).map((m) => m.id), [2]);
});
test("scoped master switch preserves models outside current range", () => {
  assert.deepEqual(switchModelScope([3], modelsInScope(models, [3], filters), true), [3, 1, 2]);
  assert.deepEqual(switchModelScope([1, 2, 3], modelsInScope(models, [1, 2, 3], filters), false), [3]);
});
test("disabled models are never newly granted but can be revoked", () => {
  assert.deepEqual(switchModelScope([], models, true), [1, 2, 3]);
  assert.deepEqual(switchModelScope([4, 3], models, false), []);
  assert.deepEqual(toggleOneModel([], models[3], true), []);
});
test("task limit applies to both batch and single model switches", () => {
  assert.deepEqual(switchModelScope([3], models, true, 2), [3, 1]);
  assert.deepEqual(toggleOneModel([1, 2], models[2], true, 2), [1, 2]);
  assert.deepEqual(toggleOneModel([1, 2], models[1], false, 2), [1]);
});
