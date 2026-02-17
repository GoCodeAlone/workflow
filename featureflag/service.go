package featureflag

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
)

// Service is a caching proxy that sits between consumers and a Provider.
// It adds an in-memory cache, SSE broadcasting, and audit logging.
type Service struct {
	provider Provider
	cache    *FlagCache
	logger   *slog.Logger

	// SSE broadcaster
	mu          sync.RWMutex
	subscribers map[uint64]chan FlagChangeEvent
	nextID      atomic.Uint64
}

// NewService creates a Service wrapping the given provider and cache.
func NewService(provider Provider, cache *FlagCache, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Service{
		provider:    provider,
		cache:       cache,
		logger:      logger,
		subscribers: make(map[uint64]chan FlagChangeEvent),
	}

	// Forward provider change events to the SSE broadcaster and invalidate cache.
	provider.Subscribe(func(evt FlagChangeEvent) {
		s.cache.InvalidateFlag(evt.Key)
		s.broadcast(evt)
		s.logger.Info("flag changed",
			"key", evt.Key,
			"source", evt.Source,
		)
	})

	return s
}

// Evaluate returns the flag value for the given key. Cache is checked first;
// on a miss the provider is queried and the result is cached.
func (s *Service) Evaluate(ctx context.Context, key string, evalCtx EvaluationContext) (FlagValue, error) {
	if v, ok := s.cache.Get(key, evalCtx.UserKey); ok {
		s.logger.Debug("cache hit", "key", key, "user", evalCtx.UserKey)
		return v, nil
	}

	val, err := s.provider.Evaluate(ctx, key, evalCtx)
	if err != nil {
		return FlagValue{}, err
	}

	s.cache.Set(key, evalCtx.UserKey, val)
	s.logger.Debug("cache miss, fetched from provider", "key", key, "user", evalCtx.UserKey)
	return val, nil
}

// AllFlags delegates to the provider (cache is per-key, so we don't cache AllFlags).
func (s *Service) AllFlags(ctx context.Context, evalCtx EvaluationContext) ([]FlagValue, error) {
	return s.provider.AllFlags(ctx, evalCtx)
}

// broadcast sends a change event to all SSE subscribers (non-blocking).
func (s *Service) broadcast(evt FlagChangeEvent) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for id, ch := range s.subscribers {
		select {
		case ch <- evt:
		default:
			s.logger.Warn("SSE flag event dropped for slow subscriber", "subscriber_id", id, "key", evt.Key)
		}
	}
}

// subscribe registers a new SSE subscriber. Returns the channel and an unsubscribe function.
func (s *Service) subscribe() (<-chan FlagChangeEvent, func()) {
	ch := make(chan FlagChangeEvent, 64)
	id := s.nextID.Add(1)

	s.mu.Lock()
	s.subscribers[id] = ch
	s.mu.Unlock()

	unsub := sync.Once{}
	return ch, func() {
		unsub.Do(func() {
			s.mu.Lock()
			delete(s.subscribers, id)
			s.mu.Unlock()
			close(ch)
		})
	}
}

// SSEHandler returns an HTTP handler that streams flag change events to clients.
// Events are formatted as:
//
//	event: flag.updated
//	data: {"key":"my-flag","value":true,"type":"boolean","source":"generic"}
func (s *Service) SSEHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		ch, unsubscribe := s.subscribe()
		defer unsubscribe()

		ctx := r.Context()

		s.logger.Info("SSE flag client connected", "remote_addr", r.RemoteAddr)

		for {
			select {
			case <-ctx.Done():
				s.logger.Info("SSE flag client disconnected", "remote_addr", r.RemoteAddr)
				return
			case evt, open := <-ch:
				if !open {
					return
				}
				data, err := json.Marshal(evt)
				if err != nil {
					s.logger.Error("failed to marshal SSE flag event", "error", err)
					continue
				}
				fmt.Fprintf(w, "event: flag.updated\ndata: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}

// SubscriberCount returns the number of active SSE subscribers.
func (s *Service) SubscriberCount() int {
	s.mu.RLock()
	n := len(s.subscribers)
	s.mu.RUnlock()
	return n
}
