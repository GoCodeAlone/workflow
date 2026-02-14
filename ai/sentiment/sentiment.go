package sentiment

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// Score represents a sentiment score from -1 (very negative) to 1 (very positive).
type Score float64

const (
	ScoreVeryNegative Score = -1.0
	ScoreNegative     Score = -0.5
	ScoreNeutral      Score = 0.0
	ScorePositive     Score = 0.5
	ScoreVeryPositive Score = 1.0
)

// Label returns a human-readable label for the score.
func (s Score) Label() string {
	switch {
	case s <= -0.6:
		return "very_negative"
	case s <= -0.2:
		return "negative"
	case s <= 0.2:
		return "neutral"
	case s <= 0.6:
		return "positive"
	default:
		return "very_positive"
	}
}

// Analysis represents the sentiment analysis result for a single message or conversation.
type Analysis struct {
	Score      Score     `json:"score"`
	Label      string    `json:"label"`
	Confidence float64   `json:"confidence"`
	Keywords   []string  `json:"keywords,omitempty"`
	AnalyzedAt time.Time `json:"analyzedAt"`
}

// TimelinePoint represents a sentiment measurement at a point in time.
type TimelinePoint struct {
	Timestamp time.Time `json:"timestamp"`
	Score     Score     `json:"score"`
	Label     string    `json:"label"`
	MessageID string    `json:"messageId,omitempty"`
}

// Trend represents a sentiment trend over a conversation.
type Trend struct {
	ConversationID string          `json:"conversationId"`
	Points         []TimelinePoint `json:"points"`
	CurrentScore   Score           `json:"currentScore"`
	AverageScore   Score           `json:"averageScore"`
	Direction      string          `json:"direction"` // "improving", "declining", "stable"
	SharpDrop      bool            `json:"sharpDrop"`
	SharpDropAt    *time.Time      `json:"sharpDropAt,omitempty"`
}

// Message represents a message for sentiment analysis.
type Message struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	Role      string    `json:"role"`
	Timestamp time.Time `json:"timestamp"`
}

// LLMProvider abstracts the AI sentiment analysis backend.
type LLMProvider interface {
	AnalyzeSentiment(ctx context.Context, systemPrompt, userPrompt string) (*Analysis, error)
}

// SentimentAnalyzer performs sentiment analysis on messages.
type SentimentAnalyzer struct {
	provider      LLMProvider
	positiveWords map[string]float64
	negativeWords map[string]float64
	intensifiers  map[string]float64
	negations     []string
}

// NewSentimentAnalyzer creates a new SentimentAnalyzer.
// If provider is nil, only lexicon-based analysis is used.
func NewSentimentAnalyzer(provider LLMProvider) *SentimentAnalyzer {
	return &SentimentAnalyzer{
		provider:      provider,
		positiveWords: defaultPositiveWords(),
		negativeWords: defaultNegativeWords(),
		intensifiers:  defaultIntensifiers(),
		negations:     defaultNegations(),
	}
}

// Analyze performs sentiment analysis on a single text.
func (a *SentimentAnalyzer) Analyze(ctx context.Context, text string) (*Analysis, error) {
	if text == "" {
		return nil, fmt.Errorf("empty text")
	}

	// Try LLM provider first
	if a.provider != nil {
		systemPrompt := buildSentimentSystemPrompt()
		userPrompt := fmt.Sprintf("Analyze the sentiment of this text:\n\n%s", text)
		result, err := a.provider.AnalyzeSentiment(ctx, systemPrompt, userPrompt)
		if err == nil && result != nil {
			return result, nil
		}
	}

	// Lexicon-based fallback
	return a.lexiconAnalysis(text), nil
}

// AnalyzeMessages performs sentiment analysis on multiple messages.
func (a *SentimentAnalyzer) AnalyzeMessages(ctx context.Context, messages []Message) ([]Analysis, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}

	results := make([]Analysis, 0, len(messages))
	for _, msg := range messages {
		if msg.Role != "texter" {
			continue
		}
		analysis, err := a.Analyze(ctx, msg.Body)
		if err != nil {
			continue
		}
		analysis.AnalyzedAt = msg.Timestamp
		results = append(results, *analysis)
	}
	return results, nil
}

