package cliutil

// SharedCliOptions holds the command-line flags shared by both the interactive
// and non-interactive Codex entry points.
//
// It mirrors codex_utils_cli::SharedCliOptions. Optional scalar flags are
// modeled as pointers so an unset flag (Go nil) is distinguishable from a flag
// explicitly set to an empty value, matching the upstream Option<T> fields.
//
// Following the package's immutable style, the merge helpers return a new
// SharedCliOptions rather than mutating the receiver. The struct's slices are
// always copied when stored or returned so callers cannot observe aliasing.
type SharedCliOptions struct {
	// Images are optional image file paths to attach to the initial prompt
	// (--image/-i).
	Images []string

	// Model is the model the agent should use (--model/-m).
	Model *string

	// OSS selects an open-source provider (--oss).
	OSS bool

	// OSSProvider names a specific local provider such as lmstudio or ollama
	// (--local-provider).
	OSSProvider *string

	// ConfigProfileV2 selects a $CODEX_HOME/<name>.config.toml layer
	// (--profile/-p).
	ConfigProfileV2 *ProfileV2Name

	// SandboxMode selects the sandbox policy (--sandbox/-s).
	SandboxMode *SandboxModeCliArg

	// DangerouslyBypassApprovalsAndSandbox skips all confirmation prompts and
	// disables sandboxing (--dangerously-bypass-approvals-and-sandbox / --yolo).
	DangerouslyBypassApprovalsAndSandbox bool

	// BypassHookTrust runs enabled hooks without persisted hook trust for this
	// invocation (--dangerously-bypass-hook-trust).
	BypassHookTrust bool

	// Cwd sets the agent's working root (--cd/-C).
	Cwd *string

	// AddDir lists additional writable directories (--add-dir).
	AddDir []string
}

// clone returns a deep copy of the options so the merge helpers never alias the
// receiver's or argument's backing storage.
func (o SharedCliOptions) clone() SharedCliOptions {
	return SharedCliOptions{
		Images:                               cloneStrings(o.Images),
		Model:                                cloneStrPtr(o.Model),
		OSS:                                  o.OSS,
		OSSProvider:                          cloneStrPtr(o.OSSProvider),
		ConfigProfileV2:                      cloneProfilePtr(o.ConfigProfileV2),
		SandboxMode:                          cloneSandboxPtr(o.SandboxMode),
		DangerouslyBypassApprovalsAndSandbox: o.DangerouslyBypassApprovalsAndSandbox,
		BypassHookTrust:                      o.BypassHookTrust,
		Cwd:                                  cloneStrPtr(o.Cwd),
		AddDir:                               cloneStrings(o.AddDir),
	}
}

// InheritExecRootOptions returns a new SharedCliOptions that fills unset fields
// of the receiver from root, mirroring the upstream inherit_exec_root_options.
//
// Semantics preserved from upstream:
//
//   - Whether the receiver "selected a sandbox mode" is decided before merging,
//     and is true when SandboxMode is set or the dangerous bypass flag is set.
//   - Scalar options (Model, OSSProvider, ConfigProfileV2, Cwd) inherit from
//     root only when unset on the receiver.
//   - OSS is OR-ed: it becomes true when root has it set.
//   - SandboxMode inherits from root only when unset on the receiver.
//   - The dangerous bypass flag inherits from root only when the receiver did
//     not itself select a sandbox mode.
//   - BypassHookTrust inherits from root only when not already set.
//   - root's images and add-dirs, when non-empty, are prepended to the
//     receiver's corresponding slices.
//
// Neither the receiver nor root is mutated.
func (o SharedCliOptions) InheritExecRootOptions(root SharedCliOptions) SharedCliOptions {
	result := o.clone()
	selfSelectedSandboxMode := result.SandboxMode != nil || result.DangerouslyBypassApprovalsAndSandbox

	if result.Model == nil {
		result.Model = cloneStrPtr(root.Model)
	}
	if root.OSS {
		result.OSS = true
	}
	if result.OSSProvider == nil {
		result.OSSProvider = cloneStrPtr(root.OSSProvider)
	}
	if result.ConfigProfileV2 == nil {
		result.ConfigProfileV2 = cloneProfilePtr(root.ConfigProfileV2)
	}
	if result.SandboxMode == nil {
		result.SandboxMode = cloneSandboxPtr(root.SandboxMode)
	}
	if !selfSelectedSandboxMode {
		result.DangerouslyBypassApprovalsAndSandbox = root.DangerouslyBypassApprovalsAndSandbox
	}
	if !result.BypassHookTrust {
		result.BypassHookTrust = root.BypassHookTrust
	}
	if result.Cwd == nil {
		result.Cwd = cloneStrPtr(root.Cwd)
	}
	if len(root.Images) > 0 {
		result.Images = concatStrings(root.Images, result.Images)
	}
	if len(root.AddDir) > 0 {
		result.AddDir = concatStrings(root.AddDir, result.AddDir)
	}
	return result
}

