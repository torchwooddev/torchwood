package server

import (
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrorCode 返回错误的 gRPC code；非 status 错误返回 codes.Unknown。
func ErrorCode(err error) codes.Code {
	return status.Code(err)
}

// IsPermissionDenied 报告错误是否为 PermissionDenied（如 API Key scope 不足）。
func IsPermissionDenied(err error) bool {
	return status.Code(err) == codes.PermissionDenied
}

// IsUnauthenticated 报告错误是否为 Unauthenticated（凭证缺失/过期/无效）。
func IsUnauthenticated(err error) bool {
	return status.Code(err) == codes.Unauthenticated
}

// HTTPErrorClass 把错误按 grpc-gateway 的 gRPC→HTTP 映射归入粗粒度类别，
// 供脚本化调用方（如 CLI）做退出码分支：
//
//	0 = 成功（codes.OK）
//	2 = HTTP 4xx 客户端错误（InvalidArgument / Unauthenticated /
//	    PermissionDenied / NotFound / AlreadyExists / FailedPrecondition /
//	    Aborted / OutOfRange / Canceled(408/499)）
//	4 = 限流（ResourceExhausted，HTTP 429；配合 ExtractRetryAfter 取退避）
//	3 = 其余（Internal / Unknown / DataLoss / Unimplemented / Unavailable /
//	    DeadlineExceeded 等，HTTP 5xx；非 status 错误也归入此类）
func HTTPErrorClass(err error) int {
	switch status.Code(err) {
	case codes.OK:
		return 0
	case codes.ResourceExhausted:
		return 4
	case codes.Canceled, codes.InvalidArgument, codes.OutOfRange,
		codes.FailedPrecondition, codes.Unauthenticated, codes.PermissionDenied,
		codes.NotFound, codes.Aborted, codes.AlreadyExists:
		return 2
	default:
		return 3
	}
}

// ExtractRetryAfter 从错误的 google.rpc.RetryInfo detail 中提取建议退避
// 时长（服务端限流 ResourceExhausted 时附带）。无 detail 或解析失败返回 false。
func ExtractRetryAfter(err error) (time.Duration, bool) {
	st, ok := status.FromError(err)
	if !ok {
		return 0, false
	}
	for _, d := range st.Details() {
		if ri, ok := d.(*errdetails.RetryInfo); ok && ri.GetRetryDelay().AsDuration() > 0 {
			return ri.GetRetryDelay().AsDuration(), true
		}
	}
	return 0, false
}
