package identity

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// pathNodeDomain separates the identifiers of new path nodes from the legacy
// resource identifiers.
const pathNodeDomain = "helianthus.semgraph.path-node/v1"

// Anchor tags of a path-node key.
const (
	anchorTagSlot = "slot"
	anchorTagKey  = "key"
)

// pathNodeID is "resource:" and the first 32 lowercase hexadecimal characters
// of SHA-256 over the domain bytes followed, for each field in order, by its
// UTF-8 byte length as four big-endian bytes and then its bytes. The fields
// are the installation, the driver kind, the instance key, the node kind, the
// anchor tag and the anchor members: the parent and the slot for a slot
// anchor, the identity digest for a key anchor.
func pathNodeID(key driver.NodeKey) semreg.ResourceID {
	fields := []string{key.Installation, string(key.Driver), string(key.Instance), string(key.Kind)}
	if key.Anchor.Key != "" {
		fields = append(fields, anchorTagKey, string(key.Anchor.Key))
	} else {
		fields = append(fields, anchorTagSlot, string(key.Anchor.Parent), key.Anchor.Slot)
	}
	digest := sha256.New()
	digest.Write([]byte(pathNodeDomain))
	for _, field := range fields {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(field)))
		digest.Write(length[:])
		digest.Write([]byte(field))
	}
	return semreg.ResourceID("resource:" + hex.EncodeToString(digest.Sum(nil))[:32])
}
