package idgen

import (
	"fmt"
	"sync"
	"time"
)

const (
	snowflakeEpochMs  = 1577836800000 // 2020-01-01 UTC
	snowflakeNodeBits = 10
	snowflakeSeqBits  = 12
	snowflakeMaxNode  = (1 << snowflakeNodeBits) - 1
	snowflakeMaxSeq   = (1 << snowflakeSeqBits) - 1
)

// Snowflake generates 64-bit time-sortable numeric string IDs.
type Snowflake struct {
	mu       sync.Mutex
	nodeID   int64
	lastMs   int64
	sequence int64
}

func NewSnowflake(nodeID int64) (*Snowflake, error) {
	if nodeID < 0 || nodeID > snowflakeMaxNode {
		return nil, fmt.Errorf("snowflake node_id must be between 0 and %d", snowflakeMaxNode)
	}
	return &Snowflake{nodeID: nodeID}, nil
}

func (s *Snowflake) NextString() string {
	return fmt.Sprintf("%d", s.Next())
}

func (s *Snowflake) Next() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli()
	if now == s.lastMs {
		s.sequence = (s.sequence + 1) & snowflakeMaxSeq
		if s.sequence == 0 {
			for now <= s.lastMs {
				now = time.Now().UnixMilli()
			}
		}
	} else {
		s.sequence = 0
	}
	s.lastMs = now

	id := ((now - snowflakeEpochMs) << (snowflakeNodeBits + snowflakeSeqBits)) |
		(s.nodeID << snowflakeSeqBits) |
		s.sequence
	return id
}
