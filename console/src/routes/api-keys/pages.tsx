import { useCallback, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Plus, Copy } from "lucide-react";
import {
  listAPIKeys,
  getAPIKey,
  createAPIKey,
  deleteAPIKey,
  type APIKey,
} from "@/api/apiKeys";
import { useAuth } from "@/hooks/useAuth";
import { useAdminRole, isPlatformAdmin } from "@/hooks/useAdminRole";
import { ResourceListPage } from "@/components/list/ResourceListPage";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import type { ColumnDef } from "@/components/list/DataTable";
import {
  FormPageWrapper,
  FormField,
  DetailPageWrapper,
  DetailGrid,
  DetailSkeleton,
  NotFound,
  BulkDeleteButton,
  RowDeleteButton,
  DeleteButton,
} from "@/components/resource/shared";

const columns: ColumnDef<APIKey>[] = [
  {
    key: "id",
    header: "ID",
    className: "font-mono text-xs max-w-[140px] truncate",
    cell: (k) => k.id,
  },
  { key: "name", header: "名称", cell: (k) => k.name },
  { key: "scopes", header: "Scopes", cell: (k) => k.scopes.join(", ") || "—" },
  {
    key: "status",
    header: "状态",
    cell: (k) => (
      <Badge variant={k.enabled ? "default" : "secondary"}>
        {k.enabled ? "Active" : "Disabled"}
      </Badge>
    ),
  },
  {
    key: "created",
    header: "创建时间",
    cell: (k) => new Date(k.created_at).toLocaleString(),
  },
];

export function ApiKeysListPage() {
  const { projectId } = useAuth();
  const { role } = useAdminRole();
  const queryClient = useQueryClient();
  const [bulkDeleting, setBulkDeleting] = useState(false);
  const platformAdmin = isPlatformAdmin(role);

  const { data: keys = [], isLoading } = useQuery({
    queryKey: ["api-keys", projectId],
    queryFn: listAPIKeys,
    enabled: !!projectId,
  });

  const remove = useMutation({
    mutationFn: deleteAPIKey,
    onSuccess: () => {
      toast.success("API Key 已删除");
      queryClient.invalidateQueries({ queryKey: ["api-keys"] });
    },
  });

  const getSearchText = useCallback(
    (k: APIKey) => `${k.id} ${k.name} ${k.scopes.join(" ")}`,
    []
  );

  const handleBulkDelete = async (selected: APIKey[], clear: () => void) => {
    setBulkDeleting(true);
    try {
      const results = await Promise.allSettled(selected.map((k) => deleteAPIKey(k.id)));
      const failed = results.filter((r) => r.status === "rejected").length;
      const succeeded = results.length - failed;
      if (failed > 0) {
        toast.error(`删除完成：成功 ${succeeded} 个，失败 ${failed} 个`);
      } else {
        toast.success(`已删除 ${selected.length} 个 API Key`);
      }
      queryClient.invalidateQueries({ queryKey: ["api-keys"] });
      clear();
    } finally {
      setBulkDeleting(false);
    }
  };

  return (
    <ResourceListPage
      title="API Keys"
      description="管理当前项目的服务端 API Key"
      searchPlaceholder="搜索名称或 ID..."
      isLoading={isLoading}
      items={keys}
      columns={columns}
      getSearchText={getSearchText}
      detailPath={(k) => `/console/api-keys/${k.id}`}
      toolbarActions={
        platformAdmin ? (
          <Button asChild>
            <Link to="/console/api-keys/new">
              <Plus className="h-4 w-4 mr-2" />
              新建 API Key
            </Link>
          </Button>
        ) : undefined
      }
      selectionActions={
        platformAdmin
          ? (selected, clear) => (
              <BulkDeleteButton
                count={selected.length}
                loading={bulkDeleting}
                onConfirm={() => handleBulkDelete(selected, clear)}
              />
            )
          : undefined
      }
      rowActions={
        platformAdmin
          ? (k) => (
              <RowDeleteButton
                onConfirm={() => remove.mutate(k.id)}
                loading={remove.isPending}
              />
            )
          : undefined
      }
      emptyTitle="暂无 API Key"
      emptyDescription="创建 API Key 以访问服务端 API"
      emptyAction={
        platformAdmin ? (
          <Button asChild>
            <Link to="/console/api-keys/new">新建 API Key</Link>
          </Button>
        ) : undefined
      }
    />
  );
}

