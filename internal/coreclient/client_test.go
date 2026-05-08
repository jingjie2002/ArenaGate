package coreclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPClientCreateTicket(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/match/tickets" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var req CreateTicketRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.PlayerID != "p1" || req.MatchMode != "duel" {
			t.Fatalf("unexpected payload: %#v", req)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(Ticket{
			TicketID: "ticket_1",
			PlayerID: "p1",
			Status:   "queued",
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, time.Second)
	ticket, err := client.CreateTicket(context.Background(), CreateTicketRequest{
		PlayerID:  "p1",
		MMRScore:  1200,
		MatchMode: "duel",
		MaxWaitMS: 30000,
	})
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	if ticket.TicketID != "ticket_1" || ticket.Status != "queued" {
		t.Fatalf("unexpected ticket: %#v", ticket)
	}
}
