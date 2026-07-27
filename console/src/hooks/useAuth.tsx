import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
} from "react";
import { getAuthToken, getProjectID, setProjectID } from "@/api/client";
import { login as apiLogin, logout as apiLogout } from "@/api/auth";

interface AuthContextValue {
  token: string | null;
  projectId: string | null;
  isAuthenticated: boolean;
  login: (email: string, password: string) => Promise<string>;
  logout: () => void;
  selectProject: (projectID: string) => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [token, setToken] = useState<string | null>(() => getAuthToken());
  const [projectId, setProjectIdState] = useState<string | null>(() => getProjectID());

  const login = useCallback(async (email: string, password: string) => {
    const t = await apiLogin({ email, password });
    setToken(t);
    return t;
  }, []);

  const logout = useCallback(() => {
    // Fire-and-forget: apiLogout revokes the session server-side (best-effort)
    // and clears stored tokens; clear local state immediately either way.
    void apiLogout();
    setProjectID(null);
    setProjectIdState(null);
    setToken(null);
  }, []);

  const selectProject = useCallback((projectID: string) => {
    setProjectID(projectID);
    setProjectIdState(projectID);
  }, []);

  const currentToken = token ?? getAuthToken();

  const value = useMemo(
    () => ({
      token: currentToken,
      projectId,
      isAuthenticated: !!currentToken,
      login,
      logout,
      selectProject,
    }),
    [currentToken, projectId, login, logout, selectProject]
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
