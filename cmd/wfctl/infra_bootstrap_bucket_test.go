package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// fakeBucketClient implements spacesBucketClient for unit tests.
type fakeBucketClient struct {
	headErr      error // nil → bucket exists; *s3types.NotFound → bucket absent; other → fatal
	createErr    error
	headCalled   bool
	createCalled bool
	lastBucket   string
	lastRegion   string
}

func (f *fakeBucketClient) HeadBucket(_ context.Context, input *s3.HeadBucketInput, _ ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	f.headCalled = true
	if input.Bucket != nil {
		f.lastBucket = *input.Bucket
	}
	if f.headErr != nil {
		return nil, f.headErr
	}
	return &s3.HeadBucketOutput{}, nil
}

func (f *fakeBucketClient) CreateBucket(_ context.Context, input *s3.CreateBucketInput, _ ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	f.createCalled = true
	if input.Bucket != nil {
		f.lastBucket = *input.Bucket
	}
	if input.CreateBucketConfiguration != nil {
		f.lastRegion = string(input.CreateBucketConfiguration.LocationConstraint)
	}
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &s3.CreateBucketOutput{}, nil
}

func TestBootstrapDOSpacesBucket_AlreadyExists(t *testing.T) {
	client := &fakeBucketClient{headErr: nil} // nil → bucket exists (200 OK)
	if err := bootstrapDOSpacesBucketWithClient(context.Background(), "my-bucket", "nyc3", client); err != nil {
		t.Fatalf("expected no error when bucket already exists, got: %v", err)
	}
	if !client.headCalled {
		t.Error("expected HeadBucket to be called")
	}
	if client.createCalled {
		t.Error("expected CreateBucket NOT to be called when bucket already exists")
	}
}

func TestBootstrapDOSpacesBucket_CreatesNew(t *testing.T) {
	client := &fakeBucketClient{
		headErr: &s3types.NotFound{}, // 404 → bucket doesn't exist
	}
	if err := bootstrapDOSpacesBucketWithClient(context.Background(), "new-bucket", "nyc3", client); err != nil {
		t.Fatalf("expected no error creating new bucket, got: %v", err)
	}
	if !client.createCalled {
		t.Error("expected CreateBucket to be called when bucket does not exist")
	}
	if client.lastBucket != "new-bucket" {
		t.Errorf("CreateBucket bucket: want %q, got %q", "new-bucket", client.lastBucket)
	}
	if client.lastRegion != "nyc3" {
		t.Errorf("CreateBucket region: want %q, got %q", "nyc3", client.lastRegion)
	}
}

func TestBootstrapDOSpacesBucket_CreateErrorSurfaced(t *testing.T) {
	createErr := errors.New("spaces quota exceeded")
	client := &fakeBucketClient{
		headErr:   &s3types.NotFound{},
		createErr: createErr,
	}
	err := bootstrapDOSpacesBucketWithClient(context.Background(), "bad-bucket", "nyc3", client)
	if err == nil {
		t.Fatal("expected error on create failure")
	}
	if !strings.Contains(err.Error(), "quota exceeded") {
		t.Errorf("expected create error in message, got: %v", err)
	}
}

func TestBootstrapDOSpacesBucket_HeadFatalError(t *testing.T) {
	// A non-404 HeadBucket error (e.g. 403 Forbidden) must fail fast without creating.
	client := &fakeBucketClient{
		headErr: errors.New("403 Forbidden"),
	}
	err := bootstrapDOSpacesBucketWithClient(context.Background(), "my-bucket", "nyc3", client)
	if err == nil {
		t.Fatal("expected error on fatal HeadBucket failure")
	}
	if client.createCalled {
		t.Error("expected CreateBucket NOT to be called after fatal HeadBucket error")
	}
}

func TestBootstrapDOSpacesBucket_AlreadyOwnedIsOK(t *testing.T) {
	// BucketAlreadyOwnedByYou is a benign race: concurrent create won, we still own it.
	client := &fakeBucketClient{
		headErr:   &s3types.NotFound{},
		createErr: &s3types.BucketAlreadyOwnedByYou{},
	}
	if err := bootstrapDOSpacesBucketWithClient(context.Background(), "my-bucket", "nyc3", client); err != nil {
		t.Fatalf("expected no error for BucketAlreadyOwnedByYou, got: %v", err)
	}
}

func TestBootstrapDOSpacesBucket_MissingCredentials(t *testing.T) {
	// bootstrapDOSpacesBucket (the real entry point) must fail if credentials are absent.
	err := bootstrapDOSpacesBucket(context.Background(), "my-bucket", "nyc3", "", "")
	if err == nil {
		t.Fatal("expected error when access key and secret key are empty")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "access key") {
		t.Errorf("expected 'access key' in error message, got: %v", err)
	}
}
