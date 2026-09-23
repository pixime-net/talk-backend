package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/google/uuid"
	"github.com/pixime-net/talk/internal/domain"
)

// Handler handles AG-UI protocol HTTP requests.
type Handler struct {
	log             *slog.Logger
	chatFn          ChatFunc
	supportedModels []string
}

// ChatFunc is the function signature for processing a conversation turn.
// It receives the thread ID, model alias, user messages, and chat options
// containing the SSE writer for event emission via the pipeline.
type ChatFunc func(ctx context.Context, threadID string, modelAlias string, messages []types.Message, opts ChatOptions) error

// ChatOptions carries per-request options for the chat function.
type ChatOptions struct {
	SSEWriter      *SSEWriter
	ThinkingEffort domain.ThinkingEffort
}

// NewHandler creates an AG-UI protocol handler.
// supportedModels lists valid model aliases for error messages.
// If chatFn is nil, a placeholder response is used.
func NewHandler(log *slog.Logger, chatFn ChatFunc, supportedModels []string) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{log: log, chatFn: chatFn, supportedModels: supportedModels}
}

// ServeHTTP handles POST /agent requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	input, ok := h.parseRequest(w, r)
	if !ok {
		return
	}

	if h.handleResume(w, r, &input) {
		return
	}

	modelAlias, thinkingEffort, ok := h.resolveForwardedProps(w, r, input)
	if !ok {
		return
	}

	h.streamChat(w, r, input, modelAlias, thinkingEffort)
}

// parseRequest decodes the JSON body and validates that either messages or
// resume entries are present.
func (h *Handler) parseRequest(w http.ResponseWriter, r *http.Request) (types.RunAgentInput, bool) {
	var input types.RunAgentInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return input, false
	}

	if len(input.Messages) == 0 && len(input.Resume) == 0 {
		http.Error(w, `{"error":"messages field is required"}`, http.StatusBadRequest)
		return input, false
	}

	return input, true
}

// handleResume validates and routes a resume request. It mutates the input in
// place (appending the continuation prompt for resolved resumes) and returns
// done=true when the response has already been written (cancelled with no new
// user message) or when an error response was sent.
func (h *Handler) handleResume(w http.ResponseWriter, r *http.Request, input *types.RunAgentInput) (done bool) {
	if len(input.Resume) == 0 {
		return false
	}

	if input.ThreadID == "" {
		http.Error(w, `{"error":"threadId is required when resuming an interrupt"}`, http.StatusBadRequest)
		return true
	}

	hasResolved, hasCancelled, valid := classifyResumeStatus(input.Resume)
	if !valid {
		http.Error(w, `{"error":"unknown resume status, expected resolved or cancelled"}`, http.StatusBadRequest)
		return true
	}
	if hasResolved && hasCancelled {
		http.Error(w, `{"error":"conflicting resume statuses: cannot mix resolved and cancelled"}`, http.StatusBadRequest)
		return true
	}

	if hasCancelled {
		return h.handleCancelledResume(w, r, input)
	}

	// Resolved: append a continuation prompt so the model continues from where it left off.
	input.Messages = append(input.Messages, types.Message{
		Role:    "user",
		Content: "Please continue where you left off.",
	})
	return false
}

// handleCancelledResume handles the cancelled resume case. When the user has
// not sent a new message, it emits an empty run and signals done. When a new
// user message is present, it falls through to normal chat processing.
func (h *Handler) handleCancelledResume(w http.ResponseWriter, r *http.Request, input *types.RunAgentInput) (done bool) {
	if lastMessageIsUser(input.Messages) {
		return false
	}

	sse, err := NewSSEWriter(w, h.log)
	if err != nil {
		http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
		return true
	}

	runID := input.RunID
	if runID == "" {
		runID = uuid.New().String()
	}
	_ = sse.WriteEvent(r.Context(), events.NewRunStartedEvent(input.ThreadID, runID))
	_ = sse.WriteEvent(r.Context(), events.NewRunFinishedEvent(input.ThreadID, runID))
	return true
}

// resolveForwardedProps extracts and validates all values from forwardedProps.
// modelAlias is required; an SSE RUN_ERROR is written and ok=false is returned
// if it is missing or unsupported. thinkingEffort is optional and defaults to off.
func (h *Handler) resolveForwardedProps(w http.ResponseWriter, r *http.Request, input types.RunAgentInput) (modelAlias string, thinkingEffort domain.ThinkingEffort, ok bool) {
	modelAlias, err := extractModelAlias(input.ForwardedProps)
	if err != nil {
		h.writeSSEError(w, r, fmt.Sprintf("%s Available models: %s.", err.Error(), strings.Join(h.supportedModels, ", ")))
		return "", "", false
	}

	if !h.isModelSupported(modelAlias) {
		h.writeSSEError(w, r, fmt.Sprintf("Unknown model %q. Available models: %s.", modelAlias, strings.Join(h.supportedModels, ", ")))
		return "", "", false
	}

	return modelAlias, extractThinkingEffort(input.ForwardedProps), true
}

