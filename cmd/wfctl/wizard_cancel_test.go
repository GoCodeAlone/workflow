package main

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestWizardCtrlCCancels(t *testing.T) {
	m := newWizardModel()
	got, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	if cmd == nil {
		t.Fatal("ctrl+c did not request quit")
	}
	wm := got.(wizardModel)
	if !wm.cancelled {
		t.Fatal("ctrl+c did not mark wizard as cancelled")
	}
}

func TestWizardEscAtFirstScreenCancels(t *testing.T) {
	m := newWizardModel()
	got, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if cmd == nil {
		t.Fatal("esc on first screen did not request quit")
	}
	wm := got.(wizardModel)
	if !wm.cancelled {
		t.Fatal("esc on first screen did not mark wizard as cancelled")
	}
}
