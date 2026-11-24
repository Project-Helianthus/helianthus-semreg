// Package sources holds the declared sources of every element and resolves
// them under the authority the drivers publish: each source is a link with its
// priority, path, resolution and availability, the value comes only from the
// highest-priority declaration, and an unknown, stale or expired top source is
// shown as such and never replaced by a lower one. It also keeps the exact
// lineage of derived values across assets.
//
// The package imports the standard library and packages of this module only.
// It performs no unit conversion: a sample is the producer's candidate already
// in SI.
package sources
