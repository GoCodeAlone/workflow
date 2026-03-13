package main

import (
	"testing"
)

func TestCheckTrailingFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "flags before positional arg",
			args:    []string{"-author", "jon", "myplugin"},
			wantErr: false,
		},
		{
			name:    "flags after positional arg",
			args:    []string{"myplugin", "-author", "jon"},
			wantErr: true,
		},
		{
			name:    "all flags no positional",
			args:    []string{"-author", "jon", "-version", "1.0.0"},
			wantErr: false,
		},
		{
			name:    "no flags",
			args:    []string{"myplugin"},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkTrailingFlags(tc.args)
			if (err != nil) != tc.wantErr {
				t.Errorf("checkTrailingFlags(%v) error = %v, wantErr %v", tc.args, err, tc.wantErr)
			}
		})
	}
}