// lexiconAnalysis performs keyword-based sentiment analysis.
func (a *SentimentAnalyzer) lexiconAnalysis(text string) *Analysis {
	words := strings.Fields(strings.ToLower(text))

	var totalScore float64
	var matchCount int
	var keywords []string

	for i, word := range words {
		// Clean punctuation
		cleaned := strings.Trim(word, ".,!?;:'\"()")

		// Check for negation before this word
		negated := false
		if i > 0 {
			prevWord := strings.Trim(words[i-1], ".,!?;:'\"()")
			for _, neg := range a.negations {
				if prevWord == neg {
					negated = true
					break
				}
			}
		}

		// Check for intensifier
		intensifier := 1.0
		if i > 0 {
			prevCleaned := strings.Trim(words[i-1], ".,!?;:'\"()")
			if mult, ok := a.intensifiers[prevCleaned]; ok {
				intensifier = mult
			}
		}

		if score, ok := a.positiveWords[cleaned]; ok {
			if negated {
				totalScore -= score * intensifier
			} else {
				totalScore += score * intensifier
			}
			matchCount++
			keywords = append(keywords, cleaned)
		} else if score, ok := a.negativeWords[cleaned]; ok {
			if negated {
				totalScore += score * intensifier // negation flips negative to positive
			} else {
				totalScore -= score * intensifier
			}
			matchCount++
			keywords = append(keywords, cleaned)
		}
	}

	// Normalize to -1 to 1 range
	var normalizedScore Score
	if matchCount > 0 {
		normalizedScore = Score(math.Max(-1, math.Min(1, totalScore/float64(matchCount))))
	}

	// Confidence based on match density
	confidence := 0.3 // base confidence for lexicon
	if len(words) > 0 {
		density := float64(matchCount) / float64(len(words))
		confidence = math.Min(0.8, 0.3+density*2)
	}

	return &Analysis{
		Score:      Score(math.Round(float64(normalizedScore)*100) / 100),
		Label:      normalizedScore.Label(),
		Confidence: math.Round(confidence*100) / 100,
		Keywords:   keywords,
		AnalyzedAt: time.Now().UTC(),
	}
}

// TrendDetector tracks sentiment trends over conversation timelines.
type TrendDetector struct {
	analyzer           *SentimentAnalyzer
	trends             map[string]*Trend
	mu                 sync.RWMutex
	sharpDropThreshold float64
	alertCallback      func(conversationID string, trend *Trend)
}

// TrendConfig holds configuration for the TrendDetector.
type TrendConfig struct {
	SharpDropThreshold float64 // score drop per message that triggers alert (default 0.5)
	AlertCallback      func(conversationID string, trend *Trend)
}

// DefaultTrendConfig returns default trend detection configuration.
func DefaultTrendConfig() TrendConfig {
	return TrendConfig{
		SharpDropThreshold: 0.5,
	}
}

// NewTrendDetector creates a new TrendDetector.
func NewTrendDetector(analyzer *SentimentAnalyzer, cfg TrendConfig) *TrendDetector {
	if cfg.SharpDropThreshold == 0 {
		cfg.SharpDropThreshold = 0.5
	}
	return &TrendDetector{
		analyzer:           analyzer,
		trends:             make(map[string]*Trend),
		sharpDropThreshold: cfg.SharpDropThreshold,
		alertCallback:      cfg.AlertCallback,
	}
}

// TrackConversation analyzes all messages and builds a sentiment timeline.
func (d *TrendDetector) TrackConversation(ctx context.Context, conversationID string, messages []Message) (*Trend, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}

	var points []TimelinePoint
	var totalScore float64
	var count int

	for _, msg := range messages {
		if msg.Role != "texter" {
			continue
		}

		analysis, err := d.analyzer.Analyze(ctx, msg.Body)
		if err != nil {
			continue
		}

		points = append(points, TimelinePoint{
			Timestamp: msg.Timestamp,
			Score:     analysis.Score,
			Label:     analysis.Label,
			MessageID: msg.ID,
		})
		totalScore += float64(analysis.Score)
		count++
	}

	if count == 0 {
		return &Trend{
			ConversationID: conversationID,
			Points:         points,
			CurrentScore:   ScoreNeutral,
			AverageScore:   ScoreNeutral,
			Direction:      "stable",
		}, nil
	}

	trend := &Trend{
		ConversationID: conversationID,
		Points:         points,
		CurrentScore:   points[len(points)-1].Score,
		AverageScore:   Score(math.Round(totalScore/float64(count)*100) / 100),
	}

	// Determine direction from recent trend
	trend.Direction = d.computeDirection(points)

	// Check for sharp drops
	trend.SharpDrop, trend.SharpDropAt = d.detectSharpDrop(points)

	// Store trend
	d.mu.Lock()
	d.trends[conversationID] = trend
	d.mu.Unlock()

	// Fire alert callback on sharp drop
	if trend.SharpDrop && d.alertCallback != nil {
		d.alertCallback(conversationID, trend)
	}

	return trend, nil
}

