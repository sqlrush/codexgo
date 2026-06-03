package tui

// keymapConfig is the deserialized `[tui].keymap` config block: one map of
// action -> [KeybindingsSpec] per context. Because the foundation stores the
// keymap opaquely (config.Tui.Keymap is an untyped TOML tree), this layer
// decodes that tree into typed per-context tables.
//
// Port of codex_config::types::TuiKeymap. Each context map holds only the
// actions the user explicitly configured; absent actions fall back to defaults
// during resolution.
type keymapConfig struct {
	global        map[string]KeybindingsSpec
	chat          map[string]KeybindingsSpec
	composer      map[string]KeybindingsSpec
	editor        map[string]KeybindingsSpec
	vimNormal     map[string]KeybindingsSpec
	vimOperator   map[string]KeybindingsSpec
	vimTextObject map[string]KeybindingsSpec
	pager         map[string]KeybindingsSpec
	list          map[string]KeybindingsSpec
	approval      map[string]KeybindingsSpec
}

// keymapContexts maps each TOML context key to the field it populates. The
// "global" context corresponds to the app keymap surface (see [AppKeymap]).
var keymapContextKeys = []string{
	"global", "chat", "composer", "editor",
	"vim_normal", "vim_operator", "vim_text_object",
	"pager", "list", "approval",
}

// decodeKeymapConfig builds a [keymapConfig] from an opaque decoded TOML tree
// (the value of config.Tui.Keymap). Unknown contexts are ignored; invalid spec
// node shapes are deferred to parse time (left absent here). A nil tree yields
// an all-empty config, meaning every action falls back to its default.
func decodeKeymapConfig(tree any) keymapConfig {
	cfg := keymapConfig{}
	root, ok := tree.(map[string]any)
	if !ok {
		return cfg
	}
	for _, ctx := range keymapContextKeys {
		ctxNode, ok := root[ctx]
		if !ok {
			continue
		}
		ctxMap, ok := ctxNode.(map[string]any)
		if !ok {
			continue
		}
		decoded := decodeContext(ctxMap)
		switch ctx {
		case "global":
			cfg.global = decoded
		case "chat":
			cfg.chat = decoded
		case "composer":
			cfg.composer = decoded
		case "editor":
			cfg.editor = decoded
		case "vim_normal":
			cfg.vimNormal = decoded
		case "vim_operator":
			cfg.vimOperator = decoded
		case "vim_text_object":
			cfg.vimTextObject = decoded
		case "pager":
			cfg.pager = decoded
		case "list":
			cfg.list = decoded
		case "approval":
			cfg.approval = decoded
		}
	}
	return cfg
}

// decodeContext converts one context map into action -> spec entries. Nodes that
// are not string or string-list values are skipped (treated as unconfigured).
func decodeContext(ctxMap map[string]any) map[string]KeybindingsSpec {
	out := make(map[string]KeybindingsSpec, len(ctxMap))
	for action, node := range ctxMap {
		spec, ok := parseSpec(node)
		if !ok {
			continue
		}
		out[action] = spec
	}
	return out
}

// specRef returns a pointer to the configured spec for an action, or nil when
// the action was not configured in the context. The nil/non-nil distinction
// drives fallback vs. explicit-unbind semantics during resolution.
func specRef(ctx map[string]KeybindingsSpec, action string) *KeybindingsSpec {
	if ctx == nil {
		return nil
	}
	spec, ok := ctx[action]
	if !ok {
		return nil
	}
	return &spec
}
