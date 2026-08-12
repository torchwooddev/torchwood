package client

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	clientv1 "github.com/torchwooddev/torchwood/genproto/client/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func bundle() *clientv1.TokenBundle {
	return &clientv1.TokenBundle{AccessToken: "a", RefreshToken: "r", ExpiresAt: timestamppb.New(time.Unix(1893456000, 0))}
}

func TestMemoryStoreRoundTrip(t *testing.T) {
	s := NewMemoryTokenStore()
	got, err := s.Load()
	require.NoError(t, err)
	require.Nil(t, got)
	require.NoError(t, s.Save(bundle()))
	got, err = s.Load()
	require.NoError(t, err)
	require.Equal(t, "a", got.AccessToken)
	require.NoError(t, s.Clear())
	got, _ = s.Load()
	require.Nil(t, got)
}

func TestFileStoreRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tokens.json")
	s := NewFileTokenStore(p)
	got, err := s.Load() // 文件不存在 -> (nil, nil)
	require.NoError(t, err)
	require.Nil(t, got)

	require.NoError(t, s.Save(bundle()))
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(p)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	}
	got, err = NewFileTokenStore(p).Load()
	require.NoError(t, err)
	require.Equal(t, "r", got.RefreshToken)
	require.NotNil(t, got.ExpiresAt)
	require.Equal(t, int64(1893456000), got.ExpiresAt.AsTime().Unix())

	require.NoError(t, s.Clear())
	require.NoError(t, s.Clear()) // 幂等
	got, _ = s.Load()
	require.Nil(t, got)
}

func TestFileStoreCorrupt(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tokens.json")
	require.NoError(t, os.WriteFile(p, []byte("{bad"), 0o600))
	_, err := NewFileTokenStore(p).Load()
	require.Error(t, err)
}

func TestFileStoreConcurrent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tokens.json")
	s := NewFileTokenStore(p)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Save(bundle())
			_, _ = s.Load()
		}()
	}
	wg.Wait()
	got, err := s.Load()
	require.NoError(t, err)
	require.Equal(t, "a", got.AccessToken)
}
