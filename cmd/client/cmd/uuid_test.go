package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestUUIDCmdNoAPIKey(t *testing.T) {
	g := &globalFlags{output: "json", timeout: "30s"}
	cmd := NewUUIDCmd()
	if err := g.validate(cmd, nil); err != nil {
		t.Fatalf("uuid 命令应豁免 api-key 校验：%v", err)
	}
}

func TestUUIDCmdPrintsUniqueUUIDv4(t *testing.T) {
	cmd := NewUUIDCmd()
	a := captureStdout(t, func() {
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	b := captureStdout(t, func() {
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	idA := strings.TrimSpace(a)
	idB := strings.TrimSpace(b)
	if _, err := uuid.Parse(idA); err != nil {
		t.Fatalf("输出不是合法 UUID：%q (%v)", idA, err)
	}
	if idA == idB {
		t.Fatalf("连续两次生成应不同：%q", idA)
	}
	if !strings.HasSuffix(a, "\n") {
		t.Fatalf("应输出换行，got %q", a)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	return buf.String()
}
