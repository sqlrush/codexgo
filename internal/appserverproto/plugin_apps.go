package appserverproto

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// This file ports the v2 plugin, apps, skills, hooks, marketplace,
// collaborationMode, and experimentalFeature surfaces, plus the init()
// registration of every method and notification owned by this area.
//
// Reference: app-server-protocol/src/protocol/v2/{plugin,apps,hook,
// collaboration_mode,experimental_feature}.rs.

// -----------------------------------------------------------------------------
// hook.rs: enums referenced by plugin.rs and hooks/list
// -----------------------------------------------------------------------------

// HookEventName is the camelCase v2 hook event name. Rust: v2_enum_from_core!
// over CoreHookEventName (camelCase).
type HookEventName string

const (
	HookEventNamePreToolUse        HookEventName = "preToolUse"
	HookEventNamePermissionRequest HookEventName = "permissionRequest"
	HookEventNamePostToolUse       HookEventName = "postToolUse"
	HookEventNamePreCompact        HookEventName = "preCompact"
	HookEventNamePostCompact       HookEventName = "postCompact"
	HookEventNameSessionStart      HookEventName = "sessionStart"
	HookEventNameUserPromptSubmit  HookEventName = "userPromptSubmit"
	HookEventNameSubagentStart     HookEventName = "subagentStart"
	HookEventNameSubagentStop      HookEventName = "subagentStop"
	HookEventNameStop              HookEventName = "stop"
)

// HookHandlerType is the camelCase v2 hook handler type. Rust:
// v2_enum_from_core! over CoreHookHandlerType (camelCase).
type HookHandlerType string

const (
	HookHandlerTypeCommand HookHandlerType = "command"
	HookHandlerTypePrompt  HookHandlerType = "prompt"
	HookHandlerTypeAgent   HookHandlerType = "agent"
)

// HookSource is the camelCase v2 hook source. Rust: v2_enum_from_core! over
// CoreHookSource (camelCase).
type HookSource string

const (
	HookSourceSystem                  HookSource = "system"
	HookSourceUser                    HookSource = "user"
	HookSourceProject                 HookSource = "project"
	HookSourceMdm                     HookSource = "mdm"
	HookSourceSessionFlags            HookSource = "sessionFlags"
	HookSourcePlugin                  HookSource = "plugin"
	HookSourceCloudRequirements       HookSource = "cloudRequirements"
	HookSourceLegacyManagedConfigFile HookSource = "legacyManagedConfigFile"
	HookSourceLegacyManagedConfigMdm  HookSource = "legacyManagedConfigMdm"
	HookSourceUnknown                 HookSource = "unknown"
)

// HookTrustStatus is the camelCase v2 hook trust status. Rust:
// v2_enum_from_core! over CoreHookTrustStatus (camelCase).
type HookTrustStatus string

const (
	HookTrustStatusManaged   HookTrustStatus = "managed"
	HookTrustStatusUntrusted HookTrustStatus = "untrusted"
	HookTrustStatusTrusted   HookTrustStatus = "trusted"
	HookTrustStatusModified  HookTrustStatus = "modified"
)

// -----------------------------------------------------------------------------
// apps.rs: app/list
// -----------------------------------------------------------------------------

// AppsListParams requests a page of apps. Rust: camelCase; the Option fields are
// omitted when absent (ts optional = nullable means no skip on the Rust side,
// but the macro-defined serde keeps them as plain Option, always emitted).
// forceRefetch defaults to false and is omitted when false.
type AppsListParams struct {
	Cursor       *string `json:"cursor"`
	Limit        *uint32 `json:"limit"`
	ThreadID     *string `json:"threadId"`
	ForceRefetch bool    `json:"forceRefetch,omitempty"`
}

// AppBranding carries app branding metadata. Rust: camelCase.
type AppBranding struct {
	Category          *string `json:"category"`
	Developer         *string `json:"developer"`
	Website           *string `json:"website"`
	PrivacyPolicy     *string `json:"privacyPolicy"`
	TermsOfService    *string `json:"termsOfService"`
	IsDiscoverableApp bool    `json:"isDiscoverableApp"`
}

// AppReview carries an app review status. Rust: camelCase.
type AppReview struct {
	Status string `json:"status"`
}

// AppScreenshot describes one app screenshot. Rust: camelCase; fileId and
// userPrompt accept snake_case aliases on input (file_id, user_prompt).
type AppScreenshot struct {
	URL        *string `json:"url"`
	FileID     *string `json:"fileId"`
	UserPrompt string  `json:"userPrompt"`
}