// GetTrend returns the stored trend for a conversation.
func (d *TrendDetector) GetTrend(conversationID string) (*Trend, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	trend, ok := d.trends[conversationID]
	return trend, ok
}

// computeDirection determines trend direction from the last few points.
func (d *TrendDetector) computeDirection(points []TimelinePoint) string {
	if len(points) < 2 {
		return "stable"
	}

	// Look at last 3 points (or all if fewer)
	start := 0
	if len(points) > 3 {
		start = len(points) - 3
	}
	recent := points[start:]

	first := float64(recent[0].Score)
	last := float64(recent[len(recent)-1].Score)
	diff := last - first

	switch {
	case diff > 0.2:
		return "improving"
	case diff < -0.2:
		return "declining"
	default:
		return "stable"
	}
}

// detectSharpDrop checks for sudden sentiment drops between consecutive messages.
func (d *TrendDetector) detectSharpDrop(points []TimelinePoint) (bool, *time.Time) {
	for i := 1; i < len(points); i++ {
		drop := float64(points[i-1].Score) - float64(points[i].Score)
		if drop >= d.sharpDropThreshold {
			ts := points[i].Timestamp
			return true, &ts
		}
	}
	return false, nil
}

func buildSentimentSystemPrompt() string {
	return `You are a sentiment analysis AI for a crisis text line platform.
Analyze the sentiment of messages and return a JSON object with:
- score: float from -1 (very negative) to 1 (very positive)
- label: one of "very_negative", "negative", "neutral", "positive", "very_positive"
- confidence: float from 0 to 1
- keywords: array of words that influenced the sentiment score`
}

func defaultPositiveWords() map[string]float64 {
	return map[string]float64{
		"good": 0.6, "better": 0.7, "best": 0.8,
		"happy": 0.8, "glad": 0.6, "great": 0.7,
		"wonderful": 0.9, "amazing": 0.9, "love": 0.8,
		"like": 0.4, "hope": 0.6, "hopeful": 0.7,
		"grateful": 0.8, "thankful": 0.7, "thanks": 0.5,
		"helped": 0.6, "helpful": 0.6, "safe": 0.7,
		"calm": 0.6, "relaxed": 0.6, "peaceful": 0.7,
		"strong": 0.6, "confident": 0.6, "brave": 0.7,
		"okay": 0.3, "fine": 0.3, "alright": 0.3,
		"improving": 0.6, "progress": 0.5, "healing": 0.6,
		"support": 0.5, "comfort": 0.5, "smile": 0.6,
	}
}

func defaultNegativeWords() map[string]float64 {
	return map[string]float64{
		"sad": 0.6, "depressed": 0.8, "hopeless": 0.9,
		"anxious": 0.6, "worried": 0.5, "scared": 0.7,
		"angry": 0.7, "furious": 0.9, "hate": 0.8,
		"terrible": 0.8, "awful": 0.8, "horrible": 0.8,
		"worst": 0.9, "bad": 0.5, "painful": 0.7,
		"hurt": 0.7, "suffering": 0.8, "miserable": 0.8,
		"lonely": 0.6, "alone": 0.5, "empty": 0.7,
		"numb": 0.6, "worthless": 0.9, "useless": 0.8,
		"failure": 0.8, "burden": 0.8, "guilty": 0.6,
		"ashamed": 0.7, "afraid": 0.7, "panic": 0.8,
		"overwhelmed": 0.7, "stressed": 0.6, "exhausted": 0.6,
		"crying": 0.5, "tears": 0.5, "broken": 0.8,
		"trapped": 0.8, "stuck": 0.5, "lost": 0.5,
		"suicide": 1.0, "die": 0.9, "kill": 1.0,
		"cutting": 0.9, "harm": 0.8,
	}
}

func defaultIntensifiers() map[string]float64 {
	return map[string]float64{
		"very":       1.5,
		"really":     1.4,
		"extremely":  1.8,
		"incredibly": 1.7,
		"so":         1.3,
		"absolutely": 1.6,
		"completely": 1.5,
		"totally":    1.4,
	}
}

func defaultNegations() []string {
	return []string{
		"not", "no", "never", "neither", "nobody", "nothing",
		"nowhere", "nor", "cannot", "can't", "won't", "don't",
		"doesn't", "didn't", "isn't", "aren't", "wasn't",
	}
}
