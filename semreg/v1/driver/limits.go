package driver

import (
	"fmt"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// LimitOrigin says where a value limit comes from. There is no other origin.
type LimitOrigin string

// LimitOrigin values.
const (
	// LimitDeviceQuery means the device was queried when SemReg was first
	// populated. A queried limit applies over the specification's.
	LimitDeviceQuery LimitOrigin = "device_query"
	// LimitSpecification means the limit is a clause of the specification. It
	// applies only where no query exists.
	LimitSpecification LimitOrigin = "specification"
)

// ValueLimits are the limits of one value with their origin. This package
// defines no limit value; a driver publishes the queried limit when both a
// queried and a specification limit exist for one element.
type ValueLimits struct {
	Min      *v1.Quantity     `json:"min,omitempty"`
	Max      *v1.Quantity     `json:"max,omitempty"`
	Step     *v1.Quantity     `json:"step,omitempty"`
	Origin   LimitOrigin      `json:"origin"`
	Evidence []v1.EvidenceRef `json:"evidence"`
}

// Validate accepts only the two origins and requires evidence naming the query
// observation or the specification clause.
func (l ValueLimits) Validate() error {
	if l.Origin != LimitDeviceQuery && l.Origin != LimitSpecification {
		return fmt.Errorf("limit origin %q is neither device_query nor specification", l.Origin)
	}
	for name, q := range map[string]*v1.Quantity{"min": l.Min, "max": l.Max, "step": l.Step} {
		if q != nil {
			if err := q.Validate(); err != nil {
				return within(name, err)
			}
		}
	}
	return validateEvidence("limit evidence", l.Evidence)
}
