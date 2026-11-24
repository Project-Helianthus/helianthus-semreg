# Namespace plan

This plan states the names GraphQL and MCP use for the SemReg runtime tree. It
is the same plan for both surfaces. `plan.go` is its Go form and is what the
tests check. The plan names no driver and no vendor.

## 1. Semantic namespace

The semantic namespace is addressed by resource, lens, plane or definition
identifiers only (`api/v1/graph.md` API-4). The four graph reads and their names
are the ones `api/v1/graph.md` section 30 fixes:

| Read | GraphQL field | MCP tool |
|---|---|---|
| Listing | `semantic_resources` | `semantic_v1_resources_list` |
| Traversal | `semantic_traversal` | `semantic_v1_traverse` |
| Lens retrieval | `semantic_lens` | `semantic_v1_lens_get` |
| Element catalog | `semantic_element_catalog` | `semantic_v1_catalog_get` |

The four reads return graph reads only. A driver's diagnostics are not part of
them; they stay in the driver's namespace (section 3).

A read keyed by a native profile, a vendor or a pack belongs to its driver's
namespace. It moves there under the rule of section 3, and its result stays
byte-identical apart from its name. No alias and no compatibility table is kept
for the old name.

## 2. Tree verbs

The tree has nine verbs. Their names are `tree_<verb>` in GraphQL and
`tree_v1_<verb>` in MCP:

`capture`, `release`, `list`, `describe`, `read`, `prepare`, `call`, `outcome`,
`changes`.

| Verb | Go form |
|---|---|
| capture | `Tree.Capture` |
| release | `TreeCut.Close` / `Tree.Release` |
| list | `TreeCut.List` |
| describe | `TreeCut.Describe` |
| read | `TreeCut.Read` |
| prepare | `Tree.Prepare` |
| call | `Tree.Call` |
| outcome | `Tree.Outcome` |
| changes | `Tree.Resume` |

## 3. Driver namespaces

A driver reserves a `NamespaceID` at planning time. One `NamespaceID` has one
owner; a second owner is a planning error. The one rule that derives the names
of a member in both surfaces from the `NamespaceID` and the member's tree key is:

- GraphQL: `<namespace>_<verb>`
- MCP: `<namespace>_v1_<verb>`
- the argument of both is the member's tree key, unchanged.

An attribute takes `describe` and `read`. A method (and a procedure, which is a
method on an instance or transport node) takes `describe`, `prepare` and `call`.
`outcome` is the tree's own verb over a receipt.

The key is the native key of that namespace: the node path of the grammar below,
`attributes` or `methods`, `native`, and one segment holding the namespace and
the member's stable local id. The same key addresses the member in GraphQL and
in MCP.

A `NamespaceID` that is not lowercase letters, digits and underscores starting
with a letter, a key that is not a native key of that namespace, and a verb the
member's facet does not take have no name. A member or read with no name from
this rule is a stop-and-report point; no name is invented.

## 4. Tree keys

```
/installations/<installation>/instances/<driver-kind>/<instance>/transports/<transport>/devices/<device>/{attributes,methods}/{sem,native}/<key>
```

- Every opaque segment is the unpadded base64url of the canonical byte tuple of
  its parts (each part as its uvarint length and its bytes), encoded once.
- A semantic attribute key is the canonical JSON of the qualified fact key
  (pack, version, fact and canonical dimensions, the subject among them); a
  semantic method key is the canonical JSON of the operation's definition
  reference; a native key is the namespace and the stable local id.
- A non-canonical encoding, an invalid ancestry (a transport outside an
  instance, a device outside a transport or device) and a key that does not
  render back to the same text are rejected.
- No segment is derived from an address, a certificate identifier, a serial, a
  slice position, a generation or a display unit.

## 5. Reads are detached

Reading a member returns the driver's retained value with its time; nothing
here starts, schedules or sends a bus request. A method is sent only when it is
called.
