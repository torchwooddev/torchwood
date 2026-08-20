import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { TorchwoodProvider } from "@/lib/torchwood-context";
import { AppLayout, AuthLayout } from "@/components/Layout";
import { GuestRoute, ProtectedRoute } from "@/components/RouteGuards";
import { AccountPage } from "@/pages/AccountPage";
import { DatabasesPage } from "@/pages/DatabasesPage";
import { HomePage } from "@/pages/HomePage";
import { LoginPage, OAuthCallbackPage } from "@/pages/LoginPage";
import { RegisterPage } from "@/pages/RegisterPage";
import { EconomyPage } from "@/pages/EconomyPage";
import { ServerPage } from "@/pages/ServerPage";
import { SettingsPage } from "@/pages/SettingsPage";
import { GroupsPage } from "@/pages/GroupsPage";

export default function App() {
  return (
    <TorchwoodProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<Navigate to="/app" replace />} />

          <Route element={<GuestRoute />}>
            <Route element={<AuthLayout />}>
              <Route path="/login" element={<LoginPage />} />
              <Route path="/login/oauth/callback" element={<OAuthCallbackPage />} />
              <Route path="/register" element={<RegisterPage />} />
            </Route>
          </Route>

          <Route element={<ProtectedRoute />}>
            <Route element={<AppLayout />}>
              <Route path="/app" element={<HomePage />} />
              <Route path="/app/account" element={<AccountPage />} />
              <Route path="/app/databases" element={<DatabasesPage />} />
              <Route path="/app/groups" element={<GroupsPage />} />
              <Route path="/app/server" element={<ServerPage />} />
              <Route path="/app/economy" element={<EconomyPage />} />
              <Route path="/app/settings" element={<SettingsPage />} />
            </Route>
          </Route>

          <Route path="*" element={<Navigate to="/app" replace />} />
        </Routes>
      </BrowserRouter>
    </TorchwoodProvider>
  );
}
