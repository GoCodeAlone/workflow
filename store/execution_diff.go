package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Execution diff types
// ---------------------------------------------------------------------------

// ExecutionDiff compares two executions step by step.
type ExecutionDiff struct {
	ExecutionA uuid.UUID   `json:"execution_a"`
	ExecutionB uuid.UUID   `json:"execution_b"`
	StepDiffs  []StepDiff  `json:"step_diffs"`
	Summary    DiffSummary `json:"summary"`
}

// StepDiff represents the difference between a step across two executions.
type StepDiff struct {
	StepName  string         `json:"step_name"`
	Status    string         `json:"status"` // "same", "different", "added", "removed"
	OutputA   map[string]any `json:"output_a,omitempty"`
	OutputB   map[string]any `json:"output_b,omitempty"`
	DurationA time.Duration  `json:"duration_a"`
	DurationB time.Duration  `json:"duration_b"`
	Changes   []FieldChange  `json:"changes,omitempty"`
}

// FieldChange represents a single field difference between two maps.
type FieldChange struct {
	Path   string `json:"path"`
	ValueA any    `json:"value_a"`
	ValueB any    `json:"value_b"`
}

// DiffSummary provides aggregate counts for an execution diff.
type DiffSummary struct {
	TotalSteps   int `json:"total_steps"`
	SameSteps    int `json:"same_steps"`
	DiffSteps    int `json:"different_steps"`
	AddedSteps   int `json:"added_steps"`
	RemovedSteps int `json:"removed_steps"`
}

// ---------------------------------------------------------------------------
// DiffCalculator
// ---------------------------------------------------------------------------

// DiffCalculator computes diffs between executions.
type DiffCalculator struct {
	eventStore EventStore
}

// NewDiffCalculator creates a new DiffCalculator using the given EventStore.
func NewDiffCalculator(eventStore EventStore) *DiffCalculator {
	return &DiffCalculator{eventStore: eventStore}
}

// stepInfo holds extracted step data for comparison.
type stepInfo struct {
	output      map[string]any
	startedAt   *time.Time
	completedAt *time.Time
}

// Compare computes a structured diff between two executions.
func (d *DiffCalculator) Compare(ctx context.Context, execA, execB uuid.UUID) (*ExecutionDiff, error) {
	eventsA, err := d.eventStore.GetEvents(ctx, execA)
	if err != nil {
		return nil, fmt.Errorf("get events for execution A: %w", err)
	}
	eventsB, err := d.eventStore.GetEvents(ctx, execB)
	if err != nil {
		return nil, fmt.Errorf("get events for execution B: %w", err)
	}

	if len(eventsA) == 0 {
		return nil, fmt.Errorf("execution A (%s): %w", execA, ErrNotFound)
	}
	if len(eventsB) == 0 {
		return nil, fmt.Errorf("execution B (%s): %w", execB, ErrNotFound)
	}

	stepsA := extractSteps(eventsA)
	stepsB := extractSteps(eventsB)

	// Collect all unique step names preserving order of first appearance.
	allSteps := mergeStepNames(stepsA, stepsB)

	diff := &ExecutionDiff{
		ExecutionA: execA,
		ExecutionB: execB,
	}

	for _, stepName := range allSteps {
		infoA, inA := stepsA[stepName]
		infoB, inB := stepsB[stepName]

		sd := StepDiff{StepName: stepName}

		switch {
		case inA && inB:
			// Both executions have this step.
			sd.OutputA = infoA.output
			sd.OutputB = infoB.output
			sd.DurationA = stepDuration(infoA)
			sd.DurationB = stepDuration(infoB)
			sd.Changes = DiffMaps(infoA.output, infoB.output)
			if len(sd.Changes) == 0 {
				sd.Status = "same"
				diff.Summary.SameSteps++
			} else {
				sd.Status = "different"
				diff.Summary.DiffSteps++
			}

		case inA && !inB:
			sd.Status = "removed"
			sd.OutputA = infoA.output
			sd.DurationA = stepDuration(infoA)
			diff.Summary.RemovedSteps++

		case !inA && inB:
			sd.Status = "added"
			sd.OutputB = infoB.output
			sd.DurationB = stepDuration(infoB)
			diff.Summary.AddedSteps++
		}

		diff.StepDiffs = append(diff.StepDiffs, sd)
	}

	diff.Summary.TotalSteps = len(allSteps)
	return diff, nil
}

