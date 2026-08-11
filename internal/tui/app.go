package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/IFAKA/tmpmail/internal/clipboard"
	mailx "github.com/IFAKA/tmpmail/internal/mail"
	"github.com/IFAKA/tmpmail/internal/provider"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type screen int

const (
	screenInbox screen = iota
	screenMessage
	screenActions
	screenHelp
	screenSearch
	screenRaw
)

// theme is deliberately centralized: every screen should feel like the same
// small instrument, including its empty and failure states.
var theme = struct {
	title, address, heading, muted, divider, panel, selected, unread, ready, warning, danger, footer, action lipgloss.Style
}{
	title:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")),
	address:  lipgloss.NewStyle().Foreground(lipgloss.Color("81")),
	heading:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")),
	muted:    lipgloss.NewStyle().Faint(true),
	divider:  lipgloss.NewStyle().Foreground(lipgloss.Color("238")),
	panel:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238")).Padding(1, 2),
	selected: lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("15")).Bold(true),
	unread:   lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true),
	ready:    lipgloss.NewStyle().Foreground(lipgloss.Color("78")).Bold(true),
	warning:  lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true),
	danger:   lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true),
	footer:   lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
	action:   lipgloss.NewStyle().Foreground(lipgloss.Color("117")),
}

type App struct {
	p             provider.Provider
	mu            sync.Mutex
	mb            provider.Mailbox
	msgs          []provider.MessageSummary
	cursor        int
	current       *provider.Message
	cache         map[string]provider.Message
	actions       []mailx.ActionCandidate
	actionCursor  int
	screen        screen
	status        string
	providerState string
	searchQuery   string
	previous      screen
	statusUntil   time.Time
	spinnerView   string
	listView      string
	actionsView   string
	quit          chan struct{}
	quitOnce      sync.Once
	nextRefresh   time.Time
	width         int
	height        int
}

func New(p provider.Provider) *App {
	return &App{p: p, status: "starting...", providerState: "LOADING", spinnerView: "⠋", quit: make(chan struct{}), cache: make(map[string]provider.Message)}
}

func (a *App) Run() error {
	_, err := tea.NewProgram(newModel(a), tea.WithAltScreen(), tea.WithInputTTY()).Run()
	return err
}

func (a *App) stop() { a.quitOnce.Do(func() { close(a.quit) }) }

func (a *App) createMailbox() error {
	a.mu.Lock()
	a.providerState = "LOADING"
	a.status = "creating mailbox..."
	a.mu.Unlock()
	mb, err := a.p.CreateMailbox(context.Background())
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.mb = mb
	a.msgs = nil
	a.cursor = 0
	a.providerState = "EMPTY"
	a.status = "waiting for mail..."
	a.mu.Unlock()
	if err := clipboard.Copy(mb.Address); err != nil {
		a.mu.Lock()
		a.status = "mailbox ready · clipboard unavailable: " + err.Error()
		a.mu.Unlock()
	} else {
		a.mu.Lock()
		a.status = "address copied"
		a.statusUntil = time.Now().Add(4 * time.Second)
		a.mu.Unlock()
	}
	return nil
}

func (a *App) refresh() {
	a.mu.Lock()
	if !a.nextRefresh.IsZero() && time.Now().Before(a.nextRefresh) {
		a.mu.Unlock()
		return
	}
	a.nextRefresh = time.Now().Add(2 * time.Second)
	mb := a.mb
	if mb.Token == "" {
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	msgs, err := a.p.ListMessages(context.Background(), mb)
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		var httpErr provider.HTTPError
		if errors.As(err, &httpErr) {
			switch httpErr.StatusCode() {
			case 401, 404:
				a.providerState = "EXPIRED"
				a.status = "mailbox expired or deleted · cached mail remains readable"
			case 429:
				a.providerState = "RATE LIMITED"
				wait := httpErr.RetryAfter()
				if wait <= 0 {
					wait = 5 * time.Second
				}
				a.nextRefresh = time.Now().Add(wait)
				a.status = fmt.Sprintf("rate limited · retrying in %s", wait.Round(time.Second))
			default:
				a.providerState = "OFFLINE"
				a.status = "provider unavailable · cached mail available"
			}
		} else {
			a.providerState = "OFFLINE"
			a.status = "offline · cached mail available"
		}
		return
	}
	a.msgs = msgs
	if len(msgs) == 0 {
		a.providerState = "EMPTY"
	} else {
		a.providerState = "READY"
	}
	if a.cursor >= len(a.msgs) {
		a.cursor = max(0, len(a.msgs)-1)
	}
}