// ApplySubcommandOverrides returns a new SharedCliOptions that overlays the
// subcommand's set fields onto the receiver, mirroring the upstream
// apply_subcommand_overrides.
//
// Semantics preserved from upstream:
//
//   - Whether the subcommand "selected a sandbox mode" is decided up front, and
//     is true when its SandboxMode is set or its dangerous bypass flag is set.
//   - Set scalar options (Model, OSSProvider, ConfigProfileV2, Cwd) override the
//     receiver's.
//   - OSS is OR-ed in from the subcommand.
//   - When the subcommand selected a sandbox mode, both its SandboxMode and its
//     dangerous bypass flag override the receiver's.
//   - BypassHookTrust is OR-ed in from the subcommand.
//   - A non-empty subcommand images list replaces the receiver's images.
//   - A non-empty subcommand add-dir list is appended to the receiver's.
//
// Neither the receiver nor subcommand is mutated.
func (o SharedCliOptions) ApplySubcommandOverrides(subcommand SharedCliOptions) SharedCliOptions {
	result := o.clone()
	subcommandSelectedSandboxMode := subcommand.SandboxMode != nil ||
		subcommand.DangerouslyBypassApprovalsAndSandbox

	if subcommand.Model != nil {
		result.Model = cloneStrPtr(subcommand.Model)
	}
	if subcommand.OSS {
		result.OSS = true
	}
	if subcommand.OSSProvider != nil {
		result.OSSProvider = cloneStrPtr(subcommand.OSSProvider)
	}
	if subcommand.ConfigProfileV2 != nil {
		result.ConfigProfileV2 = cloneProfilePtr(subcommand.ConfigProfileV2)
	}
	if subcommandSelectedSandboxMode {
		result.SandboxMode = cloneSandboxPtr(subcommand.SandboxMode)
		result.DangerouslyBypassApprovalsAndSandbox = subcommand.DangerouslyBypassApprovalsAndSandbox
	}
	if subcommand.BypassHookTrust {
		result.BypassHookTrust = true
	}
	if subcommand.Cwd != nil {
		result.Cwd = cloneStrPtr(subcommand.Cwd)
	}
	if len(subcommand.Images) > 0 {
		result.Images = cloneStrings(subcommand.Images)
	}
	if len(subcommand.AddDir) > 0 {
		result.AddDir = concatStrings(result.AddDir, subcommand.AddDir)
	}
	return result
}

// cloneStrings returns a copy of s, or nil when s is nil.
func cloneStrings(s []string) []string {
	if s == nil {
		return nil
	}
	cp := make([]string, len(s))
	copy(cp, s)
	return cp
}

// concatStrings returns a fresh slice containing the elements of a followed by
// the elements of b.
func concatStrings(a, b []string) []string {
	out := make([]string, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}

// cloneStrPtr returns a deep copy of a *string.
func cloneStrPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

// cloneSandboxPtr returns a deep copy of a *SandboxModeCliArg.
func cloneSandboxPtr(s *SandboxModeCliArg) *SandboxModeCliArg {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

// cloneProfilePtr returns a deep copy of a *ProfileV2Name.
func cloneProfilePtr(p *ProfileV2Name) *ProfileV2Name {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
