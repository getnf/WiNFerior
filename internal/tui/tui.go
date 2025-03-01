package tui

import (
	"database/sql"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/getnf/winferior/internal/db"
	"github.com/getnf/winferior/internal/handlers"
	"github.com/getnf/winferior/internal/types"
	"github.com/getnf/winferior/internal/utils"
)

func ThemeWinferiorInstall() *huh.Theme {
	t := huh.ThemeBase()

	t.Focused.Base = t.Focused.Base.BorderForeground(lipgloss.Color("7"))
	t.Focused.Title = t.Focused.Title.Foreground(lipgloss.Color("3"))
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(lipgloss.Color("3"))
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(lipgloss.Color("2"))
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(lipgloss.Color("2"))
	t.Focused.SelectedPrefix = t.Focused.SelectedPrefix.Foreground(lipgloss.Color("2"))

	return t
}

func ThemeWinferiorUninstall() *huh.Theme {
	t := ThemeWinferiorInstall()

	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(lipgloss.Color("1"))
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(lipgloss.Color("1"))
	t.Focused.SelectedPrefix = t.Focused.SelectedPrefix.Foreground(lipgloss.Color("1"))

	return t
}

func myKeyBinds(submitMessage string) *huh.KeyMap {
	var binding huh.KeyMap

	binding.Quit = key.NewBinding(key.WithKeys("ctrl+c", "q"), key.WithHelp("ctrl+c / q", "quit"))
	binding.MultiSelect = huh.MultiSelectKeyMap{
		Up:          key.NewBinding(key.WithKeys("up", "k", "ctrl+p"), key.WithHelp("k / ↑ / C-p:", "Previous")),
		Down:        key.NewBinding(key.WithKeys("down", "j", "ctrl+n"), key.WithHelp("j / ↓ / C-n:", "Next")),
		GotoTop:     key.NewBinding(key.WithKeys("home"), key.WithHelp("Home:", "Go to the top")),
		GotoBottom:  key.NewBinding(key.WithKeys("end"), key.WithHelp("End:", "Go to the bottom")),
		Toggle:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab:", "Toggle")),
		Filter:      key.NewBinding(key.WithKeys(" ", "/"), key.WithHelp("space / /:", "Filter")),
		SetFilter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎ :", "Set filter"), key.WithDisabled()),
		ClearFilter: key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc:", "Clear filter"), key.WithDisabled()),
		Submit:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎ :", submitMessage)),
	}

	return &binding
}

func SelectFontsToInstall(data types.NerdFonts, database *sql.DB, downloadPath string, extractPath string, keepTar bool) error {
	var selectedFontsNames []string
	var selectedFonts []types.Font
	fontsNames := data.GetFontsNames()
	optionsFromFonts := huh.NewOptions(fontsNames...)

	ms := huh.NewMultiSelect[string]().
		Options(
			optionsFromFonts...,
		).
		Title("Select fonts to install").
		Value(&selectedFontsNames).
		Filterable(true)

	form := huh.NewForm(
		huh.NewGroup(
			ms,
		),
	).WithTheme(
		ThemeWinferiorInstall(),
	).WithKeyMap(myKeyBinds("Install fonts"))

	form.Run()

	for _, fontName := range selectedFontsNames {
		selectedFontName := data.GetFont(fontName)
		selectedFonts = append(selectedFonts, selectedFontName)
	}

	for _, font := range selectedFonts {
		err := handlers.InstallFont(font, downloadPath, extractPath, keepTar)
		if err != nil {
			return err
		}
		db.InsertIntoInstalledFonts(database, font, data.GetVersion())
	}

	return nil
}

func SelectFontsToUninstall(installedFonts []types.Font, database *sql.DB, extractPath string) error {
	var selectedFonts []string
	installedFontsNames := utils.Fold(installedFonts, func(f types.Font) string {
		return f.Name
	})
	optionsFromInstalledFonts := huh.NewOptions(installedFontsNames...)

	ms := huh.NewMultiSelect[string]().
		Options(
			optionsFromInstalledFonts...,
		).
		Title("Select fonts to uninstall").
		Value(&selectedFonts).
		Filterable(true)

	form := huh.NewForm(
		huh.NewGroup(
			ms,
		),
	).WithTheme(
		ThemeWinferiorUninstall(),
	).WithKeyMap(myKeyBinds("Uninstall fonts"))

	form.Run()

	for _, font := range selectedFonts {
		err := handlers.UninstallFont(extractPath, font)
		if err != nil {
			return err
		}
		db.DeleteInstalledFont(database, font)
	}

	return nil
}
