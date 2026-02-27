package module

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// mockS3Uploader is an in-memory S3 client for testing.
type mockS3Uploader struct {
	lastInput *s3.PutObjectInput
	err       error
}

func (m *mockS3Uploader) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	m.lastInput = input
	return &s3.PutObjectOutput{}, m.err
}

func TestS3UploadStep_BasicUpload(t *testing.T) {
	mock := &mockS3Uploader{}
	factory := NewS3UploadStepFactory()
	step, err := factory("upload", map[string]any{
		"bucket":    "my-bucket",
		"region":    "us-east-1",
		"key":       "files/test.png",
		"body_from": "file_data",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*S3UploadStep).s3Client = mock

	body := base64.StdEncoding.EncodeToString([]byte("hello world"))
	pc := NewPipelineContext(map[string]any{"file_data": body}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["key"] != "files/test.png" {
		t.Errorf("expected key 'files/test.png', got %v", result.Output["key"])
	}
	if result.Output["bucket"] != "my-bucket" {
		t.Errorf("expected bucket 'my-bucket', got %v", result.Output["bucket"])
	}
	want := "https://my-bucket.s3.us-east-1.amazonaws.com/files/test.png"
	if result.Output["url"] != want {
		t.Errorf("expected url %q, got %v", want, result.Output["url"])
	}
}

func TestS3UploadStep_TemplatedKey(t *testing.T) {
	mock := &mockS3Uploader{}
	factory := NewS3UploadStepFactory()
	step, err := factory("upload-tmpl", map[string]any{
		"bucket":    "avatars",
		"region":    "us-west-2",
		"key":       "avatars/{{ .user_id }}/photo.{{ .ext }}",
		"body_from": "data",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*S3UploadStep).s3Client = mock

	body := base64.StdEncoding.EncodeToString([]byte("png-data"))
	pc := NewPipelineContext(map[string]any{
		"data":    body,
		"user_id": "u-123",
		"ext":     "png",
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["key"] != "avatars/u-123/photo.png" {
		t.Errorf("expected resolved key, got %v", result.Output["key"])
	}
}

func TestS3UploadStep_StaticContentType(t *testing.T) {
	mock := &mockS3Uploader{}
	factory := NewS3UploadStepFactory()
	step, err := factory("upload-ct", map[string]any{
		"bucket":       "my-bucket",
		"region":       "us-east-1",
		"key":          "img.png",
		"body_from":    "data",
		"content_type": "image/png",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*S3UploadStep).s3Client = mock

	body := base64.StdEncoding.EncodeToString([]byte("png"))
	pc := NewPipelineContext(map[string]any{"data": body}, nil)

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if mock.lastInput == nil {
		t.Fatal("expected PutObject to be called")
	}
	if mock.lastInput.ContentType == nil || *mock.lastInput.ContentType != "image/png" {
		t.Errorf("expected ContentType 'image/png', got %v", mock.lastInput.ContentType)
	}
}

func TestS3UploadStep_ContentTypeFrom(t *testing.T) {
	mock := &mockS3Uploader{}
	factory := NewS3UploadStepFactory()
	step, err := factory("upload-ctf", map[string]any{
		"bucket":            "my-bucket",
		"region":            "us-east-1",
		"key":               "upload",
		"body_from":         "data",
		"content_type_from": "mime",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*S3UploadStep).s3Client = mock

	body := base64.StdEncoding.EncodeToString([]byte("bytes"))
	pc := NewPipelineContext(map[string]any{
		"data": body,
		"mime": "image/jpeg",
	}, nil)

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if mock.lastInput == nil || mock.lastInput.ContentType == nil || *mock.lastInput.ContentType != "image/jpeg" {
		t.Errorf("expected ContentType 'image/jpeg', got %v", mock.lastInput.ContentType)
	}
}

func TestS3UploadStep_CustomEndpoint(t *testing.T) {
	mock := &mockS3Uploader{}
	factory := NewS3UploadStepFactory()
	step, err := factory("upload-minio", map[string]any{
		"bucket":    "mybucket",
		"region":    "us-east-1",
		"key":       "obj/key",
		"body_from": "data",
		"endpoint":  "http://localhost:9000",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*S3UploadStep).s3Client = mock

	body := base64.StdEncoding.EncodeToString([]byte("data"))
	pc := NewPipelineContext(map[string]any{"data": body}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	want := "http://localhost:9000/mybucket/obj/key"
	if result.Output["url"] != want {
		t.Errorf("expected url %q, got %v", want, result.Output["url"])
	}
}

func TestS3UploadStep_BodyFromStepOutput(t *testing.T) {
	mock := &mockS3Uploader{}
	factory := NewS3UploadStepFactory()
	step, err := factory("upload-step", map[string]any{
		"bucket":    "bucket",
		"region":    "us-east-1",
		"key":       "file",
		"body_from": "steps.parse.raw_data",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*S3UploadStep).s3Client = mock

	body := base64.StdEncoding.EncodeToString([]byte("content"))
	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("parse", map[string]any{"raw_data": body})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Output["key"] != "file" {
		t.Errorf("expected key 'file', got %v", result.Output["key"])
	}
}

func TestS3UploadStep_MissingRequiredFields(t *testing.T) {
	factory := NewS3UploadStepFactory()

	tests := []struct {
		name    string
		config  map[string]any
		wantErr string
	}{
		{
			name:    "missing bucket",
			config:  map[string]any{"region": "us-east-1", "key": "k", "body_from": "b"},
			wantErr: "'bucket' is required",
		},
		{
			name:    "missing region",
			config:  map[string]any{"bucket": "b", "key": "k", "body_from": "b"},
			wantErr: "'region' is required",
		},
		{
			name:    "missing key",
			config:  map[string]any{"bucket": "b", "region": "us-east-1", "body_from": "b"},
			wantErr: "'key' is required",
		},
		{
			name:    "missing body_from",
			config:  map[string]any{"bucket": "b", "region": "us-east-1", "key": "k"},
			wantErr: "'body_from' is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := factory("test-step", tt.config, nil)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestS3UploadStep_InvalidBase64(t *testing.T) {
	mock := &mockS3Uploader{}
	factory := NewS3UploadStepFactory()
	step, err := factory("upload-bad", map[string]any{
		"bucket":    "b",
		"region":    "us-east-1",
		"key":       "k",
		"body_from": "data",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*S3UploadStep).s3Client = mock

	pc := NewPipelineContext(map[string]any{"data": "not-valid-base64!!!"}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
	if !strings.Contains(err.Error(), "base64-decode") {
		t.Errorf("expected base64 decode error, got %q", err.Error())
	}
}

func TestS3UploadStep_NonStringBody(t *testing.T) {
	mock := &mockS3Uploader{}
	factory := NewS3UploadStepFactory()
	step, err := factory("upload-nonstr", map[string]any{
		"bucket":    "b",
		"region":    "us-east-1",
		"key":       "k",
		"body_from": "data",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*S3UploadStep).s3Client = mock

	pc := NewPipelineContext(map[string]any{"data": 12345}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for non-string body")
	}
	if !strings.Contains(err.Error(), "base64-encoded string") {
		t.Errorf("expected type error, got %q", err.Error())
	}
}

func TestS3UploadStep_URLEncoding(t *testing.T) {
	// Ensure URL-safe base64 is also accepted.
	mock := &mockS3Uploader{}
	factory := NewS3UploadStepFactory()
	step, err := factory("upload-urlb64", map[string]any{
		"bucket":    "b",
		"region":    "us-east-1",
		"key":       "k",
		"body_from": "data",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	step.(*S3UploadStep).s3Client = mock

	// Use URL-safe base64 encoding.
	body := base64.URLEncoding.EncodeToString([]byte("url-safe content"))
	pc := NewPipelineContext(map[string]any{"data": body}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
}

func TestS3UploadStep_Name(t *testing.T) {
	factory := NewS3UploadStepFactory()
	step, err := factory("my-upload", map[string]any{
		"bucket":    "b",
		"region":    "us-east-1",
		"key":       "k",
		"body_from": "d",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	if step.Name() != "my-upload" {
		t.Errorf("expected name 'my-upload', got %q", step.Name())
	}
}
