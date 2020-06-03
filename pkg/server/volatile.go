package server

import (
	"sync/atomic"
)

type RaftVolatileState struct {
	commitIndex        uint64
	lastApplied        uint64
}

func (v *RaftVolatileState) setCommitIndex(commit uint64) {
	atomic.StoreUint64(&v.commitIndex, commit)
}

func (v *RaftVolatileState) setLastApplied(applied uint64) {
	atomic.StoreUint64(&v.lastApplied, applied)
}

func (v *RaftVolatileState) CommitIndex() uint64 {
	return atomic.LoadUint64(&v.commitIndex)
}

func (v *RaftVolatileState) LastApplied() uint64  {
	return atomic.LoadUint64(&v.lastApplied)
}