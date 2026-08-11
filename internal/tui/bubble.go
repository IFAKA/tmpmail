package tui

import (
	"fmt"
	"io"
	"time"

	mailx "github.com/IFAKA/tmpmail/internal/mail"
	"github.com/IFAKA/tmpmail/internal/provider"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tickMsg time.Time
type mailboxMsg struct{ err error }

type inboxItem struct{ message provider.MessageSummary }

func (i inboxItem) FilterValue() string {
	return i.message.From + " " + i.message.Subject + " " + i.message.Intro
}

type actionItem struct{ action mailx.ActionCandidate }

func (i actionItem) FilterValue() string { return i.action.Value + " " + i.action.Evidence }

type compactDelegate struct{ kind string }

func (d compactDelegate) Height() int                         { return 1 }
func (d compactDelegate) Spacing() int                        { return 0 }
func (d compactDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (d compactDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	line := "  "
	if index == m.Index() {
		line = "▶ "
	}
	if d.kind == "inbox" {
		x := item.(inboxItem).message
		unread := " "
		if !x.Seen {
			unread = "●"
		}
		tm := "--:--"
		if !x.CreatedAt.IsZero() {
			tm = x.CreatedAt.Local().Format("15:04")
		}
		line += unread + " " + cut(x.From, 20) + "  " + cut(x.Subject, max(12, m.Width()-60)) + "  " + cut(bestSummaryAction(x), 14) + "  " + tm
	} else {
		x := item.(actionItem).action
		line += fmt.Sprintf("%-12s %-*s %3d%%  %s", string(x.Type), max(12, m.Width()/3), cut(x.Value, max(12, m.Width()/3)), x.Score, cut(x.Evidence, max(12, m.Width()/3)))
	}
	style := lipgloss.NewStyle().Width(max(1, m.Width()))
	if index == m.Index() {
		style = theme.selected.Width(max(1, m.Width()))
	}
	_, _ = io.WriteString(w, style.Render(line))
}

type keyMap struct {
	Up, Down, First, Last, HalfDown, HalfUp, Open, Back, Copy, CopyAddress, New, Refresh, Actions, Search, Raw, Help, Quit key.Binding
}

func newKeyMap() keyMap {
	bind := func(keys []string, helpText string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(keys[0], helpText))
	}
	return keyMap{
		Up:          bind([]string{"k", "up"}, "up"),
		Down:        bind([]string{"j", "down"}, "down"),
		First:       bind([]string{"g"}, "first"),
		Last:        bind([]string{"G"}, "last"),
		HalfDown:    bind([]string{"ctrl+d"}, "half-page down"),
		HalfUp:      bind([]string{"ctrl+u"}, "half-page up"),
		Open:        bind([]string{"l", "enter"}, "open"),
		Back:        bind([]string{"h", "esc"}, "back"),
		Copy:        bind([]string{"y"}, "copy"),
		CopyAddress: bind([]string{"Y"}, "address"),
		New:         bind([]string{"n"}, "new"),
		Refresh:     bind([]string{"r"}, "refresh"),
		Actions:     bind([]string{"a"}, "actions"),
		Search:      bind([]string{"/"}, "search"),
		Raw:         bind([]string{"o"}, "raw"),
		Help:        bind([]string{"?"}, "help"),
		Quit:        bind([]string{"q", "ctrl+c"}, "quit"),
	}
}

type model struct {
	app           *App
	keys          keyMap
	help          help.Model
	search        textinput.Model
	messageView   viewport.Model
	rawView       viewport.Model
	lastRawID     string
	spinner       spinner.Model
	inboxList     list.Model
	actionsList   list.Model
	lastMessageID string
	width, height int
}

func newModel(app *App) *model {
	h := help.New()
	h.ShowAll = false
	search := textinput.New()
	search.Placeholder = "search inbox"
	search.CharLimit = 120
	messageView := viewport.New(80, 20)
	messageView.MouseWheelEnabled = false
	rawView := viewport.New(80, 20)
	rawView.MouseWheelEnabled = false
	sp := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(theme.warning))
	inboxList := list.New(nil, compactDelegate{kind: "inbox"}, 80, 20)
	actionsList := list.New(nil, compactDelegate{kind: "actions"}, 80, 20)
	for _, l := range []*list.Model{&inboxList, &actionsList} {
		l.SetShowTitle(false)
		l.SetShowStatusBar(false)
		l.SetShowHelp(false)
		l.SetShowFilter(false)
		l.SetFilteringEnabled(false)
		l.SetShowPagination(false)
	}
	return &model{app: app, keys: newKeyMap(), help: h, search: search, messageView: messageView, rawView: rawView, spinner: sp, inboxList: inboxList, actionsList: actionsList}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.createMailbox(), tickCmd(), spinner.Tick)
}

func tickCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) createMailbox() tea.Cmd {
	return func() tea.Msg {
		return mailboxMsg{err: m.app.createMailbox()}
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width
		m.messageView.Width = max(20, msg.Width-8)
		m.messageView.Height = max(5, msg.Height-16)
		m.rawView.Width = m.messageView.Width
		m.rawView.Height = m.messageView.Height
		m.inboxList.SetSize(max(20, msg.Width-8), max(1, msg.Height-10))
		m.actionsList.SetSize(max(20, msg.Width-8), max(1, msg.Height-10))
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		m.app.mu.Lock()
		m.app.spinnerView = m.spinner.View()
		m.app.mu.Unlock()
		return m, cmd
	case mailboxMsg:
		if msg.err != nil {
			m.app.mu.Lock()
			m.app.providerState = "FATAL"
			m.app.status = msg.err.Error()
			m.app.mu.Unlock()
		}
		return m, nil
	case tickMsg:
		m.app.refresh()
		return m, tickCmd()
	case tea.KeyMsg:
		if m.app.screen == screenHelp {
			if msg.String() == "q" || msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			if msg.String() == "h" || msg.String() == "esc" || msg.String() == "?" {
				m.app.screen = m.app.previous
				m.help.ShowAll = false
			}
			return m, nil
		}
		if m.app.screen == screenSearch {
			if msg.String() == "esc" {
				m.app.screen = screenInbox
				m.search.Reset()
				m.app.searchQuery = ""
				return m, nil
			}
			if msg.String() == "enter" {
				m.app.screen = screenInbox
				m.search.Blur()
				return m, nil
			}
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(msg)
			m.app.searchQuery = m.search.Value()
			return m, cmd
		}
		if m.app.screen == screenRaw {
			if msg.String() == "q" || msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			if msg.String() == "h" || msg.String() == "esc" {
				m.app.screen = screenMessage
				return m, nil
			}
			switch msg.String() {
			case "j", "down":
				m.rawView.LineDown(1)
			case "k", "up":
				m.rawView.LineUp(1)
			case "ctrl+d":
				m.rawView.PageDown()
			case "ctrl+u":
				m.rawView.PageUp()
			}
			return m, nil
		}
		if key.Matches(msg, m.keys.Quit) {
			return m, tea.Quit
		}
		if msg.String() == "?" {
			m.app.mu.Lock()
			m.app.previous = m.app.screen
			m.app.screen = screenHelp
			m.app.mu.Unlock()
			m.help.ShowAll = true
			return m, nil
		}
		if msg.String() == "/" && m.app.screen == screenInbox {
			m.app.screen = screenSearch
			m.help.ShowAll = false
			m.search.Focus()
			return m, textinput.Blink
		}
		if m.app.screen == screenMessage {
			if m.app.current != nil && m.lastMessageID != m.app.current.ID {
				m.lastMessageID = m.app.current.ID
				m.messageView.SetContent(m.messageContent())
			}
			switch msg.String() {
			case "j", "down":
				m.messageView.LineDown(1)
				return m, nil
			case "k", "up":
				m.messageView.LineUp(1)
				return m, nil
			case "ctrl+d":
				m.messageView.PageDown()
				return m, nil
			case "ctrl+u":
				m.messageView.PageUp()
				return m, nil
			}
		}
		if key.Matches(msg, m.keys.Up) {
			m.app.handle("k")
		} else if key.Matches(msg, m.keys.Down) {
			m.app.handle("j")
		} else if msg.String() == "ctrl+d" {
			m.app.handle("\x04")
		} else if msg.String() == "ctrl+u" {
			m.app.handle("\x15")
		} else if msg.String() == "enter" {
			m.app.handle("\r")
		} else {
			m.app.handle(msg.String())
		}
		m.syncMessageViewport()
		m.syncRawViewport()
		return m, nil
	}
	return m, nil
}

