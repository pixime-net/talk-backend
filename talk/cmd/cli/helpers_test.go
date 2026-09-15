package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/pixime-net/talk/internal/domain"
	"github.com/pixime-net/talk/internal/mcp"
)

// spyPrinter captures all output for assertions in tests.
type spyPrinter struct {
	out strings.Builder
	err strings.Builder
}

func (p *spyPrinter) Printf(format string, args ...any) { fmt.Fprintf(&p.out, format, args...) }
func (p *spyPrinter) Println(args ...any)               { fmt.Fprintln(&p.out, args...) }
func (p *spyPrinter) Errorf(format string, args ...any) { fmt.Fprintf(&p.err, format, args...) }
func (p *spyPrinter) Output() string                    { return p.out.String() }
func (p *spyPrinter) ErrOutput() string                 { return p.err.String() }
func (p *spyPrinter) Reset()                            { p.out.Reset(); p.err.Reset() }

// scriptReader implements Reader with pre-scripted answers.
type scriptReader struct {
	answers []string
	idx     int
}

func newScriptReader(answers ...string) *scriptReader {
	return &scriptReader{answers: answers}
}

func (s *scriptReader) ReadLine(_ string) (string, error) {
	if s.idx >= len(s.answers) {
		return "", fmt.Errorf("no more scripted answers")
	}
	ans := s.answers[s.idx]
	s.idx++
	return ans, nil
}

func (s *scriptReader) ReadLineRaw(prompt string) (string, error) {
	return s.ReadLine(prompt)
}

// stubPromptProvider returns a fixed system prompt or error.
type stubPromptProvider struct {
	text string
	err  error
}

func (s *stubPromptProvider) SystemPrompt(_ context.Context) (string, error) {
	return s.text, s.err
}

// fakeStore implements domain.MessageStore with minimal in-memory state.
type fakeStore struct {
	messages map[string][]domain.Message
}

func newFakeStore() *fakeStore {
	return &fakeStore{messages: make(map[string][]domain.Message)}
}

func (s *fakeStore) AddMessage(_ context.Context, msg domain.Message, scope domain.SessionScope) error {
	s.messages[scope.SessionID()] = append(s.messages[scope.SessionID()], msg)
	return nil
}
func (s *fakeStore) AllMessages(_ context.Context, sessionID string) ([]domain.Message, error) {
	return s.messages[sessionID], nil
}
func (s *fakeStore) ClearMessages(_ context.Context, sessionID string) error {
	delete(s.messages, sessionID)
	return nil
}

// fakeSessionBrowser implements domain.SessionBrowser.
type fakeSessionBrowser struct {
	sessions  []domain.SessionSummary
	turns     map[string][]domain.HistoryTurn
	deleteErr error
	listErr   error
	loadErr   error
	deleted   []string
}

func newFakeSessionBrowser() *fakeSessionBrowser {
	return &fakeSessionBrowser{
		turns: make(map[string][]domain.HistoryTurn),
	}
}

func (s *fakeSessionBrowser) ListSessions(_ context.Context, _ string) ([]domain.SessionSummary, error) {
	return s.sessions, s.listErr
}

func (s *fakeSessionBrowser) LoadHistoryTurnsFromSession(_ context.Context, id string) ([]domain.HistoryTurn, error) {
	return s.turns[id], s.loadErr
}

func (s *fakeSessionBrowser) DeleteSession(_ context.Context, id string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.deleted = append(s.deleted, id)
	for i, sess := range s.sessions {
		if sess.ID == id {
			s.sessions = append(s.sessions[:i], s.sessions[i+1:]...)
			break
		}
	}
	return nil
}

// fakeRegistry implements mcp.Registry for testing.
type fakeRegistry struct {
	servers []mcp.ServerConfig
	addErr  error
	rmErr   error
}

func (r *fakeRegistry) Add(_ context.Context, cfg mcp.ServerConfig) error {
	if r.addErr != nil {
		return r.addErr
	}
	r.servers = append(r.servers, cfg)
	return nil
}

func (r *fakeRegistry) Remove(_ context.Context, id string) error {
	if r.rmErr != nil {
		return r.rmErr
	}
	for i, s := range r.servers {
		if s.ID == id {
			r.servers = append(r.servers[:i], r.servers[i+1:]...)
			return nil
		}
	}
	return nil
}

func (r *fakeRegistry) Get(_ context.Context, id string) (mcp.ServerConfig, error) {
	for _, s := range r.servers {
		if s.ID == id {
			return s, nil
		}
	}
	return mcp.ServerConfig{}, fmt.Errorf("not found")
}

func (r *fakeRegistry) List(_ context.Context) ([]mcp.ServerConfig, error) {
	return r.servers, nil
}

// fakeRouter implements ModelSwitcher for testing.
type fakeRouter struct {
	client domain.LlmClient
	err    error
}

func (r *fakeRouter) Get(_ string) (domain.LlmClient, error) {
	return r.client, r.err
}

// fakeLlmClient implements domain.LlmClient as a no-op.
type fakeLlmClient struct{}

func (fakeLlmClient) Complete(_ context.Context, _ string, _ []domain.Message, _ []domain.Tool, _ domain.CompletionOptions) (*domain.Message, domain.Usage, error) {
	return &domain.Message{Role: "assistant", Content: "fake"}, domain.Usage{}, nil
}

// newTestApp creates an App with a spyPrinter and given options for testing.
func newTestApp(p *spyPrinter) *App {
	return &App{
		Printer:        p,
		Scope:          domain.NewSessionScope("test-session-id", "test-user"),
		Messages:       newFakeStore(),
		Sessions:       newFakeSessionBrowser(),
		PromptProvider: &stubPromptProvider{text: "You are a helpful assistant."},
		CurrentModel:   "sonnet-4.6",
		Reader:         newScriptReader(), // empty reader by default
		Router:         &fakeRouter{client: fakeLlmClient{}},
	}
}
