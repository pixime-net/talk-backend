package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/pixime-net/talk/internal/domain"
)

func (a *App) cmdModel() {
	models := domain.SupportedModels()
	slices.Sort(models)

	a.Println("\n" + emphasize("Available models:"))
	for i, model := range models {
		//d, _ := domain.Lookup(model)
		if string(model) == a.CurrentModel {
			a.Printf("  [%d] %s%s\n", i+1, cyan(fmt.Sprintf("%-14s", model)), green("← current"))
		} else {
			a.Printf("  [%d] %-14s\n", i+1, model)
		}
	}

	choice, err := a.Reader.ReadLine(fmt.Sprintf("Choose [1-%d]: ", len(models)))
	if err != nil {
		return
	}
	n, err := strconv.Atoi(strings.TrimSpace(choice))
	if err != nil || n < 1 || n > len(models) {
		a.Println(yellow("Invalid choice, keeping current model."))
		return
	}

	selected := models[n-1]
	client, err := a.Router.Get(selected)
	if err != nil {
		a.Errorf("%s%s\n", red("Error building client: "), err.Error())
		return
	}
	modelDescriptor, err := domain.Lookup(selected)
	if err != nil {
		a.Errorf("%s%s\n", red("Error resolving model: "), err.Error())
		return
	}
	a.Manager.SetClient(client, modelDescriptor)
	a.CurrentModel = string(selected)
	a.Printf("Switched to %s.\n", green(string(selected)))
}