// extractSteps groups events by step name and extracts output data and timing.
func extractSteps(events []ExecutionEvent) map[string]*stepInfo {
	steps := make(map[string]*stepInfo)

	for i := range events {
		ev := &events[i]
		var data map[string]any
		if len(ev.EventData) > 0 {
			_ = json.Unmarshal(ev.EventData, &data)
		}
		if data == nil {
			continue
		}

		stepName, _ := data["step_name"].(string)
		if stepName == "" {
			continue
		}

		if _, ok := steps[stepName]; !ok {
			steps[stepName] = &stepInfo{}
		}
		info := steps[stepName]

		switch ev.EventType {
		case EventStepStarted:
			t := ev.CreatedAt
			info.startedAt = &t

		case EventStepOutputRecorded:
			if outputRaw, ok := data["output"]; ok {
				if m, ok := outputRaw.(map[string]any); ok {
					info.output = m
				}
			}

		case EventStepCompleted, EventStepFailed:
			t := ev.CreatedAt
			info.completedAt = &t
		}
	}

	return steps
}

// mergeStepNames produces a deduplicated, order-preserving list of step names
// from two step maps. Steps from A appear first in their original order,
// followed by any steps only in B.
func mergeStepNames(a, b map[string]*stepInfo) []string {
	// Collect all keys and sort them for deterministic output.
	seen := make(map[string]bool)
	var names []string

	// Gather keys from both maps.
	for k := range a {
		if !seen[k] {
			seen[k] = true
			names = append(names, k)
		}
	}
	for k := range b {
		if !seen[k] {
			seen[k] = true
			names = append(names, k)
		}
	}

	sort.Strings(names)
	return names
}

// stepDuration computes the duration of a step from its timing information.
func stepDuration(info *stepInfo) time.Duration {
	if info == nil || info.startedAt == nil || info.completedAt == nil {
		return 0
	}
	return info.completedAt.Sub(*info.startedAt)
}

// ---------------------------------------------------------------------------
// DiffMaps — recursive field comparison
// ---------------------------------------------------------------------------

// DiffMaps recursively compares two maps and returns a list of field changes.
// Paths are dot-separated for nested keys.
func DiffMaps(a, b map[string]any) []FieldChange {
	var changes []FieldChange
	diffMapsRecursive("", a, b, &changes)

	// Sort changes by path for deterministic output.
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Path < changes[j].Path
	})

	return changes
}

func diffMapsRecursive(prefix string, a, b map[string]any, changes *[]FieldChange) {
	// Check all keys in a.
	for k, va := range a {
		path := joinPath(prefix, k)
		vb, inB := b[k]

		if !inB {
			// Key removed in b.
			*changes = append(*changes, FieldChange{
				Path:   path,
				ValueA: va,
				ValueB: nil,
			})
			continue
		}

		// Both have the key — compare values.
		compareValues(path, va, vb, changes)
	}

	// Check for keys in b that are not in a (added).
	for k, vb := range b {
		if _, inA := a[k]; !inA {
			path := joinPath(prefix, k)
			*changes = append(*changes, FieldChange{
				Path:   path,
				ValueA: nil,
				ValueB: vb,
			})
		}
	}
}

func compareValues(path string, va, vb any, changes *[]FieldChange) {
	// If both are maps, recurse.
	mapA, aIsMap := va.(map[string]any)
	mapB, bIsMap := vb.(map[string]any)
	if aIsMap && bIsMap {
		diffMapsRecursive(path, mapA, mapB, changes)
		return
	}

	// Compare using JSON serialization for reliable equality.
	jsonA, errA := json.Marshal(va)
	jsonB, errB := json.Marshal(vb)

	if errA != nil || errB != nil || !bytes.Equal(jsonA, jsonB) {
		*changes = append(*changes, FieldChange{
			Path:   path,
			ValueA: va,
			ValueB: vb,
		})
	}
}

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}
