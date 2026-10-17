"use client";
import {readDraft,writeDraft,removeDraft,sameAnswers,type PendingSubmission} from "../../lib/offline";
import { GuidedTutor } from "../components/GuidedTutor";
import { ScoreEvidence } from "../components/ScoreEvidence";
import { RecallCardCreator } from "../components/RecallCardCreator";
import { MistakePanel } from "../components/MistakePanel";
import { MathText } from "../components/MathText";
import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { api, APIError, requestID, upload, saveBeforeLeave } from "../../lib/api";
import {
  subjects,
  hanCount,
  timeLabel,
  type Job,
  type QuestionContent,
  type Timeline,
} from "../../lib/learning";
import { Loading, Navigation, useProtected } from "../providers";
import {
  LearningModelPicker,
  type ModelOptions,
  type ModelSelection,
} from "../components/LearningModelPicker";
import { LearningFigure } from "../components/LearningFigure";
import { AdaptivePanel } from "../components/AdaptivePanel";
import { ExamStructure } from "../components/ExamStructure";
import type { CatalogSubject } from "../../lib/curriculum";
import { TimelinePlayer } from "../components/TimelinePlayer";
type Chapter = {
  id: string;
  subject_id: string;
  parent_id: string | null;
  title: string;
  textbook: string;
  term: string;
  edition?: string;
  volume?: string;
  curriculum_chapter?: string;
  question_count: number;
};
type Catalog = {
  subjects: CatalogSubject[];
  chapters: Chapter[];
};
type Feedback = {
  parts?: {
    id: string;
    correct: boolean;
    needs_ai: boolean;
    points: number;
    max_points: number;
  }[];
  score: number;
  max_score: number;
  needs_ai: boolean;
  ai_reference: boolean;
  incomplete?: boolean;
  han_count?: number;
  timed_out?: boolean;
  ai_grading?: {
    feedback: string;
    parts: { id: string; score: number; reason: string; mistake: string }[];
  };
  essay_review?: {
    score: number;
    reason: string;
    disagreement: string;
    suggestions: string[];
  };
  independent_reviews?: {
    score: number;
    reason: string;
    evidence: string[];
    suggestions: string[];
  }[];
};
type Attempt = {
 exam_session_id?:string;
 score_evidence?: {id:string;score:number;max_score:number;confidence:string;created_at:string;engine_version:string;note:string;supersedes_id?:string}[];
  draft_version?: number;
  id: string;
  question: {
    id: string;
    subject_id: string;
    kind: string;
    content: QuestionContent;
    solution: {
      answers: Record<string, string>;
      explanation: string;
      rubric: string;
    };
    assets: { id: string; alt: string }[];
  };
  deadline: string | null;
  server_time: string;
  submitted_at: string | null;
  answers: Record<string, string> | null;
  draft: Record<string, string> | null;
  feedback: Feedback | null;
  jobs?: Job[];
  explanation_needed: boolean;
};
export default function Practice() {
  const auth = useProtected();
 const [pendingSync,setPendingSync]=useState(false),[draftConflict,setDraftConflict]=useState(false);
 const expiredOnce=useRef("");
 const userRef=useRef("");userRef.current=auth.user?.id||"";
 const draftBase=useRef(0),pendingSubmission=useRef<PendingSubmission|null>(null);
  const [modelOptions, setModelOptions] = useState<ModelOptions | null>(null);
  const [modelSelection, setModelSelection] = useState<ModelSelection>({
    model_id: 0,
    reviewer_ids: [],
    lead_model_id: 0,
  });
  const [modelStatus, setModelStatus] = useState("");
  const modelsChanged = useRef(false);
  const [catalog, setCatalog] = useState<Catalog | null>(null),
    [subject, setSubject] = useState("english"),
    [kind, setKind] = useState(""),
    [textbook, setTextbook] = useState(""),
    [term, setTerm] = useState(""),
    [edition, setEdition] = useState(""),
    [volume, setVolume] = useState(""),
    [chapters, setChapters] = useState<string[]>([]),
    [attempt, setAttempt] = useState<Attempt | null>(null),
    [answers, setAnswers] = useState<Record<string, string>>({}),
    [job, setJob] = useState<Job | null>(null),
    [step, setStep] = useState(""),
    [question, setQuestion] = useState(""),
    [chat, setChat] = useState<{ question: string; answer: string }[]>([]),
    [personal, setPersonal] = useState(false),
    [error, setError] = useState(""),
    [explainError, setExplainError] = useState(""),
    [busy, setBusy] = useState(false),
    [seconds, setSeconds] = useState<number | null>(null),
    [saving, setSaving] = useState("");
  const questionPanel = useRef<HTMLElement>(null),
    feedbackPanel = useRef<HTMLDivElement>(null);
  const offset = useRef(0),
    draftVersion = useRef(0),
    answersRef = useRef(answers),
    submitRef = useRef(false),
    dirty = useRef(false),
    attemptRef = useRef<Attempt | null>(null),
    explanationRequested = useRef(""),
    nextRequest = useRef(""),
    submissionRequest = useRef("");
  answersRef.current = answers;
  attemptRef.current = attempt;
  useEffect(() => {
    if (!attempt) return;
    const panel = attempt.submitted_at
      ? feedbackPanel.current
      : questionPanel.current;
    panel?.scrollIntoView({ block: "start" });
    panel?.querySelector<HTMLElement>("h2,h3")?.focus({ preventScroll: true });
  }, [attempt?.id, !!attempt?.submitted_at]);
  function install(a: Attempt) {
    if (a.id !== attemptRef.current?.id) submissionRequest.current = "";
    setSubject(a.question.subject_id);
    setKind(a.question.kind);
    setAttempt(a);
    draftVersion.current=a.draft_version||0;draftBase.current=a.draft_version||0;
    const local=!a.exam_session_id?readDraft(userRef.current,a.id):null;
    let restored=a.answers||a.draft||{};
    dirty.current=false;setDraftConflict(false);pendingSubmission.current=null;setPendingSync(false);setSaving("");
    if(a.submitted_at||a.exam_session_id){removeDraft(userRef.current,a.id)}
    else if(local){
      if(local.pending_submission){pendingSubmission.current=local.pending_submission;submissionRequest.current=local.pending_submission.client_request_id;setPendingSync(true);restored=local.pending_submission.answers;setSaving("上次交卷尚待确认，请重试或核对服务器记录");}
      else if(local.base_version===draftBase.current){restored=local.answers;draftVersion.current=local.version;dirty.current=true;setSaving("已恢复本机草稿，等待与服务器同步");}
      else if(local.version===draftBase.current&&sameAnswers(local.answers,a.draft||{})){removeDraft(userRef.current,a.id)}
      else{setDraftConflict(true);setSaving("本机与服务器草稿不同，请核对后选择。");}
    }
    setAnswers(restored);answersRef.current=restored;
    offset.current=Date.parse(a.server_time)-Date.now();
    const existing = a.jobs?.filter((j) => j.kind === "explanation").at(-1);
    if (existing) {
      explanationRequested.current = a.id;
      void api<Job>(`/learning/jobs/${existing.id}`)
        .then(setJob)
        .catch((e) => setExplainError(e.message));
    }
  }
  useEffect(() => {
    if (!auth.allowed) return;
    const params = new URLSearchParams(window.location.search);
    const requested = params.get("subject");
    if (subjects.some((s) => s.id === requested)) setSubject(requested!);
    api<ModelOptions>("/learning/model-options")
      .then((data) => {
        setModelOptions(data);
        setModelSelection(data.selection);
      })
      .catch((e) => setModelStatus(e.message));
    api<Catalog>("/learning/catalog")
      .then(setCatalog)
      .catch((e) => setError(e.message));
    const id = params.get("attempt");
    if (id)
      api<Attempt>(`/learning/attempts/${encodeURIComponent(id)}`)
        .then(install)
        .catch((e) => setError(e.message));
  }, [auth.allowed]);
  const types = catalog?.subjects.find((s) => s.id === subject)?.types || [];
  useEffect(() => {
    if (types.length && !types.some((t) => t.id === kind)) setKind(types[0].id);
  }, [types, kind]);
  useEffect(() => {
    setChapters([]);
    setTextbook("");
    setTerm("");
    setEdition("");
    setVolume("");
  }, [subject]);
  useEffect(() => {
    if (!modelsChanged.current) return;
    const timer = setTimeout(() => {
      setModelStatus("正在保存选择…");
      void api("/learning/model-preferences", modelSelection)
        .then(() => setModelStatus("模型选择已保存"))
        .catch((e) => setModelStatus(e.message));
    }, 500);
    return () => clearTimeout(timer);
  }, [modelSelection]);
  function chooseModels(value: ModelSelection) {
    modelsChanged.current = true;
    setModelSelection(value);
  }
  const requestExplain = useCallback(
    async (a: Attempt, reason: "incorrect" | "self_report", force = false) => {
      if (!a.submitted_at || (explanationRequested.current === a.id && !force))
        return;
      explanationRequested.current = a.id;
      setExplainError("");
      try {
        const result = await api<{ id: string }>(
          `/learning/attempts/${a.id}/explanation`,
          {
            personal,
            reason,
            model_selection: modelOptions ? modelSelection : undefined,
          },
        );
        let next = await api<Job>(`/learning/jobs/${result.id}`);
        if (next.status === "failed") {
          await api(`/learning/jobs/${result.id}/retry`, {});
          next = { ...next, status: "queued", error: "" };
        }
        if (attemptRef.current?.id === a.id) setJob(next);
      } catch (e) {
        if (attemptRef.current?.id === a.id)
          setExplainError((e as Error).message);
      }
    },
    [personal, modelSelection, modelOptions],
  );
  useEffect(() => {
    if (attempt?.submitted_at && attempt.explanation_needed && modelOptions)
      void requestExplain(attempt, "incorrect");
  }, [
    attempt?.id,
    attempt?.submitted_at,
    attempt?.explanation_needed,
    modelOptions,
    requestExplain,
  ]);
  useEffect(() => {
    if (
      !job ||
      job.status === "failed" ||
      (job.status === "completed" && (!attempt?.submitted_at || job.result))
    )
      return;
    let active = true;
    const timer = setInterval(
      () =>
        api<Job>(`/learning/jobs/${job.id}`)
          .then((j) => {
            if (active) setJob(j);
          })
          .catch((e) => {
            if (active) setExplainError(e.message);
          }),
      2000,
    );
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [job?.id, job?.status, !!job?.result, attempt?.submitted_at]);
  const gradeFailed = !!attempt?.jobs?.some(
    (j) => j.kind !== "explanation" && j.status === "failed",
  );
  useEffect(() => {
    if (!attempt?.submitted_at || !attempt.feedback?.needs_ai || gradeFailed)
      return;
    let active = true;
    const timer = setInterval(
      () =>
        void api<Attempt>(`/learning/attempts/${attempt.id}`)
          .then((next) => {
            if (active && attemptRef.current?.id === next.id) setAttempt(next);
          })
          .catch((e) => {
            if (active) setError(e.message);
          }),
      3000,
    );
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [
    attempt?.id,
    attempt?.submitted_at,
    attempt?.feedback?.needs_ai,
    gradeFailed,
  ]);
  const saveDraft = useCallback(async () => {
    const a = attemptRef.current;
    if (!a || a.submitted_at || !dirty.current || pendingSubmission.current) return;
    const current = answersRef.current;const version=draftVersion.current;const base=draftBase.current;
    try {
      await api(`/learning/attempts/${a.id}/draft`, {
        answers: current,
        version,base_version:base,
      });
      if (attemptRef.current?.id !== a.id) return;
      draftBase.current=Math.max(draftBase.current,version);
      if (current === answersRef.current&&!pendingSubmission.current) {dirty.current=false;removeDraft(userRef.current,a.id)}
      else if(!a.exam_session_id&&!pendingSubmission.current)writeDraft({user_id:userRef.current,attempt_id:a.id,base_version:draftBase.current,version:draftVersion.current,answers:answersRef.current});
      setSaving("已保存到服务器");
    } catch (e) {
      if (attemptRef.current?.id !== a.id) return;
      setSaving((e as Error).message);
    }
  }, []);
  useEffect(() => {
    const timer = setInterval(() => void saveDraft(), 1500);
    return () => clearInterval(timer);
  }, [saveDraft]);
  useEffect(() => {
    const flush = () => {
      const a = attemptRef.current;
      if (a && !a.submitted_at && dirty.current)
        saveBeforeLeave(`/learning/attempts/${a.id}/draft`, {
          answers: answersRef.current,
          version: draftVersion.current,base_version:draftBase.current,
        });
    };
    const background = () => {
      if (document.visibilityState === "hidden") flush();
    };
    const navigation = (event: MouseEvent) => {
      if (
        event.defaultPrevented ||
        event.button !== 0 ||
        event.metaKey ||
        event.ctrlKey ||
        event.shiftKey ||
        event.altKey
      )
        return;
      if (!(event.target instanceof Element)) return;
      const link = event.target.closest<HTMLAnchorElement>("a[href]");
      if (
        link &&
        link.target !== "_blank" &&
        link.origin === window.location.origin &&
        (link.pathname !== window.location.pathname ||
          link.search !== window.location.search)
      )
        flush();
    };
    window.addEventListener("pagehide", flush);
    document.addEventListener("visibilitychange", background);
    document.addEventListener("click", navigation, true);
    return () => {
      flush();
      window.removeEventListener("pagehide", flush);
      document.removeEventListener("visibilitychange", background);
      document.removeEventListener("click", navigation, true);
    };
  }, []);
  const submit = useCallback(async () => {
    const a = attemptRef.current;
    if (!a || a.submitted_at || submitRef.current) return;
    submitRef.current = true;
    setBusy(true);
    setError("");
    try {
      if (!a.deadline || Date.now() + offset.current < Date.parse(a.deadline))
        await saveDraft();
      if (!submissionRequest.current) submissionRequest.current = requestID();
      const body=pendingSubmission.current||{client_request_id:submissionRequest.current,answers:answersRef.current,personal,model_selection:modelOptions?modelSelection:undefined};
      if(!a.exam_session_id){pendingSubmission.current=body;setPendingSync(true);writeDraft({user_id:userRef.current,attempt_id:a.id,base_version:draftBase.current,version:Math.max(1,draftVersion.current),answers:answersRef.current,pending_submission:body})}
      const next = await api<Attempt>(`/learning/attempts/${a.id}/submit`,body);
      install(next);
      if (job?.id) setJob(await api<Job>(`/learning/jobs/${job.id}`));
    } catch (e) {
      if(e instanceof APIError&&e.status===400){pendingSubmission.current=null;setPendingSync(false);writeDraft({user_id:userRef.current,attempt_id:a.id,base_version:draftBase.current,version:Math.max(1,draftVersion.current),answers:answersRef.current})}
      setError((e as Error).message);
    } finally {
      submitRef.current = false;
      setBusy(false);
    }
  }, [personal, modelSelection, modelOptions, job?.id, saveDraft]);
  useEffect(() => {
    if (!attempt?.deadline || attempt.submitted_at) {
      setSeconds(null);
      return;
    }
    const tick = () => {
      const left =
        (Date.parse(attempt.deadline!) - Date.now() - offset.current) / 1000;
      setSeconds(Math.max(0, left));
      if (left <= 0&&expiredOnce.current!==attempt.id){expiredOnce.current=attempt.id;void submit();}
    };
    tick();
    const timer = setInterval(tick, 300);
    return () => clearInterval(timer);
  }, [attempt?.id, attempt?.deadline, attempt?.submitted_at, submit]);
  useEffect(()=>{const online=()=>{if(pendingSubmission.current)void submit();else void saveDraft()};window.addEventListener("online",online);return()=>window.removeEventListener("online",online)},[submit,saveDraft]);
  async function start() {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      if (!nextRequest.current) nextRequest.current = requestID();
      const a = await api<Attempt>("/learning/attempts", {
        subject_id: subject,
        kind,
        chapter_ids: chapters,
        textbook,
        term,
        edition,
        volume,
        request_id: nextRequest.current,
      });
      nextRequest.current = "";
      setJob(null);
      setExplainError("");
      explanationRequested.current = "";
      install(a);
      setChat([]);
      setStep("");
      window.history.replaceState(
        null,
        "",
        `/practice?subject=${subject}&attempt=${a.id}`,
      );
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  function answer(id: string, text: string) {
    if(pendingSubmission.current)return;
    submissionRequest.current = "";
    draftVersion.current++;
    const next={...answersRef.current,[id]:text};setAnswers(next);answersRef.current=next;
    const a=attemptRef.current;if(a&&!a.exam_session_id)writeDraft({user_id:userRef.current,attempt_id:a.id,base_version:draftBase.current,version:draftVersion.current,answers:next});
    dirty.current = true;
    setSaving("等待保存…");
  }
  async function ask() {
    if (!attempt || !job || !step || !question.trim() || busy) return;
    setBusy(true);
    setError("");
    const text = question;
    try {
      const result = await api<{ answer: string }>(
        `/learning/attempts/${attempt.id}/chat`,
        {
          job_id: job.id,
          step_id: step,
          content: text,
          request_id: requestID(),
          personal,
          model_selection: modelOptions ? modelSelection : undefined,
        },
      );
      setChat((v) => [...v, { question: text, answer: result.answer }]);
      setQuestion("");
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  async function importText(file: File) {
    setError("");
    try {
      const data = new FormData();
      data.append("file", file);
      const result = await upload<{ text: string }>(
        "/learning/text-import",
        data,
      );
      const id = attempt?.question.content.parts[0]?.id;
      if (id) answer(id, result.text);
    } catch (e) {
      setError((e as Error).message);
    }
  }
  if (!auth.allowed) return <Loading />;
  const current = subjects.find((s) => s.id === subject)!;
  const available =
    catalog?.chapters.filter(
      (c) =>
        c.subject_id === subject &&
        (!textbook || c.textbook === textbook) &&
        (!term || c.term === term) &&
        (!edition || c.edition === edition) &&
        (!volume || c.volume === volume),
    ) || [];
  const submitted = !!attempt?.submitted_at;
  const timeline = job?.result as Timeline | undefined;
  const essay =
    attempt?.question.subject_id === "chinese" &&
    attempt.question.kind === "essay";
  return (
    <main className="shell">
      <Navigation />
      <div id="page-content" tabIndex={-1} className="practice-layout">
        <header className="page-intro">
          <span className="eyebrow">广东高中 · 专项练习</span>
          <h1>
            {attempt
              ? subjects.find((s) => s.id === attempt.question.subject_id)?.name
              : current.name}
            ，一步步学懂。
          </h1>
          <p>
            {!attempt
              ? "先选题型与范围，再开始专注练习。"
              : submitted
                ? "先看反馈。答错会生成动态讲解，没理解也可以主动求助。"
                : "安心作答，草稿自动保存。提交后再看反馈与解析。"}
          </p>
          <ol className="practice-steps" aria-label="练习进度">
            {["选择范围", "独立作答", "反馈与讲解"].map((label, i) => (
              <li
                key={label}
                aria-current={
                  i === (!attempt ? 0 : submitted ? 2 : 1) ? "step" : undefined
                }
              >
                <span>{i + 1}</span>
                {label}
              </li>
            ))}
          </ol>
        </header>
        <div className="subject-tabs" role="group" aria-label="选择科目">
          {subjects.map((s) => (
            <button
              key={s.id}
              className={subject === s.id ? "primary" : "quiet"}
              disabled={busy || (!!attempt && !submitted)}
              onClick={() => {
                setSubject(s.id);
                setAttempt(null);
                setJob(null);
              }}
            >
              {s.name}
            </button>
          ))}
        </div>
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        {pendingSync&&attempt&&!submitted&&<section className="panel glass"><p>这次交卷尚未确认，重试沿用原请求和答案。</p><div className="button-row"><button disabled={busy} onClick={()=>void submit()}>重试交卷</button><button disabled={busy} onClick={()=>void api<Attempt>(`/learning/attempts/${attempt.id}`).then(install).catch(e=>setError(e.message))}>核对服务器记录</button><button disabled={busy} onClick={()=>{removeDraft(userRef.current,attempt.id);pendingSubmission.current=null;setPendingSync(false);void api<Attempt>(`/learning/attempts/${attempt.id}`).then(install).catch(e=>setError(e.message))}}>使用服务器草稿</button></div></section>}
        {draftConflict&&attempt&&<section className="panel glass"><p>服务器保留了另一份草稿。本机草稿尚未覆盖它。</p><button onClick={()=>{const local=readDraft(userRef.current,attempt.id);if(local){const next=local.answers;setAnswers(next);answersRef.current=next;draftVersion.current=Math.max(draftVersion.current,local.version)+1;dirty.current=true;setDraftConflict(false)}}}>确认采用本机草稿</button><button onClick={()=>{removeDraft(userRef.current,attempt.id);setDraftConflict(false)}}>保留服务器草稿</button></section>}
        {!attempt && (
          <div className="practice-setup">
            <section className="panel glass filter-panel">
              <h2>今天练什么？</h2>
              <ExamStructure
                subject={catalog?.subjects.find((s) => s.id === subject)}
              />
              {catalog?.subjects.find((s) => s.id === subject)?.curriculum && (
                <p className="setting-note">
                  {
                    catalog.subjects.find((s) => s.id === subject)!.curriculum!
                      .note
                  }
                </p>
              )}
              <div className="learning-filters">
                {catalog?.subjects.find((s) => s.id === subject)
                  ?.curriculum && (
                  <>
                    <label>
                      教材版本
                      <select
                        aria-label="教材版本"
                        value={edition}
                        onChange={(e) => {
                          setEdition(e.target.value);
                          setTextbook("");
                          setVolume("");
                          setChapters([]);
                        }}
                      >
                        <option value="">
                          {subject === "english"
                            ? "北师大版2019及待核对旧资料"
                            : "全部版本"}
                        </option>
                        {Array.from(
                          new Set([
                            ...(subject === "english" ? ["北师大版2019"] : []),
                            ...(catalog.chapters
                              .filter(
                                (c) =>
                                  c.subject_id === subject &&
                                  (subject !== "english" ||
                                    c.edition === "北师大版2019"),
                              )
                              .map((c) => c.edition)
                              .filter(Boolean) as string[]),
                          ]),
                        ).map((v) => (
                          <option key={v}>{v}</option>
                        ))}
                      </select>
                    </label>
                    <label>
                      教材册次
                      <select
                        aria-label="教材册次"
                        value={volume}
                        onChange={(e) => {
                          setVolume(e.target.value);
                          setTextbook("");
                          setChapters([]);
                        }}
                      >
                        <option value="">全部册次</option>
                        {Array.from(
                          new Set([
                            ...(catalog.subjects.find((s) => s.id === subject)
                              ?.curriculum?.volumes || []),
                            ...(catalog.chapters
                              .filter((c) => c.subject_id === subject)
                              .map((c) => c.volume)
                              .filter(Boolean) as string[]),
                          ]),
                        ).map((v) => (
                          <option key={v} value={v}>
                            {v} ·{" "}
                            {catalog.chapters
                              .filter(
                                (c) =>
                                  c.subject_id === subject &&
                                  c.volume === v &&
                                  (!edition || c.edition === edition),
                              )
                              .reduce(
                                (n, c) => n + Number(c.question_count),
                                0,
                              )}
                            题
                          </option>
                        ))}
                      </select>
                    </label>
                  </>
                )}
                <label>
                  题型
                  <select
                    value={kind}
                    onChange={(e) => setKind(e.target.value)}
                  >
                    {types.map((t) => (
                      <option value={t.id} key={t.id}>
                        {t.name}
                        {catalog?.subjects.find((s) => s.id === subject)
                          ?.published_counts
                          ? ` · ${catalog.subjects.find((s) => s.id === subject)!.published_counts![t.id] || 0}题`
                          : ""}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  教材范围
                  <select
                    value={textbook}
                    onChange={(e) => {
                      setTextbook(e.target.value);
                      setChapters([]);
                    }}
                  >
                    <option value="">全部教材</option>
                    {Array.from(
                      new Set(
                        catalog?.chapters
                          .filter((c) => c.subject_id === subject)
                          .map((c) => c.textbook)
                          .filter(Boolean),
                      ),
                    ).map((v) => (
                      <option key={v}>{v}</option>
                    ))}
                  </select>
                </label>
                <label>
                  学期
                  <select
                    value={term}
                    onChange={(e) => {
                      setTerm(e.target.value);
                      setChapters([]);
                    }}
                  >
                    <option value="">全部学期</option>
                    {Array.from(
                      new Set(
                        catalog?.chapters
                          .filter((c) => c.subject_id === subject)
                          .map((c) => c.term)
                          .filter(Boolean),
                      ),
                    ).map((v) => (
                      <option key={v}>{v}</option>
                    ))}
                  </select>
                </label>
              </div>
              <fieldset className="chapter-picker">
                <legend>练习章节 · 可多选，未选则使用全部范围</legend>
                {available
                  .filter((c) => Number(c.question_count) > 0)
                  .map((c) => (
                    <label key={c.id}>
                      <input
                        type="checkbox"
                        checked={chapters.includes(c.id)}
                        onChange={(e) =>
                          setChapters((v) =>
                            e.target.checked
                              ? [...v, c.id]
                              : v.filter((id) => id !== c.id),
                          )
                        }
                      />
                      <span>
                        {c.curriculum_chapter
                          ? `${c.curriculum_chapter} / `
                          : ""}
                        {c.title}
                        <small>
                          {c.textbook} · {c.question_count}题
                        </small>
                      </span>
                    </label>
                  ))}
                {!available.some((c) => Number(c.question_count) > 0) && (
                  <p className="empty-state">
                    这个范围还没有已发布题目。
                    {auth.user?.role === "admin" && (
                      <Link href="/admin/learning">去导入教学资料</Link>
                    )}
                  </p>
                )}
              </fieldset>
              <p className="range-summary">
                {types.find((t) => t.id === kind)?.name || "选择题型"} ·{" "}
                {chapters.length
                  ? `已选${chapters.length}个章节`
                  : "当前教材与学期的全部章节"}
              </p>
              {subject === "chinese" && (
                <p className="setting-note">
                  平台训练：作文30分钟，有效汉字超过850；阅读30分钟。上传Word或TXT也计入训练时间。
                </p>
              )}
              <button
                className="primary"
                disabled={
                  busy ||
                  !kind ||
                  !available.some((c) => Number(c.question_count) > 0)
                }
                onClick={start}
              >
                {busy ? "正在选题…" : "开始练习"}
              </button>
              {subject === "english" && (
                <p className="section-links">
                  <Link href="/english">词汇复习</Link> ·{" "}
                  <Link href="/challenge">极速四选一</Link> ·{" "}
                  <Link href="/assistant">长句与听力</Link>
                </p>
              )}
            </section>
            <aside className="panel glass model-setup">
              <AdaptivePanel title="模型与身份组">
                <LearningModelPicker
                  options={modelOptions}
                  value={modelSelection}
                  onChange={chooseModels}
                  personal={personal}
                  onPersonal={setPersonal}
                  essay={subject === "chinese" && kind === "essay"}
                  status={modelStatus}
                />
              </AdaptivePanel>
            </aside>
          </div>
        )}
        {attempt && (
          <>
            {submitted && <MistakePanel attemptId={attempt.id} subject={attempt.question.subject_id} />}
            {submitted && <RecallCardCreator attemptId={attempt.id} />}
            {submitted && <ScoreEvidence evidence={attempt.score_evidence||[]} />}
            {submitted && <GuidedTutor attemptId={attempt.id} selection={modelSelection} personal={personal} />}
            {submitted && (
              <nav className="review-jumps" aria-label="本题复习">
                <a href="#practice-feedback">查看作答反馈</a>
                <a href="#practice-explanation">查看动态讲解</a>
              </nav>
            )}
            <div className="practice-context">
              <div>
                <strong>
                  {types.find((t) => t.id === attempt.question.kind)?.name ||
                    attempt.question.kind}
                </strong>
                <span>
                  {" "}
                  · 已作答{" "}
                  {Object.values(answers).filter((v) => v.trim()).length} /{" "}
                  {attempt.question.content.parts.length}
                </span>
              </div>
              <details>
                <summary>AI 模型与身份组</summary>
                <LearningModelPicker
                  options={modelOptions}
                  value={modelSelection}
                  onChange={chooseModels}
                  personal={personal}
                  onPersonal={setPersonal}
                  essay={!!essay}
                  status={modelStatus}
                />
              </details>
            </div>
            <div
              className={
                submitted ? "practice-session reviewed" : "practice-session"
              }
            >
              <section
                className="panel glass question-panel"
                ref={questionPanel}
              >
                <div className="question-heading">
                  <span className="eyebrow">
                    {submitted ? "练习反馈" : "专注这一题"}
                  </span>
                  {seconds !== null && (
                    <strong
                      className={`practice-timer ${seconds < 60 ? "urgent" : ""}`}
                      role="timer"
                      aria-label="剩余时间"
                    >
                      {timeLabel(seconds)}
                    </strong>
                  )}
                </div>
                <h2 tabIndex={-1}>
                  <MathText text={attempt.question.content.text} />
                </h2>
                {attempt.question.content.passage && (
                  <div className="reading-passage">
                    <MathText text={attempt.question.content.passage} />
                  </div>
                )}
                {attempt.question.assets.map((a) => (
                  <LearningFigure
                    key={a.id}
                    src={`/api/v1/learning/assets/${a.id}`}
                    alt={a.alt}
                  />
                ))}
                {essay && !submitted && (
                  <label className="file-button">
                    导入 .docx / .txt
                    <input
                      type="file"
                      accept=".docx,.txt"
                      onChange={(e) => {
                        if (e.target.files?.[0])
                          void importText(e.target.files[0]);
                        e.target.value = "";
                      }}
                    />
                  </label>
                )}
                {attempt.question.content.parts.map((p) => (
                  <fieldset className="question-part" key={p.id}>
                    <legend>
                      {p.id}. <MathText text={p.text} />{" "}
                      <small>{p.points}分</small>
                    </legend>
                    {p.choices?.length ? (
                      <div className="question-choices">
                        {p.choices.map((c) => (
                          <label
                            key={c.id}
                            className={
                              submitted &&
                              (attempt.question.solution.answers[p.id] || "")
                                .split(",")
                                .map((v) => v.trim())
                                .includes(c.id)
                                ? "correct-choice"
                                : (answers[p.id] || "")
                                      .split(",")
                                      .includes(c.id)
                                  ? submitted
                                    ? "incorrect-choice"
                                    : "picked"
                                  : ""
                            }
                          >
                            <input
                              type={
                                attempt.question.kind === "multiple_choice"
                                  ? "checkbox"
                                  : "radio"
                              }
                              name={`part-${p.id}`}
                              disabled={submitted || busy || pendingSync}
                              checked={(answers[p.id] || "")
                                .split(",")
                                .includes(c.id)}
                              onChange={(e) => {
                                if (
                                  attempt.question.kind === "multiple_choice"
                                ) {
                                  const v = (answers[p.id] || "")
                                    .split(",")
                                    .filter(Boolean)
                                    .filter((id) => id !== c.id);
                                  if (e.target.checked) v.push(c.id);
                                  answer(p.id, v.sort().join(","));
                                } else answer(p.id, c.id);
                              }}
                            />
                            <span>
                              <b>{c.id}</b> <MathText text={c.text} />
                            </span>
                          </label>
                        ))}
                      </div>
                    ) : (
                      <textarea
                        aria-label={`第${p.id}题作答`}
                        rows={essay ? 18 : 5}
                        value={answers[p.id] || ""}
                        disabled={submitted || busy || pendingSync}
                        maxLength={12000}
                        onChange={(e) => answer(p.id, e.target.value)}
                        placeholder="写下你的思路与答案…"
                      />
                    )}
                    {submitted && (
                      <p className="reference-answer">
                        参考答案：
                        <MathText
                          text={attempt.question.solution.answers[p.id]}
                        />
                      </p>
                    )}
                  </fieldset>
                ))}
                {essay && (
                  <p className="word-counter">
                    有效汉字 {hanCount(Object.values(answers).join(""))} /
                    至少851 <span>标点、数字、字母不计入</span>
                  </p>
                )}
                {!submitted ? (
                  <div className="question-submit">
                    <button
                      className="primary"
                      disabled={busy}
                      onClick={() => void submit()}
                    >
                      {busy ? "正在交卷…" : "提交作答"}
                    </button>
                    <span className="setting-note" role="status">
                      {saving}
                    </span>
                  </div>
                ) : (
                  <>
                    <div
                      className="learning-feedback"
                      id="practice-feedback"
                      ref={feedbackPanel}
                    >
                      <h3 tabIndex={-1}>
                        {attempt.feedback?.incomplete
                          ? "作文未达到训练字数"
                          : attempt.feedback?.needs_ai
                            ? "已交卷，正在等待 AI 评价"
                            : `参考得分 ${attempt.feedback?.score ?? 0} / ${attempt.feedback?.max_score ?? 0}`}
                      </h3>
                      {attempt.feedback?.timed_out && (
                        <p>训练时间已结束，使用截止前保存的作答。</p>
                      )}
                      {attempt.feedback?.ai_reference && (
                        <p className="setting-note">
                          AI 训练参考评分，请结合教师意见。
                        </p>
                      )}
                      {attempt.feedback?.ai_grading && (
                        <>
                          <p>{attempt.feedback.ai_grading.feedback}</p>
                          {attempt.feedback.ai_grading.parts.map((p) => (
                            <p key={p.id}>
                              {p.id}：{p.score}分 · {p.reason}
                              <br />
                              错因：{p.mistake}
                            </p>
                          ))}
                        </>
                      )}
                      {attempt.feedback?.essay_review && (
                        <>
                          <p>{attempt.feedback.essay_review.reason}</p>
                          <p>{attempt.feedback.essay_review.disagreement}</p>
                          <ul>
                            {attempt.feedback.essay_review.suggestions.map(
                              (s, i) => (
                                <li key={i}>{s}</li>
                              ),
                            )}
                          </ul>
                          <details>
                            <summary>查看独立评审</summary>
                            {attempt.feedback.independent_reviews?.map(
                              (v, i) => (
                                <article key={i}>
                                  <h4>
                                    评审{i + 1} · {v.score}分
                                  </h4>
                                  <p>{v.reason}</p>
                                  {v.evidence.map((s, j) => (
                                    <blockquote key={j}>{s}</blockquote>
                                  ))}
                                  <ul>
                                    {v.suggestions.map((s, j) => (
                                      <li key={j}>{s}</li>
                                    ))}
                                  </ul>
                                </article>
                              ),
                            )}
                          </details>
                        </>
                      )}
                      {attempt.jobs
                        ?.filter(
                          (j) =>
                            j.status === "failed" && j.kind !== "explanation",
                        )
                        .map((j) => (
                          <p key={j.id}>
                            {j.error}
                            <button
                              className="quiet"
                              onClick={() =>
                                api(`/learning/jobs/${j.id}/retry`, {
                                  personal,
                                  model_selection: modelOptions
                                    ? modelSelection
                                    : undefined,
                                })
                                  .then(() =>
                                    api<Attempt>(
                                      `/learning/attempts/${attempt.id}`,
                                    ),
                                  )
                                  .then((next) => {
                                    if (attemptRef.current?.id === next.id)
                                      setAttempt(next);
                                  })
                                  .catch((e) => setError(e.message))
                              }
                            >
                              重试评价
                            </button>
                          </p>
                        ))}
                    </div>
                    <details open>
                      <summary>参考解析</summary>
                      <p className="preserve-text">
                        <MathText
                          text={attempt.question.solution.explanation}
                        />
                      </p>
                      <p>
                        <MathText text={attempt.question.solution.rubric} />
                      </p>
                    </details>
                    <div className="question-submit">
                      <button
                        className="primary"
                        disabled={busy}
                        onClick={() => void start()}
                      >
                        {busy ? "正在选题…" : "再练一道"}
                      </button>{" "}
                      <button
                        className="quiet"
                        disabled={busy}
                        onClick={() => {
                          setAttempt(null);
                          setJob(null);
                          explanationRequested.current = "";
                          setExplainError("");
                          window.history.replaceState(
                            null,
                            "",
                            `/practice?subject=${subject}`,
                          );
                        }}
                      >
                        调整练习范围
                      </button>
                    </div>
                  </>
                )}
              </section>
              {submitted && (
                <section
                  className="panel glass dynamic-panel"
                  id="practice-explanation"
                >
                  <div className="panel-heading">
                    <h2>一步步，看懂思路。</h2>
                    <span className="access-badge">按需讲解</span>
                  </div>
                  {!job && !explainError && (
                    <p className="setting-note">
                      {attempt.explanation_needed
                        ? "这道题有失分，正在准备讲解。"
                        : "觉得还没学会？点下方按钮，再用动态标注看懂关键步骤。"}
                    </p>
                  )}
                  {timeline ? (
                    <>
                      <TimelinePlayer
                        timeline={timeline}
                        assets={attempt.question.assets}
                        onStep={setStep}
                      />
                      <section className="embedded-tutor">
                        <h3>这一步，还没理解？</h3>
                        <p className="setting-note">
                          只围绕当前题目和选中的步骤追问。
                        </p>
                        {chat.map((v, i) => (
                          <div key={i} className="step-dialogue">
                            <p>
                              <strong>你：</strong>
                              {v.question}
                            </p>
                            <p className="preserve-text">
                              <strong>AI：</strong>
                              {v.answer}
                            </p>
                          </div>
                        ))}
                        <form
                          onSubmit={(e) => {
                            e.preventDefault();
                            void ask();
                          }}
                        >
                          <label>
                            对当前步骤的疑问
                            <textarea
                              value={question}
                              onChange={(e) => setQuestion(e.target.value)}
                              maxLength={2000}
                              rows={3}
                              placeholder="例如：为什么这部分是定语，而不是主句？"
                            />
                          </label>
                          <button
                            className="primary"
                            disabled={busy || !question.trim() || !step}
                          >
                            问问 AI
                          </button>
                        </form>
                      </section>
                    </>
                  ) : (
                    <div className="explanation-request">
                      {(job || explainError) && (
                        <p role="status">
                          {job?.status === "failed"
                            ? job.error
                            : explainError || "正在生成这道题的动态讲解…"}
                        </p>
                      )}
                      {(!job || job.status === "failed" || explainError) && (
                        <button
                          className="primary"
                          disabled={busy || !modelOptions}
                          onClick={() =>
                            void requestExplain(attempt, "self_report", true)
                          }
                        >
                          {job?.status === "failed" || explainError
                            ? "重新生成讲解"
                            : "没理解，动态讲解"}
                        </button>
                      )}
                    </div>
                  )}
                </section>
              )}
            </div>
          </>
        )}
      </div>
    </main>
  );
}
