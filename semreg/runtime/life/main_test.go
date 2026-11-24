package life

// How the tests of this package keep their time.
//
// Every test builds its own stores, services and fixtures, shares no state
// and calls t.Parallel, so the package takes as long as its slowest test and
// not the sum of them. Two tests stay serial because their outcome depends on
// which of two goroutines gets there first (a stop against a start still in
// flight) and a busy machine must not decide it.
//
// The fixtures' file-system hooks (blockHooks) skip the device flush of the
// atomic write cycle: it changes nothing a reader in the same process can
// see, and its cost, which differs a great deal between machines, was most of
// the time the service fixtures spent. The tests that go through
// OpenFileStore keep the flush.
//
// Waiting for a condition goes through waitFor; a delay that is a property of
// the behavior under test (a budget, a retry delay, a deadline) is a value the
// test passes to the service, kept as short as the case allows.