export function ApiKeyNewPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { role } = useAdminRole();
  const [name, setName] = useState("");
  const [scopes, setScopes] = useState("");
  const [createdSecret, setCreatedSecret] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: createAPIKey,
    onSuccess: (data) => {
      setCreatedSecret(data.secret);
      toast.success("API Key 创建成功，请立即复制 Secret");
      queryClient.invalidateQueries({ queryKey: ["api-keys"] });
    },
  });

  const copySecret = () => {
    if (createdSecret) {
      navigator.clipboard.writeText(createdSecret);
      toast.success("Secret 已复制");
    }
  };

  if (createdSecret) {
    return (
      <FormPageWrapper
        title="API Key 已创建"
        description="Secret 仅显示一次，请妥善保存"
        backTo="/console/api-keys"
        submitLabel="返回列表"
        onSubmit={(e) => {
          e.preventDefault();
          navigate("/console/api-keys");
        }}
      >
        <div className="rounded-md bg-muted p-4 flex items-center justify-between gap-4">
          <code className="break-all text-xs flex-1">{createdSecret}</code>
          <Button variant="secondary" size="sm" type="button" onClick={copySecret}>
            <Copy className="h-4 w-4 mr-1" />
            复制
          </Button>
        </div>
      </FormPageWrapper>
    );
  }

  return (
    <FormPageWrapper
      title="新建 API Key"
      description="Secret 创建后仅显示一次"
      backTo="/console/api-keys"
      submitLabel="创建"
      onSubmit={(e) => {
        e.preventDefault();
        mutation.mutate({
          name,
          scopes: scopes.split(",").map((s) => s.trim()).filter(Boolean),
        });
      }}
      loading={mutation.isPending}
      submitDisabled={!isPlatformAdmin(role)}
    >
      <FormField id="name" label="名称" value={name} onChange={setName} required placeholder="Production API Key" />
      <FormField
        id="scopes"
        label="Scopes（逗号分隔）"
        value={scopes}
        onChange={setScopes}
        placeholder="users.read, users.write"
      />
    </FormPageWrapper>
  );
}

export function ApiKeyDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const { data: key, isLoading } = useQuery({
    queryKey: ["api-keys", id],
    queryFn: () => getAPIKey(id!),
    enabled: !!id,
  });

  const remove = useMutation({
    mutationFn: deleteAPIKey,
    onSuccess: () => {
      toast.success("API Key 已删除");
      queryClient.invalidateQueries({ queryKey: ["api-keys"] });
      navigate("/console/api-keys");
    },
  });

  if (isLoading) return <DetailSkeleton />;
  if (!key) return <NotFound backTo="/console/api-keys" />;

  return (
    <DetailPageWrapper
      title={key.name}
      description="API Key 详情"
      backTo="/console/api-keys"
      actions={
        <DeleteButton onConfirm={() => remove.mutate(key.id)} loading={remove.isPending} />
      }
    >
      <DetailGrid
        items={[
          { label: "ID", value: key.id, mono: true },
          { label: "名称", value: key.name },
          { label: "Scopes", value: key.scopes.join(", ") || "—" },
          { label: "状态", value: key.enabled ? "Active" : "Disabled" },
          { label: "过期时间", value: key.expire_at ? new Date(key.expire_at).toLocaleString() : "永不过期" },
          { label: "创建时间", value: new Date(key.created_at).toLocaleString() },
          { label: "更新时间", value: new Date(key.updated_at).toLocaleString() },
        ]}
      />
    </DetailPageWrapper>
  );
}
