import { useCallback, useState, useEffect } from "react";
import { Link, useNavigate, useParams, useOutletContext } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Plus, Settings2, Shield, Hash, ListTree, Fingerprint, Calendar } from "lucide-react";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  CollectionStatCard,
  type CollectionOutletContext,
} from "@/routes/databases/CollectionLayout";
import {
  listDatabases,
  getDatabase,
  createDatabase,
  deleteDatabase,
  listCollections,
  getCollection,
  createCollection,
  deleteCollection,
  updateCollection,
  createAttribute,
  createIndex,
  listDocuments,
  getDocument,
  createDocument,
  updateDocument,
  deleteDocument,
  type Database,
  type Collection,
  type Attribute,
  type Document,
} from "@/api/databases";
import { useAuth } from "@/hooks/useAuth";
import { ResourceListPage } from "@/components/list/ResourceListPage";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
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
import { PermissionEditor } from "@/components/resource/PermissionEditor";

const ATTRIBUTE_TYPES = [
  { value: "string", label: "String" },
  { value: "integer", label: "Integer" },
  { value: "float", label: "Float" },
  { value: "boolean", label: "Boolean" },
  { value: "datetime", label: "Datetime" },
  { value: "email", label: "Email" },
  { value: "url", label: "URL" },
  { value: "json", label: "JSON" },
] as const;

const INDEX_TYPES = [
  { value: "key", label: "Key" },
  { value: "unique", label: "Unique" },
  { value: "fulltext", label: "Fulltext" },
] as const;

const STRING_LIKE_TYPES = new Set(["string", "email", "url"]);

