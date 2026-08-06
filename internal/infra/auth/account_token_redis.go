package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	domainauth "github.com/torchwoodio/torchwood/internal/domain/auth"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	verificationTokenTTL = 24 * time.Hour
	recoveryTokenTTL     = time.Hour
)

type accountTokenRecord struct {
	ProjectID  string `json:"project_id"`
	UserID     string `json:"user_id"`
	Email      string `json:"email,omitempty"`
	Purpose    string `json:"purpose"`
	SecretHash string `json:"secret_hash"`
}

// RedisAccountTokenStore stores account verification and recovery tokens in Redis.
type RedisAccountTokenStore struct {
	rdb *redis.Client
}

func NewRedisAccountTokenStore(rdb *redis.Client) *RedisAccountTokenStore {
	return &RedisAccountTokenStore{rdb: rdb}
}

func (s *RedisAccountTokenStore) CheckSendRateLimit(ctx context.Context, projectID, target, ip string) error {
	store := &RedisOTPChallengeStore{rdb: s.rdb}
	return store.CheckSendRateLimit(ctx, projectID, target, ip)
}

func (s *RedisAccountTokenStore) CreateVerificationToken(ctx context.Context, projectID, userID, email string) (string, time.Time, error) {
	return s.createToken(ctx, projectID, userID, email, domainauth.AccountTokenPurposeVerification, verificationTokenTTL)
}

func (s *RedisAccountTokenStore) VerifyVerificationToken(ctx context.Context, projectID, userID, secret string) error {
	return s.verifyToken(ctx, projectID, userID, secret, domainauth.AccountTokenPurposeVerification)
}

func (s *RedisAccountTokenStore) CreateRecoveryToken(ctx context.Context, projectID, userID, email string) (string, time.Time, error) {
	return s.createToken(ctx, projectID, userID, email, domainauth.AccountTokenPurposeRecovery, recoveryTokenTTL)
}

func (s *RedisAccountTokenStore) VerifyRecoveryToken(ctx context.Context, projectID, userID, secret string) error {
	return s.verifyToken(ctx, projectID, userID, secret, domainauth.AccountTokenPurposeRecovery)
}

func (s *RedisAccountTokenStore) createToken(ctx context.Context, projectID, userID, email, purpose string, ttl time.Duration) (string, time.Time, error) {
	secret, err := generateAccountTokenSecret()
	if err != nil {
		return "", time.Time{}, status.Error(codes.Internal, "account token generation failed")
	}
	expireAt := time.Now().Add(ttl)
	record := accountTokenRecord{
		ProjectID:  projectID,
		UserID:     userID,
		Email:      email,
		Purpose:    purpose,
		SecretHash: HashOTP(secret),
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return "", time.Time{}, status.Error(codes.Internal, "account token encode failed")
	}
	key := accountTokenKey(purpose, projectID, userID)
	if err := s.rdb.Set(ctx, key, payload, ttl).Err(); err != nil {
		return "", time.Time{}, status.Error(codes.Internal, "account token store failed")
	}
	return secret, expireAt, nil
}

func (s *RedisAccountTokenStore) verifyToken(ctx context.Context, projectID, userID, secret, purpose string) error {
	key := accountTokenKey(purpose, projectID, userID)
	raw, err := s.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return status.Error(codes.Unauthenticated, "invalid or expired account token")
	}
	if err != nil {
		return status.Error(codes.Internal, "account token lookup failed")
	}
	var record accountTokenRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return status.Error(codes.Internal, "account token decode failed")
	}
	if record.ProjectID != projectID || record.UserID != userID || record.Purpose != purpose {
		return status.Error(codes.Unauthenticated, "invalid or expired account token")
	}
	if record.SecretHash != HashOTP(secret) {
		return status.Error(codes.Unauthenticated, "invalid or expired account token")
	}
	if err := s.rdb.Del(ctx, key).Err(); err != nil {
		return status.Error(codes.Internal, "account token cleanup failed")
	}
	return nil
}

func accountTokenKey(purpose, projectID, userID string) string {
	return fmt.Sprintf("Torchwood:account:token:%s:%s:%s", purpose, projectID, userID)
}

func generateAccountTokenSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
