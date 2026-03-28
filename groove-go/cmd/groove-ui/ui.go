package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---- Styles ----------------------------------------------------------------

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			Padding(0, 1)

	peerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	senderSelfStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86"))

	senderPeerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39"))

	timestampStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("238"))

	dividerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("238"))

	inputBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("205")).
				Padding(0, 1)
)

// ---- Messages (Bubble Tea events) ------------------------------------------

// incomingMsg carries a chat line received from the pubsub layer.
type incomingMsg struct {
	from      string
	body      string
	workspace string
	ts        time.Time
}

// peerCountMsg updates the connected peer count in the status bar.
type peerCountMsg int

// waitForMessage is a tea.Cmd that blocks on the incoming channel.
func waitForMessage(ch <-chan incomingMsg) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

// pollPeers periodically refreshes the peer count.
func pollPeers(getPeers func() int) tea.Cmd {
	return tea.Tick(2*time.Second, func(_ time.Time) tea.Msg {
		return peerCountMsg(getPeers())
	})
}

// ---- Model -----------------------------------------------------------------

type model struct {
	workspace string
	selfID    string
	viewport  viewport.Model
	textarea  textarea.Model
	messages  []string
	incoming  <-chan incomingMsg
	getPeers  func() int
	peers     int
	publish   func(context.Context, string) error
	ctx       context.Context
	width     int
	height    int
	ready     bool
}

func newModel(
	ctx context.Context,
	workspace, selfID string,
	incoming <-chan incomingMsg,
	getPeers func() int,
	publish func(context.Context, string) error,
	history []incomingMsg,
) model {
	ta := textarea.New()
	ta.Placeholder = "Type a message…"
	ta.Focus()
	ta.CharLimit = 500
	ta.SetWidth(80)
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetEnabled(false) // Enter sends, not newlines

	m := model{
		workspace: workspace,
		selfID:    selfID,
		textarea:  ta,
		incoming:  incoming,
		getPeers:  getPeers,
		publish:   publish,
		ctx:       ctx,
	}

	// Pre-load history
	for _, h := range history {
		m.messages = append(m.messages, m.formatLine(h.from, h.body, h.ts, false))
	}
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		waitForMessage(m.incoming),
		pollPeers(m.getPeers),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		taCmd tea.Cmd
		vpCmd tea.Cmd
	)

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		inputHeight := 5 // textarea + border
		headerHeight := 2
		vpHeight := m.height - inputHeight - headerHeight
		if vpHeight < 1 {
			vpHeight = 1
		}
		if !m.ready {
			m.viewport = viewport.New(m.width, vpHeight)
			m.viewport.SetContent(m.renderMessages())
			m.ready = true
		} else {
			m.viewport.Width = m.width
			m.viewport.Height = vpHeight
		}
		m.textarea.SetWidth(m.width - 4)

	case incomingMsg:
		line := m.formatLine(msg.from, msg.body, msg.ts, false)
		m.messages = append(m.messages, line)
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()
		return m, waitForMessage(m.incoming) // wait for next message

	case peerCountMsg:
		m.peers = int(msg)
		return m, pollPeers(m.getPeers)

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit

		case tea.KeyEnter:
			text := strings.TrimSpace(m.textarea.Value())
			if text != "" {
				_ = m.publish(m.ctx, text)
				line := m.formatLine(m.selfID, text, time.Now(), true)
				m.messages = append(m.messages, line)
				m.viewport.SetContent(m.renderMessages())
				m.viewport.GotoBottom()
				m.textarea.Reset()
			}
		}
	}

	m.textarea, taCmd = m.textarea.Update(msg)
	m.viewport, vpCmd = m.viewport.Update(msg)
	return m, tea.Batch(taCmd, vpCmd)
}

func (m model) View() string {
	if !m.ready {
		return "\n  Initialising…"
	}

	// Header
	peerInfo := peerStyle.Render(fmt.Sprintf("  %d peer(s) connected", m.peers))
	header := titleStyle.Render("GrooveGO  #"+m.workspace) + peerInfo
	divider := dividerStyle.Render(strings.Repeat("─", m.width))

	// Input box
	input := inputBorderStyle.Render(m.textarea.View())

	return fmt.Sprintf("%s\n%s\n%s\n%s",
		header,
		divider,
		m.viewport.View(),
		input,
	)
}

// formatLine renders a single chat line.
func (m model) formatLine(from, body string, ts time.Time, isSelf bool) string {
	t := timestampStyle.Render(ts.Format("15:04"))
	var name string
	if isSelf {
		name = senderSelfStyle.Render("me")
	} else {
		name = senderPeerStyle.Render(shortID(from))
	}
	return fmt.Sprintf("%s  %s: %s", t, name, body)
}

func (m model) renderMessages() string {
	if len(m.messages) == 0 {
		return peerStyle.Render("  No messages yet — say hello!")
	}
	return strings.Join(m.messages, "\n")
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
