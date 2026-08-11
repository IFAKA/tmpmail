package tui

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/IFAKA/tmpmail/internal/clipboard"
	mailx "github.com/IFAKA/tmpmail/internal/mail"
	"github.com/IFAKA/tmpmail/internal/provider"
)

type screen int

const (
	screenInbox screen = iota
	screenMessage
	screenActions
)

type App struct {
	p             provider.Provider
	mu            sync.Mutex
	mb            provider.Mailbox
	msgs          []provider.MessageSummary
	cursor        int
	current       *provider.Message
	actions       []mailx.ActionCandidate
	actionCursor  int
	screen        screen
	status        string
	providerState string
	quit          chan struct{}
}

func New(p provider.Provider) *App {
	return &App{p: p, status: "starting...", providerState: "LOADING", quit: make(chan struct{})}
}

func (a *App) Run() error {
	restore, err := rawTerminal()
	if err != nil {
		return err
	}
	defer restore()
	fmt.Print("\x1b[?1049h\x1b[?25l")
	defer fmt.Print("\x1b[?25h\x1b[?1049l")

	if err := a.createMailbox(); err != nil {
		a.providerState = "FATAL"
		a.status = err.Error()
	}
	go a.pollLoop()
	a.draw()

	r := bufio.NewReader(os.Stdin)
	for {
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		key := string(b)
		if b == 27 { // Esc or arrow escape sequence
			if r.Buffered() >= 2 {
				b2, _ := r.ReadByte()
				b3, _ := r.ReadByte()
				if b2 == '[' {
					if b3 == 'A' {
						key = "k"
					}
					if b3 == 'B' {
						key = "j"
					}
				}
			} else {
				key = "esc"
			}
		}
		if a.handle(key) {
			return nil
		}
		a.draw()
	}
}

func rawTerminal() (func(), error) {
	if _, err := exec.LookPath("stty"); err != nil {
		return nil, fmt.Errorf("stty required: %w", err)
	}
	old, err := exec.Command("stty", "-g").Output()
	if err != nil {
		return nil, err
	}
	if err := exec.Command("stty", "raw", "-echo").Run(); err != nil {
		return nil, err
	}
	state := strings.TrimSpace(string(old))
	return func() { _ = exec.Command("stty", state).Run() }, nil
}

func (a *App) createMailbox() error {
	a.mu.Lock()
	a.providerState = "LOADING"
	a.status = "creating mailbox..."
	a.mu.Unlock()
	a.draw()
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
		a.mu.Unlock()
	}
	return nil
}

func (a *App) pollLoop() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.refresh()
			a.draw()
		case <-a.quit:
			return
		}
	}
}

func (a *App) refresh() {
	a.mu.Lock()
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
		a.providerState = "OFFLINE"
		a.status = "offline · cached mail available · " + err.Error()
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
		close(a.quit)
		return true
	}
	switch a.screen {
	case screenInbox:
		switch k {
		case "q":
			close(a.quit)
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
		case "l", "\r":
			if len(a.msgs) > 0 {
				id := a.msgs[a.cursor].ID
				mb := a.mb
				a.mu.Unlock()
				m, err := a.p.GetMessage(context.Background(), mb, id)
				a.mu.Lock()
				if err != nil {
					a.status = "open failed: " + err.Error()
				} else {
					a.current = &m
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
			}
		case "r":
			go func() { a.refresh(); a.draw() }()
		case "n":
			go func() {
				if err := a.createMailbox(); err != nil {
					a.mu.Lock()
					a.providerState = "FATAL"
					a.status = err.Error()
					a.mu.Unlock()
				}
				a.draw()
			}()
		case "?":
			a.status = "j/k move · g/G first/last · l open · Y address · n new · r refresh · q quit"
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
			} else if len(a.actions) == 1 || (a.actions[0].Score >= 80 && a.actions[0].Score-a.actions[1].Score >= 20) {
				a.copyAction(a.actions[0])
			} else {
				a.screen = screenActions
			}
		case "?":
			a.status = "h/Esc back · y smart-copy · a actions · q back"
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
		}
	}
	return false
}

func (a *App) copyAction(x mailx.ActionCandidate) {
	if err := clipboard.Copy(x.Value); err != nil {
		a.status = "clipboard unavailable · value: " + x.Value
	} else {
		a.status = "copied " + string(x.Type) + ": " + x.Value
	}
}

func (a *App) draw() {
	a.mu.Lock()
	defer a.mu.Unlock()
	w, h := terminalSize()
	if w < 32 || h < 6 {
		fmt.Print("\x1b[2J\x1b[Hterminal too small\r\nneed ≥ 32×6\r\n\r\nq quit")
		return
	}
	var b strings.Builder
	switch a.screen {
	case screenInbox:
		fmt.Fprintf(&b, "tmpmail  %s  [%s]\n\n", blank(a.mb.Address, "creating..."), a.providerState)
		if a.providerState == "LOADING" {
			b.WriteString("Creating temporary mailbox...\n")
		} else if len(a.msgs) == 0 {
			b.WriteString("Waiting for mail...\n")
		} else {
			for i, m := range a.msgs {
				mark := " "
				if i == a.cursor {
					mark = ">"
				}
				unread := " "
				if !m.Seen {
					unread = "●"
				}
				fmt.Fprintf(&b, "%s %s %-16s %s\n", mark, unread, cut(m.From, 16), cut(m.Subject, max(12, w-24)))
			}
		}
		fmt.Fprintf(&b, "\n%s\n%s", cut(a.status, w), "j/k move · l open · Y address · n new · r refresh · ?")
	case screenMessage:
		m := a.current
		fmt.Fprintf(&b, "%s\nFrom: %s\n\n", m.Subject, m.From)
		body := m.Text
		if strings.TrimSpace(body) == "" {
			body = "[HTML-only message]\n" + stripView(m.HTML)
		}
		b.WriteString(wrap(body, w, h-8))
		b.WriteString("\n\n")
		if len(a.actions) > 0 {
			fmt.Fprintf(&b, "best: %s  (%d)\n", cut(a.actions[0].Value, w-16), a.actions[0].Score)
		}
		b.WriteString("h/Esc back · y smart-copy · a actions · ?")
	case screenActions:
		b.WriteString("Detected actions\n\n")
		for i, x := range a.actions {
			mark := " "
			if i == a.actionCursor {
				mark = ">"
			}
			fmt.Fprintf(&b, "%s %-4s %-32s %3d  %s\n", mark, x.Type, cut(x.Value, 32), x.Score, cut(x.Evidence, max(8, w-48)))
		}
		b.WriteString("\nj/k move · y copy · Esc cancel")
	}
	fmt.Print("\x1b[2J\x1b[H" + strings.ReplaceAll(b.String(), "\n", "\r\n"))
}

func terminalSize() (int, int) {
	out, err := exec.Command("stty", "size").Output()
	if err != nil {
		return 80, 24
	}
	p := strings.Fields(string(out))
	if len(p) != 2 {
		return 80, 24
	}
	h, _ := strconv.Atoi(p[0])
	w, _ := strconv.Atoi(p[1])
	return w, h
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
	words := strings.Fields(s)
	if w < 10 {
		w = 10
	}
	var lines []string
	var line string
	for _, x := range words {
		if len([]rune(line))+1+len([]rune(x)) > w {
			lines = append(lines, line)
			line = x
		} else if line == "" {
			line = x
		} else {
			line += " " + x
		}
	}
	if line != "" {
		lines = append(lines, line)
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
