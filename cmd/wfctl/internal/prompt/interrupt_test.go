package prompt

import (
	"reflect"
	"strings"
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

	tableMultiSelect := &tableMultiSelectModel{}
	tableMultiSelect.Update(msg)
	if !tableMultiSelect.quit {
		t.Fatal("tableMultiSelectModel did not mark ctrl+c as quit")
	}
}

func TestParseIndexSelection(t *testing.T) {
	got, err := parseIndexSelection("1,3-4,3", 5)
	if err != nil {
		t.Fatalf("parseIndexSelection: %v", err)
	}
	want := []int{0, 2, 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("indexes = %v, want %v", got, want)
	}

	if _, err := parseIndexSelection("0", 5); err == nil {
		t.Fatal("parseIndexSelection accepted out-of-range selection")
	}
}

func TestTableMultiSelectModelViewShowsRows(t *testing.T) {
	m := newTableMultiSelectModel("Pick secrets",
		[]TableColumn{
			{Title: "Secret", Width: 24},
			{Title: "github:repo", Width: 12},
		},
		[]TableItem{
			{Cells: []string{"DIGITALOCEAN_TOKEN", "set"}, Preselected: true},
			{Cells: []string{"HOVER_PASSWORD", "unset"}},
		},
	)
	view := m.View().Content
	for _, want := range []string{"Pick secrets", "Secret", "github:repo", "DIGITALOCEAN_TOKEN", "HOVER_PASSWORD", "[x]", "[ ]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}
