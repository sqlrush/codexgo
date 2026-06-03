package hooks

// universalOutput is the parsed, semantic form of the shared hook output
// envelope (continue/stopReason/suppressOutput/systemMessage). Mirrors
// UniversalOutput. ContinueProcessing defaults to true when the wire omits the
// "continue" key (see parseUniversalWire).
type universalOutput struct {
	ContinueProcessing bool
	StopReason         *string
	SuppressOutput     bool
	SystemMessage      *string
}
