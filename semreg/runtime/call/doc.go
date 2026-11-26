// Package call admits one closed operation request, selects the single setting
// owner, rechecks that route at dispatch, and reports evidence that keeps
// acceptance, acknowledgement and readback confirmation apart.
//
// A finite request hands its caller's context to the executor unchanged. The
// timeout and the intent deadline reach the executor through the dispatch and
// the intent, and the router puts no deadline of its own on the context, so the
// native transport's established timeout stays in charge.
package call
