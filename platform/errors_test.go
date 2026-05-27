package platform_test

import (
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/platform"
)

func TestResourceNotFoundError_IsErrResourceNotFound(t *testing.T) {
	structErr := &platform.ResourceNotFoundError{Name: "alpha", Provider: "stub"}
	if !errors.Is(structErr, interfaces.ErrResourceNotFound) {
		t.Errorf("errors.Is(*ResourceNotFoundError, ErrResourceNotFound) = false; want true after Is method added")
	}
	if errors.Is(structErr, interfaces.ErrImageNotInRegistry) {
		t.Errorf("errors.Is(*ResourceNotFoundError, ErrImageNotInRegistry) = true; want false (different sentinel)")
	}
}
