// Package identity gives every node and link a driver publishes a durable
// identity. The identifier of a node is a function of its key alone: a kind the
// catalog lists as a resource kind keeps the legacy allocation, so every
// existing identifier keeps its value, and any other node is minted under the
// path-node domain from an injectively encoded key. No arrival index, epoch,
// generation, clock reading, network address, credential or current value enters
// an identifier.
//
// A changed serial or identifier is a new, unbound identity: the earlier
// identity and its history stay, and only a person rebinds, by an explicit
// statement. A link keeps its key, and the same To, while its target is absent
// and when it returns; each publisher's claim of it and its basis are kept apart.
// Two observations of one device become one resolved resource only on evidence a
// driver attests about its own member, or on the operator's certification, and
// the result does not depend on the order the claims were admitted in.
//
// The package imports the standard library and packages of this module only.
package identity