// UnmarshalJSON accepts the camelCase keys plus the snake_case input aliases.
func (a *AppScreenshot) UnmarshalJSON(data []byte) error {
	var w struct {
		URL          *string `json:"url"`
		FileID       *string `json:"fileId"`
		FileIDAlias  *string `json:"file_id"`
		UserPrompt   *string `json:"userPrompt"`
		UserPromptAl *string `json:"user_prompt"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return fmt.Errorf("appserverproto: decode AppScreenshot: %w", err)
	}
	a.URL = w.URL
	a.FileID = w.FileID
	if a.FileID == nil {
		a.FileID = w.FileIDAlias
	}
	switch {
	case w.UserPrompt != nil:
		a.UserPrompt = *w.UserPrompt
	case w.UserPromptAl != nil:
		a.UserPrompt = *w.UserPromptAl
	}
	return nil
}

// AppMetadata carries extended app metadata. Rust: camelCase.
type AppMetadata struct {
	Review                     *AppReview       `json:"review"`
	Categories                 *[]string        `json:"categories"`
	SubCategories              *[]string        `json:"subCategories"`
	SeoDescription             *string          `json:"seoDescription"`
	Screenshots                *[]AppScreenshot `json:"screenshots"`
	Developer                  *string          `json:"developer"`
	Version                    *string          `json:"version"`
	VersionID                  *string          `json:"versionId"`
	VersionNotes               *string          `json:"versionNotes"`
	FirstPartyType             *string          `json:"firstPartyType"`
	FirstPartyRequiresInstall  *bool            `json:"firstPartyRequiresInstall"`
	ShowInComposerWhenUnlinked *bool            `json:"showInComposerWhenUnlinked"`
}

// AppInfo describes a listable app. Rust: camelCase; isAccessible defaults to
// false, isEnabled defaults to true (default_enabled), pluginDisplayNames
// defaults to empty - all always emitted.
type AppInfo struct {
	ID                  string             `json:"id"`
	Name                string             `json:"name"`
	Description         *string            `json:"description"`
	LogoURL             *string            `json:"logoUrl"`
	LogoURLDark         *string            `json:"logoUrlDark"`
	DistributionChannel *string            `json:"distributionChannel"`
	Branding            *AppBranding       `json:"branding"`
	AppMetadata         *AppMetadata       `json:"appMetadata"`
	Labels              *map[string]string `json:"labels"`
	InstallURL          *string            `json:"installUrl"`
	IsAccessible        bool               `json:"isAccessible"`
	IsEnabled           bool               `json:"isEnabled"`
	PluginDisplayNames  []string           `json:"pluginDisplayNames"`
}

// MarshalJSON ensures pluginDisplayNames emits an array, never null.
func (a AppInfo) MarshalJSON() ([]byte, error) {
	type alias AppInfo
	out := alias(a)
	if out.PluginDisplayNames == nil {
		out.PluginDisplayNames = []string{}
	}
	return json.Marshal(out)
}

// AppSummary is the compact app summary used by plugin responses. Rust:
// camelCase.
type AppSummary struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	InstallURL  *string `json:"installUrl"`
	NeedsAuth   bool    `json:"needsAuth"`
}

// AppsListResponse is a page of apps plus a next-page cursor. Rust: camelCase.
type AppsListResponse struct {
	Data       []AppInfo `json:"data"`
	NextCursor *string   `json:"nextCursor"`
}

// AppListUpdatedNotification reports a changed app list. Rust: camelCase.
type AppListUpdatedNotification struct {
	Data []AppInfo `json:"data"`
}

// -----------------------------------------------------------------------------
// collaboration_mode.rs: collaborationMode/list
// -----------------------------------------------------------------------------

// CollaborationModeListParams is an empty request. Rust: camelCase, no fields.
type CollaborationModeListParams struct{}

// CollaborationModeMaskV2 is the v2 collaboration mode preset mask. The
// reasoning_effort field is Option<Option<ReasoningEffort>> in Rust; it is
// always emitted (the outer Option has no skip), so a DoubleOption value field
// models the absent/null/value states.
//
// Distinct from the internal protocol CollaborationModeMask: this is the v2
// API mirror whose reasoning_effort key is snake_case (explicit rename) while
// the other keys are camelCase.
type CollaborationModeMaskV2 struct {
	Name            string                                  `json:"name"`
	Mode            *protocol.ModeKind                      `json:"mode"`
	Model           *string                                 `json:"model"`
	ReasoningEffort DoubleOption[*protocol.ReasoningEffort] `json:"reasoning_effort"`
}

// CollaborationModeListResponse is the collaboration mode presets response.
// Rust: camelCase.
type CollaborationModeListResponse struct {
	Data []CollaborationModeMaskV2 `json:"data"`
}

// -----------------------------------------------------------------------------
// experimental_feature.rs: experimentalFeature/list
// -----------------------------------------------------------------------------

// ExperimentalFeatureListParams requests a page of features. Rust: camelCase;
// all Option fields always emitted (null when absent).
type ExperimentalFeatureListParams struct {
	Cursor   *string `json:"cursor"`
	Limit    *uint32 `json:"limit"`
	ThreadID *string `json:"threadId"`
}

// ExperimentalFeatureStage is a feature lifecycle stage. Rust: camelCase.
type ExperimentalFeatureStage string

const (
	ExperimentalFeatureStageBeta             ExperimentalFeatureStage = "beta"
	ExperimentalFeatureStageUnderDevelopment ExperimentalFeatureStage = "underDevelopment"
	ExperimentalFeatureStageStable           ExperimentalFeatureStage = "stable"
	ExperimentalFeatureStageDeprecated       ExperimentalFeatureStage = "deprecated"
	ExperimentalFeatureStageRemoved          ExperimentalFeatureStage = "removed"
)

// ExperimentalFeature describes one experimental feature flag. Rust: camelCase.
type ExperimentalFeature struct {
	Name           string                   `json:"name"`
	Stage          ExperimentalFeatureStage `json:"stage"`
	DisplayName    *string                  `json:"displayName"`
	Description    *string                  `json:"description"`
	Announcement   *string                  `json:"announcement"`
	Enabled        bool                     `json:"enabled"`
	DefaultEnabled bool                     `json:"defaultEnabled"`
}

// ExperimentalFeatureListResponse is a page of features plus a cursor. Rust:
// camelCase.
type ExperimentalFeatureListResponse struct {
	Data       []ExperimentalFeature `json:"data"`
	NextCursor *string               `json:"nextCursor"`
}

// -----------------------------------------------------------------------------
// plugin.rs: skills/list, hooks/list, marketplace/*, plugin/*
// -----------------------------------------------------------------------------

// SkillsListParams requests skills for working directories. Rust: camelCase;
// cwds is omitted when empty, forceReload omitted when false.
type SkillsListParams struct {
	Cwds        []string `json:"cwds,omitempty"`
	ForceReload bool     `json:"forceReload,omitempty"`
}

// SkillScope is the v2 skill scope. Rust: snake_case (lowercase variants).
type SkillScope string

const (
	SkillScopeUser   SkillScope = "user"
	SkillScopeRepo   SkillScope = "repo"
	SkillScopeSystem SkillScope = "system"
	SkillScopeAdmin  SkillScope = "admin"
)

// SkillToolDependency describes a skill tool dependency. Rust: camelCase; the
// "type" field is explicitly renamed; the trailing Option fields are omitted
// when absent (skip_serializing_if).
type SkillToolDependency struct {
	Type        string  `json:"type"`
	Value       string  `json:"value"`
	Description *string `json:"description,omitempty"`
	Transport   *string `json:"transport,omitempty"`
	Command     *string `json:"command,omitempty"`
	URL         *string `json:"url,omitempty"`
}

// SkillDependencies lists a skill's tool dependencies. Rust: camelCase.
type SkillDependencies struct {
	Tools []SkillToolDependency `json:"tools"`
}

// SkillInterface carries presentation metadata for a skill. Rust: camelCase;
// every field is `#[ts(optional)]` so all are always emitted (null when absent).
type SkillInterface struct {
	DisplayName      *string                `json:"displayName"`
	ShortDescription *string                `json:"shortDescription"`
	IconSmall        *protocol.AbsolutePath `json:"iconSmall"`
	IconLarge        *protocol.AbsolutePath `json:"iconLarge"`
	BrandColor       *string                `json:"brandColor"`
	DefaultPrompt    *string                `json:"defaultPrompt"`
}

// SkillMetadata describes one skill. Rust: camelCase; shortDescription,
// interface, dependencies are omitted when absent (skip_serializing_if).
type SkillMetadata struct {
	Name             string                `json:"name"`
	Description      string                `json:"description"`
	ShortDescription *string               `json:"shortDescription,omitempty"`
	Interface        *SkillInterface       `json:"interface,omitempty"`
	Dependencies     *SkillDependencies    `json:"dependencies,omitempty"`
	Path             protocol.AbsolutePath `json:"path"`
	Scope            SkillScope            `json:"scope"`
	Enabled          bool                  `json:"enabled"`
}

// SkillErrorInfo reports a skill-loading error. Rust: camelCase.
type SkillErrorInfo struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// SkillsListEntry groups skills discovered for one cwd. Rust: camelCase.
type SkillsListEntry struct {
	Cwd    string           `json:"cwd"`
	Skills []SkillMetadata  `json:"skills"`
	Errors []SkillErrorInfo `json:"errors"`
}

// SkillsListResponse returns skill entries per cwd. Rust: camelCase.
type SkillsListResponse struct {
	Data []SkillsListEntry `json:"data"`
}

// SkillsChangedNotification signals that watched skill files changed. Rust:
// camelCase, no fields.
type SkillsChangedNotification struct{}

// HooksListParams requests hooks for working directories. Rust: camelCase; cwds
// is omitted when empty.
type HooksListParams struct {
	Cwds []string `json:"cwds,omitempty"`
}

// HookMetadata describes one configured hook. Rust: camelCase.
type HookMetadata struct {
	Key           string                `json:"key"`
	EventName     HookEventName         `json:"eventName"`
	HandlerType   HookHandlerType       `json:"handlerType"`
	Matcher       *string               `json:"matcher"`
	Command       *string               `json:"command"`
	TimeoutSec    uint64                `json:"timeoutSec"`
	StatusMessage *string               `json:"statusMessage"`
	SourcePath    protocol.AbsolutePath `json:"sourcePath"`
	Source        HookSource            `json:"source"`
	PluginID      *string               `json:"pluginId"`
	DisplayOrder  int64                 `json:"displayOrder"`
	Enabled       bool                  `json:"enabled"`
	IsManaged     bool                  `json:"isManaged"`
	CurrentHash   string                `json:"currentHash"`
	TrustStatus   HookTrustStatus       `json:"trustStatus"`
}

// HookErrorInfo reports a hook-loading error. Rust: camelCase.
type HookErrorInfo struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// HooksListEntry groups hooks discovered for one cwd. Rust: camelCase.
type HooksListEntry struct {
	Cwd      string          `json:"cwd"`
	Hooks    []HookMetadata  `json:"hooks"`
	Warnings []string        `json:"warnings"`
	Errors   []HookErrorInfo `json:"errors"`
}

// HooksListResponse returns hook entries per cwd. Rust: camelCase.
type HooksListResponse struct {
	Data []HooksListEntry `json:"data"`
}

// MarketplaceAddParams adds a plugin marketplace. Rust: camelCase; refName and
// sparsePaths always emitted (null when absent).
type MarketplaceAddParams struct {
	Source      string    `json:"source"`
	RefName     *string   `json:"refName"`
	SparsePaths *[]string `json:"sparsePaths"`
}

// MarketplaceAddResponse reports a marketplace add. Rust: camelCase.
type MarketplaceAddResponse struct {
	MarketplaceName string                `json:"marketplaceName"`
	InstalledRoot   protocol.AbsolutePath `json:"installedRoot"`
	AlreadyAdded    bool                  `json:"alreadyAdded"`
}

// MarketplaceRemoveParams removes a marketplace by name. Rust: camelCase.
type MarketplaceRemoveParams struct {
	MarketplaceName string `json:"marketplaceName"`
}

// MarketplaceRemoveResponse reports a marketplace removal. Rust: camelCase.
type MarketplaceRemoveResponse struct {
	MarketplaceName string                 `json:"marketplaceName"`
	InstalledRoot   *protocol.AbsolutePath `json:"installedRoot"`
}

// MarketplaceUpgradeParams upgrades one or all marketplaces. Rust: camelCase;
// marketplaceName always emitted (null when absent).
type MarketplaceUpgradeParams struct {
	MarketplaceName *string `json:"marketplaceName"`
}

// MarketplaceUpgradeErrorInfo reports a per-marketplace upgrade error. Rust:
// camelCase.
type MarketplaceUpgradeErrorInfo struct {
	MarketplaceName string `json:"marketplaceName"`
	Message         string `json:"message"`
}

// MarketplaceUpgradeResponse reports upgrade results. Rust: camelCase.
type MarketplaceUpgradeResponse struct {
	SelectedMarketplaces []string                      `json:"selectedMarketplaces"`
	UpgradedRoots        []protocol.AbsolutePath       `json:"upgradedRoots"`
	Errors               []MarketplaceUpgradeErrorInfo `json:"errors"`
}

// PluginListMarketplaceKind filters marketplaces by kind. Rust: kebab-case
// renames.
type PluginListMarketplaceKind string

const (
	PluginListMarketplaceKindLocal              PluginListMarketplaceKind = "local"
	PluginListMarketplaceKindVertical           PluginListMarketplaceKind = "vertical"
	PluginListMarketplaceKindWorkspaceDirectory PluginListMarketplaceKind = "workspace-directory"
	PluginListMarketplaceKindSharedWithMe       PluginListMarketplaceKind = "shared-with-me"
)

// PluginListParams lists plugin marketplaces. Rust: camelCase; cwds and
// marketplaceKinds always emitted (null when absent).
type PluginListParams struct {
	Cwds             *[]protocol.AbsolutePath     `json:"cwds"`
	MarketplaceKinds *[]PluginListMarketplaceKind `json:"marketplaceKinds"`
}

// PluginInstalledParams lists installed plugins. Rust: camelCase; both Option
// fields always emitted (null when absent).
type PluginInstalledParams struct {
	Cwds                         *[]protocol.AbsolutePath `json:"cwds"`
	InstallSuggestionPluginNames *[]string                `json:"installSuggestionPluginNames"`
}

// MarketplaceLoadErrorInfo reports a marketplace load error. Rust: camelCase.
type MarketplaceLoadErrorInfo struct {
	MarketplacePath protocol.AbsolutePath `json:"marketplacePath"`
	Message         string                `json:"message"`
}

// PluginListResponse returns marketplaces and load errors. Rust: camelCase;
// the `#[serde(default)]` vecs are always emitted (empty arrays).
type PluginListResponse struct {
	Marketplaces          []PluginMarketplaceEntry   `json:"marketplaces"`
	MarketplaceLoadErrors []MarketplaceLoadErrorInfo `json:"marketplaceLoadErrors"`
	FeaturedPluginIDs     []string                   `json:"featuredPluginIds"`
}

// MarshalJSON ensures the default vecs emit arrays, never null.
func (p PluginListResponse) MarshalJSON() ([]byte, error) {
	type alias PluginListResponse
	out := alias(p)
	if out.Marketplaces == nil {
		out.Marketplaces = []PluginMarketplaceEntry{}
	}
	if out.MarketplaceLoadErrors == nil {
		out.MarketplaceLoadErrors = []MarketplaceLoadErrorInfo{}
	}
	if out.FeaturedPluginIDs == nil {
		out.FeaturedPluginIDs = []string{}
	}
	return json.Marshal(out)
}

// PluginInstalledResponse returns installed marketplaces and load errors. Rust:
// camelCase; the `#[serde(default)]` vec is always emitted (empty array).
type PluginInstalledResponse struct {
	Marketplaces          []PluginMarketplaceEntry   `json:"marketplaces"`
	MarketplaceLoadErrors []MarketplaceLoadErrorInfo `json:"marketplaceLoadErrors"`
}

// MarshalJSON ensures the default vec emits an array, never null.
func (p PluginInstalledResponse) MarshalJSON() ([]byte, error) {
	type alias PluginInstalledResponse
	out := alias(p)
	if out.Marketplaces == nil {
		out.Marketplaces = []PluginMarketplaceEntry{}
	}
	if out.MarketplaceLoadErrors == nil {
		out.MarketplaceLoadErrors = []MarketplaceLoadErrorInfo{}
	}
	return json.Marshal(out)
}

// PluginReadParams reads a plugin's detail. Rust: camelCase; marketplacePath and
// remoteMarketplaceName always emitted (null when absent).
type PluginReadParams struct {
	MarketplacePath       *protocol.AbsolutePath `json:"marketplacePath"`
	RemoteMarketplaceName *string                `json:"remoteMarketplaceName"`
	PluginName            string                 `json:"pluginName"`
}

// PluginReadResponse wraps a plugin detail. Rust: camelCase.
type PluginReadResponse struct {
	Plugin PluginDetail `json:"plugin"`
}

// PluginSkillReadParams reads a plugin skill body. Rust: camelCase.
type PluginSkillReadParams struct {
	RemoteMarketplaceName string `json:"remoteMarketplaceName"`
	RemotePluginID        string `json:"remotePluginId"`
	SkillName             string `json:"skillName"`
}

// PluginSkillReadResponse carries an optional skill body. Rust: camelCase.
type PluginSkillReadResponse struct {
	Contents *string `json:"contents"`
}

// PluginShareSaveParams shares a plugin. Rust: camelCase; the trailing Option
// fields always emitted (null when absent).
type PluginShareSaveParams struct {
	PluginPath      protocol.AbsolutePath       `json:"pluginPath"`
	RemotePluginID  *string                     `json:"remotePluginId"`
	Discoverability *PluginShareDiscoverability `json:"discoverability"`
	ShareTargets    *[]PluginShareTarget        `json:"shareTargets"`
}

// PluginShareSaveResponse reports a share save. Rust: camelCase.
type PluginShareSaveResponse struct {
	RemotePluginID string `json:"remotePluginId"`
	ShareURL       string `json:"shareUrl"`
}

// PluginShareUpdateTargetsParams updates share targets. Rust: camelCase.
type PluginShareUpdateTargetsParams struct {
	RemotePluginID  string                           `json:"remotePluginId"`
	Discoverability PluginShareUpdateDiscoverability `json:"discoverability"`
	ShareTargets    []PluginShareTarget              `json:"shareTargets"`
}

// PluginShareUpdateTargetsResponse reports updated principals. Rust: camelCase.
type PluginShareUpdateTargetsResponse struct {
	Principals      []PluginSharePrincipal     `json:"principals"`
	Discoverability PluginShareDiscoverability `json:"discoverability"`
}

// PluginShareListParams is an empty share-list request. Rust: camelCase.
type PluginShareListParams struct{}

// PluginShareListItem is one shared-plugin list entry. Rust: camelCase.
type PluginShareListItem struct {
	Plugin          PluginSummary          `json:"plugin"`
	LocalPluginPath *protocol.AbsolutePath `json:"localPluginPath"`
}

// PluginShareListResponse lists shared plugins. Rust: camelCase.
type PluginShareListResponse struct {
	Data []PluginShareListItem `json:"data"`
}

// PluginShareCheckoutParams checks out a shared plugin. Rust: camelCase.
type PluginShareCheckoutParams struct {
	RemotePluginID string `json:"remotePluginId"`
}

// PluginShareCheckoutResponse reports a share checkout. Rust: camelCase.
type PluginShareCheckoutResponse struct {
	RemotePluginID  string                `json:"remotePluginId"`
	PluginID        string                `json:"pluginId"`
	PluginName      string                `json:"pluginName"`
	PluginPath      protocol.AbsolutePath `json:"pluginPath"`
	MarketplaceName string                `json:"marketplaceName"`
	MarketplacePath protocol.AbsolutePath `json:"marketplacePath"`
	RemoteVersion   *string               `json:"remoteVersion"`
}

// PluginShareDeleteParams deletes a shared plugin. Rust: camelCase.
type PluginShareDeleteParams struct {
	RemotePluginID string `json:"remotePluginId"`
}

// PluginShareDeleteResponse is empty. Rust: camelCase.
type PluginShareDeleteResponse struct{}

// PluginShareDiscoverability is a plugin's share discoverability. Rust:
// UPPER_CASE renames.
type PluginShareDiscoverability string

const (
	PluginShareDiscoverabilityListed   PluginShareDiscoverability = "LISTED"
	PluginShareDiscoverabilityUnlisted PluginShareDiscoverability = "UNLISTED"
	PluginShareDiscoverabilityPrivate  PluginShareDiscoverability = "PRIVATE"
)

// PluginShareUpdateDiscoverability is the subset valid for share updates. Rust:
// UPPER_CASE renames.
type PluginShareUpdateDiscoverability string

const (
	PluginShareUpdateDiscoverabilityUnlisted PluginShareUpdateDiscoverability = "UNLISTED"
	PluginShareUpdateDiscoverabilityPrivate  PluginShareUpdateDiscoverability = "PRIVATE"
)

// PluginSharePrincipalType is a share principal type. Rust: lowercase renames.
type PluginSharePrincipalType string

const (
	PluginSharePrincipalTypeUser      PluginSharePrincipalType = "user"
	PluginSharePrincipalTypeGroup     PluginSharePrincipalType = "group"
	PluginSharePrincipalTypeWorkspace PluginSharePrincipalType = "workspace"
)

// PluginShareTargetRole is a share target role. Rust: rename_all = "lowercase".
type PluginShareTargetRole string

const (
	PluginShareTargetRoleReader PluginShareTargetRole = "reader"
	PluginShareTargetRoleEditor PluginShareTargetRole = "editor"
)

// PluginSharePrincipalRole is a share principal role. Rust: rename_all =
// "lowercase".
type PluginSharePrincipalRole string

const (
	PluginSharePrincipalRoleReader PluginSharePrincipalRole = "reader"
	PluginSharePrincipalRoleEditor PluginSharePrincipalRole = "editor"
	PluginSharePrincipalRoleOwner  PluginSharePrincipalRole = "owner"
)

// PluginShareTarget targets a principal for sharing. Rust: camelCase.
type PluginShareTarget struct {
	PrincipalType PluginSharePrincipalType `json:"principalType"`
	PrincipalID   string                   `json:"principalId"`
	Role          PluginShareTargetRole    `json:"role"`
}

// PluginSharePrincipal is a resolved share principal. Rust: camelCase.
type PluginSharePrincipal struct {
	PrincipalType PluginSharePrincipalType `json:"principalType"`
	PrincipalID   string                   `json:"principalId"`
	Role          PluginSharePrincipalRole `json:"role"`
	Name          string                   `json:"name"`
}

// PluginInstallPolicy describes how a plugin may be installed. Rust: UPPER_CASE
// renames.
type PluginInstallPolicy string

const (
	PluginInstallPolicyNotAvailable       PluginInstallPolicy = "NOT_AVAILABLE"
	PluginInstallPolicyAvailable          PluginInstallPolicy = "AVAILABLE"
	PluginInstallPolicyInstalledByDefault PluginInstallPolicy = "INSTALLED_BY_DEFAULT"
)

// PluginAuthPolicy describes when a plugin requires auth. Rust: UPPER_CASE
// renames.
type PluginAuthPolicy string

const (
	PluginAuthPolicyOnInstall PluginAuthPolicy = "ON_INSTALL"
	PluginAuthPolicyOnUse     PluginAuthPolicy = "ON_USE"
)

// PluginAvailability is a plugin's availability state. Rust: UPPER_CASE renames;
// the default is Available, and "ENABLED" is accepted as an input alias.
type PluginAvailability string

const (
	PluginAvailabilityAvailable       PluginAvailability = "AVAILABLE"
	PluginAvailabilityDisabledByAdmin PluginAvailability = "DISABLED_BY_ADMIN"
)

// NormalizePluginAvailability maps the accepted "ENABLED" alias to "AVAILABLE".
func NormalizePluginAvailability(v string) PluginAvailability {
	switch v {
	case "ENABLED", "AVAILABLE":
		return PluginAvailabilityAvailable
	default:
		return PluginAvailability(v)
	}
}

// PluginShareContext carries remote-sharing context for a plugin. Rust:
// camelCase; remoteVersion is `#[serde(default)]` (always emitted, null).
type PluginShareContext struct {
	RemotePluginID       string                      `json:"remotePluginId"`
	RemoteVersion        *string                     `json:"remoteVersion"`
	Discoverability      *PluginShareDiscoverability `json:"discoverability"`
	ShareURL             *string                     `json:"shareUrl"`
	CreatorAccountUserID *string                     `json:"creatorAccountUserId"`
	CreatorName          *string                     `json:"creatorName"`
	SharePrincipals      *[]PluginSharePrincipal     `json:"sharePrincipals"`
}

// PluginInterface carries presentation metadata for a plugin. Rust: camelCase;
// all fields always emitted (the vecs are always serialized arrays).
type PluginInterface struct {
	DisplayName       *string                 `json:"displayName"`
	ShortDescription  *string                 `json:"shortDescription"`
	LongDescription   *string                 `json:"longDescription"`
	DeveloperName     *string                 `json:"developerName"`
	Category          *string                 `json:"category"`
	Capabilities      []string                `json:"capabilities"`
	WebsiteURL        *string                 `json:"websiteUrl"`
	PrivacyPolicyURL  *string                 `json:"privacyPolicyUrl"`
	TermsOfServiceURL *string                 `json:"termsOfServiceUrl"`
	DefaultPrompt     *[]string               `json:"defaultPrompt"`
	BrandColor        *string                 `json:"brandColor"`
	ComposerIcon      *protocol.AbsolutePath  `json:"composerIcon"`
	ComposerIconURL   *string                 `json:"composerIconUrl"`
	Logo              *protocol.AbsolutePath  `json:"logo"`
	LogoURL           *string                 `json:"logoUrl"`
	Screenshots       []protocol.AbsolutePath `json:"screenshots"`
	ScreenshotURLs    []string                `json:"screenshotUrls"`
}

// MarshalJSON ensures the always-serialized vecs emit arrays, never null.
func (p PluginInterface) MarshalJSON() ([]byte, error) {
	type alias PluginInterface
	out := alias(p)
	if out.Capabilities == nil {
		out.Capabilities = []string{}
	}
	if out.Screenshots == nil {
		out.Screenshots = []protocol.AbsolutePath{}
	}
	if out.ScreenshotURLs == nil {
		out.ScreenshotURLs = []string{}
	}
	return json.Marshal(out)
}

// PluginSourceKind discriminates PluginSource variants. Rust: camelCase tags.
type PluginSourceKind string

const (
	PluginSourceLocal  PluginSourceKind = "local"
	PluginSourceGit    PluginSourceKind = "git"
	PluginSourceRemote PluginSourceKind = "remote"
)

// PluginSource is an internally-tagged enum (tag = "type", camelCase) describing
// where a plugin comes from. Only the active variant's fields are populated.
type PluginSource struct {
	Kind PluginSourceKind

	// Path applies to the Local variant.
	Path *protocol.AbsolutePath
	// URL, GitPath, RefName, and Sha apply to the Git variant.
	URL     *string
	GitPath *string
	RefName *string
	Sha     *string
}

// MarshalJSON encodes the internally-tagged PluginSource.
func (p PluginSource) MarshalJSON() ([]byte, error) {
	switch p.Kind {
	case PluginSourceLocal:
		return json.Marshal(struct {
			Type PluginSourceKind       `json:"type"`
			Path *protocol.AbsolutePath `json:"path"`
		}{Type: p.Kind, Path: p.Path})
	case PluginSourceGit:
		return json.Marshal(struct {
			Type    PluginSourceKind `json:"type"`
			URL     *string          `json:"url"`
			Path    *string          `json:"path"`
			RefName *string          `json:"refName"`
			Sha     *string          `json:"sha"`
		}{Type: p.Kind, URL: p.URL, Path: p.GitPath, RefName: p.RefName, Sha: p.Sha})
	case PluginSourceRemote:
		return json.Marshal(struct {
			Type PluginSourceKind `json:"type"`
		}{Type: p.Kind})
	case "":
		return nil, fmt.Errorf("appserverproto: PluginSource has empty kind")
	default:
		return nil, fmt.Errorf("appserverproto: unknown PluginSource kind: %q", p.Kind)
	}
}

// UnmarshalJSON decodes the internally-tagged PluginSource.
func (p *PluginSource) UnmarshalJSON(data []byte) error {
	var w struct {
		Type    PluginSourceKind `json:"type"`
		Path    json.RawMessage  `json:"path"`
		URL     *string          `json:"url"`
		RefName *string          `json:"refName"`
		Sha     *string          `json:"sha"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return fmt.Errorf("appserverproto: decode PluginSource: %w", err)
	}
	switch w.Type {
	case PluginSourceLocal:
		var path protocol.AbsolutePath
		if len(w.Path) > 0 && string(w.Path) != "null" {
			if err := json.Unmarshal(w.Path, &path); err != nil {
				return fmt.Errorf("appserverproto: decode PluginSource.path: %w", err)
			}
		}
		*p = PluginSource{Kind: w.Type, Path: &path}
	case PluginSourceGit:
		var gitPath *string
		if len(w.Path) > 0 && string(w.Path) != "null" {
			var gp string
			if err := json.Unmarshal(w.Path, &gp); err != nil {
				return fmt.Errorf("appserverproto: decode PluginSource.path: %w", err)
			}
			gitPath = &gp
		}
		*p = PluginSource{Kind: w.Type, URL: w.URL, GitPath: gitPath, RefName: w.RefName, Sha: w.Sha}
	case PluginSourceRemote:
		*p = PluginSource{Kind: w.Type}
	case "":
		return fmt.Errorf("appserverproto: PluginSource missing type")
	default:
		return fmt.Errorf("appserverproto: unknown PluginSource type: %q", w.Type)
	}
	return nil
}

// PluginSummary describes a listable plugin. Rust: camelCase; localVersion and
// availability are `#[serde(default)]` (always emitted), keywords always an array.
type PluginSummary struct {
	ID             string              `json:"id"`
	RemotePluginID *string             `json:"remotePluginId"`
	LocalVersion   *string             `json:"localVersion"`
	Name           string              `json:"name"`
	ShareContext   *PluginShareContext `json:"shareContext"`
	Source         PluginSource        `json:"source"`
	Installed      bool                `json:"installed"`
	Enabled        bool                `json:"enabled"`
	InstallPolicy  PluginInstallPolicy `json:"installPolicy"`
	AuthPolicy     PluginAuthPolicy    `json:"authPolicy"`
	Availability   PluginAvailability  `json:"availability"`
	Interface      *PluginInterface    `json:"interface"`
	Keywords       []string            `json:"keywords"`
}

// MarshalJSON ensures keywords emits an array, never null.
func (p PluginSummary) MarshalJSON() ([]byte, error) {
	type alias PluginSummary
	out := alias(p)
	if out.Keywords == nil {
		out.Keywords = []string{}
	}
	return json.Marshal(out)
}

// MarketplaceInterface carries presentation metadata for a marketplace. Rust:
// camelCase.
type MarketplaceInterface struct {
	DisplayName *string `json:"displayName"`
}

// PluginMarketplaceEntry describes one marketplace and its plugins. Rust:
// camelCase.
type PluginMarketplaceEntry struct {
	Name      string                 `json:"name"`
	Path      *protocol.AbsolutePath `json:"path"`
	Interface *MarketplaceInterface  `json:"interface"`
	Plugins   []PluginSummary        `json:"plugins"`
}

// SkillSummary is the compact skill summary used in plugin details. Rust:
// camelCase.
type SkillSummary struct {
	Name             string                 `json:"name"`
	Description      string                 `json:"description"`
	ShortDescription *string                `json:"shortDescription"`
	Interface        *SkillInterface        `json:"interface"`
	Path             *protocol.AbsolutePath `json:"path"`
	Enabled          bool                   `json:"enabled"`
}

// PluginHookSummary is the compact hook summary used in plugin details. Rust:
// camelCase.
type PluginHookSummary struct {
	Key       string        `json:"key"`
	EventName HookEventName `json:"eventName"`
}

// PluginDetail describes a single plugin in full. Rust: camelCase.
type PluginDetail struct {
	MarketplaceName string                 `json:"marketplaceName"`
	MarketplacePath *protocol.AbsolutePath `json:"marketplacePath"`
	Summary         PluginSummary          `json:"summary"`
	Description     *string                `json:"description"`
	Skills          []SkillSummary         `json:"skills"`
	Hooks           []PluginHookSummary    `json:"hooks"`
	Apps            []AppSummary           `json:"apps"`
	McpServers      []string               `json:"mcpServers"`
}

// PluginInstallParams installs a plugin. Rust: camelCase; marketplacePath and
// remoteMarketplaceName always emitted (null when absent).
type PluginInstallParams struct {
	MarketplacePath       *protocol.AbsolutePath `json:"marketplacePath"`
	RemoteMarketplaceName *string                `json:"remoteMarketplaceName"`
	PluginName            string                 `json:"pluginName"`
}

// PluginInstallResponse reports a plugin install. Rust: camelCase.
type PluginInstallResponse struct {
	AuthPolicy      PluginAuthPolicy `json:"authPolicy"`
	AppsNeedingAuth []AppSummary     `json:"appsNeedingAuth"`
}

// PluginUninstallParams uninstalls a plugin by id. Rust: camelCase.
type PluginUninstallParams struct {
	PluginID string `json:"pluginId"`
}

// PluginUninstallResponse is empty. Rust: camelCase.
type PluginUninstallResponse struct{}

// -----------------------------------------------------------------------------
// Registration
// -----------------------------------------------------------------------------

func init() {
	// config/*
	Register(MethodSpec{
		Method:    "config/read",
		NewParams: func() any { return new(ConfigReadParams) },
		NewResult: func() any { return new(ConfigReadResponse) },
	})
	Register(MethodSpec{
		Method:    "config/batchWrite",
		NewParams: func() any { return new(ConfigBatchWriteParams) },
		NewResult: func() any { return new(ConfigWriteResponse) },
	})

	// model/list
	Register(MethodSpec{
		Method:    "model/list",
		NewParams: func() any { return new(ModelListParams) },
		NewResult: func() any { return new(ModelListResponse) },
	})

	// app/list
	Register(MethodSpec{
		Method:    "app/list",
		NewParams: func() any { return new(AppsListParams) },
		NewResult: func() any { return new(AppsListResponse) },
	})

	// skills/list, hooks/list
	Register(MethodSpec{
		Method:    "skills/list",
		NewParams: func() any { return new(SkillsListParams) },
		NewResult: func() any { return new(SkillsListResponse) },
	})
	Register(MethodSpec{
		Method:    "hooks/list",
		NewParams: func() any { return new(HooksListParams) },
		NewResult: func() any { return new(HooksListResponse) },
	})

	// marketplace/*
	Register(MethodSpec{
		Method:    "marketplace/add",
		NewParams: func() any { return new(MarketplaceAddParams) },
		NewResult: func() any { return new(MarketplaceAddResponse) },
	})
	Register(MethodSpec{
		Method:    "marketplace/remove",
		NewParams: func() any { return new(MarketplaceRemoveParams) },
		NewResult: func() any { return new(MarketplaceRemoveResponse) },
	})
	Register(MethodSpec{
		Method:    "marketplace/upgrade",
		NewParams: func() any { return new(MarketplaceUpgradeParams) },
		NewResult: func() any { return new(MarketplaceUpgradeResponse) },
	})

	// plugin/*
	Register(MethodSpec{
		Method:    "plugin/list",
		NewParams: func() any { return new(PluginListParams) },
		NewResult: func() any { return new(PluginListResponse) },
	})
	Register(MethodSpec{
		Method:    "plugin/installed",
		NewParams: func() any { return new(PluginInstalledParams) },
		NewResult: func() any { return new(PluginInstalledResponse) },
	})
	Register(MethodSpec{
		Method:    "plugin/read",
		NewParams: func() any { return new(PluginReadParams) },
		NewResult: func() any { return new(PluginReadResponse) },
	})
	Register(MethodSpec{
		Method:    "plugin/skill/read",
		NewParams: func() any { return new(PluginSkillReadParams) },
		NewResult: func() any { return new(PluginSkillReadResponse) },
	})
	Register(MethodSpec{
		Method:    "plugin/share/save",
		NewParams: func() any { return new(PluginShareSaveParams) },
		NewResult: func() any { return new(PluginShareSaveResponse) },
	})
	Register(MethodSpec{
		Method:    "plugin/share/updateTargets",
		NewParams: func() any { return new(PluginShareUpdateTargetsParams) },
		NewResult: func() any { return new(PluginShareUpdateTargetsResponse) },
	})
	Register(MethodSpec{
		Method:    "plugin/share/list",
		NewParams: func() any { return new(PluginShareListParams) },
		NewResult: func() any { return new(PluginShareListResponse) },
	})
	Register(MethodSpec{
		Method:    "plugin/share/checkout",
		NewParams: func() any { return new(PluginShareCheckoutParams) },
		NewResult: func() any { return new(PluginShareCheckoutResponse) },
	})
	Register(MethodSpec{
		Method:    "plugin/share/delete",
		NewParams: func() any { return new(PluginShareDeleteParams) },
		NewResult: func() any { return new(PluginShareDeleteResponse) },
	})
	Register(MethodSpec{
		Method:    "plugin/install",
		NewParams: func() any { return new(PluginInstallParams) },
		NewResult: func() any { return new(PluginInstallResponse) },
	})
	Register(MethodSpec{
		Method:    "plugin/uninstall",
		NewParams: func() any { return new(PluginUninstallParams) },
		NewResult: func() any { return new(PluginUninstallResponse) },
	})

	// experimentalFeature/list
	Register(MethodSpec{
		Method:    "experimentalFeature/list",
		NewParams: func() any { return new(ExperimentalFeatureListParams) },
		NewResult: func() any { return new(ExperimentalFeatureListResponse) },
	})

	// collaborationMode/list (experimental)
	Register(MethodSpec{
		Method:       "collaborationMode/list",
		NewParams:    func() any { return new(CollaborationModeListParams) },
		NewResult:    func() any { return new(CollaborationModeListResponse) },
		Experimental: true,
	})

	// Notifications owned by this area.
	RegisterNotification(NotificationSpec{
		Method:    "app/list/updated",
		NewParams: func() any { return new(AppListUpdatedNotification) },
	})
	RegisterNotification(NotificationSpec{
		Method:    "model/rerouted",
		NewParams: func() any { return new(ModelReroutedNotification) },
	})
	RegisterNotification(NotificationSpec{
		Method:    "model/verification",
		NewParams: func() any { return new(ModelVerificationNotification) },
	})
	RegisterNotification(NotificationSpec{
		Method:    "configWarning",
		NewParams: func() any { return new(ConfigWarningNotification) },
	})
	RegisterNotification(NotificationSpec{
		Method:    "skills/changed",
		NewParams: func() any { return new(SkillsChangedNotification) },
	})
}
