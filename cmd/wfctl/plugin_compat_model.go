package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

var errInvalidRegistrySHA256 = errors.New("invalid sha256")

const (
	PluginCompatibilityModeTypedIaC = "typed-iac"

	PluginCompatibilityStatusPass = "pass"
	PluginCompatibilityStatusFail = "fail"

	CompatibilityTrustFirstParty CompatibilityTrustMode = "first_party"
	CompatibilityTrustAdvisory   CompatibilityTrustMode = "advisory"
)

var strictSemverRe = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)

// CompatibilityTrustMode controls whether compatibility evidence can affect
// install/update/lock decisions for a registry source.
type CompatibilityTrustMode string

func (m *CompatibilityTrustMode) UnmarshalYAML(unmarshal func(any) error) error {
	var raw string
	if err := unmarshal(&raw); err != nil {
		return err
	}
	trust, err := ParseCompatibilityTrustMode(raw)
	if err != nil {
		return err
	}
	*m = trust
	return nil
}

func ParseCompatibilityTrustMode(raw string) (CompatibilityTrustMode, error) {
	switch CompatibilityTrustMode(raw) {
	case "", CompatibilityTrustAdvisory:
		return CompatibilityTrustAdvisory, nil
	case CompatibilityTrustFirstParty:
		return CompatibilityTrustFirstParty, nil
	case "signed":
		return "", fmt.Errorf("compatibility evidence trust mode %q requires a signature ADR before it can be used", raw)
	default:
		return "", fmt.Errorf("unknown compatibility evidence trust mode %q", raw)
	}
}

type RegistryCompatibilityEvidenceConfig struct {
	Trust CompatibilityTrustMode `yaml:"trust,omitempty" json:"trust,omitempty"`
}

type CompatibilityEvidencePolicy struct {
	FirstParty         string `json:"firstParty,omitempty"`
	Community          string `json:"community,omitempty"`
	RequiredFromEngine string `json:"requiredFromEngine,omitempty"`
	LatestEngine       string `json:"latestEngine,omitempty"`
	Stale              bool   `json:"stale,omitempty"`
}

type PluginVersionIndex struct {
	Plugin         string                      `json:"plugin"`
	GeneratedAt    string                      `json:"generatedAt,omitempty"`
	EvidencePolicy CompatibilityEvidencePolicy `json:"evidencePolicy,omitempty"`
	Versions       []PluginVersionRecord       `json:"versions"`
}

type PluginVersionRecord struct {
	Version          string                        `json:"version"`
	MinEngineVersion string                        `json:"minEngineVersion,omitempty"`
	Downloads        []PluginDownload              `json:"downloads,omitempty"`
	Compatibility    []PluginCompatibilityEvidence `json:"compatibility,omitempty"`
}

type PluginCompatibilityRange struct {
	Min        string `json:"min"`
	Max        string `json:"max"`
	Derivation string `json:"derivation"`
}

type PluginCompatibilityEvidence struct {
	Plugin                string                    `json:"plugin,omitempty"`
	Version               string                    `json:"version,omitempty"`
	EngineVersion         string                    `json:"engineVersion,omitempty"`
	WfctlVersion          string                    `json:"wfctlVersion,omitempty"`
	Mode                  string                    `json:"mode,omitempty"`
	Status                string                    `json:"status,omitempty"`
	EvidenceDigest        string                    `json:"evidenceDigest,omitempty"`
	OS                    string                    `json:"os,omitempty"`
	Arch                  string                    `json:"arch,omitempty"`
	ArchiveSHA256         string                    `json:"archiveSHA256,omitempty"`
	BinarySHA256          string                    `json:"binarySHA256,omitempty"`
	PluginManifestSHA256  string                    `json:"pluginManifestSHA256,omitempty"`
	CompatibleEngineRange *PluginCompatibilityRange `json:"compatibleEngineRange,omitempty"`
	Repository            string                    `json:"repository,omitempty"`
	Ref                   string                    `json:"ref,omitempty"`
	Commit                string                    `json:"commit,omitempty"`
	WorkflowRunURL        string                    `json:"workflowRunURL,omitempty"`
	GeneratedBy           string                    `json:"generatedBy,omitempty"`
	StdoutTail            string                    `json:"stdoutTail,omitempty"`
	StderrTail            string                    `json:"stderrTail,omitempty"`
	FailureReason         string                    `json:"failureReason,omitempty"`
}

