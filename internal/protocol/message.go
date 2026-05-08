package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
)

const (
	TypeAuth         = "auth"
	TypeAuthed       = "authed"
	TypePing         = "ping"
	TypePong         = "pong"
	TypeEnqueue      = "enqueue_match"
	TypeQueued       = "match_queued"
	TypeCancel       = "cancel_match"
	TypeCancelled    = "match_cancelled"
	TypeStatus       = "match_status"
	TypeFound        = "match_found"
	TypeServerNotice = "server_notice"
	TypeMaintenance  = "maintenance_state"
	TypeError        = "error"
	DefaultMode      = "duel"
	DefaultMaxWait   = int64(30000)
	DefaultMMRScore  = int64(1000)
)

type Message struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	PlayerID  string `json:"player_id,omitempty"`
	Token     string `json:"token,omitempty"`
	MMRScore  int64  `json:"mmr_score,omitempty"`
	MatchMode string `json:"match_mode,omitempty"`
	MaxWaitMS int64  `json:"max_wait_ms,omitempty"`
	TicketID  string `json:"ticket_id,omitempty"`
}

type Response struct {
	Type        string   `json:"type"`
	RequestID   string   `json:"request_id,omitempty"`
	PlayerID    string   `json:"player_id,omitempty"`
	TicketID    string   `json:"ticket_id,omitempty"`
	Status      string   `json:"status,omitempty"`
	MatchID     string   `json:"match_id,omitempty"`
	RoomID      string   `json:"room_id,omitempty"`
	ServerID    string   `json:"server_id,omitempty"`
	ServerAddr  string   `json:"server_addr,omitempty"`
	Players     []string `json:"players,omitempty"`
	Maintenance bool     `json:"maintenance,omitempty"`
	Message     string   `json:"message,omitempty"`
}

func Decode(data []byte) (Message, error) {
	var msg Message
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&msg); err != nil {
		return msg, err
	}
	if msg.Type == "" {
		return msg, errors.New("type is required")
	}
	return msg, nil
}

func NormalizeEnqueue(msg Message) Message {
	if msg.MatchMode == "" {
		msg.MatchMode = DefaultMode
	}
	if msg.MaxWaitMS <= 0 {
		msg.MaxWaitMS = DefaultMaxWait
	}
	if msg.MMRScore <= 0 {
		msg.MMRScore = DefaultMMRScore
	}
	return msg
}

func ErrorResponse(requestID string, message string) Response {
	return Response{
		Type:      TypeError,
		RequestID: requestID,
		Message:   message,
	}
}
