import axios from "axios";
import type { AxiosRequestConfig, InternalAxiosRequestConfig } from "axios";
import { toast } from "sonner";

export const api = axios.create({
  baseURL: "/v1",
  headers: {
    "Content-Type": "application/json",
  },
});

// ApiRequestConfig carries internal flags understood by the response
// interceptor: __skipAuthRetry opts a request out of the 401 refresh flow
// (e.g. sign-out), __authRetried marks a request already retried once.
export interface ApiRequestConfig extends AxiosRequestConfig {
  __skipAuthRetry?: boolean;
  __authRetried?: boolean;
}

const REFRESH_TOKEN_KEY = "graviton_console_refresh_token";

export function setAuthToken(token: string | null) {
  if (token) {
    localStorage.setItem("GRAVITON_console_token", token);
    authRedirecting = false;
  } else {
    localStorage.removeItem("GRAVITON_console_token");
  }
}

export function clearAuthToken() {
  localStorage.removeItem("GRAVITON_console_token");
}

export function getAuthToken(): string | null {
  return localStorage.getItem("GRAVITON_console_token");
}

export function getRefreshToken(): string | null {
  return localStorage.getItem(REFRESH_TOKEN_KEY);
}

export function setRefreshToken(token: string | null) {
  if (token) {
    localStorage.setItem(REFRESH_TOKEN_KEY, token);
  } else {
    localStorage.removeItem(REFRESH_TOKEN_KEY);
  }
}

// refreshAuthTokenSingleFlight refreshes the console token pair using the
// stored refresh token. Concurrent callers share one in-flight request so a
// burst of 401s triggers exactly one refresh. Uses bare axios to bypass the
// interceptors (a 401 from refresh itself must not recurse).
let refreshPromise: Promise<string> | null = null;

export function refreshAuthTokenSingleFlight(): Promise<string> {
  if (!refreshPromise) {
    refreshPromise = doRefreshToken().finally(() => {
      refreshPromise = null;
    });
  }
  return refreshPromise;
}

async function doRefreshToken(): Promise<string> {
  const refreshToken = getRefreshToken();
  if (!refreshToken) {
    throw new Error("no refresh token");
  }
  const res = await axios.post<{ access_token: string; refresh_token?: string }>(
    "/v1/console/auth/refresh",
    { refresh_token: refreshToken }
  );
  const accessToken = res.data?.access_token;
  if (!accessToken) {
    throw new Error("refresh response missing access_token");
  }
  setAuthToken(accessToken);
  if (res.data?.refresh_token) {
    setRefreshToken(res.data.refresh_token);
  }
  return accessToken;
}

export function setProjectID(projectID: string | null) {
  if (projectID) {
    localStorage.setItem("GRAVITON_console_project", projectID);
  } else {
    localStorage.removeItem("GRAVITON_console_project");
  }
}

export function getProjectID(): string | null {
  return localStorage.getItem("GRAVITON_console_project");
}

api.interceptors.request.use((config) => {
  const token = getAuthToken();
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  const projectID = getProjectID();
  if (projectID) {
    config.headers["X-Graviton-Project"] = projectID;
  }
  return config;
});

let authRedirecting = false;

function forceReLogin() {
  if (authRedirecting) {
    return;
  }
  authRedirecting = true;
  toast.error("Session expired. Please sign in again.");
  clearAuthToken();
  setRefreshToken(null);
  setProjectID(null);
  window.location.href = "/console/login";
}

api.interceptors.response.use(
  (response) => response,
  async (error) => {
    if (axios.isCancel(error)) {
      return Promise.reject(error);
    }
    const status = error?.response?.status;
    const config = error?.config as
      | (InternalAxiosRequestConfig & ApiRequestConfig)
      | undefined;
    const message =
      error?.response?.data?.error?.message ||
      error?.message ||
      "Request failed";

    if (status === 401) {
      const isLoginRequest = config?.url === "/console/auth/sign-in";
      const isMissingProject = message.includes("missing project context");
      if (isLoginRequest) {
        toast.error(message);
      } else if (
        config &&
        !config.__skipAuthRetry &&
        !config.__authRetried &&
        !isMissingProject
      ) {
        // Access token expired: refresh the token pair once (single-flight)
        // and retry the original request; only give up if refresh fails.
        try {
          await refreshAuthTokenSingleFlight();
          config.__authRetried = true;
          return api.request(config);
        } catch {
          forceReLogin();
        }
      } else if (!isMissingProject) {
        forceReLogin();
      }
    } else if (status >= 400) {
      toast.error(message);
    }
    return Promise.reject(error);
  }
);
