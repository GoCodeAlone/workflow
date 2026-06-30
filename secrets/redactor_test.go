package secrets

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// stubProvider is a minimal in-memory secrets.Provider used to exercise
// Redactor.LoadFromProvider. It satisfies the full Provider interface
// (Name/Get/Set/Delete/List).
type stubProvider struct {
	vals map[string]string
}

func (s *stubProvider) Name() string { return "stub" }

func (s *stubProvider) Get(_ context.Context, key string) (string, error) {
	if v, ok := s.vals[key]; ok {
		return v, nil
	}
	return "", ErrNotFound
}

func (s *stubProvider) Set(_ context.Context, key, value string) error {
	if s.vals == nil {
		s.vals = map[string]string{}
	}
	s.vals[key] = value
	return nil
}

func (s *stubProvider) Delete(_ context.Context, key string) error {
	delete(s.vals, key)
	return nil
}

func (s *stubProvider) List(_ context.Context) ([]string, error) {
	keys := make([]string, 0, len(s.vals))
	for k := range s.vals {
		keys = append(keys, k)
	}
	return keys, nil
}

func TestRedactor_AddValue_Redacts(t *testing.T) {
	r := NewRedactor()
	r.AddValue("API_KEY", "sk-secret-123")
	got := r.Redact("the key is sk-secret-123 here")
	want := "the key is [REDACTED:API_KEY] here"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRedactor_LoadFromProvider_FullReplace(t *testing.T) {
	r := NewRedactor()
	r.AddValue("ENV", "envval")
	if err := r.LoadFromProvider(context.Background(), &stubProvider{vals: map[string]string{"K1": "v1"}}); err != nil {
		t.Fatalf("LoadFromProvider error: %v", err)
	}
	// full-replace: ENV gone, K1 present
	if got := r.Redact("envval"); got != "envval" {
		t.Errorf("ENV not dropped after full-replace: got %q want %q", got, "envval")
	}
	if got := r.Redact("v1"); got != "[REDACTED:K1]" {
		t.Errorf("K1 not loaded after full-replace: got %q want %q", got, "[REDACTED:K1]")
	}
}

func TestRedactor_NoProvider_RedactsAddedOnly(t *testing.T) {
	r := NewRedactor() // zero-value, no provider
	r.AddValue("X", "xx")
	if got := r.Redact("xx"); got != "[REDACTED:X]" {
		t.Errorf("got %q want %q", got, "[REDACTED:X]")
	}
}

func TestRedactor_Concurrent(t *testing.T) {
	// -race guard: concurrent Redact during LoadFromProvider + AddValue must
	// not trigger a data race. All three touch the same underlying map under
	// differing locks (RLock vs write lock); the RWMutex must serialize them.
	r := NewRedactor()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			_ = r.LoadFromProvider(context.Background(), &stubProvider{vals: map[string]string{"K": "v"}})
		}()
		go func() {
			defer wg.Done()
			r.AddValue("K", "v")
		}()
		go func() {
			defer wg.Done()
			_ = r.Redact("v")
		}()
	}
	wg.Wait() // -race: no data race
}

func TestRedactor_LongestValueWins(t *testing.T) {
	// A shorter secret value that is a substring of a longer one must not shadow
	// the longer value (which would leave a partial leak). Longest-first wins.
	r := NewRedactor()
	r.AddValue("SHORT", "key123")
	r.AddValue("LONG", "key12345")
	got := r.Redact("cred=key12345 and also key123")
	// "key12345" must be fully redacted as LONG; the standalone "key123" as SHORT.
	if strings.Contains(got, "key12345") || strings.Contains(got, "key123") {
		t.Fatalf("partial leak: %q (substring-ordering regression)", got)
	}
	if !strings.Contains(got, "[REDACTED:LONG]") || !strings.Contains(got, "[REDACTED:SHORT]") {
		t.Fatalf("expected both labels redacted, got %q", got)
	}
}
