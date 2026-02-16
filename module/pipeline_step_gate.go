package module

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/CrisisTextLine/modular"
)

// GateStep implements an approval gate within a pipeline. It supports
// manual, automated, and scheduled gate types.
type GateStep struct {
	name                  string
	gateType              string // "manual", "automated", "scheduled"
	approvers             []string
	timeout               time.Duration
	autoApproveConditions []string
	scheduledWindow       *ScheduledWindow
}

// ScheduledWindow defines a time window during which a scheduled gate passes.
type ScheduledWindow struct {
	Weekdays  []time.Weekday
	StartHour int
	EndHour   int
}

// NewGateStepFactory returns a StepFactory that creates GateStep instances.
func NewGateStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		gateType, _ := config["type"].(string)
		if gateType == "" {
			return nil, fmt.Errorf("gate step %q: 'type' is required", name)
		}
		switch gateType {
		case "manual", "automated", "scheduled":
			// valid
		default:
			return nil, fmt.Errorf("gate step %q: invalid type %q (expected manual, automated, or scheduled)", name, gateType)
		}

		timeout := 24 * time.Hour
		if ts, ok := config["timeout"].(string); ok && ts != "" {
			d, err := time.ParseDuration(ts)
			if err != nil {
				return nil, fmt.Errorf("gate step %q: invalid timeout %q: %w", name, ts, err)
			}
			timeout = d
		}

		var approvers []string
		if rawApprovers, ok := config["approvers"].([]any); ok {
			for i, a := range rawApprovers {
				s, ok := a.(string)
				if !ok {
					return nil, fmt.Errorf("gate step %q: approvers[%d] must be a string", name, i)
				}
				approvers = append(approvers, s)
			}
		}

		var conditions []string
		if rawConds, ok := config["auto_approve_conditions"].([]any); ok {
			for i, c := range rawConds {
				s, ok := c.(string)
				if !ok {
					return nil, fmt.Errorf("gate step %q: auto_approve_conditions[%d] must be a string", name, i)
				}
				conditions = append(conditions, s)
			}
		}

		var window *ScheduledWindow
		if schedRaw, ok := config["schedule"].(map[string]any); ok {
			window = &ScheduledWindow{}
			if wdRaw, ok := schedRaw["weekdays"].([]any); ok {
				for _, wd := range wdRaw {
					if s, ok := wd.(string); ok {
						if d, err := parseWeekday(s); err == nil {
							window.Weekdays = append(window.Weekdays, d)
						}
					}
				}
			}
			if sh, ok := schedRaw["start_hour"].(int); ok {
				window.StartHour = sh
			}
			if eh, ok := schedRaw["end_hour"].(int); ok {
				window.EndHour = eh
			}
		}

		return &GateStep{
			name:                  name,
			gateType:              gateType,
			approvers:             approvers,
			timeout:               timeout,
			autoApproveConditions: conditions,
			scheduledWindow:       window,
		}, nil
	}
}

// Name returns the step name.
func (s *GateStep) Name() string { return s.name }

// Execute evaluates the gate based on its type and returns a gate result.
func (s *GateStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	switch s.gateType {
	case "automated":
		return s.executeAutomated(pc)
	case "manual":
		return s.executeManual()
	case "scheduled":
		return s.executeScheduled()
	default:
		return nil, fmt.Errorf("gate step %q: unsupported gate type %q", s.name, s.gateType)
	}
}

// executeAutomated evaluates conditions against the pipeline context.
func (s *GateStep) executeAutomated(pc *PipelineContext) (*StepResult, error) {
	for _, cond := range s.autoApproveConditions {
		if !evaluateCondition(cond, pc.Current) {
			return &StepResult{
				Output: map[string]any{
					"gate_result": map[string]any{
						"passed": false,
						"type":   "automated",
						"reason": fmt.Sprintf("condition not met: %s", cond),
					},
				},
			}, nil
		}
	}

	return &StepResult{
		Output: map[string]any{
			"gate_result": map[string]any{
				"passed": true,
				"type":   "automated",
				"reason": "all conditions met",
			},
		},
	}, nil
}

// executeManual returns a pending result indicating manual approval is needed.
func (s *GateStep) executeManual() (*StepResult, error) {
	return &StepResult{
		Output: map[string]any{
			"gate_result": map[string]any{
				"passed":            false,
				"type":              "manual",
				"reason":            "awaiting manual approval",
				"approval_required": true,
				"approvers":         s.approvers,
				"timeout":           s.timeout.String(),
			},
		},
	}, nil
}

// executeScheduled checks if the current time falls within the configured window.
func (s *GateStep) executeScheduled() (*StepResult, error) {
	now := time.Now()

	if s.scheduledWindow == nil {
		return &StepResult{
			Output: map[string]any{
				"gate_result": map[string]any{
					"passed": true,
					"type":   "scheduled",
					"reason": "no schedule window configured, passing by default",
				},
			},
		}, nil
	}

	inWindow := s.isInWindow(now)
	reason := "current time is within the scheduled window"
	if !inWindow {
		reason = fmt.Sprintf("current time %s is outside the scheduled window", now.Format(time.RFC3339))
	}

	return &StepResult{
		Output: map[string]any{
			"gate_result": map[string]any{
				"passed": inWindow,
				"type":   "scheduled",
				"reason": reason,
			},
		},
	}, nil
}

// isInWindow checks if the given time is within the scheduled window.
func (s *GateStep) isInWindow(t time.Time) bool {
	w := s.scheduledWindow

	// Check weekday if specified
	if len(w.Weekdays) > 0 {
		dayMatch := false
		for _, wd := range w.Weekdays {
			if t.Weekday() == wd {
				dayMatch = true
				break
			}
		}
		if !dayMatch {
			return false
		}
	}

	// Check hour range
	hour := t.Hour()
	if w.StartHour <= w.EndHour {
		return hour >= w.StartHour && hour < w.EndHour
	}
	// Wraps midnight (e.g., 22 to 6)
	return hour >= w.StartHour || hour < w.EndHour
}

// evaluateCondition performs simple dot-path == value matching against data.
// Format: "key.subkey == value" (supports "true", "false", string values).
func evaluateCondition(condition string, data map[string]any) bool {
	parts := strings.SplitN(condition, "==", 2)
	if len(parts) != 2 {
		return false
	}

	path := strings.TrimSpace(parts[0])
	expected := strings.TrimSpace(parts[1])

	val := resolveKeyPath(path, data)
	if val == nil {
		return false
	}

	actual := fmt.Sprintf("%v", val)
	return actual == expected
}

// resolveKeyPath traverses a dot-separated path in nested maps.
func resolveKeyPath(path string, data map[string]any) any {
	segments := strings.Split(path, ".")
	var current any = data

	for _, seg := range segments {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current, ok = m[seg]
		if !ok {
			return nil
		}
	}
	return current
}

// parseWeekday converts a weekday name to time.Weekday.
func parseWeekday(s string) (time.Weekday, error) {
	switch strings.ToLower(s) {
	case "sunday", "sun":
		return time.Sunday, nil
	case "monday", "mon":
		return time.Monday, nil
	case "tuesday", "tue":
		return time.Tuesday, nil
	case "wednesday", "wed":
		return time.Wednesday, nil
	case "thursday", "thu":
		return time.Thursday, nil
	case "friday", "fri":
		return time.Friday, nil
	case "saturday", "sat":
		return time.Saturday, nil
	default:
		return 0, fmt.Errorf("unknown weekday: %s", s)
	}
}
