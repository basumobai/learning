"use client";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
} from "react";
import { usePathname, useRouter } from "next/navigation";
import Link from "next/link";
import { api, authState, setCSRF, type User } from "../lib/api";

const AuthContext = createContext<{
  user: User | null;
  ready: boolean;
  refresh: () => Promise<void>;
}>({ user: null, ready: false, refresh: async () => {} });
export function Providers({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [ready, setReady] = useState(false);
  const refresh = useCallback(async () => {
    try {
      setUser((await authState()).user);
    } catch {
      setUser(null);
    } finally {
      setReady(true);
    }
  }, []);
  useEffect(() => {
    refresh();
  }, [refresh]);
  return (
    <AuthContext.Provider value={{ user, ready, refresh }}>
      {children}
    </AuthContext.Provider>
  );
}
export function useAuth() {
  return useContext(AuthContext);
}
export function useProtected(admin = false) {
  const auth = useAuth();
  const router = useRouter();
  const path = usePathname();
  useEffect(() => {
    if (!auth.ready) return;
    if (!auth.user) router.replace("/login");
    else if (auth.user.must_change_password && path !== "/settings")
      router.replace("/settings");
    else if (admin && auth.user.role !== "admin") router.replace("/");
  }, [auth.ready, auth.user, router, path, admin]);
  return {
    ...auth,
    allowed:
      auth.ready &&
      !!auth.user &&
      (!auth.user.must_change_password || path === "/settings") &&
      (!admin || auth.user.role === "admin"),
  };
}
export function Navigation() {
  const { user } = useAuth();
  const path = usePathname();
  const router = useRouter();
  const items = [
    ["/", "今日复习"],
    ["/assistant", "AI 助学"],
    ["/vocabulary", "词库"],
    ["/settings", "账户与设置"],
    ...(user?.role === "admin" ? [["/admin", "管理"]] : []),
  ];
  async function logout() {
    try {
      await api("/auth/logout", {});
    } finally {
      setCSRF("");
      router.replace("/login");
      window.location.assign("/login");
    }
  }
  return (
    <header className="topbar glass app-topbar">
      <Link className="brand" href="/">
        <span className="mark">E</span>
        <span>
          ELS<span className="brand-detail">英语助学</span>
        </span>
      </Link>
      <nav aria-label="主要导航">
        {items.map(([href, label]) => (
          <Link
            key={href}
            href={href}
            aria-current={path === href ? "page" : undefined}
          >
            {label}
          </Link>
        ))}
      </nav>
      <div className="account-menu">
        <span>{user?.display_name}</span>
        <button className="quiet" onClick={logout}>
          退出
        </button>
      </div>
    </header>
  );
}
export function Loading() {
  return (
    <main className="shell">
      <div className="panel glass">
        <p role="status">正在读取账户与学习记录…</p>
      </div>
    </main>
  );
}
