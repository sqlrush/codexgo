package appserverproto

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// MethodSpec describes one client->server request method: its wire name plus
// constructors for fresh, decodable params and result values.
//
// This replaces the Rust client_request_definitions! macro variant. Each area
// agent registers the methods it owns from an init() in its own file:
//
//	func init() {
//	    Register(MethodSpec{
//	        Method:    "thread/start",
//	        NewParams: func() any { return new(ThreadStartParams) },
//	        NewResult: func() any { return new(ThreadStartResponse) },
//	    })
//	}
//
// NewParams / NewResult must return a non-nil pointer suitable for json.Unmarshal.
type MethodSpec struct {
	// Method is the exact wire method string (e.g. "thread/start").
	Method string
	// NewParams returns a fresh pointer to decode this method's params into.
	NewParams func() any
	// NewResult returns a fresh pointer to decode this method's result into.
	NewResult func() any
	// Experimental marks methods gated behind the experimental_api capability.
	Experimental bool
}

// NotificationSpec describes one server->client notification: its wire method
// and a constructor for a fresh, decodable params value. Mirrors the Rust
// server_notification_definitions! variant.
type NotificationSpec struct {
	// Method is the exact wire method string (e.g. "thread/started").
	Method string
	// NewParams returns a fresh pointer to decode this notification's params into.
	NewParams func() any
	// Experimental marks notifications gated behind the experimental_api capability.
	Experimental bool
}

// ServerRequestSpec describes one server->client request: its wire method plus
// constructors for params and response. Mirrors the Rust
// server_request_definitions! variant.
type ServerRequestSpec struct {
	// Method is the exact wire method string (e.g. "item/tool/call").
	Method string
	// NewParams returns a fresh pointer to decode this request's params into.
	NewParams func() any
	// NewResult returns a fresh pointer to decode this request's response into.
	NewResult func() any
}

// registries holds the three dispatch tables. A mutex guards registration so
// init() ordering across files in the package is safe even under -race, and so
// area agents need not coordinate.
var registries = struct {
	mu             sync.RWMutex
	clientRequests map[string]MethodSpec
	serverNotes    map[string]NotificationSpec
	serverRequests map[string]ServerRequestSpec
}{
	clientRequests: make(map[string]MethodSpec),
	serverNotes:    make(map[string]NotificationSpec),
	serverRequests: make(map[string]ServerRequestSpec),
}

// Register adds a client request method to the registry. It panics on a missing
// method name, nil constructors, or a duplicate registration, because these are
// programmer errors discoverable at package-load time.
func Register(spec MethodSpec) {
	if spec.Method == "" {
		panic("appserverproto: Register called with empty Method")
	}
	if spec.NewParams == nil || spec.NewResult == nil {
		panic(fmt.Sprintf("appserverproto: Register(%q) requires non-nil NewParams and NewResult", spec.Method))
	}
	registries.mu.Lock()
	defer registries.mu.Unlock()
	if _, dup := registries.clientRequests[spec.Method]; dup {
		panic(fmt.Sprintf("appserverproto: duplicate client request method %q", spec.Method))
	}
	registries.clientRequests[spec.Method] = spec
}

// RegisterNotification adds a server notification to the registry.
func RegisterNotification(spec NotificationSpec) {
	if spec.Method == "" {
		panic("appserverproto: RegisterNotification called with empty Method")
	}
	if spec.NewParams == nil {
		panic(fmt.Sprintf("appserverproto: RegisterNotification(%q) requires non-nil NewParams", spec.Method))
	}
	registries.mu.Lock()
	defer registries.mu.Unlock()
	if _, dup := registries.serverNotes[spec.Method]; dup {
		panic(fmt.Sprintf("appserverproto: duplicate server notification method %q", spec.Method))
	}
	registries.serverNotes[spec.Method] = spec
}

