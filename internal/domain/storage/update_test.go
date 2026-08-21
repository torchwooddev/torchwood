package storage

import "testing"

func TestNormalizeBucketUpdateColumns(t *testing.T) {
	t.Parallel()

	got, err := NormalizeBucketUpdateColumns(map[string]any{
		"name":   "  docs  ",
		"public": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["name"] != "docs" {
		t.Fatalf("name = %v", got["name"])
	}

	_, err = NormalizeBucketUpdateColumns(map[string]any{"id": "x"})
	if err == nil {
		t.Fatal("expected error for unknown column")
	}
	_, err = NormalizeBucketUpdateColumns(nil)
	if err == nil {
		t.Fatal("expected error for empty map")
	}
}

func TestNormalizeFileUpdateColumns(t *testing.T) {
	t.Parallel()

	got, err := NormalizeFileUpdateColumns(map[string]any{
		"name":      " a.txt ",
		"mime_type": "text/plain",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["name"] != "a.txt" {
		t.Fatalf("name = %v", got["name"])
	}

	_, err = NormalizeFileUpdateColumns(map[string]any{"size": int64(1)})
	if err == nil {
		t.Fatal("expected error for size")
	}
	_, err = NormalizeFileUpdateColumns(map[string]any{"owner_user_id": "u1"})
	if err == nil {
		t.Fatal("expected error for owner_user_id")
	}
}
