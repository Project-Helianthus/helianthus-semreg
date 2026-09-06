package operation_test

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func TestAdmissionExactRouteAndImmutableHooks(t *testing.T) {
	fixture := newOperationFixture(t)
	fixture.pack.mutateHooks = true
	before, err := semreg.CanonicalJSON(fixture.intent)
	if err != nil {
		t.Fatal(err)
	}
	admission := mustAdmit(t, fixture)
	route, ok := admission.Route()
	if !ok || route != (operation.Route{CapabilityInstance: "capability:test", ServiceInstance: "service:test", BindingID: "binding:test", SourceID: "source:test", SourceEpochID: "epoch:test:1", DriverGeneration: "1"}) {
		t.Fatalf("unexpected exact route: %+v", route)
	}
	after, _ := semreg.CanonicalJSON(fixture.intent)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("pack hook mutated caller intent")
	}
	copyIntent := admission.Intent()
	copyIntent.Arguments[0].ID = "field.test.changed"
	if admission.Intent().Arguments[0].ID != testField.ID {
		t.Fatal("admission intent accessor leaked mutation")
	}
}

func TestAuthorityResolverInputIsImmutable(t *testing.T) {
	fixture := newOperationFixture(t)
	before, _ := semreg.CanonicalJSON(fixture.intent)
	resolver := operation.AuthorityResolverFunc(func(intent operation.Intent, _ operation.Route, _ semreg.EvaluationContext) error {
		intent.Arguments[0].ID = "field.test.changed"
		intent.Causal.Path = append(intent.Causal.Path, "target:mutated")
		return nil
	})
	if _, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, resolver); err != nil {
		t.Fatal(err)
	}
	after, _ := semreg.CanonicalJSON(fixture.intent)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("authority resolver mutated caller intent")
	}
}

func TestAdmissionRejectionMatrix(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*operationFixture)
		authority operation.AuthorityResolver
		want      semreg.ErrorID
	}{
		{"authority missing", nil, nil, semreg.AuthorityMissing},
		{"authority expired", nil, operation.AuthorityResolverFunc(func(operation.Intent, operation.Route, semreg.EvaluationContext) error { return errors.New("expired") }), semreg.AuthorityMissing},
		{"deadline equality", func(f *operationFixture) { f.intent.Deadline = timePoint("150") }, operation.AuthorityResolverFunc(authorize), semreg.DeadlineExpired},
		{"deadline uncertainty overlap", func(f *operationFixture) {
			f.current.EvaluatedAt.UncertaintyNS = "2"
			f.intent.Deadline = timePoint("153")
			f.intent.Deadline.UncertaintyNS = "1"
		}, operation.AuthorityResolverFunc(authorize), semreg.DeadlineExpired},
		{"causal expired", func(f *operationFixture) { f.intent.Causal.ExpiresAt = timePoint("150") }, operation.AuthorityResolverFunc(authorize), semreg.CausalBudgetExceeded},
		{"semantic revision", func(f *operationFixture) { f.intent.ExpectedSemanticRevision = "2" }, operation.AuthorityResolverFunc(authorize), semreg.RevisionConflict},
		{"capability component revision", func(f *operationFixture) { f.intent.ExpectedCapabilityRevision = "2" }, operation.AuthorityResolverFunc(authorize), semreg.RevisionConflict},
		{"capability instance revision", func(f *operationFixture) { f.intent.ExpectedCapabilityInstanceRevision = "2" }, operation.AuthorityResolverFunc(authorize), semreg.RevisionConflict},
		{"source epoch", func(f *operationFixture) { f.intent.ExpectedSourceEpochID = "epoch:test:old" }, operation.AuthorityResolverFunc(authorize), semreg.StaleSourceEpoch},
		{"driver generation", func(f *operationFixture) { f.intent.ExpectedDriverGeneration = "2" }, operation.AuthorityResolverFunc(authorize), semreg.StaleDriverGeneration},
		{"precondition missing", func(f *operationFixture) { f.intent.Preconditions[0].CandidateID = "candidate:missing" }, operation.AuthorityResolverFunc(authorize), semreg.PreconditionFailed},
		{"precondition revision", func(f *operationFixture) { f.intent.Preconditions[0].CandidateRevision = "2" }, operation.AuthorityResolverFunc(authorize), semreg.PreconditionFailed},
		{"precondition false", func(f *operationFixture) { f.intent.Preconditions[0].Expected = boolValue(false) }, operation.AuthorityResolverFunc(authorize), semreg.PreconditionFailed},
		{"argument constraint", func(f *operationFixture) { f.intent.Arguments[0].Value = boolValue(false) }, operation.AuthorityResolverFunc(authorize), semreg.AmbiguousRoute},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newOperationFixture(t)
			if test.mutate != nil {
				test.mutate(&fixture)
			}
			_, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, test.authority)
			errorID(t, err, test.want)
			// Rejection is atomic: the corrected bytes under the same key remain
			// admissible because no partial idempotency entry was installed.
			if _, err := fixture.kernel.Admit(fixture.snapshot, context("150", "150"), validIntent(), operation.AuthorityResolverFunc(authorize)); err != nil {
				t.Fatalf("rejection mutated admission state: %v", err)
			}
		})
	}
}

