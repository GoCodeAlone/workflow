package suggestions

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Message represents a single conversation message for generating suggestions.
type Message struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	Role      string    `json:"role"` // "counselor", "texter", "system"
	Timestamp time.Time `json:"timestamp"`
}

// Suggestion represents a response suggestion for a counselor.
type Suggestion struct {
	Text       string  `json:"text"`
	Category   string  `json:"category"` // "empathy", "question", "resource", "safety", "reframe"
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning,omitempty"`
}

// Template defines a fallback response template.
type Template struct {
	Category string   `json:"category"`
	Patterns []string `json:"patterns"` // keywords that trigger this template
	Texts    []string `json:"texts"`    // response options
}

// LLMProvider abstracts the AI generation backend for suggestions.
type LLMProvider interface {
	GenerateSuggestions(ctx context.Context, systemPrompt, userPrompt string) ([]Suggestion, error)
}

// SuggestionEngine generates response suggestions for conversations.
type SuggestionEngine struct {
	provider  LLMProvider
	templates []Template
	cache     map[string]cachedSuggestion
	cacheMu   sync.RWMutex
	cacheTTL  time.Duration
}

type cachedSuggestion struct {
	suggestions []Suggestion
	expiresAt   time.Time
}

// Config holds configuration for the SuggestionEngine.
type Config struct {
	CacheTTL  time.Duration
	Templates []Template
}

// DefaultConfig returns a SuggestionEngine config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		CacheTTL:  2 * time.Minute,
		Templates: defaultTemplates(),
	}
}

// NewSuggestionEngine creates a new SuggestionEngine.
// If provider is nil, only template-based fallback suggestions are available.
func NewSuggestionEngine(provider LLMProvider, cfg Config) *SuggestionEngine {
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 2 * time.Minute
	}
	templates := cfg.Templates
	if len(templates) == 0 {
		templates = defaultTemplates()
	}
	return &SuggestionEngine{
		provider:  provider,
		templates: templates,
		cache:     make(map[string]cachedSuggestion),
		cacheTTL:  cfg.CacheTTL,
	}
}

// GetSuggestions returns response suggestions for a conversation.
// If the LLM provider is unavailable, it falls back to template-based suggestions.
func (e *SuggestionEngine) GetSuggestions(ctx context.Context, conversationID string, messages []Message) ([]Suggestion, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}

	// Check cache
	e.cacheMu.RLock()
	if cached, ok := e.cache[conversationID]; ok && time.Now().Before(cached.expiresAt) {
		e.cacheMu.RUnlock()
		return cached.suggestions, nil
	}
	e.cacheMu.RUnlock()

	var suggestions []Suggestion

	// Try LLM provider first
	if e.provider != nil {
		systemPrompt := buildSystemPrompt()
		userPrompt := buildUserPrompt(messages)
		llmSuggestions, err := e.provider.GenerateSuggestions(ctx, systemPrompt, userPrompt)
		if err == nil && len(llmSuggestions) > 0 {
			suggestions = llmSuggestions
		}
		// Fall through to templates on error
	}

	// Fallback to templates if no LLM suggestions
	if len(suggestions) == 0 {
		suggestions = e.templateFallback(messages)
	}

	// Cache the result
	e.cacheMu.Lock()
	e.cache[conversationID] = cachedSuggestion{
		suggestions: suggestions,
		expiresAt:   time.Now().Add(e.cacheTTL),
	}
	e.cacheMu.Unlock()

	return suggestions, nil
}

// ClearCache removes all cached suggestions.
func (e *SuggestionEngine) ClearCache() {
	e.cacheMu.Lock()
	e.cache = make(map[string]cachedSuggestion)
	e.cacheMu.Unlock()
}

// InvalidateConversation removes cached suggestions for a specific conversation.
func (e *SuggestionEngine) InvalidateConversation(conversationID string) {
	e.cacheMu.Lock()
	delete(e.cache, conversationID)
	e.cacheMu.Unlock()
}

