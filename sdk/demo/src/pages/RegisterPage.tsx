import { FormEvent, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useTorchwood } from "@/lib/torchwood-context";

export function RegisterPage() {
  const { client, setAuth, run } = useTorchwood();
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setLoading(true);
    setError(null);
    try {
      const res = await run(() =>
        client.account.signUp({ email, password, name: name || "SDK User" })
      );
      if (res.mfa_required || !res.tokens) {
        setError("该账号已启用 MFA：请前往登录页完成二次验证。");
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
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }

  return (
    <form className="panel space-y-4 p-6" onSubmit={onSubmit}>
      <h2 className="text-lg font-semibold text-white">注册</h2>
      {error ? (
        <div className="rounded-lg border border-red-500/40 bg-red-500/10 px-3 py-2 text-sm text-red-200">
          {error}
        </div>
      ) : null}
      <label className="block space-y-1">
        <span className="text-xs text-Torchwood-muted">昵称</span>
        <input
          className="field"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="SDK User"
        />
      </label>
      <label className="block space-y-1">
        <span className="text-xs text-Torchwood-muted">邮箱</span>
        <input
          className="field"
          type="email"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder="you@example.com"
        />
      </label>
      <label className="block space-y-1">
        <span className="text-xs text-Torchwood-muted">密码</span>
        <input
          className="field"
          type="password"
          required
          minLength={8}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder="至少 8 位"
        />
      </label>
      <button type="submit" className="btn-primary w-full" disabled={loading}>
        {loading ? "注册中…" : "创建账号"}
      </button>
      <p className="text-center text-sm text-Torchwood-muted">
        已有账号？{" "}
        <Link className="text-Torchwood-accent hover:underline" to="/login">
          登录
        </Link>
      </p>
    </form>
  );
}