// streamChat opens an SSE stream and runs the full chat lifecycle:
// RUN_STARTED → chatFn → RUN_FINISHED (or interrupt/error event).
func (h *Handler) streamChat(w http.ResponseWriter, r *http.Request, input types.RunAgentInput, modelAlias string, thinkingEffort domain.ThinkingEffort) {
	sse, err := NewSSEWriter(w, h.log)
	if err != nil {
		http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	threadID := input.ThreadID
	if threadID == "" {
		threadID = uuid.New().String()
	}
	runID := input.RunID
	if runID == "" {
		runID = uuid.New().String()
	}

	ctx := r.Context()

	if err := sse.WriteEvent(ctx, events.NewRunStartedEvent(threadID, runID)); err != nil {
		h.log.Error("writing RUN_STARTED", slog.String("error", err.Error()))
		return
	}

	if h.chatFn != nil {
		if h.runChat(ctx, sse, input, threadID, runID, modelAlias, thinkingEffort) {
			return
		}
	}

	if ctx.Err() != nil {
		h.log.Debug("client disconnected before RUN_FINISHED", slog.String("thread_id", threadID))
		return
	}
	if err := sse.WriteEvent(ctx, events.NewRunFinishedEvent(threadID, runID)); err != nil {
		h.log.Error("writing RUN_FINISHED", slog.String("error", err.Error()))
	}
}

// runChat calls chatFn and emits the appropriate terminal SSE event on error.
// Returns done=true when a terminal event has already been written.
func (h *Handler) runChat(ctx context.Context, sse *SSEWriter, input types.RunAgentInput, threadID, runID, modelAlias string, thinkingEffort domain.ThinkingEffort) (done bool) {
	err := h.chatFn(ctx, threadID, modelAlias, input.Messages, ChatOptions{
		SSEWriter:      sse,
		ThinkingEffort: thinkingEffort,
	})

	if ctx.Err() != nil {
		h.log.Debug("client disconnected during chat", slog.String("thread_id", threadID))
		return true
	}

	if err == nil {
		return false
	}

	if errors.Is(err, domain.ErrMaxToolIterations) {
		finishedEvent := events.NewRunFinishedEventWithOptions(threadID, runID,
			events.WithOutcome(events.RunFinishedOutcome{
				Type: events.RunFinishedOutcomeTypeInterrupt,
				Interrupts: []types.Interrupt{{
					ID:      uuid.New().String(),
					Reason:  "talk:max_iterations",
					Message: "I reached the tool call limit. Click Continue to let me keep working.",
				}},
			}),
		)
		_ = sse.WriteEvent(ctx, finishedEvent)
		return true
	}

	_ = sse.WriteEvent(ctx, events.NewRunErrorEvent(err.Error()))
	return true
}

// writeSSEError opens an SSE stream solely to emit a RUN_ERROR event.
func (h *Handler) writeSSEError(w http.ResponseWriter, r *http.Request, msg string) {
	sse, err := NewSSEWriter(w, h.log)
	if err != nil {
		http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}
	_ = sse.WriteEvent(r.Context(), events.NewRunErrorEvent(msg))
}

// classifyResumeStatus iterates the resume entries and returns whether any
// resolved or cancelled statuses were found. valid is false if an unknown
// status is encountered.
func classifyResumeStatus(entries []types.ResumeEntry) (hasResolved, hasCancelled, valid bool) {
	for _, entry := range entries {
		switch entry.Status {
		case types.ResumeStatusResolved:
			hasResolved = true
		case types.ResumeStatusCancelled:
			hasCancelled = true
		default:
			return false, false, false
		}
	}
	return hasResolved, hasCancelled, true
}

// extractModelAlias extracts the model alias from forwardedProps.
// Returns an error if forwardedProps is not a map or the model key is missing/empty.
func extractModelAlias(forwardedProps any) (string, error) {
	// Validate that forwardedProps is not nil
	if forwardedProps == nil {
		return "", fmt.Errorf("the model field is required")
	}

	// Validate that forwardedProps is a map[string]any
	props, ok := forwardedProps.(map[string]any)
	if !ok {
		return "", fmt.Errorf("the model field is required")
	}

	// Check for the presence of the "model" key
	modelRaw, exists := props["model"]
	if !exists {
		return "", fmt.Errorf("the model field is required")
	}

	// Validate that the model field is a non-empty string
	model, ok := modelRaw.(string)
	if !ok || model == "" {
		return "", fmt.Errorf("the model field is required")
	}

	return model, nil
}

// isModelSupported checks if the given model alias is in the list of supported models.
func (h *Handler) isModelSupported(alias string) bool {
	for _, m := range h.supportedModels {
		if m == alias {
			return true
		}
	}
	return false
}

// extractThinkingEffort extracts the thinking effort level from forwardedProps.
// Returns ThinkingOff for missing, invalid, or unrecognized values.
func extractThinkingEffort(forwardedProps any) domain.ThinkingEffort {
	// Default to ThinkingOff if forwardedProps is nil or not a map.
	props, ok := forwardedProps.(map[string]any)
	if !ok {
		return domain.ThinkingOff
	}
	// Default to ThinkingOff if the key is missing or the value is not a string.
	raw, exists := props["thinkingEffort"]
	if !exists {
		return domain.ThinkingOff
	}
	// Map string values to ThinkingEffort constants.
	value, ok := raw.(string)
	if !ok {
		return domain.ThinkingOff
	}
	// Return the corresponding ThinkingEffort constant, defaulting
	// to ThinkingOff for unrecognized values.
	switch value {
	case "low":
		return domain.ThinkingLow
	case "medium":
		return domain.ThinkingMedium
	case "high":
		return domain.ThinkingHigh
	default:
		return domain.ThinkingOff
	}
}

// lastMessageIsUser reports whether the final message in the slice is from the
// user, indicating a new turn to process rather than a bare interrupt cancel.
func lastMessageIsUser(messages []types.Message) bool {
	if len(messages) == 0 {
		return false
	}
	return messages[len(messages)-1].Role == "user"
}
