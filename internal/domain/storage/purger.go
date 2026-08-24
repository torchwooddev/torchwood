package storage

import "context"

// Purger 抽象对象存储按前缀批量清理的能力（Round4 J5-2）：项目删除后，
// 共享桶内 {projectID}/ 前缀下的文件字节必须随之清除（GDPR 类合规要求），
// 否则删除项目会永久残留用户数据。
type Purger interface {
	// PurgePrefix 删除 bucket 下指定前缀的全部对象，返回删除的对象数。
	// List+Delete 幂等；调用方负责超时与重试预算。
	PurgePrefix(ctx context.Context, bucket, prefix string) (int, error)
}
