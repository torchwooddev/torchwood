package server

import (
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
