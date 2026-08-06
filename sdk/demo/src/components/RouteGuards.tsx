import { Navigate, Outlet } from "react-router-dom";
import { useTorchwood } from "@/lib/torchwood-context";

export function ProtectedRoute() {
  const { auth } = useTorchwood();
  if (!auth) {
    return <Navigate to="/login" replace />;
  }
  return <Outlet />;
}

export function GuestRoute() {
  const { auth } = useTorchwood();
  if (auth) {
    return <Navigate to="/app" replace />;
  }
  return <Outlet />;
}
