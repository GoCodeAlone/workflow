package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSearchOrgForPluginPins_Verified(t *testing.T) {
	fake := func(ctx context.Context, endpoint string) ([]byte, error) {
		return []byte(`{
			"total_count": 2,
			"items": [
				{"path": "wfctl.yaml", "repository": {"full_name": "GoCodeAlone/buymywishlist"}},
				{"path": "wfctl.yaml", "repository": {"full_name": "GoCodeAlone/core-dump"}}
			]
		}`), nil
	}
	pins, err := searchOrgForPluginPins(context.Background(), "GoCodeAlone", "digitalocean", fake)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pins) != 2 {
		t.Fatalf("expected 2 pins, got %d: %v", len(pins), pins)
	}
	if pins[0] != "GoCodeAlone/buymywishlist/wfctl.yaml" {
		t.Errorf("pin[0]=%q want GoCodeAlone/buymywishlist/wfctl.yaml", pins[0])
	}
}

func TestSearchOrgForPluginPins_Experimental(t *testing.T) {
	fake := func(ctx context.Context, endpoint string) ([]byte, error) {
		return []byte(`{"total_count": 0, "items": []}`), nil
	}
	pins, err := searchOrgForPluginPins(context.Background(), "GoCodeAlone", "aws", fake)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pins) != 0 {
		t.Fatalf("expected 0 pins, got %d: %v", len(pins), pins)
	}
}

func TestSearchOrgForPluginPins_GHAPIError(t *testing.T) {
	fake := func(ctx context.Context, endpoint string) ([]byte, error) {
		return nil, errors.New("rate limit")
	}
	_, err := searchOrgForPluginPins(context.Background(), "GoCodeAlone", "aws", fake)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSearchOrgForPluginPins_BadJSON(t *testing.T) {
	fake := func(ctx context.Context, endpoint string) ([]byte, error) {
		return []byte(`not json`), nil
	}
	_, err := searchOrgForPluginPins(context.Background(), "GoCodeAlone", "aws", fake)
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
}

func TestUrlQueryEscape(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"a b c", "a%20b%20c"},
		{`"hello"`, "%22hello%22"},
		{"org:GoCodeAlone", "org%3AGoCodeAlone"},
		{`filename:wfctl.yaml org:X "name: workflow-plugin-aws"`,
			`filename%3Awfctl.yaml%20org%3AX%20%22name%3A%20workflow-plugin-aws%22`},
	}
	for _, tc := range cases {
		got := urlQueryEscape(tc.in)
		if got != tc.want {
			t.Errorf("escape(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestSearchEndpoint_BuildsExpectedURL(t *testing.T) {
	var captured string
	fake := func(ctx context.Context, endpoint string) ([]byte, error) {
		captured = endpoint
		return []byte(`{"items":[]}`), nil
	}
	_, _ = searchOrgForPluginPins(context.Background(), "GoCodeAlone", "twilio", fake)
	wantSub := "filename%3Awfctl.yaml%20org%3AGoCodeAlone"
	wantPlugin := "workflow-plugin-twilio"
	if captured == "" {
		t.Fatal("endpoint not captured")
	}
	if !strings.Contains(captured, wantSub) {
		t.Errorf("endpoint missing %q: %s", wantSub, captured)
	}
	if !strings.Contains(captured, wantPlugin) {
		t.Errorf("endpoint missing plugin name %q: %s", wantPlugin, captured)
	}
}
