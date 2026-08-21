package assets

import (
	"context"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 编译期：五动词在 Service 上，不在 Holding 上。
var _ interface {
	Grant(context.Context, Scope, GrantCommand) (*OpResult, error)
	Consume(context.Context, Scope, ConsumeCommand) (*OpResult, error)
	Transfer(context.Context, Scope, TransferCommand) (*OpResult, error)
	Mutate(context.Context, Scope, MutateCommand) (*OpResult, error)
	Expire(context.Context, Scope, ExpireCommand) (*OpResult, error)
	ExpireDue(context.Context, Scope, time.Time, int) (int64, error)
} = (*Service)(nil)

func TestHoldingHasNoAssetVerbs(t *testing.T) {
	t.Parallel()
	ptr := reflect.TypeOf((*Holding)(nil))
	val := reflect.TypeOf(Holding{})
	for _, name := range []string{"Grant", "Consume", "Transfer", "Mutate", "Expire", "ExpireDue"} {
		if _, ok := ptr.MethodByName(name); ok {
			t.Errorf("Holding 不得有五动词方法 %s", name)
		}
		if _, ok := val.MethodByName(name); ok {
			t.Errorf("Holding 不得有五动词方法 %s", name)
		}
	}
}

func TestServiceDoesNotExportLiveHolding(t *testing.T) {
	t.Parallel()
	if _, ok := reflect.TypeOf((*Service)(nil)).MethodByName("LiveHolding"); ok {
		t.Error("Service 不得导出 LiveHolding；锁读由 app.LiveHoldingForUpdate 封装")
	}
}

func TestDomainDoesNotImportGRPC(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, im := range f.Imports {
			p := strings.Trim(im.Path.Value, `"`)
			if p == "google.golang.org/grpc" || strings.HasPrefix(p, "google.golang.org/grpc/") {
				t.Errorf("%s imports %s", path, p)
			}
		}
		return nil
	})
	require.NoError(t, err)
}

func TestValidateCode(t *testing.T) {
	t.Parallel()
	_, err := ValidateCode("Gold")
	require.NoError(t, err)
	_, err = ValidateCode("1bad")
	require.ErrorIs(t, err, ErrInvalidCode)
}