func NormalizePluginVersionIndex(index *PluginVersionIndex, defaultPlugin string) (*PluginVersionIndex, error) {
	if index == nil {
		return nil, fmt.Errorf("compatibility index is required")
	}
	out := *index
	if strings.TrimSpace(out.Plugin) == "" {
		out.Plugin = defaultPlugin
	}
	if strings.TrimSpace(out.Plugin) == "" {
		return nil, fmt.Errorf("compatibility index plugin is required")
	}
	out.Versions = slices.Clone(index.Versions)
	for i := range out.Versions {
		version, err := CanonicalPluginVersion(out.Versions[i].Version)
		if err != nil {
			return nil, fmt.Errorf("compatibility index %s version: %w", out.Plugin, err)
		}
		out.Versions[i].Version = version
		if out.Versions[i].MinEngineVersion != "" {
			minEngine, err := CanonicalEngineVersion(out.Versions[i].MinEngineVersion)
			if err != nil {
				return nil, fmt.Errorf("compatibility index %s minEngineVersion: %w", version, err)
			}
			out.Versions[i].MinEngineVersion = minEngine
		}
		out.Versions[i].Downloads = slices.Clone(index.Versions[i].Downloads)
		for j := range out.Versions[i].Downloads {
			if out.Versions[i].Downloads[j].SHA256 == "" {
				continue
			}
			sha, err := NormalizeSHA256Hex(out.Versions[i].Downloads[j].SHA256)
			if err != nil {
				return nil, fmt.Errorf("%w: compatibility index %s download %s/%s sha256: %w", errInvalidRegistrySHA256, version, out.Versions[i].Downloads[j].OS, out.Versions[i].Downloads[j].Arch, err)
			}
			out.Versions[i].Downloads[j].SHA256 = sha
		}
		out.Versions[i].Compatibility = slices.Clone(index.Versions[i].Compatibility)
		for j := range out.Versions[i].Compatibility {
			ev, err := ValidateCompatibilityEvidence(out.Versions[i].Compatibility[j])
			if err != nil {
				return nil, fmt.Errorf("compatibility index %s evidence[%d]: %w", version, j, err)
			}
			if ev.Plugin != out.Plugin && normalizePluginName(ev.Plugin) != normalizePluginName(out.Plugin) {
				return nil, fmt.Errorf("compatibility index evidence plugin %q does not match %q", ev.Plugin, out.Plugin)
			}
			if ev.Version != version {
				return nil, fmt.Errorf("compatibility index evidence version %q does not match %q", ev.Version, version)
			}
			out.Versions[i].Compatibility[j] = ev
		}
	}
	sortCompatibilityIndex(&out)
	return &out, nil
}

func CanonicalPluginVersion(version string) (string, error) {
	return canonicalStrictSemver(version, "plugin version")
}

func CanonicalEngineVersion(version string) (string, error) {
	return canonicalStrictSemver(version, "engine version")
}

func canonicalStrictSemver(version, label string) (string, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return "", fmt.Errorf("%s is required", label)
	}
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	if !strictSemverRe.MatchString(version) || !semver.IsValid(version) {
		return "", fmt.Errorf("%s %q must be semver MAJOR.MINOR.PATCH with optional leading v", label, strings.TrimPrefix(version, "v"))
	}
	return version, nil
}

func CanonicalEvidenceEngineVersion(version string) (string, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return "", fmt.Errorf("engine version is required")
	}
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	if (!strictSemverRe.MatchString(version) && !module.IsPseudoVersion(version)) || !semver.IsValid(version) {
		return "", fmt.Errorf("engine version %q must be semver MAJOR.MINOR.PATCH or a Go pseudo-version, with optional leading v", strings.TrimPrefix(version, "v"))
	}
	return version, nil
}

