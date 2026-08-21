package groups

import "testing"

func TestNormalizeGroupUpdateColumns(t *testing.T) {
	t.Parallel()

	got, err := NormalizeGroupUpdateColumns(map[string]any{
		"name":  "  Team A  ",
		"prefs": map[string]any{"k": "v"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["name"] != "Team A" {
		t.Fatalf("name = %v", got["name"])
	}

	_, err = NormalizeGroupUpdateColumns(map[string]any{"total": int64(1)})
	if err == nil {
		t.Fatal("expected error for total")
	}

	_, err = NormalizeGroupUpdateColumns(nil)
	if err == nil {
		t.Fatal("expected error for empty map")
	}

	_, err = NormalizeGroupUpdateColumns(map[string]any{"name": ""})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}
