package lsp

import (
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	glspserver "github.com/tliron/glsp/server"
)

// Version is set at build time.
var Version = "dev"

// Server is the workflow LSP server.
type Server struct {
	registry *Registry
	store    *DocumentStore
	handler  protocol.Handler
	server   *glspserver.Server
}

// NewServer creates a new LSP server with all handlers registered.
func NewServer() *Server {
	s := &Server{
		registry: NewRegistry(),
		store:    NewDocumentStore(),
	}
	s.handler = protocol.Handler{
		Initialize:            s.initialize,
		Initialized:           s.initialized,
		Shutdown:              s.shutdown,
		TextDocumentDidOpen:   s.didOpen,
		TextDocumentDidChange: s.didChange,
		TextDocumentDidSave:   s.didSave,
		TextDocumentCompletion: s.completion,
		TextDocumentHover:     s.hover,
	}
	s.server = glspserver.NewServer(&s.handler, "workflow-lsp", false)
	return s
}

// RunStdio starts the LSP server over stdio (blocking).
func (s *Server) RunStdio() error {
	return s.server.RunStdio()
}

// initialize handles the LSP initialize request.
func (s *Server) initialize(_ *glsp.Context, params *protocol.InitializeParams) (any, error) {
	_ = params
	capabilities := s.handler.CreateServerCapabilities()

	syncKind := protocol.TextDocumentSyncKindFull
	capabilities.TextDocumentSync = &protocol.TextDocumentSyncOptions{
		OpenClose: boolPtr(true),
		Change:    &syncKind,
		Save:      boolPtr(true),
	}

	return protocol.InitializeResult{
		Capabilities: capabilities,
		ServerInfo: &protocol.InitializeResultServerInfo{
			Name:    "workflow-lsp-server",
			Version: &Version,
		},
	}, nil
}

// initialized handles the initialized notification.
func (s *Server) initialized(_ *glsp.Context, _ *protocol.InitializedParams) error {
	return nil
}

// shutdown handles the shutdown request.
func (s *Server) shutdown(_ *glsp.Context) error {
	return nil
}

// didOpen handles textDocument/didOpen.
func (s *Server) didOpen(ctx *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	doc := s.store.Set(string(params.TextDocument.URI), params.TextDocument.Text)
	s.publishDiagnostics(ctx, string(params.TextDocument.URI), doc)
	return nil
}

// didChange handles textDocument/didChange.
func (s *Server) didChange(ctx *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
	if len(params.ContentChanges) == 0 {
		return nil
	}
	// We use full sync — take the last change.
	var content string
	for _, change := range params.ContentChanges {
		if c, ok := change.(protocol.TextDocumentContentChangeEventWhole); ok {
			content = c.Text
		}
	}
	doc := s.store.Set(string(params.TextDocument.URI), content)
	s.publishDiagnostics(ctx, string(params.TextDocument.URI), doc)
	return nil
}

// didSave handles textDocument/didSave.
func (s *Server) didSave(ctx *glsp.Context, params *protocol.DidSaveTextDocumentParams) error {
	doc := s.store.Get(string(params.TextDocument.URI))
	if doc != nil {
		s.publishDiagnostics(ctx, string(params.TextDocument.URI), doc)
	}
	return nil
}

// completion handles textDocument/completion.
func (s *Server) completion(_ *glsp.Context, params *protocol.CompletionParams) (any, error) {
	uri := string(params.TextDocument.URI)
	doc := s.store.Get(uri)
	if doc == nil {
		return nil, nil
	}
	line := int(params.Position.Line)
	char := int(params.Position.Character)
	ctx := ContextAt(doc.Content, line, char)
	items := Completions(s.registry, doc, ctx)
	return items, nil
}

// hover handles textDocument/hover.
func (s *Server) hover(_ *glsp.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	uri := string(params.TextDocument.URI)
	doc := s.store.Get(uri)
	if doc == nil {
		return nil, nil
	}
	line := int(params.Position.Line)
	char := int(params.Position.Character)
	ctx := ContextAt(doc.Content, line, char)
	return Hover(s.registry, doc, ctx), nil
}

// publishDiagnostics sends textDocument/publishDiagnostics notification to the client.
func (s *Server) publishDiagnostics(ctx *glsp.Context, uri string, doc *Document) {
	if doc == nil {
		return
	}
	diags := Diagnostics(s.registry, doc)
	params := protocol.PublishDiagnosticsParams{
		URI:         protocol.DocumentUri(uri),
		Diagnostics: diags,
	}
	if ctx.Notify != nil {
		ctx.Notify(string(protocol.ServerTextDocumentPublishDiagnostics), params)
	}
}

func boolPtr(v bool) *bool { return &v }
