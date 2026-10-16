"use client";
import { useEffect,useRef,useState } from "react";
import { api,requestID } from "../../lib/api";
import { AudioOutput,useSpeech,useVoice,VoicePicker } from "../speech";

type Task={index:number;kind:"read"|"ask"|"answer"|"retell";cue:string};
type Reference={reference:string;question:string;reply:string;points:string[];evidence:string};
type Feedback={items:{index:number;assessment:string;suggestion:string}[];summary:string};
export type SpeakingLesson={id:string;kind:"listening";format:string;title?:string;scenario:string;keywords:string[];speaking_tasks:Task[];reading_text?:string;submitted?:boolean;responses?:string[];notes?:string;reference_tasks?:Reference[];transcript?:string;translation?:string;speaking_feedback?:Feedback};

export function GuangdongPractice({lesson,modelID,onUpdate}:{lesson:SpeakingLesson;modelID:number;onUpdate:(d:SpeakingLesson)=>void}) {
  const voice=useVoice(),speech=useSpeech();
  const [step,setStep]=useState(0),[responses,setResponses]=useState<string[]>(lesson.responses||lesson.speaking_tasks.map(()=>"")),[notes,setNotes]=useState(lesson.notes||"");
  const [error,setError]=useState(""),[busy,setBusy]=useState(false),[seconds,setSeconds]=useState<number|null>(null);
  const [feedback,setFeedback]=useState<Feedback|null>(lesson.speaking_feedback||null);
  const [recording,setRecording]=useState(false),[recordings,setRecordings]=useState<Record<number,string>>({});
  const recorder=useRef<MediaRecorder|null>(null),stream=useRef<MediaStream|null>(null),urls=useRef<string[]>([]),stopTimer=useRef<ReturnType<typeof setTimeout>|null>(null);
  const submissionID=useRef(requestID()),feedbackID=useRef(requestID());
  const current=lesson.speaking_tasks[step];
  useEffect(()=>{if(seconds===null||seconds<=0)return;const timer=setTimeout(()=>setSeconds(s=>s===null?null:s-1),1000);return()=>clearTimeout(timer);},[seconds]);
  useEffect(()=>()=>{if(recorder.current?.state==="recording")recorder.current.stop();stream.current?.getTracks().forEach(t=>t.stop());if(stopTimer.current)clearTimeout(stopTimer.current);urls.current.forEach(u=>URL.revokeObjectURL(u));},[]);
  function stop(){if(stopTimer.current)clearTimeout(stopTimer.current);if(recorder.current?.state==="recording")recorder.current.stop();stream.current?.getTracks().forEach(t=>t.stop());setRecording(false);}
  async function record(){
    setError("");
    if(!navigator.mediaDevices?.getUserMedia||!window.MediaRecorder){setError("当前浏览器不支持录音，请使用HTTPS并允许麦克风，或直接输入作答文字。");return;}
    try{const media=await navigator.mediaDevices.getUserMedia({audio:true});stream.current=media;const r=new MediaRecorder(media);recorder.current=r;const chunks:BlobPart[]=[];const index=step;
      r.ondataavailable=e=>{if(e.data.size)chunks.push(e.data);};r.onstop=()=>{const u=URL.createObjectURL(new Blob(chunks,{type:r.mimeType}));urls.current.push(u);setRecordings(v=>({...v,[index]:u}));media.getTracks().forEach(t=>t.stop());setRecording(false);};
      r.start();setRecording(true);stopTimer.current=setTimeout(stop,180000);
    }catch{setError("未能使用麦克风，请在浏览器设置中允许录音，或改用文字作答。");}
  }
  async function submit(){setBusy(true);setError("");try{const d=await api<SpeakingLesson>(`/ai/training/${lesson.id}/answers`,{request_id:submissionID.current,answers:[],responses,notes});onUpdate(d);}catch(e){setError((e as Error).message);}finally{setBusy(false);}}
  async function assess(){setBusy(true);setError("");try{const f=await api<Feedback>(`/ai/training/${lesson.id}/feedback`,{request_id:feedbackID.current,model_id:modelID});setFeedback(f);feedbackID.current=requestID();}catch(e){setError((e as Error).message);}finally{setBusy(false);}}
  function play(segment:string){void speech.play(`/ai/training/${lesson.id}/speech`,{...voice.body,segment});}
  const prepare=current.kind==="ask"?20:current.kind==="answer"?10:60;
  return <section className="panel glass listening-lesson"><h2>{lesson.title}</h2><p>{lesson.scenario}</p><p className="setting-note">广东听说题型专项 · 原创练习。Part B 为三问五答；Part C 建议听两遍后准备一分钟复述。计时可手动开始，按个人节奏训练。</p>
    <VoicePicker config={voice}/><button className="primary" disabled={!voice.model||speech.busy||recording} onClick={()=>play("source")}>播放原始材料{lesson.format==="guangdong_c"?"（建议两遍）":""}</button><AudioOutput player={speech}/>
    {lesson.reading_text&&<p className="preserve-text" lang="en">{lesson.reading_text}</p>}
    {lesson.keywords?.length>0&&<p>提示词：{lesson.keywords.join(" · ")}</p>}
    <label>听力笔记<textarea rows={4} maxLength={4000} placeholder="记录人物、时间、数字、转折与结果，可用缩写。" value={notes} disabled={lesson.submitted} onChange={e=>setNotes(e.target.value)}/></label>
    <div className="segments" aria-label="听说作答步骤">{lesson.speaking_tasks.map((t,i)=><button className="quiet" key={i} disabled={recording} aria-pressed={step===i} onClick={()=>{speech.stop();setStep(i);setSeconds(null);}}>{t.kind==="ask"?`问${i+1}`:t.kind==="answer"?`答${i-2}`:t.kind==="read"?"朗读":"复述"}</button>)}</div>
    <h3>第 {step+1}/{lesson.speaking_tasks.length} 步 · {current.cue}</h3>
    {current.kind==="answer"&&<button className="quiet" disabled={!voice.model||speech.busy||recording} onClick={()=>play(`question-${step}`)}>听当前问题</button>}
    {!lesson.submitted&&<><button className="quiet" disabled={recording} onClick={()=>setSeconds(prepare)}>开始准备计时（{prepare}秒）</button>{seconds!==null&&<p role="timer">{seconds>0?`准备剩余 ${seconds} 秒`:"准备结束，可以开始作答。"}</p>}
    <div className="action-row"><button type="button" className="quiet" disabled={speech.busy} onClick={()=>recording?stop():void record()}>{recording?"结束录音":"录制这一项作答"}</button></div></>}
    {recordings[step]&&<div><audio controls src={recordings[step]}/><a href={recordings[step]} download={`learning-speaking-${step+1}.webm`}>下载本项录音</a></div>}
    <label>我的英文作答／录音转写<textarea rows={5} maxLength={4000} value={lesson.submitted?(lesson.responses?.[step]||""):responses[step]} disabled={lesson.submitted} onChange={e=>setResponses(v=>v.map((s,i)=>i===step?e.target.value:s))} placeholder="先开口作答，再手动记下自己的英语表达，供交卷后核对与AI评价。"/></label>
    {current.kind==="ask"&&<button className="quiet" disabled={!voice.model||speech.busy||recording} onClick={()=>play(`reply-${step}`)}>已提问，听计算机回答并记笔记</button>}
    <p className="setting-note">录音仅在当前页面本机回放，离开前可下载。AI只评价填写的文字；语音转写和专业发音评分尚未接入。</p>
    {!lesson.submitted?<button className="primary" disabled={busy||recording} onClick={()=>void submit()}>{busy?"正在提交…":"提交本组作答，核对参考表达"}</button>:<><p className="notice">已交卷。以下为参考表达与内容要点，可接受合理的同义表达。</p>
      {lesson.reference_tasks?.[step]&&<article><h3>本项参考</h3><p lang="en">{lesson.reference_tasks[step].question}</p><p lang="en">{lesson.reference_tasks[step].reference}</p><ul>{lesson.reference_tasks[step].points.map((p,i)=><li key={i}>{p}</li>)}</ul><p className="evidence">依据：{lesson.reference_tasks[step].evidence}</p></article>}
      <details><summary>完整原文与译文</summary><p className="preserve-text" lang="en">{lesson.transcript}</p><p>{lesson.translation}</p></details>
      <button className="primary" disabled={busy||!modelID||!(lesson.responses||[]).some(s=>s.trim())} onClick={()=>void assess()}>AI逐项评价我的作答文字</button>
      {feedback&&<div><h3>优先改进</h3><p>{feedback.summary}</p>{feedback.items.map(f=><article key={f.index}><h4>任务 {f.index+1}</h4><p>{f.assessment}</p><p lang="en">{f.suggestion}</p></article>)}</div>}
    </>}
    {error&&<p role="alert" className="error">{error}</p>}
  </section>;
}
