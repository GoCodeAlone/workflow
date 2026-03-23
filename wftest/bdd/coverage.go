package bdd

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// PipelineCoverageEntry describes a .feature file reference to a pipeline.
type PipelineCoverageEntry struct {
	Pipeline    string
	FeatureFile string
	Line        int
	Via         string // "tag" or "route"
}

// CoverageReport summarises pipeline and scenario coverage from static analysis.
// PassingScenarios and PendingScenarios require a live test run; they are zero
// when returned by CalculateCoverage (static only).
type CoverageReport struct {
	TotalPipelines       int
	CoveredPipelines     []PipelineCoverageEntry
	UncoveredPipelines   []string
	TotalScenarios       int
	ImplementedScenarios int // scenarios that reference a known pipeline
	PassingScenarios     int // filled in by test runner, not static analysis
	PendingScenarios     int // filled in by test runner, not static analysis
	UndefinedScenarios   int // TotalScenarios - ImplementedScenarios
}

// pipelineTrigger holds the minimal trigger config we parse from app YAML.
type pipelineTrigger struct {
	Type   string            `yaml:"type"`
	Config map[string]string `yaml:"config"`
}

type pipelineEntry struct {
	Trigger pipelineTrigger `yaml:"trigger"`
}

type appConfig struct {
	Pipelines map[string]pipelineEntry `yaml:"pipelines"`
}

// httpRoute is used as a map key for matching HTTP trigger routes.
type httpRoute struct{ method, path string }

var (
	tagPipelineRe = regexp.MustCompile(`@pipeline:(\S+)`)
	httpStepRe    = regexp.MustCompile(`(?i)\bI\s+(GET|POST|PUT|DELETE|PATCH)\s+"([^"]+)"`)
)

// CalculateCoverage performs static analysis: parses configPath for pipeline
// definitions and walks featureDir for .feature files. Returns a CoverageReport
// showing which pipelines are exercised by BDD scenarios.
func CalculateCoverage(configPath, featureDir string) (*CoverageReport, error) {
	pipelines, err := parsePipelineConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("parse config %s: %w", configPath, err)
	}

	// Build a route index: (METHOD, path) → pipeline name for HTTP triggers.
	routeIndex := make(map[httpRoute]string, len(pipelines))
	for name, p := range pipelines {
		if strings.EqualFold(p.Trigger.Type, "http") {
			m := strings.ToUpper(p.Trigger.Config["method"])
			pa := p.Trigger.Config["path"]
			if m != "" && pa != "" {
				routeIndex[httpRoute{m, pa}] = name
			}
		}
	}

	// Scan all .feature files.
	covered := map[string]PipelineCoverageEntry{} // pipeline → first coverage entry
	totalScenarios := 0
	implementedScenarios := 0

	err = filepath.WalkDir(featureDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".feature") {
			return err
		}
		total, impl, c, ferr := scanFeatureFile(path, pipelines, routeIndex)
		if ferr != nil {
			return ferr
		}
		totalScenarios += total
		implementedScenarios += impl
		for name, entry := range c {
			if _, already := covered[name]; !already {
				covered[name] = entry
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan feature dir %s: %w", featureDir, err)
	}

	// Build the final report.
	report := &CoverageReport{
		TotalPipelines:       len(pipelines),
		TotalScenarios:       totalScenarios,
		ImplementedScenarios: implementedScenarios,
		UndefinedScenarios:   totalScenarios - implementedScenarios,
	}

	for _, entry := range covered {
		report.CoveredPipelines = append(report.CoveredPipelines, entry)
	}
	sort.Slice(report.CoveredPipelines, func(i, j int) bool {
		return report.CoveredPipelines[i].Pipeline < report.CoveredPipelines[j].Pipeline
	})

	for name := range pipelines {
		if _, ok := covered[name]; !ok {
			report.UncoveredPipelines = append(report.UncoveredPipelines, name)
		}
	}
	sort.Strings(report.UncoveredPipelines)

	return report, nil
}

// parsePipelineConfig reads configPath and returns pipeline entries keyed by name.
func parsePipelineConfig(configPath string) (map[string]pipelineEntry, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var app appConfig
	if err := yaml.Unmarshal(data, &app); err != nil {
		return nil, err
	}
	if app.Pipelines == nil {
		return map[string]pipelineEntry{}, nil
	}
	return app.Pipelines, nil
}

// scanFeatureFile parses a single .feature file and returns:
//   - total scenario count
//   - implemented scenario count (those referencing a known pipeline)
//   - a map of pipeline name → first coverage entry found in this file
func scanFeatureFile(
	path string,
	pipelines map[string]pipelineEntry,
	routeIndex map[httpRoute]string,
) (total, implemented int, covered map[string]PipelineCoverageEntry, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, nil, err
	}
	defer f.Close()

	covered = make(map[string]PipelineCoverageEntry)

	// Tags accumulate before a Scenario: line, then are consumed.
	var pendingTags []string
	lineNum := 0
	inScenario := false
	scenarioLine := 0
	var scenarioTags []string
	scenarioHasPipeline := false

	flushScenario := func() {
		if !inScenario {
			return
		}
		total++
		if scenarioHasPipeline {
			implemented++
		}
		inScenario = false
		scenarioTags = nil
		scenarioHasPipeline = false
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Collect @pipeline:name tags (and other tags).
		if strings.HasPrefix(line, "@") {
			if !inScenario {
				for _, m := range tagPipelineRe.FindAllStringSubmatch(line, -1) {
					pendingTags = append(pendingTags, m[1])
				}
			}
			continue
		}

		// New scenario: close previous and open new.
		if strings.HasPrefix(line, "Scenario:") || strings.HasPrefix(line, "Scenario Outline:") {
			flushScenario()
			inScenario = true
			scenarioLine = lineNum
			scenarioTags = pendingTags
			pendingTags = nil

			// Explicit @pipeline:name tags.
			for _, tag := range scenarioTags {
				if _, ok := pipelines[tag]; ok {
					scenarioHasPipeline = true
					if _, already := covered[tag]; !already {
						covered[tag] = PipelineCoverageEntry{
							Pipeline:    tag,
							FeatureFile: path,
							Line:        scenarioLine,
							Via:         "tag",
						}
					}
				}
			}
			continue
		}

		// Feature: line resets pending tags (tags on Feature: apply to the feature, not scenarios).
		if strings.HasPrefix(line, "Feature:") {
			flushScenario()
			pendingTags = nil
			continue
		}

		// Inside a scenario: scan for HTTP step patterns.
		if inScenario {
			if m := httpStepRe.FindStringSubmatch(line); m != nil {
				method := strings.ToUpper(m[1])
				reqPath := stripQueryString(m[2])
				key := struct{ method, path string }{method, reqPath}
				if name, ok := routeIndex[key]; ok {
					scenarioHasPipeline = true
					if _, already := covered[name]; !already {
						covered[name] = PipelineCoverageEntry{
							Pipeline:    name,
							FeatureFile: path,
							Line:        lineNum,
							Via:         "route",
						}
					}
				}
			}
		}
	}
	flushScenario()
	return total, implemented, covered, scanner.Err()
}

func stripQueryString(p string) string {
	if i := strings.IndexByte(p, '?'); i >= 0 {
		return p[:i]
	}
	return p
}
