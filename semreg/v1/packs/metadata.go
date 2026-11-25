// Package packs composes the accepted built-in semantic pack validators.
package packs

import (
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/energymanagement"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/equipment"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/evse"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/infrastructure"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/pv"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/storage"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/thermal"
)

// NewMetadataRegistry returns immutable metadata for the accepted packs.
func NewMetadataRegistry() (*semreg.PackMetadataRegistry, error) {
	return semreg.NewPackMetadataRegistry(
		semreg.PackMetadataSource{Validator: thermal.New(), Metadata: thermal.Metadata()},
		semreg.PackMetadataSource{Validator: pv.New(), Metadata: pv.Metadata()},
		semreg.PackMetadataSource{Validator: storage.New(), Metadata: storage.Metadata()},
		semreg.PackMetadataSource{Validator: energymanagement.New(), Metadata: energymanagement.Metadata()},
		semreg.PackMetadataSource{Validator: equipment.New(), Metadata: equipment.Metadata()},
		semreg.PackMetadataSource{Validator: evse.New(), Metadata: evse.Metadata()},
		semreg.PackMetadataSource{Validator: infrastructure.New(), Metadata: infrastructure.Metadata()},
	)
}
