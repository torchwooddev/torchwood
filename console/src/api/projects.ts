import { api } from "./client";

export interface Project {
  id: string;
  name: string;
  description?: string;
  status: string;
  created_at: string;
  updated_at: string;
}

export interface ListProjectsResponse {
  projects: Project[];
  meta?: { total_count?: number; page_size?: number };
}

export async function listProjects(): Promise<Project[]> {
  const res = await api.get<ListProjectsResponse>("/server/projects");
  return res.data.projects ?? [];
}

export async function getProject(id: string): Promise<Project> {
  const res = await api.get<Project>(`/server/projects/${id}`);
  return res.data;
}

export async function createProject(input: {
  id: string;
  name: string;
  description?: string;
}): Promise<Project> {
  const res = await api.post<Project>("/server/projects", input);
  return res.data;
}

export async function updateProject(
  id: string,
  input: { name?: string; description?: string }
): Promise<Project> {
  const res = await api.patch<Project>(`/server/projects/${id}`, input);
  return res.data;
}

export async function deleteProject(id: string): Promise<void> {
  await api.delete(`/server/projects/${id}`);
}
