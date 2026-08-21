package bunrepo

import (
	"errors"
	"strings"

	"github.com/uptrace/bun/driver/pgdriver"
)

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr pgdriver.Error
	if errors.As(err, &pgErr) && pgErr.Field('C') == "23505" {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "sqlstate 23505") || strings.Contains(s, "unique constraint") || strings.Contains(s, "duplicate key")
}
