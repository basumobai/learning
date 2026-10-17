"use client";
import {MathText} from "../components/MathText";
import {useEffect,useState} from "react";
import {api} from "../../lib/api";
import {subjects} from "../../lib/learning";
import {Navigation,Loading,useProtected} from "../providers";

type Unit={unit_id:string;body:{title:string;unit_kind:string;subject_id:string;content:{text:string;passage?:string;parts:{id:string;text:string;choices?:{id:string;text:string}[]}[]};pages:number[]};assets:{id:string;alt:string}[]};
type Resource={id:string;title:string;subject_id:string;units:number};

export default function Library(){
 const auth=useProtected(),[resources,setResources]=useState<Resource[]>([]),[units,setUnits]=useState<Unit[]>([]),[current,setCurrent]=useState(''),[message,setMessage]=useState(''),[offset,setOffset]=useState(0),[total,setTotal]=useState(0),[catalogOffset,setCatalogOffset]=useState(0),[catalogTotal,setCatalogTotal]=useState(0),[query,setQuery]=useState(''),[subject,setSubject]=useState(''),[refresh,setRefresh]=useState(0);
 useEffect(()=>{if(!auth.user)return;let active=true;void api<{resources:Resource[];total:number}>(`/resources/library?${new URLSearchParams({q:query,subject,offset:String(catalogOffset)})}`).then(r=>{if(active){setResources(r.resources);setCatalogTotal(r.total??r.resources.length)}}).catch(e=>{if(active)setMessage(e.message)});return()=>{active=false}},[auth.user,query,subject,catalogOffset,refresh]);
 useEffect(()=>{if(!current){setUnits([]);setTotal(0);return}let active=true;void api<{resource:{units:Unit[];unit_count:number}}>(`/resources/library/${current}?offset=${offset}`).then(r=>{if(active){setUnits(r.resource.units);setTotal(r.resource.unit_count)}}).catch(e=>{if(active)setMessage(e.message)});return()=>{active=false}},[current,offset,refresh]);
 function selectResource(id:string){setCurrent(id);setOffset(0);setUnits([]);setTotal(0);setMessage('')}
 if(!auth.user)return <Loading/>;
 return <><Navigation/><main className="resource-workspace" id="page-content" tabIndex={-1}>
  <h1>学习资源库</h1><p>已审核发布的教材内容与独立专题资料。</p><p role="status">{message}</p>
  <section className="resource-filters">
   <label>搜索正式资源<input value={query} onChange={e=>{setQuery(e.target.value);setCatalogOffset(0);selectResource('')}}/></label>
   <label>资源学科<select value={subject} onChange={e=>{setSubject(e.target.value);setCatalogOffset(0);selectResource('')}}><option value="">全部学科</option>{subjects.map(s=><option key={s.id} value={s.id}>{s.name}</option>)}</select></label>
   <button onClick={()=>{setMessage('');setRefresh(v=>v+1)}}>刷新资源</button>
  </section>
  <label>选择资源<select value={current} onChange={e=>selectResource(e.target.value)}><option value="">请选择</option>{resources.map(r=><option key={r.id} value={r.id}>{r.title} · {r.units}个内容单元</option>)}</select></label>
  <div className="resource-actions"><button disabled={!catalogOffset} onClick={()=>{setCatalogOffset(Math.max(0,catalogOffset-25));selectResource('')}}>上一批资源</button><span>共{catalogTotal}份资源 · 第{catalogOffset/25+1}页</span><button disabled={catalogOffset+resources.length>=catalogTotal} onClick={()=>{setCatalogOffset(catalogOffset+25);selectResource('')}}>下一批资源</button></div>
  {!resources.length&&<p>当前条件下暂无正式内容，现有题库练习仍可正常使用。</p>}
  {units.map(u=><article className="card resource-unit" key={u.unit_id}><h2>{u.body.title}</h2><p>{u.body.subject_id} · 原PDF第{u.body.pages.join('、')}页</p><p style={{whiteSpace:'pre-wrap'}}><MathText text={u.body.content.text}/></p>{u.body.content.passage&&<blockquote style={{whiteSpace:'pre-wrap'}}><MathText text={u.body.content.passage}/></blockquote>}{u.body.content.parts?.map(p=><div key={p.id}><p>{p.id}. <MathText text={p.text}/></p>{p.choices?.map(c=><p key={c.id}>{c.id}. <MathText text={c.text}/></p>)}</div>)}{u.assets.map(a=><figure key={a.id}><img src={`/api/v1/resources/library/assets/${a.id}`} alt={a.alt}/><figcaption>{a.alt}</figcaption></figure>)}{u.body.unit_kind==='question'&&<a href={`/practice?subject=${u.body.subject_id}`}>进入学科练习</a>}</article>)}
  {current&&<div className="resource-actions"><button disabled={!offset} onClick={()=>setOffset(Math.max(0,offset-25))}>上一页内容</button><span>{total}个内容单元</span><button disabled={offset+units.length>=total} onClick={()=>setOffset(offset+25)}>下一页内容</button></div>}
 </main></>
}
