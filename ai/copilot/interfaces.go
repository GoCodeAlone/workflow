package copilotai

import (
	"context"

	copilot "github.com/github/copilot-sdk/go"
)

// SessionWrapper wraps the methods we use from copilot.Session so they can be mocked.
type SessionWrapper interface {
	SendAndWait(ctx context.Context, opts copilot.MessageOptions) (*copilot.SessionEvent, error)
	Destroy() error
}

// ClientWrapper wraps the methods we use from copilot.Client so they can be mocked.
type ClientWrapper interface {
	CreateSession(ctx context.Context, cfg *copilot.SessionConfig) (SessionWrapper, error)
}

// realClientWrapper delegates to a real copilot.Client.
type realClientWrapper struct {
	cli *copilot.Client
}

func (w *realClientWrapper) CreateSession(ctx context.Context, cfg *copilot.SessionConfig) (SessionWrapper, error) {
	sess, err := w.cli.CreateSession(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &realSessionWrapper{sess: sess}, nil
}

// realSessionWrapper delegates to a real copilot.Session.
type realSessionWrapper struct {
	sess *copilot.Session
}

func (w *realSessionWrapper) SendAndWait(ctx context.Context, opts copilot.MessageOptions) (*copilot.SessionEvent, error) {
	return w.sess.SendAndWait(ctx, opts)
}

func (w *realSessionWrapper) Destroy() error {
	return w.sess.Destroy()
}
