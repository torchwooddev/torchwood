package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/redis/go-redis/v9"
	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"github.com/torchwooddev/torchwood/pkg/secretbox"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	totpPeriod          = 30
	totpDigits          = 6
	totpSkew            = 1
	totpCodeReuseTTL    = 60 * time.Second
	mfaFailWindow       = 15 * time.Minute
	mfaFailMaxAttempts  = 5
	totpPendingActivate = 10 * time.Minute
)

// TOTPService implements domainauth.MFAService with secretbox-encrypted secrets
// stored on the factor, replay protection and per-factor failure locking in Redis.
type TOTPService struct {
	cfg *config.AppConfig
	rdb *redis.Client
}

func NewTOTPService(cfg *config.AppConfig, rdb *redis.Client) domainauth.MFAService {
	return &TOTPService{cfg: cfg, rdb: rdb}
}

func (s *TOTPService) jwtSecret() string {
	return s.cfg.GetSecurity().GetJwt().GetSecret()
}

func (s *TOTPService) CreateTOTPFactor(ctx context.Context, issuer, userID, email string) (*domainauth.Factor, string, string, error) {
	if s.jwtSecret() == "" {
		return nil, "", "", status.Error(codes.Internal, "mfa secret is not configured")
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: email,
		Period:      totpPeriod,
		Digits:      otp.Digits(totpDigits),
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return nil, "", "", status.Error(codes.Internal, "totp key generation failed")
	}
	encrypted, err := secretbox.Encrypt(key.Secret(), s.jwtSecret())
	if err != nil {
		return nil, "", "", status.Error(codes.Internal, "totp secret encryption failed")
	}
	factor := &domainauth.Factor{
		ID:        "fac_" + idgen.UUID().String(),
		Type:      domainauth.FactorTypeTOTP,
		Secret:    encrypted,
		Status:    domainauth.FactorStatusPending,
		CreatedAt: time.Now(),
	}
	return factor, key.Secret(), key.URL(), nil
}

func (s *TOTPService) VerifyTOTPFactor(ctx context.Context, factor *domainauth.Factor, code string) error {
	if factor == nil {
		return status.Error(codes.Internal, "mfa factor is required")
	}
	if err := s.checkFactorLock(ctx, factor.ID); err != nil {
		return err
	}
	plain, err := s.decryptSecret(factor)
	if err != nil {
		return err
	}
	if !s.validate(ctx, plain, code) {
		if err := s.recordFactorFailure(ctx, factor.ID); err != nil {
			return err
		}
		return status.Error(codes.Unauthenticated, "invalid mfa code")
	}
	if err := s.claimUsedCode(ctx, plain); err != nil {
		return err
	}
	_ = s.clearFactorFailures(ctx, factor.ID)
	return nil
}

func (s *TOTPService) ValidateTOTP(ctx context.Context, factor *domainauth.Factor, code string) error {
	if factor == nil {
		return status.Error(codes.Internal, "mfa factor is required")
	}
	plain, err := s.decryptSecret(factor)
	if err != nil {
		return err
	}
	if !s.validate(ctx, plain, code) {
		return status.Error(codes.Unauthenticated, "invalid mfa code")
	}
	return nil
}

func (s *TOTPService) decryptSecret(factor *domainauth.Factor) (string, error) {
	if s.jwtSecret() == "" {
		return "", status.Error(codes.Internal, "mfa secret is not configured")
	}
	plain, err := secretbox.Decrypt(factor.Secret, s.jwtSecret())
	if err != nil || plain == "" {
		return "", status.Error(codes.Internal, "mfa secret decryption failed")
	}
	return plain, nil
}

func (s *TOTPService) validate(ctx context.Context, plainSecret, code string) bool {
	ok, err := totp.ValidateCustom(code, plainSecret, time.Now(), totp.ValidateOpts{
		Period:    totpPeriod,
		Skew:      totpSkew,
		Digits:    otp.Digits(totpDigits),
		Algorithm: otp.AlgorithmSHA1,
	})
	return err == nil && ok
}

// claimUsedCode 防重放：对 skew 覆盖的三个时间窗口（step-1/step/step+1）全部
// 原子 SETNX 占用，任一已被占用则视为同一验证码重放，拒绝本次校验。
func (s *TOTPService) claimUsedCode(ctx context.Context, plainSecret string) error {
	if s.rdb == nil {
		return nil
	}
	sum := sha256.Sum256([]byte(plainSecret))
	digest := hex.EncodeToString(sum[:])
	now := time.Now().Unix()
	for _, step := range []int64{now/totpPeriod - 1, now / totpPeriod, now/totpPeriod + 1} {
		key := fmt.Sprintf("Torchwood:mfa:used:%s:%d", digest, step)
		ok, err := s.rdb.SetNX(ctx, key, "1", totpCodeReuseTTL).Result()
		if err != nil {
			return status.Error(codes.Internal, "mfa replay check failed")
		}
		if !ok {
			return status.Error(codes.Unauthenticated, "mfa code already used")
		}
	}
	return nil
}

func (s *TOTPService) checkFactorLock(ctx context.Context, factorID string) error {
	if s.rdb == nil || factorID == "" {
		return nil
	}
	key := mfaFailKey(factorID)
	count, err := s.rdb.Get(ctx, key).Int()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return status.Error(codes.Internal, "mfa lock check failed")
	}
	if count >= mfaFailMaxAttempts {
		return status.Error(codes.ResourceExhausted, "mfa attempts exceeded")
	}
	return nil
}

func (s *TOTPService) recordFactorFailure(ctx context.Context, factorID string) error {
	if s.rdb == nil || factorID == "" {
		return nil
	}
	key := mfaFailKey(factorID)
	pipe := s.rdb.TxPipeline()
	pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, mfaFailWindow)
	if _, err := pipe.Exec(ctx); err != nil {
		return status.Error(codes.Internal, "mfa failure tracking failed")
	}
	return nil
}

func (s *TOTPService) clearFactorFailures(ctx context.Context, factorID string) error {
	if s.rdb == nil || factorID == "" {
		return nil
	}
	return s.rdb.Del(ctx, mfaFailKey(factorID)).Err()
}

func mfaFailKey(factorID string) string {
	return "Torchwood:mfa:fail:" + factorID
}

var _ domainauth.MFAService = (*TOTPService)(nil)
