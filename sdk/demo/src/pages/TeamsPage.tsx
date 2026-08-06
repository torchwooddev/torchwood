import { useState } from "react";
import { useTorchwood } from "@/lib/torchwood-context";
import { suffix } from "@/lib/storage";
import { ErrorBanner, JsonPanel, MethodTag, PageHeader } from "@/components/Ui";

export function TeamsPage() {
  const { client, auth, setAuth, run, lastError } = useTorchwood();
  const [result, setResult] = useState<unknown>(null);
  const [loading, setLoading] = useState(false);
  const [teamName, setTeamName] = useState(`Web Team ${suffix()}`);
  const [inviteEmail, setInviteEmail] = useState("invitee@torchwood.local");
  const [selectedTeamId, setSelectedTeamId] = useState("");

  async function exec(label: string, fn: () => Promise<unknown>) {
    setLoading(true);
    try {
      const data = await run(fn);
      setResult({ action: label, data });
      return data;
    } catch {
      return null;
    } finally {
      setLoading(false);
    }
  }

  async function createTeamFlow() {
    setLoading(true);
    try {
      const team = await run(() => client.teams.createTeam(teamName));
      setSelectedTeamId(team.id);
      if (auth?.refreshToken) {
        const tokens = await run(() => client.account.refresh(auth.refreshToken));
        setAuth({
          ...auth,
          accessToken: tokens.access_token,
          refreshToken: tokens.refresh_token,
        });
        setResult({ action: "createTeam() + refresh()", data: { team, tokens } });
      } else {
        setResult({ action: "createTeam()", data: team });
      }
    } catch {
      /* banner */
    } finally {
      setLoading(false);
    }
  }

  return (
    <div>
      <PageHeader
        title="Teams API"
        description="创建团队、刷新 Token 获取 team 角色、邀请成员并列出成员。"
      />
      <ErrorBanner message={lastError} />

      <div className="mb-4 grid gap-3 md:grid-cols-2">
        <label className="block space-y-1">
          <span className="text-xs text-Torchwood-muted">团队名称</span>
          <input className="field" value={teamName} onChange={(e) => setTeamName(e.target.value)} />
        </label>
        <label className="block space-y-1">
          <span className="text-xs text-Torchwood-muted">teamId（邀请/列表用）</span>
          <input
            className="field"
            value={selectedTeamId}
            onChange={(e) => setSelectedTeamId(e.target.value)}
            placeholder="创建团队后自动填入"
          />
        </label>
        <label className="block space-y-1 md:col-span-2">
          <span className="text-xs text-Torchwood-muted">邀请邮箱</span>
          <input
            className="field"
            type="email"
            value={inviteEmail}
            onChange={(e) => setInviteEmail(e.target.value)}
          />
        </label>
      </div>

      <div className="mb-4 flex flex-wrap gap-2">
        <button type="button" className="btn-primary" disabled={loading} onClick={createTeamFlow}>
          <MethodTag method="POST" /> createTeam() + refresh()
        </button>
        <button
          type="button"
          className="btn-secondary"
          disabled={loading}
          onClick={() => exec("teams.listTeams()", () => client.teams.listTeams())}
        >
          <MethodTag method="GET" /> listTeams()
        </button>
        <button
          type="button"
          className="btn-secondary"
          disabled={loading || !selectedTeamId}
          onClick={() =>
            exec("teams.createMembership()", () =>
              client.teams.createMembership(selectedTeamId, {
                email: inviteEmail,
                name: "Invited Member",
                roles: ["member"],
              })
            )
          }
        >
          <MethodTag method="POST" /> createMembership()
        </button>
        <button
          type="button"
          className="btn-secondary"
          disabled={loading || !selectedTeamId}
          onClick={() =>
            exec("teams.listMemberships()", () =>
              client.teams.listMemberships(selectedTeamId)
            )
          }
        >
          <MethodTag method="GET" /> listMemberships()
        </button>
      </div>

      <JsonPanel title="SDK 响应" data={result} />
    </div>
  );
}
