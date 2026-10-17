"use client";
import { useEffect, useState } from "react";
import { usePathname } from "next/navigation";

const sections = [
  { title: "内容与资源", items: [
    { href: "/admin/resources", label: "资源处理工作台", detail: "上传 · A/B/C 区" },
    { href: "/admin/resources/jobs", label: "任务与运行状态", detail: "Worker · 重试" },
    { href: "/admin/resources/shares", label: "网盘导入", detail: "文件筛选 · 下载" },
    { href: "/admin/engine", label: "评分与学习证据", detail: "规则 · 映射 · 质量评测" },
    { href: "/admin/resources/config", label: "教材与知识点", detail: "分类 · 预算" },
    { href: "/admin/learning", label: "旧版题库与导入", detail: "兼容功能" },
  ]},
  { title: "模型与权限", items: [
    { href: "/admin#providers", label: "AI 服务商与模型", detail: "目录 · 批量选用" },
    { href: "/admin#groups", label: "身份组授权", detail: "模型权限" },
    { href: "/admin/learning#tasks", label: "AI 任务路由", detail: "提示词 · 降级" },
  ]},
  { title: "系统管理", items: [
    { href: "/admin#users", label: "用户与安全", detail: "账户 · 注册" },
    { href: "/admin#audit", label: "操作记录", detail: "审计日志" },
  ]},
] as const;

export function AdminNavigation() {
  const pathname = usePathname();
  const [hash, setHash] = useState("");
  useEffect(() => {
    const sync = () => setHash(window.location.hash);
    sync();
    window.addEventListener("hashchange", sync);
    return () => window.removeEventListener("hashchange", sync);
  }, [pathname]);
  const activeHref = pathname === "/admin" && !hash ? "/admin#providers" : pathname + hash;
  return (
    <nav className="admin-settings-nav" aria-label="管理员功能导航">
      <div className="admin-nav-title">管理设置 <span>Learning</span></div>
      {sections.map((section) => (
        <div className="admin-nav-section" key={section.title}>
          <p className="admin-nav-heading">{section.title}</p>
          <div className="admin-nav-links">
            {section.items.map((item) => {
              const active = item.href.includes("#")
                ? activeHref === item.href
                : pathname === item.href;
              return (
                <a key={item.href} href={item.href} aria-current={active ? "page" : undefined}>
                  <strong>{item.label}</strong>
                  <small>{item.detail}</small>
                </a>
              );
            })}
          </div>
        </div>
      ))}
    </nav>
  );
}
