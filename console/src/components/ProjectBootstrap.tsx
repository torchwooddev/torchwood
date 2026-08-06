import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { listProjects } from "@/api/projects";
import { useAuth } from "@/hooks/useAuth";

/** Ensures admin console requests have X-Torchwood-Project before project-scoped APIs run. */
export function ProjectBootstrap() {
  const { projectId, selectProject } = useAuth();

  const { data: projects = [] } = useQuery({
    queryKey: ["projects"],
    queryFn: listProjects,
  });

  useEffect(() => {
    if (projects.length === 0) return;
    const firstId = projects[0].id;
    if (!projectId) {
      selectProject(firstId);
      return;
    }
    if (!projects.some((p) => p.id === projectId)) {
      selectProject(firstId);
    }
  }, [projectId, projects, selectProject]);

  return null;
}
