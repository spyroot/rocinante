/*
	Raft RaftProtocol volatile state
	hold commit index and last applied

	Mustafa Bayramov
*/
package server

import (
	"sync/atomic"
)

/**
  Raft Logs Volatile State
*/
type RaftVolatileState struct {
	commitIndex uint64
	lastApplied uint64
}

/**
Set commit index
*/
func (v *RaftVolatileState) setCommitIndex(commit uint64) {
	atomic.StoreUint64(&v.commitIndex, commit)
}

/*
	Set last applied entry
*/
func (v *RaftVolatileState) setLastApplied(applied uint64) {
	atomic.StoreUint64(&v.lastApplied, applied)
}

/**
Return last commit index
*/
func (v *RaftVolatileState) CommitIndex() uint64 {
	return atomic.LoadUint64(&v.commitIndex)
}

/**
  Return last applied
*/
func (v *RaftVolatileState) LastApplied() uint64 {
	return atomic.LoadUint64(&v.lastApplied)
}
