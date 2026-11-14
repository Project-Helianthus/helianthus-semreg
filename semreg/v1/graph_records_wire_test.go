package semreg

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// The pinned inputs below were captured from the code before the graph members
// existed. They are the digest input of a batch (batch_digest omitted) and the
// identity input of the snapshot it produced (snapshot_id omitted). Repeated
// evidence digests are written as #c# and expand to 64 copies of c; #batch#
// is the digest of the pinned batch input, computed here, not pinned.
const graphPinnedBatchInput = `{"asset_id":"asset:site",` +
	`"batch_id":"batch:source:a:epoch:a:1:1",` +
	`"binding_upserts":[{"asset_id":"asset:site","binding_id":"binding:a","driver_generation":"1","native_resource":{"access":"public","contract":"test.contract/v1","digest":"sha256:#1#","kind":"evidence.test","owner":"owner.test","redaction":"none"},"revision":"1","source_epoch_id":"epoch:a","source_id":"source:a","state":"current"}],` +
	`"capability_upserts":[{"activation_evidence":[{"access":"public","contract":"helianthus.gateway.driver.activation.v1","digest":"sha256:#3#","kind":"driver.activation","owner":"helianthus.gateway","redaction":"metadata_only"}],"asset_id":"asset:site","availability":"available","binding_id":"binding:a","constraints":[],"definition":{"id":"capability.test","pack":{"id":"pack.test","version":"1.0.0"},"version":"1.0.0"},"driver_generation":"1","instance_id":"capability:binding:a","qualification":"qualified","revision":"1","service_instance":"service:binding:a","source_epoch_id":"epoch:a"}],` +
	`"capability_withdrawals":[],` +
	`"contract":"helianthus.semantic.kernel/v1",` +
	`"driver_generation":"1",` +
	`"expected_semantic_revision":"0",` +
	`"fact_upserts":[{"binding_id":"binding:a","candidate_id":"candidate:binding:a","driver_generation":"1","evidence":[{"access":"public","contract":"test.contract/v1","digest":"sha256:#1#","kind":"evidence.test","owner":"owner.test","redaction":"none"}],"freshness_policy":{"fresh_for_ns":"30","max_wall_uncertainty_ns":"1","policy_id":"policy:a","retain_for_ns":"300","version":"1.0.0"},"key":{"dimensions":[],"fact_id":"fact.power","pack_id":"pack.test","pack_version":"1.0.0"},"origin":{"binding_id":"binding:a","evidence":[{"access":"public","contract":"test.contract/v1","digest":"sha256:#4#","kind":"evidence.test","owner":"owner.test","redaction":"none"}],"kind":"native_observation","origin_id":"origin:candidate:binding:a","source_epoch_id":"epoch:a","source_id":"source:a"},"quality":{"assertion":"observed","availability":"available","freshness":"fresh","promotion":"promoted","qualification":"qualified","reasons":[],"validity":"good"},"revision":"1","source_epoch_id":"epoch:a","times":{"evaluate_monotonic":{"clock_epoch_id":"clock:a","nanoseconds":"6"},"evaluated_at":{"clock_id":"clock.utc","uncertainty_ns":"0","unix_nanoseconds":"101"},"receipt_monotonic":{"clock_epoch_id":"clock:a","nanoseconds":"5"},"received_at":{"clock_id":"clock.utc","uncertainty_ns":"0","unix_nanoseconds":"100"}},"value":{"boolean":true,"kind":"boolean"}}],` +
	`"fact_withdrawals":[],` +
	`"generation_fences":[],` +
	`"identity_link_upserts":[{"asset_id":"asset:site","basis":[{"access":"public","contract":"test.contract/v1","digest":"sha256:#3#","kind":"evidence.test","owner":"owner.test","redaction":"none"}],"binding_id":"binding:a","revision":"1","state":"qualified"}],` +
	`"observed_at":{"clock_id":"clock.utc","uncertainty_ns":"0","unix_nanoseconds":"201"},` +
	`"sequence":"1",` +
	`"service_upserts":[{"asset_id":"asset:site","availability":"available","binding_id":"binding:a","definition":{"id":"service.test","pack":{"id":"pack.test","version":"1.0.0"},"version":"1.0.0"},"driver_generation":"1","instance_id":"service:binding:a","qualification":"qualified","revision":"1","source_epoch_id":"epoch:a"}],` +
	`"service_withdrawals":[],` +
	`"source_epoch_id":"epoch:a",` +
	`"source_id":"source:a",` +
	`"source_retirements":[],` +
	`"source_upserts":[{"profile_id":"profile.test","profile_version":"release-1","protocol_id":"protocol.test","registry_evidence":{"access":"public","contract":"test.contract/v1","digest":"sha256:#1#","kind":"evidence.test","owner":"owner.test","redaction":"none"},"revision":"1","source_epoch_id":"epoch:a","source_id":"source:a","started_at":{"clock_id":"clock.utc","uncertainty_ns":"0","unix_nanoseconds":"100"},"state":"current"}]}`

const graphPinnedSnapshotInput = `{"asset_id":"asset:site",` +
	`"bindings":[{"asset_id":"asset:site","binding_id":"binding:a","driver_generation":"1","native_resource":{"access":"public","contract":"test.contract/v1","digest":"sha256:#1#","kind":"evidence.test","owner":"owner.test","redaction":"none"},"revision":"1","source_epoch_id":"epoch:a","source_id":"source:a","state":"current"}],` +
	`"capabilities":[{"activation_evidence":[{"access":"public","contract":"helianthus.gateway.driver.activation.v1","digest":"sha256:#3#","kind":"driver.activation","owner":"helianthus.gateway","redaction":"metadata_only"}],"asset_id":"asset:site","availability":"available","binding_id":"binding:a","constraints":[],"definition":{"id":"capability.test","pack":{"id":"pack.test","version":"1.0.0"},"version":"1.0.0"},"driver_generation":"1","instance_id":"capability:binding:a","qualification":"qualified","revision":"1","service_instance":"service:binding:a","source_epoch_id":"epoch:a"}],` +
	`"contract":"helianthus.semantic.kernel/v1",` +
	`"cursors":[{"driver_generation":"1","fenced":false,"last_batch_digest":"sha256:#batch#","last_sequence":"1","source_epoch_id":"epoch:a","source_id":"source:a"}],` +
	`"evaluate_monotonic":{"clock_epoch_id":"clock-epoch:publication","nanoseconds":"100"},` +
	`"evaluated_at":{"clock_id":"clock.utc","uncertainty_ns":"0","unix_nanoseconds":"201"},` +
	`"facts":[{"asset_id":"asset:site","candidates":[{"binding_id":"binding:a","candidate_id":"candidate:binding:a","driver_generation":"1","evidence":[{"access":"public","contract":"test.contract/v1","digest":"sha256:#1#","kind":"evidence.test","owner":"owner.test","redaction":"none"}],"freshness_policy":{"fresh_for_ns":"30","max_wall_uncertainty_ns":"1","policy_id":"policy:a","retain_for_ns":"300","version":"1.0.0"},"key":{"dimensions":[],"fact_id":"fact.power","pack_id":"pack.test","pack_version":"1.0.0"},"origin":{"binding_id":"binding:a","evidence":[{"access":"public","contract":"test.contract/v1","digest":"sha256:#4#","kind":"evidence.test","owner":"owner.test","redaction":"none"}],"kind":"native_observation","origin_id":"origin:candidate:binding:a","source_epoch_id":"epoch:a","source_id":"source:a"},"quality":{"assertion":"observed","availability":"available","freshness":"fresh","promotion":"promoted","qualification":"qualified","reasons":[],"validity":"good"},"revision":"1","source_epoch_id":"epoch:a","times":{"evaluate_monotonic":{"clock_epoch_id":"clock:a","nanoseconds":"6"},"evaluated_at":{"clock_id":"clock.utc","uncertainty_ns":"0","unix_nanoseconds":"101"},"receipt_monotonic":{"clock_epoch_id":"clock:a","nanoseconds":"5"},"received_at":{"clock_id":"clock.utc","uncertainty_ns":"0","unix_nanoseconds":"100"}},"value":{"boolean":true,"kind":"boolean"}}],"conflicts":[],"key":{"dimensions":[],"fact_id":"fact.power","pack_id":"pack.test","pack_version":"1.0.0"},"revision":"1"}],` +
	`"fences":[],` +
	`"identity_links":[{"asset_id":"asset:site","basis":[{"access":"public","contract":"test.contract/v1","digest":"sha256:#3#","kind":"evidence.test","owner":"owner.test","redaction":"none"}],"binding_id":"binding:a","revision":"1","state":"qualified"}],` +
	`"revisions":{"capabilities":"1","facts":"1","identity":"1","semantic":"1","services":"1"},` +
	`"services":[{"asset_id":"asset:site","availability":"available","binding_id":"binding:a","definition":{"id":"service.test","pack":{"id":"pack.test","version":"1.0.0"},"version":"1.0.0"},"driver_generation":"1","instance_id":"service:binding:a","qualification":"qualified","revision":"1","source_epoch_id":"epoch:a"}],` +
	`"sources":[{"profile_id":"profile.test","profile_version":"release-1","protocol_id":"protocol.test","registry_evidence":{"access":"public","contract":"test.contract/v1","digest":"sha256:#1#","kind":"evidence.test","owner":"owner.test","redaction":"none"},"revision":"1","source_epoch_id":"epoch:a","source_id":"source:a","started_at":{"clock_id":"clock.utc","uncertainty_ns":"0","unix_nanoseconds":"100"},"state":"current"}]}`