func (a *App) handle(k string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if k == "\x03" {
		a.stop()
		return true
	}
	switch a.screen {
	case screenInbox:
		switch k {
		case "q":
			a.stop()
			return true
		case "j":
			if a.cursor < len(a.msgs)-1 {
				a.cursor++
			}
		case "k":
			if a.cursor > 0 {
				a.cursor--
			}
		case "g":
			a.cursor = 0
		case "G":
			if len(a.msgs) > 0 {
				a.cursor = len(a.msgs) - 1
			}
		case "\x04":
			a.moveByHalfPage(1)
		case "\x15":
			a.moveByHalfPage(-1)
		case "l", "\r":
			if len(a.msgs) > 0 {
				id := a.msgs[a.cursor].ID
				mb := a.mb
				a.mu.Unlock()
				m, err := a.p.GetMessage(context.Background(), mb, id)
				a.mu.Lock()
				if err != nil {
					if cached, ok := a.cache[id]; ok {
						a.current = &cached
						a.actions = mailx.Extract(cached.Subject, cached.Text, cached.HTML)
						a.screen = screenMessage
						a.status = "offline · showing cached message"
						break
					}
					a.status = "open failed: " + err.Error()
				} else {
					a.current = &m
					a.cache[id] = m
					a.actions = mailx.Extract(m.Subject, m.Text, m.HTML)
					a.screen = screenMessage
					a.actionCursor = 0
				}
			}
		case "Y":
			if err := clipboard.Copy(a.mb.Address); err != nil {
				a.status = "clipboard unavailable: " + err.Error()
			} else {
				a.status = "address copied"
				a.statusUntil = time.Now().Add(4 * time.Second)
			}
		case "r":
			if a.mb.Token == "" {
				go func() {
					if err := a.createMailbox(); err != nil {
						a.mu.Lock()
						a.providerState = "FATAL"
						a.status = err.Error()
						a.mu.Unlock()
					}
				}()
			} else {
				go func() { a.refresh() }()
			}
		case "n":
			go func() {
				if err := a.createMailbox(); err != nil {
					a.mu.Lock()
					a.providerState = "FATAL"
					a.status = err.Error()
					a.mu.Unlock()
				}
			}()
		case "?":
			a.status = "j/k move · Ctrl-d/u page · g/G first/last · l open · Y address · n new · r refresh · q quit"
		}
	case screenMessage:
		switch k {
		case "h", "esc", "q":
			a.screen = screenInbox
			a.current = nil
			a.actions = nil
		case "a":
			if len(a.actions) > 0 {
				a.screen = screenActions
			}
		case "y":
			if len(a.actions) == 0 {
				a.status = "no OTP/link detected"
			} else if a.actions[0].Score >= 50 && (len(a.actions) == 1 || a.actions[0].Score-a.actions[1].Score >= 20) {
				a.copyAction(a.actions[0])
			} else {
				a.screen = screenActions
			}
		case "?":
			a.status = "h/Esc back · y smart-copy · a actions · q back"
		case "o":
			a.screen = screenRaw
			a.status = "raw/source view"
		}
	case screenActions:
		switch k {
		case "j":
			if a.actionCursor < len(a.actions)-1 {
				a.actionCursor++
			}
		case "k":
			if a.actionCursor > 0 {
				a.actionCursor--
			}
		case "y", "\r":
			if len(a.actions) > 0 {
				a.copyAction(a.actions[a.actionCursor])
				a.screen = screenMessage
			}
		case "h", "esc", "q":
			a.screen = screenMessage
		case "l":
			if len(a.actions) > 0 {
				a.screen = screenMessage
				a.status = "action: " + a.actions[a.actionCursor].Value
			}
		}
	case screenHelp, screenSearch, screenRaw:
		// Bubble Tea handles these modal screens so text input and viewport
		// updates stay in the event loop.
	}
	return false
}

func (a *App) moveByHalfPage(direction int) {
	h := a.height
	if h == 0 {
		h = 24
	}
	step := max(1, h/2)
	a.cursor += direction * step
	if a.cursor < 0 {
		a.cursor = 0
	}
	if a.cursor >= len(a.msgs) {
		a.cursor = max(0, len(a.msgs)-1)
	}
}

func (a *App) copyAction(x mailx.ActionCandidate) {
	if err := clipboard.Copy(x.Value); err != nil {
		a.status = "clipboard unavailable · value: " + x.Value
	} else {
		a.status = "copied " + string(x.Type) + ": " + x.Value
		a.statusUntil = time.Now().Add(4 * time.Second)
	}
}