// RegisterServerRequest adds a server->client request to the registry.
func RegisterServerRequest(spec ServerRequestSpec) {
	if spec.Method == "" {
		panic("appserverproto: RegisterServerRequest called with empty Method")
	}
	if spec.NewParams == nil || spec.NewResult == nil {
		panic(fmt.Sprintf("appserverproto: RegisterServerRequest(%q) requires non-nil NewParams and NewResult", spec.Method))
	}
	registries.mu.Lock()
	defer registries.mu.Unlock()
	if _, dup := registries.serverRequests[spec.Method]; dup {
		panic(fmt.Sprintf("appserverproto: duplicate server request method %q", spec.Method))
	}
	registries.serverRequests[spec.Method] = spec
}

// Lookup resolves a client request method to its MethodSpec.
func Lookup(method string) (MethodSpec, bool) {
	registries.mu.RLock()
	defer registries.mu.RUnlock()
	spec, ok := registries.clientRequests[method]
	return spec, ok
}

// LookupNotification resolves a server notification method to its spec.
func LookupNotification(method string) (NotificationSpec, bool) {
	registries.mu.RLock()
	defer registries.mu.RUnlock()
	spec, ok := registries.serverNotes[method]
	return spec, ok
}

// LookupServerRequest resolves a server request method to its spec.
func LookupServerRequest(method string) (ServerRequestSpec, bool) {
	registries.mu.RLock()
	defer registries.mu.RUnlock()
	spec, ok := registries.serverRequests[method]
	return spec, ok
}

// RegisteredMethods returns the registered client request method names, sorted.
func RegisteredMethods() []string {
	registries.mu.RLock()
	defer registries.mu.RUnlock()
	return sortedKeys(registries.clientRequests)
}

// RegisteredNotifications returns the registered server notification method
// names, sorted.
func RegisteredNotifications() []string {
	registries.mu.RLock()
	defer registries.mu.RUnlock()
	return sortedKeysNote(registries.serverNotes)
}

// RegisteredServerRequests returns the registered server request method names,
// sorted.
func RegisteredServerRequests() []string {
	registries.mu.RLock()
	defer registries.mu.RUnlock()
	return sortedKeysSrv(registries.serverRequests)
}

// DecodeClientRequestParams resolves the method on a JSONRPCRequest and decodes
// its params into a freshly constructed, typed value. A nil params field decodes
// the JSON literal `null` so methods with `Option<()>` params accept absence.
func DecodeClientRequestParams(req JSONRPCRequest) (any, error) {
	spec, ok := Lookup(req.Method)
	if !ok {
		return nil, fmt.Errorf("appserverproto: unknown client request method %q", req.Method)
	}
	return decodeInto(spec.NewParams(), req.Params)
}

// DecodeServerNotificationParams resolves the method on a JSONRPCNotification and
// decodes its params into a freshly constructed, typed value.
func DecodeServerNotificationParams(note JSONRPCNotification) (any, error) {
	spec, ok := LookupNotification(note.Method)
	if !ok {
		return nil, fmt.Errorf("appserverproto: unknown server notification method %q", note.Method)
	}
	return decodeInto(spec.NewParams(), note.Params)
}

// DecodeServerRequestParams resolves the method on a JSONRPCRequest and decodes
// its params into a freshly constructed, typed value, using the server-request
// registry (server->client direction).
func DecodeServerRequestParams(req JSONRPCRequest) (any, error) {
	spec, ok := LookupServerRequest(req.Method)
	if !ok {
		return nil, fmt.Errorf("appserverproto: unknown server request method %q", req.Method)
	}
	return decodeInto(spec.NewParams(), req.Params)
}

// decodeInto unmarshals raw params into target, treating absent params as null.
func decodeInto(target any, raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		raw = json.RawMessage("null")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return nil, fmt.Errorf("appserverproto: decode params: %w", err)
	}
	return target, nil
}

func sortedKeys(m map[string]MethodSpec) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeysNote(m map[string]NotificationSpec) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeysSrv(m map[string]ServerRequestSpec) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
