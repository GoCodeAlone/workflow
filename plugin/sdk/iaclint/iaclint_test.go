package iaclint_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/plugin/sdk/iaclint"
)

func TestValidationKind_String(t *testing.T) {
	cases := map[iaclint.ValidationKind]string{
		iaclint.KindTCPPort:          "TCPPort",
		iaclint.KindNonNegativeInt:   "NonNegativeInt",
		iaclint.KindNonEmptyString:   "NonEmptyString",
		iaclint.KindStringEnum:       "StringEnum",
		iaclint.KindIntegerOnlyFloat: "IntegerOnlyFloat",
	}
	for kind, want := range cases {
		if got := kind.String(); got != want {
			t.Errorf("kind %d: got %q, want %q", kind, got, want)
		}
	}
}
