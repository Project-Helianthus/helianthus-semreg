package driver

// ViewSpec declares one typed view. Root names its entry record; Records
// contains the root and all records named by its fields. View and record names
// are scoped to the bound namespace and instance. Views take no arguments.
type ViewSpec struct {
	Root    string       `json:"root"`
	Records []ViewRecord `json:"records"`
}

// ViewRecord is a driver-named record and its fields, in declaration order.
type ViewRecord struct {
	Name   string      `json:"name"`
	Fields []ViewField `json:"fields"`
}

// ViewField is a named, typed field sourced from an already registered readable
// member of this instance and namespace. Node in the MemberKey is the ID of
// the registered node; Unit is the driver's unit label, when supplied.
type ViewField struct {
	Name   string    `json:"name"`
	Type   ViewType  `json:"type"`
	Unit   string    `json:"unit,omitempty"`
	Member MemberKey `json:"member"`
}

// ViewType names a scalar supplied by the driver, or a record in the view if
// Record is true. List means a list of that type. Nullable describes whether
// the field itself can be null. The registry does not interpret the values.
type ViewType struct {
	Name     string `json:"name"`
	Record   bool   `json:"record,omitempty"`
	List     bool   `json:"list,omitempty"`
	Nullable bool   `json:"nullable,omitempty"`
}
