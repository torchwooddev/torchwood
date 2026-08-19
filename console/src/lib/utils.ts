import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/** 展示 int64（网关可能给 string）；禁止 Number()/parseFloat。 */
export function formatInt64(value: string | number | undefined | null): string {
  if (value === undefined || value === null || value === "") return "0";
  return String(value);
}

/** 校验表单里的最小单位整数（可空）。 */
export function isInt64Input(value: string): boolean {
  return value === "" || /^-?\d+$/.test(value);
}
