package interfaces_test

import (
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestMigrationRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     interfaces.MigrationRequest
		wantErr error
	}{
		{"missing DSN", interfaces.MigrationRequest{Source: interfaces.MigrationSource{Dir: "/m"}}, interfaces.ErrValidation},
		{"missing source", interfaces.MigrationRequest{DSN: "postgres://"}, interfaces.ErrValidation},
		{"ok", interfaces.MigrationRequest{DSN: "postgres://", Source: interfaces.MigrationSource{Dir: "/m"}}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}