func (m *model) syncMessageViewport() {
	if m.app.screen != screenMessage || m.app.current == nil || m.lastMessageID == m.app.current.ID {
		return
	}
	m.lastMessageID = m.app.current.ID
	m.messageView.SetContent(m.messageContent())
	m.messageView.GotoTop()
}

func (m *model) syncRawViewport() {
	if m.app.screen != screenRaw || m.app.current == nil || m.lastRawID == m.app.current.ID {
		return
	}
	m.lastRawID = m.app.current.ID
	m.rawView.SetContent(m.rawContent())
	m.rawView.GotoTop()
}

func (m *model) View() string {
	m.syncLists()
	content := ""
	if m.app.screen == screenMessage {
		content = m.messageView.View()
	} else if m.app.screen == screenRaw {
		content = m.rawView.View()
	}
	return m.app.view(m.width, m.height, m.footer(), content)
}

func (m *model) syncLists() {
	m.app.mu.Lock()
	msgs := append([]provider.MessageSummary(nil), m.app.msgs...)
	actionsSnapshot := append([]mailx.ActionCandidate(nil), m.app.actions...)
	cursor, actionCursor := m.app.cursor, m.app.actionCursor
	m.app.mu.Unlock()
	inbox := make([]list.Item, 0, len(msgs))
	for _, x := range msgs {
		inbox = append(inbox, inboxItem{message: x})
	}
	_ = m.inboxList.SetItems(inbox)
	m.inboxList.Select(min(cursor, max(0, len(inbox)-1)))
	actions := make([]list.Item, 0, len(actionsSnapshot))
	for _, x := range actionsSnapshot {
		actions = append(actions, actionItem{action: x})
	}
	_ = m.actionsList.SetItems(actions)
	m.actionsList.Select(min(actionCursor, max(0, len(actions)-1)))
	inboxView, actionsView := m.inboxList.View(), m.actionsList.View()
	m.app.mu.Lock()
	m.app.listView, m.app.actionsView = inboxView, actionsView
	m.app.mu.Unlock()
}

func (m *model) footer() string {
	switch m.app.screen {
	case screenInbox:
		if m.app.providerState == "FATAL" {
			return "r retry · q quit"
		}
		if m.app.providerState == "EXPIRED" {
			return "j/k cached mail · n new mailbox · q quit"
		}
		return "j/k move · l/Enter open · y copy · Y address · / search · ? help · q quit"
	case screenMessage:
		return "j/k scroll · Ctrl-d/u page · y copy · a actions · o raw · h/Esc back · ? help"
	case screenActions:
		return "j/k select · y/Enter copy · l inspect · h/Esc back · ? help"
	case screenSearch:
		return "type to filter · Enter accept · Esc cancel"
	case screenRaw:
		return "j/k scroll · Ctrl-d/u page · h/Esc back · q quit"
	case screenHelp:
		return "h/Esc close · ? close · q quit"
	default:
		return m.help.View(m.keys)
	}
}

func (m *model) messageContent() string {
	m.app.mu.Lock()
	defer m.app.mu.Unlock()
	if m.app.current == nil {
		return ""
	}
	body := m.app.current.Text
	if body == "" {
		body = "[HTML message converted to text]\n" + mailx.Readable(m.app.current.HTML)
	}
	return body
}

func (m *model) rawContent() string {
	m.app.mu.Lock()
	defer m.app.mu.Unlock()
	if m.app.current == nil {
		return ""
	}
	return m.app.current.Text + "\n\n" + m.app.current.HTML
}

var _ tea.Model = (*model)(nil)
var _ help.KeyMap = keyMap{}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Open, k.Copy, k.CopyAddress, k.Search, k.Help, k.Quit}
}
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Back, k.Open, k.First, k.Last, k.HalfDown, k.HalfUp},
		{k.Copy, k.CopyAddress, k.Actions, k.Search, k.New, k.Refresh, k.Raw, k.Help, k.Quit},
	}
}
