package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ValidateServices checks the services: section for correctness.
func ValidateServices(services map[string]*ServiceConfig) error {
	var errs []error
	for name, svc := range services {
		if svc == nil {
			errs = append(errs, fmt.Errorf("services[%s]: service config is nil", name))
			continue
		}
		if svc.Scaling != nil {
			if svc.Scaling.Min > svc.Scaling.Max && svc.Scaling.Max > 0 {
				errs = append(errs, fmt.Errorf("services[%s].scaling: min (%d) exceeds max (%d)", name, svc.Scaling.Min, svc.Scaling.Max))
			}
			if svc.Scaling.Replicas < 0 {
				errs = append(errs, fmt.Errorf("services[%s].scaling.replicas must not be negative", name))
			}
		}
		for i, exp := range svc.Expose {
			if exp.Port <= 0 || exp.Port > 65535 {
				errs = append(errs, fmt.Errorf("services[%s].expose[%d]: port %d is out of range", name, i, exp.Port))
			}
		}
	}
	return errors.Join(errs...)
}

// ValidateMeshRoutes checks mesh.routes references against known service names.
func ValidateMeshRoutes(mesh *MeshConfig, services map[string]*ServiceConfig) []string {
	if mesh == nil {
		return nil
	}
	validVia := []string{"nats", "http", "grpc"}
	var warnings []string
	for i, route := range mesh.Routes {
		if route.From == "" {
			warnings = append(warnings, fmt.Sprintf("mesh.routes[%d]: from is required", i))
		} else if len(services) > 0 {
			if _, ok := services[route.From]; !ok {
				warnings = append(warnings, fmt.Sprintf("mesh.routes[%d]: from=%q does not reference a known service", i, route.From))
			}
		}
		if route.To == "" {
			warnings = append(warnings, fmt.Sprintf("mesh.routes[%d]: to is required", i))
		} else if len(services) > 0 {
			if _, ok := services[route.To]; !ok {
				warnings = append(warnings, fmt.Sprintf("mesh.routes[%d]: to=%q does not reference a known service", i, route.To))
			}
		}
		if route.Via != "" && !slices.Contains(validVia, strings.ToLower(route.Via)) {
			warnings = append(warnings, fmt.Sprintf("mesh.routes[%d]: via=%q is not a recognised transport (valid: %s)", i, route.Via, strings.Join(validVia, ", ")))
		}
	}
	return warnings
}