var graphPinnedDigest = regexp.MustCompile(`#([0-9a-f])#`)

func graphPinnedBytes(template string, batchHex string) []byte {
	expanded := strings.ReplaceAll(template, "#batch#", batchHex)
	return []byte(graphPinnedDigest.ReplaceAllStringFunc(expanded, func(match string) string {
		return strings.Repeat(match[1:2], 64)
	}))
}

func graphSHA256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// graphWithoutMember returns the JCS bytes of a JSON object without one member.
func graphWithoutMember(t *testing.T, raw []byte, omit string) []byte {
	t.Helper()
	node, duplicate, err := parseJSON(raw)
	if err != nil || duplicate || node.kind != 'o' {
		t.Fatalf("not a strict JSON object: duplicate=%v err=%v", duplicate, err)
	}
	filtered := node.object[:0]
	for _, member := range node.object {
		if member.key != omit {
			filtered = append(filtered, member)
		}
	}
	node.object = filtered
	var out bytes.Buffer
	if err := writeCanonical(&out, node); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// graphIdentityKey computes an anchor key as the contract defines it: sha256
// over the JCS bytes of the identity object {scheme, issuer, token}.
func graphIdentityKey(t *testing.T, scheme, issuer, token string) Digest {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"scheme": scheme, "issuer": issuer, "token": token})
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	return Digest("sha256:" + graphSHA256Hex(canonical))
}

func graphDefinition(id DefinitionID) DefinitionRef {
	return DefinitionRef{Pack: PackRef{ID: "pack.test", Version: "1.0.0"}, ID: id, Version: "1.0.0"}
}

func graphOperatorOrigin() OriginRef {
	return OriginRef{OriginID: "origin:operator", Kind: OriginOperator, Evidence: []EvidenceRef{publicEvidence("6")}}
}

func graphSlotResource(id ResourceID) Resource {
	slot := "slot-1"
	return Resource{ResourceID: id, Kind: "resource.equipment", Anchor: ResourceAnchor{Class: AnchorEquipment, Basis: AnchorOperatorSlot, Slot: &slot, Evidence: []EvidenceRef{publicEvidence("6")}}, State: ResourceCurrent, Origin: graphOperatorOrigin(), Revision: "1"}
}

func graphNativeResource(t *testing.T, id ResourceID) Resource {
	t.Helper()
	key := graphIdentityKey(t, "scheme.test", "", "token-1")
	previousSlot := "slot-1"
	binding, source, epoch := NativeBindingID("binding:a"), SourceID("source:a"), SourceEpochID("epoch:a")
	return Resource{
		ResourceID: id, Kind: "resource.equipment",
		Anchor:     ResourceAnchor{Class: AnchorEquipment, Basis: AnchorNativeIdentity, Key: &key, Evidence: []EvidenceRef{publicEvidence("7")}},
		Supersedes: &AnchorSupersession{Previous: ResourceAnchor{Class: AnchorEquipment, Basis: AnchorOperatorSlot, Slot: &previousSlot, Evidence: []EvidenceRef{publicEvidence("6")}}, Evidence: []EvidenceRef{publicEvidence("8")}},
		State:      ResourceCurrent,
		Origin:     OriginRef{OriginID: "origin:native", Kind: OriginNativeObservation, SourceID: &source, SourceEpochID: &epoch, BindingID: &binding, Evidence: []EvidenceRef{publicEvidence("4")}},
		Revision:   "2",
	}
}

func graphComponentResource(id, parent ResourceID) Resource {
	slot := "input-1"
	r := graphSlotResource(id)
	r.Kind, r.Anchor.Class, r.Anchor.Basis, r.Anchor.Parent, r.Anchor.Slot = "resource.input", AnchorComponent, AnchorNativeSlot, &parent, &slot
	return r
}

func graphRelation(definition DefinitionRef, from, to ResourceID) RelationInstance {
	return RelationInstance{Definition: definition, From: from, To: to, State: LinkQualified, BasisClass: RelationConfiguration, Basis: []EvidenceRef{publicEvidence("9")}, Origin: graphOperatorOrigin(), Revision: "1"}
}

func graphAbsence(definition DefinitionRef, resource ResourceID, side AbsenceSide) RelationAbsence {
	return RelationAbsence{Definition: definition, Resource: resource, Side: side, State: LinkCandidate, BasisClass: RelationConfiguration, Basis: []EvidenceRef{publicEvidence("9")}, Origin: graphOperatorOrigin(), Revision: "1"}
}

// graphBatch is a valid first batch, optionally carrying every graph member.
func graphBatch(t *testing.T, withGraph bool) PublicationBatch {
	t.Helper()
	batch := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
	if withGraph {
		resource := ResourceID("resource:inverter")
		key := graphIdentityKey(t, "scheme.test", "", "token-1")
		batch.IdentityLinkUpserts[0].Resource, batch.IdentityLinkUpserts[0].Key = &resource, &key
		batch.ServiceUpserts[0].Resource = &resource
		batch.ResourceUpserts = []Resource{graphNativeResource(t, "resource:inverter"), graphComponentResource("resource:inverter:input", "resource:inverter")}
		batch.RelationUpserts = []RelationInstance{graphRelation(graphDefinition("pack.relation.contains"), "resource:inverter", "resource:inverter:input")}
		batch.RelationWithdrawals = []RelationKey{{Definition: graphDefinition("pack.relation.measures"), From: "resource:meter", To: "resource:inverter"}}
		batch.AbsenceUpserts = []RelationAbsence{graphAbsence(graphDefinition("pack.relation.supplies"), "resource:inverter", AbsenceTo)}
		batch.AbsenceWithdrawals = []RelationAbsenceKey{{Definition: graphDefinition("pack.relation.supplies"), Resource: "resource:inverter", Side: AbsenceFrom}}
	}
	sealPublicationBatch(t, &batch)
	return batch
}

