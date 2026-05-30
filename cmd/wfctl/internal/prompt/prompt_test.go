package prompt_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/cmd/wfctl/internal/prompt"
)

// All tests run in a non-TTY environment (stdin is a pipe in test
// subprocess), so every constructor must return ErrNotInteractive
// immediately rather than hanging.

func TestSelect_NonTTY(t *testing.T) {
	_, err := prompt.Select("Pick one", []string{"a", "b"})
	if err != prompt.ErrNotInteractive {
		t.Fatalf("Select: got %v, want ErrNotInteractive", err)
	}
}

func TestMultiSelect_NonTTY(t *testing.T) {
	_, err := prompt.MultiSelect("Pick many", []prompt.Item{{Label: "a"}, {Label: "b"}})
	if err != prompt.ErrNotInteractive {
		t.Fatalf("MultiSelect: got %v, want ErrNotInteractive", err)
	}
}

func TestInput_NonTTY(t *testing.T) {
	_, err := prompt.Input("value", false)
	if err != prompt.ErrNotInteractive {
		t.Fatalf("Input: got %v, want ErrNotInteractive", err)
	}
}

func TestInputMasked_NonTTY(t *testing.T) {
	_, err := prompt.Input("password", true)
	if err != prompt.ErrNotInteractive {
		t.Fatalf("Input(masked): got %v, want ErrNotInteractive", err)
	}
}

func TestConfirm_NonTTY(t *testing.T) {
	_, err := prompt.Confirm("Are you sure?", true)
	if err != prompt.ErrNotInteractive {
		t.Fatalf("Confirm: got %v, want ErrNotInteractive", err)
	}
}

func TestSelectZeroValue(t *testing.T) {
	// When non-interactive the index is 0 (zero value).
	idx, err := prompt.Select("Pick", []string{"x"})
	if err != prompt.ErrNotInteractive {
		t.Fatalf("unexpected err: %v", err)
	}
	if idx != 0 {
		t.Errorf("idx = %d, want 0", idx)
	}
}

func TestMultiSelectZeroValue(t *testing.T) {
	indices, err := prompt.MultiSelect("Pick", []prompt.Item{{Label: "x"}})
	if err != prompt.ErrNotInteractive {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(indices) != 0 {
		t.Errorf("indices = %v, want []", indices)
	}
}

func TestInputZeroValue(t *testing.T) {
	s, err := prompt.Input("label", false)
	if err != prompt.ErrNotInteractive {
		t.Fatalf("unexpected err: %v", err)
	}
	if s != "" {
		t.Errorf("s = %q, want empty", s)
	}
}

func TestConfirmZeroValue(t *testing.T) {
	// Default value is irrelevant when ErrNotInteractive is returned.
	v, err := prompt.Confirm("Sure?", true)
	if err != prompt.ErrNotInteractive {
		t.Fatalf("unexpected err: %v", err)
	}
	if v {
		t.Errorf("v = true, want false (zero value)")
	}
}
