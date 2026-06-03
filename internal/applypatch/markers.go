package applypatch

// Patch envelope marker strings. These MUST match Codex byte-for-byte because
// the model emits these envelopes directly. They mirror the constants in
// `parser.rs`.
const (
	beginPatchMarker         = "*** Begin Patch"
	environmentIDMarker      = "*** Environment ID: "
	endPatchMarker           = "*** End Patch"
	addFileMarker            = "*** Add File: "
	deleteFileMarker         = "*** Delete File: "
	updateFileMarker         = "*** Update File: "
	moveToMarker             = "*** Move to: "
	eofMarker                = "*** End of File"
	changeContextMarker      = "@@ "
	emptyChangeContextMarker = "@@"
)
