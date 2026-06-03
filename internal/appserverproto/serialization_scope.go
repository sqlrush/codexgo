package appserverproto

// SerializationScopeKind discriminates the keyed families a client request may
// serialize against. Mirrors the Rust enum ClientRequestSerializationScope.
//
// The scope controls request ordering on the server: requests sharing a scope
// key are serialized relative to each other, while None-scoped requests run
// concurrently. This package carries the scope as data; the dispatcher that
// owns sequencing lives elsewhere.
type SerializationScopeKind int

const (
	// ScopeNone indicates a request that is not serialized against any key
	// (Rust: None). Such requests may run concurrently.
	ScopeNone SerializationScopeKind = iota
	// ScopeGlobal serializes against a named global lock (Rust: Global(key)).
	ScopeGlobal
	// ScopeGlobalSharedRead serializes against a named global read-shared lock
	// (Rust: GlobalSharedRead(key)).
	ScopeGlobalSharedRead
	// ScopeThread serializes against a thread id (Rust: Thread { thread_id }).
	ScopeThread
	// ScopeThreadPath serializes against a rollout path (Rust: ThreadPath { path }).
	ScopeThreadPath
	// ScopeCommandExecProcess serializes against a command/exec process id
	// (Rust: CommandExecProcess { process_id }).
	ScopeCommandExecProcess
	// ScopeProcess serializes against a process/spawn handle (Rust: Process { process_handle }).
	ScopeProcess
	// ScopeFuzzyFileSearchSession serializes against a fuzzy search session id
	// (Rust: FuzzyFileSearchSession { session_id }).
	ScopeFuzzyFileSearchSession
	// ScopeFsWatch serializes against a filesystem watch id (Rust: FsWatch { watch_id }).
	ScopeFsWatch
	// ScopeMcpOauth serializes against an MCP server name (Rust: McpOauth { server_name }).
	ScopeMcpOauth
)

// SerializationScope is a keyed scope for a client request. The Kind selects
// which family applies; Key holds the discriminating string (global key, thread
// id, process id, etc.). For ScopeNone the zero value is used.
//
// This mirrors the Rust enum's variants. Because every keyed variant carries a
// single string (and ThreadPath a path string), one Key field suffices.
type SerializationScope struct {
	Kind SerializationScopeKind
	Key  string
}

// NoScope returns the unscoped (concurrent) scope.
func NoScope() *SerializationScope { return nil }

// GlobalScope serializes against a named global lock.
func GlobalScope(key string) *SerializationScope {
	return &SerializationScope{Kind: ScopeGlobal, Key: key}
}

// GlobalSharedReadScope serializes against a named global read-shared lock.
func GlobalSharedReadScope(key string) *SerializationScope {
	return &SerializationScope{Kind: ScopeGlobalSharedRead, Key: key}
}

// ThreadScope serializes against a thread id.
func ThreadScope(threadID string) *SerializationScope {
	return &SerializationScope{Kind: ScopeThread, Key: threadID}
}

// ThreadPathScope serializes against a rollout path.
func ThreadPathScope(path string) *SerializationScope {
	return &SerializationScope{Kind: ScopeThreadPath, Key: path}
}

// CommandExecProcessScope serializes against a command/exec process id.
func CommandExecProcessScope(processID string) *SerializationScope {
	return &SerializationScope{Kind: ScopeCommandExecProcess, Key: processID}
}

// ProcessScope serializes against a process/spawn handle.
func ProcessScope(handle string) *SerializationScope {
	return &SerializationScope{Kind: ScopeProcess, Key: handle}
}

// FuzzyFileSearchSessionScope serializes against a fuzzy search session id.
func FuzzyFileSearchSessionScope(sessionID string) *SerializationScope {
	return &SerializationScope{Kind: ScopeFuzzyFileSearchSession, Key: sessionID}
}

// FsWatchScope serializes against a filesystem watch id.
func FsWatchScope(watchID string) *SerializationScope {
	return &SerializationScope{Kind: ScopeFsWatch, Key: watchID}
}

// McpOauthScope serializes against an MCP server name.
func McpOauthScope(serverName string) *SerializationScope {
	return &SerializationScope{Kind: ScopeMcpOauth, Key: serverName}
}
