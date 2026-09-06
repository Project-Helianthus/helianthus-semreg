// Package semreg implements the protocol-neutral v1 semantic foundation.
// It deliberately owns no transport, protocol, vendor, gateway, or consumer
// behavior. The BASE publication/snapshot, evaluation, and presentation
// selection runtimes, protocol-neutral operation admission/evidence runtime,
// and the separate pure projection/compatibility record package are
// implemented.
//
// Decode is the strict wire entry point. It rejects malformed UTF-8, duplicate
// or unknown members, null, wrong token types, missing required members, and
// trailing data recursively before binding JSON to a typed record.
//
// Record Validate methods check self-contained structure. Definitions that
// claim qualification or promotion additionally cross the explicit Registry
// boundary through ValidateFactCandidate, ValidateService, ValidateCapability,
// or ValidateField so one exact registered owner hook is dispatched.
package semreg
