"use client";
import {useEffect,useState} from "react";
import {usePathname} from "next/navigation";
import Link from "next/link";
import {api} from "../../lib/api";
export function OpenExamBanner({userId}:{userId:string}){const path=usePathname();const [id,setId]=useState("");useEffect(()=>{let live=true;void api<{exam:{id:string}|null}>("/learning/exams/active").then(d=>{if(live)setId(d.exam?.id||"")}).catch(()=>{});return()=>{live=false}},[userId,path]);return id&&path!=="/exams"?<aside className="exam-banner" role="status">有一份模拟卷尚未交卷。<Link href={`/exams?id=${encodeURIComponent(id)}`}>恢复模拟卷</Link></aside>:null}
