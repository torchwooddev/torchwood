package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	verificationTokenTTL = 24 * time.Hour
	recoveryTokenTTL     = time.Hour
	magicURLTokenTTL     = time.Hour
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

func (s *RedisAccountTokenStore) CreateMagicURLToken(ctx context.Context, projectID, userID, email string) (string, string, time.Time, error) {
	secret, expireAt, err := s.createToken(ctx, projectID, userID, email, domainauth.AccountTokenPurposeMagicURL, magicURLTokenTTL)
	if err != nil {
		return "", "", time.Time{}, err
	}
	// 不透明 challengeID 与 secret 无关，仅用于 API 响应回传；secret 只在邮件链接里。
	challengeID, err := generateAccountTokenSecret()
	if err != nil {
		return "", "", time.Time{}, status.Error(codes.Internal, "account token generation failed")
	}
	return challengeID, secret, expireAt, nil
}

func (s *RedisAccountTokenStore) VerifyMagicURLToken(ctx context.Context, projectID, userID, secret string) error {
	return s.verifyToken(ctx, projectID, userID, secret, domainauth.AccountTokenPurposeMagicURL)
}

func (s *RedisAccountTokenStore) CreateEmailChangeToken(ctx context.Context, projectID, userID, email string) (string, time.Time, error) {
	return s.createToken(ctx, projectID, userID, email, domainauth.AccountTokenPurposeEmailChange, verificationTokenTTL)
}

// VerifyEmailChangeToken 原子消费邮箱变更 token 并返回 record 中的新邮箱。
func (s *RedisAccountTokenStore) VerifyEmailChangeToken(ctx context.Context, projectID, userID, secret string) (string, error) {
	return s.verifyTokenWithEmail(ctx, projectID, userID, secret, domainauth.AccountTokenPurposeEmailChange)
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

// verifyToken 通过 GETDEL 原子消费：校验与删除一体，杜绝并发双消费
// （recovery 双重置 / magic URL 双会话）。
func (s *RedisAccountTokenStore) verifyToken(ctx context.Context, projectID, userID, secret, purpose string) error {
	_, err := s.verifyTokenWithEmail(ctx, projectID, userID, secret, purpose)
	return err
}

// verifyTokenWithEmail 同 verifyToken，额外返回 record 中携带的 email
// （email_change 消费后需要新邮箱地址）。
func (s *RedisAccountTokenStore) verifyTokenWithEmail(ctx context.Context, projectID, userID, secret, purpose string) (string, error) {
	key := accountTokenKey(purpose, projectID, userID)
	raw, err := s.rdb.GetDel(ctx, key).Bytes()
	if err == redis.Nil {
		return "", status.Error(codes.Unauthenticated, "invalid or expired account token")
	}
	if err != nil {
		return "", status.Error(codes.Internal, "account token lookup failed")
	}
	var record accountTokenRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return "", status.Error(codes.Internal, "account token decode failed")
	}
	if record.ProjectID != projectID || record.UserID != userID || record.Purpose != purpose {
		return "", status.Error(codes.Unauthenticated, "invalid or expired account token")
	}
	if record.SecretHash != HashOTP(secret) {
		return "", status.Error(codes.Unauthenticated, "invalid or expired account token")
	}
	return record.Email, nil
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
