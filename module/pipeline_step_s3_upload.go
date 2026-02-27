package module

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/CrisisTextLine/modular"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// s3PutObjectAPI is the minimal S3 interface needed by S3UploadStep,
// allowing injection of a mock client in tests.
type s3PutObjectAPI interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

// S3UploadStep uploads binary data (base64-encoded in the pipeline context)
// to S3-compatible object storage and returns the public URL, key, and bucket.
type S3UploadStep struct {
	name            string
	bucket          string
	region          string
	key             string // may contain Go template expressions (e.g. "avatars/{{.user_id}}/{{uuid}}.{{.ext}}")
	bodyFrom        string // dot-path to base64-encoded body in the pipeline context
	contentTypeFrom string // dot-path to MIME type (optional)
	contentType     string // static MIME type (optional)
	endpoint        string // custom S3 endpoint for MinIO/LocalStack (optional)
	tmpl            *TemplateEngine
	s3Client        s3PutObjectAPI // injected in tests; nil triggers lazy init
}

// NewS3UploadStepFactory returns a StepFactory that creates S3UploadStep instances.
func NewS3UploadStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		bucket := os.ExpandEnv(s3UploadStringConfig(config, "bucket"))
		if bucket == "" {
			return nil, fmt.Errorf("s3_upload step %q: 'bucket' is required", name)
		}

		region := os.ExpandEnv(s3UploadStringConfig(config, "region"))
		if region == "" {
			return nil, fmt.Errorf("s3_upload step %q: 'region' is required", name)
		}

		key := s3UploadStringConfig(config, "key")
		if key == "" {
			return nil, fmt.Errorf("s3_upload step %q: 'key' is required", name)
		}

		bodyFrom := s3UploadStringConfig(config, "body_from")
		if bodyFrom == "" {
			return nil, fmt.Errorf("s3_upload step %q: 'body_from' is required", name)
		}

		return &S3UploadStep{
			name:            name,
			bucket:          bucket,
			region:          region,
			key:             key,
			bodyFrom:        bodyFrom,
			contentTypeFrom: s3UploadStringConfig(config, "content_type_from"),
			contentType:     s3UploadStringConfig(config, "content_type"),
			endpoint:        os.ExpandEnv(s3UploadStringConfig(config, "endpoint")),
			tmpl:            NewTemplateEngine(),
		}, nil
	}
}

// s3UploadStringConfig extracts a string value from a config map, returning ""
// if the key is absent or the value is not a string.
func s3UploadStringConfig(config map[string]any, key string) string {
	v, _ := config[key].(string)
	return v
}

// Name returns the step name.
func (s *S3UploadStep) Name() string { return s.name }

// Execute uploads binary data from the pipeline context to S3 and returns the
// public URL, the resolved key, and the bucket name as step output.
func (s *S3UploadStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	// Resolve the key template (supports {{ .field }} and {{ uuid }} etc.)
	resolvedKey, err := s.tmpl.Resolve(s.key, pc)
	if err != nil {
		return nil, fmt.Errorf("s3_upload step %q: failed to resolve key template: %w", s.name, err)
	}

	// Resolve body_from dot-path to obtain the base64-encoded body.
	bodyVal, err := s.resolveFromPath(pc, s.bodyFrom)
	if err != nil {
		return nil, fmt.Errorf("s3_upload step %q: body_from %q: %w", s.name, s.bodyFrom, err)
	}

	bodyStr, ok := bodyVal.(string)
	if !ok {
		return nil, fmt.Errorf("s3_upload step %q: body_from value must be a base64-encoded string, got %T", s.name, bodyVal)
	}

	bodyBytes, err := s3UploadDecodeBase64(bodyStr)
	if err != nil {
		return nil, fmt.Errorf("s3_upload step %q: failed to base64-decode body: %w", s.name, err)
	}

	// Resolve content type (content_type_from takes precedence over content_type).
	contentType, err := s.resolveContentType(pc)
	if err != nil {
		return nil, fmt.Errorf("s3_upload step %q: %w", s.name, err)
	}

	// Obtain the S3 client (injected or built from config).
	client, err := s.getClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("s3_upload step %q: failed to build S3 client: %w", s.name, err)
	}

	input := &s3.PutObjectInput{
		Bucket: &s.bucket,
		Key:    &resolvedKey,
		Body:   bytes.NewReader(bodyBytes),
	}
	if contentType != "" {
		input.ContentType = &contentType
	}

	if _, err = client.PutObject(ctx, input); err != nil {
		return nil, fmt.Errorf("s3_upload step %q: PutObject failed: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"url":    s.buildURL(resolvedKey),
		"key":    resolvedKey,
		"bucket": s.bucket,
	}}, nil
}

// resolveFromPath walks a dot-separated path (e.g. "steps.parse.data") through
// the pipeline context, including step outputs under the "steps" key.
func (s *S3UploadStep) resolveFromPath(pc *PipelineContext, path string) (any, error) {
	data := make(map[string]any, len(pc.Current)+1)
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
	return resolveDottedPath(data, path)
}

// resolveContentType returns the effective content type:
// content_type_from (dot-path lookup) takes precedence; falls back to the
// static content_type field.
func (s *S3UploadStep) resolveContentType(pc *PipelineContext) (string, error) {
	if s.contentTypeFrom != "" {
		ctVal, err := s.resolveFromPath(pc, s.contentTypeFrom)
		if err != nil {
			return "", fmt.Errorf("content_type_from %q: %w", s.contentTypeFrom, err)
		}
		if ct, ok := ctVal.(string); ok {
			return ct, nil
		}
	}
	return s.contentType, nil
}

// getClient returns the injected client if set, otherwise builds a new AWS S3
// client from the step's region and optional custom endpoint.
func (s *S3UploadStep) getClient(ctx context.Context) (s3PutObjectAPI, error) {
	if s.s3Client != nil {
		return s.s3Client, nil
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(s.region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	var s3Opts []func(*s3.Options)
	if s.endpoint != "" {
		ep := s.endpoint
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = &ep
			o.UsePathStyle = true
		})
	}

	return s3.NewFromConfig(cfg, s3Opts...), nil
}

// buildURL constructs the public URL for the uploaded object.
// When a custom endpoint is configured (MinIO, LocalStack, etc.) it uses
// path-style: {endpoint}/{bucket}/{key}.
// Otherwise it uses the standard AWS virtual-hosted URL:
// https://{bucket}.s3.{region}.amazonaws.com/{key}.
func (s *S3UploadStep) buildURL(key string) string {
	if s.endpoint != "" {
		ep := strings.TrimRight(s.endpoint, "/")
		return fmt.Sprintf("%s/%s/%s", ep, s.bucket, key)
	}
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s.bucket, s.region, key)
}

// s3UploadDecodeBase64 attempts standard base64, then URL-safe base64,
// then the raw (no-padding) variants, returning the first successful decode.
func s3UploadDecodeBase64(encoded string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(encoded); err == nil {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(encoded); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(encoded); err == nil {
		return b, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	return b, nil
}
