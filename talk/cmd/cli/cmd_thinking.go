package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pixime-net/talk/internal/domain"
)

var thinkingLevels = []domain.ThinkingEffort{
	domain.ThinkingOff,
	domain.ThinkingLow,
	domain.ThinkingMedium,
	domain.ThinkingHigh,
}

func (a *App) cmdThinking() {
	current := a.Manager.ThinkingEffort()
	if current == "" {
		current = domain.ThinkingOff
	}

	a.Println("\n" + emphasize("Thinking levels:"))
	for i, level := range thinkingLevels {
		if level == current {
			a.Printf("  [%d] %s%s\n", i+1, cyan(fmt.Sprintf("%-10s", string(level))), green("\u2190 current"))
		} else {
			a.Printf("  [%d] %-10s\n", i+1, string(level))
		}
	}

	choice, err := a.Reader.ReadLine(fmt.Sprintf("Choose [1-%d]: ", len(thinkingLevels)))
	if err != nil {
		return
	}
	n, err := strconv.Atoi(strings.TrimSpace(choice))
	if err != nil || n < 1 || n > len(thinkingLevels) {
		a.Println(yellow("Invalid choice, keeping current level."))
		return
	}

	selected := thinkingLevels[n-1]
	a.Manager.SetThinkingEffort(selected)
	if selected == domain.ThinkingOff {
		a.Printf("Thinking set to %s.\n", faint("off"))
	} else {
		a.Printf("Thinking set to %s.\n", green(string(selected)))
	}
}
