package shared

import (
	"errors"
	"fmt"
	"strconv"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/pkg/ident"
)

// 域错误码体系（redesign §4.1）：域码稳定 snake_case 静态映射 gRPC code，
// 消息格式 "CODE: message"，ErrorInfo detail 携带 reason/retryable——Agent
// 可机器判定恢复路径。POC 直接替换旧文案，无兼容映射。

// domainCodeGRPC 是域码 → gRPC code 的静态映射（唯一真相源）。
var domainCodeGRPC = map[string]codes.Code{
	databases.ErrCodePermissionDenied:         codes.PermissionDenied,
	databases.ErrCodeAlreadyExists:            codes.AlreadyExists,
	databases.ErrCodeNotFound:                 codes.NotFound,
	databases.ErrCodeNoFieldsToUpdate:         codes.InvalidArgument,
	databases.ErrCodeVersionRequired:          codes.FailedPrecondition,
	databases.ErrCodeVersionInvalid:           codes.InvalidArgument,
	databases.ErrCodeVersionConflict:          codes.FailedPrecondition,
	databases.ErrCodeVersionColumnConflict:    codes.FailedPrecondition,
	databases.ErrCodeVersionColumnUnavailable: codes.InvalidArgument,
	databases.ErrCodeInvalidArgument:          codes.InvalidArgument,
	databases.ErrCodeTooLarge:                 codes.InvalidArgument,
	databases.ErrCodeExhausted:                codes.ResourceExhausted,
	databases.ErrCodeIdempotencyKeyConflict:   codes.InvalidArgument,
	databases.ErrCodeIdempotencyInProgress:    codes.Aborted,
}

// domainCodeMessage 是域码的人类可读消息（与领域哨兵文案同源）。
var domainCodeMessage = map[string]string{
	databases.ErrCodePermissionDenied:         databases.ErrPermissionDenied.Error(),
	databases.ErrCodeAlreadyExists:            databases.ErrDuplicateKey.Error(),
	databases.ErrCodeNotFound:                 databases.ErrDocumentNotFound.Error(),
	databases.ErrCodeNoFieldsToUpdate:         databases.ErrNoFieldsToUpdate.Error(),
	databases.ErrCodeVersionRequired:          databases.ErrVersionRequired.Error(),
	databases.ErrCodeVersionInvalid:           databases.ErrVersionInvalid.Error(),
	databases.ErrCodeVersionConflict:          databases.ErrVersionMismatch.Error(),
	databases.ErrCodeVersionColumnConflict:    databases.ErrVersionColumnConflict.Error(),
	databases.ErrCodeVersionColumnUnavailable: databases.ErrVersionColumnUnavailable.Error(),
	databases.ErrCodeInvalidArgument:          "invalid argument",
	databases.ErrCodeTooLarge:                 "document payload too large",
	databases.ErrCodeExhausted:                "resource exhausted",
	databases.ErrCodeIdempotencyKeyConflict:   databases.ErrIdempotencyKeyConflict.Error(),
	databases.ErrCodeIdempotencyInProgress:    "request with the same idempotency key is still in progress",
}

const errorInfoDomain = "torchwood.document"

// DomainStatus 构造携带稳定域码的 gRPC status：消息 "CODE: message"，
// ErrorInfo detail（reason/retryable）。
func DomainStatus(domainCode string) error {
	st := status.New(domainCodeGRPC[domainCode], domainCode+": "+domainCodeMessage[domainCode])
	st, _ = st.WithDetails(&errdetails.ErrorInfo{
		Reason: domainCode,
		Domain: errorInfoDomain,
		Metadata: map[string]string{
			"retryable": strconv.FormatBool(databases.ErrorCodeRetryable(domainCode)),
		},
	})
	return st.Err()
}

// DomainStatusWithOp 是 DomainStatus 的批事务变体：消息与 ErrorInfo
// metadata 均携带失败 op index（ATOMIC 模式 violations 定位）。
func DomainStatusWithOp(domainCode string, opIndex int) error {
	code := domainCodeGRPC[domainCode]
	st := status.New(code, fmt.Sprintf("%s: op[%d]: %s", domainCode, opIndex, domainCodeMessage[domainCode]))
	st, _ = st.WithDetails(&errdetails.ErrorInfo{
		Reason: domainCode,
		Domain: errorInfoDomain,
		Metadata: map[string]string{
			"retryable": strconv.FormatBool(databases.ErrorCodeRetryable(domainCode)),
			"op_index":  strconv.Itoa(opIndex),
		},
	})
	return st.Err()
}

// UpdateDocumentVersionRequired 校验用户集合 Update/Delete 的 OCC 版本参数
// （Phase 1 裁决②三态拆分）：
//   - 缺省（nil）→ FailedPrecondition / DOCUMENT.VERSION_REQUIRED；
//   - 显式 ≤0（非法值）→ InvalidArgument / DOCUMENT.VERSION_INVALID；
//   - 正确值 → 通过。
func UpdateDocumentVersionRequired(version *int64) error {
	if version == nil {
		return DomainStatus(databases.ErrCodeVersionRequired)
	}
	if *version <= 0 {
		return DomainStatus(databases.ErrCodeVersionInvalid)
	}
	return nil
}

// MapDocumentDBError 将文档库错误映射为携带稳定域码的 gRPC status：
// ① 领域哨兵（可穿透 fmt.Errorf 包装链）→ DomainStatus；
// ② infra 已产出的域码 status（SQLSTATE 路径）从包装链提取后透传保真——
//
//	不提取会因丢失 GRPCStatus() 实现而退化为 Internal；
//
// ③ 其余原样返回。
func MapDocumentDBError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ident.ErrInvalidSchemaResourceID) {
		return MapIdentError(err)
	}
	if code := databases.ErrorDomainCode(err); code != "" {
		return DomainStatus(code)
	}
	var gs interface{ GRPCStatus() *status.Status }
	if errors.As(err, &gs) {
		return gs.GRPCStatus().Err()
	}
	return err
}
