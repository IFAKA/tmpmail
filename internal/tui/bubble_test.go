package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	mailx "github.com/IFAKA/tmpmail/internal/mail"
	"github.com/IFAKA/tmpmail/internal/provider"
	tea "github.com/charmbracelet/bubbletea"
)

type fakeProvider struct {
	list []provider.MessageSummary
	err  error
}

func (f *fakeProvider) CreateMailbox(context.Context) (provider.Mailbox, error) {
	return provider.Mailbox{ID: "mb", Address: "x@example.test", Token: "token"}, nil
}
func (f *fakeProvider) ListMessages(context.Context, provider.Mailbox) ([]provider.MessageSummary, error) {
	return f.list, f.err
}
func (f *fakeProvider) GetMessage(context.Context, provider.Mailbox, string) (provider.Message, error) {
	return provider.Message{}, errors.New("not implemented")
}
func (f *fakeProvider) DeleteMessage(context.Context, provider.Mailbox, string) error { return nil }

type fakeHTTPError struct {
	status int
	retry  time.Duration
}

func (e fakeHTTPError) Error() string             { return "fake provider error" }
func (e fakeHTTPError) StatusCode() int           { return e.status }
func (e fakeHTTPError) RetryAfter() time.Duration { return e.retry }

func keyMsg(s string) tea.KeyMsg {
	if s == "esc" {
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	if s == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestHelpSearchAndRawTransitions(t *testing.T) {
	a := New(nil)
	m := newModel(a)
	m.width, m.height = 80, 24

	m.Update(keyMsg("?"))
	if a.screen != screenHelp {
		t.Fatalf("? screen = %v, want help", a.screen)
	}
	m.Update(keyMsg("h"))
	if a.screen != screenInbox {
		t.Fatalf("help close screen = %v, want inbox", a.screen)
	}
	m.Update(keyMsg("/"))
	if a.screen != screenSearch {
		t.Fatalf("/ screen = %v, want search", a.screen)
	}
	m.Update(keyMsg("g"))
	if a.searchQuery != "g" {
		t.Fatalf("search query = %q, want g", a.searchQuery)
	}
	m.Update(keyMsg("esc"))
	if a.screen != screenInbox || a.searchQuery != "" {
		t.Fatalf("search close screen=%v query=%q", a.screen, a.searchQuery)
	}
}

func TestAmbiguousYOpensCandidatePicker(t *testing.T) {
	a := New(nil)
	a.screen = screenMessage
	a.current = &provider.Message{MessageSummary: provider.MessageSummary{ID: "1", Subject: "Choices"}, Text: "OTP codes: 482910 and 921847"}
	a.actions = mailx.Extract(a.current.Subject, a.current.Text, "")
	m := newModel(a)
	m.Update(keyMsg("y"))
	if a.screen != screenActions {
		t.Fatalf("ambiguous y screen = %v, actions=%#v, want actions", a.screen, a.actions)
	}
}

func TestRawViewClosesWithEscape(t *testing.T) {
	a := New(nil)
	a.screen = screenMessage
	a.current = &provider.Message{MessageSummary: provider.MessageSummary{ID: "1"}, Text: "raw"}
	m := newModel(a)
	m.Update(keyMsg("o"))
	if a.screen != screenRaw {
		t.Fatalf("o screen = %v, want raw", a.screen)
	}
	m.Update(keyMsg("esc"))
	if a.screen != screenMessage {
		t.Fatalf("raw escape screen = %v, want message", a.screen)
	}
}

func TestMessageViewportContainsMessageBody(t *testing.T) {
	a := New(nil)
	a.screen = screenMessage
	a.current = &provider.Message{MessageSummary: provider.MessageSummary{ID: "1"}, Text: "visible message"}
	m := newModel(a)
	m.width, m.height = 80, 24
	m.messageView.Width = 72
	m.messageView.Height = 14
	m.syncMessageViewport()
	if !strings.Contains(m.messageView.View(), "visible message") {
		t.Fatalf("viewport=%q", m.messageView.View())
	}
}

func TestRefreshPreservesSelectionAndCachedRowsOffline(t *testing.T) {
	p := &fakeProvider{list: []provider.MessageSummary{{ID: "1", From: "a"}, {ID: "2", From: "b"}, {ID: "3", From: "c"}}}
	a := New(p)
	a.mb = provider.Mailbox{Token: "token", Address: "inbox@example.test"}
	a.cursor = 2
	a.refresh()
	if a.providerState != "READY" || a.cursor != 2 {
		t.Fatalf("ready state=%q cursor=%d", a.providerState, a.cursor)
	}
	p.list = nil
	p.err = errors.New("offline")
	a.nextRefresh = time.Time{}
	a.refresh()
	if a.providerState != "OFFLINE" || len(a.msgs) != 3 || a.cursor != 2 {
		t.Fatalf("offline state=%q messages=%d cursor=%d", a.providerState, len(a.msgs), a.cursor)
	}
}

func TestRateLimitBlocksManualRefreshUntilRetryAfter(t *testing.T) {
	p := &fakeProvider{err: fakeHTTPError{status: 429, retry: time.Minute}}
	a := New(p)
	a.mb = provider.Mailbox{Token: "token"}
	a.refresh()
	if a.providerState != "RATE LIMITED" || time.Until(a.nextRefresh) < 50*time.Second {
		t.Fatalf("state=%q next=%s", a.providerState, a.nextRefresh)
	}
	before := a.nextRefresh
	a.refresh()
	if !a.nextRefresh.Equal(before) {
		t.Fatal("manual refresh bypassed Retry-After")
	}
}

func TestStatusExpiresAndTinyViewStaysReadable(t *testing.T) {
	a := New(nil)
	a.status = "copied value"
	a.statusUntil = time.Now().Add(-time.Second)
	if strings.Contains(a.view(80, 24, "q quit", ""), "copied value") {
		t.Fatal("expired status remained visible")
	}
	tiny := a.view(20, 5, "", "")
	if !strings.Contains(tiny, "terminal too small") || strings.Contains(tiny, "\x1b[") {
		t.Fatalf("tiny view=%q", tiny)
	}
}

func TestUnicodeAndLargeRawMessageRemainReadable(t *testing.T) {
	a := New(nil)
	a.screen = screenRaw
	a.current = &provider.Message{MessageSummary: provider.MessageSummary{ID: "unicode"}, Text: strings.Repeat("こんにちは café 😀\n", 100)}
	m := newModel(a)
	m.width, m.height = 40, 12
	m.syncRawViewport()
	if !strings.Contains(m.rawView.View(), "こんにちは") {
		t.Fatal("unicode raw content missing")
	}
	if m.rawView.TotalLineCount() < 50 {
		t.Fatalf("large content was truncated: %d lines", m.rawView.TotalLineCount())
	}
}