func NormalizeSHA256Hex(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) != sha256.Size*2 {
		return "", fmt.Errorf("sha256 must be %d hex characters", sha256.Size*2)
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("sha256 must be hex: %w", err)
	}
	return hex.EncodeToString(decoded), nil
}

func ValidateCompatibilityEvidence(ev PluginCompatibilityEvidence) (PluginCompatibilityEvidence, error) {
	var err error
	if ev.Plugin == "" {
		return ev, fmt.Errorf("plugin is required")
	}
	if ev.Version, err = CanonicalPluginVersion(ev.Version); err != nil {
		return ev, err
	}
	if ev.EngineVersion, err = CanonicalEvidenceEngineVersion(ev.EngineVersion); err != nil {
		return ev, err
	}
	if ev.WfctlVersion != "" {
		ev.WfctlVersion = strings.TrimSpace(ev.WfctlVersion)
		if canonical, err := CanonicalEvidenceEngineVersion(ev.WfctlVersion); err == nil {
			ev.WfctlVersion = canonical
		}
	}
	if ev.Mode != PluginCompatibilityModeTypedIaC {
		return ev, fmt.Errorf("unsupported compatibility mode %q", ev.Mode)
	}
	if ev.Status != PluginCompatibilityStatusPass && ev.Status != PluginCompatibilityStatusFail {
		return ev, fmt.Errorf("unsupported compatibility status %q", ev.Status)
	}
	if ev.OS == "" || ev.Arch == "" {
		return ev, fmt.Errorf("os and arch are required")
	}
	if ev.ArchiveSHA256 != "" {
		if ev.ArchiveSHA256, err = NormalizeSHA256Hex(ev.ArchiveSHA256); err != nil {
			return ev, fmt.Errorf("archiveSHA256: %w", err)
		}
	}
	if ev.BinarySHA256 != "" {
		if ev.BinarySHA256, err = NormalizeSHA256Hex(ev.BinarySHA256); err != nil {
			return ev, fmt.Errorf("binarySHA256: %w", err)
		}
	}
	if ev.PluginManifestSHA256 != "" {
		if ev.PluginManifestSHA256, err = NormalizeSHA256Hex(ev.PluginManifestSHA256); err != nil {
			return ev, fmt.Errorf("pluginManifestSHA256: %w", err)
		}
	}
	if ev.CompatibleEngineRange != nil {
		if ev.CompatibleEngineRange.Min, err = CanonicalEngineVersion(ev.CompatibleEngineRange.Min); err != nil {
			return ev, fmt.Errorf("compatibleEngineRange.min: %w", err)
		}
		if ev.CompatibleEngineRange.Max, err = CanonicalEngineVersion(ev.CompatibleEngineRange.Max); err != nil {
			return ev, fmt.Errorf("compatibleEngineRange.max: %w", err)
		}
	}
	ev.EvidenceDigest, err = ComputeEvidenceDigest(ev)
	if err != nil {
		return ev, err
	}
	return ev, nil
}

func ComputeEvidenceDigest(ev PluginCompatibilityEvidence) (string, error) {
	data, err := canonicalJSONWithoutEvidenceDigest(ev)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func canonicalJSONWithoutEvidenceDigest(ev PluginCompatibilityEvidence) ([]byte, error) {
	data, err := json.Marshal(ev)
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	if m, ok := value.(map[string]any); ok {
		delete(m, "evidenceDigest")
	}
	var buf bytes.Buffer
	if err := writeCanonicalJSON(&buf, value); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeCanonicalJSON(buf *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if v {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case float64, string:
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		buf.Write(data)
	case []any:
		buf.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonicalJSON(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		buf.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			keyData, err := json.Marshal(key)
			if err != nil {
				return err
			}
			buf.Write(keyData)
			buf.WriteByte(':')
			if err := writeCanonicalJSON(buf, v[key]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("unsupported JSON value %T", value)
	}
	return nil
}
