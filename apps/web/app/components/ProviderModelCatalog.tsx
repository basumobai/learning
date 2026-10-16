"use client";
import { useEffect, useMemo, useRef, useState } from "react";
import { api } from "../../lib/api";
import { ModelScopeSelector } from "./ModelScopeSelector";

type CatalogEntry = {
 model_id: string; name: string; context_length: number | null;
 input_modalities: string[] | null; output_modalities: string[] | null;
 pricing: Record<string,string> | null; price_unit: string | null;
 source_url: string; fetched_at: string; available: boolean; selected_id: number | null;
};
export function ProviderModelCatalog({ providerId, onClose, onSaved }: {providerId:string;onClose:()=>void;onSaved:()=>Promise<unknown>}) {
 const dialog=useRef<HTMLDialogElement>(null);
 const [entries,setEntries]=useState<CatalogEntry[]>([]),[selected,setSelected]=useState<string[]>([]);
 const [busy,setBusy]=useState(false),[error,setError]=useState(""),[sort,setSort]=useState("name");
 const [modality,setModality]=useState("all"),[kind,setKind]=useState<"chat"|"tts">("chat"),[voices,setVoices]=useState("");
 async function load(sync=false) {
  setBusy(true);setError("");
  try {
   if(sync) await api(`/admin/ai/providers/${providerId}/models/sync`,{});
   const result=await api<{models:CatalogEntry[]}>(`/admin/ai/providers/${providerId}/models`);
   if(!sync&&!result.models.length){await api(`/admin/ai/providers/${providerId}/models/sync`,{});const again=await api<{models:CatalogEntry[]}>(`/admin/ai/providers/${providerId}/models`);setEntries(again.models);}
   else setEntries(result.models);
  } catch(e){setError((e as Error).message+"。可关闭后使用手动添加。");} finally{setBusy(false);}
 }
 useEffect(()=>{dialog.current?.showModal();void load();},[providerId]);
 const models=useMemo(()=>entries.map((m,i)=>({...m,id:i+1,kind,enabled:m.available})).filter(m=>modality==="all"||m.input_modalities?.includes(modality)).sort((a,b)=>sort==="context"?(b.context_length||0)-(a.context_length||0):sort==="price"?(a.price_unit==="USD/token"?Number(a.pricing?.completion??Infinity):Infinity)-(b.price_unit==="USD/token"?Number(b.pricing?.completion??Infinity):Infinity):a.name.localeCompare(b.name)),[entries,modality,sort,kind]);
 return <dialog ref={dialog} className="model-catalog-dialog" onCancel={onClose} aria-labelledby="catalog-title">
  <div className="panel-heading"><h2 id="catalog-title">选择服务商模型</h2><button type="button" className="quiet" onClick={onClose}>关闭</button></div>
  <p>目录由服务商提供。未知能力和价格保留为空；保存选用后仍需授权身份组。</p>
  {error&&<p className="error" role="alert">{error}</p>}
  <div className="form-grid">
   <label>排序<select value={sort} onChange={e=>setSort(e.target.value)}><option value="name">名称</option><option value="context">上下文从大到小</option><option value="price">公开输出价格从低到高</option></select></label>
   <label>输入模态<select value={modality} onChange={e=>setModality(e.target.value)}><option value="all">全部（含未知）</option><option value="text">文字</option><option value="image">图像</option><option value="audio">音频</option></select></label>
   <label>选用用途<select value={kind} onChange={e=>setKind(e.target.value as "chat"|"tts")}><option value="chat">AI文字与图文任务</option><option value="tts">语音 TTS</option></select></label>
   {kind==="tts"&&<label>服务商支持的音色（逗号分隔）<input value={voices} onChange={e=>setVoices(e.target.value)}/></label>}
  </div>
  {busy?<p role="status">正在获取模型目录…</p>:<ModelScopeSelector models={models} selected={entries.flatMap((m,i)=>selected.includes(m.model_id)?[i+1]:[])} onChange={ids=>setSelected(entries.filter((_,i)=>ids.includes(i+1)).map(m=>m.model_id))} label="服务商模型目录"/>}
  <details><summary>能力、公开价格和来源（{entries.length}项）</summary><div className="catalog-metadata">{models.slice(0,200).map(m=><article key={m.model_id}><strong>{m.name}</strong><code>{m.model_id}</code><span>上下文：{m.context_length?.toLocaleString()||"未提供"} · 输入：{m.input_modalities?.join("、")||"未知"} · 输出：{m.output_modalities?.join("、")||"未知"}</span><span>价格：{m.pricing?Object.entries(m.pricing).map(([k,v])=>`${k}: ${v}`).join(" / "):"未提供"} {m.price_unit||"（单位未提供）"}</span><a href={m.source_url} target="_blank" rel="noreferrer">服务商目录来源</a><small>获取于 {new Date(m.fetched_at).toLocaleString()}{!m.available?" · 已不在最新目录中":""}</small></article>)}</div></details>
  <div className="catalog-actions"><button type="button" className="quiet" disabled={busy} onClick={()=>void load(true)}>刷新目录 / 测试连接</button><button type="button" className="primary" disabled={busy||!selected.length||(kind==="tts"&&!voices.trim())} onClick={async()=>{setBusy(true);setError("");try{await api("/admin/ai/model-selections",{provider_id:providerId,model_ids:selected,kind,voices:voices.split(",").map(v=>v.trim()).filter(Boolean)},"PUT");await onSaved();onClose();}catch(e){setError((e as Error).message);}finally{setBusy(false);}}}>选用 {selected.length} 个模型</button></div>
 </dialog>;
}
