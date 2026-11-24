package life

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// controlSchemaVersion is the one control-record format this package decodes.
// Any other version is preserved verbatim and fails control closed; it is
// never dropped or read as missing.
const controlSchemaVersion = 1

// controlRecoveryFormat tags the typed recovery envelope.
const controlRecoveryFormat = "semreg.runtime.life.recovery"

// ControlLoadDisposition distinguishes the control-record load outcomes.
// Missing, corrupt, unreadable, unsupported and locked never collapse.
type ControlLoadDisposition string

const (
	// ControlLoadMissing: readable state with no control material at all; the
	// only disposition eligible for the one-time bootstrap seed.
	ControlLoadMissing ControlLoadDisposition = "MISSING"
	// ControlLoadLoaded: a valid namespace.
	ControlLoadLoaded ControlLoadDisposition = "LOADED"
	// ControlLoadCorrupt: whole-file corruption or a namespace whose structure,
	// mark, catalog revision or recovery cannot be validated.
	ControlLoadCorrupt ControlLoadDisposition = "CORRUPT"
	// ControlLoadUnreadable: the state file could not be read.
	ControlLoadUnreadable ControlLoadDisposition = "UNREADABLE"
	// ControlLoadUnsupported: a schema version other than 1.
	ControlLoadUnsupported ControlLoadDisposition = "UNSUPPORTED"
	// ControlLoadLocked: another writer holds the exclusive store lock.
	ControlLoadLocked ControlLoadDisposition = "LOCKED"
)

// Errors returned by the control store outside the refusal table.
var (
	ErrControlInvalidConfig   = errors.New("life: invalid instance configuration")
	ErrControlReadbackPending = errors.New("life: ambiguous persistence awaiting readback")
	ErrControlInvalidRecovery = errors.New("life: invalid recovery envelope")
	ErrControlInvalidWork     = errors.New("life: invalid transition record")
	ErrStatePersistBlocked    = errors.New("life: persistence blocked")
)

