package serverhttp

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// countingReader 统计从源读取的字节数（用于断言"未读全量"）。
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// pngHeaderWithDimensions 构造带合法 CRC 的 PNG 签名 + IHDR 头（真彩色 8bit），
// 使 image.DecodeConfig 能成功解析出指定宽高。
func pngHeaderWithDimensions(width, height uint32) []byte {
	sig := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

	data := make([]byte, 13)
	binary.BigEndian.PutUint32(data[0:4], width)
	binary.BigEndian.PutUint32(data[4:8], height)
	data[8] = 8 // bit depth
	data[9] = 2 // color type: truecolor
	// data[10:13] = compression/filter/interlace = 0

	crc := crc32.NewIEEE()
	_, _ = crc.Write([]byte("IHDR"))
	_, _ = crc.Write(data)
	var crcSum [4]byte
	binary.BigEndian.PutUint32(crcSum[:], crc.Sum32())

	chunk := []byte{0, 0, 0, 13}
	chunk = append(chunk, "IHDR"...)
	chunk = append(chunk, data...)
	chunk = append(chunk, crcSum[:]...)

	return append(sig, chunk...)
}

// G4-2：预览源必须先做头部宽高校验再读全量——IHDR width=8193（超
// maxPreviewSourceDimension=8192）的合法 PNG 头应在有限 header 阶段即被拒绝：
// 返回 InvalidArgument（HTTP 400）且未读全量（读取字节数 ≤ header 上限）。
func TestPreviewSourceConfig_OversizedDimensionRejectedWithoutFullRead(t *testing.T) {
	t.Parallel()

	header := pngHeaderWithDimensions(maxPreviewSourceDimension+1, 100)
	stream := append(header, make([]byte, 1<<20)...) // 1MiB 填充模拟大文件剩余部分
	counted := &countingReader{r: bytes.NewReader(stream)}

	cfg, _, err := previewSourceConfig(counted)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, err.Error(), "dimensions too large")
	require.Equal(t, 0, cfg.Width, "失败时返回零值 config")

	require.Less(t, counted.n, int64(len(stream)), "尺寸超限拒绝时不得读全量文件")
	require.LessOrEqual(t, counted.n, int64(maxPreviewHeaderBytes), "头部阶段读取不得超过 header 上限")

	// 该错误经 httpError 映射为 HTTP 400。
	rec := httptest.NewRecorder()
	httpError(rec, err)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// G4-4：非图片/损坏图片 → InvalidArgument（HTTP 400），而非 500。
func TestPreviewSourceConfig_CorruptOrNonImageRejected(t *testing.T) {
	t.Parallel()

	t.Run("garbage bytes", func(t *testing.T) {
		_, _, err := previewSourceConfig(bytes.NewReader([]byte("definitely not an image")))
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.Contains(t, err.Error(), "cannot decode image")
	})

	t.Run("png header with corrupted ihdr crc", func(t *testing.T) {
		header := pngHeaderWithDimensions(64, 64)
		header[len(header)-1] ^= 0xFF // 破坏 IHDR CRC
		_, _, err := previewSourceConfig(bytes.NewReader(header))
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.Contains(t, err.Error(), "cannot decode image")
	})

	// 解码失败错误经 httpError 映射为 HTTP 400。
	rec := httptest.NewRecorder()
	httpError(rec, status.Error(codes.InvalidArgument, "cannot decode image"))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// G4-2 对照：合法小图头部解析成功，宽高正确。
func TestPreviewSourceConfig_ValidHeader(t *testing.T) {
	t.Parallel()

	cfg, _, err := previewSourceConfig(bytes.NewReader(pngHeaderWithDimensions(100, 50)))
	require.NoError(t, err)
	require.Equal(t, 100, cfg.Width)
	require.Equal(t, 50, cfg.Height)
}

// 回归（CI TestFileHandler_Preview 400）：header 阶段消费的字节必须可拼回——
// 小图片会整个被 header 阶段读完，调用方用 MultiReader(header, 剩余流)
// 必须还原出完整原始字节，否则整图解码拿到空流误报 400。
func TestPreviewSourceConfig_HeaderBytesRejoinable(t *testing.T) {
	t.Parallel()

	// 完整合法小 PNG（非仅 header），模拟集成测试中的 40x20 上传文件。
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	require.NoError(t, png.Encode(&buf, img))
	raw := buf.Bytes()
	require.Less(t, len(raw), maxPreviewHeaderBytes, "小文件应整个落入 header 上限内")

	counted := &countingReader{r: bytes.NewReader(raw)}
	cfg, header, err := previewSourceConfig(counted)
	require.NoError(t, err)
	require.Equal(t, 40, cfg.Width)
	require.Equal(t, 20, cfg.Height)
	require.Equal(t, raw, header, "小文件的 header 阶段即读完整个文件")

	// 拼回后必须还原完整流，且二次解码成功。
	rejoined, err := io.ReadAll(io.MultiReader(bytes.NewReader(header), counted))
	require.NoError(t, err)
	require.Equal(t, raw, rejoined)
	_, _, err = image.Decode(bytes.NewReader(rejoined))
	require.NoError(t, err)
}
