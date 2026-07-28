package client

import (
	"context"

	domainauth "github.com/deeploop-ai/graviton/internal/domain/auth"
	"github.com/deeploop-ai/graviton/internal/domain/databases"
	"github.com/deeploop-ai/graviton/internal/domain/messaging"
	"github.com/deeploop-ai/graviton/internal/domain/projects"
	infraauth "github.com/deeploop-ai/graviton/internal/infra/auth"
	inframessaging "github.com/deeploop-ai/graviton/internal/infra/messaging"
	"github.com/deeploop-ai/graviton/internal/pkg/config"
	"github.com/redis/go-redis/v9"
)

func NewTestAccount(cfg *config.AppConfig, projectRepo projects.Repository, docDB databases.DocumentDB) *Account {
	return NewTestAccountWithRedis(cfg, projectRepo, docDB, nil)
}

func NewTestAccountWithRedis(cfg *config.AppConfig, projectRepo projects.Repository, docDB databases.DocumentDB, rdb *redis.Client) *Account {
	return NewTestAccountWithDeps(cfg, projectRepo, nil, docDB, rdb, nil, nil)
}

func NewTestAccountWithMailer(cfg *config.AppConfig, projectRepo projects.Repository, docDB databases.DocumentDB, rdb *redis.Client, mailer messaging.Mailer) *Account {
	return NewTestAccountWithDeps(cfg, projectRepo, nil, docDB, rdb, mailer, nil)
}

func NewTestAccountWithDeps(
	cfg *config.AppConfig,
	projectRepo projects.Repository,
	oauthProviders projects.OAuthProviderRepository,
	docDB databases.DocumentDB,
	rdb *redis.Client,
	mailer messaging.Mailer,
	sms messaging.SMSSender,
) *Account {
	roles := NewUserRoles(docDB)
	var rotation domainauth.RefreshRotationStore
	if rdb != nil {
		rotation = infraauth.NewRedisRefreshRotationStore(rdb)
	}
	sessions := infraauth.NewSessionService(cfg, docDB, roles, rotation)
	var otp domainauth.OTPChallengeStore
	var oauthState domainauth.OAuthStateStore
	var tokens domainauth.AccountTokenStore
	var loginThrottle domainauth.LoginThrottle
	if rdb != nil {
		otp = infraauth.NewRedisOTPChallengeStore(rdb, cfg)
		oauthState = infraauth.NewRedisOAuthStateStore(rdb)
		tokens = infraauth.NewRedisAccountTokenStore(rdb)
		loginThrottle = infraauth.NewRedisLoginThrottle(rdb)
	}
	if mailer == nil {
		mailer = inframessaging.NewMailer(cfg)
	}
	if sms == nil {
		sms = inframessaging.NewSMSService(cfg)
	}
	return NewAccount(cfg, projectRepo, oauthProviders, docDB, sessions, otp, oauthState, tokens, loginThrottle, rotation, nil, mailer, sms)
}

// CaptureMailer records sent messages for tests.
type CaptureMailer struct {
	Subjects []string
	Bodies   []string
}

func (m *CaptureMailer) Send(_ context.Context, _, subject, body string) error {
	m.Subjects = append(m.Subjects, subject)
	m.Bodies = append(m.Bodies, body)
	return nil
}

// CaptureSMSSender records sent SMS for tests.
type CaptureSMSSender struct {
	To   []string
	Body []string
}

func (s *CaptureSMSSender) Send(_ context.Context, to, body string) error {
	s.To = append(s.To, to)
	s.Body = append(s.Body, body)
	return nil
}
