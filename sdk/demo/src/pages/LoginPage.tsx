import { FormEvent, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { useTorchwood } from "@/lib/torchwood-context";

type LoginTab = "password" | "email_otp" | "phone_otp" | "oauth" | "wechat";

const TABS: { id: LoginTab; label: string }[] = [
  { id: "password", label: "密码" },
  { id: "email_otp", label: "邮箱 OTP" },
  { id: "phone_otp", label: "短信 OTP" },
  { id: "oauth", label: "OAuth" },
  { id: "wechat", label: "微信小程序" },
];

export function LoginPage() {
  const [tab, setTab] = useState<LoginTab>("password");
  const [searchParams] = useSearchParams();
  const oauthError = searchParams.get("error");

  return (
    <div className="panel space-y-4 p-6">
      <h2 className="text-lg font-semibold text-white">登录</h2>
      {oauthError ? (
        <div className="rounded-lg border border-red-500/40 bg-red-500/10 px-3 py-2 text-sm text-red-200">
          OAuth 登录失败：{oauthError}
        </div>
      ) : null}

      <div className="flex flex-wrap gap-2 border-b border-Torchwood-border pb-3">
        {TABS.map((item) => (
          <button
            key={item.id}
            type="button"
            className={`rounded-md px-3 py-1.5 text-xs font-medium transition ${
              tab === item.id
                ? "bg-Torchwood-accent text-slate-950"
                : "bg-Torchwood-panel text-Torchwood-muted hover:text-white"
            }`}
            onClick={() => setTab(item.id)}
          >
            {item.label}
          </button>
        ))}
      </div>

      {tab === "password" ? <PasswordLoginForm /> : null}
      {tab === "email_otp" ? <EmailOTPLoginForm /> : null}
      {tab === "phone_otp" ? <PhoneOTPLoginForm /> : null}
      {tab === "oauth" ? <OAuthLoginPanel /> : null}
      {tab === "wechat" ? <WeChatMiniProgramForm /> : null}

      <p className="text-center text-sm text-Torchwood-muted">
        还没有账号？{" "}
        <Link className="text-Torchwood-accent hover:underline" to="/register">
          注册
        </Link>
      </p>
    </div>
  );
}

function useSignInSuccess() {
  const { setAuth } = useTorchwood();
  const navigate = useNavigate();
  return (res: {
    account: { id: string; email: string; name: string };
    tokens?: { access_token: string; refresh_token: string };
  }) => {
    if (!res.tokens) {
      // MFA 分支无 tokens：引导二次认证（demo 暂不实现，直接返回）。
      return;
    }
    setAuth({
      accessToken: res.tokens.access_token,
      refreshToken: res.tokens.refresh_token,
      email: res.account.email,
      name: res.account.name,
      userId: res.account.id,
    });
    navigate("/app");
  };
}

function ErrorBox({ error }: { error: string | null }) {
  if (!error) return null;
  return (
    <div className="rounded-lg border border-red-500/40 bg-red-500/10 px-3 py-2 text-sm text-red-200">
      {error}
    </div>
  );
}

function PasswordLoginForm() {
  const { client, run } = useTorchwood();
  const onSuccess = useSignInSuccess();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setLoading(true);
    setError(null);
    try {
      const res = await run(() => client.account.signIn({ email, password }));
      onSuccess(res);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }

  return (
    <form className="space-y-4" onSubmit={onSubmit}>
      <ErrorBox error={error} />
      <label className="block space-y-1">
        <span className="text-xs text-Torchwood-muted">邮箱</span>
        <input className="field" type="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
      </label>
      <label className="block space-y-1">
        <span className="text-xs text-Torchwood-muted">密码</span>
        <input className="field" type="password" required value={password} onChange={(e) => setPassword(e.target.value)} />
      </label>
      <button type="submit" className="btn-primary w-full" disabled={loading}>
        {loading ? "登录中…" : "登录"}
      </button>
    </form>
  );
}

function EmailOTPLoginForm() {
  const { client, run } = useTorchwood();
  const onSuccess = useSignInSuccess();
  const [email, setEmail] = useState("");
  const [challengeId, setChallengeId] = useState("");
  const [otp, setOtp] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sent, setSent] = useState(false);

  async function sendCode() {
    setLoading(true);
    setError(null);
    try {
      const res = await run(() => client.account.createEmailOTP({ email }));
      setChallengeId(res.challenge_id);
      setSent(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setLoading(true);
    setError(null);
    try {
      const res = await run(() =>
        client.account.createEmailOTPSession({ email, challenge_id: challengeId, otp })
      );
      onSuccess(res);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }

  return (
    <form className="space-y-4" onSubmit={onSubmit}>
      <ErrorBox error={error} />
      <label className="block space-y-1">
        <span className="text-xs text-Torchwood-muted">邮箱</span>
        <input className="field" type="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
      </label>
      {!sent ? (
        <button type="button" className="btn-secondary w-full" disabled={loading || !email} onClick={sendCode}>
          {loading ? "发送中…" : "发送验证码"}
        </button>
      ) : (
        <>
          <label className="block space-y-1">
            <span className="text-xs text-Torchwood-muted">验证码</span>
            <input className="field" required value={otp} onChange={(e) => setOtp(e.target.value)} placeholder="6 位数字" />
          </label>
          <button type="submit" className="btn-primary w-full" disabled={loading}>
            {loading ? "验证中…" : "验证并登录"}
          </button>
        </>
      )}
    </form>
  );
}

function PhoneOTPLoginForm() {
  const { client, run } = useTorchwood();
  const onSuccess = useSignInSuccess();
  const [phone, setPhone] = useState("+8613800138000");
  const [challengeId, setChallengeId] = useState("");
  const [otp, setOtp] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sent, setSent] = useState(false);

  async function sendCode() {
    setLoading(true);
    setError(null);
    try {
      const res = await run(() => client.account.createPhoneOTP({ phone }));
      setChallengeId(res.challenge_id);
      setSent(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setLoading(true);
    setError(null);
    try {
      const res = await run(() =>
        client.account.createPhoneOTPSession({ phone, challenge_id: challengeId, otp })
      );
      onSuccess(res);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }

  return (
    <form className="space-y-4" onSubmit={onSubmit}>
      <ErrorBox error={error} />
      <p className="text-xs text-Torchwood-muted">需配置 Twilio 或开启 dev_log_sms 查看日志中的验证码。</p>
      <label className="block space-y-1">
        <span className="text-xs text-Torchwood-muted">手机号（E.164）</span>
        <input className="field" required value={phone} onChange={(e) => setPhone(e.target.value)} />
      </label>
      {!sent ? (
        <button type="button" className="btn-secondary w-full" disabled={loading || !phone} onClick={sendCode}>
          {loading ? "发送中…" : "发送短信验证码"}
        </button>
      ) : (
        <>
          <label className="block space-y-1">
            <span className="text-xs text-Torchwood-muted">验证码</span>
            <input className="field" required value={otp} onChange={(e) => setOtp(e.target.value)} />
          </label>
          <button type="submit" className="btn-primary w-full" disabled={loading}>
            {loading ? "验证中…" : "验证并登录"}
          </button>
        </>
      )}
    </form>
  );
}

function OAuthLoginPanel() {
  const { client, run } = useTorchwood();
  const [loading, setLoading] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const callbackBase = useMemo(
    () => `${window.location.origin}/login/oauth/callback`,
    []
  );

  async function startOAuth(provider: string) {
    setLoading(provider);
    setError(null);
    try {
      const res = await run(() =>
        client.account.createOAuth2Session({
          provider,
          success: callbackBase,
          failure: `${window.location.origin}/login?error=oauth_${provider}`,
        })
      );
      window.location.href = res.redirect_url;
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setLoading(null);
    }
  }

  return (
    <div className="space-y-3">
      <ErrorBox error={error} />
      <p className="text-xs text-Torchwood-muted">
        请先在 Console Settings 配置 OAuth Provider，并将回调地址注册到对应平台。
      </p>
      <div className="grid gap-2 sm:grid-cols-2">
        {[
          { id: "google", label: "Google" },
          { id: "github", label: "GitHub" },
          { id: "wechat_web", label: "微信扫码" },
        ].map((p) => (
          <button
            key={p.id}
            type="button"
            className="btn-secondary w-full"
            disabled={loading !== null}
            onClick={() => startOAuth(p.id)}
          >
            {loading === p.id ? "跳转中…" : p.label}
          </button>
        ))}
      </div>
    </div>
  );
}

function WeChatMiniProgramForm() {
  const { client, run } = useTorchwood();
  const onSuccess = useSignInSuccess();
  const [code, setCode] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setLoading(true);
    setError(null);
    try {
      const res = await run(() => client.account.createWeChatMiniProgramSession({ code }));
      onSuccess(res);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }

  return (
    <form className="space-y-4" onSubmit={onSubmit}>
      <ErrorBox error={error} />
      <p className="text-xs text-Torchwood-muted">
        开发调试：粘贴 wx.login 返回的 code。需配置 wechat_miniprogram OAuth Provider。
      </p>
      <label className="block space-y-1">
        <span className="text-xs text-Torchwood-muted">wx.login code</span>
        <input className="field" required value={code} onChange={(e) => setCode(e.target.value)} />
      </label>
      <button type="submit" className="btn-primary w-full" disabled={loading}>
        {loading ? "登录中…" : "code2session 登录"}
      </button>
    </form>
  );
}

export function OAuthCallbackPage() {
  const { setAuth } = useTorchwood();
  const navigate = useNavigate();
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const hash = new URLSearchParams(window.location.hash.replace(/^#/, ""));
    const accessToken = hash.get("access_token");
    const refreshToken = hash.get("refresh_token");
    const userId = hash.get("userId");

    if (!accessToken || !refreshToken) {
      setError("OAuth 回调缺少 token，请重试。");
      return;
    }

    setAuth({
      accessToken,
      refreshToken,
      userId: userId ?? "",
      email: "",
      name: "",
    });
    window.history.replaceState({}, "", "/login/oauth/callback");
    navigate("/app", { replace: true });
  }, [navigate, setAuth]);

  return (
    <div className="panel p-6 text-center text-sm text-Torchwood-muted">
      {error ?? "OAuth 登录处理中…"}
    </div>
  );
}
