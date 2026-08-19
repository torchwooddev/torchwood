import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { useTorchwood } from "@/lib/torchwood-context";

const nav = [
  { to: "/app", label: "概览", end: true },
  { to: "/app/account", label: "Account" },
  { to: "/app/databases", label: "Databases" },
  { to: "/app/teams", label: "Teams" },
  { to: "/app/server", label: "Server API" },
  { to: "/app/economy", label: "Economy" },
  { to: "/app/settings", label: "设置" },
];

export function AppLayout() {
  const { auth, client, setAuth } = useTorchwood();
  const navigate = useNavigate();

  async function handleSignOut() {
    try {
      await client.account.signOut();
    } catch {
      // ignore network errors on logout
    }
    setAuth(null);
    navigate("/login");
  }

  return (
    <div className="min-h-screen lg:flex">
      <aside className="border-b border-Torchwood-border bg-Torchwood-panel/80 lg:w-64 lg:border-b-0 lg:border-r">
        <div className="px-5 py-6">
          <div className="text-xs uppercase tracking-[0.2em] text-Torchwood-muted">Torchwood</div>
          <div className="mt-1 text-lg font-semibold text-white">SDK Playground</div>
          <div className="mt-3 truncate text-xs text-Torchwood-muted">{auth?.email}</div>
        </div>
        <nav className="flex gap-1 overflow-x-auto px-3 pb-4 lg:flex-col lg:overflow-visible">
          {nav.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              className={({ isActive }) => (isActive ? "nav-link-active" : "nav-link")}
            >
              {item.label}
            </NavLink>
          ))}
        </nav>
        <div className="hidden px-3 pb-6 lg:block">
          <button type="button" className="btn-danger w-full" onClick={handleSignOut}>
            退出登录
          </button>
        </div>
      </aside>

      <main className="flex-1 px-4 py-6 md:px-8 md:py-8">
        <Outlet />
      </main>
    </div>
  );
}

export function AuthLayout() {
  return (
    <div className="flex min-h-screen items-center justify-center px-4 py-10">
      <div className="w-full max-w-md">
        <div className="mb-8 text-center">
          <div className="text-xs uppercase tracking-[0.25em] text-Torchwood-muted">Torchwood SDK</div>
          <h1 className="mt-2 text-3xl font-semibold text-white">Playground</h1>
          <p className="mt-2 text-sm text-Torchwood-muted">注册、登录并体验 Client / Server API</p>
        </div>
        <Outlet />
      </div>
    </div>
  );
}