func TestAdmissionTimeReevaluationAndCandidateAxes(t *testing.T) {
	t.Run("stored fresh ages stale", func(t *testing.T) {
		fixture := newOperationFixture(t)
		fixture.current = context("1100", "1100")
		fixture.intent.Deadline = timePoint("2000")
		fixture.intent.Causal.ExpiresAt = timePoint("2000")
		_, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
		errorID(t, err, semreg.PreconditionFailed)
	})

	axes := []struct {
		name   string
		mutate func(*semreg.FactCandidate)
	}{
		{"qualification", func(c *semreg.FactCandidate) {
			c.Quality.Qualification = semreg.QualificationCandidate
			c.Quality.Promotion = semreg.PromotionUnpromoted
		}},
		{"promotion", func(c *semreg.FactCandidate) { c.Quality.Promotion = semreg.PromotionUnpromoted }},
		{"validity", func(c *semreg.FactCandidate) { c.Quality.Validity = semreg.ValiditySuspect }},
		{"availability", func(c *semreg.FactCandidate) { c.Quality.Availability = semreg.AvailabilityDegraded }},
	}
	for _, axis := range axes {
		t.Run(axis.name, func(t *testing.T) {
			fixture := newOperationFixtureWith(t, func(batch *semreg.PublicationBatch) { axis.mutate(&batch.FactUpserts[0]) })
			_, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
			errorID(t, err, semreg.PreconditionFailed)
		})
	}
	t.Run("open conflict", func(t *testing.T) {
		fixture := newOperationFixtureWith(t, func(batch *semreg.PublicationBatch) {
			batch.FactUpserts = append(batch.FactUpserts, factCandidate("candidate:interlock:z", preKey, false, "100", "100", "1"))
		})
		_, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
		errorID(t, err, semreg.PreconditionFailed)
	})
}

func TestCapabilityRouteMatrix(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*semreg.PublicationBatch)
		intent func(*operation.Intent)
		want   semreg.ErrorID
	}{
		{"candidate", func(b *semreg.PublicationBatch) { b.CapabilityUpserts[0].Qualification = semreg.QualificationCandidate }, nil, semreg.CapabilityNotQualified},
		{"degraded forbidden", func(b *semreg.PublicationBatch) { b.CapabilityUpserts[0].Availability = semreg.AvailabilityDegraded }, nil, semreg.CapabilityUnavailable},
		{"withdrawn", func(b *semreg.PublicationBatch) { b.CapabilityUpserts[0].Availability = semreg.AvailabilityWithdrawn }, nil, semreg.CapabilityUnavailable},
		{"service candidate", func(b *semreg.PublicationBatch) { b.ServiceUpserts[0].Qualification = semreg.QualificationCandidate }, nil, semreg.CapabilityNotQualified},
		{"multiple", func(b *semreg.PublicationBatch) {
			second := b.CapabilityUpserts[0]
			second.InstanceID = "capability:test:z"
			b.CapabilityUpserts = append(b.CapabilityUpserts, second)
		}, nil, semreg.AmbiguousRoute},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newOperationFixtureWith(t, test.mutate)
			if test.intent != nil {
				test.intent(&fixture.intent)
			}
			_, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
			errorID(t, err, test.want)
		})
	}
	t.Run("degraded explicitly admitted by requirement and pack", func(t *testing.T) {
		fixture := newOperationFixtureWith(t, func(b *semreg.PublicationBatch) { b.CapabilityUpserts[0].Availability = semreg.AvailabilityDegraded })
		fixture.intent.RequiredCapability.AllowDegraded = true
		mustAdmit(t, fixture)
	})
}

