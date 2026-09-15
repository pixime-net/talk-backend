package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/pixime-net/talk/internal/domain"
)

const (
	fmtErrLine    = "%s%s\n"
	errPrefix     = "Error: "
	labelUntitled = "(untitled)"
)

func (a *App) cmdSession(ctx context.Context, args string) {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		// Default to list.
		a.cmdSessionList(ctx)
		return
	}
	switch parts[0] {
	case "list":
		a.cmdSessionList(ctx)
	case "new":
		a.cmdSessionNew(ctx)
	case "remove":
		a.cmdSessionRemove(ctx)
	default:
		a.Printf("Unknown /session subcommand %s. Available: %s, %s, %s\n",
			red(parts[0]), yellow("list"), yellow("new"), yellow("remove"))
	}
}

func (a *App) cmdSessionNew(_ context.Context) {
	newID := domain.GenerateSessionID()
	a.Scope = domain.NewSessionScope(newID, a.Scope.UserID())
	if a.Manager != nil {
		a.Manager.SetScope(a.Scope)
	}
	a.Printf("%s\n", green("✓ New session created."))
}

func (a *App) printSessions(sessions []domain.SessionSummary) {
	for i, s := range sessions {
		marker := ""
		if s.ID == a.Scope.SessionID() {
			marker = " " + green("← current")
		}
		title := s.Title
		if title == "" {
			title = labelUntitled
		}
		a.Printf("  [%d] %s  %s  %s%s\n",
			i+1,
			faint(s.CreatedAt.Format("2006-01-02 15:04")),
			faint(fmt.Sprintf("%d turns", s.TurnCount)),
			title,
			marker)
	}
}

func (a *App) cmdSessionList(ctx context.Context) {
	sessions, err := a.Sessions.ListSessions(ctx, a.Scope.UserID())
	if err != nil {
		a.Errorf(fmtErrLine, red(errPrefix), err.Error())
		return
	}

	a.Println("\n" + emphasize("Sessions:"))
	if len(sessions) == 0 {
		a.Println(faint("  (no past sessions found)"))
		return
	}

	a.printSessions(sessions)

	choice, err := a.Reader.ReadLine(fmt.Sprintf("Choose [1-%d] or 'new' (Enter to cancel): ", len(sessions)))
	if err != nil || strings.TrimSpace(choice) == "" {
		return
	}
	choice = strings.TrimSpace(choice)
	if choice == "new" {
		a.cmdSessionNew(ctx)
		return
	}
	n, err := strconv.Atoi(choice)
	if err != nil || n < 1 || n > len(sessions) {
		a.Println(yellow("Invalid choice."))
		return
	}
	selected := sessions[n-1].ID
	a.Scope = domain.NewSessionScope(selected, a.Scope.UserID())
	if a.Manager != nil {
		a.Manager.SetScope(a.Scope)
	}
	title := sessions[n-1].Title
	if title == "" {
		title = labelUntitled
	}
	a.Printf("Switched to session %s.\n", green(title))
}

func (a *App) cmdSessionRemove(ctx context.Context) {
	sessions, err := a.Sessions.ListSessions(ctx, a.Scope.UserID())
	if err != nil {
		a.Errorf(fmtErrLine, red(errPrefix), err.Error())
		return
	}
	if len(sessions) == 0 {
		a.Println(faint("(no sessions to remove)"))
		return
	}

	a.Println("\n" + emphasize("Sessions:"))
	a.printSessions(sessions)

	choice, err := a.Reader.ReadLine(fmt.Sprintf("Remove [1-%d] (Enter to cancel): ", len(sessions)))
	if err != nil || strings.TrimSpace(choice) == "" {
		return
	}
	n, err := strconv.Atoi(strings.TrimSpace(choice))
	if err != nil || n < 1 || n > len(sessions) {
		a.Println(yellow("Invalid choice."))
		return
	}

	selected := sessions[n-1]
	if selected.ID == a.Scope.SessionID() {
		a.Println(yellow("Cannot remove the current session."))
		return
	}

	if err := a.Sessions.DeleteSession(ctx, selected.ID); err != nil {
		a.Errorf(fmtErrLine, red("Error removing session: "), err.Error())
		return
	}
	title := selected.Title
	if title == "" {
		title = labelUntitled
	}
	a.Printf("Removed session %s.\n", green(title))
}

func (a *App) cmdMemory(ctx context.Context) {
	turns, err := a.Sessions.LoadHistoryTurnsFromSession(ctx, a.Scope.SessionID())
	if err != nil {
		a.Errorf(fmtErrLine, red(errPrefix), err.Error())
		return
	}
	if len(turns) == 0 {
		a.Println(faint("(no history for current session)"))
		return
	}
	// Resolve session title.
	title := ""
	if sessions, err := a.Sessions.ListSessions(ctx, a.Scope.UserID()); err == nil {
		for _, s := range sessions {
			if s.ID == a.Scope.SessionID() {
				title = s.Title
				break
			}
		}
	}
	header := emphasize("Session history:")
	if title != "" {
		header += "  " + cyan(title)
	}
	a.Printf("\n%s\n", header)
	for i, t := range turns {
		turnIDStr := ""
		if t.TurnID != "" {
			turnIDStr = "  " + faint(t.TurnID)
		}
		a.Printf("\n%s  %s%s\n",
			emphasize(fmt.Sprintf("Turn %d", i+1)),
			faint(t.At.Format("2006-01-02 15:04:05")),
			turnIDStr)
		a.Printf("  %s %s\n", green(bold+"You"+reset+":"), t.Question)
		a.Printf("  %s %s\n", cyan(bold+"Assistant"+reset+":"), t.Answer)
		if t.CallCount > 1 {
			a.Printf("  %s\n", faint(fmt.Sprintf("(%d LLM calls)", t.CallCount)))
		}
	}
}
