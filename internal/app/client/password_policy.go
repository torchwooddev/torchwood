package client

import "github.com/torchwooddev/torchwood/internal/domain/users"

// validatePasswordStrength 委托领域层策略，保持单一事实来源。
func validatePasswordStrength(pw string) error {
	return users.ValidatePasswordStrength(pw)
}
