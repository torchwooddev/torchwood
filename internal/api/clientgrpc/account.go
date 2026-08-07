package clientgrpc

import (
	"context"

	clientv1 "github.com/torchwooddev/torchwood/genproto/client/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	"github.com/torchwooddev/torchwood/internal/app/client"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type AccountService struct {
	clientv1.UnimplementedAccountServiceServer
	account *client.Account
}

func NewAccountService(account *client.Account) *AccountService {
	return &AccountService{account: account}
}

func (s *AccountService) SignUp(ctx context.Context, req *clientv1.SignUpRequest) (*clientv1.SignUpResponse, error) {
	user, tokens, _, err := s.account.SignUp(ctx, client.SignUpCommand{
		ProjectID: req.GetProjectId(),
		Email:     req.GetEmail(),
		Password:  req.GetPassword(),
		Name:      req.GetName(),
	})
	if err != nil {
		return nil, err
	}
	return &clientv1.SignUpResponse{
		Account: mapUser(user),
		Tokens:  mapTokens(tokens),
	}, nil
}

func (s *AccountService) SignIn(ctx context.Context, req *clientv1.SignInRequest) (*clientv1.SignInResponse, error) {
	user, tokens, _, err := s.account.SignIn(ctx, client.SignInCommand{
		ProjectID: req.GetProjectId(),
		Email:     req.GetEmail(),
		Password:  req.GetPassword(),
	})
	if err != nil {
		return nil, err
	}
	return &clientv1.SignInResponse{
		Account: mapUser(user),
		Tokens:  mapTokens(tokens),
	}, nil
}

func (s *AccountService) SignOut(ctx context.Context, _ *clientv1.SignOutRequest) (*sharedv1.Empty, error) {
	if _, ok := contexts.Principal(ctx); !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	if err := s.account.SignOut(ctx); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func (s *AccountService) Me(ctx context.Context, _ *clientv1.MeRequest) (*clientv1.Account, error) {
	user, err := s.account.Me(ctx)
	if err != nil {
		return nil, err
	}
	return mapUser(user), nil
}

func (s *AccountService) RefreshToken(ctx context.Context, req *clientv1.RefreshTokenRequest) (*clientv1.RefreshTokenResponse, error) {
	tokens, _, err := s.account.RefreshToken(ctx, client.RefreshTokenCommand{
		ProjectID:    req.GetProjectId(),
		RefreshToken: req.GetRefreshToken(),
	})
	if err != nil {
		return nil, err
	}
	return &clientv1.RefreshTokenResponse{Tokens: mapTokens(tokens)}, nil
}

func (s *AccountService) UpdateAccount(ctx context.Context, req *clientv1.UpdateAccountRequest) (*clientv1.Account, error) {
	user, err := s.account.UpdateAccount(ctx, client.UpdateAccountCommand{
		Name:        req.GetName(),
		Email:       req.GetEmail(),
		Password:    req.GetPassword(),
		OldPassword: req.GetOldPassword(),
	})
	if err != nil {
		return nil, err
	}
	return mapUser(user), nil
}

func (s *AccountService) ListSessions(ctx context.Context, _ *clientv1.ListSessionsRequest) (*clientv1.ListSessionsResponse, error) {
	sessions, err := s.account.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*clientv1.Session, len(sessions))
	for i := range sessions {
		out[i] = mapSession(&sessions[i])
	}
	return &clientv1.ListSessionsResponse{Sessions: out}, nil
}

func (s *AccountService) DeleteSession(ctx context.Context, req *clientv1.DeleteSessionRequest) (*sharedv1.Empty, error) {
	if err := s.account.DeleteSession(ctx, req.GetSessionId()); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func (s *AccountService) DeleteSessions(ctx context.Context, req *clientv1.DeleteSessionsRequest) (*sharedv1.Empty, error) {
	if err := s.account.DeleteSessions(ctx, req.GetKeepCurrent()); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func (s *AccountService) GetPrefs(ctx context.Context, _ *clientv1.GetPrefsRequest) (*clientv1.GetPrefsResponse, error) {
	prefs, err := s.account.GetPrefs(ctx)
	if err != nil {
		return nil, err
	}
	data, err := structpb.NewStruct(prefs)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "prefs is not serializable")
	}
	return &clientv1.GetPrefsResponse{Prefs: data}, nil
}

func (s *AccountService) UpdatePrefs(ctx context.Context, req *clientv1.UpdatePrefsRequest) (*clientv1.GetPrefsResponse, error) {
	if req.GetPrefs() == nil {
		return nil, status.Error(codes.InvalidArgument, "prefs is required")
	}
	prefs, err := s.account.UpdatePrefs(ctx, req.GetPrefs().AsMap())
	if err != nil {
		return nil, err
	}
	data, err := structpb.NewStruct(prefs)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "prefs is not serializable")
	}
	return &clientv1.GetPrefsResponse{Prefs: data}, nil
}

