import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Trash2 } from "lucide-react";
import { useAuth } from "@/hooks/useAuth";
import {
  OAUTH_PROVIDER_OPTIONS,
  deleteOAuthProvider,
  listOAuthProviders,
  oauthCallbackURL,
  upsertOAuthProvider,
  type OAuthProvider,
} from "@/api/oauthProviders";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

type SettingsTab = "oauth" | "messaging";

export function SettingsPage() {
  const [tab, setTab] = useState<SettingsTab>("oauth");

  return (
    <div className="space-y-6 max-w-4xl">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Settings</h1>
        <p className="text-muted-foreground text-sm mt-1">
          当前选中项目的认证与消息配置
        </p>
      </div>

      <div className="flex gap-2 border-b pb-2">
        <Button
          variant={tab === "oauth" ? "default" : "ghost"}
          size="sm"
          onClick={() => setTab("oauth")}
        >
          OAuth Providers
        </Button>
        <Button
          variant={tab === "messaging" ? "default" : "ghost"}
          size="sm"
          onClick={() => setTab("messaging")}
        >
          Email / SMS
        </Button>
      </div>

      {tab === "oauth" ? <OAuthProvidersPanel /> : <MessagingPanel />}
    </div>
  );
}

function OAuthProvidersPanel() {
  const { projectId } = useAuth();
  const queryClient = useQueryClient();
  const [selectedProvider, setSelectedProvider] = useState<string>("google");
  const [enabled, setEnabled] = useState(true);
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [scopesText, setScopesText] = useState("openid, email, profile");

  const { data: providers = [], isLoading } = useQuery({
    queryKey: ["oauth-providers", projectId],
    queryFn: listOAuthProviders,
    enabled: !!projectId,
  });

  const save = useMutation({
    mutationFn: upsertOAuthProvider,
    onSuccess: () => {
      toast.success("OAuth 配置已保存");
      setClientSecret("");
      queryClient.invalidateQueries({ queryKey: ["oauth-providers"] });
    },
  });

  const remove = useMutation({
    mutationFn: deleteOAuthProvider,
    onSuccess: () => {
      toast.success("OAuth 配置已删除");
      queryClient.invalidateQueries({ queryKey: ["oauth-providers"] });
    },
  });

  const loadProvider = (provider: string) => {
    setSelectedProvider(provider);
    const existing = providers.find((p) => p.provider === provider);
    const defaults = OAUTH_PROVIDER_OPTIONS.find((o) => o.id === provider);
    if (existing) {
      setEnabled(existing.enabled);
      setClientId(existing.client_id);
      setClientSecret("");
      setScopesText((existing.scopes ?? []).join(", "));
    } else {
      setEnabled(true);
      setClientId("");
      setClientSecret("");
      setScopesText((defaults?.defaultScopes ?? []).join(", "));
    }
  };

  const onSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!projectId) {
      toast.error("请先选择项目");
      return;
    }
    const scopes = scopesText
      .split(/[,\s]+/)
      .map((s) => s.trim())
      .filter(Boolean);
    save.mutate({
      provider: selectedProvider,
      enabled,
      client_id: clientId,
      client_secret: clientSecret || undefined,
      scopes,
    });
  };

  if (!projectId) {
    return (
      <p className="text-sm text-muted-foreground">请先在侧边栏选择一个项目。</p>
    );
  }

  return (
    <div className="grid gap-8 lg:grid-cols-[1fr_320px]">
      <form className="space-y-4 rounded-lg border p-6" onSubmit={onSubmit}>
        <div className="space-y-2">
          <Label>Provider</Label>
          <Select
            value={selectedProvider}
            onValueChange={(v) => loadProvider(v)}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {OAUTH_PROVIDER_OPTIONS.map((opt) => (
                <SelectItem key={opt.id} value={opt.id}>
                  {opt.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="flex items-center gap-2">
          <Checkbox
            id="enabled"
            checked={enabled}
            onChange={(e) => setEnabled(e.target.checked)}
          />
          <Label htmlFor="enabled">启用</Label>
        </div>

        <div className="space-y-2">
          <Label htmlFor="client_id">Client ID / AppID</Label>
          <Input
            id="client_id"
            value={clientId}
            onChange={(e) => setClientId(e.target.value)}
            required
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="client_secret">
            Client Secret / AppSecret
            {providers.find((p) => p.provider === selectedProvider)?.has_client_secret ? (
              <span className="text-muted-foreground font-normal ml-2">
                （留空则保留原值）
              </span>
            ) : null}
          </Label>
          <Input
            id="client_secret"
            type="password"
            value={clientSecret}
            onChange={(e) => setClientSecret(e.target.value)}
            placeholder="首次配置必填"
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="scopes">Scopes（逗号分隔，微信可留空）</Label>
          <Input
            id="scopes"
            value={scopesText}
            onChange={(e) => setScopesText(e.target.value)}
          />
        </div>

        <p className="text-xs text-muted-foreground">
          回调地址：
          <code className="ml-1 rounded bg-muted px-1 py-0.5">
            {oauthCallbackURL(selectedProvider)}
          </code>
        </p>

        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "保存中…" : "保存配置"}
        </Button>
      </form>

      <div className="space-y-3">
        <h3 className="text-sm font-medium">已配置</h3>
        {isLoading ? (
          <p className="text-sm text-muted-foreground">加载中…</p>
        ) : providers.length === 0 ? (
          <p className="text-sm text-muted-foreground">暂无 OAuth 配置</p>
        ) : (
          providers.map((p) => (
            <ProviderCard
              key={p.provider}
              provider={p}
              onEdit={() => loadProvider(p.provider)}
              onDelete={() => remove.mutate(p.provider)}
              deleting={remove.isPending}
            />
          ))
        )}
      </div>
    </div>
  );
}

function ProviderCard({
  provider,
  onEdit,
  onDelete,
  deleting,
}: {
  provider: OAuthProvider;
  onEdit: () => void;
  onDelete: () => void;
  deleting: boolean;
}) {
  const label =
    OAUTH_PROVIDER_OPTIONS.find((o) => o.id === provider.provider)?.label ??
    provider.provider;

  return (
    <div className="rounded-lg border p-3 space-y-2">
      <div className="flex items-center justify-between gap-2">
        <span className="font-medium text-sm">{label}</span>
        <Badge variant={provider.enabled ? "default" : "secondary"}>
          {provider.enabled ? "Enabled" : "Disabled"}
        </Badge>
      </div>
      <p className="text-xs text-muted-foreground font-mono truncate">
        {provider.client_id}
      </p>
      <div className="flex gap-2">
        <Button type="button" variant="outline" size="sm" onClick={onEdit}>
          编辑
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="text-destructive"
          disabled={deleting}
          onClick={onDelete}
        >
          <Trash2 className="h-4 w-4" />
        </Button>
      </div>
    </div>
  );
}

function MessagingPanel() {
  return (
    <div className="rounded-lg border p-6 space-y-4 text-sm">
      <p className="text-muted-foreground">
        Email OTP 与 SMS OTP 目前使用<strong className="text-foreground">平台级</strong>
        配置（环境变量 / <code className="rounded bg-muted px-1">configs/config.yaml</code>
        ），项目级 SMTP 将在后续版本支持。
      </p>
      <div className="space-y-2">
        <h3 className="font-medium">Email OTP（SMTP）</h3>
        <ul className="list-disc pl-5 text-muted-foreground space-y-1">
          <li>
            <code>TORCHWOOD_MESSAGING_SMTP_HOST</code> / <code>PORT</code> /{" "}
            <code>USERNAME</code> / <code>PASSWORD</code>
          </li>
          <li>
            开发模式：<code>TORCHWOOD_MESSAGING_DEV_LOG_OTP=true</code> 将验证码写入服务日志
          </li>
        </ul>
      </div>
      <div className="space-y-2">
        <h3 className="font-medium">SMS OTP（Twilio）</h3>
        <ul className="list-disc pl-5 text-muted-foreground space-y-1">
          <li>
            <code>TORCHWOOD_MESSAGING_SMS_TWILIO_ACCOUNT_SID</code> /{" "}
            <code>AUTH_TOKEN</code> / <code>FROM</code>
          </li>
          <li>
            开发模式：<code>TORCHWOOD_MESSAGING_DEV_LOG_SMS=true</code>
          </li>
        </ul>
      </div>
    </div>
  );
}