// graphSnapshot is the kernel snapshot of the plain first batch, optionally
// carrying graph members added afterwards with a recomputed identifier.
func graphSnapshot(t *testing.T, withGraph bool) Snapshot {
	t.Helper()
	kernel := newTestPublicationKernel(t, "asset:site")
	snapshot, _, err := kernel.Apply(graphBatch(t, false), publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	if withGraph {
		resource := ResourceID("resource:inverter")
		key := graphIdentityKey(t, "scheme.test", "", "token-1")
		snapshot.IdentityLinks[0].Resource, snapshot.IdentityLinks[0].Key = &resource, &key
		snapshot.Services[0].Resource = &resource
		snapshot.Revisions.Structure = "1"
		snapshot.Resources = []Resource{graphNativeResource(t, "resource:inverter"), graphComponentResource("resource:inverter:input", "resource:inverter")}
		snapshot.Relations = []RelationInstance{graphRelation(graphDefinition("pack.relation.contains"), "resource:inverter", "resource:inverter:input")}
		snapshot.RelationAbsences = []RelationAbsence{graphAbsence(graphDefinition("pack.relation.supplies"), "resource:inverter", AbsenceTo)}
		recomputeSnapshotID(t, &snapshot)
	}
	return snapshot
}

func graphReseal(snapshot *Snapshot) {
	if id, err := snapshot.computedID(); err == nil {
		snapshot.SnapshotID = id
	}
}

func graphInsertAfter(t *testing.T, raw []byte, anchor, insert string) []byte {
	t.Helper()
	if bytes.Count(raw, []byte(anchor)) != 1 {
		t.Fatalf("anchor %q is not unique in %s", anchor, raw)
	}
	return bytes.Replace(raw, []byte(anchor), []byte(anchor+insert), 1)
}

// graphPrepend inserts members at the start of a JSON object.
func graphPrepend(raw []byte, members string) []byte {
	return append([]byte("{"+members), raw[1:]...)
}

func graphReplaceOnce(t *testing.T, raw []byte, old, replacement string) []byte {
	t.Helper()
	if bytes.Count(raw, []byte(old)) != 1 {
		t.Fatalf("%q is not unique in %s", old, raw)
	}
	return bytes.Replace(raw, []byte(old), []byte(replacement), 1)
}

func graphCanonical(t *testing.T, record Record) []byte {
	t.Helper()
	canonical, err := CanonicalJSON(record)
	if err != nil {
		t.Fatalf("%T canonical: %v", record, err)
	}
	return canonical
}

func graphRoundTrip[T Record](t *testing.T, record T) []byte {
	t.Helper()
	canonical := graphCanonical(t, record)
	decoded, err := Decode[T](canonical)
	if err != nil {
		t.Fatalf("%T decode: %v; raw=%s", record, err, canonical)
	}
	again := graphCanonical(t, decoded)
	if !bytes.Equal(canonical, again) {
		t.Fatalf("%T round trip changed bytes:\n%s\n%s", record, canonical, again)
	}
	if !reflect.DeepEqual(record, decoded) {
		t.Fatalf("%T round trip changed the value:\n%+v\n%+v", record, record, decoded)
	}
	return canonical
}

// (a) A batch and a snapshot without graph members keep their exact bytes,
// digest and identifier, including when a typed caller sets empty graph
// collections.
func TestGraphRecordBytesUnchangedWithoutGraphMembers(t *testing.T) {
	wantBatch := graphPinnedBytes(graphPinnedBatchInput, "")
	batch := graphBatch(t, false)
	raw, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	if got := graphWithoutMember(t, raw, "batch_digest"); !bytes.Equal(got, wantBatch) {
		t.Fatalf("batch digest input changed:\n got %s\nwant %s", got, wantBatch)
	}
	batchHex := graphSHA256Hex(wantBatch)
	if batch.BatchDigest != Digest("sha256:"+batchHex) {
		t.Fatalf("batch digest changed: %s", batch.BatchDigest)
	}
	if got := graphWithoutMember(t, graphCanonical(t, batch), "batch_digest"); !bytes.Equal(got, wantBatch) {
		t.Fatalf("canonical batch changed: %s", got)
	}

	emptyGraph := batch
	emptyGraph.ResourceUpserts, emptyGraph.RelationUpserts, emptyGraph.RelationWithdrawals = []Resource{}, []RelationInstance{}, []RelationKey{}
	emptyGraph.AbsenceUpserts, emptyGraph.AbsenceWithdrawals = []RelationAbsence{}, []RelationAbsenceKey{}
	if digest, err := emptyGraph.ComputedDigest(); err != nil || digest != batch.BatchDigest {
		t.Fatalf("empty graph collections changed the batch digest: %s %v", digest, err)
	}
	if !bytes.Equal(graphCanonical(t, emptyGraph), graphCanonical(t, batch)) {
		t.Fatal("empty graph collections changed the canonical batch")
	}

	kernel := newTestPublicationKernel(t, "asset:site")
	snapshot, canonical, err := kernel.Apply(batch, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	wantSnapshot := graphPinnedBytes(graphPinnedSnapshotInput, batchHex)
	if got := graphWithoutMember(t, canonical, "snapshot_id"); !bytes.Equal(got, wantSnapshot) {
		t.Fatalf("snapshot identity input changed:\n got %s\nwant %s", got, wantSnapshot)
	}
	if snapshot.SnapshotID != SnapshotID("sha256:"+graphSHA256Hex(wantSnapshot)) {
		t.Fatalf("snapshot identifier changed: %s", snapshot.SnapshotID)
	}
	decoded, err := Decode[Snapshot](canonical)
	if err != nil || !bytes.Equal(graphCanonical(t, decoded), canonical) {
		t.Fatalf("pinned snapshot does not decode to its bytes: %v", err)
	}
	emptySnapshot := snapshot
	emptySnapshot.Resources, emptySnapshot.Relations, emptySnapshot.RelationAbsences = []Resource{}, []RelationInstance{}, []RelationAbsence{}
	if id, err := emptySnapshot.computedID(); err != nil || id != snapshot.SnapshotID || !bytes.Equal(graphCanonical(t, emptySnapshot), canonical) {
		t.Fatalf("empty graph collections changed the snapshot: %s %v", id, err)
	}
	if bytes.Contains(canonical, []byte(`"structure"`)) || snapshot.Revisions.Structure != "" {
		t.Fatalf("a snapshot without structural change carries a structure revision: %s", canonical)
	}
}

// (b) Every new record, and every record that gained a graph member, round
// trips through canonical JSON with the member present.
func TestGraphRecordCanonicalRoundTrip(t *testing.T) {
	native := graphNativeResource(t, "resource:inverter")
	graphRoundTrip(t, native)
	graphRoundTrip(t, graphSlotResource("resource:logger"))
	graphRoundTrip(t, graphComponentResource("resource:inverter:input", "resource:inverter"))
	graphRoundTrip(t, native.Anchor)
	graphRoundTrip(t, *native.Supersedes)
	graphRoundTrip(t, graphRelation(graphDefinition("pack.relation.contains"), "resource:a", "resource:b"))
	graphRoundTrip(t, RelationKey{Definition: graphDefinition("pack.relation.contains"), From: "resource:a", To: "resource:b"})
	graphRoundTrip(t, graphAbsence(graphDefinition("pack.relation.supplies"), "resource:a", AbsenceFrom))
	graphRoundTrip(t, RelationAbsenceKey{Definition: graphDefinition("pack.relation.supplies"), Resource: "resource:a", Side: AbsenceTo})
	for _, state := range []ResourceState{ResourceCurrent, ResourceWithdrawn, ResourceRetired} {
		r := graphSlotResource("resource:logger")
		r.State = state
		graphRoundTrip(t, r)
	}
	for _, class := range []AnchorClass{AnchorEquipment, AnchorComponent, AnchorFunction, AnchorEpisode} {
		r := graphSlotResource("resource:logger")
		r.Anchor.Class = class
		graphRoundTrip(t, r)
	}
	for _, basis := range []RelationBasis{RelationNativeObservation, RelationConfiguration, RelationGateRule} {
		relation := graphRelation(graphDefinition("pack.relation.contains"), "resource:a", "resource:b")
		relation.BasisClass = basis
		graphRoundTrip(t, relation)
		absence := graphAbsence(graphDefinition("pack.relation.supplies"), "resource:a", AbsenceFrom)
		absence.BasisClass = basis
		graphRoundTrip(t, absence)
	}
	for _, state := range []LinkState{LinkCandidate, LinkQualified, LinkRejected, LinkConflict} {
		relation := graphRelation(graphDefinition("pack.relation.contains"), "resource:a", "resource:b")
		relation.State = state
		graphRoundTrip(t, relation)
	}
	resource := ResourceID("resource:inverter")
	key := graphIdentityKey(t, "scheme.test", "issuer-1", "token-1")
	link := publicationLink("asset:site", "binding:a")
	link.Resource, link.Key = &resource, &key
	if canonical := graphRoundTrip(t, link); !bytes.Contains(canonical, []byte(`"resource":"resource:inverter"`)) || !bytes.Contains(canonical, []byte(`"key":"`+string(key)+`"`)) {
		t.Fatalf("identity link graph members missing: %s", canonical)
	}
	service := validService()
	service.Resource = &resource
	if canonical := graphRoundTrip(t, service); !bytes.Contains(canonical, []byte(`"resource":"resource:inverter"`)) {
		t.Fatalf("service resource missing: %s", canonical)
	}

	batch := graphBatch(t, true)
	canonical := graphCanonical(t, batch)
	decodedBatch, err := Decode[PublicationBatch](canonical)
	if err != nil || !bytes.Equal(graphCanonical(t, decodedBatch), canonical) {
		t.Fatalf("batch with graph members does not round trip: %v", err)
	}
	for _, member := range []string{"resource_upserts", "relation_upserts", "relation_withdrawals", "absence_upserts", "absence_withdrawals"} {
		if !bytes.Contains(canonical, []byte(`"`+member+`":[{`)) {
			t.Fatalf("batch member %s missing: %s", member, canonical)
		}
	}
	snapshot := graphSnapshot(t, true)
	canonical = graphCanonical(t, snapshot)
	decodedSnapshot, err := Decode[Snapshot](canonical)
	if err != nil || !bytes.Equal(graphCanonical(t, decodedSnapshot), canonical) {
		t.Fatalf("snapshot with graph members does not round trip: %v", err)
	}
	for _, member := range []string{`"resources":[{`, `"relations":[{`, `"relation_absences":[{`, `"structure":"1"`} {
		if !bytes.Contains(canonical, []byte(member)) {
			t.Fatalf("snapshot member %s missing: %s", member, canonical)
		}
	}
	if snapshot.SnapshotID == graphSnapshot(t, false).SnapshotID {
		t.Fatal("graph members do not enter the snapshot identifier")
	}
}

// (c) A present empty graph collection, or a present empty structure revision,
// is rejected rather than decoded to the omitted form.
func TestGraphRecordPresentEmptyCollectionIsInvalidValue(t *testing.T) {
	batch := graphCanonical(t, graphBatch(t, false))
	for _, member := range []string{"resource_upserts", "relation_upserts", "relation_withdrawals", "absence_upserts", "absence_withdrawals"} {
		_, err := Decode[PublicationBatch](graphPrepend(batch, `"`+member+`":[],`))
		requireID(t, err, InvalidValue)
	}
	snapshot := graphCanonical(t, graphSnapshot(t, false))
	for _, member := range []string{"resources", "relations", "relation_absences"} {
		_, err := Decode[Snapshot](graphPrepend(snapshot, `"`+member+`":[],`))
		requireID(t, err, InvalidValue)
	}
	// The pre-existing optional retained_observations keeps its behaviour.
	if _, err := Decode[Snapshot](graphPrepend(snapshot, `"retained_observations":[],`)); err != nil {
		t.Fatalf("retained observations presence rule changed: %v", err)
	}
	for _, insert := range []string{`"structure":"",`, `"structure":"0",`} {
		_, err := Decode[Snapshot](graphInsertAfter(t, snapshot, `"revisions":{`, insert))
		requireID(t, err, InvalidIdentifier)
		_, err = Decode[RevisionVector](graphPrepend(graphCanonical(t, graphSnapshot(t, false).Revisions), insert))
		requireID(t, err, InvalidIdentifier)
	}
	_, err := Decode[RevisionVector](graphPrepend(graphCanonical(t, graphSnapshot(t, false).Revisions), `"structure":1,`))
	requireID(t, err, InvalidValue)
}

// (d) G-NEG-REL-4 and its siblings: an unknown member in any new record, alone
// or nested in a batch or a snapshot, is unknown_member.
func TestGraphRecordUnknownMemberIsRejected(t *testing.T) {
	const extra = `"plane_id":"energy:zone:1",`
	check := func(name string, decode func([]byte) error, raw []byte) {
		t.Helper()
		if err := decode(raw); err != nil {
			t.Fatalf("%s: the unmodified record does not decode: %v", name, err)
		}
		requireID(t, decode(graphPrepend(raw, extra)), UnknownMember)
	}
	native := graphNativeResource(t, "resource:inverter")
	check("Resource", func(raw []byte) error { _, err := Decode[Resource](raw); return err }, graphCanonical(t, native))
	check("ResourceAnchor", func(raw []byte) error { _, err := Decode[ResourceAnchor](raw); return err }, graphCanonical(t, native.Anchor))
	check("AnchorSupersession", func(raw []byte) error { _, err := Decode[AnchorSupersession](raw); return err }, graphCanonical(t, *native.Supersedes))
	check("RelationInstance", func(raw []byte) error { _, err := Decode[RelationInstance](raw); return err }, graphCanonical(t, graphRelation(graphDefinition("pack.relation.contains"), "resource:a", "resource:b")))
	check("RelationKey", func(raw []byte) error { _, err := Decode[RelationKey](raw); return err }, graphCanonical(t, RelationKey{Definition: graphDefinition("pack.relation.contains"), From: "resource:a", To: "resource:b"}))
	check("RelationAbsence", func(raw []byte) error { _, err := Decode[RelationAbsence](raw); return err }, graphCanonical(t, graphAbsence(graphDefinition("pack.relation.supplies"), "resource:a", AbsenceTo)))
	check("RelationAbsenceKey", func(raw []byte) error { _, err := Decode[RelationAbsenceKey](raw); return err }, graphCanonical(t, RelationAbsenceKey{Definition: graphDefinition("pack.relation.supplies"), Resource: "resource:a", Side: AbsenceTo}))

	batch := graphCanonical(t, graphBatch(t, true))
	for _, anchor := range []string{`"resource_upserts":[{`, `"relation_upserts":[{`, `"relation_withdrawals":[{`, `"absence_upserts":[{`, `"absence_withdrawals":[{`, `"anchor":{"basis":"native_identity",`, `"supersedes":{`} {
		_, err := Decode[PublicationBatch](graphInsertAfter(t, batch, anchor, extra))
		requireID(t, err, UnknownMember)
	}
	snapshot := graphCanonical(t, graphSnapshot(t, true))
	for _, anchor := range []string{`"resources":[{`, `"relations":[{`, `"relation_absences":[{`} {
		_, err := Decode[Snapshot](graphInsertAfter(t, snapshot, anchor, extra))
		requireID(t, err, UnknownMember)
	}
}

type graphWireMember struct {
	name, goType string
	optional     bool
}

func graphWireMembers(t reflect.Type) []graphWireMember {
	var members []graphWireMember
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		parts := strings.Split(field.Tag.Get("json"), ",")
		members = append(members, graphWireMember{parts[0], field.Type.String(), len(parts) > 1 && parts[1] == "omitempty"})
	}
	return members
}

// (e) Every member of every new struct, in the order of the contract's
// sketches, and the exact graph members added to the existing records.
func TestGraphRecordMembersMatchContractSketches(t *testing.T) {
	definition, evidence, origin, revision := graphWireMember{"definition", "semreg.DefinitionRef", false}, "[]semreg.EvidenceRef", graphWireMember{"origin", "semreg.OriginRef", false}, graphWireMember{"revision", "semreg.Uint64", false}
	want := map[reflect.Type][]graphWireMember{
		reflect.TypeOf(Resource{}): {
			{"resource_id", "semreg.ResourceID", false}, {"kind", "semreg.DefinitionID", false}, {"anchor", "semreg.ResourceAnchor", false},
			{"supersedes", "*semreg.AnchorSupersession", true}, {"state", "semreg.ResourceState", false}, origin, revision,
		},
		reflect.TypeOf(ResourceAnchor{}): {
			{"class", "semreg.AnchorClass", false}, {"basis", "semreg.AnchorBasis", false}, {"key", "*semreg.Digest", true},
			{"parent", "*semreg.ResourceID", true}, {"slot", "*string", true}, {"evidence", evidence, false},
		},
		reflect.TypeOf(AnchorSupersession{}): {{"previous", "semreg.ResourceAnchor", false}, {"evidence", evidence, false}},
		reflect.TypeOf(RelationInstance{}): {
			definition, {"from", "semreg.ResourceID", false}, {"to", "semreg.ResourceID", false}, {"state", "semreg.LinkState", false},
			{"basis_class", "semreg.RelationBasis", false}, {"basis", evidence, false}, origin, revision,
		},
		reflect.TypeOf(RelationKey{}): {definition, {"from", "semreg.ResourceID", false}, {"to", "semreg.ResourceID", false}},
		reflect.TypeOf(RelationAbsence{}): {
			definition, {"resource", "semreg.ResourceID", false}, {"side", "semreg.AbsenceSide", false}, {"state", "semreg.LinkState", false},
			{"basis_class", "semreg.RelationBasis", false}, {"basis", evidence, false}, origin, revision,
		},
		reflect.TypeOf(RelationAbsenceKey{}): {definition, {"resource", "semreg.ResourceID", false}, {"side", "semreg.AbsenceSide", false}},
	}
	for typ, members := range want {
		if got := graphWireMembers(typ); !reflect.DeepEqual(got, members) {
			t.Errorf("%s members:\n got %+v\nwant %+v", typ, got, members)
		}
	}
	if typ := reflect.TypeOf(ResourceID("")); typ.Kind() != reflect.String || typ == reflect.TypeOf(OpaqueID("")) || typ == reflect.TypeOf(AssetID("")) {
		t.Fatal("ResourceID is not a distinct opaque identifier")
	}

	// The records that predate the graph keep every required member and gain
	// only the optional graph members.
	gained := map[reflect.Type][]graphWireMember{
		reflect.TypeOf(IdentityLink{}):    {{"resource", "*semreg.ResourceID", true}, {"key", "*semreg.Digest", true}},
		reflect.TypeOf(ServiceInstance{}): {{"resource", "*semreg.ResourceID", true}},
		reflect.TypeOf(PublicationBatch{}): {
			{"resource_upserts", "[]semreg.Resource", true}, {"relation_upserts", "[]semreg.RelationInstance", true},
			{"relation_withdrawals", "[]semreg.RelationKey", true}, {"absence_upserts", "[]semreg.RelationAbsence", true},
			{"absence_withdrawals", "[]semreg.RelationAbsenceKey", true},
		},
		reflect.TypeOf(Snapshot{}): {
			{"retained_observations", "[]semreg.RetainedObservationRecord", true}, {"resources", "[]semreg.Resource", true},
			{"relations", "[]semreg.RelationInstance", true}, {"relation_absences", "[]semreg.RelationAbsence", true},
		},
		reflect.TypeOf(RevisionVector{}): {{"structure", "semreg.Uint64", true}},
	}
	required := map[reflect.Type]string{
		reflect.TypeOf(IdentityLink{}):     "asset_id binding_id state basis revision",
		reflect.TypeOf(ServiceInstance{}):  "instance_id asset_id definition binding_id source_epoch_id driver_generation qualification availability revision",
		reflect.TypeOf(PublicationBatch{}): "contract batch_id batch_digest asset_id source_id source_epoch_id driver_generation sequence expected_semantic_revision observed_at source_upserts source_retirements binding_upserts identity_link_upserts fact_upserts fact_withdrawals service_upserts service_withdrawals capability_upserts capability_withdrawals generation_fences",
		reflect.TypeOf(Snapshot{}):         "contract snapshot_id asset_id revisions evaluated_at evaluate_monotonic sources bindings identity_links facts services capabilities fences cursors",
		reflect.TypeOf(RevisionVector{}):   "semantic identity facts services capabilities",
	}
	for typ, members := range gained {
		var optional []graphWireMember
		var names []string
		for _, member := range graphWireMembers(typ) {
			if member.optional {
				optional = append(optional, member)
			} else {
				names = append(names, member.name)
			}
		}
		if !reflect.DeepEqual(optional, members) {
			t.Errorf("%s optional members:\n got %+v\nwant %+v", typ, optional, members)
		}
		if got := strings.Join(names, " "); got != required[typ] {
			t.Errorf("%s required members changed:\n got %s\nwant %s", typ, got, required[typ])
		}
	}
	if got := graphOptionalMembers[reflect.TypeOf(PublicationBatch{})]; len(got) != len(gained[reflect.TypeOf(PublicationBatch{})]) {
		t.Fatalf("present-empty rule does not cover every batch graph collection: %v", got)
	}
	if got := graphOptionalMembers[reflect.TypeOf(Snapshot{})]; len(got) != len(gained[reflect.TypeOf(Snapshot{})])-1 {
		t.Fatalf("present-empty rule does not cover every snapshot graph collection: %v", got)
	}
}

// (f) structure is omitted until set, present afterwards and always positive.
func TestGraphRecordStructureOmittedUntilSet(t *testing.T) {
	vector := RevisionVector{Semantic: "1", Identity: "1", Facts: "1", Services: "1", Capabilities: "1"}
	if canonical := graphCanonical(t, vector); bytes.Contains(canonical, []byte("structure")) {
		t.Fatalf("unset structure is serialized: %s", canonical)
	}
	vector.Structure = "3"
	if canonical := graphRoundTrip(t, vector); !bytes.Contains(canonical, []byte(`"structure":"3"`)) {
		t.Fatalf("set structure is not serialized: %s", canonical)
	}
	vector.Structure = "0"
	requireID(t, vector.Validate(), InvalidIdentifier)
	vector.Structure = "01"
	requireID(t, vector.Validate(), InvalidIdentifier)

	kernel := newTestPublicationKernel(t, "asset:site")
	first := graphBatch(t, false)
	if _, _, err := kernel.Apply(first, publicationMonotonic); err != nil {
		t.Fatal(err)
	}
	second := publicationBatch("asset:site", "source:a", "epoch:a", "1", "2", "1")
	second.FactWithdrawals = []CandidateID{"candidate:binding:a"}
	sealPublicationBatch(t, &second)
	snapshot, canonical, err := kernel.Apply(second, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revisions.Structure != "" || bytes.Contains(canonical, []byte(`"structure"`)) {
		t.Fatalf("a kernel without structural change set structure: %s", canonical)
	}
	if snapshot.Revisions.Semantic != "2" || snapshot.Revisions.Facts != "2" {
		t.Fatalf("revision vector did not advance as before: %+v", snapshot.Revisions)
	}
}

// Value shapes of the new records: enumerations, slot text, evidence lists,
// key presence and revisions. No publication rule is applied.
func TestGraphRecordValueShapes(t *testing.T) {
	type mutation struct {
		name   string
		record func() Record
		want   ErrorID
	}
	resource := func(change func(*Resource)) func() Record {
		return func() Record {
			r := graphSlotResource("resource:logger")
			change(&r)
			return r
		}
	}
	native := func(change func(*Resource)) func() Record {
		return func() Record {
			r := graphNativeResource(t, "resource:inverter")
			change(&r)
			return r
		}
	}
	relation := func(change func(*RelationInstance)) func() Record {
		return func() Record {
			r := graphRelation(graphDefinition("pack.relation.contains"), "resource:a", "resource:b")
			change(&r)
			return r
		}
	}
	absence := func(change func(*RelationAbsence)) func() Record {
		return func() Record {
			a := graphAbsence(graphDefinition("pack.relation.supplies"), "resource:a", AbsenceTo)
			change(&a)
			return a
		}
	}
	text := func(s string) *string { return &s }
	empty := ResourceID("")
	badKey := Digest("sha256:" + strings.Repeat("G", 64))
	someKey := graphIdentityKey(t, "scheme.test", "", "token-2")
	cases := []mutation{
		{"resource id empty", resource(func(r *Resource) { r.ResourceID = "" }), InvalidIdentifier},
		{"resource id syntax", resource(func(r *Resource) { r.ResourceID = "resource logger" }), InvalidIdentifier},
		{"kind syntax", resource(func(r *Resource) { r.Kind = "Equipment" }), InvalidIdentifier},
		{"resource state", resource(func(r *Resource) { r.State = "active" }), InvalidEnum},
		{"resource revision zero", resource(func(r *Resource) { r.Revision = "0" }), InvalidIdentifier},
		{"resource revision syntax", resource(func(r *Resource) { r.Revision = "01" }), InvalidIdentifier},
		{"resource origin", resource(func(r *Resource) { r.Origin.Kind = "guess" }), InvalidEnum},
		{"anchor class", resource(func(r *Resource) { r.Anchor.Class = "unit" }), InvalidEnum},
		{"anchor basis", resource(func(r *Resource) { r.Anchor.Basis = "address" }), InvalidEnum},
		{"native identity without key", native(func(r *Resource) { r.Anchor.Key = nil }), InvalidValue},
		{"native slot with key", resource(func(r *Resource) { r.Anchor.Basis, r.Anchor.Key = AnchorNativeSlot, &someKey }), InvalidValue},
		{"operator slot with key", resource(func(r *Resource) { r.Anchor.Key = &someKey }), InvalidValue},
		{"key syntax", native(func(r *Resource) { r.Anchor.Key = &badKey }), InvalidValue},
		{"parent syntax", resource(func(r *Resource) { r.Anchor.Parent = &empty }), InvalidIdentifier},
		{"slot empty", resource(func(r *Resource) { r.Anchor.Slot = text("") }), BoundsExceeded},
		{"slot over 128 bytes", resource(func(r *Resource) { r.Anchor.Slot = text(strings.Repeat("\u00e9", 64) + "x") }), BoundsExceeded},
		{"slot not NFC", resource(func(r *Resource) { r.Anchor.Slot = text("zone-é") }), InvalidValue},
		{"slot control character", resource(func(r *Resource) { r.Anchor.Slot = text("zone\t1") }), InvalidValue},
		{"anchor evidence nil", resource(func(r *Resource) { r.Anchor.Evidence = nil }), MissingMember},
		{"anchor evidence empty", resource(func(r *Resource) { r.Anchor.Evidence = []EvidenceRef{} }), BoundsExceeded},
		{"anchor evidence over 32", resource(func(r *Resource) { r.Anchor.Evidence = publicationEvidenceRange(1, 33) }), BoundsExceeded},
		{"anchor evidence duplicate", resource(func(r *Resource) { r.Anchor.Evidence = []EvidenceRef{publicEvidence("6"), publicEvidence("6")} }), DuplicateKey},
		{"anchor evidence order", resource(func(r *Resource) { r.Anchor.Evidence = []EvidenceRef{publicEvidence("7"), publicEvidence("6")} }), NoncanonicalOrder},
		{"supersession evidence nil", native(func(r *Resource) { r.Supersedes.Evidence = nil }), MissingMember},
		{"supersession evidence empty", native(func(r *Resource) { r.Supersedes.Evidence = []EvidenceRef{} }), IdentityDrift},
		{"supersession evidence over 32", native(func(r *Resource) { r.Supersedes.Evidence = publicationEvidenceRange(1, 33) }), BoundsExceeded},
		{"supersession previous", native(func(r *Resource) { r.Supersedes.Previous.Basis = "address" }), InvalidEnum},
		{"relation definition", relation(func(r *RelationInstance) { r.Definition.Version = "1.0" }), InvalidIdentifier},
		{"relation from", relation(func(r *RelationInstance) { r.From = "" }), InvalidIdentifier},
		{"relation to", relation(func(r *RelationInstance) { r.To = "" }), InvalidIdentifier},
		{"relation state withdrawn", relation(func(r *RelationInstance) { r.State = LinkWithdrawn }), InvalidEnum},
		{"relation basis class", relation(func(r *RelationInstance) { r.BasisClass = "similarity" }), InvalidEnum},
		{"relation basis nil", relation(func(r *RelationInstance) { r.Basis = nil }), MissingMember},
		{"relation basis empty", relation(func(r *RelationInstance) { r.Basis = []EvidenceRef{} }), RelationNotQualified},
		{"relation basis over 32", relation(func(r *RelationInstance) { r.Basis = publicationEvidenceRange(1, 33) }), BoundsExceeded},
		{"relation origin", relation(func(r *RelationInstance) { r.Origin.Evidence = []EvidenceRef{} }), BoundsExceeded},
		{"relation revision", relation(func(r *RelationInstance) { r.Revision = "0" }), InvalidIdentifier},
		{"absence resource", absence(func(a *RelationAbsence) { a.Resource = "" }), InvalidIdentifier},
		{"absence side", absence(func(a *RelationAbsence) { a.Side = "both" }), InvalidEnum},
		{"absence state rejected", absence(func(a *RelationAbsence) { a.State = LinkRejected }), InvalidEnum},
		{"absence state conflict", absence(func(a *RelationAbsence) { a.State = LinkConflict }), InvalidEnum},
		{"absence basis class", absence(func(a *RelationAbsence) { a.BasisClass = "ordinal" }), InvalidEnum},
		{"absence basis empty", absence(func(a *RelationAbsence) { a.Basis = []EvidenceRef{} }), RelationNotQualified},
		{"absence revision", absence(func(a *RelationAbsence) { a.Revision = "" }), InvalidIdentifier},
		{"relation key definition", func() Record {
			return RelationKey{Definition: DefinitionRef{Pack: PackRef{ID: "Pack", Version: "1.0.0"}, ID: "pack.relation.contains", Version: "1.0.0"}, From: "resource:a", To: "resource:b"}
		}, InvalidIdentifier},
		{"absence key side", func() Record {
			return RelationAbsenceKey{Definition: graphDefinition("pack.relation.supplies"), Resource: "resource:a", Side: "middle"}
		}, InvalidEnum},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := tc.record()
			requireID(t, record.Validate(), tc.want)
			_, err := CanonicalJSON(record)
			requireID(t, err, tc.want)
		})
	}
	slot := strings.Repeat("\u00e9", 64)
	if err := (resource(func(r *Resource) { r.Anchor.Slot = &slot })()).Validate(); err != nil {
		t.Fatalf("a 128-byte NFC slot is rejected: %v", err)
	}
	if err := (relation(func(r *RelationInstance) { r.Basis = publicationEvidenceRange(1, 32) })()).Validate(); err != nil {
		t.Fatalf("32 basis references are rejected: %v", err)
	}

	// Graph members of records that predate them, typed and on the wire.
	link := publicationLink("asset:site", "binding:a")
	link.Resource = &empty
	_, err := CanonicalJSON(link)
	requireID(t, err, InvalidIdentifier)
	link.Resource, link.Key = nil, &badKey
	_, err = CanonicalJSON(link)
	requireID(t, err, InvalidValue)
	raw := graphCanonical(t, publicationLink("asset:site", "binding:a"))
	_, err = Decode[IdentityLink](graphPrepend(raw, `"resource":"",`))
	requireID(t, err, InvalidIdentifier)
	_, err = Decode[IdentityLink](graphPrepend(raw, `"key":"sha256:00",`))
	requireID(t, err, InvalidValue)
	service := validService()
	service.Resource = &empty
	_, err = CanonicalJSON(service)
	requireID(t, err, InvalidIdentifier)
	_, err = Decode[ServiceInstance](graphPrepend(graphCanonical(t, validService()), `"resource":"a b",`))
	requireID(t, err, InvalidIdentifier)

	// The same members inside a batch and a snapshot.
	batch := graphBatch(t, false)
	batch.IdentityLinkUpserts[0].Resource = &empty
	_, err = batch.ComputedDigest()
	requireID(t, err, InvalidIdentifier)
	batch = graphBatch(t, false)
	batch.ServiceUpserts[0].Resource = &empty
	_, err = batch.ComputedDigest()
	requireID(t, err, InvalidIdentifier)
	batch = graphBatch(t, true)
	batch.RelationUpserts[0].State = LinkConflict
	_, err = batch.ComputedDigest()
	requireID(t, err, InvalidValue)
	batch = graphBatch(t, true)
	batch.ResourceUpserts[1].Anchor.Slot = text("")
	_, err = batch.ComputedDigest()
	requireID(t, err, BoundsExceeded)
	snapshot := graphSnapshot(t, true)
	snapshot.IdentityLinks[0].Key = &badKey
	requireID(t, snapshot.Validate(), InvalidValue)
	snapshot = graphSnapshot(t, true)
	snapshot.Services[0].Resource = &empty
	requireID(t, snapshot.Validate(), InvalidIdentifier)
	snapshot = graphSnapshot(t, true)
	snapshot.Relations[0].Basis = []EvidenceRef{}
	requireID(t, snapshot.Validate(), RelationNotQualified)
	snapshot = graphSnapshot(t, true)
	snapshot.Revisions.Structure = "0"
	requireID(t, snapshot.Validate(), InvalidIdentifier)
	// A kernel-derived conflict is a valid snapshot state.
	snapshot = graphSnapshot(t, true)
	snapshot.Relations[0].State = LinkConflict
	graphReseal(&snapshot)
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("conflict relation rejected in a snapshot: %v", err)
	}
}

