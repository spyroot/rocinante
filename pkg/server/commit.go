package server

type KeyValuePair struct {
	Key   string `protobuf:"bytes,1,opt,name=key,proto3" json:"key,omitempty"`
	Value []byte `protobuf:"bytes,2,opt,name=value,proto3" json:"value,omitempty"`
}

/**
	Commit entry re-present a
 */
type CommitEntry struct {

	// Command that committed.
	Command KeyValuePair

	// Index is the log index at which the client command is committed.
	Index uint64

	// Term is the Raft term at which the client command is committed.
	Term uint64
}