func (a *App) view(width, height int, footer, messageBody string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.statusUntil.IsZero() && time.Now().After(a.statusUntil) {
		a.status = ""
		a.statusUntil = time.Time{}
	}
	w, h := width, height
	a.width, a.height = w, h
	if w < 32 || h < 6 {
		return lipgloss.NewStyle().Width(max(0, w)).Height(max(0, h)).Render("tmpmail\n\nterminal too small\nneed ≥ 32×6\n\nq quit")
	}
	var body strings.Builder
	switch a.screen {
	case screenInbox:
		if a.providerState == "LOADING" {
			body.WriteString(centerPanel(w, h, panelContent("CONNECTING", a.spinnerView+" creating temporary mailbox", "please wait")))
		} else if a.providerState == "FATAL" {
			body.WriteString(centerPanel(w, h, panelContent("STARTUP FAILED", a.status, "r retry · q quit")))
		} else if len(a.msgs) == 0 {
			body.WriteString(centerPanel(w, h, panelContent("INBOX EMPTY", a.spinnerView+" waiting for mail", "refreshing automatically")))
		} else {
			body.WriteString(theme.heading.Render(fmt.Sprintf("INBOX · %d emails", len(a.msgs))))
			if a.listView != "" {
				body.WriteString("\n" + a.listView + "\n")
			} else {
				body.WriteString("\n" + renderInboxRows(a.msgs, a.cursor, w-8) + "\n")
			}
		}
		if a.providerState == "OFFLINE" || a.providerState == "RATE LIMITED" || a.providerState == "EXPIRED" {
			body.WriteString("\n" + stateLabel(a.providerState) + " " + theme.muted.Render(cut(a.status, w-16)) + "\n")
		}
	case screenMessage:
		m := a.current
		var message strings.Builder
		fmt.Fprintf(&message, "%s\nFrom: %s\nTo: %s\n", theme.heading.Render(m.Subject), m.From, a.mb.Address)
		if !m.CreatedAt.IsZero() {
			fmt.Fprintf(&message, "Time: %s\n", m.CreatedAt.Local().Format("2006-01-02 15:04"))
		}
		message.WriteString("\n")
		contentBody := messageBody
		if contentBody == "" {
			contentBody = m.Text
			if strings.TrimSpace(contentBody) == "" {
				contentBody = "[HTML-only message]\n" + mailx.Readable(m.HTML)
			}
			contentBody = wrap(contentBody, max(20, w-8), h-10)
		}
		message.WriteString(contentBody)
		message.WriteString("\n\n")
		if len(a.actions) > 0 {
			fmt.Fprintf(&message, "%s %s  (%d)\n", theme.muted.Render("best:"), cut(a.actions[0].Value, w-16), a.actions[0].Score)
		}
		if a.status != "" {
			message.WriteString(theme.muted.Render(cut(a.status, w)) + "\n")
		}
		body.WriteString(theme.panel.Width(max(20, w-8)).Render(message.String()))
	case screenActions:
		body.WriteString(theme.heading.Render("DETECTED ACTIONS") + "\n")
		if a.actionsView != "" {
			body.WriteString(a.actionsView)
		} else {
			body.WriteString(renderActions(a.actions, a.actionCursor, w-8))
		}
		if a.status != "" {
			fmt.Fprintf(&body, "\n%s\n", theme.muted.Render(cut(a.status, w)))
		}
	case screenHelp:
		help := "j/k move     h/Esc back     l/Enter open     g/G first/last\nCtrl-d/u page   y copy action   Y copy address   a candidates\n/ search       n new mailbox   r refresh       o raw/source\n? help         q quit"
		body.WriteString(theme.heading.Render("KEYS") + "\n\n" + theme.panel.Width(max(20, w-8)).Render(help))
	case screenSearch:
		body.WriteString(theme.heading.Render("SEARCH INBOX") + "\n\n")
		fmt.Fprintf(&body, "Search  %s\n\n", lipgloss.NewStyle().Bold(true).Render(a.searchQuery+"█"))
		rows := make([]string, 0)
		for i, m := range a.msgs {
			if a.searchQuery == "" || strings.Contains(strings.ToLower(m.From+" "+m.Subject+" "+m.Intro), strings.ToLower(a.searchQuery)) {
				mark := "  "
				if i == a.cursor {
					mark = "▶ "
				}
				rows = append(rows, mark+cut(m.From, 24)+"  "+cut(m.Subject, max(20, w-36)))
			}
		}
		body.WriteString(strings.Join(rows, "\n"))
		body.WriteString("\n\n" + theme.muted.Render("type query · Enter accept · Esc clear"))
	case screenRaw:
		body.WriteString(theme.heading.Render("RAW / SOURCE") + "\n\n")
		if a.current != nil {
			body.WriteString(theme.panel.Width(max(20, w-8)).Render(messageBody))
		}
	}
	return shell(w, h, addressFor(a.mb), a.providerState, body.String(), footer)
}