type baseOnlyPack struct{ *testOperationPack }

func TestDefinitionOwnerAndExpectedEffectMatrix(t *testing.T) {
	pack := &testOperationPack{}
	base := &baseOnlyPack{testOperationPack: pack}
	if _, err := operation.NewKernel(base); err != nil {
		// Embedded methods include operation hooks, so exercise a truly base-only
		// wrapper below instead.
		t.Fatal(err)
	}
	withoutHook := basePackValidator{delegate: pack}
	_, err := operation.NewKernel(withoutHook)
	errorID(t, err, semreg.DefinitionOwnerMissing)
	missingOperation := &missingOperationPack{testOperationPack: &testOperationPack{}}
	missingKernel, err := operation.NewKernel(missingOperation)
	if err != nil {
		t.Fatal(err)
	}
	errorID(t, missingKernel.ValidateIntent(validIntent()), semreg.DefinitionOwnerMissing)

	conflict := &testOperationPack{}
	conflictIndex := conflictPack{testOperationPack: conflict}
	_, err = operation.NewKernel(pack, &conflictIndex)
	errorID(t, err, semreg.DefinitionOwnerConflict)

	fixture := newOperationFixture(t)
	fixture.intent.ExpectedEffect.Expected = boolValue(false)
	_, err = fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
	errorID(t, err, semreg.InvalidValue)
	if fixture.pack.intentCalls != 1 {
		t.Fatalf("rejecting operation validation hook calls: %d", fixture.pack.intentCalls)
	}

	distinct := &distinctOperationPack{testOperationPack: &testOperationPack{}}
	for _, validators := range [][]semreg.PackValidator{{pack, distinct}, {distinct, pack}} {
		kernel, err := operation.NewKernel(validators...)
		if err != nil {
			t.Fatal(err)
		}
		orderedFixture := newOperationFixture(t)
		orderedFixture.kernel = kernel
		route, ok := mustAdmit(t, orderedFixture).Route()
		if !ok || route.CapabilityInstance != "capability:test" {
			t.Fatalf("registration order changed exact dispatch: %+v", route)
		}
	}
}

type missingOperationPack struct{ *testOperationPack }

func (p missingOperationPack) Definitions() semreg.DefinitionIndex {
	index := p.testOperationPack.Definitions()
	index.Operations = []semreg.DefinitionRef{}
	return index
}

type distinctOperationPack struct{ *testOperationPack }

func (p distinctOperationPack) Pack() semreg.PackRef {
	return semreg.PackRef{ID: "pack.distinct", Version: "1.0.0"}
}
func (p distinctOperationPack) Definitions() semreg.DefinitionIndex {
	pack := p.Pack()
	ref := func(id semreg.DefinitionID) semreg.DefinitionRef {
		return semreg.DefinitionRef{Pack: pack, ID: id, Version: "1.0.0"}
	}
	return semreg.DefinitionIndex{
		Pack: pack, Fields: []semreg.DefinitionRef{ref("field.distinct.level")}, Services: []semreg.DefinitionRef{ref("service.distinct.control")},
		Capabilities: []semreg.DefinitionRef{ref("capability.distinct.level")}, Operations: []semreg.DefinitionRef{ref("operation.distinct.set_level")}, EffectRules: []semreg.DefinitionRef{ref("effect.distinct.level")},
	}
}

type basePackValidator struct{ delegate *testOperationPack }

