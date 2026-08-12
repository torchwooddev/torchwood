package serverhttp

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	appfunctions "github.com/torchwooddev/torchwood/internal/app/functions"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/auth"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/pkg/grpc/interceptor"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// FunctionsServiceCreateDeployment 是 multipart 上传对应的 scope 检查方法
// （与 gRPC CreateDeployment 一致）。
const FunctionsServiceCreateDeployment = "/torchwood.server.v1.FunctionsService/CreateDeployment"

// maxCodePackageBytes 是 zip 代码包上传上限（§5.6，50 MiB）。
const maxCodePackageBytes = 50 << 20

// FunctionsHandler 提供 deployment 代码包 multipart 上传。
type FunctionsHandler struct {
	cfg       *config.AppConfig
	auth      *httpAuth
	functions *appfunctions.Functions
	trusted   *interceptor.TrustedProxies
	logger    *slog.Logger
}

// NewFunctionsHandler creates a new functions HTTP handler.
func NewFunctionsHandler(
	cfg *config.AppConfig,
	validator *auth.Validator,
	functions *appfunctions.Functions,
	logger *slog.Logger,
) (*FunctionsHandler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	trusted, err := interceptor.ParseTrustedProxies(cfg.GetSecurity().GetTrustedProxies())
	if err != nil {
		return nil, fmt.Errorf("parse security.trusted_proxies: %w", err)
	}
	return &FunctionsHandler{cfg: cfg, auth: newHTTPAuth(validator), functions: functions, trusted: trusted, logger: logger}, nil
}

// Register attaches the deployment upload route to the gateway mux.
func (h *FunctionsHandler) Register(mux *runtime.ServeMux) {
	_ = mux.HandlePath("POST", "/v1/server/functions/{functionId}/deployments/code", h.upload)
}

// logOp 输出 deployment 上传的结构化访问日志（成功/失败各一条）。
func (h *FunctionsHandler) logOp(r *http.Request, op, functionID, deploymentID string, principal *shared.Principal, err error) {
	attrs := []any{
		slog.String("op", op),
		slog.String("function_id", functionID),
		slog.String("deployment_id", deploymentID),
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
		h.logger.Warn("deployment operation failed", attrs...)
		return
	}
	h.logger.Info("deployment operation", attrs...)
}

func (h *FunctionsHandler) clientIP(r *http.Request) string {
	return h.trusted.ResolveClientIP(
		interceptor.PeerIPFromAddr(r.RemoteAddr),
		r.Header.Get("X-Forwarded-For"),
		r.Header.Get("X-Real-Ip"),
	)
}

// upload POST /v1/server/functions/{functionId}/deployments/code
// multipart 字段 code（zip 文件，≤50MiB）。
func (h *FunctionsHandler) upload(w http.ResponseWriter, r *http.Request, pathParams map[string]string) {
	ctx := r.Context()
	functionID := pathParams["functionId"]
	principal, err := h.authorize(r)
	if err != nil {
		h.logOp(r, "deployment-upload", functionID, "", nil, err)
		httpError(w, err)
		return
	}
	projectID := h.auth.projectID(r, principal)
	if projectID == "" {
		err := status.Error(codes.Unauthenticated, "missing project context")
		h.logOp(r, "deployment-upload", functionID, "", principal, err)
		httpError(w, err)
		return
	}
	if functionID == "" {
		httpError(w, status.Error(codes.InvalidArgument, "missing function id"))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxCodePackageBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		httpError(w, status.Error(codes.InvalidArgument, "invalid multipart form or file too large"))
		return
	}
	defer r.MultipartForm.RemoveAll()

	fileHeaders := r.MultipartForm.File["code"]
	if len(fileHeaders) == 0 {
		httpError(w, status.Error(codes.InvalidArgument, "missing code file"))
		return
	}
	fh := fileHeaders[0]
	f, err := fh.Open()
	if err != nil {
		httpError(w, status.Error(codes.Internal, "cannot open uploaded code package"))
		return
	}
	defer f.Close()

	code, err := io.ReadAll(io.LimitReader(f, maxCodePackageBytes+1))
	if err != nil {
		httpError(w, status.Error(codes.Internal, "cannot read uploaded code package"))
		return
	}
	if len(code) > maxCodePackageBytes {
		httpError(w, status.Error(codes.InvalidArgument, "code package exceeds 50 MiB limit"))
		return
	}
	if !isZipMagic(code) {
		httpError(w, status.Error(codes.InvalidArgument, "invalid zip file: missing PK zip signature"))
		return
	}

	dep, err := h.functions.CreateDeployment(ctx, appfunctions.CreateDeploymentCommand{
		ProjectID:  projectID,
		FunctionID: functionID,
		Code:       code,
	})
	if err != nil {
		h.logOp(r, "deployment-upload", functionID, "", principal, err)
		httpError(w, err)
		return
	}
	h.logOp(r, "deployment-upload", functionID, dep.ID, principal, nil)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":          dep.ID,
		"function_id": dep.FunctionID,
		"size":        dep.Size,
		"status":      dep.Status,
		"error":       dep.Error,
		"created_at":  dep.CreatedAt,
		"updated_at":  dep.UpdatedAt,
	})
}

// authorize 与 file_handler 一致：API key 走 FunctionsService/CreateDeployment
// scope（functions.write），admin 走 X-Torchwood-Project + ValidateAdminProjectAccess；
// 认证/项目解析等公共逻辑见 httpAuth（auth.go）。端用户（Bearer JWT / 会话 cookie）
// 一律禁止上传部署代码包：任意注册用户可借此触发 Docker 构建并部署恶意代码窃取
// 函数环境变量（安全评审 03）。
func (h *FunctionsHandler) authorize(r *http.Request) (*shared.Principal, error) {
	principal, err := h.auth.authorize(r, func(*http.Request) string {
		return FunctionsServiceCreateDeployment
	})
	if err != nil {
		return nil, err
	}
	if principal.CredentialType == shared.CredentialTypeToken || principal.CredentialType == shared.CredentialTypeSession {
		if principal.ActorKind != shared.ActorKindAdmin {
			return nil, status.Error(codes.PermissionDenied, "end-user credentials cannot upload deployments")
		}
	}
	return principal, nil
}

// isZipMagic 校验 zip 魔数 PK\x03\x04。
func isZipMagic(code []byte) bool {
	return len(code) >= 4 && bytes.Equal(code[:4], []byte{0x50, 0x4B, 0x03, 0x04})
}
