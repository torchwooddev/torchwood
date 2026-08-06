import { FormEvent, useState } from "react";
import { useTorchwood } from "@/lib/torchwood-context";
import { PageHeader } from "@/components/Ui";

export function SettingsPage() {
  const { settings, updateSettings } = useTorchwood();
  const [form, setForm] = useState(settings);
  const [saved, setSaved] = useState(false);

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    updateSettings(form);
    setSaved(true);
    window.setTimeout(() => setSaved(false), 2000);
  }

  return (
    <div>
      <PageHeader
        title="连接设置"
        description="配置 Torchwood 服务地址、项目 ID、Server API Key 以及 Documents 演示用的库/集合 ID。"
      />

      <form className="panel max-w-2xl space-y-4 p-6" onSubmit={onSubmit}>
        <label className="block space-y-1">
          <span className="text-xs text-Torchwood-muted">Endpoint</span>
          <input
            className="field"
            value={form.endpoint}
            onChange={(e) => setForm({ ...form, endpoint: e.target.value })}
            placeholder="http://localhost:9080"
          />
        </label>
        <label className="block space-y-1">
          <span className="text-xs text-Torchwood-muted">Project ID</span>
          <input
            className="field"
            value={form.projectId}
            onChange={(e) => setForm({ ...form, projectId: e.target.value })}
            placeholder="default"
          />
        </label>
        <label className="block space-y-1">
          <span className="text-xs text-Torchwood-muted">Server API Key</span>
          <input
            className="field font-mono"
            type="password"
            value={form.apiKey}
            onChange={(e) => setForm({ ...form, apiKey: e.target.value })}
            placeholder="Torchwood-default-api-key-..."
          />
        </label>
        <label className="block space-y-1">
          <span className="text-xs text-Torchwood-muted">Documents demoDbId</span>
          <input
            className="field font-mono"
            value={form.demoDbId}
            onChange={(e) => setForm({ ...form, demoDbId: e.target.value })}
            placeholder="由 Documents 页初始化后自动写入"
          />
        </label>
        <label className="block space-y-1">
          <span className="text-xs text-Torchwood-muted">Documents demoCollId</span>
          <input
            className="field font-mono"
            value={form.demoCollId}
            onChange={(e) => setForm({ ...form, demoCollId: e.target.value })}
            placeholder="posts"
          />
        </label>
        <div className="flex items-center gap-3">
          <button type="submit" className="btn-primary">
            保存设置
          </button>
          {saved ? <span className="text-sm text-Torchwood-success">已保存</span> : null}
        </div>
      </form>
    </div>
  );
}
