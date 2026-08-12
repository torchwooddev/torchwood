package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoProtoGRPCImports 兜底：CLI 源码不得直接 import genproto/grpc/protobuf。
func TestNoProtoGRPCImports(t *testing.T) {
	forbidden := []string{
		"github.com/torchwooddev/torchwood/genproto",
		"google.golang.org/grpc",
		"google.golang.org/protobuf",
	}
	for _, dir := range []string{".", "cmd"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "import_guard_test.go" {
				continue
			}
			path := filepath.Join(dir, name)
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				impPath := strings.Trim(imp.Path.Value, `"`)
				for _, bad := range forbidden {
					if strings.HasPrefix(impPath, bad) {
						t.Errorf("%s imports forbidden package %s", path, impPath)
					}
				}
			}
		}
	}
}