// templateFallback generates suggestions based on keyword matching against templates.
func (e *SuggestionEngine) templateFallback(messages []Message) []Suggestion {
	// Get the most recent texter message
	var lastTexterMsg string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "texter" {
			lastTexterMsg = strings.ToLower(messages[i].Body)
			break
		}
	}

	if lastTexterMsg == "" {
		return []Suggestion{
			{
				Text:       "How are you feeling right now?",
				Category:   "question",
				Confidence: 0.5,
			},
		}
	}

	var suggestions []Suggestion
	for _, tmpl := range e.templates {
		for _, pattern := range tmpl.Patterns {
			if strings.Contains(lastTexterMsg, pattern) {
				for _, text := range tmpl.Texts {
					suggestions = append(suggestions, Suggestion{
						Text:       text,
						Category:   tmpl.Category,
						Confidence: 0.6,
					})
				}
				break // only match first pattern per template
			}
		}
	}

	// Always include a generic empathy response
	if len(suggestions) == 0 {
		suggestions = append(suggestions, Suggestion{
			Text:       "It sounds like you're going through a lot. Can you tell me more about what you're experiencing?",
			Category:   "empathy",
			Confidence: 0.4,
		})
	}

	// Limit to top 5 suggestions
	if len(suggestions) > 5 {
		suggestions = suggestions[:5]
	}

	return suggestions
}

func buildSystemPrompt() string {
	return `You are an AI assistant helping crisis counselors generate response suggestions.
Generate 3-5 response suggestions for the counselor based on the conversation.
Each suggestion should be empathetic, appropriate for a crisis text line context, and follow best practices.

Categories for suggestions:
- empathy: Reflective listening and validation
- question: Open-ended exploratory questions
- resource: Sharing helpful resources or coping strategies
- safety: Safety planning and risk assessment
- reframe: Helping reframe the situation positively

Return suggestions as a JSON array with fields: text, category, confidence (0-1), reasoning.`
}

func buildUserPrompt(messages []Message) string {
	var b strings.Builder
	b.WriteString("Conversation transcript:\n\n")
	for _, msg := range messages {
		role := msg.Role
		switch role {
		case "texter":
			role = "Texter"
		case "counselor":
			role = "Counselor"
		}
		fmt.Fprintf(&b, "[%s] %s: %s\n", msg.Timestamp.Format("15:04"), role, msg.Body)
	}
	b.WriteString("\nGenerate response suggestions for the counselor.")
	return b.String()
}

func defaultTemplates() []Template {
	return []Template{
		{
			Category: "safety",
			Patterns: []string{"kill myself", "suicide", "end my life", "want to die", "not alive"},
			Texts: []string{
				"I'm really glad you shared that with me. Are you thinking about suicide right now?",
				"That sounds incredibly painful. I want to make sure you're safe. Do you have a plan?",
				"Thank you for trusting me with that. Your safety is my priority. Can we talk about what's happening?",
			},
		},
		{
			Category: "safety",
			Patterns: []string{"hurt myself", "self-harm", "cutting", "burning myself"},
			Texts: []string{
				"I hear you. Are you safe right now?",
				"Thank you for telling me. Can you tell me more about when you feel the urge to hurt yourself?",
			},
		},
		{
			Category: "empathy",
			Patterns: []string{"sad", "depressed", "hopeless", "empty", "numb"},
			Texts: []string{
				"It sounds like you're carrying a lot of heavy feelings right now.",
				"That must be really difficult. How long have you been feeling this way?",
			},
		},
		{
			Category: "empathy",
			Patterns: []string{"anxious", "worried", "panic", "scared", "afraid"},
			Texts: []string{
				"It sounds like anxiety is really weighing on you. What's been triggering these feelings?",
				"That sounds overwhelming. Let's take this one step at a time.",
			},
		},
		{
			Category: "question",
			Patterns: []string{"don't know", "confused", "not sure", "unsure"},
			Texts: []string{
				"That's okay. Can you describe what you're feeling right now, even if it's hard to put into words?",
				"Sometimes it's hard to sort through everything. What feels most pressing right now?",
			},
		},
		{
			Category: "resource",
			Patterns: []string{"can't sleep", "insomnia", "nightmares"},
			Texts: []string{
				"Sleep issues can really impact everything else. Have you tried any relaxation techniques before bed?",
				"That sounds exhausting. Would it help to talk about some strategies that others have found helpful?",
			},
		},
		{
			Category: "reframe",
			Patterns: []string{"failure", "worthless", "useless", "stupid", "burden"},
			Texts: []string{
				"I hear that you're being really hard on yourself. What would you say to a friend who felt this way?",
				"It takes courage to reach out. That tells me you're stronger than you might feel right now.",
			},
		},
	}
}
