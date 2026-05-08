package coreclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type CreateTicketRequest struct {
	PlayerID  string `json:"player_id"`
	MMRScore  int64  `json:"mmr_score"`
	MatchMode string `json:"match_mode"`
	MaxWaitMS int64  `json:"max_wait_ms"`
}

type Ticket struct {
	TicketID  string `json:"TicketID"`
	PlayerID  string `json:"PlayerID"`
	MMRScore  int64  `json:"MMRScore"`
	MatchMode string `json:"MatchMode"`
	Status    string `json:"Status"`
	MatchID   string `json:"MatchID"`
	RoomID    string `json:"RoomID"`
	CreatedAt int64  `json:"CreatedAt"`
	UpdatedAt int64  `json:"UpdatedAt"`
	ExpiresAt int64  `json:"ExpiresAt"`
}

type MatchResult struct {
	MatchID    string   `json:"MatchID"`
	RoomID     string   `json:"RoomID"`
	ServerID   string   `json:"ServerID"`
	ServerAddr string   `json:"ServerAddr"`
	MatchMode  string   `json:"MatchMode"`
	PlayerIDs  []string `json:"PlayerIDs"`
	Status     string   `json:"Status"`
	CreatedAt  int64    `json:"CreatedAt"`
}

type Client interface {
	CreateTicket(ctx context.Context, req CreateTicketRequest) (*Ticket, error)
	GetTicket(ctx context.Context, ticketID string) (*Ticket, error)
	CancelTicket(ctx context.Context, ticketID string) (*Ticket, error)
	GetResult(ctx context.Context, matchID string) (*MatchResult, error)
}

type HTTPClient struct {
	baseURL string
	client  *http.Client
}

func NewHTTPClient(baseURL string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *HTTPClient) CreateTicket(ctx context.Context, req CreateTicketRequest) (*Ticket, error) {
	var ticket Ticket
	if err := c.doJSON(ctx, http.MethodPost, "/api/match/tickets", req, &ticket); err != nil {
		return nil, err
	}
	return &ticket, nil
}

func (c *HTTPClient) GetTicket(ctx context.Context, ticketID string) (*Ticket, error) {
	var ticket Ticket
	if err := c.doJSON(ctx, http.MethodGet, "/api/match/tickets/"+ticketID, nil, &ticket); err != nil {
		return nil, err
	}
	return &ticket, nil
}

func (c *HTTPClient) CancelTicket(ctx context.Context, ticketID string) (*Ticket, error) {
	var ticket Ticket
	if err := c.doJSON(ctx, http.MethodDelete, "/api/match/tickets/"+ticketID, nil, &ticket); err != nil {
		return nil, err
	}
	return &ticket, nil
}

func (c *HTTPClient) GetResult(ctx context.Context, matchID string) (*MatchResult, error) {
	var result MatchResult
	if err := c.doJSON(ctx, http.MethodGet, "/api/match/results/"+matchID, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *HTTPClient) doJSON(ctx context.Context, method string, path string, payload any, target any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("CoreRank %s %s failed: status=%d body=%s", method, path, response.StatusCode, string(data))
	}

	if target == nil {
		return nil
	}
	return json.NewDecoder(response.Body).Decode(target)
}