function AttributeList({
  attributes,
  onAdd,
  disabled,
}: {
  attributes: Attribute[];
  onAdd: () => void;
  disabled?: boolean;
}) {
  return (
    <Card className="flex flex-col">
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-4">
        <div>
          <CardTitle className="text-base">Attributes</CardTitle>
          <p className="mt-1 text-sm text-muted-foreground">定义文档字段类型与约束</p>
        </div>
        <Button size="sm" onClick={onAdd} disabled={disabled}>
          <Plus className="mr-2 h-4 w-4" />
          添加
        </Button>
      </CardHeader>
      <CardContent className="flex-1">
        {attributes.length === 0 ? (
          <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-10 text-center">
            <ListTree className="mb-3 h-8 w-8 text-muted-foreground/50" />
            <p className="text-sm text-muted-foreground">暂无字段定义</p>
            {!disabled && (
              <Button size="sm" variant="outline" className="mt-4" onClick={onAdd}>
                添加第一个 Attribute
              </Button>
            )}
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Key</TableHead>
                <TableHead>Type</TableHead>
                <TableHead>约束</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {attributes.map((attr) => (
                <TableRow key={attr.id}>
                  <TableCell className="font-mono font-medium">{attr.key}</TableCell>
                  <TableCell>
                    <Badge variant="outline">
                      {attr.type}
                      {attr.array ? "[]" : ""}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-1.5">
                      {attr.required && <Badge variant="secondary">required</Badge>}
                      {attr.array && <Badge variant="secondary">array</Badge>}
                      {attr.size ? <Badge variant="secondary">size {attr.size}</Badge> : null}
                      {!attr.required && !attr.array && !attr.size && (
                        <span className="text-sm text-muted-foreground">—</span>
                      )}
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}

function IndexList({
  indexes,
  onAdd,
  canAdd,
  disabled,
}: {
  indexes: Collection["indexes"];
  onAdd: () => void;
  canAdd: boolean;
  disabled?: boolean;
}) {
  return (
    <Card className="flex flex-col">
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-4">
        <div>
          <CardTitle className="text-base">Indexes</CardTitle>
          <p className="mt-1 text-sm text-muted-foreground">为查询性能创建索引</p>
        </div>
        <Button size="sm" onClick={onAdd} disabled={!canAdd || disabled}>
          <Plus className="mr-2 h-4 w-4" />
          添加
        </Button>
      </CardHeader>
      <CardContent className="flex-1">
        {!canAdd && !disabled && (
          <p className="mb-4 rounded-md bg-muted/50 px-3 py-2 text-sm text-muted-foreground">
            请先添加至少一个 Attribute，再创建 Index。
          </p>
        )}
        {indexes.length === 0 ? (
          <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-10 text-center">
            <Hash className="mb-3 h-8 w-8 text-muted-foreground/50" />
            <p className="text-sm text-muted-foreground">
              {disabled ? "暂无索引" : canAdd ? "暂无索引" : "添加 Attribute 后可创建索引"}
            </p>
            {canAdd && !disabled && (
              <Button size="sm" variant="outline" className="mt-4" onClick={onAdd}>
                添加第一个 Index
              </Button>
            )}
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>Attributes</TableHead>
                <TableHead>Type</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {indexes.map((idx) => (
                <TableRow key={idx.id}>
                  <TableCell className="font-mono text-xs">{idx.id}</TableCell>
                  <TableCell className="font-mono text-sm">{idx.attributes.join(", ")}</TableCell>
                  <TableCell>
                    <Badge variant="outline">{idx.type}</Badge>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}

function AddAttributeDialog({
  open,
  onOpenChange,
  loading,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  loading: boolean;
  onSubmit: (input: {
    key: string;
    type: string;
    size?: number;
    required: boolean;
    array: boolean;
  }) => void;
}) {
  const [key, setKey] = useState("");
  const [type, setType] = useState("string");
  const [size, setSize] = useState("");
  const [required, setRequired] = useState(false);
  const [array, setArray] = useState(false);

  const reset = () => {
    setKey("");
    setType("string");
    setSize("");
    setRequired(false);
    setArray(false);
  };

  const handleOpenChange = (next: boolean) => {
    if (!next) reset();
    onOpenChange(next);
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>添加 Attribute</DialogTitle>
          <DialogDescription>为 Collection 定义字段类型与约束。</DialogDescription>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            onSubmit({
              key: key.trim(),
              type,
              size: size ? Number(size) : undefined,
              required,
              array,
            });
          }}
        >
          <div className="space-y-2">
            <Label htmlFor="attr-key">Key</Label>
            <Input
              id="attr-key"
              value={key}
              onChange={(e) => setKey(e.target.value)}
              placeholder="title"
              required
            />
          </div>
          <div className="space-y-2">
            <Label>Type</Label>
            <Select value={type} onValueChange={setType}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ATTRIBUTE_TYPES.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {STRING_LIKE_TYPES.has(type) && (
            <div className="space-y-2">
              <Label htmlFor="attr-size">Size（可选）</Label>
              <Input
                id="attr-size"
                type="number"
                min={1}
                value={size}
                onChange={(e) => setSize(e.target.value)}
                placeholder="256"
              />
            </div>
          )}
          <div className="flex flex-wrap gap-6">
            <label className="flex items-center gap-2 text-sm">
              <Checkbox checked={required} onChange={(e) => setRequired(e.target.checked)} />
              Required
            </label>
            <label className="flex items-center gap-2 text-sm">
              <Checkbox checked={array} onChange={(e) => setArray(e.target.checked)} />
              Array
            </label>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => handleOpenChange(false)}>
              取消
            </Button>
            <Button type="submit" disabled={loading || !key.trim()}>
              {loading ? "添加中..." : "添加"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function AddIndexDialog({
  open,
  onOpenChange,
  loading,
  attributes,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  loading: boolean;
  attributes: Attribute[];
  onSubmit: (input: { id: string; type: string; attributes: string[] }) => void;
}) {
  const [id, setId] = useState("");
  const [type, setType] = useState("key");
  const [selected, setSelected] = useState<string[]>([]);

  const reset = () => {
    setId("");
    setType("key");
    setSelected([]);
  };

  const handleOpenChange = (next: boolean) => {
    if (!next) reset();
    onOpenChange(next);
  };

  const toggleAttribute = (key: string) => {
    setSelected((prev) =>
      prev.includes(key) ? prev.filter((item) => item !== key) : [...prev, key]
    );
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>添加 Index</DialogTitle>
          <DialogDescription>选择索引类型，并指定参与索引的 Attribute。</DialogDescription>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            onSubmit({ id: id.trim(), type, attributes: selected });
          }}
        >
          <div className="space-y-2">
            <Label htmlFor="index-id">ID</Label>
            <Input
              id="index-id"
              value={id}
              onChange={(e) => setId(e.target.value)}
              placeholder="title_key"
              required
            />
          </div>
          <div className="space-y-2">
            <Label>Type</Label>
            <Select value={type} onValueChange={setType}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {INDEX_TYPES.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label>Attributes</Label>
            <div className="rounded-md border divide-y max-h-48 overflow-y-auto">
              {attributes.map((attr) => (
                <label
                  key={attr.key}
                  className="flex items-center gap-2 px-3 py-2 text-sm cursor-pointer hover:bg-muted/50"
                >
                  <Checkbox
                    checked={selected.includes(attr.key)}
                    onChange={() => toggleAttribute(attr.key)}
                  />
                  <span className="font-mono">{attr.key}</span>
                  <Badge variant="outline" className="ml-auto">
                    {attr.type}
                  </Badge>
                </label>
              ))}
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => handleOpenChange(false)}>
              取消
            </Button>
            <Button type="submit" disabled={loading || !id.trim() || selected.length === 0}>
              {loading ? "添加中..." : "添加"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function EditPermissionsDialog({
  open,
  onOpenChange,
  loading,
  initialPermissions,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  loading: boolean;
  initialPermissions: string[];
  onSubmit: (permissions: string[]) => void;
}) {
  const [permissions, setPermissions] = useState<string[]>([]);

  useEffect(() => {
    if (open) {
      setPermissions(initialPermissions);
    }
  }, [open, initialPermissions]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>编辑 Collection 权限</DialogTitle>
          <DialogDescription>
            修改集合级权限规则。无文档级权限的文档将回退到此规则。
          </DialogDescription>
        </DialogHeader>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            onSubmit(permissions);
          }}
        >
          <PermissionEditor permissions={permissions} onChange={setPermissions} />
          <DialogFooter className="mt-4">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button type="submit" disabled={loading}>
              {loading ? "保存中..." : "保存"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

const dbColumns: ColumnDef<Database>[] = [
  {
    key: "id",
    header: "ID",
    className: "font-mono text-xs max-w-[140px] truncate",
    cell: (d) => d.id,
  },
  { key: "name", header: "名称", cell: (d) => d.name },
  {
    key: "created",
    header: "创建时间",
    cell: (d) => new Date(d.created_at).toLocaleString(),
  },
];

export function DatabasesListPage() {
  const { projectId } = useAuth();
  const queryClient = useQueryClient();
  const [bulkDeleting, setBulkDeleting] = useState(false);

  const { data: databases = [], isLoading } = useQuery({
    queryKey: ["databases", projectId],
    queryFn: listDatabases,
    enabled: !!projectId,
  });

  const remove = useMutation({
    mutationFn: deleteDatabase,
    onSuccess: () => {
      toast.success("Database 已删除");
      queryClient.invalidateQueries({ queryKey: ["databases"] });
    },
  });

  const getSearchText = useCallback((d: Database) => `${d.id} ${d.name}`, []);

  const handleBulkDelete = async (selected: Database[], clear: () => void) => {
    setBulkDeleting(true);
    try {
      await Promise.all(selected.map((d) => deleteDatabase(d.id)));
      toast.success(`已删除 ${selected.length} 个 Database`);
      queryClient.invalidateQueries({ queryKey: ["databases"] });
      clear();
    } finally {
      setBulkDeleting(false);
    }
  };

  return (
    <ResourceListPage
      title="Databases"
      description="管理数据库与集合"
      searchPlaceholder="搜索数据库名称或 ID..."
      isLoading={isLoading}
      items={databases}
      columns={dbColumns}
      getSearchText={getSearchText}
      detailPath={(d) => `/console/databases/${d.id}`}
      toolbarActions={
        <Button asChild>
          <Link to="/console/databases/new">
            <Plus className="h-4 w-4 mr-2" />
            新建 Database
          </Link>
        </Button>
      }
      selectionActions={(selected, clear) => (
        <BulkDeleteButton
          count={selected.length}
          loading={bulkDeleting}
          onConfirm={() => handleBulkDelete(selected, clear)}
        />
      )}
      rowActions={(d) => (
        <RowDeleteButton onConfirm={() => remove.mutate(d.id)} loading={remove.isPending} />
      )}
      emptyTitle="暂无 Database"
      emptyDescription="创建第一个 Database"
      emptyAction={
        <Button asChild>
          <Link to="/console/databases/new">新建 Database</Link>
        </Button>
      }
    />
  );
}

export function DatabaseNewPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [id, setId] = useState("");

  const mutation = useMutation({
    mutationFn: createDatabase,
    onSuccess: (db) => {
      toast.success("Database 创建成功");
      queryClient.invalidateQueries({ queryKey: ["databases"] });
      navigate(`/console/databases/${db.id}`);
    },
  });

  return (
    <FormPageWrapper
      title="新建 Database"
      backTo="/console/databases"
      submitLabel="创建"
      onSubmit={(e) => {
        e.preventDefault();
        mutation.mutate({
          id: id || name.toLowerCase().replace(/\s+/g, "_"),
          name,
        });
      }}
      loading={mutation.isPending}
    >
      <FormField id="name" label="名称" value={name} onChange={setName} required placeholder="Production DB" />
      <FormField id="id" label="ID（可选）" value={id} onChange={setId} placeholder="production" />
    </FormPageWrapper>
  );
}

export function DatabaseDetailPage() {
  const { dbId } = useParams<{ dbId: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [bulkDeleting, setBulkDeleting] = useState(false);

  const { data: database, isLoading: dbLoading } = useQuery({
    queryKey: ["databases", dbId],
    queryFn: () => getDatabase(dbId!),
    enabled: !!dbId,
  });

  const { data: collections = [], isLoading: collLoading } = useQuery({
    queryKey: ["collections", dbId],
    queryFn: () => listCollections(dbId!),
    enabled: !!dbId,
  });

  const removeDb = useMutation({
    mutationFn: deleteDatabase,
    onSuccess: () => {
      toast.success("Database 已删除");
      queryClient.invalidateQueries({ queryKey: ["databases"] });
      navigate("/console/databases");
    },
  });

  const removeColl = useMutation({
    mutationFn: (collId: string) => deleteCollection(dbId!, collId),
    onSuccess: () => {
      toast.success("Collection 已删除");
      queryClient.invalidateQueries({ queryKey: ["collections", dbId] });
    },
  });

  const collColumns: ColumnDef<Collection>[] = [
    {
      key: "id",
      header: "ID",
      className: "font-mono text-xs max-w-[140px] truncate",
      cell: (c) => (
        <span className="flex items-center gap-2">
          <span className="truncate">{c.id}</span>
          {c.is_system && <Badge variant="secondary">系统</Badge>}
        </span>
      ),
    },
    { key: "name", header: "名称", cell: (c) => c.name },
    {
      key: "attributes",
      header: "Attributes",
      cell: (c) => <Badge variant="secondary">{c.attributes.length}</Badge>,
    },
    {
      key: "indexes",
      header: "Indexes",
      cell: (c) => <Badge variant="secondary">{c.indexes.length}</Badge>,
    },
  ];

  const getCollSearchText = useCallback(
    (c: Collection) => `${c.id} ${c.name}`,
    []
  );

  const handleBulkDeleteColl = async (selected: Collection[], clear: () => void) => {
    setBulkDeleting(true);
    try {
      const deletable = selected.filter((c) => !c.is_system);
      if (deletable.length === 0) {
        clear();
        return;
      }
      await Promise.all(deletable.map((c) => deleteCollection(dbId!, c.id)));
      toast.success(`已删除 ${deletable.length} 个 Collection`);
      queryClient.invalidateQueries({ queryKey: ["collections", dbId] });
      clear();
    } finally {
      setBulkDeleting(false);
    }
  };

  if (dbLoading) return <DetailSkeleton />;
  if (!database) return <NotFound backTo="/console/databases" />;

  return (
    <div className="space-y-6">
      <DetailPageWrapper
        title={database.name}
        description="Database 详情与 Collection 管理"
        backTo="/console/databases"
        actions={
          <DeleteButton
            onConfirm={() => removeDb.mutate(database.id)}
            loading={removeDb.isPending}
          />
        }
      >
        <DetailGrid
          items={[
            { label: "ID", value: database.id, mono: true },
            { label: "名称", value: database.name },
            { label: "创建时间", value: new Date(database.created_at).toLocaleString() },
          ]}
        />
      </DetailPageWrapper>

      <ResourceListPage
        title=""
        cardTitle="Collections"
        searchPlaceholder="搜索 Collection..."
        isLoading={collLoading}
        items={collections}
        columns={collColumns}
        getSearchText={getCollSearchText}
        isRowSelectable={(c) => !c.is_system}
        detailPath={(c) => `/console/databases/${dbId}/collections/${c.id}`}
        toolbarActions={
          <Button asChild>
            <Link to={`/console/databases/${dbId}/collections/new`}>
              <Plus className="h-4 w-4 mr-2" />
              新建 Collection
            </Link>
          </Button>
        }
        selectionActions={(selected, clear) => (
          <BulkDeleteButton
            count={selected.filter((c) => !c.is_system).length}
            loading={bulkDeleting}
            onConfirm={() => handleBulkDeleteColl(selected, clear)}
          />
        )}
        rowActions={(c) =>
          c.is_system ? null : (
            <RowDeleteButton
              onConfirm={() => removeColl.mutate(c.id)}
              loading={removeColl.isPending}
            />
          )
        }
        emptyTitle="暂无 Collection"
        emptyDescription="在此 Database 中创建 Collection"
        emptyAction={
          <Button asChild>
            <Link to={`/console/databases/${dbId}/collections/new`}>新建 Collection</Link>
          </Button>
        }
      />
    </div>
  );
}

export function CollectionNewPage() {
  const { dbId } = useParams<{ dbId: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [id, setId] = useState("");
  const [permissions, setPermissions] = useState<string[]>([]);

  const mutation = useMutation({
    mutationFn: () =>
      createCollection(dbId!, {
        id: id || name.toLowerCase().replace(/\s+/g, "_"),
        name,
        permissions: permissions.length > 0 ? permissions : undefined,
      }),
    onSuccess: (coll) => {
      toast.success("Collection 创建成功");
      queryClient.invalidateQueries({ queryKey: ["collections", dbId] });
      navigate(`/console/databases/${dbId}/collections/${coll.id}`);
    },
  });

  return (
    <FormPageWrapper
      title="新建 Collection"
      backTo={`/console/databases/${dbId}`}
      backLabel="返回 Database"
      submitLabel="创建"
      onSubmit={(e) => {
        e.preventDefault();
        mutation.mutate();
      }}
      loading={mutation.isPending}
    >
      <FormField id="name" label="名称" value={name} onChange={setName} required placeholder="posts" />
      <FormField id="id" label="ID（可选）" value={id} onChange={setId} placeholder="posts" />
      <div className="pt-2 border-t">
        <PermissionEditor permissions={permissions} onChange={setPermissions} />
      </div>
    </FormPageWrapper>
  );
}

export function CollectionDetailPage() {
  const { dbId, collId } = useOutletContext<CollectionOutletContext>();
  const queryClient = useQueryClient();
  const [attrDialogOpen, setAttrDialogOpen] = useState(false);
  const [indexDialogOpen, setIndexDialogOpen] = useState(false);
  const [permDialogOpen, setPermDialogOpen] = useState(false);

  const { data: collection, isLoading } = useQuery({
    queryKey: ["collections", dbId, collId],
    queryFn: () => getCollection(dbId, collId),
  });

  const updatePerms = useMutation({
    mutationFn: (input: { permissions: string[] }) =>
      updateCollection(dbId, collId, input),
    onSuccess: () => {
      toast.success("权限已更新");
      queryClient.invalidateQueries({ queryKey: ["collections", dbId, collId] });
      setPermDialogOpen(false);
    },
  });

  const addAttribute = useMutation({
    mutationFn: (input: {
      key: string;
      type: string;
      size?: number;
      required: boolean;
      array: boolean;
    }) => createAttribute(dbId, collId, input),
    onSuccess: () => {
      toast.success("Attribute 已添加");
      queryClient.invalidateQueries({ queryKey: ["collections", dbId, collId] });
      queryClient.invalidateQueries({ queryKey: ["collections", dbId] });
      setAttrDialogOpen(false);
    },
  });

  const addIndex = useMutation({
    mutationFn: (input: { id: string; type: string; attributes: string[] }) =>
      createIndex(dbId, collId, {
        ...input,
        orders: input.attributes.map(() => "asc"),
      }),
    onSuccess: () => {
      toast.success("Index 已添加");
      queryClient.invalidateQueries({ queryKey: ["collections", dbId, collId] });
      queryClient.invalidateQueries({ queryKey: ["collections", dbId] });
      setIndexDialogOpen(false);
    },
  });

  if (isLoading) return <DetailSkeleton />;
  if (!collection) return null;

  const readonly = collection.is_system;

  return (
    <>
      <div className="space-y-6">
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <CollectionStatCard icon={Fingerprint} label="Collection ID" value={collection.id} mono />
          <CollectionStatCard
            icon={ListTree}
            label="Attributes"
            value={collection.attributes.length}
          />
          <CollectionStatCard icon={Hash} label="Indexes" value={collection.indexes.length} />
          <CollectionStatCard
            icon={Calendar}
            label="创建时间"
            value={new Date(collection.created_at).toLocaleDateString()}
          />
        </div>

        <Card>
          <CardHeader className="flex flex-row items-start justify-between space-y-0">
            <div>
              <CardTitle className="flex items-center gap-2 text-base">
                <Shield className="h-4 w-4" />
                权限规则
              </CardTitle>
              <p className="mt-1 text-sm text-muted-foreground">
                集合级权限；无文档级权限的文档将回退到此规则
              </p>
            </div>
            <Button size="sm" variant="outline" onClick={() => setPermDialogOpen(true)} disabled={readonly}>
              <Settings2 className="mr-1 h-4 w-4" />
              编辑
            </Button>
          </CardHeader>
          <CardContent>
            {collection.permissions.length > 0 ? (
              <div className="flex flex-wrap gap-2">
                {collection.permissions.map((p) => (
                  <Badge key={p} variant="secondary" className="font-mono text-xs">
                    {p}
                  </Badge>
                ))}
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">
                未设置自定义权限规则，使用系统默认策略。
              </p>
            )}
          </CardContent>
        </Card>

        <div className="grid gap-6 lg:grid-cols-2">
          <AttributeList
            attributes={collection.attributes}
            onAdd={() => setAttrDialogOpen(true)}
            disabled={readonly}
          />
          <IndexList
            indexes={collection.indexes}
            canAdd={collection.attributes.length > 0}
            onAdd={() => setIndexDialogOpen(true)}
            disabled={readonly}
          />
        </div>
      </div>

      <AddAttributeDialog
        open={attrDialogOpen}
        onOpenChange={setAttrDialogOpen}
        loading={addAttribute.isPending}
        onSubmit={(input) => addAttribute.mutate(input)}
      />
      <AddIndexDialog
        open={indexDialogOpen}
        onOpenChange={setIndexDialogOpen}
        loading={addIndex.isPending}
        attributes={collection.attributes}
        onSubmit={(input) => addIndex.mutate(input)}
      />
      <EditPermissionsDialog
        open={permDialogOpen}
        onOpenChange={setPermDialogOpen}
        loading={updatePerms.isPending}
        initialPermissions={collection.permissions}
        onSubmit={(perms) => updatePerms.mutate({ permissions: perms })}
      />
    </>
  );
}

const documentColumns: ColumnDef<Document>[] = [
  {
    key: "id",
    header: "ID",
    className: "font-mono text-xs max-w-[160px] truncate",
    cell: (d) => d.id,
  },
  {
    key: "updated",
    header: "更新时间",
    cell: (d) => new Date(d.updated_at).toLocaleString(),
  },
];

function DocumentListSection({
  dbId,
  collId,
  attributes,
  readonly,
}: {
  dbId: string;
  collId: string;
  attributes: Attribute[];
  readonly?: boolean;
}) {
  const queryClient = useQueryClient();
  const [bulkDeleting, setBulkDeleting] = useState(false);
  const { data: documents = [], isLoading } = useQuery({
    queryKey: ["documents", dbId, collId],
    queryFn: () => listDocuments(dbId, collId),
  });

  const remove = useMutation({
    mutationFn: (docId: string) => deleteDocument(dbId, collId, docId),
    onSuccess: () => {
      toast.success("Document 已删除");
      queryClient.invalidateQueries({ queryKey: ["documents", dbId, collId] });
    },
  });

  const columns: ColumnDef<Document>[] = [
    ...documentColumns,
    ...attributes.slice(0, 4).map((attr) => ({
      key: attr.key,
      header: attr.key,
      cell: (d: Document) => {
        const val = d.data?.[attr.key];
        if (val == null) return "—";
        const text = typeof val === "object" ? JSON.stringify(val) : String(val);
        return text.length > 48 ? `${text.slice(0, 48)}…` : text;
      },
    })),
  ];

  const getSearchText = useCallback(
    (d: Document) => `${d.id} ${JSON.stringify(d.data ?? {})}`,
    []
  );

  const handleBulkDelete = async (selected: Document[], clear: () => void) => {
    setBulkDeleting(true);
    try {
      await Promise.all(selected.map((d) => deleteDocument(dbId, collId, d.id)));
      toast.success(`已删除 ${selected.length} 个 Document`);
      queryClient.invalidateQueries({ queryKey: ["documents", dbId, collId] });
      clear();
    } finally {
      setBulkDeleting(false);
    }
  };

  return (
    <ResourceListPage
      cardTitle="文档"
      searchPlaceholder="搜索 Document ID 或字段内容..."
      isLoading={isLoading}
      items={documents}
      columns={columns}
      getSearchText={getSearchText}
      toolbarActions={
        readonly ? undefined : (
          <Button asChild size="sm">
            <Link to={`/console/databases/${dbId}/collections/${collId}/documents/new`}>
              <Plus className="mr-2 h-4 w-4" />
              新建 Document
            </Link>
          </Button>
        )
      }
      selectionActions={
        readonly
          ? undefined
          : (selected, clear) => (
              <BulkDeleteButton
                count={selected.length}
                loading={bulkDeleting}
                onConfirm={() => handleBulkDelete(selected, clear)}
              />
            )
      }
      detailPath={(d) =>
        `/console/databases/${dbId}/collections/${collId}/documents/${d.id}`
      }
      rowActions={(d) =>
        readonly ? null : (
          <RowDeleteButton
            onConfirm={() => remove.mutate(d.id)}
            loading={remove.isPending}
          />
        )
      }
      emptyTitle="暂无 Document"
      emptyDescription={readonly ? undefined : "在此 Collection 中创建第一条文档记录"}
      emptyAction={
        readonly ? undefined : (
          <Button asChild size="sm">
            <Link to={`/console/databases/${dbId}/collections/${collId}/documents/new`}>
              新建 Document
            </Link>
          </Button>
        )
      }
    />
  );
}

export function DocumentsListPage() {
  const { dbId, collId } = useOutletContext<CollectionOutletContext>();

  const { data: collection, isLoading } = useQuery({
    queryKey: ["collections", dbId, collId],
    queryFn: () => getCollection(dbId, collId),
  });

  if (isLoading) return <DetailSkeleton />;
  if (!collection) return null;

  return (
    <DocumentListSection
      dbId={dbId}
      collId={collId}
      attributes={collection.attributes}
      readonly={collection.is_system}
    />
  );
}

function parseFieldValue(type: string, raw: string): unknown {
  if (raw === "") return null;
  switch (type) {
    case "integer":
      return Number.parseInt(raw, 10);
    case "float":
      return Number.parseFloat(raw);
    case "boolean":
      return raw === "true";
    case "json":
      return JSON.parse(raw);
    default:
      return raw;
  }
}

function DocumentFormFields({
  attributes,
  values,
  onChange,
}: {
  attributes: Attribute[];
  values: Record<string, string>;
  onChange: (key: string, value: string) => void;
}) {
  if (attributes.length === 0) {
    return (
      <FormField
        id="payload"
        label="Data (JSON)"
        value={values.__json ?? "{}"}
        onChange={(v) => onChange("__json", v)}
        placeholder='{"title":"Hello"}'
      />
    );
  }

  return (
    <>
      {attributes.map((attr) => (
        <FormField
          key={attr.key}
          id={attr.key}
          label={`${attr.key} (${attr.type})`}
          value={values[attr.key] ?? ""}
          onChange={(v) => onChange(attr.key, v)}
          required={attr.required}
          type={attr.type === "integer" || attr.type === "float" ? "number" : "text"}
        />
      ))}
    </>
  );
}

function buildDocumentData(
  attributes: Attribute[],
  values: Record<string, string>
): Record<string, unknown> {
  if (attributes.length === 0) {
    return JSON.parse(values.__json || "{}") as Record<string, unknown>;
  }
  const data: Record<string, unknown> = {};
  for (const attr of attributes) {
    if (values[attr.key] === undefined || values[attr.key] === "") {
      if (attr.required) {
        throw new Error(`${attr.key} is required`);
      }
      continue;
    }
    data[attr.key] = parseFieldValue(attr.type, values[attr.key]);
  }
  return data;
}

export function DocumentNewPage() {
  const { dbId, collId } = useParams();
  const navigate = useNavigate();
  const [values, setValues] = useState<Record<string, string>>({ __json: "{}" });
  const [permissions, setPermissions] = useState<string[]>([]);

  const { data: collection, isLoading } = useQuery({
    queryKey: ["collections", dbId, collId],
    queryFn: () => getCollection(dbId!, collId!),
    enabled: !!dbId && !!collId,
  });

  const create = useMutation({
    mutationFn: () =>
      createDocument(dbId!, collId!, {
        data: buildDocumentData(collection!.attributes, values),
        permissions: permissions.length > 0 ? permissions : undefined,
      }),
    onSuccess: (doc) => {
      toast.success("Document 已创建");
      navigate(`/console/databases/${dbId}/collections/${collId}/documents/${doc.id}`);
    },
    onError: (err: Error) => toast.error(err.message),
  });

  if (isLoading) return <DetailSkeleton />;
  if (!collection) {
    return <NotFound backTo={`/console/databases/${dbId}/collections/${collId}/documents`} />;
  }

  const documentsPath = `/console/databases/${dbId}/collections/${collId}/documents`;

  return (
    <FormPageWrapper
      title="新建 Document"
      description={`Collection: ${collection.name}`}
      backTo={documentsPath}
      backLabel="返回文档列表"
      loading={create.isPending}
      submitLabel="创建"
      onSubmit={(e) => {
        e.preventDefault();
        create.mutate();
      }}
    >
      <DocumentFormFields
        attributes={collection.attributes}
        values={values}
        onChange={(key, value) => setValues((prev) => ({ ...prev, [key]: value }))}
      />
      <div className="pt-2 border-t">
        <PermissionEditor permissions={permissions} onChange={setPermissions} />
      </div>
    </FormPageWrapper>
  );
}

export function DocumentDetailPage() {
  const { dbId, collId, docId } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [values, setValues] = useState<Record<string, string>>({});
  const [initialized, setInitialized] = useState(false);

  const { data: collection } = useQuery({
    queryKey: ["collections", dbId, collId],
    queryFn: () => getCollection(dbId!, collId!),
    enabled: !!dbId && !!collId,
  });

  const { data: document, isLoading } = useQuery({
    queryKey: ["documents", dbId, collId, docId],
    queryFn: () => getDocument(dbId!, collId!, docId!),
    enabled: !!dbId && !!collId && !!docId,
  });

  useEffect(() => {
    if (!document || initialized) return;
    const next: Record<string, string> = {};
    if ((collection?.attributes.length ?? 0) === 0) {
      next.__json = JSON.stringify(document.data ?? {}, null, 2);
    } else {
      for (const attr of collection?.attributes ?? []) {
        const raw = document.data?.[attr.key];
        next[attr.key] = raw == null ? "" : String(raw);
      }
    }
    setValues(next);
    setInitialized(true);
  }, [collection, document, initialized]);

  const save = useMutation({
    mutationFn: () =>
      updateDocument(dbId!, collId!, docId!, {
        data: buildDocumentData(collection!.attributes, values),
      }),
    onSuccess: () => {
      toast.success("Document 已更新");
      queryClient.invalidateQueries({ queryKey: ["documents", dbId, collId] });
      queryClient.invalidateQueries({ queryKey: ["documents", dbId, collId, docId] });
    },
    onError: (err: Error) => toast.error(err.message),
  });

  const remove = useMutation({
    mutationFn: () => deleteDocument(dbId!, collId!, docId!),
    onSuccess: () => {
      toast.success("Document 已删除");
      navigate(`/console/databases/${dbId}/collections/${collId}/documents`);
    },
  });

  const documentsPath = `/console/databases/${dbId}/collections/${collId}/documents`;

  if (isLoading) return <DetailSkeleton />;
  if (!document || !collection) {
    return <NotFound backTo={documentsPath} />;
  }

  return (
    <DetailPageWrapper
      title="Document"
      description={`ID: ${document.id}`}
      backTo={documentsPath}
      backLabel="返回文档列表"
      actions={
        collection.is_system ? null : (
          <DeleteButton onConfirm={() => remove.mutate()} loading={remove.isPending} />
        )
      }
    >
      <DetailGrid
        items={[
          { label: "ID", value: document.id, mono: true },
          { label: "创建时间", value: new Date(document.created_at).toLocaleString() },
          { label: "更新时间", value: new Date(document.updated_at).toLocaleString() },
        ]}
      />
      <Card className="mt-6">
        <CardContent className="pt-6">
          <form
            onSubmit={(e) => {
              e.preventDefault();
              save.mutate();
            }}
            className="space-y-4 max-w-lg"
          >
            <DocumentFormFields
              attributes={collection.attributes}
              values={values}
              onChange={(key, value) => setValues((prev) => ({ ...prev, [key]: value }))}
            />
            <Button type="submit" disabled={save.isPending}>
              {save.isPending ? "保存中..." : "保存"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </DetailPageWrapper>
  );
}
