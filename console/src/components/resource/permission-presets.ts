export const PERMISSION_PRESETS: Record<string, string[]> = {
  "公开可读": ["read:any"],
  "认证用户可写": ["read:users", "create:users", "update:users", "delete:users"],
  "仅 API Key": ["read:keys", "create:keys", "update:keys", "delete:keys"],
  "用户组可读": ["read:group:<id>"],
};
