package prompt

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestErrCancelledAliasesInterrupted(t *testing.T) {
	if !errors.Is(ErrInterrupted, ErrCancelled) {
		t.Fatalf("ErrInterrupted should alias ErrCancelled")
	}
	if errors.Is(ErrCancelled, ErrNotInteractive) {
		t.Fatalf("ErrCancelled must be distinct from ErrNotInteractive")
	}
}

func TestPromptModelsMarkCtrlCAsInterrupted(t *testing.T) {
	msg := tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl})

	input := &inputModel{}
	input.Update(msg)
	if !input.quit || !input.interrupted {
		t.Fatal("inputModel did not mark ctrl+c as quit")
	}

	confirm := &confirmModel{}
	confirm.Update(msg)
	if !confirm.quit || !confirm.interrupted {
		t.Fatal("confirmModel did not mark ctrl+c as quit")
	}

	selectModel := &selectModel{}
	selectModel.Update(msg)
	if !selectModel.quit || !selectModel.interrupted {
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

func TestPromptModelsMarkEscAsInterrupted(t *testing.T) {
	msg := tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape})

	input := &inputModel{}
	input.Update(msg)
	if !input.quit || !input.interrupted {
		t.Fatal("inputModel did not mark esc as quit")
	}

	confirm := &confirmModel{}
	confirm.Update(msg)
	if !confirm.quit || !confirm.interrupted {
		t.Fatal("confirmModel did not mark esc as quit")
	}

	selectModel := &selectModel{}
	selectModel.Update(msg)
	if !selectModel.quit || !selectModel.interrupted {
		t.Fatal("selectModel did not mark esc as quit")
	}

	multiSelect := &multiSelectModel{}
	multiSelect.Update(msg)
	if !multiSelect.quit || !multiSelect.interrupted {
		t.Fatal("multiSelectModel did not mark esc as quit")
	}

	tableMultiSelect := &tableMultiSelectModel{}
	tableMultiSelect.Update(msg)
	if !tableMultiSelect.quit || !tableMultiSelect.interrupted {
		t.Fatal("tableMultiSelectModel did not mark esc as quit")
	}
}

func TestMultiSelectModelSelectedIndexes(t *testing.T) {
	m := newMultiSelectModel("Pick", []Item{
		{Label: "set", Preselected: true},
		{Label: "unset"},
	})

	if got, want := m.selectedIndexes(), []int{0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selected indexes = %v, want %v", got, want)
	}

	m.Update(tea.KeyPressMsg(tea.Key{Text: "j", Code: 'j'}))
	m.Update(tea.KeyPressMsg(tea.Key{Text: " ", Code: ' '}))
	if got, want := m.selectedIndexes(), []int{0, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selected indexes after toggle = %v, want %v", got, want)
	}
}

func TestTableMultiSelectModelSelectedIndexes(t *testing.T) {
	m := newTableMultiSelectModel("Pick",
		[]TableColumn{{Title: "Secret", Width: 12}},
		[]TableItem{
			{Cells: []string{"A"}, Preselected: true},
			{Cells: []string{"B"}},
		},
	)

	if got, want := m.selectedIndexes(), []int{0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selected indexes = %v, want %v", got, want)
	}

	m.Update(tea.KeyPressMsg(tea.Key{Text: "j", Code: 'j'}))
	m.Update(tea.KeyPressMsg(tea.Key{Text: " ", Code: ' '}))
	if got, want := m.selectedIndexes(), []int{0, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selected indexes after toggle = %v, want %v", got, want)
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
	for _, notWant := range []string{"Choose numbers/ranges", "Set "} {
		if strings.Contains(view, notWant) {
			t.Fatalf("view contains stale prompt text %q:\n%s", notWant, view)
		}
	}
}
