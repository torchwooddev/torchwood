export interface AppSettings {
  endpoint: string;
  projectId: string;
  apiKey: string;
  demoDbId: string;
  demoCollId: string;
  demoDocId: string;
  demoDocVersion: number;
  demoIndexId: string;
}

export interface AuthState {
  accessToken: string;
  refreshToken: string;
  email: string;
  name: string;
  userId: string;
}

const SETTINGS_KEY = "Torchwood-demo-settings";
const AUTH_KEY = "Torchwood-demo-auth";

const defaultSettings = (): AppSettings => ({
  endpoint: import.meta.env.VITE_TORCHWOOD_ENDPOINT || "http://localhost:9080",
  projectId: import.meta.env.VITE_TORCHWOOD_PROJECT_ID || "default",
  apiKey: "",
  demoDbId: "",
  demoCollId: "posts",
  demoDocId: "",
  demoDocVersion: 0,
  demoIndexId: "",
});

export function loadSettings(): AppSettings {
  try {
    const raw = localStorage.getItem(SETTINGS_KEY);
    if (!raw) return defaultSettings();
    return { ...defaultSettings(), ...JSON.parse(raw) };
  } catch {
    return defaultSettings();
  }
}

export function saveSettings(settings: AppSettings): void {
  localStorage.setItem(SETTINGS_KEY, JSON.stringify(settings));
}

export function loadAuth(): AuthState | null {
  try {
    const raw = localStorage.getItem(AUTH_KEY);
    return raw ? (JSON.parse(raw) as AuthState) : null;
  } catch {
    return null;
  }
}

export function saveAuth(auth: AuthState | null): void {
  if (!auth) {
    localStorage.removeItem(AUTH_KEY);
    return;
  }
  localStorage.setItem(AUTH_KEY, JSON.stringify(auth));
}

export function suffix(): string {
  return Date.now().toString(36);
}

export function formatError(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}