func (s *AccountService) CreateEmailOTP(ctx context.Context, req *clientv1.CreateEmailOTPRequest) (*clientv1.ChallengeResponse, error) {
	challenge, err := s.account.CreateEmailOTP(ctx, client.CreateEmailOTPCommand{
		ProjectID: req.GetProjectId(),
		Email:     req.GetEmail(),
	})
	if err != nil {
		return nil, err
	}
	return &clientv1.ChallengeResponse{
		ChallengeId: challenge.ChallengeID,
		ExpireAt:    challenge.ExpireAt.Unix(),
	}, nil
}

func (s *AccountService) CreateEmailOTPSession(ctx context.Context, req *clientv1.CreateEmailOTPSessionRequest) (*clientv1.SignInResponse, error) {
	user, tokens, _, err := s.account.CreateEmailOTPSession(ctx, client.CreateEmailOTPSessionCommand{
		ProjectID:   req.GetProjectId(),
		Email:       req.GetEmail(),
		ChallengeID: req.GetChallengeId(),
		OTP:         req.GetOtp(),
	})
	if err != nil {
		return nil, err
	}
	return &clientv1.SignInResponse{
		Account: mapUser(user),
		Tokens:  mapTokens(tokens),
	}, nil
}

func (s *AccountService) CreateOAuth2Session(ctx context.Context, req *clientv1.CreateOAuth2SessionRequest) (*clientv1.CreateOAuth2SessionResponse, error) {
	redirectURL, err := s.account.CreateOAuth2Session(ctx, client.CreateOAuth2SessionCommand{
		ProjectID: req.GetProjectId(),
		Provider:  req.GetProvider(),
		Success:   req.GetSuccess(),
		Failure:   req.GetFailure(),
	})
	if err != nil {
		return nil, err
	}
	return &clientv1.CreateOAuth2SessionResponse{RedirectUrl: redirectURL}, nil
}

func (s *AccountService) CreateOAuth2TokenSession(ctx context.Context, req *clientv1.CreateOAuth2TokenSessionRequest) (*clientv1.SignInResponse, error) {
	user, tokens, _, err := s.account.CreateOAuth2TokenSession(ctx, client.CreateOAuth2TokenSessionCommand{
		ProjectID: req.GetProjectId(),
		Provider:  req.GetProvider(),
		Success:   req.GetSuccess(),
		Failure:   req.GetFailure(),
		Code:      req.GetCode(),
		State:     req.GetState(),
	})
	if err != nil {
		return nil, err
	}
	return &clientv1.SignInResponse{
		Account: mapUser(user),
		Tokens:  mapTokens(tokens),
	}, nil
}

func (s *AccountService) CreatePhoneOTP(ctx context.Context, req *clientv1.CreatePhoneOTPRequest) (*clientv1.ChallengeResponse, error) {
	challenge, err := s.account.CreatePhoneOTP(ctx, client.CreatePhoneOTPCommand{
		ProjectID: req.GetProjectId(),
		Phone:     req.GetPhone(),
	})
	if err != nil {
		return nil, err
	}
	return &clientv1.ChallengeResponse{
		ChallengeId: challenge.ChallengeID,
		ExpireAt:    challenge.ExpireAt.Unix(),
	}, nil
}

func (s *AccountService) CreatePhoneOTPSession(ctx context.Context, req *clientv1.CreatePhoneOTPSessionRequest) (*clientv1.SignInResponse, error) {
	user, tokens, _, err := s.account.CreatePhoneOTPSession(ctx, client.CreatePhoneOTPSessionCommand{
		ProjectID:   req.GetProjectId(),
		Phone:       req.GetPhone(),
		ChallengeID: req.GetChallengeId(),
		OTP:         req.GetOtp(),
	})
	if err != nil {
		return nil, err
	}
	return &clientv1.SignInResponse{
		Account: mapUser(user),
		Tokens:  mapTokens(tokens),
	}, nil
}

func (s *AccountService) CreateWeChatMiniProgramSession(ctx context.Context, req *clientv1.CreateWeChatMiniProgramSessionRequest) (*clientv1.SignInResponse, error) {
	user, tokens, _, err := s.account.CreateWeChatMiniProgramSession(ctx, client.CreateWeChatMiniProgramSessionCommand{
		ProjectID: req.GetProjectId(),
		Code:      req.GetCode(),
	})
	if err != nil {
		return nil, err
	}
	return &clientv1.SignInResponse{
		Account: mapUser(user),
		Tokens:  mapTokens(tokens),
	}, nil
}

