package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ValidateNetworking checks the networking: section for correctness.
// It validates ingress references against services and their exposed ports.
func ValidateNetworking(networking *NetworkingConfig, services map[string]*ServiceConfig) error {
	if networking == nil {
		return nil
	}

	var errs []error

	for i, ing := range networking.Ingress {
		// Validate the service reference.
		if ing.Service != "" && len(services) > 0 {
			svc, ok := services[ing.Service]
			if !ok {
				errs = append(errs, fmt.Errorf("networking.ingress[%d]: service=%q does not reference a known service", i, ing.Service))
			} else if svc != nil && len(svc.Expose) > 0 {
				// Check that the port is actually exposed by the service.
				portExposed := false
				for _, exp := range svc.Expose {
					if exp.Port == ing.Port {
						portExposed = true
						break
					}
				}
				if !portExposed {
					errs = append(errs, fmt.Errorf("networking.ingress[%d]: service=%q does not expose port %d (exposed: %v)", i, ing.Service, ing.Port, exposedPorts(svc)))
				}
			}
		}

		if ing.Port <= 0 || ing.Port > 65535 {
			errs = append(errs, fmt.Errorf("networking.ingress[%d]: port %d is out of range", i, ing.Port))
		}
		if ing.ExternalPort != 0 && (ing.ExternalPort <= 0 || ing.ExternalPort > 65535) {
			errs = append(errs, fmt.Errorf("networking.ingress[%d]: externalPort %d is out of range", i, ing.ExternalPort))
		}

		// Validate TLS provider if specified.
		if ing.TLS != nil && ing.TLS.Provider != "" {
			validProviders := []string{"letsencrypt", "manual", "acm", "cloudflare"}
			if !slices.Contains(validProviders, strings.ToLower(ing.TLS.Provider)) {
				errs = append(errs, fmt.Errorf("networking.ingress[%d].tls.provider=%q is not valid (valid: %s)", i, ing.TLS.Provider, strings.Join(validProviders, ", ")))
			}
		}
	}

	for i, pol := range networking.Policies {
		if pol.From == "" {
			errs = append(errs, fmt.Errorf("networking.policies[%d]: from is required", i))
		}
		if len(pol.To) == 0 {
			errs = append(errs, fmt.Errorf("networking.policies[%d]: to must not be empty", i))
		}
	}

	return errors.Join(errs...)
}

// ValidateSecurity checks the security: section.
func ValidateSecurity(security *SecurityConfig) error {
	if security == nil {
		return nil
	}
	var errs []error

	if security.TLS != nil && security.TLS.Provider != "" {
		validProviders := []string{"letsencrypt", "manual", "acm", "cloudflare"}
		if !slices.Contains(validProviders, strings.ToLower(security.TLS.Provider)) {
			errs = append(errs, fmt.Errorf("security.tls.provider=%q is not valid (valid: %s)", security.TLS.Provider, strings.Join(validProviders, ", ")))
		}
	}

	return errors.Join(errs...)
}

// CrossValidate performs cross-section warnings between services, networking, and mesh.
// It returns warnings (non-fatal) as a string slice.
func CrossValidate(cfg *WorkflowConfig) []string {
	var warnings []string

	// Warn if a service exposes a port that has no ingress route.
	if len(cfg.Services) > 0 && cfg.Networking != nil {
		ingressPorts := make(map[string]map[int]bool)
		for _, ing := range cfg.Networking.Ingress {
			if ing.Service == "" {
				continue
			}
			if ingressPorts[ing.Service] == nil {
				ingressPorts[ing.Service] = make(map[int]bool)
			}
			ingressPorts[ing.Service][ing.Port] = true
		}
		for svcName, svc := range cfg.Services {
			if svc == nil {
				continue
			}
			for _, exp := range svc.Expose {
				if !ingressPorts[svcName][exp.Port] {
					warnings = append(warnings, fmt.Sprintf("service %q exposes port %d but no networking.ingress routes to it", svcName, exp.Port))
				}
			}
		}
	}

	return warnings
}

func exposedPorts(svc *ServiceConfig) []int {
	ports := make([]int, 0, len(svc.Expose))
	for _, e := range svc.Expose {
		ports = append(ports, e.Port)
	}
	return ports
}
