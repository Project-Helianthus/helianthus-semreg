package roweval

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// The three selector geometries are declared here, in the test files only.

type cmdSel struct {
	PB, SB, Op, Group, Instance byte
	Register                    uint16
	Direction                   string
}

type blkSel struct {
	Function byte
	Table    string
	Block    uint16
	Offset   uint16
	Count    uint16
}

type frmSel struct {
	Identifier uint32
	Format     string
	DLC        byte
	Mux        string
	Slice      string
}

// ---- independent decimal helpers (they share no code with the evaluator)

func dec(s string) v1.Decimal {
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	exp := 0
	if i := strings.Index(s, "."); i >= 0 {
		exp = -(len(s) - i - 1)
		s = s[:i] + s[i+1:]
	}
	s = strings.TrimLeft(s, "0")
	if s == "" {
		return v1.Decimal{Coefficient: "0"}
	}
	for strings.HasSuffix(s, "0") {
		s, exp = s[:len(s)-1], exp+1
	}
	if neg {
		s = "-" + s
	}
	return v1.Decimal{Coefficient: s, Exponent10: int32(exp)}
}

func ratOfDecimal(d v1.Decimal) *big.Rat {
	n, _ := new(big.Int).SetString(d.Coefficient, 10)
	r := new(big.Rat).SetInt(n)
	p := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(abs(int(d.Exponent10)))), nil)
	if d.Exponent10 >= 0 {
		return r.Mul(r, new(big.Rat).SetInt(p))
	}
	return r.Quo(r, new(big.Rat).SetInt(p))
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// sameNumber reports whether a Decimal equals the plain decimal text exactly.
func sameNumber(got v1.Decimal, want string) bool {
	w, ok := new(big.Rat).SetString(want)
	return ok && ratOfDecimal(got).Cmp(w) == 0
}

func ptr[T any](v T) *T { return &v }

// ---- descriptors a driver would supply

func swapBytes(raw uint64, width uint16) (uint64, error) {
	if width%8 != 0 {
		return 0, fmt.Errorf("width %d is not whole bytes", width)
	}
	var out uint64
	for i := uint16(0); i < width/8; i++ {
		out |= (raw >> (8 * i) & 0xff) << (width - 8 - 8*i)
	}
	return out, nil
}

func swapWords(raw uint64, width uint16) (uint64, error) {
	if width%32 != 0 {
		return 0, fmt.Errorf("width %d is not whole 16-bit word pairs", width)
	}
	var out uint64
	n := width / 16
	for i := uint16(0); i < n; i++ {
		out |= (raw >> (16 * i) & 0xffff) << (16 * (n - 1 - i))
	}
	return out, nil
}

func reverseBits(raw uint64, width uint16) (uint64, error) {
	var out uint64
	for i := uint16(0); i < width; i++ {
		out |= (raw >> i & 1) << (width - 1 - i)
	}
	return out, nil
}

func identity(raw uint64, _ uint16) (uint64, error) { return raw, nil }

func signed(raw uint64, width uint16) int64 {
	if width < 64 && raw>>(width-1)&1 == 1 {
		return int64(raw | ^uint64(0)<<width)
	}
	return int64(raw)
}

func fromInt(n int64) v1.Decimal { return dec(strconv.FormatInt(n, 10)) }

func intOf(d v1.Decimal) (*big.Int, error) {
	r := ratOfDecimal(d)
	if !r.IsInt() {
		return nil, fmt.Errorf("%s is not an integer", d.Coefficient)
	}
	return r.Num(), nil
}

var testCodecs = map[string]Codec{
	"u": {
		Decode: func(raw uint64, _ uint16) (v1.Decimal, error) { return dec(strconv.FormatUint(raw, 10)), nil },
		Encode: func(n v1.Decimal, w uint16) (uint64, error) {
			i, err := intOf(n)
			if err != nil {
				return 0, err
			}
			if i.Sign() < 0 || i.BitLen() > int(w) {
				return 0, fmt.Errorf("out of range")
			}
			return i.Uint64(), nil
		},
	},
	"s": {
		Decode: func(raw uint64, w uint16) (v1.Decimal, error) { return fromInt(signed(raw, w)), nil },
		Encode: func(n v1.Decimal, w uint16) (uint64, error) {
			i, err := intOf(n)
			if err != nil {
				return 0, err
			}
			lim := new(big.Int).Lsh(big.NewInt(1), uint(w-1))
			if i.Cmp(new(big.Int).Neg(lim)) < 0 || i.Cmp(lim) >= 0 {
				return 0, fmt.Errorf("out of range")
			}
			return uint64(i.Int64()) & (^uint64(0) >> (64 - w)), nil
		},
	},
	"f32": {
		Decode: func(raw uint64, w uint16) (v1.Decimal, error) {
			if w != 32 {
				return v1.Decimal{}, fmt.Errorf("width %d", w)
			}
			f := math.Float32frombits(uint32(raw))
			if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
				return v1.Decimal{}, fmt.Errorf("not a finite number")
			}
			return dec(strconv.FormatFloat(float64(f), 'f', -1, 32)), nil
		},
		Encode: func(n v1.Decimal, w uint16) (uint64, error) {
			f, _ := ratOfDecimal(n).Float32()
			return uint64(math.Float32bits(f)), nil
		},
	},
}

