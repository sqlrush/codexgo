// Package extensionapi is the faithful Go port of codex's ext/extension-api
// crate: the async extension contributor traits/interfaces for tool and request
// interception plus the registration/dispatch registry the engine uses.
package extensionapi

import (
	"reflect"
	"sync"
)

// ExtensionData is typed extension-owned data attached to one host object.
//
// It mirrors the Rust `ExtensionData`, which keys an erased value map by
// `TypeId`. Here entries are keyed by the dynamic [reflect.Type] of the stored
// value, so each Go type occupies exactly one slot. Stored values are shared
// (the host hands back the same value on subsequent reads), matching the Rust
// `Arc<dyn Any>` semantics.
type ExtensionData struct {
	levelID string
	mu      sync.Mutex
	entries map[reflect.Type]any
}

// NewExtensionData creates an empty attachment map for one host-owned scope.
// Mirrors Rust `ExtensionData::new`.
func NewExtensionData(levelID string) *ExtensionData {
	return &ExtensionData{
		levelID: levelID,
		entries: make(map[reflect.Type]any),
	}
}

// LevelID returns the host identity for the scope this data is attached to.
// Mirrors Rust `ExtensionData::level_id`.
func (d *ExtensionData) LevelID() string {
	return d.levelID
}

// ExtensionDataGet returns the attached value of type T, if one exists.
//
// It is a free function rather than a method because Go methods cannot
// introduce new type parameters. Mirrors Rust `ExtensionData::get::<T>`.
func ExtensionDataGet[T any](d *ExtensionData) (T, bool) {
	var zero T
	key := typeKey[T]()
	d.mu.Lock()
	defer d.mu.Unlock()
	value, ok := d.entries[key]
	if !ok {
		return zero, false
	}
	typed, ok := value.(T)
	if !ok {
		// Stored an incompatible value; mirrors the Rust unreachable! panic.
		panic("typed extension data stored an incompatible value")
	}
	return typed, true
}

// ExtensionDataGetOrInit returns the attached value of type T, inserting one
// from init when absent.
//
// The initializer runs while the map is locked, so it should stay cheap;
// heavyweight lazy work belongs inside the attached value itself. Mirrors Rust
// `ExtensionData::get_or_init::<T>`.
func ExtensionDataGetOrInit[T any](d *ExtensionData, init func() T) T {
	key := typeKey[T]()
	d.mu.Lock()
	defer d.mu.Unlock()
	if value, ok := d.entries[key]; ok {
		typed, ok := value.(T)
		if !ok {
			panic("typed extension data stored an incompatible value")
		}
		return typed
	}
	created := init()
	d.entries[key] = created
	return created
}

// ExtensionDataInsert stores value as the attachment of type T, returning any
// previous value. Mirrors Rust `ExtensionData::insert::<T>`.
func ExtensionDataInsert[T any](d *ExtensionData, value T) (T, bool) {
	var zero T
	key := typeKey[T]()
	d.mu.Lock()
	defer d.mu.Unlock()
	previous, ok := d.entries[key]
	d.entries[key] = value
	if !ok {
		return zero, false
	}
	typed, ok := previous.(T)
	if !ok {
		panic("typed extension data stored an incompatible value")
	}
	return typed, true
}

// ExtensionDataRemove removes and returns the attached value of type T, if one
// exists. Mirrors Rust `ExtensionData::remove::<T>`.
func ExtensionDataRemove[T any](d *ExtensionData) (T, bool) {
	var zero T
	key := typeKey[T]()
	d.mu.Lock()
	defer d.mu.Unlock()
	value, ok := d.entries[key]
	if !ok {
		return zero, false
	}
	delete(d.entries, key)
	typed, ok := value.(T)
	if !ok {
		panic("typed extension data stored an incompatible value")
	}
	return typed, true
}

// typeKey returns the reflect.Type used as a map key for stored values of type
// T. It uses a typed nil pointer so interface and concrete types alike yield a
// stable, distinct key.
func typeKey[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}
