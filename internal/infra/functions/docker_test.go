package functions

import (
	"archive/zip"
	"bufio"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestReadBuildOutput_SuccessStream(t *testing.T) {
	stream := "{\"stream\":\"Step 1/2 : FROM node:18-alpine\\n\"}\n" +
		"{\"stream\":\"Successfully built abc123\\n\"}\n"
	log, err := readBuildOutput(strings.NewReader(stream))
	require.NoError(t, err)
	require.Contains(t, log, "Step 1/2")
	require.Contains(t, log, "Successfully built")
}

func TestReadBuildOutput_ErrorJSON(t *testing.T) {
	stream := "{\"stream\":\"Step 2/2 : RUN nope\\n\"}\n" +
		"{\"errorDetail\":{\"message\":\"The command '/bin/sh -c nope' returned a non-zero code: 127\"},\"error\":\"The command '/bin/sh -c nope' returned a non-zero code: 127\"}\n"
	log, err := readBuildOutput(strings.NewReader(stream))
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-zero code")
	require.Contains(t, log, "The command '/bin/sh -c nope'")
}

func TestReadBuildOutput_ErrorDetailOnly(t *testing.T) {
	stream := "{\"errorDetail\":{\"message\":\"failed to solve: no matching manifest\"}}\n"
	_, err := readBuildOutput(strings.NewReader(stream))
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to solve")
}

func TestReadBuildOutput_LongStreamKeepsErrorTail(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < (maxBuildLogBytes/16)+100; i++ {
		sb.WriteString("{\"stream\":\"some build progress line content\\n\"}\n")
	}
	sb.WriteString("{\"error\":\"the final failure\"}\n")
	log, err := readBuildOutput(strings.NewReader(sb.String()))
	require.Error(t, err)
	require.Contains(t, err.Error(), "the final failure")
	require.Len(t, log, maxBuildLogBytes, "日志保留尾部 64KB")
	require.True(t, strings.HasSuffix(log, "{\"error\":\"the final failure\"}\n"), "错误行位于日志末尾")
}

func TestReadBuildOutput_PlainTextNoError(t *testing.T) {
	log, err := readBuildOutput(strings.NewReader("plain text line, not json\n"))
	require.NoError(t, err)
	require.Contains(t, log, "plain text line")
}

func TestBuildError_FitsBudget(t *testing.T) {
	log := strings.Repeat("L", maxBuildLogBytes)
	err := buildError(errors.New("boom: build failed"), log)
	msg := err.Error()
	require.Contains(t, msg, "boom: build failed")
	require.Contains(t, msg, "build log tail:")
	require.True(t, len(msg) <= maxBuildLogBytes, "错误总长不得超过构建日志预算")
}

func TestTailBuffer_KeepsTail(t *testing.T) {
	var b tailBuffer
	for i := 0; i < maxBuildLogBytes/1024+1; i++ {
		_, _ = b.Write([]byte(strings.Repeat(string(rune('a'+i%26)), 1024)))
	}
	_, _ = b.Write([]byte(strings.Repeat("z", 1024)))
	got := b.String()
	require.Len(t, got, maxBuildLogBytes)
	require.True(t, strings.HasSuffix(got, strings.Repeat("z", 1024)))
	require.False(t, strings.HasPrefix(got, strings.Repeat("a", 1024)), "头部已被丢弃")
}

// partialWriter 每次只落盘前 3 字节（模拟底层 writer 部分写入）。
type partialWriter struct{}

func (partialWriter) Write(p []byte) (int, error) {
	if len(p) > 3 {
		return 3, nil
	}
	return len(p), nil
}

// TestBudgetWriter_EnforcesActualByteBudget 按实际写入字节计数（G6-1/R07-P1-1）：
// 预算精确到字节、超限整段拒绝、计数跟随底层实际写入（不信任声明大小）。
func TestBudgetWriter_EnforcesActualByteBudget(t *testing.T) {
	var buf bytes.Buffer
	w := &budgetWriter{dst: &buf, limit: 100}
	n, err := w.Write([]byte("0123456789"))
	require.NoError(t, err)
	require.Equal(t, 10, n)
	n, err = w.Write(make([]byte, 90))
	require.NoError(t, err)
	require.Equal(t, 90, n)
	require.Equal(t, int64(100), w.written, "恰好打满预算")

	_, err = w.Write([]byte("x"))
	require.ErrorIs(t, err, errZipBudgetExceeded)
	require.Equal(t, 100, buf.Len(), "超限字节不得写入目标 writer")

	// 底层部分写入时，计数按实际写入字节而非请求字节。
	bw := &budgetWriter{dst: partialWriter{}, limit: 10}
	_, err = bw.Write(make([]byte, 5))
	require.NoError(t, err)
	require.Equal(t, int64(3), bw.written, "计数跟随底层实际写入")
}

// le16/le32 小端序列化（手工构造 zip 用）；掩码截断为有意为之。
func le16(v uint16) []byte {
	return []byte{byte(v & 0xFF), byte((v >> 8) & 0xFF)}
}

func le32(v uint32) []byte {
	return []byte{byte(v & 0xFF), byte((v >> 8) & 0xFF), byte((v >> 16) & 0xFF), byte((v >> 24) & 0xFF)}
}

// craftLyingZip 手工构造"声明 200 字节、实际数据区仅 10 字节"的伪造 zip
// （stored 条目 index.js；声明大小与实际内容不符，模拟 zip bomb 的声明侧欺骗）。
func craftLyingZip() []byte {
	const name = "index.js"
	var buf bytes.Buffer
	// local file header（30 + nameLen）。
	buf.Write(le32(0x04034b50))
	buf.Write(le16(20))  // version needed
	buf.Write(le16(0))   // flags
	buf.Write(le16(0))   // method: stored
	buf.Write(le16(0))   // mod time
	buf.Write(le16(0))   // mod date
	buf.Write(le32(0))   // crc32（读取端在大小不符时先报错，不校验 CRC）
	buf.Write(le32(200)) // compressed size（声明）
	buf.Write(le32(200)) // uncompressed size（声明）
	buf.Write(le16(uint16(len(name))))
	buf.Write(le16(0)) // extra len
	buf.Write([]byte(name))
	// 实际数据区只有 10 字节（声明 200，严重不符）。
	buf.Write([]byte("0123456789"))
	// central directory（46 + nameLen）。
	cdOffset := buf.Len()
	buf.Write(le32(0x02014b50))
	buf.Write(le16(20)) // version made by
	buf.Write(le16(20)) // version needed
	buf.Write(le16(0))  // flags
	buf.Write(le16(0))  // method
	buf.Write(le16(0))  // mod time
	buf.Write(le16(0))  // mod date
	buf.Write(le32(0))  // crc32
	buf.Write(le32(200))
	buf.Write(le32(200))
	buf.Write(le16(uint16(len(name))))
	buf.Write(le16(0)) // extra len
	buf.Write(le16(0)) // comment len
	buf.Write(le16(0)) // disk number start
	buf.Write(le16(0)) // internal attrs
	buf.Write(le32(0)) // external attrs
	buf.Write(le32(0)) // local header offset
	buf.Write([]byte(name))
	// end of central directory（22）。
	cdSize := buf.Len() - cdOffset
	buf.Write(le32(0x06054b50))
	buf.Write(le16(0))                        // disk number
	buf.Write(le16(0))                        // cd start disk
	buf.Write(le16(1))                        // entries on disk
	buf.Write(le16(1))                        // total entries
	buf.Write(le32(uint32(uint64(cdSize))))   // #nosec G115 -- 测试构造值远小于 2^32
	buf.Write(le32(uint32(uint64(cdOffset)))) // #nosec G115 -- 测试构造值远小于 2^32
	buf.Write(le16(0))                        // comment len
	return buf.Bytes()
}

// TestExtractZip_LyingDeclaredSize_ErrorsAndCleansPartial 声明 200 字节但实际
// 数据区只有 10 字节的伪造 zip：解压中途报错（实际写入与声明不符），且
// 整个解压目标目录必须被清理（G6-1/R07-P1-1 + G11-3 不留半成品要求）。
func TestExtractZip_LyingDeclaredSize_ErrorsAndCleansPartial(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "lying.zip")
	require.NoError(t, os.WriteFile(zipPath, craftLyingZip(), 0o600))
	destDir := filepath.Join(t.TempDir(), "out")

	_, err := extractZipWithLimits(zipPath, destDir, zipExtractLimits{
		maxEntries:    1000,
		maxEntryBytes: 4096,
		maxTotalBytes: 1 << 20,
	})
	require.Error(t, err, "声明与实际内容不符必须报错")

	requireExtractDirCleared(t, destDir)
}

