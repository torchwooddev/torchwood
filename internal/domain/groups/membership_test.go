package groups

import "testing"

func TestValidateStatus(t *testing.T) {
	for _, s := range []string{StatusPending, StatusAccepted, StatusRejected} {
		if err := ValidateStatus(s); err != nil {
			t.Fatalf("ValidateStatus(%q) = %v", s, err)
		}
	}
	if err := ValidateStatus("active"); err == nil {
		t.Fatal("expected error for invalid status")
	}
	if err := ValidateStatus(""); err == nil {
		t.Fatal("expected error for empty status")
	}
}

func TestValidateRole(t *testing.T) {
	for _, role := range []string{RoleOwner, RoleAdmin, RoleMember} {
		if err := ValidateRole(role); err != nil {
			t.Fatalf("ValidateRole(%q) = %v", role, err)
		}
	}
	if err := ValidateRole("viewer"); err == nil {
		t.Fatal("expected error for invalid role")
	}
}

func TestPrimaryRole(t *testing.T) {
	if got := PrimaryRole(nil); got != RoleMember {
		t.Fatalf("PrimaryRole(nil) = %q", got)
	}
	if got := PrimaryRole([]string{RoleOwner, RoleAdmin}); got != RoleOwner {
		t.Fatalf("PrimaryRole = %q", got)
	}
}
