import { useState } from "react";
import { Link } from "react-router-dom";
import { useTorchwood } from "@/lib/torchwood-context";
import { suffix } from "@/lib/storage";
import { ErrorBanner, JsonPanel, MethodTag, PageHeader } from "@/components/Ui";

export function ServerPage() {
  const { settings, serverClient, run, lastError } = useTorchwood();
  const [result, setResult] = useState<unknown>(null);
  const [loading, setLoading] = useState(false);

  async function exec(label: string, fn: () => Promise<unknown>) {
    if (!settings.apiKey) return;
    setLoading(true);
    try {
      const data = await run(fn);
      setResult({ action: label, data });
    } catch {
      /* banner */
    } finally {
      setLoading(false);
    }
  }

  const disabled = loading || !settings.apiKey;

  return (
    <div>
      <PageHeader
        title="Server API"
        description="使用 API Key 调用 Server SDK：健康检查、项目、用户、数据库与用户组。"
      />
      <ErrorBanner message={lastError} />

      {!settings.apiKey ? (
        <div className="mb-4 rounded-lg border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-100">
          请先在{" "}
          <Link className="text-Torchwood-accent underline" to="/app/settings">
            设置
          </Link>{" "}
          填写 Server API Key（登录 Console 后在 API Keys 页面创建）。
        </div>
      ) : null}

      <div className="mb-4 flex flex-wrap gap-2">
        <button
          type="button"
          className="btn-secondary"
          disabled={disabled}
          onClick={() =>
            exec("server.health.check()", () => serverClient().server.health.check())
          }
        >
          <MethodTag method="GET" /> health.check()
        </button>
        <button
          type="button"
          className="btn-secondary"
          disabled={disabled}
          onClick={() =>
            exec("server.projects.list()", () => serverClient().server.projects.list())
          }
        >
          <MethodTag method="GET" /> projects.list()
        </button>
        <button
          type="button"
          className="btn-secondary"
          disabled={disabled}
          onClick={() =>
            exec("server.users.list()", () =>
              serverClient().server.users.list({ page_size: 10 })
            )
          }
        >
          <MethodTag method="GET" /> users.list()
        </button>
        <button
          type="button"
          className="btn-secondary"
          disabled={disabled}
          onClick={() =>
            exec("server.groups.create()", () =>
              serverClient().server.groups.create({ name: `Server Group ${suffix()}` })
            )
          }
        >
          <MethodTag method="POST" /> groups.create()
        </button>
        <button
          type="button"
          className="btn-secondary"
          disabled={disabled}
          onClick={() =>
            exec("server.databases.list()", () => serverClient().server.databases.listDatabases())
          }
        >
          <MethodTag method="GET" /> databases.list()
        </button>
      </div>

      <JsonPanel title="SDK 响应" data={result} />
    </div>
  );
}
