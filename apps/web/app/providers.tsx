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
import { Icon, type IconName } from "./icons";

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
  const items: { href: string; label: string; icon: IconName }[] = [
    { href: "/", label: "学习", icon: "book" },
    { href: "/assistant", label: "AI 助学", icon: "sparkles" },
    { href: "/vocabulary", label: "词库", icon: "library" },
    { href: "/settings", label: "设置", icon: "settings" },
    ...(user?.role === "admin"
      ? [{ href: "/admin", label: "管理", icon: "shield" as const }]
      : []),
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
    <header className="topbar app-topbar">
      <a className="skip-link" href="#page-content">
        跳到页面内容
      </a>
      <Link className="brand" href="/">
        <span className="mark">
          <Icon name="book" />
        </span>
        <span>
          Learning<span className="brand-detail">学习平台</span>
        </span>
      </Link>
      <nav aria-label="主要导航">
        {items.map(({ href, label, icon }) => (
          <Link
            key={href}
            href={href}
            aria-current={path === href ? "page" : undefined}
          >
            <Icon name={icon} />
            <span>{label}</span>
          </Link>
        ))}
      </nav>
      <div className="account-menu">
        <span className="account-avatar" aria-hidden="true">
          {user?.display_name?.slice(0, 1) || "E"}
        </span>
        <span className="account-name" title={user?.display_name}>
          {user?.display_name}
        </span>
        <button
          className="quiet logout-button"
          onClick={logout}
          aria-label="退出登录"
          title="退出登录"
        >
          <Icon name="logout" />
        </button>
      </div>
    </header>
  );
}
export function Loading() {
  return (
    <main className="shell loading-shell" aria-busy="true">
      <div className="loading-heading" aria-hidden="true">
        <span className="mark">
          <Icon name="book" />
        </span>
        <span className="skeleton skeleton-title" />
      </div>
      <p className="loading-status" role="status">
        正在读取账户与学习记录…
      </p>
      <div className="stats" aria-hidden="true">
        {[0, 1, 2].map((n) => (
          <div key={n} className="stat glass skeleton-card">
            <span className="skeleton skeleton-label" />
            <span className="skeleton skeleton-value" />
          </div>
        ))}
      </div>
      <div className="panel glass skeleton-content" aria-hidden="true">
        <span className="skeleton skeleton-title" />
        <span className="skeleton skeleton-label" />
      </div>
    </main>
  );
}
