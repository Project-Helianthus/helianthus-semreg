package semreg

import (
	"encoding/json"
	"reflect"
	"testing"
)

type taggedFieldWinner struct {
	Wire  int
	Bound bool `json:"Wire"`
}

func (taggedFieldWinner) Validate() error { return nil }

func TestEffectiveJSONFieldsTaggedDirectWinner(t *testing.T) {
	fields := effectiveJSONFields(reflect.TypeOf(taggedFieldWinner{}))
	field, found := fields["Wire"]
	if !found || field.Name != "Bound" || len(field.Index) != 1 || field.Index[0] != 1 {
		t.Fatalf("effective Wire field = %#v, found=%t", field, found)
	}
	decoded, err := Decode[taggedFieldWinner]([]byte(`{"Wire":true}`))
	if err != nil || !decoded.Bound || decoded.Wire != 0 {
		t.Fatalf("tagged winner decode = %+v, %v", decoded, err)
	}
	_, err = Decode[taggedFieldWinner]([]byte(`{}`))
	requireID(t, err, MissingMember)
}

type promotedDiscardedItems struct {
	Items []DefinitionID `json:"items"`
}

type anonymousPromotionShape struct {
	promotedDiscardedItems
	Required bool `json:"required"`
}

func (anonymousPromotionShape) Validate() error { return nil }

func TestEffectiveJSONFieldsAnonymousPromotionDoesNotInventNesting(t *testing.T) {
	raw := []byte(`{"promotedDiscardedItems":{"items":["fact.test","fact.test"]}}`)
	var ordinary anonymousPromotionShape
	if err := json.Unmarshal(raw, &ordinary); err != nil || ordinary.Items != nil {
		t.Fatalf("ordinary binding = %+v, %v", ordinary, err)
	}
	fields := effectiveJSONFields(reflect.TypeOf(anonymousPromotionShape{}))
	if _, found := fields["promotedDiscardedItems"]; found {
		t.Fatal("untagged anonymous struct acquired a named JSON binding")
	}
	field, found := fields["items"]
	if !found || len(field.Index) != 2 || field.Index[0] != 0 || field.Index[1] != 0 {
		t.Fatalf("promoted items field = %#v, found=%t", field, found)
	}
	_, err := Decode[anonymousPromotionShape](raw)
	requireID(t, err, MissingMember)
}

func TestEffectiveJSONFieldsCacheReuse(t *testing.T) {
	type cached struct {
		Value string `json:"value"`
	}
	typ := reflect.TypeOf(cached{})
	first := cachedJSONFieldMetadata(typ)
	if first != cachedJSONFieldMetadata(typ) {
		t.Fatal("effective JSON metadata was not reused")
	}
	if allocations := testing.AllocsPerRun(100, func() {
		_ = cachedJSONFieldMetadata(typ)
	}); allocations != 0 {
		t.Fatalf("cached JSON metadata lookup allocated: %.0f", allocations)
	}
}
