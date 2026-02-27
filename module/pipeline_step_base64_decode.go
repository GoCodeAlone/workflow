package module

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
	"strings"

	"github.com/CrisisTextLine/modular"
)

const (
	base64DecodeFormatDataURI   = "data_uri"
	base64DecodeFormatRawBase64 = "raw_base64"
)

// mimeToExtension maps common MIME types to their canonical file extensions.
var mimeToExtension = map[string]string{
	"image/jpeg":               ".jpg",
	"image/png":                ".png",
	"image/gif":                ".gif",
	"image/webp":               ".webp",
	"image/bmp":                ".bmp",
	"image/tiff":               ".tiff",
	"image/svg+xml":            ".svg",
	"image/x-icon":             ".ico",
	"application/pdf":          ".pdf",
	"application/zip":          ".zip",
	"text/plain":               ".txt",
	"text/html":                ".html",
	"text/css":                 ".css",
	"text/javascript":          ".js",
	"application/json":         ".json",
	"application/xml":          ".xml",
	"audio/mpeg":               ".mp3",
	"audio/ogg":                ".ogg",
	"audio/wav":                ".wav",
	"video/mp4":                ".mp4",
	"video/webm":               ".webm",
	"video/ogg":                ".ogv",
	"application/octet-stream": ".bin",
	"application/gzip":         ".gz",
	"application/x-tar":        ".tar",
	"application/vnd.ms-excel": ".xls",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": ".xlsx",
	"application/msword": ".doc",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": ".docx",
}

// Base64DecodeStep decodes base64-encoded content (raw or data-URI), optionally
// validating the MIME type and decoded size.
type Base64DecodeStep struct {
	name          string
	inputFrom     string
	format        string
	allowedTypes  []string
	maxSizeBytes  int
	validateMagic bool
}

// NewBase64DecodeStepFactory returns a StepFactory that creates Base64DecodeStep instances.
func NewBase64DecodeStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		inputFrom, _ := config["input_from"].(string)
		if inputFrom == "" {
			return nil, fmt.Errorf("base64_decode step %q: 'input_from' is required", name)
		}

		format, _ := config["format"].(string)
		if format == "" {
			format = base64DecodeFormatDataURI
		}
		if format != base64DecodeFormatDataURI && format != base64DecodeFormatRawBase64 {
			return nil, fmt.Errorf("base64_decode step %q: 'format' must be %q or %q", name, base64DecodeFormatDataURI, base64DecodeFormatRawBase64)
		}

		var allowedTypes []string
		if raw, ok := config["allowed_types"].([]any); ok {
			for _, t := range raw {
				if s, ok := t.(string); ok && s != "" {
					allowedTypes = append(allowedTypes, strings.ToLower(s))
				}
			}
		}

		maxSizeBytes := 0
		switch v := config["max_size_bytes"].(type) {
		case int:
			maxSizeBytes = v
		case int64:
			maxSizeBytes = int(v)
		case float64:
			maxSizeBytes = int(v)
		}

		validateMagic, _ := config["validate_magic_bytes"].(bool)

		return &Base64DecodeStep{
			name:          name,
			inputFrom:     inputFrom,
			format:        format,
			allowedTypes:  allowedTypes,
			maxSizeBytes:  maxSizeBytes,
			validateMagic: validateMagic,
		}, nil
	}
}

// Name returns the step name.
func (s *Base64DecodeStep) Name() string { return s.name }

// Execute decodes the base64 content from the pipeline context, validates it,
// and returns structured metadata plus the re-encoded base64 data.
func (s *Base64DecodeStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	// Resolve the input value from the pipeline context.
	// A missing or unresolvable path is treated as invalid input rather than a
	// hard error, consistent with the step's non-fatal validation semantics.
	raw, err := s.resolveInput(pc)
	if err != nil {
		return s.invalid(fmt.Sprintf("could not resolve input_from %q: %v", s.inputFrom, err))
	}

	encoded, ok := raw.(string)
	if !ok {
		return s.invalid(fmt.Sprintf("input at %q is not a string (got %T)", s.inputFrom, raw))
	}

	// Parse the encoded string and determine the claimed MIME type.
	var claimedMIME, b64data string
	switch s.format {
	case base64DecodeFormatDataURI:
		claimedMIME, b64data, err = parseDataURI(encoded)
		if err != nil {
			return s.invalid(fmt.Sprintf("invalid data-URI: %v", err))
		}
	case base64DecodeFormatRawBase64:
		b64data = encoded
	}

	// Guard against excessively large allocations when max_size_bytes is set.
	// Base64 encodes 3 bytes into 4 characters, so the decoded length is at
	// most ceil(len(b64data)/4)*3. If that upper bound already exceeds the
	// limit we can reject without decoding the full payload.
	if s.maxSizeBytes > 0 {
		estimatedMax := (len(b64data)/4 + 1) * 3
		if estimatedMax > s.maxSizeBytes {
			// Perform a precise check only when the estimate exceeds the limit.
			// We still need to decode to get the exact size, but we use the
			// estimate as an early-exit hint for clearly oversized inputs.
			if len(b64data) > (s.maxSizeBytes/3+1)*4 {
				return s.invalid(fmt.Sprintf("encoded length indicates decoded size would exceed max_size_bytes %d", s.maxSizeBytes))
			}
		}
	}

	// Decode the base64 payload.
	decoded, err := base64.StdEncoding.DecodeString(b64data)
	if err != nil {
		// Try URL-safe / padded variants used in some base64 encoders.
		decoded, err = base64.RawStdEncoding.DecodeString(b64data)
		if err != nil {
			decoded, err = base64.URLEncoding.DecodeString(b64data)
			if err != nil {
				decoded, err = base64.RawURLEncoding.DecodeString(b64data)
				if err != nil {
					return s.invalid("base64 decode failed: not valid base64")
				}
			}
		}
	}

	// Enforce max size with exact decoded length.
	if s.maxSizeBytes > 0 && len(decoded) > s.maxSizeBytes {
		return s.invalid(fmt.Sprintf("decoded size %d exceeds max_size_bytes %d", len(decoded), s.maxSizeBytes))
	}

	// Detect actual MIME type via magic bytes (using Go's built-in sniffer).
	detectedMIME := http.DetectContentType(decoded)
	// DetectContentType may include parameters (e.g. "text/plain; charset=utf-8"); strip them.
	detectedMIME, _, _ = mime.ParseMediaType(detectedMIME)
	if detectedMIME == "" {
		detectedMIME = "application/octet-stream"
	}

	// Determine the effective content type: prefer the claimed type from the
	// data-URI when not validating magic bytes; otherwise use the detected type.
	contentType := detectedMIME
	if s.format == base64DecodeFormatDataURI && claimedMIME != "" && !s.validateMagic {
		contentType = claimedMIME
	}

	// Validate magic bytes: the detected MIME should match the claimed one.
	if s.validateMagic && s.format == base64DecodeFormatDataURI && claimedMIME != "" {
		if !mimeTypesCompatible(detectedMIME, claimedMIME) {
			return s.invalid(fmt.Sprintf("magic bytes indicate %q but data-URI claims %q", detectedMIME, claimedMIME))
		}
	}

	// Check against the allowed-types whitelist.
	if len(s.allowedTypes) > 0 {
		if !mimeAllowed(contentType, s.allowedTypes) {
			return s.invalid(fmt.Sprintf("content type %q is not in allowed_types", contentType))
		}
	}

	ext := extensionForMIME(contentType)

	return &StepResult{
		Output: map[string]any{
			"content_type": contentType,
			"extension":    ext,
			"size_bytes":   len(decoded),
			"data":         base64.StdEncoding.EncodeToString(decoded),
			"valid":        true,
		},
	}, nil
}

