package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
)

// EventDecryptStep decrypts field-level encryption applied by step.event_publish.
// It reads the CloudEvents extension attributes ("encrypteddek", "encryptedfields",
// "keyid") from the current pipeline context and decrypts the specified fields
// inside the event's "data" object.
type EventDecryptStep struct {
	name  string
	keyID string // overrides the keyid extension when set
	app   modular.Application
	tmpl  *TemplateEngine
}

// NewEventDecryptStepFactory returns a StepFactory that creates EventDecryptStep instances.
func NewEventDecryptStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		step := &EventDecryptStep{
			name: name,
			app:  app,
			tmpl: NewTemplateEngine(),
		}

		// key_id overrides the keyid extension found in the event.
		// Supports "${ENV_VAR}" references.
		if k, ok := config["key_id"].(string); ok {
			step.keyID = k
		}

		return step, nil
	}
}

// Name returns the step name.
func (s *EventDecryptStep) Name() string { return s.name }

// supportedEncryptionAlgorithm is the only algorithm this step can decrypt.
const supportedEncryptionAlgorithm = "AES-256-GCM"

// Execute decrypts the fields in the incoming CloudEvent.
//
// Expected shape of pc.Current (CloudEvents envelope from step.event_publish):
//
//	{
//	  "specversion":     "1.0",          // optional
//	  "type":            "...",           // optional
//	  "source":          "...",           // optional
//	  "id":              "...",           // optional
//	  "time":            "...",           // optional
//	  "encryption":      "AES-256-GCM",  // extension — validated before decryption
//	  "keyid":           "<key-id>",      // extension
//	  "encrypteddek":    "<base64>",      // extension
//	  "encryptedfields": "field1,field2", // extension
//	  "data": {                           // payload with encrypted fields
//	    "field1": "<base64-ciphertext>",
//	    "field2": "<base64-ciphertext>",
//	    ...
//	  }
//	}
//
// The step returns the same envelope structure with "data" containing the
// decrypted field values. The encryption extension attributes are preserved.
func (s *EventDecryptStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	event := pc.Current
	if event == nil {
		return &StepResult{Output: map[string]any{"decrypted": false, "reason": "no event data"}}, nil
	}

	// Read encryption extension attributes.
	encryptedDEKB64, _ := event["encrypteddek"].(string)
	encryptedFields, _ := event["encryptedfields"].(string)
	keyID, _ := event["keyid"].(string)
	algorithm, _ := event["encryption"].(string)

	// Override keyID from step configuration if provided.
	if s.keyID != "" {
		keyID = s.keyID
	}

	// If the event has no encryption metadata, pass through unchanged.
	if encryptedDEKB64 == "" || encryptedFields == "" || keyID == "" {
		return &StepResult{Output: event}, nil
	}

	// Validate the encryption algorithm before attempting decryption.
	// Events produced by an unknown scheme should not silently fail or
	// produce garbage — return a clear error instead.
	if algorithm != "" && algorithm != supportedEncryptionAlgorithm {
		return nil, fmt.Errorf("event_decrypt step %q: unsupported encryption algorithm %q (supported: %s)", s.name, algorithm, supportedEncryptionAlgorithm)
	}

	// Locate the payload — either under "data" (CloudEvents envelope) or the event itself.
	payload, hasData := event["data"].(map[string]any)
	if !hasData {
		// Treat the whole event as the payload (flat structure without envelope).
		payload = event
	}

	decrypted, err := decryptEventFields(payload, encryptedDEKB64, encryptedFields, keyID)
	if err != nil {
		return nil, fmt.Errorf("event_decrypt step %q: %w", s.name, err)
	}

	// Rebuild the output envelope, preserving all non-data fields.
	output := make(map[string]any, len(event))
	for k, v := range event {
		output[k] = v
	}
	if hasData {
		output["data"] = decrypted
	} else {
		// Merge decrypted fields back into the top-level map.
		for k, v := range decrypted {
			output[k] = v
		}
	}

	return &StepResult{Output: output}, nil
}
