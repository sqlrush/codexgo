package appserver

import (
	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// threadStartConfig builds a [core.SessionConfiguration] from the negotiated
// connection state plus the thread/start parameters. It applies the per-field
// overrides codex honors at thread start (model, provider, cwd, approval policy,
// base/developer instructions) over the assembly defaults.
//
// This is a reduced port of the Rust thread_start config derivation: it carries
// the effective values the turn-running path reads and leaves richer derivations
// (permission profiles, sandbox policy resolution, config-layer stacks) to the
// config/permissions area agents.
func (p *Processor) threadStartConfig(conn *Conn, params *appserverproto.ThreadStartParams) core.SessionConfiguration {
	cfg := p.baseConfig()

	if params.Model != nil {
		cfg.CollaborationMode.Settings.Model = *params.Model
	}
	if params.ModelProvider != nil {
		cfg.ProviderID = *params.ModelProvider
	}
	if params.Cwd != nil {
		cfg.Cwd = *params.Cwd
	}
	if params.RuntimeWorkspaceRoots != nil {
		cfg.WorkspaceRoots = append([]string(nil), (*params.RuntimeWorkspaceRoots)...)
	}
	if params.BaseInstructions != nil {
		cfg.BaseInstructions = *params.BaseInstructions
	}
	if params.DeveloperInstructions != nil {
		v := *params.DeveloperInstructions
		cfg.DeveloperInstructions = &v
	}
	if params.Personality != nil {
		v := *params.Personality
		cfg.Personality = &v
	}
	if params.ServiceTier != nil && params.ServiceTier.Present && params.ServiceTier.Set {
		v := params.ServiceTier.Value
		cfg.ServiceTier = &v
	}
	if name, version := conn.clientInfo(); name != nil {
		cfg.AppServerClientName = name
		cfg.AppServerClientVersion = version
	}
	return cfg
}

// resumeForkConfig builds the [core.SessionConfiguration] for thread/resume and
// thread/fork from their shared override fields. Both request shapes carry the
// same model/provider/cwd/instruction overrides.
func (p *Processor) resumeForkConfig(
	conn *Conn,
	model *string,
	provider *string,
	cwd *string,
	roots *[]string,
	baseInstructions *string,
	developerInstructions *string,
) core.SessionConfiguration {
	cfg := p.baseConfig()
	if model != nil {
		cfg.CollaborationMode.Settings.Model = *model
	}
	if provider != nil {
		cfg.ProviderID = *provider
	}
	if cwd != nil {
		cfg.Cwd = *cwd
	}
	if roots != nil {
		cfg.WorkspaceRoots = append([]string(nil), (*roots)...)
	}
	if baseInstructions != nil {
		cfg.BaseInstructions = *baseInstructions
	}
	if developerInstructions != nil {
		v := *developerInstructions
		cfg.DeveloperInstructions = &v
	}
	if name, version := conn.clientInfo(); name != nil {
		cfg.AppServerClientName = name
		cfg.AppServerClientVersion = version
	}
	return cfg
}

// baseConfig returns the assembly's default session configuration. It is the
// starting point onto which per-request overrides are applied.
//
// ApprovalPolicy and ApprovalsReviewer are seeded with valid codex defaults
// (on-request / user) because the SessionConfigured event the engine emits at
// spawn embeds them; an empty AskForApproval would fail to serialize.
func (p *Processor) baseConfig() core.SessionConfiguration {
	approval := p.defaults.ApprovalPolicy
	if approval.Kind == "" {
		approval = protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest}
	}
	return core.SessionConfiguration{
		ProviderID: p.defaults.ProviderID,
		CollaborationMode: protocol.CollaborationMode{
			Settings: protocol.Settings{Model: p.defaults.Model},
		},
		Cwd:                       p.defaults.Cwd,
		CodexHome:                 p.assembly.CodexHome,
		BaseInstructions:          p.defaults.BaseInstructions,
		ApprovalPolicy:            approval,
		ApprovalsReviewer:         protocol.ApprovalsReviewerUser,
		IncludeEnvironmentContext: p.defaults.IncludeEnvironmentContext,
		SandboxMode:               p.defaults.SandboxMode,
		NetworkAccessEnabled:      p.defaults.NetworkAccessEnabled,
	}
}

// userInputsToCore maps the v2 app-server [appserverproto.UserInput] items to
// core [protocol.UserInput] items, the input the engine submits as a UserInput
// op. It mirrors the Rust V2UserInput::into_core conversion.
func userInputsToCore(items []appserverproto.UserInput) []protocol.UserInput {
	out := make([]protocol.UserInput, 0, len(items))
	for _, it := range items {
		out = append(out, userInputToCore(it))
	}
	return out
}

// userInputToCore maps a single v2 UserInput to its core equivalent.
func userInputToCore(it appserverproto.UserInput) protocol.UserInput {
	switch it.Kind {
	case appserverproto.UserInputKindText:
		return protocol.UserInput{Type: protocol.UserInputKindText, Text: it.Text}
	case appserverproto.UserInputKindImage:
		return protocol.UserInput{Type: protocol.UserInputKindImage, ImageURL: it.URL, Detail: it.Detail}
	case appserverproto.UserInputKindLocalImage:
		return protocol.UserInput{Type: protocol.UserInputKindLocalImage, Path: it.Path, Detail: it.Detail}
	case appserverproto.UserInputKindSkill:
		return protocol.UserInput{Type: protocol.UserInputKindSkill, Name: it.Name, Path: it.Path}
	case appserverproto.UserInputKindMention:
		return protocol.UserInput{Type: protocol.UserInputKindMention, Name: it.Name, MentionPath: it.Path}
	default:
		// Forward-compatible: degrade unknown variants to their text payload.
		return protocol.UserInput{Type: protocol.UserInputKindText, Text: it.Text}
	}
}