// invalid returns a StepResult with valid=false and a reason field (no error).
// All output keys are present with zero/empty defaults so that downstream
// template expressions that reference e.g. {{ .content_type }} do not fail.
func (s *Base64DecodeStep) invalid(reason string) (*StepResult, error) {
	return &StepResult{
		Output: map[string]any{
			"valid":        false,
			"reason":       reason,
			"content_type": "",
			"extension":    "",
			"size_bytes":   0,
			"data":         "",
		},
	}, nil
}

// resolveInput reads the value at s.inputFrom from the pipeline context.
func (s *Base64DecodeStep) resolveInput(pc *PipelineContext) (any, error) {
	data := make(map[string]any)
	for k, v := range pc.Current {
		data[k] = v
	}
	if len(pc.StepOutputs) > 0 {
		steps := make(map[string]any, len(pc.StepOutputs))
		for k, v := range pc.StepOutputs {
			steps[k] = v
		}
		data["steps"] = steps
	}
	return resolveDottedPath(data, s.inputFrom)
}

// parseDataURI splits a data-URI string (data:<mime>[;base64],<data>) into its
// MIME type and base64-encoded payload. Returns an error if the format is wrong
// or if the encoding is not ";base64".
func parseDataURI(s string) (mimeType, b64data string, err error) {
	if !strings.HasPrefix(s, "data:") {
		return "", "", fmt.Errorf("missing 'data:' prefix")
	}
	s = s[len("data:"):]

	commaIdx := strings.IndexByte(s, ',')
	if commaIdx < 0 {
		return "", "", fmt.Errorf("missing ',' separator")
	}

	meta := s[:commaIdx]
	b64data = s[commaIdx+1:]

	parts := strings.Split(meta, ";")
	mimeType = strings.ToLower(strings.TrimSpace(parts[0]))
	if mimeType == "" {
		mimeType = "text/plain"
	}

	// Verify that the encoding is base64 (";base64" must be present).
	isBase64 := false
	for _, p := range parts[1:] {
		if strings.TrimSpace(p) == "base64" {
			isBase64 = true
			break
		}
	}
	if !isBase64 {
		return "", "", fmt.Errorf("only base64-encoded data-URIs are supported (missing ';base64')")
	}

	return mimeType, b64data, nil
}

// mimeTypesCompatible returns true when detected and claimed MIME types are
// considered to represent the same file format. It handles common equivalences
// (e.g. "image/jpg" vs "image/jpeg") and also accepts an exact match.
func mimeTypesCompatible(detected, claimed string) bool {
	if detected == claimed {
		return true
	}
	// Normalise jpeg variants.
	normalize := func(m string) string {
		m = strings.ToLower(m)
		if m == "image/jpg" {
			return "image/jpeg"
		}
		return m
	}
	return normalize(detected) == normalize(claimed)
}

// mimeAllowed returns true when contentType matches one of the allowed types.
// The comparison is case-insensitive and strips any parameters.
func mimeAllowed(contentType string, allowed []string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	for _, a := range allowed {
		if strings.ToLower(strings.TrimSpace(a)) == ct {
			return true
		}
	}
	return false
}

// extensionForMIME returns a canonical file extension for a MIME type, falling
// back to the standard library's mime.ExtensionsByType, and ultimately ".bin".
func extensionForMIME(mimeType string) string {
	if ext, ok := mimeToExtension[strings.ToLower(mimeType)]; ok {
		return ext
	}
	// Try stdlib
	exts, err := mime.ExtensionsByType(mimeType)
	if err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ".bin"
}
