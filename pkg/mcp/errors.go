package mcp

import "errors"

// ErrTransportClosed is returned by a [Transport]'s Receive method when the
// underlying stream has been closed cleanly (for example a stdio server exited
// or an HTTP stream completed).
var ErrTransportClosed = errors.New("mcp: transport closed")

// ErrClientShutDown is returned by [Client] operations issued after the client
// has been shut down.
var ErrClientShutDown = errors.New("mcp: client is shut down")

// ErrNotInitialized is returned when an operation requiring an initialized
// session is attempted before [Client.Initialize] succeeds.
var ErrNotInitialized = errors.New("mcp: client not initialized")

// ErrAlreadyInitialized is returned when [Client.Initialize] is called more than
// once on the same client.
var ErrAlreadyInitialized = errors.New("mcp: client already initialized")
