package roweval

import v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"

func cp[T any](s []T) []T {
	if s == nil {
		return nil
	}
	return append([]T{}, s...)
}

func cpPtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneOrigin(o v1.OriginRef) v1.OriginRef {
	o.SourceID, o.SourceEpochID, o.BindingID = cpPtr(o.SourceID), cpPtr(o.SourceEpochID), cpPtr(o.BindingID)
	o.Evidence = cp(o.Evidence)
	return o
}

// cloneCandidate copies a candidate so that nothing of the caller's template is
// shared with the result.
func cloneCandidate(c v1.FactCandidate) v1.FactCandidate {
	c.Key.Dimensions = cp(c.Key.Dimensions)
	if c.Value != nil {
		v := *c.Value
		v.Quantity, v.Boolean, v.Text, v.Time = cpPtr(v.Quantity), cpPtr(v.Boolean), cpPtr(v.Text), cpPtr(v.Time)
		v.Symbol, v.Symbols = cpPtr(v.Symbol), cp(v.Symbols)
		c.Value = &v
	}
	c.Quality.Reasons = cp(c.Quality.Reasons)
	c.Times.PhenomenonAt, c.Times.SourceAt = cpPtr(c.Times.PhenomenonAt), cpPtr(c.Times.SourceAt)
	c.BindingID, c.SourceEpochID, c.DriverGeneration = cpPtr(c.BindingID), cpPtr(c.SourceEpochID), cpPtr(c.DriverGeneration)
	c.Origin = cloneOrigin(c.Origin)
	if c.Causal != nil {
		z := *c.Causal
		z.Origin = cloneOrigin(z.Origin)
		z.ParentCorrelationID = cpPtr(z.ParentCorrelationID)
		z.Path = cp(z.Path)
		c.Causal = &z
	}
	c.Evidence = cp(c.Evidence)
	if c.Derivation != nil {
		d := *c.Derivation
		d.Inputs = cp(d.Inputs)
		for i := range d.Inputs {
			d.Inputs[i].SourcePaths = cp(d.Inputs[i].SourcePaths)
		}
		d.Evidence = cp(d.Evidence)
		c.Derivation = &d
	}
	return c
}