func (b basePackValidator) Pack() semreg.PackRef                { return b.delegate.Pack() }
func (b basePackValidator) Definitions() semreg.DefinitionIndex { return b.delegate.Definitions() }
func (b basePackValidator) ValidateFact(k semreg.FactKey, v *semreg.Value) error {
	return b.delegate.ValidateFact(k, v)
}
func (b basePackValidator) ValidateService(v semreg.ServiceInstance) error {
	return b.delegate.ValidateService(v)
}
func (b basePackValidator) ValidateCapability(v semreg.CapabilityInstance) error {
	return b.delegate.ValidateCapability(v)
}
func (b basePackValidator) ValidateField(r semreg.DefinitionRef, f semreg.TypedField) error {
	return b.delegate.ValidateField(r, f)
}
func (b basePackValidator) MatchConstraints(c semreg.CapabilityInstance, f []semreg.TypedField) error {
	return b.delegate.MatchConstraints(c, f)
}
func (b basePackValidator) EvaluatePredicate(c semreg.FactCandidate, o semreg.PredicateOp, v semreg.Value) (bool, error) {
	return b.delegate.EvaluatePredicate(c, o, v)
}

type conflictPack struct{ *testOperationPack }

func (p conflictPack) Pack() semreg.PackRef {
	return semreg.PackRef{ID: "pack.other", Version: "1.0.0"}
}
func (p conflictPack) Definitions() semreg.DefinitionIndex {
	index := p.testOperationPack.Definitions()
	index.Pack = p.Pack()
	for _, group := range [][]semreg.DefinitionRef{index.Fields, index.Services, index.Capabilities, index.Operations, index.EffectRules} {
		for i := range group {
			group[i].Pack = p.Pack()
		}
	}
	return index
}

func TestCausalIngressMatrix(t *testing.T) {
	base := validIntent().Causal
	entered, err := operation.EnterCausal(base, "target:a", timePoint("150"))
	if err != nil || entered.HopCount != 1 || !reflect.DeepEqual(entered.Path, []semreg.TargetID{"target:a"}) || len(base.Path) != 0 {
		t.Fatalf("causal ingress: %+v %v", entered, err)
	}
	_, err = operation.EnterCausal(entered, "target:a", timePoint("150"))
	errorID(t, err, semreg.EchoSuppressed)
	entered.MaxHops = entered.HopCount
	_, err = operation.EnterCausal(entered, "target:b", timePoint("150"))
	errorID(t, err, semreg.CausalBudgetExceeded)
	malformed := base
	malformed.Path = []semreg.TargetID{"target:a"}
	_, err = operation.EnterCausal(malformed, "target:b", timePoint("150"))
	errorID(t, err, semreg.CausalBudgetExceeded)
	overlong := base
	overlong.ExpiresAt = timePoint("300000000101")
	_, err = operation.EnterCausal(overlong, "target:b", timePoint("150"))
	errorID(t, err, semreg.CausalBudgetExceeded)
}

func TestIdempotencyAndConcurrentAdmission(t *testing.T) {
	fixture := newOperationFixture(t)
	const workers = 32
	results := make(chan *operation.Admission, workers)
	errors := make(chan error, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			admission, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
			results <- admission
			errors <- err
		}()
	}
	wait.Wait()
	close(results)
	close(errors)
	var first *operation.Admission
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	for admission := range results {
		if first == nil {
			first = admission
		} else if admission != first {
			t.Fatal("identical concurrent intent did not deduplicate")
		}
	}
	different := fixture.intent
	different.IntentID = "intent:test:other"
	_, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, different, operation.AuthorityResolverFunc(authorize))
	errorID(t, err, semreg.SequenceConflict)
}

func TestAdmissionGuardClaimsAreExactImmutableAndSnapshotBound(t *testing.T) {
	fixture := newOperationFixture(t)
	admission := mustAdmit(t, fixture)
	want := operation.GuardClaims{
		AssetID:                            "asset:test",
		ExpectedCapabilityRevision:         "1",
		CapabilityInstance:                 "capability:test",
		ExpectedCapabilityInstanceRevision: "1",
		ServiceInstance:                    "service:test",
		BindingID:                          "binding:test",
		SourceID:                           "source:test",
		SourceEpochID:                      "epoch:test:1",
		DriverGeneration:                   "1",
	}
	claims, ok := admission.GuardClaims()
	if !ok || claims != want {
		t.Fatalf("guard claims: got %+v want %+v", claims, want)
	}
	claims.SourceEpochID = "epoch:mutated"
	claims.DriverGeneration = "99"
	if current, ok := admission.GuardClaims(); !ok || current != want {
		t.Fatalf("guard claims leaked caller mutation: %+v", current)
	}

	// An admission remains immutable evidence for its admitted snapshot. A later
	// fence does not rewrite it; INT-06 must compare these claims with native
	// lifecycle state under its own lock before dispatch.
	applyFence(t, fixture.publication, fixture.snapshot.Revisions.Semantic, "2")
	if current, ok := admission.GuardClaims(); !ok || current != want {
		t.Fatalf("later publication rewrote admitted guard claims: %+v", current)
	}
}

