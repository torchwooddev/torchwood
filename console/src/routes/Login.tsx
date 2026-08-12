import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "@/hooks/useAuth";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Copy } from "lucide-react";
import { toast } from "sonner";
import { getSetupStatus, signUp } from "@/api/auth";

type SetupState = "loading" | "login" | "setup";

export function Login() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [setupToken, setSetupToken] = useState("");
  const [setupTokenRequired, setSetupTokenRequired] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  // 未初始化时展示「初始化设置」表单，初始化完成后回到登录态。
  const [setupState, setSetupState] = useState<SetupState>("loading");
  // setup-status 探测失败时展示错误 + 重试按钮，同时退回登录表单不阻塞登录。
  const [setupProbeError, setSetupProbeError] = useState<string | null>(null);
  // 默认 API Key 明文 secret 仅展示一次，不持久化。
  const [apiKeySecret, setApiKeySecret] = useState<string | null>(null);
  const { login, isAuthenticated } = useAuth();
  const navigate = useNavigate();

  const probeSetup = useCallback(() => {
    setSetupProbeError(null);
    setSetupState("loading");
    let cancelled = false;
    getSetupStatus()
      .then((status) => {
        if (!cancelled) {
          setSetupState(status.needs_setup ? "setup" : "login");
          setSetupTokenRequired(status.setup_token_required);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setSetupState("login");
          setSetupProbeError("无法获取初始化状态，可重试或直接登录");
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (isAuthenticated) {
      navigate("/console", { replace: true });
      return;
    }
    return probeSetup();
  }, [isAuthenticated, navigate, probeSetup]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setLoading(true);
    try {
      await login(email, password);
    } catch (err) {
      const msg = extractErrorMessage(err);
      setError(msg || "Login failed");
    } finally {
      setLoading(false);
    }
  };

  const handleSignUp = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    if (password !== confirmPassword) {
      setError("两次输入的密码不一致");
      return;
    }
    setLoading(true);
    try {
      const result = await signUp({
        email,
        password,
        setup_token: setupTokenRequired ? setupToken : undefined,
      });
      // 会话 cookie 已由服务端下发；展示一次默认 API Key secret。
      setApiKeySecret(result.default_api_key_secret);
    } catch (err) {
      const msg = extractErrorMessage(err);
      setError(msg || "Setup failed");
    } finally {
      setLoading(false);
    }
  };

  // enterConsole 在用户确认复制 secret 后进入 Console（调用 login 同步
  // 本地认证状态，会话 cookie 已由 sign-up 下发）。
  const enterConsole = async () => {
    try {
      await login(email, password);
    } catch (err) {
      // login 已跳过全局 toast，错误需由本页展示。
      const msg = extractErrorMessage(err);
      toast.error(msg || "登录失败，请重试");
    } finally {
      navigate("/console", { replace: true });
    }
  };

  const copySecret = () => {
    if (apiKeySecret) {
      navigator.clipboard.writeText(apiKeySecret);
      toast.success("Secret 已复制");
    }
  };

  const isSetup = setupState === "setup";

  return (
    <div className="flex h-screen items-center justify-center bg-muted/40">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Torchwood Console</CardTitle>
          <CardDescription>
            {isSetup
              ? "首次部署：创建第一个管理员账户"
              : "Sign in with your admin account"}
          </CardDescription>
        </CardHeader>
        <form onSubmit={isSetup ? handleSignUp : handleSubmit}>
          <CardContent className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="email">Email</Label>
              <Input
                id="email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="password">Password</Label>
              <Input
                id="password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
            </div>
            {isSetup && (
              <div className="space-y-2">
                <Label htmlFor="confirm-password">确认密码</Label>
                <Input
                  id="confirm-password"
                  type="password"
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  required
                />
              </div>
            )}
            {isSetup && setupTokenRequired && (
              <div className="space-y-2">
                <Label htmlFor="setup-token">Setup Token</Label>
                <Input
                  id="setup-token"
                  type="password"
                  value={setupToken}
                  onChange={(e) => setSetupToken(e.target.value)}
                  placeholder="TORCHWOOD_SECURITY_SETUP_TOKEN"
                  required
                />
              </div>
            )}
            {setupProbeError && (
              <div className="flex items-center justify-between gap-2 rounded-md bg-destructive/10 px-3 py-2">
                <p className="text-sm text-destructive">{setupProbeError}</p>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={probeSetup}
                >
                  重试
                </Button>
              </div>
            )}
            {error && <p className="text-sm text-destructive">{error}</p>}
            {isSetup && (
              <p className="text-xs text-muted-foreground">
                注册后自动创建默认项目（default）与默认 API Key（scope=all），
                首个管理员将获得 owner 角色。
              </p>
            )}
          </CardContent>
          <CardFooter>
            <Button type="submit" className="w-full" disabled={loading || setupState === "loading"}>
              {loading
                ? isSetup
                  ? "Setting up..."
                  : "Signing in..."
                : isSetup
                  ? "创建管理员并初始化"
                  : "Sign in"}
            </Button>
          </CardFooter>
        </form>
      </Card>

      <Dialog open={apiKeySecret !== null} onOpenChange={() => {}}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>初始化完成</DialogTitle>
            <DialogDescription>
              管理员账户已创建，你已自动获得 owner 角色。默认 API Key 的
              Secret 仅在此显示一次，请立即复制并妥善保存。
            </DialogDescription>
          </DialogHeader>
          <div className="rounded-md bg-muted p-4 flex items-center justify-between gap-4">
            <code className="break-all text-xs flex-1">{apiKeySecret}</code>
            <Button variant="secondary" size="sm" type="button" onClick={copySecret}>
              <Copy className="h-4 w-4 mr-1" />
              复制
            </Button>
          </div>
          <DialogFooter>
            <Button onClick={enterConsole} className="w-full sm:w-auto">
              进入 Console
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function extractErrorMessage(err: unknown): string | undefined {
  return (
    err as {
      response?: { data?: { error?: { message?: string } } };
    }
  )?.response?.data?.error?.message;
}