// requireExtractDirCleared 断言解压目标目录被完整清理（目录本身已不存在）。
func requireExtractDirCleared(t *testing.T, destDir string) {
	t.Helper()
	_, err := os.Stat(destDir)
	require.Error(t, err, "解压目标目录必须被整体清理")
	require.True(t, os.IsNotExist(err), "目录不应残留（期望已删除）")
}

// TestExtractZip_TotalBudgetExceeded_CleansWholeDir（G11-3）：前序条目已成功
// 解压后，后续条目声明大小使总预算超限 → 报错且整个目标目录被清理，
// 已解压的前序条目不得残留。
func TestExtractZip_TotalBudgetExceeded_CleansWholeDir(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f1, err := zw.Create("pre.js")
	require.NoError(t, err)
	_, err = f1.Write([]byte("exports.hook = () => {};"))
	require.NoError(t, err)
	f2, err := zw.Create("index.js")
	require.NoError(t, err)
	_, err = f2.Write(bytes.Repeat([]byte("b"), 100))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	zipPath := filepath.Join(t.TempDir(), "big.zip")
	require.NoError(t, os.WriteFile(zipPath, buf.Bytes(), 0o600))
	destDir := filepath.Join(t.TempDir(), "out")

	_, err = extractZipWithLimits(zipPath, destDir, zipExtractLimits{
		maxEntries:    1000,
		maxEntryBytes: 4096,
		maxTotalBytes: 50,
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.ErrorContains(t, err, "total uncompressed size exceeds")

	requireExtractDirCleared(t, destDir)
}

// TestExtractZip_EntryBudgetExceeded_CleansWholeDir（G11-3）：前序条目已成功
// 解压后，后续条目声明大小超过单条目预算 → 报错且整个目标目录被清理。
func TestExtractZip_EntryBudgetExceeded_CleansWholeDir(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f1, err := zw.Create("pre.js")
	require.NoError(t, err)
	_, err = f1.Write([]byte("exports.hook = () => {};"))
	require.NoError(t, err)
	f2, err := zw.Create("index.js")
	require.NoError(t, err)
	_, err = f2.Write(bytes.Repeat([]byte("c"), 100))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	zipPath := filepath.Join(t.TempDir(), "big-entry.zip")
	require.NoError(t, os.WriteFile(zipPath, buf.Bytes(), 0o600))
	destDir := filepath.Join(t.TempDir(), "out")

	_, err = extractZipWithLimits(zipPath, destDir, zipExtractLimits{
		maxEntries:    1000,
		maxEntryBytes: 50,
		maxTotalBytes: 1 << 20,
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.ErrorContains(t, err, `exceeds 50 bytes`)

	requireExtractDirCleared(t, destDir)
}

// TestExtractZipWithLimits_DeclaredOverBudgetRejected 声明大小超过注入预算时
// 快速拒绝（声明侧预检 + 预算参数注入生效）。
func TestExtractZipWithLimits_DeclaredOverBudgetRejected(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("index.js")
	require.NoError(t, err)
	_, err = f.Write(bytes.Repeat([]byte("a"), 200))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	zipPath := filepath.Join(t.TempDir(), "big.zip")
	require.NoError(t, os.WriteFile(zipPath, buf.Bytes(), 0o600))

	_, err = extractZipWithLimits(zipPath, filepath.Join(t.TempDir(), "out"), zipExtractLimits{
		maxEntries:    1000,
		maxEntryBytes: 100,
		maxTotalBytes: 1 << 20,
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.ErrorContains(t, err, "exceeds 100 bytes")
}

// TestExtractZipWithLimits_ValidZipWithinBudget 正常 zip 在注入预算内解压成功。
func TestExtractZipWithLimits_ValidZipWithinBudget(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("index.js")
	require.NoError(t, err)
	_, err = f.Write([]byte("exports.main = () => ({ ok: true });"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	zipPath := filepath.Join(t.TempDir(), "ok.zip")
	require.NoError(t, os.WriteFile(zipPath, buf.Bytes(), 0o600))

	runtime, err := extractZipWithLimits(zipPath, filepath.Join(t.TempDir(), "out"), zipExtractLimits{
		maxEntries:    1000,
		maxEntryBytes: 4096,
		maxTotalBytes: 1 << 20,
	})
	require.NoError(t, err)
	require.Equal(t, "node-18.0", runtime)
}

// TestReadBuildOutput_LongLineWithinLimit 单行超过旧 512KB 上限（< 4MB）不再
// 丢日志（G6-7/R07-P2-6）。
func TestReadBuildOutput_LongLineWithinLimit(t *testing.T) {
	line := `{"stream":"` + strings.Repeat("a", 600*1024) + `"}` + "\n"
	log, err := readBuildOutput(strings.NewReader(line))
	require.NoError(t, err)
	require.True(t, strings.Contains(log, strings.Repeat("a", 1024)), "长行日志保留")
}

// TestReadBuildOutput_LineOverMaxFails 超过 4MB 的单行明确报错（防内存耗尽）。
func TestReadBuildOutput_LineOverMaxFails(t *testing.T) {
	line := strings.Repeat("a", maxBuildLogLine+1) + "\n"
	_, err := readBuildOutput(strings.NewReader(line))
	require.Error(t, err)
	require.ErrorIs(t, err, bufio.ErrTooLong)
}

// TestExtractZip_RejectsSymlink 符号链接条目拒绝（补 G6-1 同路径覆盖）。
func TestExtractZip_RejectsSymlink(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	hdr := &zip.FileHeader{Name: "link", Method: zip.Store}
	hdr.SetMode(os.ModeSymlink | 0o777)
	f, err := zw.CreateHeader(hdr)
	require.NoError(t, err)
	_, err = f.Write([]byte("index.js"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	zipPath := filepath.Join(t.TempDir(), "symlink.zip")
	require.NoError(t, os.WriteFile(zipPath, buf.Bytes(), 0o600))

	_, err = extractZipWithLimits(zipPath, filepath.Join(t.TempDir(), "out"), zipExtractLimits{
		maxEntries:    1000,
		maxEntryBytes: 4096,
		maxTotalBytes: 1 << 20,
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.ErrorContains(t, err, "symlink")
}
