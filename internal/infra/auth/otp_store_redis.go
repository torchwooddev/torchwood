package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	otpChallengeTTL   = 5 * time.Minute
	otpSendCooldown   = 60 * time.Second
	otpIPWindow       = time.Hour
	otpIPMaxPerWindow = 10
	otpMaxAttempts    = 5
)

// otpHMACKeyNamespace 为 OTP 哈希密钥加命名空间前缀，避免与其它 HMAC 用途混用同一密钥。
const otpHMACKeyNamespace = "torchwood-otp:"

// otpVerifyScript 原子完成"读取 challenge -> 校验归属 -> 校验尝试次数 -> 比对验证码 ->
// 成功删除 / 失败递增尝试次数"，避免 GET-改-SET 竞态导致正确验证码被并发重放签发多个会话。
// 返回值：ok / notfound / mismatch / locked / badcode。
const otpVerifyScript = `
local vals = redis.call('HMGET', KEYS[1], 'project_id', 'channel', 'target', 'code_hash', 'attempts')
if not vals[1] then
  return 'notfound'
end
if vals[1] ~= ARGV[1] or vals[2] ~= ARGV[2] or vals[3] ~= ARGV[3] then
  return 'mismatch'
end
local attempts = tonumber(vals[5]) or 0
if attempts >= tonumber(ARGV[5]) then
  redis.call('DEL', KEYS[1])
  return 'locked'
end
if vals[4] ~= ARGV[4] then
  redis.call('HINCRBY', KEYS[1], 'attempts', 1)
  return 'badcode'
end
redis.call('DEL', KEYS[1])
return 'ok'
`

// RedisOTPChallengeStore stores OTP challenges in Redis.
type RedisOTPChallengeStore struct {
	rdb *redis.Client
	// hmacKey 用于对 OTP 验证码做 HMAC-SHA256 存储，Redis 数据泄露时无法对 6 位验证码离线爆破。
	hmacKey []byte
}

func NewRedisOTPChallengeStore(rdb *redis.Client, cfg *config.AppConfig) *RedisOTPChallengeStore {
	return &RedisOTPChallengeStore{
		rdb:     rdb,
		hmacKey: []byte(otpHMACKeyNamespace + cfg.GetSecurity().GetJwt().GetSecret()),
	}
}

func (s *RedisOTPChallengeStore) hashCode(code string) string {
	mac := hmac.New(sha256.New, s.hmacKey)
	mac.Write([]byte(code))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *RedisOTPChallengeStore) CheckSendRateLimit(ctx context.Context, projectID, target, ip string) error {
	sendKey := fmt.Sprintf("Torchwood:otp:send:%s:%s", projectID, target)
	ok, err := s.rdb.SetNX(ctx, sendKey, "1", otpSendCooldown).Result()
	if err != nil {
		return status.Error(codes.Internal, "otp rate limit check failed")
	}
	if !ok {
		return status.Error(codes.ResourceExhausted, "otp send cooldown active")
	}

	if ip == "" {
		return nil
	}
	ipKey := fmt.Sprintf("Torchwood:otp:ip:%s:%s", projectID, ip)
	count, err := s.rdb.Incr(ctx, ipKey).Result()
	if err != nil {
		return status.Error(codes.Internal, "otp ip rate limit check failed")
	}
	if count == 1 {
		if err := s.rdb.Expire(ctx, ipKey, otpIPWindow).Err(); err != nil {
			return status.Error(codes.Internal, "otp ip rate limit check failed")
		}
	}
	if count > otpIPMaxPerWindow {
		return status.Error(codes.ResourceExhausted, "otp ip rate limit exceeded")
	}
	return nil
}

func (s *RedisOTPChallengeStore) CreateEmailChallenge(ctx context.Context, projectID, email, code string) (string, time.Time, error) {
	return s.createChallenge(ctx, projectID, domainauth.OTPChannelEmail, email, code)
}

func (s *RedisOTPChallengeStore) VerifyEmailChallenge(ctx context.Context, projectID, challengeID, email, code string) error {
	return s.verifyChallenge(ctx, projectID, challengeID, domainauth.OTPChannelEmail, email, code)
}

func (s *RedisOTPChallengeStore) CreatePhoneChallenge(ctx context.Context, projectID, phone, code string) (string, time.Time, error) {
	return s.createChallenge(ctx, projectID, domainauth.OTPChannelPhone, phone, code)
}

func (s *RedisOTPChallengeStore) VerifyPhoneChallenge(ctx context.Context, projectID, challengeID, phone, code string) error {
	return s.verifyChallenge(ctx, projectID, challengeID, domainauth.OTPChannelPhone, phone, code)
}

func (s *RedisOTPChallengeStore) createChallenge(ctx context.Context, projectID, channel, target, code string) (string, time.Time, error) {
	challengeID := newChallengeID()
	expireAt := time.Now().Add(otpChallengeTTL)
	key := challengeKey(challengeID)
	pipe := s.rdb.TxPipeline()
	pipe.HSet(ctx, key, map[string]any{
		"project_id": projectID,
		"channel":    channel,
		"target":     target,
		"code_hash":  s.hashCode(code),
		"attempts":   0,
	})
	pipe.Expire(ctx, key, otpChallengeTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return "", time.Time{}, status.Error(codes.Internal, "otp challenge store failed")
	}
	return challengeID, expireAt, nil
}

func (s *RedisOTPChallengeStore) verifyChallenge(ctx context.Context, projectID, challengeID, channel, target, code string) error {
	key := challengeKey(challengeID)
	result, err := s.rdb.Eval(ctx, otpVerifyScript, []string{key},
		projectID, channel, target, s.hashCode(code), otpMaxAttempts).Text()
	if err != nil {
		if strings.Contains(err.Error(), "WRONGTYPE") {
			// 旧版本以 JSON 字符串格式写入的在途 challenge 按无效处理（TTL 仅 5 分钟，影响短暂）。
			return status.Error(codes.Unauthenticated, "invalid or expired otp challenge")
		}
		return status.Error(codes.Internal, "otp challenge verify failed")
	}

	switch result {
	case "ok":
		return nil
	case "locked":
		return status.Error(codes.ResourceExhausted, "otp attempts exceeded")
	case "badcode":
		return status.Error(codes.Unauthenticated, "invalid otp code")
	default: // notfound / mismatch
		return status.Error(codes.Unauthenticated, "invalid or expired otp challenge")
	}
}

func challengeKey(challengeID string) string {
	return "Torchwood:otp:ch:" + challengeID
}

func newChallengeID() string {
	return idgen.UUID().String()
}
