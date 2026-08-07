package serverhttp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/torchwooddev/torchwood/internal/app/storage"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/auth"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/pkg/grpc/interceptor"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// inlineSafeMimeTypes 是可安全同源内联展示的 MIME 白名单，不含任何可执行脚本类型。
// image/svg+xml 可内嵌脚本，虽列入名单，但实际判断时强制降级为附件下载（见 inlineSafeMime）。
var inlineSafeMimeTypes = map[string]struct{}{
	"image/png":       {},
	"image/jpeg":      {},
	"image/gif":       {},
	"image/webp":      {},
	"image/avif":      {},
	"image/svg+xml":   {},
	"text/plain":      {},
	"application/pdf": {},
}

// FileHandler provides HTTP multipart upload/download for storage.
type FileHandler struct {
	cfg       *config.AppConfig
	validator *auth.Validator
	storage   *storage.Storage
	trusted   *interceptor.TrustedProxies
	logger    *slog.Logger
}

// NewFileHandler creates a new file HTTP handler.
func NewFileHandler(
	cfg *config.AppConfig,
	validator *auth.Validator,
	storage *storage.Storage,
) (*FileHandler, error) {
	trusted, err := interceptor.ParseTrustedProxies(cfg.GetSecurity().GetTrustedProxies())
	if err != nil {
		return nil, fmt.Errorf("parse security.trusted_proxies: %w", err)
	}
	return &FileHandler{cfg: cfg, validator: validator, storage: storage, trusted: trusted, logger: slog.Default()}, nil
}

// clientIP 与 gRPC ClientInfoInterceptor 走同一 trusted-proxy 规则。
func (h *FileHandler) clientIP(r *http.Request) string {
	return h.trusted.ResolveClientIP(
		interceptor.PeerIPFromAddr(r.RemoteAddr),
		r.Header.Get("X-Forwarded-For"),
		r.Header.Get("X-Real-Ip"),
	)
}

// logOp 输出文件上传/下载的结构化访问日志（成功/失败各一条），不记录凭证。
func (h *FileHandler) logOp(r *http.Request, op, bucketID, fileID string, principal *shared.Principal, err error) {
	attrs := []any{
		slog.String("op", op),
		slog.String("bucket_id", bucketID),
		slog.String("file_id", fileID),
		slog.String("ip", h.clientIP(r)),
	}
	if principal != nil {
		attrs = append(attrs,
			slog.String("actor_id", string(principal.ActorID)),
			slog.String("actor_kind", string(principal.ActorKind)),
			slog.String("credential_type", string(principal.CredentialType)),
			slog.String("project_id", principal.ProjectID),
		)
	}
	if err != nil {
		st, _ := status.FromError(err)
		attrs = append(attrs, slog.String("error", st.Code().String()))
		h.logger.Warn("file operation failed", attrs...)
		return
	}
	h.logger.Info("file operation", attrs...)
}

// Register attaches the upload/download routes to the gateway mux.
func (h *FileHandler) Register(mux *runtime.ServeMux) {
	_ = mux.HandlePath("POST", "/v1/storage/buckets/{bucketId}/files", h.upload)
	_ = mux.HandlePath("GET", "/v1/storage/buckets/{bucketId}/files/{fileId}/download", h.download)
	_ = mux.HandlePath("GET", "/v1/storage/buckets/{bucketId}/files/{fileId}/view", h.download)
}

// maxUploadBytes caps the total size of a multipart upload request body.
const maxUploadBytes = 100 << 20 // 100 MiB

