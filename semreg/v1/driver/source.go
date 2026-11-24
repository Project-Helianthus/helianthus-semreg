package driver

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

// Element is one canonical subject with the whole versioned, dimensioned fact
// key, so two slots of one definition on one subject are two elements that
// never contend.
type Element struct {
	Subject NodeRef    `json:"subject"`
	Key     v1.FactKey `json:"key"`
}

// Validate checks the subject and the fact key.
func (e Element) Validate() error {
	return first(within("subject", e.Subject.Validate()), within("key", e.Key.Validate()))
}

// Source is one declared source of an element. The driver supplies Priority;
// this package holds no priority rule of its own. Step is the positive SI
// resolution of the source, nil when unknown.
type Source struct {
	For      Element      `json:"for"`
	Producer NodeRef      `json:"producer"`
	PathKey  string       `json:"path_key"`
	Priority int32        `json:"priority"`
	Step     *v1.Quantity `json:"step,omitempty"`
	Via      []NodeRef    `json:"via,omitempty"`
	Limits   *ValueLimits `json:"limits,omitempty"`
}

// Validate checks the element, the producer, the path key, a positive step, the
// relays and the limits.
func (s Source) Validate() error {
	if err := first(within("for", s.For.Validate()), within("producer", s.Producer.Validate()), required("path key", s.PathKey)); err != nil {
		return err
	}
	if s.Step != nil {
		if err := positiveQuantity(*s.Step); err != nil {
			return within("step", err)
		}
	}
	for i, via := range s.Via {
		if err := via.Validate(); err != nil {
			return fmt.Errorf("via[%d]: %w", i, err)
		}
	}
	if s.Limits != nil {
		return within("limits", s.Limits.Validate())
	}
	return nil
}

func positiveQuantity(q v1.Quantity) error {
	if err := q.Validate(); err != nil {
		return err
	}
	if q.Number.Coefficient == "0" || strings.HasPrefix(q.Number.Coefficient, "-") {
		return errors.New("a step is positive")
	}
	return nil
}

// SourceRef identifies a source declaration, not a lease: the element, the
// producer and the durable path key.
type SourceRef struct {
	For      Element `json:"for"`
	Producer NodeRef `json:"producer"`
	PathKey  string  `json:"path_key"`
}

// Validate checks the element, the producer and the path key.
func (r SourceRef) Validate() error {
	return first(within("for", r.For.Validate()), within("producer", r.Producer.Validate()), required("path key", r.PathKey))
}

// ReceiptStamp is the runtime receipt of an admitted sample. The runtime sets it
// at admission; a driver never does.
type ReceiptStamp struct {
	At v1.MonotonicPoint `json:"at"`
}

// Validate checks the receipt point.
func (r ReceiptStamp) Validate() error { return r.At.Validate() }

// Sample is one admitted candidate of a source with the runtime's receipt.
type Sample struct {
	From      Source           `json:"from"`
	Candidate v1.FactCandidate `json:"candidate"`
	Receipt   ReceiptStamp     `json:"receipt"`
}

// Validate checks the source, the candidate and the receipt.
func (s Sample) Validate() error {
	return first(within("from", s.From.Validate()), within("candidate", s.Candidate.Validate()), within("receipt", s.Receipt.Validate()))
}

// SettingRoute is the route to the one owner of a setting: the element, the
// operation, the owning source declaration and the owner's route.
type SettingRoute struct {
	For       Element          `json:"for"`
	Operation v1.DefinitionRef `json:"operation"`
	Owner     SourceRef        `json:"owner"`
	Route     operation.Route  `json:"route"`
	Limits    *ValueLimits     `json:"limits,omitempty"`
}

// Validate checks the element, the operation, the owner, the route and the
// limits, and requires the owner to be a source of the same element.
func (r SettingRoute) Validate() error {
	if err := first(within("for", r.For.Validate()), within("operation", r.Operation.Validate()), within("owner", r.Owner.Validate()), within("route", r.Route.Validate())); err != nil {
		return err
	}
	if !sameElement(r.For, r.Owner.For) {
		return errors.New("the owner is not a source of the route's element")
	}
	if r.Limits != nil {
		return within("limits", r.Limits.Validate())
	}
	return nil
}

func sameElement(a, b Element) bool {
	ak, aerr := v1.CanonicalJSON(a.Key)
	bk, berr := v1.CanonicalJSON(b.Key)
	return a.Subject == b.Subject && aerr == nil && berr == nil && bytes.Equal(ak, bk)
}

