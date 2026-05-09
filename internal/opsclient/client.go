package opsclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type State map[string]string

type PlayerState struct {
	PlayerID    string `json:"player_id"`
	Status      string `json:"status"`
	BanReason   string `json:"ban_reason"`
	BannedUntil int64  `json:"banned_until"`
}

type Client interface {
	OpsState(ctx context.Context) (State, error)
	PlayerState(ctx context.Context, playerID string) (*PlayerState, error)
}

type HTTPClient struct {
	baseURL string
	client  *http.Client
}

func NewHTTPClient(baseURL string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: timeout},
	}
}

func (c *HTTPClient) OpsState(ctx context.Context) (State, error) {
	var state State
	if err := c.doJSON(ctx, "/api/public/ops-state", &state); err != nil {
		return nil, err
	}
	return state, nil
}

func (c *HTTPClient) PlayerState(ctx context.Context, playerID string) (*PlayerState, error) {
	var state PlayerState
	if err := c.doJSON(ctx, "/api/public/players/"+playerID+"/state", &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (c *HTTPClient) doJSON(ctx context.Context, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("GameOps GET %s failed: status=%d", path, response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(target)
}