// Bounds of the snapshot graph collections.
func TestGraphRecordSnapshotBounds(t *testing.T) {
	base := graphSnapshot(t, false)
	resources := make([]Resource, maxSnapshotResources+1)
	for i := range resources {
		resources[i] = graphSlotResource(ResourceID(fmt.Sprintf("resource:%06d", i)))
	}
	snapshot := base
	snapshot.Resources = resources[:maxSnapshotResources]
	graphReseal(&snapshot)
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("%d resources rejected: %v", maxSnapshotResources, err)
	}
	snapshot.Resources = resources
	requireID(t, snapshot.Validate(), BoundsExceeded)

	absences := make([]RelationAbsence, maxSnapshotAbsences+1)
	for i := range absences {
		absences[i] = graphAbsence(graphDefinition("pack.relation.supplies"), ResourceID(fmt.Sprintf("resource:%06d", i)), AbsenceFrom)
	}
	snapshot = base
	snapshot.RelationAbsences = absences
	requireID(t, snapshot.Validate(), BoundsExceeded)

	relations := make([]RelationInstance, maxSnapshotRelations+1)
	for i := range relations {
		relations[i] = graphRelation(graphDefinition("pack.relation.contains"), "resource:a", ResourceID(fmt.Sprintf("resource:%06d", i)))
	}
	snapshot = base
	snapshot.Relations = relations
	requireID(t, snapshot.Validate(), BoundsExceeded)
}