func (s *AccountService) CreateAnonymousSession(ctx context.Context, req *clientv1.CreateAnonymousSessionRequest) (*clientv1.SignInResponse, error) {
	user, tokens, _, err := s.account.CreateAnonymousSession(ctx, client.CreateAnonymousSessionCommand{
		ProjectID: req.GetProjectId(),
	})
	if err != nil {
		return nil, err
	}
	return &clientv1.SignInResponse{
		Account: mapUser(user),
		Tokens:  mapTokens(tokens),
	}, nil
}

func (s *AccountService) CreateOAuth2LinkSession(ctx context.Context, req *clientv1.CreateOAuth2LinkSessionRequest) (*clientv1.CreateOAuth2SessionResponse, error) {
	redirectURL, err := s.account.CreateOAuth2LinkSession(ctx, client.CreateOAuth2LinkSessionCommand{
		ProjectID: req.GetProjectId(),
		Provider:  req.GetProvider(),
		Success:   req.GetSuccess(),
		Failure:   req.GetFailure(),
	})
	if err != nil {
		return nil, err
	}
	return &clientv1.CreateOAuth2SessionResponse{RedirectUrl: redirectURL}, nil
}

func (s *AccountService) CreateOAuth2LinkTokenSession(ctx context.Context, req *clientv1.CreateOAuth2LinkTokenSessionRequest) (*clientv1.Account, error) {
	user, err := s.account.CreateOAuth2LinkTokenSession(ctx, client.CreateOAuth2LinkTokenSessionCommand{
		ProjectID: req.GetProjectId(),
		Provider:  req.GetProvider(),
		Code:      req.GetCode(),
		State:     req.GetState(),
	})
	if err != nil {
		return nil, err
	}
	return mapUser(user), nil
}

func (s *AccountService) CreateVerification(ctx context.Context, req *clientv1.CreateVerificationRequest) (*clientv1.CreateVerificationResponse, error) {
	challenge, err := s.account.CreateVerification(ctx, client.CreateVerificationCommand{
		ProjectID: req.GetProjectId(),
		URL:       req.GetUrl(),
	})
	if err != nil {
		return nil, err
	}
	return &clientv1.CreateVerificationResponse{
		UserId:   challenge.UserID,
		ExpireAt: challenge.ExpireAt,
	}, nil
}

func (s *AccountService) UpdateVerification(ctx context.Context, req *clientv1.UpdateVerificationRequest) (*clientv1.Account, error) {
	user, err := s.account.UpdateVerification(ctx, client.UpdateVerificationCommand{
		ProjectID: req.GetProjectId(),
		UserID:    req.GetUserId(),
		Secret:    req.GetSecret(),
	})
	if err != nil {
		return nil, err
	}
	return mapUser(user), nil
}

func (s *AccountService) CreateRecovery(ctx context.Context, req *clientv1.CreateRecoveryRequest) (*sharedv1.Empty, error) {
	if err := s.account.CreateRecovery(ctx, client.CreateRecoveryCommand{
		ProjectID: req.GetProjectId(),
		Email:     req.GetEmail(),
		URL:       req.GetUrl(),
	}); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func (s *AccountService) UpdateRecovery(ctx context.Context, req *clientv1.UpdateRecoveryRequest) (*sharedv1.Empty, error) {
	if err := s.account.UpdateRecovery(ctx, client.UpdateRecoveryCommand{
		ProjectID: req.GetProjectId(),
		UserID:    req.GetUserId(),
		Secret:    req.GetSecret(),
		Password:  req.GetPassword(),
	}); err != nil {
		return nil, err
	}
	return &sharedv1.Empty{}, nil
}

func mapUser(u *client.User) *clientv1.Account {
	if u == nil {
		return nil
	}
	return &clientv1.Account{
		Id:            u.ID,
		Email:         u.Email,
		Name:          u.Name,
		Status:        u.Status,
		EmailVerified: u.EmailVerified,
		CreatedAt:     timestamppb.New(u.CreatedAt),
		UpdatedAt:     timestamppb.New(u.UpdatedAt),
	}
}

func mapSession(s *client.Session) *clientv1.Session {
	if s == nil {
		return nil
	}
	out := &clientv1.Session{
		Id:        s.ID,
		UserId:    s.UserID,
		Provider:  s.Provider,
		UserAgent: s.UserAgent,
		Ip:        s.IP,
		CreatedAt: timestamppb.New(s.CreatedAt),
		Current:   s.Current,
	}
	if !s.ExpireAt.IsZero() {
		out.ExpireAt = timestamppb.New(s.ExpireAt)
	}
	return out
}

func mapTokens(t *client.TokenBundle) *clientv1.TokenBundle {
	if t == nil {
		return nil
	}
	return &clientv1.TokenBundle{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		ExpiresAt:    t.ExpiresAt,
	}
}
