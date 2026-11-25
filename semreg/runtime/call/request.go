package call

import (
	"bytes"
	"math/big"
	"strings"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func validateRequest(req driver.OperationRequest, finite bool) *routeRefusal {
	if err := req.Validate(); err != nil {
		return failed(ReasonInvalidArgument)
	}
	if finite && req.Timeout <= 0 {
		return failed(ReasonInvalidArgument)
	}
	if req.Arguments == nil || req.Preconditions == nil {
		return failed(ReasonInvalidArgument)
	}
	if refusal := argumentShape(req.Arguments); refusal != nil {
		return refusal
	}
	return preconditionShape(req.Preconditions)
}

func preconditionShape(preconditions []operation.Precondition) *routeRefusal {
	seen := map[string]struct{}{}
	var priorFact []byte
	var priorID string
	var priorRevision semreg.Uint64
	for i, precondition := range preconditions {
		fact, err := semreg.CanonicalJSON(precondition.Fact)
		if err != nil {
			return failed(ReasonInvalidArgument)
		}
		key := string(fact) + "\x00" + string(precondition.CandidateID) + "\x00" + string(precondition.CandidateRevision)
		if _, dup := seen[key]; dup {
			return failed(ReasonDuplicatePrecondition)
		}
		seen[key] = struct{}{}
		if i > 0 {
			cmp := bytes.Compare(priorFact, fact)
			if cmp == 0 {
				cmp = strings.Compare(priorID, string(precondition.CandidateID))
			}
			if cmp == 0 {
				cmp = compareUint(priorRevision, precondition.CandidateRevision)
			}
			if cmp > 0 {
				return failed(ReasonNoncanonicalPre)
			}
		}
		priorFact, priorID, priorRevision = fact, string(precondition.CandidateID), precondition.CandidateRevision
	}
	return nil
}

func compareUint(left, right semreg.Uint64) int {
	lv, lok := new(big.Int).SetString(string(left), 10)
	rv, rok := new(big.Int).SetString(string(right), 10)
	if !lok || !rok {
		return strings.Compare(string(left), string(right))
	}
	return lv.Cmp(rv)
}
