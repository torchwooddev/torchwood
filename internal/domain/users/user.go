package users

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/torchwooddev/torchwood/pkg/password"
)

const (
	CollectionID   = "users"
	LabelAnonymous = "anonymous"
)

var (
	ErrEmailAlreadyRegistered = errors.New("email already registered")
	ErrEmailRequired          = errors.New("email is required")
	ErrUserIDRequired         = errors.New("user id is required")
)

// User 是项目内终端用户聚合：邮箱唯一、状态、密码哈希与匿名标签。
type User struct {
	ID            string
	Email         string
	PasswordHash  string
	Name          string
	Status        string
	EmailVerified bool
	Phone         string
	PhoneVerified bool
	Labels        []string
	Prefs         map[string]any
	PendingEmail  string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (u *User) CanAuthenticate() bool {
	if u == nil {
		return false
	}
	return CanAuthenticate(u.Status)
}

func (u *User) IsAnonymous() bool {
	if u == nil {
		return false
	}
	for _, l := range u.Labels {
		if l == LabelAnonymous {
			return true
		}
	}
	return false
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// RequireUniqueEmail 在 GetByEmail 之后断言邮箱未被占用。
func RequireUniqueEmail(existing *User) error {
	if existing != nil {
		return ErrEmailAlreadyRegistered
	}
	return nil
}

func AnonymousEmail(userID string) string {
	shortID := userID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return fmt.Sprintf("anon_%s@torchwood.local", shortID)
}

// RegisterInput 是 User.Register 的入参；Password 为空表示无密码（OTP/OAuth/匿名）。
type RegisterInput struct {
	ID            string
	Email         string
	Password      string
	Name          string
	Status        string
	EmailVerified bool
	Phone         string
	PhoneVerified bool
	Anonymous     bool
	Labels        []string
	Prefs         map[string]any
}

// Register 构造新用户：规范化邮箱、校验状态/密码强度并哈希密码。
// 唯一性由调用方 GetByEmail + RequireUniqueEmail（及仓储 Insert 冲突）保证。
func Register(in RegisterInput) (*User, error) {
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return nil, ErrUserIDRequired
	}
	email := NormalizeEmail(in.Email)
	if email == "" {
		return nil, ErrEmailRequired
	}
	status := in.Status
	if status == "" {
		status = StatusActive
	}
	if err := ValidateStatus(status); err != nil {
		return nil, err
	}
	var hash string
	if in.Password != "" {
		if err := ValidatePasswordStrength(in.Password); err != nil {
			return nil, err
		}
		var err error
		hash, err = password.Hash(in.Password)
		if err != nil {
			return nil, err
		}
	}
	labels := append([]string(nil), in.Labels...)
	name := in.Name
	if in.Anonymous {
		if !containsLabel(labels, LabelAnonymous) {
			labels = append(labels, LabelAnonymous)
		}
		if name == "" {
			name = "Anonymous"
		}
	}
	prefs := in.Prefs
	if prefs == nil {
		prefs = map[string]any{}
	}
	return &User{
		ID:            id,
		Email:         email,
		PasswordHash:  hash,
		Name:          name,
		Status:        status,
		EmailVerified: in.EmailVerified,
		Phone:         strings.TrimSpace(in.Phone),
		PhoneVerified: in.PhoneVerified,
		Labels:        labels,
		Prefs:         prefs,
	}, nil
}

func containsLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

// DocumentData 是写入系统集合 users 的字段快照（含密码哈希，不进 API 投影）。
func (u *User) DocumentData() map[string]any {
	if u == nil {
		return map[string]any{}
	}
	labels := make([]any, len(u.Labels))
	for i, l := range u.Labels {
		labels[i] = l
	}
	prefs := u.Prefs
	if prefs == nil {
		prefs = map[string]any{}
	}
	data := map[string]any{
		"email":          u.Email,
		"password_hash":  u.PasswordHash,
		"name":           u.Name,
		"status":         u.Status,
		"email_verified": u.EmailVerified,
		"labels":         labels,
		"prefs":          prefs,
	}
	if u.Phone != "" {
		data["phone"] = u.Phone
		data["phone_verified"] = u.PhoneVerified
	}
	if u.PendingEmail != "" {
		data["pending_email"] = u.PendingEmail
	}
	return data
}

func LabelsFromAny(raw []any) []string {
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
