package tree

import (
	"fmt"
	"regexp"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// Surface is one of the two surfaces that carry the namespace plan.
type Surface string

// Surface values.
const (
	SurfaceGraphQL Surface = "graphql"
	SurfaceMCP     Surface = "mcp"
)

// Read is one of the four generic graph reads of the semantic namespace, with
// the names api/v1/graph.md section 30 fixes.
type Read struct {
	Read    string
	GraphQL string
	MCP     string
}

// SemanticReads are the four graph reads. Their arguments are resource, lens,
// plane and definition identifiers only (API-4); a read keyed by a native
// profile, a vendor or a pack belongs to its driver's namespace.
var SemanticReads = []Read{
	{Read: "listing", GraphQL: "semantic_resources", MCP: "semantic_v1_resources_list"},
	{Read: "traversal", GraphQL: "semantic_traversal", MCP: "semantic_v1_traverse"},
	{Read: "lens", GraphQL: "semantic_lens", MCP: "semantic_v1_lens_get"},
	{Read: "catalog", GraphQL: "semantic_element_catalog", MCP: "semantic_v1_catalog_get"},
}

// SemanticArguments are the only identifiers that address the semantic
// namespace (API-4).
var SemanticArguments = []string{"resource", "lens", "plane", "definition"}

// Verb is one tree verb.
type Verb string

// Verb values.
const (
	VerbCapture  Verb = "capture"
	VerbRelease  Verb = "release"
	VerbList     Verb = "list"
	VerbDescribe Verb = "describe"
	VerbRead     Verb = "read"
	VerbPrepare  Verb = "prepare"
	VerbCall     Verb = "call"
	VerbOutcome  Verb = "outcome"
	VerbChanges  Verb = "changes"
)

// TreeVerbs are the tree verbs, in the order of the plan.
var TreeVerbs = []Verb{VerbCapture, VerbRelease, VerbList, VerbDescribe, VerbRead, VerbPrepare, VerbCall, VerbOutcome, VerbChanges}

// TreeName is the name of a tree verb in a surface: tree_<verb> in GraphQL and
// tree_v1_<verb> in MCP.
func TreeName(surface Surface, verb Verb) (string, error) {
	if !knownVerb(verb) {
		return "", fmt.Errorf("%w: verb %q", ErrNoName, verb)
	}
	switch surface {
	case SurfaceGraphQL:
		return "tree_" + string(verb), nil
	case SurfaceMCP:
		return "tree_v1_" + string(verb), nil
	}
	return "", fmt.Errorf("%w: surface %q", ErrNoName, surface)
}

// ErrNoName is returned where the plan gives no name; no name is invented.
var ErrNoName = fmt.Errorf("the namespace plan gives no name")

var namespacePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func knownVerb(verb Verb) bool {
	for _, known := range TreeVerbs {
		if known == verb {
			return true
		}
	}
	return false
}

// DriverName is the name of one verb on one member in both surfaces, and the
// key that is its one argument in both.
type DriverName struct {
	GraphQL string
	MCP     string
	Key     string
}

// DriverNames derives the names of a driver namespace's member from the
// NamespaceID the driver reserved and the member's tree key: the surface name is
// <namespace>_<verb> in GraphQL and <namespace>_v1_<verb> in MCP, and the key is
// the one argument of both. An attribute takes describe and read; a method takes
// describe, prepare and call. A NamespaceID that is not a valid name in both
// surfaces, or a key that is not a native key of that namespace, has no name.
func DriverNames(namespace driver.NamespaceID, verb Verb, key string) (DriverName, error) {
	if !namespacePattern.MatchString(string(namespace)) {
		return DriverName{}, fmt.Errorf("%w: namespace %q", ErrNoName, namespace)
	}
	parsed, err := ParseKey(key)
	if err != nil {
		return DriverName{}, err
	}
	if parsed.Domain != KeywordNative {
		return DriverName{}, fmt.Errorf("%w: %s is not a native key", ErrNoName, key)
	}
	owner, _, err := ParseNativeSegment(parsed.Segment)
	if err != nil {
		return DriverName{}, err
	}
	if owner != namespace {
		return DriverName{}, fmt.Errorf("%w: key belongs to namespace %q", ErrNoName, owner)
	}
	switch {
	case parsed.Facet == KeywordAttributes && (verb == VerbDescribe || verb == VerbRead):
	case parsed.Facet == KeywordMethods && (verb == VerbDescribe || verb == VerbPrepare || verb == VerbCall):
	default:
		return DriverName{}, fmt.Errorf("%w: verb %q on a %s key", ErrNoName, verb, parsed.Facet)
	}
	return DriverName{GraphQL: string(namespace) + "_" + string(verb), MCP: string(namespace) + "_v1_" + string(verb), Key: key}, nil
}
