import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import { getProjectID, setProjectID } from "@/api/client";
import {
  login as apiLogin,
  logout as apiLogout,
  refreshSession,
} from "@/api/auth";

interface AuthContextValue {
  projectId: string | null;
  isAuthenticated: boolean;
  // loading 为 true 表示正在用 refresh cookie 探测初始会话状态。
  loading: boolean;
  login: (email: string, password: string) => Promise<void>;
  logout: () => void;
  selectProject: (projectID: string) => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  // 会话凭证在 HttpOnly cookie 里，JS 读不到；加载时用一次 refresh 探测
  // （成功即已登录并顺带续期），失败视为匿名。
  const [authenticated, setAuthenticated] = useState(false);
  const [loading, setLoading] = useState(true);
  const [projectId, setProjectIdState] = useState<string | null>(() => getProjectID());

  useEffect(() => {
    let cancelled = false;
    refreshSession()
      .then(() => {
        if (!cancelled) {
          setAuthenticated(true);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setAuthenticated(false);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const login = useCallback(async (email: string, password: string) => {
    await apiLogin({ email, password });
    setAuthenticated(true);
  }, []);

  const logout = useCallback(() => {
    // Fire-and-forget: apiLogout revokes the session and clears the cookies
    // server-side (best-effort); clear local state immediately either way.
    void apiLogout();
    setProjectID(null);
    setProjectIdState(null);
    setAuthenticated(false);
  }, []);

  const selectProject = useCallback((projectID: string) => {
    setProjectID(projectID);
    setProjectIdState(projectID);
  }, []);

  const value = useMemo(
    () => ({
      projectId,
      isAuthenticated: authenticated,
      loading,
      login,
      logout,
      selectProject,
    }),
    [projectId, authenticated, loading, login, logout, selectProject]
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error("useAuth must be used within AuthProvider");
  }
  return ctx;
}