func (h *FileHandler) upload(w http.ResponseWriter, r *http.Request, pathParams map[string]string) {
	ctx := r.Context()
	bucketID := pathParams["bucketId"]
	principal, err := h.authorize(r)
	if err != nil {
		h.logOp(r, "upload", bucketID, "", nil, err)
		httpError(w, err)
		return
	}
	projectID := h.projectID(r, principal)
	if projectID == "" {
		err := status.Error(codes.Unauthenticated, "missing project context")
		h.logOp(r, "upload", bucketID, "", principal, err)
		httpError(w, err)
		return
	}
	if bucketID == "" {
		httpError(w, status.Error(codes.InvalidArgument, "missing bucket id"))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		httpError(w, status.Error(codes.InvalidArgument, "invalid multipart form or file too large"))
		return
	}
	defer r.MultipartForm.RemoveAll()

	fileHeader := r.MultipartForm.File["file"]
	if len(fileHeader) == 0 {
		file, fh, err := r.FormFile("file")
		if err != nil {
			httpError(w, status.Error(codes.InvalidArgument, "missing file"))
			return
		}
		defer file.Close()
		h.createFile(ctx, w, r, projectID, bucketID, file, fh.Size, fh.Filename, fh.Header.Get("Content-Type"), principal)
		return
	}

	fh := fileHeader[0]
	f, err := fh.Open()
	if err != nil {
		httpError(w, status.Error(codes.Internal, "cannot open uploaded file"))
		return
	}
	defer f.Close()

	h.createFile(ctx, w, r, projectID, bucketID, f, fh.Size, fh.Filename, fh.Header.Get("Content-Type"), principal)
}

func (h *FileHandler) createFile(ctx context.Context, w http.ResponseWriter, r *http.Request, projectID, bucketID string, rd io.Reader, size int64, name, contentType string, principal *shared.Principal) {
	file, err := h.storage.CreateFile(ctx, storage.CreateFileCommand{
		ProjectID:   projectID,
		OwnerUserID: principal.UserID,
		BucketID:    bucketID,
		Name:        name,
		MimeType:    contentType,
	}, rd, size, databases.Principal{Roles: principal.Roles, PlatformAdmin: principal.IsPlatformAdmin})
	if err != nil {
		h.logOp(r, "upload", bucketID, "", principal, err)
		httpError(w, err)
		return
	}
	h.logOp(r, "upload", bucketID, file.ID, principal, nil)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         file.ID,
		"bucket_id":  file.BucketID,
		"name":       file.Name,
		"mime_type":  file.MimeType,
		"size":       file.Size,
		"created_at": file.CreatedAt,
		"updated_at": file.UpdatedAt,
	})
}

func (h *FileHandler) download(w http.ResponseWriter, r *http.Request, pathParams map[string]string) {
	ctx := r.Context()
	bucketID := pathParams["bucketId"]
	fileID := pathParams["fileId"]
	principal, err := h.authorize(r)
	if err != nil {
		h.logOp(r, "download", bucketID, fileID, nil, err)
		httpError(w, err)
		return
	}
	projectID := h.projectID(r, principal)
	if projectID == "" {
		err := status.Error(codes.Unauthenticated, "missing project context")
		h.logOp(r, "download", bucketID, fileID, principal, err)
		httpError(w, err)
		return
	}
	if bucketID == "" || fileID == "" {
		httpError(w, status.Error(codes.InvalidArgument, "missing bucket or file id"))
		return
	}

	file, reader, err := h.storage.GetFile(ctx, projectID, bucketID, fileID, databases.Principal{Roles: principal.Roles, PlatformAdmin: principal.IsPlatformAdmin})
	if err != nil {
		h.logOp(r, "download", bucketID, fileID, principal, err)
		httpError(w, err)
		return
	}
	defer reader.Close()
	h.logOp(r, "download", bucketID, fileID, principal, nil)

	w.Header().Set("Content-Type", file.MimeType)
	disposition := "attachment"
	if !strings.HasSuffix(r.URL.Path, "/download") && inlineSafeMime(file.MimeType) {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", contentDispositionHeader(disposition, file.Name))
	// 响应加固：禁止浏览器 MIME 嗅探，并把同源输出限制在沙箱内，杜绝存储型 XSS。
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, reader)
}

