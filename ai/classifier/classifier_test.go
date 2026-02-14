package classifier

import (
	"context"
	"testing"
	"time"
)

// mockProvider implements LLMProvider for testing.
type mockClassifierProvider struct {
	result *Classification
	err    error
}

func (m *mockClassifierProvider) Classify(_ context.Context, _, _ string) (*Classification, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func TestNewConversationClassifier(t *testing.T) {
	c := NewConversationClassifier(nil, DefaultConfig())
	if c == nil {
		t.Fatal("expected non-nil classifier")
	}
	if len(c.rules) == 0 {
		t.Error("expected default rules to be loaded")
	}
}

func TestClassify_CrisisSuicidal(t *testing.T) {
	c := NewConversationClassifier(nil, DefaultConfig())

	messages := []Message{
		{Body: "I want to kill myself tonight", Role: "texter"},
	}

	result, err := c.Classify(context.Background(), "conv-1", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Category != CategoryCrisis {
		t.Errorf("expected crisis category, got %s", result.Category)
	}
	if result.Priority != 1 {
		t.Errorf("expected priority 1, got %d", result.Priority)
	}
}

func TestClassify_CrisisSelfHarm(t *testing.T) {
	c := NewConversationClassifier(nil, DefaultConfig())

	messages := []Message{
		{Body: "I've been cutting myself for weeks", Role: "texter"},
	}

	result, err := c.Classify(context.Background(), "conv-selfharm", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Category != CategoryCrisis {
		t.Errorf("expected crisis category, got %s", result.Category)
	}
}

func TestClassify_GeneralSupport(t *testing.T) {
	c := NewConversationClassifier(nil, DefaultConfig())

	messages := []Message{
		{Body: "I'm feeling really anxious and worried about school", Role: "texter"},
	}

	result, err := c.Classify(context.Background(), "conv-2", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Category != CategoryGeneralSupport {
		t.Errorf("expected general-support category, got %s", result.Category)
	}
	if result.Priority > 4 {
		t.Errorf("expected priority <= 4, got %d", result.Priority)
	}
}

func TestClassify_Information(t *testing.T) {
	c := NewConversationClassifier(nil, DefaultConfig())

	messages := []Message{
		{Body: "Where can I find resources for help?", Role: "texter"},
	}

	result, err := c.Classify(context.Background(), "conv-3", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Category != CategoryInformation {
		t.Errorf("expected information category, got %s", result.Category)
	}
}

func TestClassify_Referral(t *testing.T) {
	c := NewConversationClassifier(nil, DefaultConfig())

	messages := []Message{
		{Body: "I need to find a therapist or psychiatrist for medication", Role: "texter"},
	}

	result, err := c.Classify(context.Background(), "conv-4", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Category != CategoryReferral {
		t.Errorf("expected referral category, got %s", result.Category)
	}
}

func TestClassify_DefaultToGeneralSupport(t *testing.T) {
	c := NewConversationClassifier(nil, DefaultConfig())

	messages := []Message{
		{Body: "hello there xyzzy foobar", Role: "texter"},
	}

	result, err := c.Classify(context.Background(), "conv-5", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Category != CategoryGeneralSupport {
		t.Errorf("expected general-support default, got %s", result.Category)
	}
	if result.Confidence > 0.5 {
		t.Errorf("expected low confidence for default, got %f", result.Confidence)
	}
}

func TestClassify_LLMProvider(t *testing.T) {
	provider := &mockClassifierProvider{
		result: &Classification{
			Category:   CategoryCrisis,
			Confidence: 0.95,
			Priority:   1,
		},
	}
	c := NewConversationClassifier(provider, DefaultConfig())

	messages := []Message{
		{Body: "some message", Role: "texter"},
	}

	result, err := c.Classify(context.Background(), "conv-6", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Category != CategoryCrisis {
		t.Errorf("expected LLM crisis result, got %s", result.Category)
	}
	if result.Confidence != 0.95 {
		t.Errorf("expected confidence 0.95, got %f", result.Confidence)
	}
}

func TestClassify_LLMFallbackToRules(t *testing.T) {
	provider := &mockClassifierProvider{
		err: context.DeadlineExceeded,
	}
	c := NewConversationClassifier(provider, DefaultConfig())

	messages := []Message{
		{Body: "I want to kill myself", Role: "texter"},
	}

	result, err := c.Classify(context.Background(), "conv-7", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Category != CategoryCrisis {
		t.Errorf("expected crisis from rule fallback, got %s", result.Category)
	}
}

func TestClassify_Caching(t *testing.T) {
	c := NewConversationClassifier(nil, Config{CacheTTL: 10 * time.Minute})

	messages := []Message{
		{Body: "I'm anxious", Role: "texter"},
	}

	r1, err := c.Classify(context.Background(), "conv-cache", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r2, err := c.Classify(context.Background(), "conv-cache", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if r1.Category != r2.Category {
		t.Error("expected cached result to match")
	}
}

func TestClassify_NoMessages(t *testing.T) {
	c := NewConversationClassifier(nil, DefaultConfig())
	_, err := c.Classify(context.Background(), "conv-empty", nil)
	if err == nil {
		t.Error("expected error for empty messages")
	}
}

func TestInvalidateConversation(t *testing.T) {
	c := NewConversationClassifier(nil, DefaultConfig())

	messages := []Message{
		{Body: "test message", Role: "texter"},
	}

	_, err := c.Classify(context.Background(), "conv-inv", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c.InvalidateConversation("conv-inv")

	c.cacheMu.RLock()
	_, ok := c.cache["conv-inv"]
	c.cacheMu.RUnlock()
	if ok {
		t.Error("expected cache entry to be removed")
	}
}

func TestClearCache(t *testing.T) {
	c := NewConversationClassifier(nil, DefaultConfig())
	c.cacheMu.Lock()
	c.cache["test"] = &Classification{Category: CategoryGeneralSupport}
	c.cacheMu.Unlock()

	c.ClearCache()

	c.cacheMu.RLock()
	if len(c.cache) != 0 {
		t.Error("expected empty cache")
	}
	c.cacheMu.RUnlock()
}

func TestAllCategories(t *testing.T) {
	cats := AllCategories()
	if len(cats) != 4 {
		t.Errorf("expected 4 categories, got %d", len(cats))
	}
}

func TestClassify_MultipleMessages(t *testing.T) {
	c := NewConversationClassifier(nil, DefaultConfig())

	messages := []Message{
		{Body: "Hi, I need some help", Role: "texter"},
		{Body: "What's going on?", Role: "counselor"},
		{Body: "I'm feeling really depressed and hopeless lately. I'm also very anxious about everything.", Role: "texter"},
	}

	result, err := c.Classify(context.Background(), "conv-multi", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Category != CategoryGeneralSupport {
		t.Errorf("expected general-support for depression/anxiety, got %s", result.Category)
	}
}

func TestClassify_GriefHigherPriority(t *testing.T) {
	c := NewConversationClassifier(nil, DefaultConfig())

	messages := []Message{
		{Body: "My mother died last week and I can't stop mourning", Role: "texter"},
	}

	result, err := c.Classify(context.Background(), "conv-grief", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Category != CategoryGeneralSupport {
		t.Errorf("expected general-support for grief, got %s", result.Category)
	}
	if result.Priority > 3 {
		t.Errorf("expected priority <= 3 for grief, got %d", result.Priority)
	}
}

func TestClassify_SubstanceReferral(t *testing.T) {
	c := NewConversationClassifier(nil, DefaultConfig())

	messages := []Message{
		{Body: "I need help with my addiction, looking for rehab and recovery options", Role: "texter"},
	}

	result, err := c.Classify(context.Background(), "conv-substance", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Category != CategoryReferral {
		t.Errorf("expected referral for substance abuse, got %s", result.Category)
	}
}