func shell(w, h int, address, state, content, footer string) string {
	top := lipgloss.JoinHorizontal(lipgloss.Top, theme.title.Render("tmpmail"), "  ", theme.address.Render(address), "  ", stateLabel(state))
	region := theme.panel.Width(max(4, w-4)).MaxHeight(max(1, h-5)).Render(content)
	bottom := theme.footer.Render(cut(footer, max(1, w-2)))
	return lipgloss.NewStyle().Width(w).Height(h).Render(lipgloss.JoinVertical(lipgloss.Left, top, theme.divider.Render(strings.Repeat("─", max(1, w-1))), region, bottom))
}

func addressFor(mb provider.Mailbox) string { return blank(mb.Address, "creating mailbox…") }
func panelContent(label, main, hint string) string {
	return theme.heading.Render(label) + "\n\n" + main + "\n\n" + theme.muted.Render(hint)
}
func centerPanel(w, h int, s string) string {
	return lipgloss.Place(max(1, w-4), max(1, h-8), lipgloss.Center, lipgloss.Center, theme.panel.Width(min(42, max(20, w-8))).Render(s))
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func renderInboxRows(msgs []provider.MessageSummary, cursor, width int) string {
	lines := []string{theme.muted.Render("   SENDER                 SUBJECT                         ACTION          TIME")}
	for i, m := range msgs {
		mark := "  "
		if i == cursor {
			mark = "▶ "
		}
		unread := " "
		if !m.Seen {
			unread = theme.unread.Render("●")
		}
		tm := "--:--"
		if !m.CreatedAt.IsZero() {
			tm = m.CreatedAt.Local().Format("15:04")
		}
		line := mark + unread + " " + cut(m.From, 20) + "  " + cut(m.Subject, max(12, width-58)) + "  " + theme.action.Render(cut(bestSummaryAction(m), 14)) + "  " + tm
		if i == cursor {
			line = theme.selected.Width(max(1, width)).Render(line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
func renderActions(xs []mailx.ActionCandidate, cursor, width int) string {
	lines := []string{}
	for i, x := range xs {
		mark := "  "
		if i == cursor {
			mark = "▶ "
		}
		line := mark + fmt.Sprintf("%-12s %-*s %3d%%  %s", string(x.Type), max(12, width/3), cut(x.Value, max(12, width/3)), x.Score, cut(x.Evidence, max(12, width/3)))
		if i == cursor {
			line = theme.selected.Width(max(1, width)).Render(line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func bestSummaryAction(m provider.MessageSummary) string {
	actions := mailx.Extract(m.Subject, m.Intro, "")
	if len(actions) == 0 || actions[0].Score < 50 {
		return ""
	}
	return actions[0].Value
}

func stateLabel(s string) string {
	color := "32"
	if s == "OFFLINE" || s == "FATAL" || s == "EXPIRED" || s == "RATE LIMITED" {
		color = "31"
	} else if s == "EMPTY" || s == "LOADING" {
		color = "33"
	}
	return statusStyle(color).Render("[" + s + "]")
}

func statusStyle(color string) lipgloss.Style {
	style := lipgloss.NewStyle().Bold(true)
	switch color {
	case "31":
		return style.Foreground(lipgloss.Color("203"))
	case "33":
		return style.Foreground(lipgloss.Color("220"))
	default:
		return style.Foreground(lipgloss.Color("78"))
	}
}
func blank(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
func cut(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n < 2 {
		return string(r[:max(0, n)])
	}
	return string(r[:n-1]) + "…"
}
func wrap(s string, w, h int) string {
	if w < 10 {
		w = 10
	}
	var lines []string
	for _, paragraph := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		line := ""
		for _, x := range words {
			if len([]rune(x)) > w {
				if line != "" {
					lines = append(lines, line)
					line = ""
				}
				for len([]rune(x)) > w {
					r := []rune(x)
					lines = append(lines, string(r[:w]))
					x = string(r[w:])
				}
			}
			if line == "" {
				line = x
			} else if len([]rune(line))+1+len([]rune(x)) <= w {
				line += " " + x
			} else {
				lines = append(lines, line)
				line = x
			}
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > h {
		lines = lines[:h]
		lines[len(lines)-1] += " …"
	}
	return strings.Join(lines, "\n")
}
func stripView(s string) string {
	in := false
	var b strings.Builder
	for _, r := range s {
		if r == '<' {
			in = true
			continue
		}
		if r == '>' {
			in = false
			continue
		}
		if !in {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
