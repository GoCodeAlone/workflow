package classifier

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// Category represents a conversation classification category.
type Category string

const (
	CategoryCrisis         Category = "crisis"
	CategoryGeneralSupport Category = "general-support"
	CategoryInformation    Category = "information"
	CategoryReferral       Category = "referral"
)

// AllCategories returns all valid classification categories.
func AllCategories() []Category {
	return []Category{
		CategoryCrisis,
		CategoryGeneralSupport,
		CategoryInformation,
		CategoryReferral,
	}
}

// Classification represents the result of classifying a conversation.
type Classification struct {
	Category     Category  `json:"category"`
	Confidence   float64   `json:"confidence"`
	Priority     int       `json:"priority"` // 1 (highest) to 5 (lowest)
	Subcategory  string    `json:"subcategory,omitempty"`
	Tags         []string  `json:"tags,omitempty"`
	ClassifiedAt time.Time `json:"classifiedAt"`
}

// Message represents a conversation message for classification.
type Message struct {
	Body string `json:"body"`
	Role string `json:"role"`
}

// LLMProvider abstracts the AI classification backend.
type LLMProvider interface {
	Classify(ctx context.Context, systemPrompt, userPrompt string) (*Classification, error)
}

// ConversationClassifier classifies conversations into categories with priority scoring.
type ConversationClassifier struct {
	provider LLMProvider
	rules    []classificationRule
	cache    map[string]*Classification
	cacheMu  sync.RWMutex
	cacheTTL time.Duration
}

// Config holds configuration for the ConversationClassifier.
type Config struct {
	CacheTTL time.Duration
}

// DefaultConfig returns default classifier configuration.
func DefaultConfig() Config {
	return Config{
		CacheTTL: 5 * time.Minute,
	}
}

// NewConversationClassifier creates a new classifier.
// If provider is nil, only rule-based classification is used.
func NewConversationClassifier(provider LLMProvider, cfg Config) *ConversationClassifier {
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 5 * time.Minute
	}
	return &ConversationClassifier{
		provider: provider,
		rules:    defaultRules(),
		cache:    make(map[string]*Classification),
		cacheTTL: cfg.CacheTTL,
	}
}

// Classify classifies a conversation and returns its category with priority.
func (c *ConversationClassifier) Classify(ctx context.Context, conversationID string, messages []Message) (*Classification, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}

	// Check cache
	c.cacheMu.RLock()
	if cached, ok := c.cache[conversationID]; ok {
		c.cacheMu.RUnlock()
		return cached, nil
	}
	c.cacheMu.RUnlock()

	var result *Classification

	// Try LLM provider first
	if c.provider != nil {
		systemPrompt := buildClassificationSystemPrompt()
		userPrompt := buildClassificationUserPrompt(messages)
		llmResult, err := c.provider.Classify(ctx, systemPrompt, userPrompt)
		if err == nil && llmResult != nil {
			result = llmResult
		}
	}

	// Fall back to rule-based classification
	if result == nil {
		result = c.ruleBasedClassification(messages)
	}

	result.ClassifiedAt = time.Now().UTC()

	// Cache the result
	c.cacheMu.Lock()
	c.cache[conversationID] = result
	c.cacheMu.Unlock()

	return result, nil
}

// InvalidateConversation removes the cached classification for a conversation.
func (c *ConversationClassifier) InvalidateConversation(conversationID string) {
	c.cacheMu.Lock()
	delete(c.cache, conversationID)
	c.cacheMu.Unlock()
}

// ClearCache removes all cached classifications.
func (c *ConversationClassifier) ClearCache() {
	c.cacheMu.Lock()
	c.cache = make(map[string]*Classification)
	c.cacheMu.Unlock()
}

// classificationRule defines a keyword-based classification rule.
type classificationRule struct {
	category    Category
	subcategory string
	keywords    []string
	priority    int
	weight      float64
}

// ruleBasedClassification classifies based on keyword matching.
func (c *ConversationClassifier) ruleBasedClassification(messages []Message) *Classification {
	// Build combined text from texter messages
	var combined strings.Builder
	for _, msg := range messages {
		if msg.Role == "texter" || msg.Role == "" {
			combined.WriteString(strings.ToLower(msg.Body))
			combined.WriteString(" ")
		}
	}
	text := combined.String()

	// Score each category
	type categoryScore struct {
		category    Category
		subcategory string
		score       float64
		priority    int
		tags        []string
	}

	scores := make(map[Category]*categoryScore)
	for cat := range map[Category]bool{CategoryCrisis: true, CategoryGeneralSupport: true, CategoryInformation: true, CategoryReferral: true} {
		scores[cat] = &categoryScore{category: cat, priority: 5}
	}

	for _, rule := range c.rules {
		matchCount := 0
		for _, kw := range rule.keywords {
			if strings.Contains(text, kw) {
				matchCount++
			}
		}
		if matchCount > 0 {
			score := rule.weight * float64(matchCount)
			cs := scores[rule.category]
			cs.score += score
			if rule.priority < cs.priority {
				cs.priority = rule.priority
			}
			if rule.subcategory != "" {
				cs.subcategory = rule.subcategory
				cs.tags = append(cs.tags, rule.subcategory)
			}
		}
	}

	// Find highest scoring category
	var best *categoryScore
	for _, cs := range scores {
		if best == nil || cs.score > best.score {
			best = cs
		}
	}

	// Default to general-support if no matches
	if best == nil || best.score == 0 {
		return &Classification{
			Category:   CategoryGeneralSupport,
			Confidence: 0.3,
			Priority:   4,
		}
	}

	// Normalize confidence to 0-1 range using sigmoid
	confidence := sigmoid(best.score)

	return &Classification{
		Category:    best.category,
		Confidence:  math.Round(confidence*100) / 100,
		Priority:    best.priority,
		Subcategory: best.subcategory,
		Tags:        best.tags,
	}
}

