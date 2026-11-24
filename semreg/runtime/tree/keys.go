package tree

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// Keyword is a fixed word of the tree grammar:
// /installations/<installation>/instances/<driver-kind>/<instance>/transports/<transport>/devices/<device>/{attributes,methods}/{sem,native}/<key>
type Keyword string

// Keyword values.
const (
	KeywordInstallations Keyword = "installations"
	KeywordInstances     Keyword = "instances"
	KeywordTransports    Keyword = "transports"
	KeywordDevices       Keyword = "devices"
	KeywordAttributes    Keyword = "attributes"
	KeywordMethods       Keyword = "methods"
	KeywordSem           Keyword = "sem"
	KeywordNative        Keyword = "native"
)

// ErrKey is wrapped by every key rejection.
var ErrKey = errors.New("tree key")

func keyError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrKey, fmt.Sprintf(format, args...))
}

var segmentEncoding = base64.RawURLEncoding.Strict()

// EncodeSegment encodes the canonical byte tuple of parts once, as unpadded
// base64url. Each part is its uvarint length and its bytes. A tuple has at least
// one part and no part is empty.
func EncodeSegment(parts ...string) (string, error) {
	if len(parts) == 0 {
		return "", keyError("a segment has at least one part")
	}
	var raw []byte
	for _, part := range parts {
		if part == "" {
			return "", keyError("a segment part is empty")
		}
		raw = binary.AppendUvarint(raw, uint64(len(part)))
		raw = append(raw, part...)
	}
	return segmentEncoding.EncodeToString(raw), nil
}

// DecodeSegment is the inverse of EncodeSegment and rejects every encoding that
// EncodeSegment would not produce.
func DecodeSegment(segment string) ([]string, error) {
	raw, err := segmentEncoding.DecodeString(segment)
	if err != nil {
		return nil, keyError("segment is not unpadded base64url")
	}
	var parts []string
	for len(raw) > 0 {
		length, n := binary.Uvarint(raw)
		if n <= 0 || length == 0 || length > uint64(len(raw)-n) {
			return nil, keyError("segment tuple is malformed")
		}
		parts = append(parts, string(raw[n:n+int(length)]))
		raw = raw[n+int(length):]
	}
	if len(parts) == 0 {
		return nil, keyError("segment tuple is empty")
	}
	again, err := EncodeSegment(parts...)
	if err != nil || again != segment {
		return nil, keyError("segment is not canonical")
	}
	return parts, nil
}

func encodeOne(s string) (string, error) { return EncodeSegment(s) }

func decodeOne(segment string) (string, error) {
	parts, err := DecodeSegment(segment)
	if err != nil {
		return "", err
	}
	if len(parts) != 1 {
		return "", keyError("segment is not one part")
	}
	return parts[0], nil
}

// NodePath is the path of one node. Its level follows from which members are
// set: installation only; with a driver and instance, an instance; with a
// transport, a transport; with devices, a device (devices nest).
type NodePath struct {
	Installation string
	Driver       driver.DriverKind
	Instance     driver.InstanceKey
	Transport    driver.NodeID
	Devices      []driver.NodeID
}

// Level names the level of a node.
type Level string

// Level values.
const (
	LevelInstallation Level = "installation"
	LevelInstance     Level = "instance"
	LevelTransport    Level = "transport"
	LevelDevice       Level = "device"
)

// Level returns the level of the node, or an error for an ancestry the grammar
// does not allow.
func (p NodePath) Level() (Level, error) {
	switch {
	case p.Installation == "":
		return "", keyError("a node has an installation")
	case p.Driver == "" && p.Instance == "" && p.Transport == "" && len(p.Devices) == 0:
		return LevelInstallation, nil
	case p.Driver == "" || p.Instance == "":
		return "", keyError("an instance has a driver kind and an instance key")
	case p.Transport == "" && len(p.Devices) == 0:
		return LevelInstance, nil
	case p.Transport == "":
		return "", keyError("a device lies under a transport")
	case len(p.Devices) == 0:
		return LevelTransport, nil
	}
	return LevelDevice, nil
}

// Child returns the path of the child with the local id: under an instance a
// transport, under a transport or a device a device.
func (p NodePath) Child(local driver.NodeID) (NodePath, error) {
	level, err := p.Level()
	if err != nil {
		return NodePath{}, err
	}
	if err := local.Validate(); err != nil {
		return NodePath{}, err
	}
	child := NodePath{Installation: p.Installation, Driver: p.Driver, Instance: p.Instance, Transport: p.Transport, Devices: append([]driver.NodeID(nil), p.Devices...)}
	switch level {
	case LevelInstallation:
		return NodePath{}, keyError("an instance is not a child declared by a driver")
	case LevelInstance:
		child.Transport = local
	default:
		child.Devices = append(child.Devices, local)
	}
	return child, nil
}

