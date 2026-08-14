package auth

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// incrWithTTLScript 原子执行 INCR + 首次 EXPIRE（Round3 H6-4）：原来的
// INCR 后 count==1 再 EXPIRE 存在崩溃窗口——进程在两条命令之间挂掉会留下
// 无 TTL 计数键，永久锁死该邮箱/IP/资源。Lua 内两条命令原子执行，
// 不存在「计数已增但 TTL 未设」的中间态。
const incrWithTTLScript = `
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return count
`

// incrWithTTL 原子自增计数器并保证首次自增后键带 TTL（失败路径不留下
// 无 TTL 键）。供 login throttle / 通用限流 / OTP IP 窗口共用。
func incrWithTTL(ctx context.Context, rdb *redis.Client, key string, ttl time.Duration) (int64, error) {
	return rdb.Eval(ctx, incrWithTTLScript, []string{key}, int64(ttl.Seconds())).Int64()
}