// Canonical order and uniqueness of every graph collection. Each key field
// is varied alone: a comparison that ignored it would see a duplicate in the
// ascending pair and accept the descending one.
func TestGraphRecordCanonicalOrderAndDuplicates(t *testing.T) {
	base := graphDefinition("pack.relation.contains")
	definitions := map[string][2]DefinitionRef{
		"definition.pack.id":      {base, {Pack: PackRef{ID: "pack.zzz", Version: "1.0.0"}, ID: base.ID, Version: base.Version}},
		"definition.pack.version": {base, {Pack: PackRef{ID: "pack.test", Version: "2.0.0"}, ID: base.ID, Version: base.Version}},
		"definition.id":           {base, {Pack: base.Pack, ID: "pack.relation.supplies", Version: base.Version}},
		"definition.version":      {base, {Pack: base.Pack, ID: base.ID, Version: "2.0.0"}},
	}
	type pair struct {
		field       string
		relations   [2]RelationInstance
		absences    [2]RelationAbsence
		hasRelation bool
		hasAbsence  bool
	}
	var pairs []pair
	for field, defs := range definitions {
		pairs = append(pairs, pair{field: field,
			relations:   [2]RelationInstance{graphRelation(defs[0], "resource:a", "resource:b"), graphRelation(defs[1], "resource:a", "resource:b")},
			absences:    [2]RelationAbsence{graphAbsence(defs[0], "resource:a", AbsenceFrom), graphAbsence(defs[1], "resource:a", AbsenceFrom)},
			hasRelation: true, hasAbsence: true})
	}
	pairs = append(pairs,
		pair{field: "from", relations: [2]RelationInstance{graphRelation(base, "resource:a", "resource:z"), graphRelation(base, "resource:b", "resource:z")}, hasRelation: true},
		pair{field: "to", relations: [2]RelationInstance{graphRelation(base, "resource:a", "resource:a"), graphRelation(base, "resource:a", "resource:b")}, hasRelation: true},
		pair{field: "resource", absences: [2]RelationAbsence{graphAbsence(base, "resource:a", AbsenceTo), graphAbsence(base, "resource:b", AbsenceTo)}, hasAbsence: true},
		pair{field: "side", absences: [2]RelationAbsence{graphAbsence(base, "resource:a", AbsenceFrom), graphAbsence(base, "resource:a", AbsenceTo)}, hasAbsence: true},
	)

	batchError := func(change func(*PublicationBatch)) error {
		batch := graphBatch(t, false)
		change(&batch)
		_, err := batch.ComputedDigest()
		return err
	}
	snapshotError := func(change func(*Snapshot)) error {
		snapshot := graphSnapshot(t, false)
		change(&snapshot)
		graphReseal(&snapshot)
		return snapshot.Validate()
	}
	expect := func(t *testing.T, name string, ascending, descending, duplicate error) {
		t.Helper()
		if ascending != nil {
			t.Fatalf("%s ascending rejected: %v", name, ascending)
		}
		requireID(t, descending, NoncanonicalOrder)
		requireID(t, duplicate, DuplicateKey)
	}
	for _, p := range pairs {
		t.Run(p.field, func(t *testing.T) {
			if p.hasRelation {
				a, z := p.relations[0], p.relations[1]
				ka, kz := RelationKey{a.Definition, a.From, a.To}, RelationKey{z.Definition, z.From, z.To}
				expect(t, "relation upserts",
					batchError(func(b *PublicationBatch) { b.RelationUpserts = []RelationInstance{a, z} }),
					batchError(func(b *PublicationBatch) { b.RelationUpserts = []RelationInstance{z, a} }),
					batchError(func(b *PublicationBatch) { b.RelationUpserts = []RelationInstance{a, a} }))
				expect(t, "relation withdrawals",
					batchError(func(b *PublicationBatch) { b.RelationWithdrawals = []RelationKey{ka, kz} }),
					batchError(func(b *PublicationBatch) { b.RelationWithdrawals = []RelationKey{kz, ka} }),
					batchError(func(b *PublicationBatch) { b.RelationWithdrawals = []RelationKey{ka, ka} }))
				expect(t, "snapshot relations",
					snapshotError(func(s *Snapshot) { s.Relations = []RelationInstance{a, z} }),
					snapshotError(func(s *Snapshot) { s.Relations = []RelationInstance{z, a} }),
					snapshotError(func(s *Snapshot) { s.Relations = []RelationInstance{a, a} }))
			}
			if p.hasAbsence {
				a, z := p.absences[0], p.absences[1]
				ka, kz := RelationAbsenceKey{a.Definition, a.Resource, a.Side}, RelationAbsenceKey{z.Definition, z.Resource, z.Side}
				expect(t, "absence upserts",
					batchError(func(b *PublicationBatch) { b.AbsenceUpserts = []RelationAbsence{a, z} }),
					batchError(func(b *PublicationBatch) { b.AbsenceUpserts = []RelationAbsence{z, a} }),
					batchError(func(b *PublicationBatch) { b.AbsenceUpserts = []RelationAbsence{a, a} }))
				expect(t, "absence withdrawals",
					batchError(func(b *PublicationBatch) { b.AbsenceWithdrawals = []RelationAbsenceKey{ka, kz} }),
					batchError(func(b *PublicationBatch) { b.AbsenceWithdrawals = []RelationAbsenceKey{kz, ka} }),
					batchError(func(b *PublicationBatch) { b.AbsenceWithdrawals = []RelationAbsenceKey{ka, ka} }))
				expect(t, "snapshot absences",
					snapshotError(func(s *Snapshot) { s.RelationAbsences = []RelationAbsence{a, z} }),
					snapshotError(func(s *Snapshot) { s.RelationAbsences = []RelationAbsence{z, a} }),
					snapshotError(func(s *Snapshot) { s.RelationAbsences = []RelationAbsence{a, a} }))
			}
		})
	}
	t.Run("resource_id", func(t *testing.T) {
		a, z := graphSlotResource("resource:a"), graphSlotResource("resource:b")
		expect(t, "resource upserts",
			batchError(func(b *PublicationBatch) { b.ResourceUpserts = []Resource{a, z} }),
			batchError(func(b *PublicationBatch) { b.ResourceUpserts = []Resource{z, a} }),
			batchError(func(b *PublicationBatch) { b.ResourceUpserts = []Resource{a, a} }))
		expect(t, "snapshot resources",
			snapshotError(func(s *Snapshot) { s.Resources = []Resource{a, z} }),
			snapshotError(func(s *Snapshot) { s.Resources = []Resource{z, a} }),
			snapshotError(func(s *Snapshot) { s.Resources = []Resource{a, a} }))
	})
	// The canonical definition is its JCS bytes, not a numeric version order.
	t.Run("definition bytes", func(t *testing.T) {
		low, high := graphDefinition("pack.relation.contains"), graphDefinition("pack.relation.contains")
		low.Version, high.Version = "1.10.0", "1.9.0"
		err := batchError(func(b *PublicationBatch) {
			b.RelationUpserts = []RelationInstance{graphRelation(low, "resource:a", "resource:b"), graphRelation(high, "resource:a", "resource:b")}
		})
		if err != nil {
			t.Fatalf("JCS order of definitions rejected: %v", err)
		}
	})

	// When another member prevents binding, the decoder still reports a
	// duplicate graph key from the wire tree.
	broken := func(raw []byte) []byte {
		return graphReplaceOnce(t, raw, `"sequence":"1"`, `"sequence":1`)
	}
	duplicateBatch := graphBatch(t, true)
	duplicateBatch.ResourceUpserts = []Resource{duplicateBatch.ResourceUpserts[0], duplicateBatch.ResourceUpserts[0]}
	duplicateBatch.RelationUpserts = append(duplicateBatch.RelationUpserts, duplicateBatch.RelationUpserts[0])
	duplicateBatch.RelationWithdrawals = append(duplicateBatch.RelationWithdrawals, duplicateBatch.RelationWithdrawals[0])
	duplicateBatch.AbsenceUpserts = append(duplicateBatch.AbsenceUpserts, duplicateBatch.AbsenceUpserts[0])
	duplicateBatch.AbsenceWithdrawals = append(duplicateBatch.AbsenceWithdrawals, duplicateBatch.AbsenceWithdrawals[0])
	full, err := json.Marshal(duplicateBatch)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range []string{"resource_upserts", "relation_upserts", "relation_withdrawals", "absence_upserts", "absence_withdrawals"} {
		single := duplicateBatch
		single.ResourceUpserts, single.RelationUpserts, single.RelationWithdrawals, single.AbsenceUpserts, single.AbsenceWithdrawals = nil, nil, nil, nil, nil
		switch member {
		case "resource_upserts":
			single.ResourceUpserts = duplicateBatch.ResourceUpserts
		case "relation_upserts":
			single.RelationUpserts = duplicateBatch.RelationUpserts
		case "relation_withdrawals":
			single.RelationWithdrawals = duplicateBatch.RelationWithdrawals
		case "absence_upserts":
			single.AbsenceUpserts = duplicateBatch.AbsenceUpserts
		case "absence_withdrawals":
			single.AbsenceWithdrawals = duplicateBatch.AbsenceWithdrawals
		}
		raw, err := json.Marshal(single)
		if err != nil {
			t.Fatal(err)
		}
		_, err = Decode[PublicationBatch](broken(raw))
		requireID(t, err, DuplicateKey)
	}
	if _, err := Decode[PublicationBatch](broken(full)); ErrorIdentifier(err) != DuplicateKey {
		t.Fatalf("duplicate graph keys not reported: %v", err)
	}
	snapshot := graphSnapshot(t, true)
	for _, change := range []func(*Snapshot){
		func(s *Snapshot) { s.Resources = []Resource{s.Resources[0], s.Resources[0]} },
		func(s *Snapshot) { s.Relations = append(s.Relations, s.Relations[0]) },
		func(s *Snapshot) { s.RelationAbsences = append(s.RelationAbsences, s.RelationAbsences[0]) },
	} {
		duplicate := snapshot
		duplicate.Resources = append([]Resource(nil), snapshot.Resources...)
		duplicate.Relations = append([]RelationInstance(nil), snapshot.Relations...)
		duplicate.RelationAbsences = append([]RelationAbsence(nil), snapshot.RelationAbsences...)
		change(&duplicate)
		raw, err := json.Marshal(duplicate)
		if err != nil {
			t.Fatal(err)
		}
		raw = graphReplaceOnce(t, raw, `"evaluated_at":{"unix_nanoseconds":"201"`, `"evaluated_at":{"unix_nanoseconds":201`)
		_, err = Decode[Snapshot](raw)
		requireID(t, err, DuplicateKey)
	}
}