var (
	controlEpochPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@-]*$`)
	controlSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

const maxControlEpochBytes = 256

func refuse(code RefusalCode, row int, reason string) *Refusal {
	return &Refusal{Code: code, Row: row, Reason: reason}
}

// ControlInstance is one configured instance and its eligibility, as the
// control store sees it.
type ControlInstance struct {
	InstanceID InstanceID
	Enabled    bool
}

// ControlStartupAction is the startup reconciliation derived from durable
// desired state. It is a plan only; the store opens nothing.
type ControlStartupAction string

// ControlStartupAction values.
const (
	StartupStarting ControlStartupAction = "STARTING"
	StartupStopped  ControlStartupAction = "STOPPED"
	StartupDisabled ControlStartupAction = "DISABLED"
	StartupFailed   ControlStartupAction = "FAILED"
)

// ControlStartup is the derived startup entry of one configured instance.
type ControlStartup struct {
	InstanceID      InstanceID
	Action          ControlStartupAction
	DesiredState    DesiredState
	DesiredRevision DesiredRevision
	Current         *Transition
	Reserved        *Transition
}

type nsRecord struct {
	Desired  DesiredState
	Revision DesiredRevision
	Sequence LifecycleSequence
	Current  *Transition
	Reserved *Transition
}

type failedRecord struct {
	raw    json.RawMessage
	reason string
}

// namespace is the in-memory control namespace. When status is not Loaded, raw
// holds the namespace value verbatim and is re-emitted on every write so
// unsupported or malformed bytes round-trip.
type namespace struct {
	status   ControlLoadDisposition
	detail   string
	raw      json.RawMessage
	mark     DesiredRevision
	catalog  CatalogRevision
	records  map[InstanceID]nsRecord
	failed   map[InstanceID]failedRecord
	recovery *RecoveryMaterial
}

func newLoadedNamespace() *namespace {
	return &namespace{status: ControlLoadLoaded, catalog: 1, records: map[InstanceID]nsRecord{}, failed: map[InstanceID]failedRecord{}}
}

func cloneRecord(r nsRecord) nsRecord {
	r.Current = cloneTransition(r.Current)
	r.Reserved = cloneTransition(r.Reserved)
	return r
}

func (ns *namespace) clone() *namespace {
	if ns == nil {
		return nil
	}
	cp := *ns
	cp.raw = append(json.RawMessage(nil), ns.raw...)
	cp.records = make(map[InstanceID]nsRecord, len(ns.records))
	for id, r := range ns.records {
		cp.records[id] = cloneRecord(r)
	}
	cp.failed = make(map[InstanceID]failedRecord, len(ns.failed))
	for id, f := range ns.failed {
		cp.failed[id] = failedRecord{raw: append(json.RawMessage(nil), f.raw...), reason: f.reason}
	}
	cp.recovery = cloneMaterial(ns.recovery)
	return &cp
}

// --- codec ---

type namespaceDTO struct {
	SchemaVersion      int                        `json:"schema_version"`
	IssuedRevisionMark string                     `json:"issued_revision_mark"`
	CatalogRevision    string                     `json:"catalog_revision"`
	Instances          map[string]json.RawMessage `json:"instances"`
	Recovery           *recoveryDTO               `json:"recovery,omitempty"`
}

type recordDTO struct {
	DesiredState      string         `json:"desired_state"`
	DesiredRevision   string         `json:"desired_revision"`
	LifecycleSequence string         `json:"lifecycle_sequence"`
	Current           *transitionDTO `json:"current"`
	Reserved          *transitionDTO `json:"reserved"`
}

type transitionDTO struct {
	Kind                   string `json:"kind"`
	DesiredRevision        string `json:"desired_revision"`
	Sequence               string `json:"sequence"`
	Phase                  string `json:"phase"`
	Epoch                  string `json:"epoch"`
	Generation             string `json:"generation"`
	NativeClosePending     bool   `json:"native_close_pending"`
	SemanticCleanupPending bool   `json:"semantic_cleanup_pending"`
	AttemptsUsed           int    `json:"attempts_used"`
	DeadlineUTC            string `json:"deadline_utc"`
}

type recoveryDTO struct {
	Format             string   `json:"format"`
	InstanceIDs        []string `json:"instance_ids"`
	MaxDesiredRevision string   `json:"max_desired_revision"`
	PayloadSHA256      string   `json:"payload_sha256"`
	Payload            string   `json:"payload"`
}

func formatCounter(v uint64) string { return strconv.FormatUint(v, 10) }

// parseCounter decodes a canonical decimal string: ASCII digits only, no sign,
// no leading zero except "0", at most the unsigned 64-bit maximum.
func parseCounter(s string, allowZero bool) (uint64, bool) {
	if len(s) == 0 || len(s) > 20 {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	if len(s) > 1 && s[0] == '0' {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil || (v == 0 && !allowZero) {
		return 0, false
	}
	return v, true
}

func transitionToDTO(t *Transition) *transitionDTO {
	if t == nil {
		return nil
	}
	dto := &transitionDTO{
		Kind: string(t.Kind), DesiredRevision: formatCounter(uint64(t.DesiredRevision)), Sequence: formatCounter(uint64(t.Sequence)),
		Phase: string(t.Phase), Epoch: string(t.Epoch), Generation: formatCounter(uint64(t.Generation)),
		NativeClosePending: t.NativeClosePending, SemanticCleanupPending: t.SemanticCleanupPending, AttemptsUsed: t.AttemptsUsed,
	}
	if !t.DeadlineUTC.IsZero() {
		dto.DeadlineUTC = t.DeadlineUTC.UTC().Format(time.RFC3339Nano)
	}
	return dto
}

func recoveryToDTO(r *RecoveryMaterial) *recoveryDTO {
	if r == nil {
		return nil
	}
	sum := sha256.Sum256(r.Payload)
	ids := make([]string, 0, len(r.DriverIDs))
	for _, id := range r.DriverIDs {
		ids = append(ids, string(id))
	}
	return &recoveryDTO{
		Format: controlRecoveryFormat, InstanceIDs: ids, MaxDesiredRevision: formatCounter(uint64(r.MaxDesiredRevision)),
		PayloadSHA256: hex.EncodeToString(sum[:]), Payload: base64.StdEncoding.EncodeToString(r.Payload),
	}
}

// encode returns the namespace value. A non-Loaded namespace is re-emitted
// verbatim. Records are keyed and therefore emitted in bytewise id order;
// invalid records keep their original bytes.
func (ns *namespace) encode() (json.RawMessage, error) {
	if ns.status != ControlLoadLoaded {
		return append(json.RawMessage(nil), ns.raw...), nil
	}
	dto := namespaceDTO{
		SchemaVersion: controlSchemaVersion, IssuedRevisionMark: formatCounter(uint64(ns.mark)), CatalogRevision: formatCounter(uint64(ns.catalog)),
		Instances: make(map[string]json.RawMessage, len(ns.records)+len(ns.failed)), Recovery: recoveryToDTO(ns.recovery),
	}
	for id, r := range ns.records {
		raw, err := json.Marshal(recordDTO{
			DesiredState: string(r.Desired), DesiredRevision: formatCounter(uint64(r.Revision)), LifecycleSequence: formatCounter(uint64(r.Sequence)),
			Current: transitionToDTO(r.Current), Reserved: transitionToDTO(r.Reserved),
		})
		if err != nil {
			return nil, err
		}
		dto.Instances[string(id)] = raw
	}
	for id, f := range ns.failed {
		dto.Instances[string(id)] = append(json.RawMessage(nil), f.raw...)
	}
	return json.Marshal(dto)
}

// decodeNamespace decodes the namespace value. It never returns nil and never
// reinterprets unsupported or malformed bytes as missing or empty.
func decodeNamespace(raw json.RawMessage) *namespace {
	failed := func(status ControlLoadDisposition, detail string) *namespace {
		return &namespace{status: status, detail: detail, raw: append(json.RawMessage(nil), raw...)}
	}
	fields, err := strictObject(raw)
	if err != nil {
		return failed(ControlLoadCorrupt, "namespace is not a closed JSON object")
	}
	rawVersion, ok := fields["schema_version"]
	if !ok {
		return failed(ControlLoadCorrupt, "schema_version missing")
	}
	version, err := decodeJSONSmallInt(rawVersion)
	if err != nil {
		return failed(ControlLoadCorrupt, "schema_version is not an integer")
	}
	if version != controlSchemaVersion {
		return failed(ControlLoadUnsupported, fmt.Sprintf("unsupported schema_version %d", version))
	}
	for key := range fields {
		switch key {
		case "schema_version", "issued_revision_mark", "catalog_revision", "instances", "recovery":
		default:
			return failed(ControlLoadCorrupt, fmt.Sprintf("unknown member %q", key))
		}
	}
	ns := newLoadedNamespace()
	markText, err := decodeJSONString(fields["issued_revision_mark"])
	if err != nil {
		return failed(ControlLoadCorrupt, "issued_revision_mark missing or not a string")
	}
	mark, ok := parseCounter(markText, true)
	if !ok {
		return failed(ControlLoadCorrupt, "issued_revision_mark is not a canonical revision")
	}
	ns.mark = DesiredRevision(mark)
	catalogText, err := decodeJSONString(fields["catalog_revision"])
	if err != nil {
		return failed(ControlLoadCorrupt, "catalog_revision missing or not a string")
	}
	catalog, ok := parseCounter(catalogText, false)
	if !ok {
		return failed(ControlLoadCorrupt, "catalog_revision is not a canonical nonzero revision")
	}
	ns.catalog = CatalogRevision(catalog)
	rawInstances, ok := fields["instances"]
	if !ok {
		return failed(ControlLoadCorrupt, "instances missing")
	}
	instances, err := strictObject(rawInstances)
	if err != nil {
		return failed(ControlLoadCorrupt, "instances is not a closed JSON object")
	}
	for id, rawRecord := range instances {
		record, reason := decodeRecord(InstanceID(id), rawRecord, ns.mark)
		if reason != "" {
			ns.failed[InstanceID(id)] = failedRecord{raw: append(json.RawMessage(nil), rawRecord...), reason: reason}
			continue
		}
		ns.records[InstanceID(id)] = record
	}
	if rawRecovery, present := fields["recovery"]; present {
		recovery, err := decodeRecovery(rawRecovery)
		if err != nil {
			return failed(ControlLoadCorrupt, err.Error())
		}
		if recovery.MaxDesiredRevision > ns.mark {
			return failed(ControlLoadCorrupt, "recovery records a desired revision above the issued-revision mark")
		}
		ns.recovery = recovery
	}
	return ns
}

func decodeRecord(id InstanceID, raw json.RawMessage, mark DesiredRevision) (nsRecord, string) {
	if !validInstanceID(id) {
		return nsRecord{}, "empty instance id"
	}
	fields, err := strictObject(raw)
	if err != nil {
		return nsRecord{}, "record is not a closed JSON object"
	}
	if !exactKeys(fields, "desired_state", "desired_revision", "lifecycle_sequence", "current", "reserved") {
		return nsRecord{}, "record members are not the closed set"
	}
	var r nsRecord
	state, err := decodeJSONString(fields["desired_state"])
	if err != nil || (state != string(DesiredRunning) && state != string(DesiredStopped)) {
		return nsRecord{}, "desired_state invalid"
	}
	r.Desired = DesiredState(state)
	text, err := decodeJSONString(fields["desired_revision"])
	if err != nil {
		return nsRecord{}, "desired_revision invalid"
	}
	revision, ok := parseCounter(text, false)
	if !ok || DesiredRevision(revision) > mark {
		return nsRecord{}, "desired_revision invalid or above the issued-revision mark"
	}
	r.Revision = DesiredRevision(revision)
	text, err = decodeJSONString(fields["lifecycle_sequence"])
	if err != nil {
		return nsRecord{}, "lifecycle_sequence invalid"
	}
	sequence, ok := parseCounter(text, true)
	if !ok {
		return nsRecord{}, "lifecycle_sequence invalid"
	}
	r.Sequence = LifecycleSequence(sequence)
	if r.Current, err = decodeTransition(fields["current"]); err != nil {
		return nsRecord{}, "current: " + err.Error()
	}
	if r.Reserved, err = decodeTransition(fields["reserved"]); err != nil {
		return nsRecord{}, "reserved: " + err.Error()
	}
	if reason := validateRecordSlots(r); reason != "" {
		return nsRecord{}, reason
	}
	return r, ""
}

func validateRecordSlots(r nsRecord) string {
	if r.Reserved != nil && r.Current == nil {
		return "reserved slot without a current slot"
	}
	for _, t := range []*Transition{r.Current, r.Reserved} {
		if t == nil {
			continue
		}
		if t.DesiredRevision > r.Revision {
			return "transition revision above the desired revision"
		}
		if t.Sequence > r.Sequence {
			return "transition sequence above the lifecycle sequence"
		}
	}
	if r.Current != nil && r.Reserved != nil {
		if r.Current.Sequence == r.Reserved.Sequence {
			return "duplicate transition sequence"
		}
		// The reserved slot is the newer transition; startup reconciles current
		// before reserved, so a reversed layout would replay out of order.
		if r.Current.Sequence > r.Reserved.Sequence {
			return "current transition sequence does not precede the reserved sequence"
		}
	}
	return ""
}

func validateTransition(t *Transition) error {
	switch t.Kind {
	case KindStart, KindStop, KindRestart, KindCancel:
	default:
		return errors.New("kind invalid")
	}
	switch t.Phase {
	case PhasePending, PhaseActive, PhaseCleanup:
	default:
		return errors.New("phase invalid")
	}
	if t.DesiredRevision == 0 || t.Sequence == 0 {
		return errors.New("revision and sequence must be nonzero")
	}
	if t.Epoch != "" && (len(t.Epoch) > maxControlEpochBytes || !controlEpochPattern.MatchString(string(t.Epoch))) {
		return errors.New("epoch invalid")
	}
	if t.AttemptsUsed < 0 || t.AttemptsUsed > DefaultMaxAttempts {
		return errors.New("attempts_used out of bounds")
	}
	if !t.DeadlineUTC.IsZero() && t.DeadlineUTC.Location() != time.UTC {
		return errors.New("deadline must be UTC")
	}
	return nil
}

func decodeTransition(raw json.RawMessage) (*Transition, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	fields, err := strictObject(raw)
	if err != nil {
		return nil, errors.New("not a closed JSON object")
	}
	if !exactKeys(fields, "kind", "desired_revision", "sequence", "phase", "epoch", "generation",
		"native_close_pending", "semantic_cleanup_pending", "attempts_used", "deadline_utc") {
		return nil, errors.New("members are not the closed set")
	}
	var t Transition
	kind, err := decodeJSONString(fields["kind"])
	if err != nil {
		return nil, errors.New("kind invalid")
	}
	t.Kind = TransitionKind(kind)
	phase, err := decodeJSONString(fields["phase"])
	if err != nil {
		return nil, errors.New("phase invalid")
	}
	t.Phase = TransitionPhase(phase)
	for _, field := range []struct {
		name      string
		allowZero bool
		set       func(uint64)
	}{
		{"desired_revision", false, func(v uint64) { t.DesiredRevision = DesiredRevision(v) }},
		{"sequence", false, func(v uint64) { t.Sequence = LifecycleSequence(v) }},
		{"generation", true, func(v uint64) { t.Generation = DriverGeneration(v) }},
	} {
		text, err := decodeJSONString(fields[field.name])
		if err != nil {
			return nil, fmt.Errorf("%s invalid", field.name)
		}
		v, ok := parseCounter(text, field.allowZero)
		if !ok {
			return nil, fmt.Errorf("%s invalid", field.name)
		}
		field.set(v)
	}
	epoch, err := decodeJSONString(fields["epoch"])
	if err != nil {
		return nil, errors.New("epoch invalid")
	}
	t.Epoch = SourceEpoch(epoch)
	if t.NativeClosePending, err = decodeJSONBool(fields["native_close_pending"]); err != nil {
		return nil, errors.New("native_close_pending invalid")
	}
	if t.SemanticCleanupPending, err = decodeJSONBool(fields["semantic_cleanup_pending"]); err != nil {
		return nil, errors.New("semantic_cleanup_pending invalid")
	}
	if t.AttemptsUsed, err = decodeJSONSmallInt(fields["attempts_used"]); err != nil {
		return nil, errors.New("attempts_used invalid")
	}
	deadline, err := decodeJSONString(fields["deadline_utc"])
	if err != nil {
		return nil, errors.New("deadline_utc invalid")
	}
	if deadline != "" {
		parsed, err := time.Parse(time.RFC3339Nano, deadline)
		if err != nil || !strings.HasSuffix(deadline, "Z") {
			return nil, errors.New("deadline_utc must be RFC 3339 UTC")
		}
		t.DeadlineUTC = parsed.UTC()
	}
	if err := validateTransition(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

func decodeRecovery(raw json.RawMessage) (*RecoveryMaterial, error) {
	fields, err := strictObject(raw)
	if err != nil || !exactKeys(fields, "format", "instance_ids", "max_desired_revision", "payload_sha256", "payload") {
		return nil, errors.New("recovery envelope is not the closed object")
	}
	format, err := decodeJSONString(fields["format"])
	if err != nil || format != controlRecoveryFormat {
		return nil, errors.New("recovery format unsupported")
	}
	var ids []string
	if !bytes.HasPrefix(bytes.TrimSpace(fields["instance_ids"]), []byte("[")) || json.Unmarshal(fields["instance_ids"], &ids) != nil {
		return nil, errors.New("recovery instance_ids invalid")
	}
	maxText, err := decodeJSONString(fields["max_desired_revision"])
	if err != nil {
		return nil, errors.New("recovery max_desired_revision invalid")
	}
	maxRev, ok := parseCounter(maxText, true)
	if !ok {
		return nil, errors.New("recovery max_desired_revision invalid")
	}
	digest, err := decodeJSONString(fields["payload_sha256"])
	if err != nil || !controlSHA256Pattern.MatchString(digest) {
		return nil, errors.New("recovery payload_sha256 invalid")
	}
	encoded, err := decodeJSONString(fields["payload"])
	if err != nil {
		return nil, errors.New("recovery payload invalid")
	}
	payload, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return nil, errors.New("recovery payload is not canonical base64")
	}
	sum := sha256.Sum256(payload)
	if hex.EncodeToString(sum[:]) != digest {
		return nil, errors.New("recovery payload digest mismatch")
	}
	rec := &RecoveryMaterial{MaxDesiredRevision: DesiredRevision(maxRev), Payload: payload}
	for _, id := range ids {
		rec.DriverIDs = append(rec.DriverIDs, InstanceID(id))
	}
	if err := validateRecoveryIDs(rec.DriverIDs); err != nil {
		return nil, err
	}
	return rec, nil
}

func validateRecoveryIDs(ids []InstanceID) error {
	for i, id := range ids {
		if !validInstanceID(id) {
			return errors.New("recovery instance_ids contains an empty id")
		}
		if i > 0 && ids[i-1] >= id {
			return errors.New("recovery instance_ids must be bytewise sorted and unique")
		}
	}
	return nil
}

// strictObject decodes one JSON object, rejecting duplicate members and
// trailing values, and returns each member's raw value.
func strictObject(raw []byte) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	out := map[string]json.RawMessage{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, errors.New("member name is not a string")
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("duplicate member %q", key)
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		out[key] = value
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return nil, errors.New("unterminated object")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing data")
	}
	return out, nil
}

func exactKeys(fields map[string]json.RawMessage, keys ...string) bool {
	if len(fields) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := fields[key]; !ok {
			return false
		}
	}
	return true
}

func decodeJSONString(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '"' {
		return "", errors.New("not a JSON string")
	}
	var s string
	if err := json.Unmarshal(trimmed, &s); err != nil {
		return "", err
	}
	return s, nil
}

func decodeJSONBool(raw json.RawMessage) (bool, error) {
	switch string(bytes.TrimSpace(raw)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, errors.New("not a JSON boolean")
}

func decodeJSONSmallInt(raw json.RawMessage) (int, error) {
	text := string(bytes.TrimSpace(raw))
	if len(text) == 0 || len(text) > 9 {
		return 0, errors.New("not a small JSON integer")
	}
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return 0, errors.New("not a small JSON integer")
		}
	}
	if len(text) > 1 && text[0] == '0' {
		return 0, errors.New("not a canonical JSON integer")
	}
	return strconv.Atoi(text)
}

func addCounter(v, n uint64) (uint64, bool) {
	if v > math.MaxUint64-n {
		return 0, false
	}
	return v + n, true
}

// --- persistence seam ---

// persister is the durable medium of one control store.
type persister interface {
	// read returns the committed bytes; (nil, nil) when none were ever written.
	read() ([]byte, error)
	// commit durably replaces the committed bytes. renamed reports that the
	// commit point passed; an error with renamed set is ambiguous persistence.
	commit(data []byte) (renamed bool, err error)
	// syncDir establishes directory durability after a commit.
	syncDir() error
}

// controlStore is the control store behind both implementations: it holds the
// committed namespace, decides requests under one serialization point, and
// commits each transaction through its persister.
type controlStore struct {
	p persister

	// writeSem is the sole-writer lock; it is also the namespace-wide control
	// serialization point.
	writeSem chan struct{}

	mu        sync.Mutex
	status    ControlLoadDisposition
	detail    string
	committed *namespace
	readOnly  *namespace
	ambiguous *namespace
	blocked   error
	closed    bool
	configs   map[InstanceID]ControlInstance
}

func newControlStore(p persister) *controlStore {
	return &controlStore{p: p, writeSem: make(chan struct{}, 1), configs: map[InstanceID]ControlInstance{}}
}

func (c *controlStore) acquireWrite(ctx context.Context) error {
	select {
	case c.writeSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *controlStore) releaseWrite() { <-c.writeSem }

// load reads the committed bytes and sets the load disposition.
func (c *controlStore) load() {
	data, err := c.p.read()
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case err != nil:
		c.status, c.detail = ControlLoadUnreadable, "state file unreadable"
		c.blocked = fmt.Errorf("%w: state file unreadable: %v", ErrStatePersistBlocked, err)
	case data == nil:
		c.status = ControlLoadMissing
	default:
		ns := decodeNamespace(data)
		c.status, c.detail, c.committed = ns.status, ns.detail, ns
	}
}

// loadReadOnly decodes the readable bytes without ever writing: the control
// state is LOCKED, no decision, work, catalog or recovery write is admitted and
// the last-known-good view stays readable.
func (c *controlStore) loadReadOnly(lockErr error) {
	data, err := c.p.read()
	c.mu.Lock()
	defer c.mu.Unlock()
	detail := "store lock unavailable: " + lockErr.Error()
	switch {
	case err == nil && data != nil:
		ns := decodeNamespace(data)
		if ns.status == ControlLoadLoaded {
			c.readOnly = ns
		} else {
			detail += "; control " + string(ns.status) + ": " + ns.detail
		}
	case err == nil:
		detail += "; state file absent"
	default:
		detail += "; state file unreadable"
	}
	c.status, c.detail = ControlLoadLocked, detail
	c.blocked = fmt.Errorf("%w: store lock unavailable: %v", ErrStatePersistBlocked, lockErr)
}

func (c *controlStore) loadedLocked() *namespace {
	if c.status != ControlLoadLoaded || c.committed == nil || c.committed.status != ControlLoadLoaded {
		return nil
	}
	return c.committed
}

// readableLocked returns the committed namespace for reads. While control is
// LOCKED it returns the last-known-good namespace with readOnly set.
func (c *controlStore) readableLocked() (ns *namespace, readOnly bool) {
	if ns := c.loadedLocked(); ns != nil {
		return ns, false
	}
	if c.status == ControlLoadLocked && c.readOnly != nil {
		return c.readOnly, true
	}
	return nil, false
}

func (c *controlStore) admissionClosedLocked() *Refusal {
	if c.closed {
		return refuse(RefusalUnavailable, 3, "admission closed: store closed")
	}
	if c.blocked != nil {
		return refuse(RefusalUnavailable, 3, "admission closed: persistence blocked: "+c.blocked.Error())
	}
	return nil
}

// ControlLoad returns the load disposition of the store.
func (c *controlStore) ControlLoad() (ControlLoadDisposition, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status, c.detail
}

// AdmissionOpen reports whether decisions can be admitted: control is loaded,
// no ambiguous persistence awaits readback, the store is open and persistence
// is not blocked.
func (c *controlStore) AdmissionOpen() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status == ControlLoadLoaded && c.ambiguous == nil && !c.closed && c.blocked == nil
}

func checkInstances(configs []ControlInstance) ([]ControlInstance, error) {
	sorted := append([]ControlInstance(nil), configs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].InstanceID < sorted[j].InstanceID })
	for i, cfg := range sorted {
		if !validInstanceID(cfg.InstanceID) {
			return nil, fmt.Errorf("%w: empty instance id", ErrControlInvalidConfig)
		}
		if i > 0 && sorted[i-1].InstanceID == cfg.InstanceID {
			return nil, fmt.Errorf("%w: duplicate instance id %s", ErrControlInvalidConfig, cfg.InstanceID)
		}
	}
	return sorted, nil
}

// Register registers the configured instances for this process and returns the
// startup plan derived from durable desired state. It opens nothing.
//
//   - MISSING: exactly one atomic seed. The mark starts at 0 and each configured
//     instance, in bytewise id order, is one accepted decision at mark + 1:
//     enabled RUNNING, disabled STOPPED.
//   - LOADED: a configured instance without a record becomes visible through a
//     mark-only write; a record whose instance is no longer configured and that
//     holds no in-flight work or recovery material is deleted with a mark-only
//     write. Both happen in one durable transaction, before any read.
//   - Any failed disposition: no write; every configured instance is FAILED.
//
// Disabled configuration always wins and never rewrites durable intent.
func (c *controlStore) Register(ctx context.Context, configs []ControlInstance) ([]ControlStartup, error) {
	sorted, err := checkInstances(configs)
	if err != nil {
		return nil, err
	}
	if err := c.acquireWrite(ctx); err != nil {
		return nil, err
	}
	defer c.releaseWrite()

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, refuse(RefusalUnavailable, 3, "admission closed: store closed")
	}
	configured := make(map[InstanceID]ControlInstance, len(sorted))
	for _, cfg := range sorted {
		configured[cfg.InstanceID] = cfg
	}
	status := c.status
	ambiguous := c.ambiguous != nil
	var committed *namespace
	if c.committed != nil {
		committed = c.committed.clone()
	}
	priorRegistered := make([]InstanceID, 0, len(c.configs))
	for id := range c.configs {
		priorRegistered = append(priorRegistered, id)
	}
	c.mu.Unlock()

	var commitErr error
	switch {
	case ambiguous:
		commitErr = ErrControlReadbackPending
	case status == ControlLoadMissing:
		stage := newLoadedNamespace()
		for _, cfg := range sorted {
			stage.mark++
			stage.catalog++
			desired := DesiredRunning
			if !cfg.Enabled {
				desired = DesiredStopped
			}
			stage.records[cfg.InstanceID] = nsRecord{Desired: desired, Revision: stage.mark}
		}
		commitErr = c.commit(stage, true)
	case status == ControlLoadLoaded && committed != nil:
		stage := committed.clone()
		recoveryIDs := map[InstanceID]bool{}
		if stage.recovery != nil {
			for _, id := range stage.recovery.DriverIDs {
				recoveryIDs[id] = true
			}
		}
		var changes uint64
		removed := map[InstanceID]bool{}
		ids := make([]InstanceID, 0, len(stage.records))
		for id := range stage.records {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			r := stage.records[id]
			if _, ok := configured[id]; ok || r.Current != nil || r.Reserved != nil || recoveryIDs[id] {
				continue
			}
			delete(stage.records, id)
			removed[id] = true
			changes++
		}
		priorSet := make(map[InstanceID]bool, len(priorRegistered))
		for _, id := range priorRegistered {
			priorSet[id] = true
			if _, ok := configured[id]; !ok && !removed[id] {
				changes++
			}
		}
		for _, cfg := range sorted {
			_, hasRecord := stage.records[cfg.InstanceID]
			_, hasFailed := stage.failed[cfg.InstanceID]
			if !hasRecord && !hasFailed && !priorSet[cfg.InstanceID] {
				changes++
			}
		}
		if changes > 0 {
			mark, ok1 := addCounter(uint64(stage.mark), changes)
			catalog, ok2 := addCounter(uint64(stage.catalog), changes)
			if !ok1 || !ok2 {
				commitErr = refuse(RefusalCounterExhausted, 10, "registration mark or catalog increment would overflow")
				break
			}
			stage.mark, stage.catalog = DesiredRevision(mark), CatalogRevision(catalog)
			commitErr = c.commit(stage, false)
		}
	}
	// Publish registration only after its durable fence. Failed load
	// dispositions still expose configured instances as FAILED startup entries.
	if commitErr == nil || (status != ControlLoadLoaded && !ambiguous) {
		c.mu.Lock()
		c.configs = configured
		c.mu.Unlock()
	}
	return c.startupPlan(), commitErr
}

func (c *controlStore) startupPlan() []ControlStartup {
	c.mu.Lock()
	defer c.mu.Unlock()
	ids := make([]InstanceID, 0, len(c.configs))
	for id := range c.configs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	ns := c.committed
	loaded := c.status == ControlLoadLoaded && ns != nil && ns.status == ControlLoadLoaded
	plan := make([]ControlStartup, 0, len(ids))
	for _, id := range ids {
		cfg := c.configs[id]
		entry := ControlStartup{InstanceID: id, DesiredState: DesiredStopped}
		if !loaded {
			entry.Action = StartupFailed
			plan = append(plan, entry)
			continue
		}
		entry.DesiredRevision = ns.mark
		if _, bad := ns.failed[id]; bad {
			entry.Action = StartupFailed
			entry.DesiredRevision = 0
			plan = append(plan, entry)
			continue
		}
		if r, ok := ns.records[id]; ok {
			entry.DesiredState, entry.DesiredRevision = r.Desired, r.Revision
			entry.Current, entry.Reserved = cloneTransition(r.Current), cloneTransition(r.Reserved)
		}
		switch {
		case !cfg.Enabled:
			entry.Action = StartupDisabled
		case entry.DesiredState == DesiredRunning:
			entry.Action = StartupStarting
		default:
			entry.Action = StartupStopped
		}
		plan = append(plan, entry)
	}
	return plan
}

// Catalog returns one coherent read of committed control state for every
// registered instance, sorted bytewise by id.
func (c *controlStore) Catalog() (ControlCatalog, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ns, readOnly := c.readableLocked()
	if ns == nil {
		return ControlCatalog{}, refuse(RefusalFailedPrecondition, 3, "control state is not loaded: "+string(c.status))
	}
	ids := make([]InstanceID, 0, len(c.configs))
	for id := range c.configs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	view := ControlCatalog{ReadOnly: readOnly, IssuedMark: ns.mark, CatalogRevision: ns.catalog}
	for _, id := range ids {
		d := recordViewLocked(ns, id, c.configs[id])
		d.ReadOnly = readOnly
		view.Drivers = append(view.Drivers, d)
	}
	return view, nil
}

// Driver returns a coherent read of one registered instance.
func (c *controlStore) Driver(id InstanceID) (ControlRecord, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cfg, ok := c.configs[id]
	if !validInstanceID(id) || !ok {
		return ControlRecord{}, refuse(RefusalNotFound, 2, "instance not registered")
	}
	ns, readOnly := c.readableLocked()
	if ns == nil {
		return ControlRecord{InstanceID: id, Enabled: cfg.Enabled, Failed: true}, nil
	}
	v := recordViewLocked(ns, id, cfg)
	v.ReadOnly = readOnly
	return v, nil
}

func recordViewLocked(ns *namespace, id InstanceID, cfg ControlInstance) ControlRecord {
	v := ControlRecord{InstanceID: id, Enabled: cfg.Enabled, DesiredState: DesiredStopped, DesiredRevision: ns.mark}
	if _, bad := ns.failed[id]; bad {
		v.Failed = true
		v.DesiredRevision = 0
		return v
	}
	if r, ok := ns.records[id]; ok {
		v.HasRecord = true
		v.DesiredState, v.DesiredRevision = r.Desired, r.Revision
		v.Current, v.Reserved = cloneTransition(r.Current), cloneTransition(r.Reserved)
	}
	return v
}

func currentViewLocked(ns *namespace, id InstanceID) (DesiredView, bool) {
	if r, ok := ns.records[id]; ok {
		return DesiredView{DesiredState: r.Desired, DesiredRevision: r.Revision}, true
	}
	return DesiredView{DesiredState: DesiredStopped, DesiredRevision: ns.mark}, false
}

// precheck applies rows 2-4: registration, control state and configured
// eligibility.
func (c *controlStore) precheck(id InstanceID) *Refusal {
	c.mu.Lock()
	defer c.mu.Unlock()
	cfg, registered := c.configs[id]
	if !validInstanceID(id) || !registered {
		return refuse(RefusalNotFound, 2, "instance not registered")
	}
	ns := c.loadedLocked()
	if ns == nil {
		return refuse(RefusalFailedPrecondition, 3, "control state failed to load: "+string(c.status))
	}
	if refusal := c.admissionClosedLocked(); refusal != nil {
		return refusal
	}
	if _, bad := ns.failed[id]; bad {
		return refuse(RefusalFailedPrecondition, 3, "instance record malformed")
	}
	if c.ambiguous != nil {
		return refuse(RefusalUnavailable, 3, "readback after ambiguous persistence still pending")
	}
	if !cfg.Enabled {
		return refuse(RefusalFailedPrecondition, 4, "instance disabled by configuration")
	}
	return nil
}

// Decide validates, decides and durably commits one control request following
// the refusal table. The comparison, mark read, allocation and durable write
// happen under the namespace-wide serialization point.
func (c *controlStore) Decide(ctx context.Context, method Method, req Request, planner Planner) (DesiredView, error) {
	if planner == nil {
		return DesiredView{}, refuse(RefusalInvalidArgument, 1, "a planner is required")
	}
	if !validMethod(method) {
		return DesiredView{}, refuse(RefusalInvalidArgument, 1, "unknown method")
	}
	if req.ExpectedRevision == 0 {
		return DesiredView{}, refuse(RefusalInvalidArgument, 1, "ExpectedRevision must be nonzero")
	}
	if refusal := c.precheck(req.InstanceID); refusal != nil {
		return DesiredView{}, refusal
	}
	if err := c.acquireWrite(ctx); err != nil {
		return DesiredView{}, refuse(RefusalDeadlineExceeded, 5, "context ended during the serialization wait")
	}
	defer c.releaseWrite()
	if refusal := c.precheck(req.InstanceID); refusal != nil {
		return DesiredView{}, refusal
	}
	if ctx.Err() != nil {
		return DesiredView{}, refuse(RefusalDeadlineExceeded, 5, "context ended after acquiring the serialization point")
	}

	c.mu.Lock()
	ns := c.loadedLocked().clone()
	c.mu.Unlock()
	if ns == nil {
		return DesiredView{}, refuse(RefusalFailedPrecondition, 3, "control state is not loaded")
	}
	current, hasRecord := currentViewLocked(ns, req.InstanceID)
	// Row 6: optimistic comparison precedes the same-desired no-op test.
	if req.ExpectedRevision != current.DesiredRevision {
		r := refuse(RefusalRevisionConflict, 6, "expected revision differs from the current desired revision")
		view := current
		r.View = &view
		return DesiredView{}, r
	}
	// Same-desired no-op writes nothing and allocates nothing.
	if (method == MethodStart && current.DesiredState == DesiredRunning) || (method == MethodStop && current.DesiredState == DesiredStopped) {
		return current, nil
	}
	if method == MethodRestart && current.DesiredState != DesiredRunning {
		return DesiredView{}, refuse(RefusalFailedPrecondition, 6, "RESTART requires desired RUNNING")
	}
	record := nsRecord{}
	if hasRecord {
		record = cloneRecord(ns.records[req.InstanceID])
	}
	// Rows 7-8: planner (quarantine or unresolved recovery, transition slots).
	plan, refusal := planner(PlanInput{Method: method, InstanceID: req.InstanceID, HasRecord: hasRecord,
		Current: cloneTransition(record.Current), Reserved: cloneTransition(record.Reserved)})
	if refusal != nil {
		if refusal.Code != RefusalFailedPrecondition && refusal.Code != RefusalTransitionBusy {
			return DesiredView{}, refuse(RefusalTransitionBusy, 8, "planner returned an out-of-table refusal")
		}
		return DesiredView{}, refusal
	}
	// Row 10: counters.
	newRevision, okMark := addCounter(uint64(ns.mark), 1)
	newCatalog, okCatalog := addCounter(uint64(ns.catalog), 1)
	if !okMark || !okCatalog {
		return DesiredView{}, refuse(RefusalCounterExhausted, 10, "mark or catalog increment would overflow")
	}
	updated, err := applyPlan(record, plan, DesiredRevision(newRevision), TransitionKind(method))
	if err != nil {
		var r *Refusal
		if errors.As(err, &r) {
			return DesiredView{}, r
		}
		return DesiredView{}, refuse(RefusalTransitionBusy, 8, err.Error())
	}
	desired := DesiredRunning
	if method == MethodStop {
		desired = DesiredStopped
	}
	updated.Desired, updated.Revision = desired, DesiredRevision(newRevision)
	ns.records[req.InstanceID] = updated
	ns.mark, ns.catalog = DesiredRevision(newRevision), CatalogRevision(newCatalog)
	if ctx.Err() != nil {
		return DesiredView{}, refuse(RefusalDeadlineExceeded, 5, "context ended at the pre-commit recheck")
	}
	if err := c.commit(ns, false); err != nil {
		return DesiredView{}, err
	}
	return DesiredView{DesiredState: desired, DesiredRevision: DesiredRevision(newRevision)}, nil
}

// applyPlan validates a plan and assigns revision and sequences to its new
// transitions.
func applyPlan(record nsRecord, plan SlotPlan, newRevision DesiredRevision, requested TransitionKind) (nsRecord, error) {
	if plan.Reserved != nil && plan.Current == nil {
		return record, fmt.Errorf("%w: reserved slot without current slot", ErrControlInvalidWork)
	}
	existing := map[LifecycleSequence]*Transition{}
	for _, t := range []*Transition{record.Current, record.Reserved} {
		if t != nil {
			existing[t.Sequence] = t
		}
	}
	out := cloneRecord(record)
	slots := []*Transition{cloneTransition(plan.Current), cloneTransition(plan.Reserved)}
	requestedAllocated := false
	for _, t := range slots {
		if t == nil {
			continue
		}
		if t.Sequence == 0 {
			if t.Kind == requested {
				requestedAllocated = true
			}
			next, ok := addCounter(uint64(out.Sequence), 1)
			if !ok {
				return record, refuse(RefusalCounterExhausted, 10, "lifecycle sequence increment would overflow")
			}
			out.Sequence = LifecycleSequence(next)
			t.Sequence = LifecycleSequence(next)
			t.DesiredRevision = newRevision
		} else if prior, ok := existing[t.Sequence]; !ok || *prior != *t {
			return record, fmt.Errorf("%w: retained transition does not match the stored slot", ErrControlInvalidWork)
		}
		if err := validateTransition(t); err != nil {
			return record, fmt.Errorf("%w: %v", ErrControlInvalidWork, err)
		}
	}
	if !requestedAllocated {
		return record, fmt.Errorf("%w: plan must allocate the requested transition kind", ErrControlInvalidWork)
	}
	out.Current, out.Reserved = slots[0], slots[1]
	if out.Current != nil && out.Reserved != nil && out.Current.Sequence >= out.Reserved.Sequence {
		return record, fmt.Errorf("%w: current transition must precede the reserved transition", ErrControlInvalidWork)
	}
	return out, nil
}

// admitted returns the committed namespace for a write, or the refusal that
// closes it.
func (c *controlStore) admitted() (*namespace, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ns := c.loadedControlClone()
	if ns == nil {
		return nil, refuse(RefusalFailedPrecondition, 3, "control state is not loaded")
	}
	if closed := c.admissionClosedLocked(); closed != nil {
		return nil, closed
	}
	if c.ambiguous != nil {
		return nil, refuse(RefusalUnavailable, 3, "readback after ambiguous persistence still pending")
	}
	return ns, nil
}

func (c *controlStore) loadedControlClone() *namespace {
	if ns := c.loadedLocked(); ns != nil {
		return ns.clone()
	}
	return nil
}

// CommitWork durably replaces an instance's typed slots without changing
// desired state, its revision or the mark (phase updates, promotion of the
// reserved slot, removal after terminal obligations). Transitions must keep the
// kind, sequence and revision of a currently stored slot. A reserved slot may
// be retained or promoted, and must be promoted before clearing both slots.
// Updates must preserve correlation, cleanup and budget progression.
func (c *controlStore) CommitWork(ctx context.Context, id InstanceID, current, reserved *Transition) error {
	if err := c.acquireWrite(ctx); err != nil {
		return err
	}
	defer c.releaseWrite()
	ns, err := c.admitted()
	if err != nil {
		return err
	}
	r, ok := ns.records[id]
	if !ok {
		return refuse(RefusalNotFound, 2, "instance has no record")
	}
	same := func(a, b *Transition) bool {
		return a != nil && b != nil && a.Sequence == b.Sequence && a.DesiredRevision == b.DesiredRevision && a.Kind == b.Kind
	}
	// A worker may update only an identity that still occupies a stored slot.
	for _, t := range []*Transition{current, reserved} {
		if t == nil {
			continue
		}
		if !same(t, r.Current) && !same(t, r.Reserved) {
			return fmt.Errorf("%w: transition does not match a stored slot", ErrControlInvalidWork)
		}
	}
	if reserved != nil && !same(reserved, r.Reserved) {
		return fmt.Errorf("%w: current transition cannot be demoted to reserved", ErrControlInvalidWork)
	}
	if r.Reserved != nil && !same(current, r.Reserved) && !same(reserved, r.Reserved) {
		return fmt.Errorf("%w: work update would discard the reserved transition", ErrControlInvalidWork)
	}
	// Durable progress is monotonic.
	for _, t := range []*Transition{current, reserved} {
		if t == nil {
			continue
		}
		stored := r.Current
		if !same(t, stored) {
			stored = r.Reserved
		}
		if err := checkWorkUpdate(stored, t); err != nil {
			return err
		}
	}
	r.Current, r.Reserved = cloneTransition(current), cloneTransition(reserved)
	for _, t := range []*Transition{r.Current, r.Reserved} {
		if t == nil {
			continue
		}
		if err := validateTransition(t); err != nil {
			return fmt.Errorf("%w: %v", ErrControlInvalidWork, err)
		}
	}
	if reason := validateRecordSlots(r); reason != "" {
		return fmt.Errorf("%w: %s", ErrControlInvalidWork, reason)
	}
	ns.records[id] = r
	return c.commit(ns, false)
}

// AdvanceCatalog durably advances the catalog view revision once for an
// externally visible observed-view transaction and returns the new value.
func (c *controlStore) AdvanceCatalog(ctx context.Context) (CatalogRevision, error) {
	if err := c.acquireWrite(ctx); err != nil {
		return 0, err
	}
	defer c.releaseWrite()
	ns, err := c.admitted()
	if err != nil {
		return 0, err
	}
	next, ok := addCounter(uint64(ns.catalog), 1)
	if !ok {
		return 0, refuse(RefusalCounterExhausted, 10, "catalog revision increment would overflow")
	}
	ns.catalog = CatalogRevision(next)
	if err := c.commit(ns, false); err != nil {
		return 0, err
	}
	return ns.catalog, nil
}

// CommitRecovery durably stores the typed recovery envelope. The payload is
// never interpreted. A nil envelope removes it.
func (c *controlStore) CommitRecovery(ctx context.Context, material *RecoveryMaterial) error {
	if material != nil {
		if err := validateRecoveryIDs(material.DriverIDs); err != nil {
			return fmt.Errorf("%w: %v", ErrControlInvalidRecovery, err)
		}
	}
	if err := c.acquireWrite(ctx); err != nil {
		return err
	}
	defer c.releaseWrite()
	ns, err := c.admitted()
	if err != nil {
		return err
	}
	if material != nil && material.MaxDesiredRevision > ns.mark {
		return fmt.Errorf("%w: recovery revision above the issued-revision mark", ErrControlInvalidRecovery)
	}
	ns.recovery = cloneMaterial(material)
	return c.commit(ns, false)
}

// Recovery returns a detached copy of the committed recovery envelope.
func (c *controlStore) Recovery() *RecoveryMaterial {
	c.mu.Lock()
	defer c.mu.Unlock()
	ns, _ := c.readableLocked()
	if ns == nil {
		return nil
	}
	return cloneMaterial(ns.recovery)
}

// ResolveReadback is the control service's readback after ambiguous
// persistence. Admission reopens only when the stored namespace agrees with
// either the staged or the previously committed namespace and directory
// durability is established. Agreement alone never reopens admission.
// Disagreement or an unreadable or corrupt readback keeps control closed.
func (c *controlStore) ResolveReadback(ctx context.Context) error {
	if err := c.acquireWrite(ctx); err != nil {
		return err
	}
	defer c.releaseWrite()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return refuse(RefusalUnavailable, 3, "admission closed: store closed")
	}
	staged := c.ambiguous
	committed := c.committed
	c.mu.Unlock()
	if staged == nil {
		return nil
	}
	data, err := c.p.read()
	if err != nil {
		c.failControl(ControlLoadUnreadable, "readback failed", nil, fmt.Errorf("%w: readback: %v", ErrStatePersistBlocked, err))
		return fmt.Errorf("life: readback: %w", err)
	}
	read := decodeNamespace(data)
	stagedRaw, err := staged.encode()
	if err != nil {
		return err
	}
	var committedRaw json.RawMessage
	if committed != nil {
		if committedRaw, err = committed.encode(); err != nil {
			return err
		}
	}
	readRaw, err := read.encode()
	if err != nil {
		return err
	}
	switch {
	case data != nil && bytes.Equal(readRaw, stagedRaw):
		if err := c.p.syncDir(); err != nil {
			return fmt.Errorf("%w: directory durability not established: %v", ErrControlReadbackPending, err)
		}
		c.mu.Lock()
		c.committed = staged
		c.status = ControlLoadLoaded
		c.ambiguous = nil
		c.mu.Unlock()
		return nil
	case bytes.Equal(readRaw, committedRaw):
		if err := c.p.syncDir(); err != nil {
			return fmt.Errorf("%w: directory durability not established: %v", ErrControlReadbackPending, err)
		}
		c.mu.Lock()
		c.ambiguous = nil
		c.mu.Unlock()
		return nil
	}
	c.failControl(ControlLoadCorrupt, "readback disagrees with the staged and committed namespaces", read, nil)
	return errors.New("life: readback disagrees with every known namespace")
}

// failControl closes control for the whole namespace. When preserved is set,
// its bytes are retained verbatim so later writes never overwrite them.
func (c *controlStore) failControl(status ControlLoadDisposition, detail string, preserved *namespace, block error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status, c.detail = status, detail
	c.ambiguous = nil
	if preserved == nil && block == nil {
		block = fmt.Errorf("%w: control readback has no namespace", ErrStatePersistBlocked)
	}
	if block != nil {
		c.blocked = block
	}
	if preserved != nil {
		if raw, err := preserved.encode(); err == nil {
			c.committed = &namespace{status: status, detail: detail, raw: raw}
		}
	}
}

// commit persists stage under the already-held serialization point and exposes
// it only after the commit is durable. A failure before the commit point
// discards the staging (UNAVAILABLE). A failure after the commit point is
// ambiguous persistence: nothing is exposed and admission stays closed until
// ResolveReadback establishes directory durability.
func (c *controlStore) commit(stage *namespace, seed bool) error {
	c.mu.Lock()
	if c.blocked != nil {
		err := c.blocked
		c.mu.Unlock()
		return &Refusal{Code: RefusalUnavailable, Row: 3, Reason: "persistence blocked: " + err.Error()}
	}
	c.mu.Unlock()
	raw, err := stage.encode()
	if err != nil {
		return refuse(RefusalUnavailable, 0, "namespace marshal failed")
	}
	renamed, err := c.p.commit(raw)
	if err != nil {
		if !renamed {
			return &Refusal{Code: RefusalUnavailable, Reason: "pre-commit write failure: " + err.Error()}
		}
		c.mu.Lock()
		c.ambiguous = stage
		c.mu.Unlock()
		return &Refusal{Code: RefusalUnavailable, Row: 3, Reason: "ambiguous persistence: " + err.Error()}
	}
	c.mu.Lock()
	c.committed = stage
	if seed || c.status == ControlLoadMissing {
		c.status = ControlLoadLoaded
	}
	c.mu.Unlock()
	return nil
}
