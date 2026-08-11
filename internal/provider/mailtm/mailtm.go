package mailtm

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/IFAKA/tmpmail/internal/provider"
)

const defaultBase = "https://api.mail.tm"

type Client struct {
	base string
	hc   *http.Client
}

func New() *Client { return &Client{base: defaultBase, hc: &http.Client{Timeout: 12 * time.Second}} }

type apiError struct {
	Status      int
	Body        string
	RetryAfterD time.Duration
}

func (e apiError) Error() string             { return fmt.Sprintf("mail.tm HTTP %d: %s", e.Status, e.Body) }
func (e apiError) StatusCode() int           { return e.Status }
func (e apiError) RetryAfter() time.Duration { return e.RetryAfterD }

func (c *Client) req(ctx context.Context, method, path, token string, body any, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if res.StatusCode < 200 || res.StatusCode > 204 {
		retryAfter := time.Duration(0)
		if value := res.Header.Get("Retry-After"); value != "" {
			if seconds, parseErr := strconv.Atoi(strings.TrimSpace(value)); parseErr == nil {
				retryAfter = time.Duration(seconds) * time.Second
			}
		}
		return apiError{Status: res.StatusCode, Body: strings.TrimSpace(string(b)), RetryAfterD: retryAfter}
	}
	if out != nil && len(b) > 0 {
		return json.Unmarshal(b, out)
	}
	return nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func (c *Client) CreateMailbox(ctx context.Context) (provider.Mailbox, error) {
	var domains json.RawMessage
	if err := c.req(ctx, http.MethodGet, "/domains?page=1", "", nil, &domains); err != nil {
		return provider.Mailbox{}, err
	}
	var members []struct {
		Domain   string `json:"domain"`
		IsActive bool   `json:"isActive"`
	}
	var collection struct {
		Members []struct {
			Domain   string `json:"domain"`
			IsActive bool   `json:"isActive"`
		} `json:"hydra:member"`
	}
	if err := json.Unmarshal(domains, &collection); err == nil && collection.Members != nil {
		members = collection.Members
	} else if err := json.Unmarshal(domains, &members); err != nil {
		return provider.Mailbox{}, fmt.Errorf("invalid domains response: %w", err)
	}
	domain := ""
	for _, d := range members {
		if d.IsActive {
			domain = d.Domain
			break
		}
	}
	if domain == "" && len(members) > 0 {
		domain = members[0].Domain
	}
	if domain == "" {
		return provider.Mailbox{}, errors.New("provider returned no domains")
	}
	address := "tm" + randomHex(5) + "@" + domain
	password := randomHex(16)
	var acct struct{ ID, Address string }
	if err := c.req(ctx, http.MethodPost, "/accounts", "", map[string]string{"address": address, "password": password}, &acct); err != nil {
		return provider.Mailbox{}, err
	}
	var tok struct {
		Token string `json:"token"`
	}
	if err := c.req(ctx, http.MethodPost, "/token", "", map[string]string{"address": address, "password": password}, &tok); err != nil {
		return provider.Mailbox{}, err
	}
	return provider.Mailbox{ID: acct.ID, Address: acct.Address, Password: password, Token: tok.Token}, nil
}

type rawSummary struct {
	ID, Subject, Intro string
	Seen               bool
	From               struct{ Address, Name string }
	CreatedAt          string `json:"createdAt"`
}

func sender(r rawSummary) string {
	if r.From.Name != "" {
		return r.From.Name
	}
	return r.From.Address
}
func parsedTime(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t }
func (c *Client) ListMessages(ctx context.Context, mb provider.Mailbox) ([]provider.MessageSummary, error) {
	var raw json.RawMessage
	if err := c.req(ctx, http.MethodGet, "/messages?page=1", mb.Token, nil, &raw); err != nil {
		return nil, err
	}
	var members []rawSummary
	var collection struct {
		Members []rawSummary `json:"hydra:member"`
	}
	if err := json.Unmarshal(raw, &collection); err == nil && collection.Members != nil {
		members = collection.Members
	} else if err := json.Unmarshal(raw, &members); err != nil {
		return nil, fmt.Errorf("invalid messages response: %w", err)
	}
	out := make([]provider.MessageSummary, 0, len(members))
	for _, m := range members {
		out = append(out, provider.MessageSummary{ID: m.ID, From: sender(m), Subject: m.Subject, Intro: m.Intro, Seen: m.Seen, CreatedAt: parsedTime(m.CreatedAt)})
	}
	return out, nil
}
func (c *Client) GetMessage(ctx context.Context, mb provider.Mailbox, id string) (provider.Message, error) {
	var r struct {
		rawSummary
		Text string
		HTML any
	}
	if err := c.req(ctx, http.MethodGet, "/messages/"+id, mb.Token, nil, &r); err != nil {
		return provider.Message{}, err
	}
	html := ""
	switch x := r.HTML.(type) {
	case string:
		html = x
	case []any:
		for _, p := range x {
			if s, ok := p.(string); ok {
				html += s
			}
		}
	}
	return provider.Message{MessageSummary: provider.MessageSummary{ID: r.ID, From: sender(r.rawSummary), Subject: r.Subject, Intro: r.Intro, Seen: r.Seen, CreatedAt: parsedTime(r.CreatedAt)}, Text: r.Text, HTML: html}, nil
}
func (c *Client) DeleteMessage(ctx context.Context, mb provider.Mailbox, id string) error {
	return c.req(ctx, http.MethodDelete, "/messages/"+id, mb.Token, nil, nil)
}