func TestIntentBoundsAndAdmissionAllocationsAreProportional(t *testing.T) {
	fixture := newOperationFixture(t)
	bounded := fixture.intent
	bounded.Arguments = make([]semreg.TypedField, 65)
	for index := range bounded.Arguments {
		bounded.Arguments[index] = semreg.TypedField{ID: semreg.DefinitionID(fmt.Sprintf("field.test.%03d", index)), Value: boolValue(true)}
	}
	errorID(t, bounded.Validate(), semreg.BoundsExceeded)

	makeSized := func(count int) operationFixture {
		return newOperationFixtureWith(t, func(batch *semreg.PublicationBatch) {
			for index := 0; index < count; index++ {
				id := semreg.CandidateID(fmt.Sprintf("candidate:padding:%03d", index))
				key := semreg.FactKey{PackID: testPackRef.ID, PackVersion: testPackRef.Version, FactID: semreg.DefinitionID(fmt.Sprintf("fact.test.padding.%03d", index)), Dimensions: []semreg.Dimension{}}
				batch.FactUpserts = append(batch.FactUpserts, factCandidate(id, key, true, "100", "100", "1"))
			}
		})
	}
	small, large := makeSized(4), makeSized(31)
	measure := func(fixture operationFixture) float64 {
		var runErr error
		allocations := testing.AllocsPerRun(5, func() {
			kernel, err := operation.NewKernel(fixture.pack)
			if err == nil {
				_, err = kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
			}
			runErr = err
		})
		if runErr != nil {
			t.Fatal(runErr)
		}
		return allocations
	}
	smallAllocations, largeAllocations := measure(small), measure(large)
	if largeAllocations > smallAllocations*10 {
		t.Fatalf("allocation growth is not proportional: small=%.0f large=%.0f", smallAllocations, largeAllocations)
	}
}

func applyFence(t *testing.T, publication *semreg.PublicationKernel, expectedRevision semreg.Uint64, sequence semreg.Uint64) semreg.Snapshot {
	t.Helper()
	batch := semreg.PublicationBatch{
		Contract: semreg.ContractKernelV1, BatchID: semreg.BatchID("batch:fence:" + string(sequence)), AssetID: "asset:test", SourceID: "source:test", SourceEpochID: "epoch:test:1", DriverGeneration: "1", Sequence: sequence,
		ExpectedSemanticRevision: expectedRevision, ObservedAt: timePoint("180"), SourceUpserts: []semreg.SourceDescriptor{}, SourceRetirements: []semreg.SourceEpochID{}, BindingUpserts: []semreg.NativeBinding{},
		IdentityLinkUpserts: []semreg.IdentityLink{}, FactUpserts: []semreg.FactCandidate{}, FactWithdrawals: []semreg.CandidateID{}, ServiceUpserts: []semreg.ServiceInstance{}, ServiceWithdrawals: []semreg.ServiceInstanceID{},
		CapabilityUpserts: []semreg.CapabilityInstance{}, CapabilityWithdrawals: []semreg.CapabilityInstanceID{},
		GenerationFences: []semreg.GenerationFence{{SourceID: "source:test", SourceEpochID: "epoch:test:1", DriverGeneration: "1", Reason: "reason.test.fence", Evidence: []semreg.EvidenceRef{evidence(6)}, Revision: "1"}},
	}
	digest, err := batch.ComputedDigest()
	if err != nil {
		t.Fatal(err)
	}
	batch.BatchDigest = digest
	snapshot, _, err := publication.Apply(batch, semreg.MonotonicPoint{ClockEpochID: "clock-epoch:test", Nanoseconds: "180"})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
