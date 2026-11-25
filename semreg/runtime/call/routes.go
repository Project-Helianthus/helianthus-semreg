package call

import (
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

type routeKind int

const (
	kindCurrent routeKind = iota
	kindWithdrawn
	kindStale
	kindUnknown
	kindNoContact
)

func matchingRoutes(cut driver.SemanticCut, req driver.OperationRequest, sel Selection, resource semreg.ResourceID) []driver.SettingRoute {
	var matched []driver.SettingRoute
	for _, route := range cut.SettingRoutes {
		if route.Operation != req.Operation || route.Route.CapabilityInstance != req.CapabilityInstanceID || route.Route.BindingID != req.BindingID {
			continue
		}
		if route.For.Key.FactID != sel.Operation.Argument.ID || !dimensionText(route.For.Key, sel.Dimension, string(resource)) {
			continue
		}
		matched = append(matched, route)
	}
	return matched
}

func dimensionText(key semreg.FactKey, id semreg.DefinitionID, resource string) bool {
	for _, dimension := range key.Dimensions {
		if dimension.ID == id && dimension.Value.Kind == semreg.ValueText && dimension.Value.Text != nil && *dimension.Value.Text == resource {
			return true
		}
	}
	return false
}

func chooseRoute(routes []driver.SettingRoute, snap semreg.Snapshot) (driver.SettingRoute, *routeRefusal) {
	if len(routes) == 0 {
		return driver.SettingRoute{}, failed(ReasonNoRoute)
	}
	var current []driver.SettingRoute
	for _, route := range routes {
		if classify(route, snap) == kindCurrent {
			current = append(current, route)
		}
	}
	if len(current) == 1 {
		return current[0], nil
	}
	if len(current) > 1 {
		return driver.SettingRoute{}, failed(ReasonAmbiguousRoute)
	}
	if len(routes) == 1 {
		switch classify(routes[0], snap) {
		case kindWithdrawn:
			return driver.SettingRoute{}, failed(ReasonWithdrawnRoute)
		case kindStale:
			return driver.SettingRoute{}, failed(ReasonStaleOwner)
		case kindUnknown:
			return driver.SettingRoute{}, failed(ReasonUnknownOwner)
		default:
			return driver.SettingRoute{}, noContact()
		}
	}
	return driver.SettingRoute{}, failed(ReasonAmbiguousRoute)
}

func classify(route driver.SettingRoute, snap semreg.Snapshot) routeKind {
	seen := false
	current := false
	retired := false
	for _, source := range snap.Sources {
		if source.SourceID != route.Route.SourceID {
			continue
		}
		seen = true
		if source.SourceEpochID != route.Route.SourceEpochID {
			continue
		}
		if source.State == semreg.SourceRetired {
			retired = true
		}
		if source.State == semreg.SourceCurrent {
			current = true
		}
	}
	if !seen {
		return kindUnknown
	}
	if retired {
		return kindWithdrawn
	}
	if !current {
		return kindStale
	}
	var binding *semreg.NativeBinding
	for i := range snap.Bindings {
		if snap.Bindings[i].BindingID == route.Route.BindingID {
			binding = &snap.Bindings[i]
		}
	}
	if binding == nil || binding.State == semreg.BindingFenced || binding.State != semreg.BindingCurrent {
		if binding != nil && binding.State == semreg.BindingRetired {
			return kindWithdrawn
		}
		return kindNoContact
	}
	if binding.SourceEpochID != route.Route.SourceEpochID || binding.DriverGeneration != route.Route.DriverGeneration || binding.SourceID != route.Route.SourceID {
		return kindNoContact
	}
	for _, service := range snap.Services {
		if service.InstanceID == route.Route.ServiceInstance && service.Availability == semreg.AvailabilityWithdrawn {
			return kindWithdrawn
		}
	}
	for _, capability := range snap.Capabilities {
		if capability.InstanceID == route.Route.CapabilityInstance && capability.Availability == semreg.AvailabilityWithdrawn {
			return kindWithdrawn
		}
	}
	for _, fence := range snap.Fences {
		if fence.SourceID == route.Route.SourceID && fence.SourceEpochID == route.Route.SourceEpochID && fence.DriverGeneration == route.Route.DriverGeneration {
			return kindNoContact
		}
	}
	return kindCurrent
}

func sameKernelRoute(admitted operation.Route, route driver.SettingRoute) bool {
	return admitted == route.Route
}

func guardRecheck(guard operation.GuardClaims, route operation.Route, snap semreg.Snapshot) *routeRefusal {
	if snap.Revisions.Capabilities != guard.ExpectedCapabilityRevision {
		return failed(ReasonGuardChanged)
	}
	var capability *semreg.CapabilityInstance
	for i := range snap.Capabilities {
		if snap.Capabilities[i].InstanceID == route.CapabilityInstance {
			capability = &snap.Capabilities[i]
		}
	}
	if capability == nil || capability.Revision != guard.ExpectedCapabilityInstanceRevision || capability.ServiceInstance != route.ServiceInstance || capability.BindingID != route.BindingID || capability.SourceEpochID != route.SourceEpochID || capability.DriverGeneration != route.DriverGeneration {
		return failed(ReasonGuardChanged)
	}
	var binding *semreg.NativeBinding
	for i := range snap.Bindings {
		if snap.Bindings[i].BindingID == route.BindingID {
			binding = &snap.Bindings[i]
		}
	}
	if binding == nil || binding.State != semreg.BindingCurrent {
		return noContact()
	}
	if binding.SourceID != route.SourceID || binding.SourceEpochID != route.SourceEpochID || binding.DriverGeneration != route.DriverGeneration {
		return failed(ReasonGuardChanged)
	}
	current := false
	for _, source := range snap.Sources {
		if source.SourceID == route.SourceID && source.SourceEpochID == route.SourceEpochID && source.State == semreg.SourceCurrent {
			current = true
		}
	}
	if !current {
		return noContact()
	}
	for _, service := range snap.Services {
		if service.InstanceID == route.ServiceInstance && (service.BindingID != route.BindingID || service.SourceEpochID != route.SourceEpochID || service.DriverGeneration != route.DriverGeneration || service.Availability == semreg.AvailabilityWithdrawn) {
			return failed(ReasonGuardChanged)
		}
	}
	for _, fence := range snap.Fences {
		if fence.SourceID == route.SourceID && fence.SourceEpochID == route.SourceEpochID && fence.DriverGeneration == route.DriverGeneration {
			return failed(ReasonGuardChanged)
		}
	}
	return nil
}
