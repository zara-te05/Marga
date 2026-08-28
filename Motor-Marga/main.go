package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	// Reemplaza esta ruta con el módulo real de tu proyecto
	"github.com/zarat/Motor-Marga/cmd"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	colorBg       = lipgloss.Color("#0F172A")
	colorBorder   = lipgloss.Color("#1E293B")
	colorAccent   = lipgloss.Color("#38BDF8")
	colorSubtle   = lipgloss.Color("#64748B")
	colorText     = lipgloss.Color("#F1F5F9")
	colorCommand  = lipgloss.Color("#7DD3FC")
	colorResponse = lipgloss.Color("#94A3B8")

	// En containerStyle dentro de main.go:
	containerStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2).
			Width(90). // Define un ancho fijo o dinámico para que no se deforme la tarjeta
			Background(colorBg)

	titleStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true).
			MarginBottom(1)

	cmdStyle = lipgloss.NewStyle().
			Foreground(colorCommand).
			Bold(true)

	outStyle = lipgloss.NewStyle().
			Foreground(colorResponse)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorSubtle).
			MarginTop(1)
)

type historyItem struct {
	command string
	output  string
}

type model struct {
	history   []historyItem
	textInput textinput.Model
	width     int
	height    int
}

func New() *model {
	ti := textinput.New()
	ti.Placeholder = "Escribe un comando..."
	ti.Focus()
	ti.CharLimit = 156
	ti.Width = 80

	pwd, _ := os.Getwd()
	folderName := filepath.Base(pwd)
	ti.Prompt = fmt.Sprintf("marga [%s]> ", folderName)
	ti.PromptStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	ti.TextStyle = lipgloss.NewStyle().Foreground(colorText)
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(colorSubtle)

	return &model{
		history:   make([]historyItem, 0),
		textInput: ti,
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m *model) evalCommand(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}

	if input == "clear" {
		m.history = nil
		return ""
	}

	args := cmd.ParseInput(input)

	// Crear el buffer y asignarlo a Cobra
	buf := new(bytes.Buffer)
	cmd.RootCmd.SetOut(buf)
	cmd.RootCmd.SetErr(buf)
	cmd.RootCmd.SetArgs(args)

	// Ejecutar comando
	err := cmd.RootCmd.Execute()

	// Actualizar prompt
	pwd, _ := os.Getwd()
	m.textInput.Prompt = fmt.Sprintf("marga [%s]> ", pwd)

	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	output := strings.TrimSpace(buf.String())
	return output
}
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "enter":
			input := m.textInput.Value()
			if strings.TrimSpace(input) == "exit" || strings.TrimSpace(input) == "quit" {
				return m, tea.Quit
			}

			output := m.evalCommand(input)

			if strings.TrimSpace(input) != "clear" && input != "" {
				m.history = append(m.history, historyItem{
					command: input,
					output:  output,
				})
			}

			m.textInput.Reset()
		}
	}

	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if m.width == 0 {
		return "Cargando..."
	}

	var content strings.Builder

	content.WriteString(titleStyle.Render("MARGA") + "\n\n")

	maxItems := 6
	start := 0
	if len(m.history) > maxItems {
		start = len(m.history) - maxItems
	}

	// En func (m model) View() string:

	for _, item := range m.history[start:] {
		content.WriteString(fmt.Sprintf("%s %s\n", lipgloss.NewStyle().Foreground(colorAccent).Render("❯"), cmdStyle.Render(item.command)))

		if item.output != "" {
			// Renderizar la salida línea por línea para evitar bloques negros en los saltos de línea
			lines := strings.Split(item.output, "\n")
			for _, line := range lines {
				if strings.TrimSpace(line) != "" {
					content.WriteString(fmt.Sprintf("  %s\n", outStyle.Render(line)))
				}
			}
		}
	}

	if len(m.history) > 0 {
		content.WriteString("\n")
	}

	content.WriteString(m.textInput.View())
	content.WriteString("\n")
	content.WriteString(helpStyle.Render("Escribe 'exit' o presiona [Ctrl+C] para salir"))

	card := containerStyle.Render(content.String())
	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		card,
	)
}

func main() {
	m := New()

	f, err := tea.LogToFile("debug.log", "debug")
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	defer f.Close()

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
