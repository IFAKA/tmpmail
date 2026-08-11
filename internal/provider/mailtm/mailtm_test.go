package mailtm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/IFAKA/tmpmail/internal/provider"
)

func TestCreateMailboxAcceptsArrayAndHydraDomains(t *testing.T) {
	for _, tc := range []struct {
		name string
		body any
	}{
		{"array", []map[string]any{{"domain": "example.test", "isActive": true}}},
		{"hydra", map[string]any{"hydra:member": []map[string]any{{"domain": "example.test", "isActive": true}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/domains":
					_ = json.NewEncoder(w).Encode(tc.body)
				case "/accounts":
					_ = json.NewEncoder(w).Encode(map[string]string{"id": "id", "address": "tm@example.test"})
				case "/token":
					_ = json.NewEncoder(w).Encode(map[string]string{"token": "token"})
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			client := New()
			client.base = srv.URL
			mb, err := client.CreateMailbox(context.Background())
			if err != nil || mb.Token != "token" || mb.Address != "tm@example.test" {
				t.Fatalf("mailbox=%#v err=%v", mb, err)
			}
		})
	}
}

func TestListMessagesAcceptsArrayAndHydraMessages(t *testing.T) {
	for _, tc := range []struct {
		name string
		body any
	}{
		{"array", []map[string]any{{"id": "1", "subject": "Verify", "seen": false}}},
		{"hydra", map[string]any{"hydra:member": []map[string]any{{"id": "1", "subject": "Verify", "seen": false}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tc.body)
			}))
			defer srv.Close()
			client := New()
			client.base = srv.URL
			msgs, err := client.ListMessages(context.Background(), provider.Mailbox{Token: "token"})
			if err != nil || len(msgs) != 1 || msgs[0].ID != "1" {
				t.Fatalf("messages=%#v err=%v", msgs, err)
			}
		})
	}
}