// compareRef orders source references by subject, canonical key, producer and
// path key.
func compareRef(a, b SourceRef) int {
	ak, _ := v1.CanonicalJSON(a.For.Key)
	bk, _ := v1.CanonicalJSON(b.For.Key)
	for _, c := range []int{
		strings.Compare(string(a.For.Subject.Bridge), string(b.For.Subject.Bridge)),
		strings.Compare(string(a.For.Subject.ID), string(b.For.Subject.ID)),
		bytes.Compare(ak, bk),
		strings.Compare(string(a.Producer.Bridge), string(b.Producer.Bridge)),
		strings.Compare(string(a.Producer.ID), string(b.Producer.ID)),
		strings.Compare(a.PathKey, b.PathKey),
	} {
		if c != 0 {
			return c
		}
	}
	return 0
}

// SourceView is one source as the runtime returns it: the declaration with its
// priority, path and resolution, and its availability.
type SourceView struct {
	Source       Source          `json:"source"`
	Availability v1.Availability `json:"availability"`
}

// Validate checks the source and the availability.
func (v SourceView) Validate() error {
	switch v.Availability {
	case v1.AvailabilityAvailable, v1.AvailabilityDegraded, v1.AvailabilityUnavailable, v1.AvailabilityWithdrawn:
	default:
		return fmt.Errorf("availability %q is not an availability", v.Availability)
	}
	return within("source", v.Source.Validate())
}

// Finding reports one pair of sources. A finding keeps its two source references
// in ascending order. With PriorityTie present it reports that both sources
// declare that priority, zero included, and carries no sample values, steps or
// incomparable marker. Without it the finding carries the sample payload:
// the two SI values, the optional steps and the incomparable state.
type Finding struct {
	First        SourceRef    `json:"first"`
	Second       SourceRef    `json:"second"`
	PriorityTie  *int32       `json:"priority_tie,omitempty"`
	FirstValue   *v1.Quantity `json:"first_value,omitempty"`
	SecondValue  *v1.Quantity `json:"second_value,omitempty"`
	FirstStep    *v1.Quantity `json:"first_step,omitempty"`
	SecondStep   *v1.Quantity `json:"second_step,omitempty"`
	Incomparable bool         `json:"incomparable,omitempty"`
}

// Validate requires two distinct source references in ascending order. A tie
// excludes the sample payload. Without a tie both SI values are required, a
// present step is positive, and a finding that is not incomparable needs both
// steps.
func (f Finding) Validate() error {
	if err := first(within("first", f.First.Validate()), within("second", f.Second.Validate())); err != nil {
		return err
	}
	if compareRef(f.First, f.Second) >= 0 {
		return errors.New("a finding needs two distinct source references in ascending order")
	}
	if f.PriorityTie != nil {
		if f.FirstValue != nil || f.SecondValue != nil || f.FirstStep != nil || f.SecondStep != nil || f.Incomparable {
			return errMixedPayload
		}
		return nil
	}
	if f.FirstValue == nil || f.SecondValue == nil {
		return errors.New("a sample finding needs both values")
	}
	for name, q := range map[string]*v1.Quantity{"first value": f.FirstValue, "second value": f.SecondValue} {
		if err := q.Validate(); err != nil {
			return within(name, err)
		}
	}
	for name, q := range map[string]*v1.Quantity{"first step": f.FirstStep, "second step": f.SecondStep} {
		if q != nil {
			if err := positiveQuantity(*q); err != nil {
				return within(name, err)
			}
		}
	}
	if !f.Incomparable && (f.FirstStep == nil || f.SecondStep == nil) {
		return errors.New("a disagreement needs both steps; with an unknown step the finding is incomparable")
	}
	return nil
}

// Resolution is the runtime's resolution of one element: the winning source, if
// any, every source and the findings in ascending order.
type Resolution struct {
	Winner   *SourceRef   `json:"winner,omitempty"`
	Sources  []SourceView `json:"sources"`
	Findings []Finding    `json:"findings,omitempty"`
}

// Validate checks every source and finding, requires the winner to be one of
// the sources and the findings to be in strictly ascending order.
func (r Resolution) Validate() error {
	for i, s := range r.Sources {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("sources[%d]: %w", i, err)
		}
	}
	if r.Winner != nil {
		if err := r.Winner.Validate(); err != nil {
			return within("winner", err)
		}
		found := false
		for _, s := range r.Sources {
			ref := SourceRef{For: s.Source.For, Producer: s.Source.Producer, PathKey: s.Source.PathKey}
			if compareRef(ref, *r.Winner) == 0 {
				found = true
			}
		}
		if !found {
			return errors.New("the winner is not one of the sources")
		}
	}
	for i, f := range r.Findings {
		if err := f.Validate(); err != nil {
			return fmt.Errorf("findings[%d]: %w", i, err)
		}
		if i > 0 {
			prev := r.Findings[i-1]
			if c := compareRef(prev.First, f.First); c > 0 || (c == 0 && compareRef(prev.Second, f.Second) >= 0) {
				return fmt.Errorf("findings[%d] is not in strictly ascending order", i)
			}
		}
	}
	return nil
}
