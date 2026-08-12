package storage

import (
	"context"
	"time"
)

// UploadSession 是分片上传会话（元数据存 Redis，分片对象暂存对象存储）。
type UploadSession struct {
	ID          string
	ProjectID   string
	BucketID    string
	FileID      string // 预生成，complete 时创建文件文档
	OwnerUserID string // 创建者用户 ID；API key 创建为空串（此时不做 owner 校验）
	Name        string
	MimeType    string // 已归一化（normalizeMimeType）
	Size        int64
	Metadata    map[string]string
	Permissions []string
	ChunkSize   int64
	PartCount   int
	Received    map[int]bool // 已收分片（续传查询）
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// UploadSessionStore 持久化上传会话（Redis 实现，TTL 24h）。
type UploadSessionStore interface {
	Create(ctx context.Context, s *UploadSession) error
	Get(ctx context.Context, uploadID string) (*UploadSession, error)
	// MarkChunk 原子标记分片已收（幂等）。
	MarkChunk(ctx context.Context, uploadID string, partNumber int) error
	// CountChunks 返回已收分片数（原子；会话不存在返回 0）。
	CountChunks(ctx context.Context, uploadID string) (int, error)
	Delete(ctx context.Context, uploadID string) error
	// LockComplete 尝试获取 complete 互斥锁（SETNX，1h TTL；超时自动释放防宕机死锁）。
	// 返回锁 token 与是否获取成功（已持有返回 ok=false）。token 供后续 IsLockOwner
	// 二次确认（回滚删对象前确认锁仍归自己持有，防误删其他 complete 的成果）。
	LockComplete(ctx context.Context, uploadID string) (token string, ok bool, err error)
	// IsLockOwner 校验 uploadID 的 complete 锁仍由 token 持有者持有（锁未过期且
	// 未被其他 complete 重新获取）。
	IsLockOwner(ctx context.Context, uploadID, token string) (bool, error)
	// UnlockComplete 释放 complete 锁。
	UnlockComplete(ctx context.Context, uploadID string) error
}

const (
	DefaultChunkSize    = 16 << 20 // 16 MiB
	UploadSessionTTL    = 24 * time.Hour
	MaxChunkSize        = 16 << 20                                      // 单分片上限
	MinComposePartSize  = 5 << 20                                       // ComposeObject：除末片外每片 ≥ 5MiB
	MaxComposePartCount = 10000                                         // ComposeObject 源数上限
	MaxUploadSize       = int64(MaxComposePartCount) * DefaultChunkSize // ≈156.25GB
)
