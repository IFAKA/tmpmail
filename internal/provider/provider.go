package provider

import (
	"context"
	"time"
)

// HTTPError is implemented by providers that can report an HTTP failure
// without making the TUI depend on provider-specific types.
type HTTPError interface {
	error
	StatusCode() int
	RetryAfter() time.Duration
}

type Mailbox struct{ ID, Address, Password, Token string }
type MessageSummary struct {
	ID, From, Subject, Intro string
	Seen                     bool
	CreatedAt                time.Time
}
type Message struct {
	MessageSummary
	Text, HTML string
}

type Provider interface {
	CreateMailbox(context.Context) (Mailbox, error)
	ListMessages(context.Context, Mailbox) ([]MessageSummary, error)
	GetMessage(context.Context, Mailbox, string) (Message, error)
	DeleteMessage(context.Context, Mailbox, string) error
}