// inlineSafeMime 判断文件 MIME 是否可安全以 inline 方式展示：
// 视频/音频（无脚本执行面）与白名单类型可以内联；SVG 可内嵌脚本，一律按附件下载。
func inlineSafeMime(mime string) bool {
	if mime == "image/svg+xml" {
		return false
	}
	if _, ok := inlineSafeMimeTypes[mime]; ok {
		return true
	}
	return strings.HasPrefix(mime, "video/") || strings.HasPrefix(mime, "audio/")
}

func (h *FileHandler) authorize(r *http.Request) (*shared.Principal, error) {
	ctx := r.Context()
	principal, err := h.authenticate(r)
	if err != nil {
		return nil, err
	}
	if principal.CredentialType == shared.CredentialTypeAPIKey &&
		!interceptor.APIKeyScopeAllowed(interceptor.StorageServiceCreateFile, principal.Permissions) {
		return nil, status.Error(codes.PermissionDenied, "api key missing required scope")
	}
	if principal.ActorKind == shared.ActorKindAdmin {
		if projectID := strings.TrimSpace(r.Header.Get("X-Torchwood-Project")); projectID != "" {
			principal.ProjectID = projectID
		}
		if err := h.validator.ValidateAdminProjectAccess(ctx, principal); err != nil {
			return nil, err
		}
	}
	return principal, nil
}

func (h *FileHandler) authenticate(r *http.Request) (*shared.Principal, error) {
	ctx := r.Context()
	if key := r.Header.Get("X-Api-Key"); key != "" {
		return h.validator.ValidateCredential(ctx, key, shared.CredentialTypeAPIKey)
	}
	if authz := r.Header.Get("Authorization"); authz != "" {
		// 与 gRPC 认证拦截器走同一解析逻辑：支持 Bearer / Session / ApiKey scheme，
		// scheme 不识别时直接拒绝，而不是把整串当 token 校验。
		credentialType, token, ok := interceptor.ParseAuthorizationHeader(authz)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "invalid authorization header")
		}
		return h.validator.ValidateCredential(ctx, token, credentialType)
	}
	for _, c := range r.Cookies() {
		if strings.HasPrefix(c.Name, "TORCHWOOD_session_") {
			return h.validator.ValidateCredential(ctx, c.Value, shared.CredentialTypeSession)
		}
	}
	return nil, status.Error(codes.Unauthenticated, "authentication credential is not provided")
}

func (h *FileHandler) projectID(r *http.Request, p *shared.Principal) string {
	if p == nil {
		return ""
	}
	switch p.CredentialType {
	case shared.CredentialTypeAPIKey:
		return p.ProjectID
	case shared.CredentialTypeToken, shared.CredentialTypeSession:
		if p.ActorKind == shared.ActorKindAdmin {
			if pid := strings.TrimSpace(r.Header.Get("X-Torchwood-Project")); pid != "" {
				return pid
			}
		}
		return p.ProjectID
	default:
		return p.ProjectID
	}
}

func safeFilename(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '"', r == '\\':
			b.WriteByte('_')
		case r < 32, r == 127:
			// drop control characters to prevent header injection
			continue
		case r == '\n', r == '\r':
			continue
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "download"
	}
	return out
}

func contentDispositionHeader(disposition, name string) string {
	safe := safeFilename(name)
	ascii := asciiFilenameFallback(safe)
	encoded := url.PathEscape(safe)
	return fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, disposition, ascii, encoded)
}

func asciiFilenameFallback(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r >= 32 && r <= 126 && r != '"' && r != '\\' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), "._ ")
	if out == "" {
		return "download"
	}
	return out
}

func httpError(w http.ResponseWriter, err error) {
	st, ok := status.FromError(err)
	if !ok {
		st = status.New(codes.Internal, err.Error())
	}
	httpStatus := runtime.HTTPStatusFromCode(st.Code())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	payload, _ := json.Marshal(map[string]any{
		"error": map[string]string{
			"type":    st.Code().String(),
			"message": st.Message(),
		},
	})
	_, _ = w.Write(payload)
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}
