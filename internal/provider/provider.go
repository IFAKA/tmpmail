package provider

import (
	"context"
	"time"
)

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
