// Package ext is the frame of the extension protocol of the hub (docs/hub/extensions.md): a separate process, an
// adapter, that the hub calls over HTTP so that corporate things (a cloud, accounts, documents, connections, memory) are
// added without changing the hub. It holds what every kind of extension needs and the machine provider of
// docs/hub/provider.md already had: the address and the token file, the describe handshake with the protocol version and
// the capabilities, the timeout, the retries with an idempotency key, and the circuit breaker; and the registry of the
// adapters with their state, which `hub doctor` and the capabilities API show.
//
// What an adapter of one kind says over this frame (the calls of the machine provider, of a credential provider and so
// on) lives with that kind, not here.
package ext
