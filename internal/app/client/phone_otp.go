package client

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/users"
	infraauth "github.com/torchwooddev/torchwood/internal/infra/auth"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/pkg/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var phoneDigitsOnly = regexp.MustCompile(`[^\d+]`)

type CreatePhoneOTPCommand struct {
	ProjectID string
	Phone     string
}

type CreatePhoneOTPSessionCommand struct {
	ProjectID   string
	Phone       string
	ChallengeID string
	OTP         string
}

func (a *Account) CreatePhoneOTP(ctx context.Context, cmd CreatePhoneOTPCommand) (*Challenge, error) {
	if a.otp == nil {
		return nil, status.Error(codes.Unimplemented, "phone otp is not configured")
	}
	if a.sms == nil {
		return nil, status.Error(codes.Unimplemented, "sms delivery is not configured")
	}
	projectID := strings.TrimSpace(cmd.ProjectID)
	phone, err := normalizePhone(cmd.Phone)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	if projectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}

	project, err := a.projectRepo.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, status.Error(codes.NotFound, "project not found")
	}
	if err := a.docDB.EnsureSystemCollections(ctx, project.ID, project.InternalID); err != nil {
		return nil, err
	}

	clientInfo := contexts.ClientInfoFrom(ctx)
	if err := a.otp.CheckSendRateLimit(ctx, projectID, phone, clientInfo.IP); err != nil {
		return nil, err
	}

	code, err := infraauth.GenerateOTP(6)
	if err != nil {
		return nil, status.Error(codes.Internal, "otp generation failed")
	}
	challengeID, expireAt, err := a.otp.CreatePhoneChallenge(ctx, projectID, phone, code)
	if err != nil {
		return nil, err
	}

	body := fmt.Sprintf("Your Torchwood sign-in code is: %s. It expires in 5 minutes.", code)
	if err := a.sms.Send(ctx, phone, body); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to send otp sms: %v", err)
	}

	return &Challenge{ChallengeID: challengeID, ExpireAt: expireAt}, nil
}

func (a *Account) CreatePhoneOTPSession(ctx context.Context, cmd CreatePhoneOTPSessionCommand) (*User, *TokenBundle, string, *MFASignInChallenge, error) {
	if a.otp == nil {
		return nil, nil, "", nil, status.Error(codes.Unimplemented, "phone otp is not configured")
	}
	projectID := strings.TrimSpace(cmd.ProjectID)
	phone, err := normalizePhone(cmd.Phone)
	if err != nil {
		return nil, nil, "", nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	challengeID := strings.TrimSpace(cmd.ChallengeID)
	otp := strings.TrimSpace(cmd.OTP)
	if projectID == "" {
		return nil, nil, "", nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	if challengeID == "" {
		return nil, nil, "", nil, status.Error(codes.InvalidArgument, "challenge_id is required")
	}
	if otp == "" {
		return nil, nil, "", nil, status.Error(codes.InvalidArgument, "otp is required")
	}

	project, err := a.projectRepo.GetProject(ctx, projectID)
	if err != nil {
		return nil, nil, "", nil, err
	}
	if project == nil {
		return nil, nil, "", nil, status.Error(codes.NotFound, "project not found")
	}
	if err := a.docDB.EnsureSystemCollections(ctx, project.ID, project.InternalID); err != nil {
		return nil, nil, "", nil, err
	}

	if err := a.otp.VerifyPhoneChallenge(ctx, projectID, challengeID, phone, otp); err != nil {
		return nil, nil, "", nil, err
	}

	user, err := a.findOrCreateUserByPhone(ctx, projectID, phone)
	if err != nil {
		return nil, nil, "", nil, err
	}
	if !users.CanAuthenticate(user.Status) {
		return nil, nil, "", nil, status.Error(codes.Unauthenticated, "user account is not active")
	}
	return a.finishSignInWithProvider(ctx, projectID, user, domainauth.ProviderPhoneOTP)
}

func (a *Account) findOrCreateUserByPhone(ctx context.Context, projectID, phone string) (*User, error) {
	list, err := a.docDB.ListDocuments(ctx, projectID, databases.SystemDatabaseID, "users", databases.Query{
		Queries:  []string{query.BuildEqual("phone", phone)},
		PageSize: 1,
	}, databases.SystemPrincipal)
	if err != nil {
		return nil, err
	}
	if len(list.Documents) > 0 {
		user := mapUserDoc(&list.Documents[0])
		if verified, _ := list.Documents[0].Data["phone_verified"].(bool); !verified {
			updated, err := a.docDB.UpdateDocument(ctx, projectID, databases.SystemDatabaseID, "users", databases.SimpleDocumentUpdate(databases.Document{
				ID:   user.ID,
				Data: map[string]any{"phone_verified": true},
			}, nil), databases.SystemPrincipal)
			if err == nil {
				user = mapUserDoc(&updated)
			}
		}
		return user, nil
	}

	userID, err := a.generateUserID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	placeholderEmail := phonePlaceholderEmail(phone)
	userDoc := databases.Document{
		ID: userID,
		Data: map[string]any{
			"email":          placeholderEmail,
			"password_hash":  "",
			"phone":          phone,
			"phone_verified": true,
			"name":           phone,
			"status":         users.StatusActive,
			"email_verified": false,
			"labels":         []any{},
			"prefs":          map[string]any{},
		},
	}
	userPerms := userDocumentPermissions(userID)
	if _, err := a.docDB.CreateDocument(ctx, projectID, databases.SystemDatabaseID, "users", userDoc, userPerms, databases.SystemPrincipal); err != nil {
		if errors.Is(err, documentdb.ErrDuplicateKey) {
			list, listErr := a.docDB.ListDocuments(ctx, projectID, databases.SystemDatabaseID, "users", databases.Query{
				Queries:  []string{query.BuildEqual("phone", phone)},
				PageSize: 1,
			}, databases.SystemPrincipal)
			if listErr != nil {
				return nil, listErr
			}
			if len(list.Documents) > 0 {
				return mapUserDoc(&list.Documents[0]), nil
			}
		}
		return nil, fmt.Errorf("create user document: %w", err)
	}
	return mapUserDoc(&userDoc), nil
}

func normalizePhone(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("phone is required")
	}
	cleaned := phoneDigitsOnly.ReplaceAllString(raw, "")
	if !strings.HasPrefix(cleaned, "+") {
		cleaned = "+" + strings.TrimPrefix(cleaned, "+")
	}
	digits := strings.TrimPrefix(cleaned, "+")
	if len(digits) < 8 || len(digits) > 15 {
		return "", fmt.Errorf("phone number must contain 8 to 15 digits")
	}
	for _, ch := range digits {
		if ch < '0' || ch > '9' {
			return "", fmt.Errorf("phone number contains invalid characters")
		}
	}
	return "+" + digits, nil
}

func phonePlaceholderEmail(phone string) string {
	safe := strings.TrimPrefix(phone, "+")
	return fmt.Sprintf("phone_%s@torchwood.local", safe)
}
