package interfaces

import "testing"

// TestActionStatus_Phase23_ValuesMatchProtoTags asserts ActionStatus iota
// values match plugin/external/proto/iac.proto ActionStatus enum tag numbers.
// Per workflow#698 Phase 2.3.
func TestActionStatus_Phase23_ValuesMatchProtoTags(t *testing.T) {
	cases := []struct {
		name    string
		status  ActionStatus
		wantInt int
	}{
		{"Unspecified=0", ActionStatusUnspecified, 0},
		{"Success=1", ActionStatusSuccess, 1},
		{"Error=2", ActionStatusError, 2},
		{"DeleteFailed=3", ActionStatusDeleteFailed, 3},
		{"Compensated=4", ActionStatusCompensated, 4},
		{"CompensationFailed=5", ActionStatusCompensationFailed, 5},
		{"Skipped=6", ActionStatusSkipped, 6},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if int(c.status) != c.wantInt {
				t.Errorf("%s: int(%d) != want %d", c.name, c.status, c.wantInt)
			}
		})
	}
}
