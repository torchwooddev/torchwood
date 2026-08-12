package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	mfaChallengeTTL     = 5 * time.Minute
	mfaChallengeKeyPre  = "Torchwood:mfa:challenge:"
	mfaChallengeUserPre = "Torchwood:mfa:challenge:user:"
	mfaChallengeMissing = "invalid or expired challenge"
)

// RedisMFAChallengeStore stores one-time MFA login challenge tokens in Redis.
type RedisMFAChallengeStore struct {
	rdb *redis.Client
}

func NewRedisMFAChallengeStore(rdb *redis.Client) domainauth.MFAChallengeStore {
	return &RedisMFAChallengeStore{rdb: rdb}
}

func (s *RedisMFAChallengeStore) Create(ctx context.Context, projectID, userID string) (string, time.Time, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, status.Error(codes.Internal, "mfa challenge generation failed")
	}
	token := hex.EncodeToString(buf)
	key := mfaChallengeKeyPre + token
	value := projectID + ":" + userID
	if err := s.rdb.Set(ctx, key, value, mfaChallengeTTL).Err(); err != nil {
		return "", time.Time{}, status.Error(codes.Internal, "mfa challenge store failed")
	}
	// 用户索引：删除因子时作废该用户全部未消费挑战。
	idxKey := mfaChallengeUserKey(projectID, userID)
	if err := s.rdb.SAdd(ctx, idxKey, token).Err(); err != nil {
		return "", time.Time{}, status.Error(codes.Internal, "mfa challenge store failed")
	}
	if err := s.rdb.Expire(ctx, idxKey, mfaChallengeTTL).Err(); err != nil {
		return "", time.Time{}, status.Error(codes.Internal, "mfa challenge store failed")
	}
	return token, time.Now().Add(mfaChallengeTTL), nil
}

func (s *RedisMFAChallengeStore) Consume(ctx context.Context, token string) (string, string, error) {
	if token == "" {
		return "", "", status.Error(codes.Unauthenticated, mfaChallengeMissing)
	}
	value, err := s.rdb.GetDel(ctx, mfaChallengeKeyPre+token).Result()
	if err == redis.Nil {
		// 不区分无效/过期/已用，防探测。
		return "", "", status.Error(codes.Unauthenticated, mfaChallengeMissing)
	}
	if err != nil {
		return "", "", status.Error(codes.Internal, "mfa challenge lookup failed")
	}
	projectID, userID, ok := strings.Cut(value, ":")
	if !ok || projectID == "" || userID == "" {
		return "", "", status.Error(codes.Unauthenticated, mfaChallengeMissing)
	}
	_ = s.rdb.SRem(ctx, mfaChallengeUserKey(projectID, userID), token).Err()
	return projectID, userID, nil
}

// RevokeByUser 作废该用户全部未消费的挑战（删除 MFA 因子时调用）。
func (s *RedisMFAChallengeStore) RevokeByUser(ctx context.Context, projectID, userID string) error {
	idxKey := mfaChallengeUserKey(projectID, userID)
	tokens, err := s.rdb.SMembers(ctx, idxKey).Result()
	if err != nil {
		return status.Error(codes.Internal, "mfa challenge revocation failed")
	}
	keys := make([]string, 0, len(tokens))
	for _, token := range tokens {
		keys = append(keys, mfaChallengeKeyPre+token)
	}
	if len(keys) > 0 {
		if err := s.rdb.Del(ctx, keys...).Err(); err != nil {
			return status.Error(codes.Internal, "mfa challenge revocation failed")
		}
	}
	if err := s.rdb.Del(ctx, idxKey).Err(); err != nil {
		return status.Error(codes.Internal, "mfa challenge revocation failed")
	}
	return nil
}

func mfaChallengeUserKey(projectID, userID string) string {
	return mfaChallengeUserPre + projectID + ":" + userID
}