// Parent returns the path one level up and false at the installation.
func (p NodePath) Parent() (NodePath, bool) {
	level, err := p.Level()
	if err != nil || level == LevelInstallation {
		return NodePath{}, false
	}
	parent := NodePath{Installation: p.Installation, Driver: p.Driver, Instance: p.Instance, Transport: p.Transport}
	switch level {
	case LevelInstance:
		parent = NodePath{Installation: p.Installation}
	case LevelTransport:
		parent.Transport = ""
	default:
		parent.Devices = append([]driver.NodeID(nil), p.Devices[:len(p.Devices)-1]...)
	}
	return parent, true
}

// Within reports whether the path is the other path or lies under it.
func (p NodePath) Within(root NodePath) bool {
	for at, ok := p, true; ok; at, ok = at.Parent() {
		if at.equal(root) {
			return true
		}
	}
	return false
}

func (p NodePath) equal(o NodePath) bool {
	if p.Installation != o.Installation || p.Driver != o.Driver || p.Instance != o.Instance || p.Transport != o.Transport || len(p.Devices) != len(o.Devices) {
		return false
	}
	for i := range p.Devices {
		if p.Devices[i] != o.Devices[i] {
			return false
		}
	}
	return true
}

// String renders the path with its leading slash.
func (p NodePath) String() string {
	level, err := p.Level()
	if err != nil {
		return ""
	}
	one := func(s string) string { out, _ := encodeOne(s); return out }
	out := "/" + string(KeywordInstallations) + "/" + one(p.Installation)
	if level == LevelInstallation {
		return out
	}
	out += "/" + string(KeywordInstances) + "/" + one(string(p.Driver)) + "/" + one(string(p.Instance))
	if level == LevelInstance {
		return out
	}
	out += "/" + string(KeywordTransports) + "/" + one(string(p.Transport))
	for _, device := range p.Devices {
		out += "/" + string(KeywordDevices) + "/" + one(string(device))
	}
	return out
}

// NodeID returns the resource id of the node: its path without the leading
// slash.
func (p NodePath) NodeID() semreg.ResourceID {
	return semreg.ResourceID(strings.TrimPrefix(p.String(), "/"))
}

// ParseNodePath parses a node path and rejects a non-canonical encoding and an
// invalid ancestry.
func ParseNodePath(s string) (NodePath, error) {
	path, rest, err := parseNode(s)
	if err != nil {
		return NodePath{}, err
	}
	if len(rest) != 0 {
		return NodePath{}, keyError("the path continues past a node")
	}
	return path, nil
}

func parseNode(s string) (NodePath, []string, error) {
	if !strings.HasPrefix(s, "/") {
		return NodePath{}, nil, keyError("a path starts with a slash")
	}
	steps := strings.Split(s[1:], "/")
	take := func(word Keyword, count int) ([]string, error) {
		if len(steps) < 1+count || steps[0] != string(word) {
			return nil, keyError("expected %s", word)
		}
		out := make([]string, count)
		for i := 0; i < count; i++ {
			value, err := decodeOne(steps[1+i])
			if err != nil {
				return nil, err
			}
			out[i] = value
		}
		steps = steps[1+count:]
		return out, nil
	}
	installation, err := take(KeywordInstallations, 1)
	if err != nil {
		return NodePath{}, nil, err
	}
	path := NodePath{Installation: installation[0]}
	if len(steps) == 0 || steps[0] != string(KeywordInstances) {
		return path, steps, nil
	}
	instance, err := take(KeywordInstances, 2)
	if err != nil {
		return NodePath{}, nil, err
	}
	path.Driver, path.Instance = driver.DriverKind(instance[0]), driver.InstanceKey(instance[1])
	if len(steps) == 0 || steps[0] != string(KeywordTransports) {
		return path, steps, nil
	}
	transport, err := take(KeywordTransports, 1)
	if err != nil {
		return NodePath{}, nil, err
	}
	path.Transport = driver.NodeID(transport[0])
	for len(steps) > 0 && steps[0] == string(KeywordDevices) {
		device, err := take(KeywordDevices, 1)
		if err != nil {
			return NodePath{}, nil, err
		}
		path.Devices = append(path.Devices, driver.NodeID(device[0]))
	}
	return path, steps, nil
}

// Facet says whether a key is an attribute or a method.
type Facet = Keyword

// Domain says whether a key is semantic or native.
type Domain = Keyword

// Key is the one key of an attribute or a method: the node it belongs to, its
// facet, its domain and its encoded segment.
type Key struct {
	Node    NodePath
	Facet   Facet
	Domain  Domain
	Segment string
}

