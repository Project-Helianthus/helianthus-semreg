package operation_test

import (
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// Both runners execute the complete supplied route input/prior state. These
// snapshots express semantic generation state, without native lifecycle work.
func expandRouteVector(t *testing.T, vector operationVector, fixture *evseFixture) {
	t.Helper()
	snapshot := &fixture.snapshot
	switch vector.ID {
	case "K-NEG-013":
		var input struct {
			Epoch      semreg.SourceEpochID `json:"source_epoch_id"`
			Generation semreg.Uint64        `json:"driver_generation"`
		}
		var prior struct {
			Fenced  semreg.Uint64 `json:"fenced_driver_generation"`
			Current semreg.Uint64 `json:"current_driver_generation"`
		}
		decodeVectorInput(t, vector.Input, &input)
		decodeVectorInput(t, vector.PriorState, &prior)
		if input.Epoch != fixture.intent.ExpectedSourceEpochID || input.Generation != fixture.intent.ExpectedDriverGeneration ||
			input.Generation != prior.Fenced || prior.Fenced != "7" || prior.Current != "8" {
			t.Fatalf("exact generation vector values: input=%+v prior=%+v", input, prior)
		}
		binding, link, service, capability, cursor := snapshot.Bindings[0], snapshot.IdentityLinks[0], snapshot.Services[0], snapshot.Capabilities[0], snapshot.Cursors[0]
		snapshot.Bindings[0].State = semreg.BindingFenced
		snapshot.IdentityLinks[0].State = semreg.LinkWithdrawn
		snapshot.Services[0].Availability = semreg.AvailabilityWithdrawn
		snapshot.Capabilities[0].Availability = semreg.AvailabilityWithdrawn
		snapshot.Facts = []semreg.FactEnvelope{}
		snapshot.Cursors[0].Fenced = true
		snapshot.Fences = []semreg.GenerationFence{{
			SourceID: binding.SourceID, SourceEpochID: input.Epoch, DriverGeneration: prior.Fenced,
			Reason: "reason.generation-fenced", Evidence: []semreg.EvidenceRef{evidence(6)}, Revision: "1",
		}}
		// Keep the generation-7 tombstones and install a distinct current
		// replacement, including its publication cursor under the same epoch.
		binding.BindingID, binding.DriverGeneration = "binding:evse:02", prior.Current
		link.BindingID = binding.BindingID
		service.InstanceID, service.BindingID, service.DriverGeneration = "service:evse:control:02", binding.BindingID, prior.Current
		capability.InstanceID, capability.ServiceInstance = "capability:limit:02", service.InstanceID
		capability.BindingID, capability.DriverGeneration = binding.BindingID, prior.Current
		cursor.DriverGeneration, cursor.LastBatchDigest = prior.Current, semreg.Digest("sha256:"+repeatHex(7))
		snapshot.Bindings = append(snapshot.Bindings, binding)
		snapshot.IdentityLinks = append(snapshot.IdentityLinks, link)
		snapshot.Services = append(snapshot.Services, service)
		snapshot.Capabilities = append(snapshot.Capabilities, capability)
		snapshot.Cursors = append(snapshot.Cursors, cursor)
		sealCorrectionSnapshot(t, snapshot)
		fenced, current := snapshot.Cursors[0], snapshot.Cursors[1]
		if fenced.SourceID != binding.SourceID || current.SourceID != binding.SourceID ||
			fenced.SourceEpochID != input.Epoch || current.SourceEpochID != input.Epoch ||
			fenced.DriverGeneration != prior.Fenced || !fenced.Fenced || current.DriverGeneration != prior.Current || current.Fenced ||
			snapshot.Bindings[0].State != semreg.BindingFenced || snapshot.Bindings[0].DriverGeneration != prior.Fenced ||
			snapshot.Bindings[1].State != semreg.BindingCurrent || snapshot.Bindings[1].DriverGeneration != prior.Current {
			t.Fatalf("exact retained/replacement generation state: %+v", snapshot)
		}
	case "K-NEG-016":
		var input struct {
			RequiredCapability semreg.DefinitionID           `json:"required_capability"`
			EligibleRoutes     []semreg.CapabilityInstanceID `json:"eligible_routes"`
		}
		decodeVectorInput(t, vector.Input, &input)
		if input.RequiredCapability != fixture.intent.RequiredCapability.DefinitionID || len(input.EligibleRoutes) != 2 {
			t.Fatalf("exact ambiguity vector values: %+v", input)
		}
		first := snapshot.Capabilities[0]
		first.InstanceID = input.EligibleRoutes[0]
		second := first
		second.InstanceID = input.EligibleRoutes[1]
		snapshot.Capabilities = []semreg.CapabilityInstance{first, second}
		sealCorrectionSnapshot(t, snapshot)
	default:
		t.Fatalf("unexpected route vector %s", vector.ID)
	}
}
