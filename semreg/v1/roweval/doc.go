// Package roweval evaluates driver-supplied table rows against acquired
// observations. It matches only what an observation supplies, evaluates rows
// forward (native payload to SemReg candidate) and inverse (admitted argument
// to native payload), and holds no protocol knowledge: the selector geometry
// is a type parameter, and the codec and layout identifiers, the roles and the
// hooks are supplied by the driver that owns the rows. The package keeps no
// session state and is passed no bus, clock, publisher or executor.
package roweval
