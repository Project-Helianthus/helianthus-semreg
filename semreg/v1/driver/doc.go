// Package driver is the host-neutral driver contract of SemReg. The SemReg
// runtime implements it and every driver codes against it: a driver states in
// these types its factory and instance plans, the durable keys of the nodes and
// links it publishes, a priority for every source of an element, the route to
// the owner of each setting, the semantic inputs it consumes, the members of its
// diagnostic namespace and value limits that say where they came from. A host
// that imports SemReg and the drivers it chooses needs nothing else from any
// other host.
//
// The package imports the standard library and packages of this module only. It
// names no protocol, vendor, register or host. Its records are plain validated
// values: each exported record type that states a rule has a Validate method
// that checks that rule and nothing else. The package adds no wire revision,
// alias or compatibility path.
package driver
