package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/cmd/wfctl/internal/prompt"
)

func TestConfirmActionTreatsPromptCancelAsCleanAbort(t *testing.T) {
	var out bytes.Buffer
	ok, err := confirmAction("Proceed?", true, &out, func(string, bool) (bool, error) {
		return false, prompt.ErrCancelled
	})
	if err != nil {
		t.Fatalf("confirmAction returned error for prompt cancel: %v", err)
	}
	if ok {
		t.Fatal("confirmAction should not approve a cancelled prompt")
	}
	if got := out.String(); !strings.Contains(got, "Cancelled.") {
		t.Fatalf("cancel output = %q, want Cancelled.", got)
	}
}

func TestConfirmActionTreatsNonInteractiveAsCleanAbort(t *testing.T) {
	var out bytes.Buffer
	ok, err := confirmAction("Proceed?", false, &out, func(string, bool) (bool, error) {
		return false, prompt.ErrNotInteractive
	})
	if err != nil {
		t.Fatalf("confirmAction returned error for non-interactive prompt: %v", err)
	}
	if ok {
		t.Fatal("confirmAction should not approve a non-interactive prompt")
	}
	if got := out.String(); !strings.Contains(got, "Cancelled.") {
		t.Fatalf("cancel output = %q, want Cancelled.", got)
	}
}

func TestConfirmActionPropagatesUnexpectedPromptError(t *testing.T) {
	want := errors.New("terminal unavailable")
	var out bytes.Buffer
	ok, err := confirmAction("Proceed?", false, &out, func(string, bool) (bool, error) {
		return false, want
	})
	if ok {
		t.Fatal("confirmAction should not approve on prompt error")
	}
	if !errors.Is(err, want) {
		t.Fatalf("confirmAction error = %v, want %v", err, want)
	}
	if out.Len() != 0 {
		t.Fatalf("unexpected output on prompt error: %q", out.String())
	}
}

func TestConfirmActionTreatsNoAnswerAsCleanAbort(t *testing.T) {
	var out bytes.Buffer
	ok, err := confirmAction("Proceed?", false, &out, func(string, bool) (bool, error) {
		return false, nil
	})
	if err != nil {
		t.Fatalf("confirmAction returned error for no answer: %v", err)
	}
	if ok {
		t.Fatal("confirmAction should not approve a no answer")
	}
	if got := out.String(); !strings.Contains(got, "Cancelled.") {
		t.Fatalf("cancel output = %q, want Cancelled.", got)
	}
}

func TestConfirmActionReturnsPromptAnswer(t *testing.T) {
	var out bytes.Buffer
	ok, err := confirmAction("Proceed?", true, &out, func(question string, def bool) (bool, error) {
		if question != "Proceed?" || !def {
			t.Fatalf("question=%q def=%v", question, def)
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("confirmAction: %v", err)
	}
	if !ok {
		t.Fatal("confirmAction should return true when prompt approves")
	}
	if out.Len() != 0 {
		t.Fatalf("unexpected output on approval: %q", out.String())
	}
}
