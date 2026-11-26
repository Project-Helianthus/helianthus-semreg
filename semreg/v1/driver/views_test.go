package driver_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// The existing session double only exercises node and member registration.
func (*syntheticNative) View(driver.ViewSpec) error {
	return errors.New("view registration is not implemented by this session double")
}

func TestViewDeclarationRoundTrip(t *testing.T) {
	spec := driver.ViewSpec{Root: "Report", Records: []driver.ViewRecord{
		{Name: "Report", Fields: []driver.ViewField{
			{Name: "samples", Type: driver.ViewType{Name: "Sample", Record: true, List: true}, Member: memberKey()},
		}},
		{Name: "Sample", Fields: []driver.ViewField{
			{Name: "reading", Type: driver.ViewType{Name: "Amount", Nullable: true}, Unit: "test units", Member: driver.MemberKey{
				Namespace: "namespace.other", Instance: "instance.two", Node: "node.two", Local: "member.two",
			}},
		}},
	}}
	encoded, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	var decoded driver.ViewSpec
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, spec) {
		t.Fatalf("decoded declaration = %+v; want %+v", decoded, spec)
	}
}
