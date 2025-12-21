// Package eventmap provides code generation for mapping between domain events
// and pupsourcing event sourcing types (es.Event and es.PersistedEvent).
//
// This package supports versioned events where directory structure determines
// event version (v1, v2, etc.), similar to protobuf package versioning.
//
// The generated code is explicit, readable, and does not use runtime reflection.
package eventmap
