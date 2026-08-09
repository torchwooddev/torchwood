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
	return projectID, userID, nil
}