func testDescriptors[S any](extent func(S) (int, bool)) Descriptors[S] {
	return Descriptors[S]{
		Codecs:     testCodecs,
		ByteOrders: map[string]Order{"le": {Decode: identity, Encode: identity}, "be": {Decode: swapBytes, Encode: swapBytes}},
		WordOrders: map[string]Order{"none": {Decode: identity, Encode: identity}, "pairs": {Decode: swapWords, Encode: swapWords}},
		BitOrders:  map[string]Order{"lsb": {Decode: identity, Encode: identity}, "msb": {Decode: reverseBits, Encode: reverseBits}},
		Extent:     extent,
	}
}

func layout(offset, width uint16, codec, byteOrder string) Layout {
	return Layout{BitOffset: offset, Width: width, Codec: codec, ByteOrder: byteOrder, WordOrder: "none", BitOrder: "lsb"}
}

// ---- testdata

func readFile(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func readJSON(t testing.TB, name string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(readFile(t, name), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func at(m any, path ...string) any {
	for _, p := range path {
		mm, ok := m.(map[string]any)
		if !ok {
			return nil
		}
		m = mm[p]
	}
	return m
}

func asString(v any) string { s, _ := v.(string); return s }
func asInt(v any) int       { f, _ := v.(float64); return int(f) }

func fileSHA(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

func b64(t testing.TB, v any) []byte {
	t.Helper()
	s, ok := v.(string)
	if !ok {
		return nil
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func evidenceOf(t testing.TB, v any) v1.EvidenceRef {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("no evidence in %v", v)
	}
	return v1.EvidenceRef{
		Owner: v1.DefinitionID(asString(m["owner"])), Kind: v1.DefinitionID(asString(m["kind"])),
		Digest: v1.Digest(asString(m["digest"])), Contract: v1.ContractVersion(asString(m["contract"])),
		Access: v1.EvidenceAccess(asString(m["access"])), Redaction: v1.RedactionState(asString(m["redaction"])),
	}
}

// ---- catalog, roles and the caller's template

var (
	thermalPack = v1.PackRef{ID: "helianthus.pack.thermal", Version: "1.0.0"}
	pvPack      = v1.PackRef{ID: "helianthus.pack.pv", Version: "1.0.0"}
	storagePack = v1.PackRef{ID: "helianthus.pack.storage", Version: "1.0.0"}
)

func ref(p v1.PackRef, id string) v1.DefinitionRef {
	return v1.DefinitionRef{Pack: p, ID: v1.DefinitionID(id), Version: "1.0.0"}
}

func testCatalog() []v1.DefinitionIndex {
	return []v1.DefinitionIndex{
		{Pack: thermalPack,
			Fields:       []v1.DefinitionRef{ref(thermalPack, "thermal.setpoint.dhw_temperature_setpoint"), ref(thermalPack, "thermal.setpoint.zone_setpoint")},
			Services:     []v1.DefinitionRef{ref(thermalPack, "thermal.service.dhw"), ref(thermalPack, "thermal.service.zone")},
			Capabilities: []v1.DefinitionRef{ref(thermalPack, "thermal.capability.dhw_setpoint"), ref(thermalPack, "thermal.capability.zone_setpoint")},
			Operations:   []v1.DefinitionRef{ref(thermalPack, "thermal.operation.set_zone_setpoint")},
		},
		{Pack: pvPack, Fields: []v1.DefinitionRef{ref(pvPack, "pv.ac.aggregate_active_power")}, Services: []v1.DefinitionRef{ref(pvPack, "pv.service.inverter")}},
		{Pack: storagePack, Fields: []v1.DefinitionRef{ref(storagePack, "storage.pack.current")}, Services: []v1.DefinitionRef{ref(storagePack, "storage.service.pack")}},
	}
}

func testRoles(roles ...string) Roles {
	r := Roles{Subjects: map[string]RoleBinding{}, Catalog: testCatalog()}
	for _, name := range roles {
		r.Subjects[name] = RoleBinding{ServiceInstance: v1.ServiceInstanceID("service." + name + ".1")}
	}
	return r
}

func ev(t testing.TB, seed string) v1.EvidenceRef {
	return v1.EvidenceRef{Owner: "test.native", Kind: "test.fixture", Contract: "test.native/v1", Access: v1.EvidenceAccessPublic, Redaction: v1.RedactionNone,
		Digest: v1.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(seed))))}
}

// observedTemplate is a complete caller-owned observed template.
func observedTemplate(policy v1.PolicyID, evidence ...v1.EvidenceRef) v1.FactCandidate {
	source, epoch, binding := v1.SourceID("source.synthetic"), v1.SourceEpochID("epoch.synthetic"), v1.NativeBindingID("binding.synthetic")
	gen := v1.Uint64("2")
	return v1.FactCandidate{
		CandidateID: "candidate.synthetic.1",
		Key:         v1.FactKey{PackID: "template.key", PackVersion: "9.9.9", FactID: "template.fact", Dimensions: []v1.Dimension{}},
		Quality: v1.Quality{Assertion: v1.AssertionObserved, Qualification: v1.QualificationQualified, Promotion: v1.PromotionPromoted,
			Validity: v1.ValidityGood, Availability: v1.AvailabilityAvailable, Freshness: v1.FreshnessFresh, Reasons: []v1.DefinitionID{}},
		Times: v1.Times{
			ReceivedAt:        v1.TimePoint{UnixNanoseconds: "100", ClockID: "clock.synthetic", UncertaintyNS: "0"},
			ReceiptMonotonic:  v1.MonotonicPoint{ClockEpochID: "clock:synthetic", Nanoseconds: "100"},
			EvaluatedAt:       v1.TimePoint{UnixNanoseconds: "200", ClockID: "clock.synthetic", UncertaintyNS: "0"},
			EvaluateMonotonic: v1.MonotonicPoint{ClockEpochID: "clock:synthetic", Nanoseconds: "200"},
			SourceAt:          &v1.TimePoint{UnixNanoseconds: "90", ClockID: "clock.synthetic", UncertaintyNS: "5"},
		},
		FreshnessPolicy:  v1.FreshnessPolicy{PolicyID: policy, Version: "1.0.0", FreshForNS: "1000", RetainForNS: "2000", MaxWallUncertaintyNS: "0"},
		BindingID:        &binding,
		SourceEpochID:    &epoch,
		DriverGeneration: &gen,
		Origin:           v1.OriginRef{OriginID: "origin.synthetic", Kind: v1.OriginNativeObservation, SourceID: &source, SourceEpochID: &epoch, BindingID: &binding, Evidence: evidence},
		Evidence:         evidence,
		Revision:         "1",
	}
}

// inferredTemplate is a complete caller-owned inferred template: a derivation
// and no native path.
func inferredTemplate(policy v1.PolicyID, evidence ...v1.EvidenceRef) v1.FactCandidate {
	c := observedTemplate(policy, evidence...)
	c.Quality.Assertion = v1.AssertionInferred
	c.BindingID, c.SourceEpochID, c.DriverGeneration = nil, nil, nil
	c.Origin = v1.OriginRef{OriginID: "origin.synthetic", Kind: v1.OriginDerived, Evidence: evidence}
	c.Derivation = &v1.Derivation{Algorithm: "derivation.attribution", Version: "1.0.0", Evidence: []v1.EvidenceRef{},
		Inputs: []v1.DerivationInput{{CandidateID: "candidate.input.1", CandidateRevision: "1", SourcePaths: []v1.SourcePathRef{
			{BindingID: "binding.synthetic", SourceID: "source.synthetic", SourceEpochID: "epoch.synthetic", DriverGeneration: "2"}}}}}
	return c
}

// wantCandidate is the independently built expectation: the template with only
// the key and the value replaced.
func wantCandidate(tpl v1.FactCandidate, key v1.FactKey, value *v1.Value) v1.FactCandidate {
	c := cloneCandidate(tpl)
	c.Key, c.Value = key, value
	return c
}

func quantity(number v1.Decimal, unit string) *v1.Value {
	return &v1.Value{Kind: v1.ValueQuantity, Quantity: &v1.Quantity{Number: number, Unit: v1.DefinitionID(unit)}}
}

func textDimension(id, text string) v1.Dimension {
	return v1.Dimension{ID: v1.DefinitionID(id), Value: v1.Value{Kind: v1.ValueText, Text: &text}}
}

func inputKind(err error) InputErrorKind {
	var ie *InputError
	if errors.As(err, &ie) {
		return ie.Kind
	}
	return ""
}

func mustEvaluate[S any](t testing.TB, tab Table[S], a Acquired[S]) Evaluation {
	t.Helper()
	e, err := Evaluate(tab, a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return e
}