func sigmoid(x float64) float64 {
	return 1.0 / (1.0 + math.Exp(-x+2))
}

func buildClassificationSystemPrompt() string {
	return `You are a conversation classification AI for a crisis text line platform.
Classify each conversation into one of these categories:
- crisis: Active crisis situations requiring immediate intervention (suicide, self-harm, abuse)
- general-support: Emotional support conversations (anxiety, depression, stress, relationships)
- information: Requests for information or resources
- referral: Conversations that need referral to specialized services

Also assign a priority score from 1-5:
1 = Critical/Immediate (active crisis, imminent danger)
2 = High (severe distress, recent crisis)
3 = Medium (moderate distress, ongoing issues)
4 = Low (mild concerns, general questions)
5 = Minimal (information-only, follow-up)

Return a JSON object with: category, confidence (0-1), priority (1-5), subcategory, tags (array).`
}

func buildClassificationUserPrompt(messages []Message) string {
	var b strings.Builder
	b.WriteString("Classify this conversation:\n\n")
	for _, msg := range messages {
		role := msg.Role
		if role == "" {
			role = "unknown"
		}
		fmt.Fprintf(&b, "[%s]: %s\n", role, msg.Body)
	}
	return b.String()
}

func defaultRules() []classificationRule {
	return []classificationRule{
		// Crisis rules (highest priority)
		{
			category:    CategoryCrisis,
			subcategory: "suicidal-ideation",
			keywords:    []string{"kill myself", "suicide", "end my life", "want to die", "better off dead", "no reason to live"},
			priority:    1,
			weight:      5.0,
		},
		{
			category:    CategoryCrisis,
			subcategory: "self-harm",
			keywords:    []string{"cut myself", "cutting myself", "hurt myself", "self-harm", "burning myself", "hitting myself"},
			priority:    1,
			weight:      4.5,
		},
		{
			category:    CategoryCrisis,
			subcategory: "immediate-danger",
			keywords:    []string{"right now", "tonight", "plan to", "going to do it", "goodbye", "final", "overdose"},
			priority:    1,
			weight:      5.0,
		},
		{
			category:    CategoryCrisis,
			subcategory: "abuse",
			keywords:    []string{"hits me", "abuses me", "beats me", "assault", "abused"},
			priority:    1,
			weight:      4.0,
		},

		// General support rules
		{
			category:    CategoryGeneralSupport,
			subcategory: "anxiety",
			keywords:    []string{"anxious", "worried", "panic", "nervous", "fear", "scared", "anxiety"},
			priority:    3,
			weight:      2.0,
		},
		{
			category:    CategoryGeneralSupport,
			subcategory: "depression",
			keywords:    []string{"sad", "hopeless", "depressed", "empty", "numb", "tired of everything"},
			priority:    3,
			weight:      2.0,
		},
		{
			category:    CategoryGeneralSupport,
			subcategory: "relationships",
			keywords:    []string{"friend", "family", "partner", "boyfriend", "girlfriend", "breakup", "lonely"},
			priority:    3,
			weight:      1.5,
		},
		{
			category:    CategoryGeneralSupport,
			subcategory: "stress",
			keywords:    []string{"stress", "overwhelmed", "pressure", "can't cope", "too much"},
			priority:    3,
			weight:      1.5,
		},
		{
			category:    CategoryGeneralSupport,
			subcategory: "grief",
			keywords:    []string{"died", "lost someone", "death", "grief", "mourning", "funeral"},
			priority:    2,
			weight:      2.5,
		},

		// Information rules
		{
			category:    CategoryInformation,
			subcategory: "resources",
			keywords:    []string{"where can i", "how do i", "resources", "hotline", "help line", "website"},
			priority:    4,
			weight:      1.0,
		},
		{
			category:    CategoryInformation,
			subcategory: "services",
			keywords:    []string{"what services", "do you offer", "hours", "available"},
			priority:    5,
			weight:      0.8,
		},

		// Referral rules
		{
			category:    CategoryReferral,
			subcategory: "substance-abuse",
			keywords:    []string{"addiction", "rehab", "substance", "recovery", "sober", "relapse"},
			priority:    2,
			weight:      3.0,
		},
		{
			category:    CategoryReferral,
			subcategory: "legal",
			keywords:    []string{"lawyer", "legal", "court", "police report", "restraining order"},
			priority:    3,
			weight:      2.0,
		},
		{
			category:    CategoryReferral,
			subcategory: "medical",
			keywords:    []string{"doctor", "hospital", "medication", "prescription", "therapist", "psychiatrist"},
			priority:    3,
			weight:      2.0,
		},
	}
}
