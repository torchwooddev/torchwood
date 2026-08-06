import type { ReactNode } from "react";

export function JsonPanel({
  title,
  data,
  empty = "点击上方按钮调用 SDK…",
}: {
  title: string;
  data: unknown;
  empty?: string;
}) {
  return (
    <div className="panel overflow-hidden">
      <div className="border-b border-Torchwood-border px-4 py-3 text-sm font-medium text-slate-200">
        {title}
      </div>
      <pre className="max-h-[420px] overflow-auto p-4 font-mono text-xs leading-relaxed text-cyan-100/90">
        {data === undefined || data === null
          ? empty
          : JSON.stringify(data, null, 2)}
      </pre>
    </div>
  );
}

export function PageHeader({
  title,
  description,
  actions,
}: {
  title: string;
  description: string;
  actions?: ReactNode;
}) {
  return (
    <div className="mb-6 flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight text-white">{title}</h1>
        <p className="mt-1 max-w-2xl text-sm text-Torchwood-muted">{description}</p>
      </div>
      {actions ? <div className="flex flex-wrap gap-2">{actions}</div> : null}
    </div>
  );
}

export function ErrorBanner({ message }: { message: string | null }) {
  if (!message) return null;
  return (
    <div className="mb-4 rounded-lg border border-red-500/40 bg-red-500/10 px-4 py-3 text-sm text-red-200">
      {message}
    </div>
  );
}

export function MethodTag({ method }: { method: string }) {
  return (
    <span className="rounded bg-Torchwood-panel px-2 py-0.5 font-mono text-[11px] text-Torchwood-accent">
      {method}
    </span>
  );
}
