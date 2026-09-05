package semreg

// Hook inputs cross the registered-validator boundary as detached typed values.
// Copy the closed public records directly, preserving nil/empty distinctions
// and exact scalar values without a serialization round trip.
func copyHookPointer[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func copyHookSlice[T any](s []T) []T {
	if s == nil {
		return nil
	}
	return append(make([]T, 0, len(s)), s...)
}

func copyHookValue(v Value) Value {
	v.Quantity = copyHookPointer(v.Quantity)
	v.Boolean = copyHookPointer(v.Boolean)
	v.Text = copyHookPointer(v.Text)
	v.Symbol = copyHookPointer(v.Symbol)
	v.Symbols = copyHookSlice(v.Symbols)
	v.Time = copyHookPointer(v.Time)
	return v
}

func copyHookKey(k FactKey) FactKey {
	k.Dimensions = copyHookSlice(k.Dimensions)
	for i := range k.Dimensions {
		k.Dimensions[i].Value = copyHookValue(k.Dimensions[i].Value)
	}
	return k
}

func copyHookField(f TypedField) TypedField {
	f.Value = copyHookValue(f.Value)
	return f
}

func copyHookFields(f []TypedField) []TypedField {
	f = copyHookSlice(f)
	for i := range f {
		f[i] = copyHookField(f[i])
	}
	return f
}

func copyHookCapability(c CapabilityInstance) CapabilityInstance {
	c.Constraints = copyHookFields(c.Constraints)
	c.ActivationEvidence = copyHookSlice(c.ActivationEvidence)
	return c
}

func copyHookOrigin(o OriginRef) OriginRef {
	o.SourceID = copyHookPointer(o.SourceID)
	o.SourceEpochID = copyHookPointer(o.SourceEpochID)
	o.BindingID = copyHookPointer(o.BindingID)
	o.Evidence = copyHookSlice(o.Evidence)
	return o
}

func copyHookCandidate(c FactCandidate) FactCandidate {
	c.Key = copyHookKey(c.Key)
	if c.Value != nil {
		v := copyHookValue(*c.Value)
		c.Value = &v
	}
	c.Quality.Reasons = copyHookSlice(c.Quality.Reasons)
	c.Times.PhenomenonAt = copyHookPointer(c.Times.PhenomenonAt)
	c.Times.SourceAt = copyHookPointer(c.Times.SourceAt)
	c.BindingID = copyHookPointer(c.BindingID)
	c.SourceEpochID = copyHookPointer(c.SourceEpochID)
	c.DriverGeneration = copyHookPointer(c.DriverGeneration)
	c.Origin = copyHookOrigin(c.Origin)
	c.Causal = copyHookPointer(c.Causal)
	if c.Causal != nil {
		c.Causal.Origin = copyHookOrigin(c.Causal.Origin)
		c.Causal.ParentCorrelationID = copyHookPointer(c.Causal.ParentCorrelationID)
		c.Causal.Path = copyHookSlice(c.Causal.Path)
	}
	c.Evidence = copyHookSlice(c.Evidence)
	c.Derivation = copyHookPointer(c.Derivation)
	if c.Derivation != nil {
		c.Derivation.Inputs = copyHookSlice(c.Derivation.Inputs)
		for i := range c.Derivation.Inputs {
			c.Derivation.Inputs[i].SourcePaths = copyHookSlice(c.Derivation.Inputs[i].SourcePaths)
		}
		c.Derivation.Evidence = copyHookSlice(c.Derivation.Evidence)
	}
	return c
}
