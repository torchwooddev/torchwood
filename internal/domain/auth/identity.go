package auth

import "time"

// OAuth and third-party identity provider identifiers.
const (
	ProviderGoogle            = "google"
	ProviderGitHub            = "github"
	ProviderApple             = "apple"
	ProviderWeChatWeb         = "wechat_web"
	ProviderWeChatMP          = "wechat_mp"
	ProviderWeChatMiniProgram = "wechat_miniprogram"
	ProviderWeChatApp         = "wechat_app"
)

// Identity links a third-party account to an Torchwood user.
type Identity struct {
	ID            string
	UserID        string
	Provider      string
	ProviderUID   string
	ProviderEmail string
	ProviderData  map[string]any
	ExpireAt      *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