// A kernel constructed without the element catalog refuses every graph member
// of a batch with membership_missing and changes no state, on Apply and on
// restore. A malformed graph member keeps its earlier structural error.
func TestGraphRecordKernelRefusesGraphMembers(t *testing.T) {
	resource := ResourceID("resource:inverter")
	key := graphIdentityKey(t, "scheme.test", "", "token-1")
	// Each variant adds one graph member to an otherwise acceptable batch; a
	// changed link or service carries the next revision so that only the
	// graph member can be refused.
	variants := map[string]func(*PublicationBatch, Uint64){
		"resource_upserts": func(b *PublicationBatch, _ Uint64) { b.ResourceUpserts = []Resource{graphSlotResource(resource)} },
		"relation_upserts": func(b *PublicationBatch, _ Uint64) {
			b.RelationUpserts = []RelationInstance{graphRelation(graphDefinition("pack.relation.contains"), resource, "resource:b")}
		},
		"relation_withdrawals": func(b *PublicationBatch, _ Uint64) {
			b.RelationWithdrawals = []RelationKey{{Definition: graphDefinition("pack.relation.contains"), From: resource, To: "resource:b"}}
		},
		"absence_upserts": func(b *PublicationBatch, _ Uint64) {
			b.AbsenceUpserts = []RelationAbsence{graphAbsence(graphDefinition("pack.relation.supplies"), resource, AbsenceTo)}
		},
		"absence_withdrawals": func(b *PublicationBatch, _ Uint64) {
			b.AbsenceWithdrawals = []RelationAbsenceKey{{Definition: graphDefinition("pack.relation.supplies"), Resource: resource, Side: AbsenceTo}}
		},
		"identity link resource": func(b *PublicationBatch, revision Uint64) {
			link := publicationLink("asset:site", "binding:a")
			link.Resource, link.Revision = &resource, revision
			b.IdentityLinkUpserts = []IdentityLink{link}
		},
		"identity link key": func(b *PublicationBatch, revision Uint64) {
			link := publicationLink("asset:site", "binding:a")
			link.Key, link.Revision = &key, revision
			b.IdentityLinkUpserts = []IdentityLink{link}
		},
		"service resource": func(b *PublicationBatch, revision Uint64) {
			service := publicationService("asset:site", "service:binding:a", "binding:a", "epoch:a", "1")
			service.Resource, service.Revision = &resource, revision
			b.ServiceUpserts = []ServiceInstance{service}
		},
	}
	for name, change := range variants {
		t.Run(name, func(t *testing.T) {
			kernel := newTestPublicationKernel(t, "asset:site")
			first := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
			change(&first, "1")
			sealPublicationBatch(t, &first)
			if err := first.Validate(); err != nil {
				t.Fatalf("graph member is not a valid record: %v", err)
			}
			assertRejectedUnchanged(t, kernel, first, MembershipMissing)

			plain := graphBatch(t, false)
			if _, _, err := kernel.Apply(plain, publicationMonotonic); err != nil {
				t.Fatal(err)
			}
			second := publicationBatch("asset:site", "source:a", "epoch:a", "1", "2", "1")
			change(&second, "2")
			sealPublicationBatch(t, &second)
			assertRejectedUnchanged(t, kernel, second, MembershipMissing)

			// Without the graph member the same history is accepted.
			control := publicationBatch("asset:site", "source:a", "epoch:a", "1", "2", "1")
			sealPublicationBatch(t, &control)
			fork, err := kernel.Fork()
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := fork.Apply(control, publicationMonotonic); err != nil {
				t.Fatalf("control batch rejected: %v", err)
			}

			restored := newTestPublicationKernel(t, "asset:site")
			err = restored.RestoreAccepted([]AcceptedPublication{{Batch: plain, PublicationMonotonic: publicationMonotonic}, {Batch: second, PublicationMonotonic: publicationMonotonic}})
			requireID(t, err, MembershipMissing)
			if _, _, ok := restored.Current(); ok {
				t.Fatal("refused restore left state behind")
			}
		})
	}
	kernel := newTestPublicationKernel(t, "asset:site")
	malformed := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
	malformed.ResourceUpserts = []Resource{graphSlotResource(resource)}
	malformed.ResourceUpserts[0].State = "active"
	if _, err := malformed.ComputedDigest(); ErrorIdentifier(err) != InvalidEnum {
		t.Fatalf("malformed resource: %v", err)
	}
	malformed.BatchDigest = graphBatch(t, false).BatchDigest
	assertRejectedUnchanged(t, kernel, malformed, InvalidEnum)
}

