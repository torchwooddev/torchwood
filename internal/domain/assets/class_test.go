package assets

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateDefMatrix_CurrencyRejectsExpiresIn(t *testing.T) {
	t.Parallel()
	ttl := int64(3600)
	err := ValidateDefMatrix(&Def{Class: ClassCurrency, ExpiresIn: &ttl})
	require.ErrorIs(t, err, ErrMatrix)
	require.Contains(t, err.Error(), "expires_in")
}

func TestValidateDefMatrix_CurrencyRejectsUpgradeable(t *testing.T) {
	t.Parallel()
	err := ValidateDefMatrix(&Def{Class: ClassCurrency, Upgradeable: true})
	require.ErrorIs(t, err, ErrMatrix)
}

func TestValidateDefMatrix_EntitlementRejectsTradable(t *testing.T) {
	t.Parallel()
	err := ValidateDefMatrix(&Def{Class: ClassEntitlement, Tradable: true})
	require.ErrorIs(t, err, ErrMatrix)
}

func TestValidateDefMatrix_StackRejectsUpgradeable(t *testing.T) {
	t.Parallel()
	err := ValidateDefMatrix(&Def{Class: ClassStack, Upgradeable: true})
	require.ErrorIs(t, err, ErrMatrix)
}

func TestValidateDefMatrix_Happy(t *testing.T) {
	t.Parallel()
	ttl := int64(86400)
	max := int64(1000)
	cases := []*Def{
		{Class: ClassCurrency, Decimals: 0},
		{Class: ClassCurrency, Decimals: 2, MaxQuantity: &max, Tradable: true},
		{Class: ClassStack, ExpiresIn: &ttl, Tradable: true},
		{Class: ClassInstance, UniquePerOwner: true, Upgradeable: true, ExpiresIn: &ttl},
		{Class: ClassEntitlement, UniquePerOwner: true, ExpiresIn: &ttl, Upgradeable: true},
	}
	for _, d := range cases {
		require.NoError(t, ValidateDefMatrix(d), "class=%s", d.Class)
	}
}

func TestValidateGrant_EntitlementRequiresExpiresAt(t *testing.T) {
	t.Parallel()
	err := ValidateGrant(ClassEntitlement, 1, false)
	require.ErrorIs(t, err, ErrExpiresAtRequired)
	require.NoError(t, ValidateGrant(ClassEntitlement, 1, true))
}

func TestValidateGrant_InstanceQuantityMustBeOne(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, ValidateGrant(ClassInstance, 2, false), ErrMatrix)
	require.NoError(t, ValidateGrant(ClassInstance, 1, false))
}

func TestValidateGrant_CurrencyRejectsExpiresAt(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, ValidateGrant(ClassCurrency, 10, true), ErrMatrix)
	require.NoError(t, ValidateGrant(ClassCurrency, 10, false))
}

func TestValidateGrant_RejectsNonPositive(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, ValidateGrant(ClassStack, 0, false), ErrInvalidQuantity)
	require.ErrorIs(t, ValidateGrant(ClassStack, -1, false), ErrInvalidQuantity)
}

func TestValidateMutateClass(t *testing.T) {
	t.Parallel()
	require.NoError(t, ValidateMutateClass(ClassInstance))
	require.NoError(t, ValidateMutateClass(ClassEntitlement))
	require.ErrorIs(t, ValidateMutateClass(ClassCurrency), ErrMatrix)
	require.ErrorIs(t, ValidateMutateClass(ClassStack), ErrMatrix)
}

func TestNormalizeOwnerType_Phase1UserOnly(t *testing.T) {
	t.Parallel()
	got, err := NormalizeOwnerType("")
	require.NoError(t, err)
	require.Equal(t, OwnerTypeUser, got)
	_, err = NormalizeOwnerType(OwnerTypeTeam)
	require.ErrorIs(t, err, ErrInvalidOwnerType)
	_, err = NormalizeOwnerType("guild")
	require.ErrorIs(t, err, ErrInvalidOwnerType)
}
