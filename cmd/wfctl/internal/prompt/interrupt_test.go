package prompt

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestPromptModelsMarkCtrlCAsInterrupted(t *testing.T) {
	msg := tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl})

	input := &inputModel{}
	input.Update(msg)
	if !input.quit {
		t.Fatal("inputModel did not mark ctrl+c as quit")
	}

	confirm := &confirmModel{}
	confirm.Update(msg)
	if !confirm.quit {
		t.Fatal("confirmModel did not mark ctrl+c as quit")
	}

	selectModel := &selectModel{}
	selectModel.Update(msg)
	if !selectModel.quit {
		t.Fatal("selectModel did not mark ctrl+c as quit")
	}

	multiSelect := &multiSelectModel{}
	multiSelect.Update(msg)
	if !multiSelect.quit {
		t.Fatal("multiSelectModel did not mark ctrl+c as quit")
	}
}
