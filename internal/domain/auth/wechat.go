package auth

import (
	"context"
	"strings"
)

// IsWeChatProvider reports whether the provider id belongs to the WeChat family.
func IsWeChatProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case ProviderWeChatWeb, ProviderWeChatMP, ProviderWeChatMiniProgram, ProviderWeChatApp:
		return true
	default:
		return false
	}
}

// WeChatProviders returns all WeChat provider identifiers used for identity linking.
func WeChatProviders() []string {
	return []string{
		ProviderWeChatWeb,
		ProviderWeChatMP,
		ProviderWeChatMiniProgram,
		ProviderWeChatApp,
	}
}

// WeChatIdentityUID prefers unionid for cross-app identity linking.
func WeChatIdentityUID(unionid, openid string) string {
	if strings.TrimSpace(unionid) != "" {
		return unionid
	}
	return openid
}

// WeChatMiniProgramExchanger 交换微信小程序登录凭证。
type WeChatMiniProgramExchanger interface {
	ExchangeWeChatMiniProgramCode(ctx context.Context, appID, appSecret, code string) (*OAuthUserInfo, error)
}
