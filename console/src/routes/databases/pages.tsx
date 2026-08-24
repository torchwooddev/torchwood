// pages.tsx 已按页面拆分（P3-12）：按职责拆为 databases.tsx / collections.tsx / documents.tsx / components.tsx
// 本文件保留为 barrel，便于既有 import { DatabasesListPage } from "@/routes/databases/pages" 兼容。
export * from "./databases";
export * from "./collections";
export * from "./documents";
export * from "./components";
