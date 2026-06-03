package sandbox

import _ "embed"

// The .sbpl policy assets are embedded verbatim from codex-sandboxing so that
// the generated Seatbelt policy matches codex byte-for-byte. Mirrors the
// include_str! constants in seatbelt.rs.

//go:embed seatbelt_base_policy.sbpl
var macosSeatbeltBasePolicy string

//go:embed seatbelt_network_policy.sbpl
var macosSeatbeltNetworkPolicy string

//go:embed restricted_read_only_platform_defaults.sbpl
var macosRestrictedReadOnlyPlatformDefaults string

// MacosPathToSeatbeltExecutable is the only sandbox-exec binary the backend will
// invoke. Pinning the absolute path defends against an attacker injecting a
// malicious sandbox-exec earlier on PATH; if /usr/bin/sandbox-exec itself has
// been tampered with the attacker already has root. Mirrors
// MACOS_PATH_TO_SEATBELT_EXECUTABLE.
const MacosPathToSeatbeltExecutable = "/usr/bin/sandbox-exec"
