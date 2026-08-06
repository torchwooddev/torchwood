import { Torchwood, TorchwoodError } from "@torchwood/sdk";
import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import {
  loadAuth,
  loadSettings,
  saveAuth,
  saveSettings,
  type AppSettings,
  type AuthState,
} from "./storage";

interface TorchwoodContextValue {
  settings: AppSettings;
  auth: AuthState | null;
  client: Torchwood;
  updateSettings: (next: AppSettings) => void;
  setAuth: (next: AuthState | null) => void;
  serverClient: () => Torchwood;
  lastError: string | null;
  setLastError: (msg: string | null) => void;
  run: <T>(fn: () => Promise<T>) => Promise<T>;
}

const TorchwoodContext = createContext<TorchwoodContextValue | null>(null);

function buildClient(settings: AppSettings, auth: AuthState | null): Torchwood {
  return Torchwood.create({
    endpoint: settings.endpoint,
    projectId: settings.projectId,
    accessToken: auth?.accessToken,
  });
}

export function TorchwoodProvider({ children }: { children: ReactNode }) {
  const [settings, setSettingsState] = useState<AppSettings>(() => loadSettings());
  const [auth, setAuthState] = useState<AuthState | null>(() => loadAuth());
  const [lastError, setLastError] = useState<string | null>(null);

  const client = useMemo(() => buildClient(settings, auth), [settings, auth]);

  const updateSettings = useCallback((next: AppSettings) => {
    saveSettings(next);
    setSettingsState(next);
  }, []);

  const setAuth = useCallback((next: AuthState | null) => {
    saveAuth(next);
    setAuthState(next);
    if (next) {
      client.setAccessToken(next.accessToken);
    } else {
      client.setAccessToken(undefined);
    }
  }, [client]);

  const serverClient = useCallback(() => {
    if (!settings.apiKey) {
      throw new TorchwoodError("请先在设置页填写 Server API Key", 0);
    }
    return Torchwood.withApiKey(settings.endpoint, settings.projectId, settings.apiKey);
  }, [settings]);

  const run = useCallback(async <T,>(fn: () => Promise<T>): Promise<T> => {
    setLastError(null);
    try {
      return await fn();
    } catch (err) {
      const message =
        err instanceof TorchwoodError
          ? `[${err.status}] ${err.message}`
          : err instanceof Error
            ? err.message
            : String(err);
      setLastError(message);
      throw err;
    }
  }, []);

  const value = useMemo(
    () => ({
      settings,
      auth,
      client,
      updateSettings,
      setAuth,
      serverClient,
      lastError,
      setLastError,
      run,
    }),
    [settings, auth, client, updateSettings, setAuth, serverClient, lastError, run]
  );

  return <TorchwoodContext.Provider value={value}>{children}</TorchwoodContext.Provider>;
}

export function useTorchwood() {
  const ctx = useContext(TorchwoodContext);
  if (!ctx) throw new Error("useTorchwood must be used within TorchwoodProvider");
  return ctx;
}