// The graph errors take the places serialization.md gives them in the
// precedence, and each is an identifier this package accepts.
func TestGraphRecordErrorPrecedence(t *testing.T) {
	order := []ErrorID{
		InvalidJSON, DuplicateKey, InvalidContract, MissingMember, UnknownMember,
		InvalidIdentifier, InvalidDecimal, InvalidValue, InvalidTime, InvalidEvidence, InvalidEnum, BoundsExceeded,
		NoncanonicalOrder, DigestMismatch, DanglingReference, DerivationCycle,
		StaleSourceEpoch, StaleDriverGeneration, StaleRelation, SequenceConflict, RevisionConflict, IncomparableClockEpoch, GenerationTransitionIncomplete,
		DefinitionOwnerConflict, DefinitionOwnerMissing, MembershipMissing, IdentityNotQualified, DuplicateIdentity, IdentityDrift, RelationNotQualified,
		CapabilityNotQualified, CapabilityUnavailable, AuthorityMissing, DeadlineExpired, PreconditionFailed,
		RouteSelectionForbidden, AmbiguousRoute, RetryForbidden,
		InvalidOutcome, EchoSuppressed, CausalBudgetExceeded, ProjectionIncomplete, AliasNotRoutable,
	}
	if len(order) != len(errorRanks) {
		t.Fatalf("precedence lists %d errors, the package ranks %d", len(order), len(errorRanks))
	}
	for i, id := range order {
		if err := id.Validate(); err != nil {
			t.Fatalf("%s is not accepted: %v", id, err)
		}
		if i > 0 && errorRanks[order[i-1]] >= errorRanks[id] {
			t.Fatalf("%s does not follow %s", id, order[i-1])
		}
	}
	for _, id := range []ErrorID{StaleRelation, MembershipMissing, DuplicateIdentity, IdentityDrift, RelationNotQualified} {
		if got := ErrorIdentifier(errID(id, "graph")); got != id {
			t.Fatalf("%s is not stable: %s", id, got)
		}
	}
}