// String renders the key.
func (k Key) String() string {
	node := k.Node.String()
	if node == "" {
		return ""
	}
	return node + "/" + string(k.Facet) + "/" + string(k.Domain) + "/" + k.Segment
}

// ParseKey parses an attribute or method key. It rejects a non-canonical
// encoding, an invalid ancestry and anything the grammar does not name, and a
// key that does not render back to the same text.
func ParseKey(s string) (Key, error) {
	node, rest, err := parseNode(s)
	if err != nil {
		return Key{}, err
	}
	if _, err := node.Level(); err != nil {
		return Key{}, err
	}
	if len(rest) != 3 {
		return Key{}, keyError("a key is a node, a facet, a domain and a segment")
	}
	key := Key{Node: node, Facet: Facet(rest[0]), Domain: Domain(rest[1]), Segment: rest[2]}
	if key.Facet != KeywordAttributes && key.Facet != KeywordMethods {
		return Key{}, keyError("facet %q is neither attributes nor methods", rest[0])
	}
	if key.Domain != KeywordSem && key.Domain != KeywordNative {
		return Key{}, keyError("domain %q is neither sem nor native", rest[1])
	}
	parts, err := DecodeSegment(key.Segment)
	if err != nil {
		return Key{}, err
	}
	if key.Domain == KeywordNative && len(parts) != 2 {
		return Key{}, keyError("a native segment is a namespace and a local id")
	}
	if key.Domain == KeywordSem && key.Facet == KeywordAttributes && len(parts) != 1 && !(len(parts) == 2 && parts[0] == IndexPart) {
		return Key{}, keyError("a semantic attribute segment is a fact key or an index")
	}
	if key.Domain == KeywordSem && key.Facet == KeywordMethods && len(parts) != 1 {
		return Key{}, keyError("a semantic method segment is one definition reference")
	}
	if key.String() != s {
		return Key{}, keyError("key is not canonical")
	}
	return key, nil
}

// IndexPart is the first part of the segment of an installation index
// attribute.
const IndexPart = "index"

// NativeSegment is the segment of a native member or method: its namespace and
// its stable local id.
func NativeSegment(namespace driver.NamespaceID, local driver.MemberID) (string, error) {
	return EncodeSegment(string(namespace), string(local))
}

// ParseNativeSegment is the inverse of NativeSegment.
func ParseNativeSegment(segment string) (driver.NamespaceID, driver.MemberID, error) {
	parts, err := DecodeSegment(segment)
	if err != nil {
		return "", "", err
	}
	if len(parts) != 2 {
		return "", "", keyError("a native segment is a namespace and a local id")
	}
	return driver.NamespaceID(parts[0]), driver.MemberID(parts[1]), nil
}

// FactSegment is the reversible segment of a semantic attribute: the canonical
// JSON of its qualified fact key, which holds the pack, the fact and the
// canonical dimensions, the subject among them.
func FactSegment(key semreg.FactKey) (string, error) {
	canonical, err := semreg.CanonicalJSON(key)
	if err != nil {
		return "", err
	}
	return EncodeSegment(string(canonical))
}

// ParseFactSegment is the inverse of FactSegment.
func ParseFactSegment(segment string) (semreg.FactKey, error) {
	text, err := decodeOne(segment)
	if err != nil {
		return semreg.FactKey{}, err
	}
	key, err := semreg.Decode[semreg.FactKey]([]byte(text))
	if err != nil {
		return semreg.FactKey{}, err
	}
	if again, err := FactSegment(key); err != nil || again != segment {
		return semreg.FactKey{}, keyError("fact segment is not canonical")
	}
	return key, nil
}

// DefinitionSegment is the reversible segment of a semantic method: the
// canonical JSON of the operation's definition reference.
func DefinitionSegment(ref semreg.DefinitionRef) (string, error) {
	canonical, err := semreg.CanonicalJSON(ref)
	if err != nil {
		return "", err
	}
	return EncodeSegment(string(canonical))
}

// ParseDefinitionSegment is the inverse of DefinitionSegment.
func ParseDefinitionSegment(segment string) (semreg.DefinitionRef, error) {
	text, err := decodeOne(segment)
	if err != nil {
		return semreg.DefinitionRef{}, err
	}
	ref, err := semreg.Decode[semreg.DefinitionRef]([]byte(text))
	if err != nil {
		return semreg.DefinitionRef{}, err
	}
	if again, err := DefinitionSegment(ref); err != nil || again != segment {
		return semreg.DefinitionRef{}, keyError("definition segment is not canonical")
	}
	return ref, nil
}
