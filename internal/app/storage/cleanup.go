package storage

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// orphanChunkAge 孤儿分片清理阈值。活跃上传会话 TTL 24h（Create/MarkChunk 刷新），
// 其分片对象 LastModified 必然 < 24h；48h 阈值提供 2 倍安全余量，
// 杜绝误删活跃上传的分片。
const orphanChunkAge = 48 * time.Hour

// CleanupOrphanChunks 遍历所有项目，删除 key 含 "/chunks/" 且 LastModified
// 超过 48h 的分片对象（会话过期/abort 或 complete 时删除失败产生的孤儿）。
// 项目列表以 DB（projects 表）为准；DeleteBucket 后 project 可能已删，
// 分片 key 前缀 `{projectID}/` 与文件对象相同，过滤以 "/chunks/" 段为准
// （文件对象 key 无该段）。返回删除的对象数。
func (s *Storage) CleanupOrphanChunks(ctx context.Context) (int, error) {
	ctx2, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	projects, err := s.projectRepo.ListProjects(ctx2)
	if err != nil {
		return 0, fmt.Errorf("list projects: %w", err)
	}
	bucket := defaultBucketName(s.cfg)
	threshold := time.Now().Add(-orphanChunkAge)
	removed := 0
	for _, p := range projects {
		objects, err := s.store.List(ctx, bucket, p.ID+"/")
		if err != nil {
			slog.Warn("list objects for chunk cleanup failed", "project", p.ID, "error", err)
			continue
		}
		for _, obj := range objects {
			if !strings.Contains(obj.Key, "/chunks/") {
				continue
			}
			if !obj.LastModified.Before(threshold) {
				continue
			}
			if err := s.store.Delete(ctx, bucket, obj.Key); err != nil {
				slog.Warn("delete orphan chunk failed", "key", obj.Key, "error", err)
				continue
			}
			removed++
		}
	}
	return removed, nil
}
