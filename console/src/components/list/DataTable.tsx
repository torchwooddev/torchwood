import { Link } from "react-router-dom";
import { Eye, Pencil } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export interface ColumnDef<T> {
  key: string;
  header: string;
  cell: (item: T) => React.ReactNode;
  className?: string;
}

interface DataTableProps<T extends { id: string }> {
  items: T[];
  columns: ColumnDef<T>[];
  selectable?: boolean;
  allSelected?: boolean;
  someSelected?: boolean;
  isSelected: (id: string) => boolean;
  onToggle: (id: string) => void;
  onToggleAll: (checked: boolean) => void;
  isRowSelectable?: (item: T) => boolean;
  detailPath?: (item: T) => string;
  editPath?: (item: T) => string;
  rowActions?: (item: T) => React.ReactNode;
}

export function DataTable<T extends { id: string }>({
  items,
  columns,
  selectable = true,
  allSelected,
  someSelected,
  isSelected,
  onToggle,
  onToggleAll,
  isRowSelectable,
  detailPath,
  editPath,
  rowActions,
}: DataTableProps<T>) {
  const hasActions = !!(detailPath || editPath || rowActions);

  const selectableItems = isRowSelectable ? items.filter(isRowSelectable) : items;
  const headerChecked =
    isRowSelectable && selectableItems.length > 0
      ? selectableItems.every((item) => isSelected(item.id))
      : allSelected;
  const headerIndeterminate =
    isRowSelectable && selectableItems.length > 0
      ? selectableItems.some((item) => isSelected(item.id)) && !headerChecked
      : someSelected;

  const handleToggleAll = (checked: boolean) => {
    if (!isRowSelectable) {
      onToggleAll(checked);
      return;
    }
    onToggleAll(false);
    if (checked) {
      selectableItems.forEach((item) => onToggle(item.id));
    }
  };

  return (
    <Table>
      <TableHeader>
        <TableRow>
          {selectable && (
            <TableHead className="w-12">
              <Checkbox
                checked={headerChecked}
                indeterminate={headerIndeterminate}
                onChange={(e) => handleToggleAll(e.target.checked)}
                aria-label="全选"
              />
            </TableHead>
          )}
          {columns.map((col) => (
            <TableHead key={col.key} className={col.className}>
              {col.header}
            </TableHead>
          ))}
          {hasActions && <TableHead className="w-32 text-right">操作</TableHead>}
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.map((item) => {
          const rowSelectable = !isRowSelectable || isRowSelectable(item);
          return (
            <TableRow key={item.id} data-state={isSelected(item.id) ? "selected" : undefined}>
              {selectable && (
                <TableCell>
                  <Checkbox
                    checked={isSelected(item.id)}
                    disabled={!rowSelectable}
                    onChange={() => onToggle(item.id)}
                    aria-label={`选择 ${item.id}`}
                  />
                </TableCell>
              )}
              {columns.map((col) => (
                <TableCell key={col.key} className={col.className}>
                  {col.cell(item)}
                </TableCell>
              ))}
              {hasActions && (
                <TableCell className="text-right">
                  <div className="flex items-center justify-end gap-1">
                    {detailPath && (
                      <Button variant="ghost" size="icon" asChild>
                        <Link to={detailPath(item)} title="查看详情">
                          <Eye className="h-4 w-4" />
                        </Link>
                      </Button>
                    )}
                    {editPath && (
                      <Button variant="ghost" size="icon" asChild>
                        <Link to={editPath(item)} title="编辑">
                          <Pencil className="h-4 w-4" />
                        </Link>
                      </Button>
                    )}
                    {rowActions?.(item)}
                  </div>
                </TableCell>
              )}
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}
