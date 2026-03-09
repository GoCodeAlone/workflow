package module

import (
	"context"
	"crypto/md5" //nolint:gosec // MD5 used for checksums, not security
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"

	"github.com/GoCodeAlone/modular"
)

// HashStep computes a cryptographic hash of a template-resolved input string.
type HashStep struct {
	name      string
	algorithm string
	input     string
	app       modular.Application
	tmpl      *TemplateEngine
}

// NewHashStepFactory returns a StepFactory that creates HashStep instances.
func NewHashStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		algorithm, _ := config["algorithm"].(string)
		if algorithm == "" {
			algorithm = "sha256"
		}

		switch algorithm {
		case "md5", "sha256", "sha512":
			// valid
		default:
			return nil, fmt.Errorf("hash step %q: unsupported algorithm %q (expected md5, sha256, or sha512)", name, algorithm)
		}

		input, _ := config["input"].(string)
		if input == "" {
			return nil, fmt.Errorf("hash step %q: 'input' is required", name)
		}

		return &HashStep{
			name:      name,
			algorithm: algorithm,
			input:     input,
			app:       app,
			tmpl:      NewTemplateEngine(),
		}, nil
	}
}

// Name returns the step name.
func (s *HashStep) Name() string { return s.name }

// Execute resolves the input template, computes the hash, and returns the hex digest.
func (s *HashStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	resolved, err := s.tmpl.Resolve(s.input, pc)
	if err != nil {
		return nil, fmt.Errorf("hash step %q: failed to resolve input: %w", s.name, err)
	}

	var h hash.Hash
	switch s.algorithm {
	case "md5":
		h = md5.New() //nolint:gosec // MD5 used for checksums, not security
	case "sha256":
		h = sha256.New()
	case "sha512":
		h = sha512.New()
	}

	h.Write([]byte(resolved))
	digest := hex.EncodeToString(h.Sum(nil))

	return &StepResult{Output: map[string]any{
		"hash":      digest,
		"algorithm": s.algorithm,
	}}, nil
}
