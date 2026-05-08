package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jingjie2002/ArenaGate/internal/config"
	"github.com/jingjie2002/ArenaGate/internal/coreclient"
	"github.com/jingjie2002/ArenaGate/internal/protocol"
)

type Server struct {
	cfg      config.Config
	core     coreclient.Client
	sessions *SessionManager
	metrics  *Metrics
}

func NewServer(cfg config.Config, core coreclient.Client) *Server {
	return &Server{
		cfg:      cfg,
		core:     core,
		sessions: NewSessionManager(),
		metrics:  NewMetrics(),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":          "ok",
			"active_sessions": s.sessions.ActiveCount(),
			"maintenance":     s.cfg.MaintenanceEnabled,
		})
	})
	mux.Handle("/metrics", s.metrics)
	mux.HandleFunc("/ws", s.handleWebSocket)
	return mux
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := acceptWebSocket(w, r, s.cfg.MaxMessageBytes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer conn.Close()

	session := s.sessions.Add(r.RemoteAddr, s.cfg.SessionRateLimit)
	s.metrics.OpenSession()
	defer func() {
		s.sessions.Remove(session)
		s.metrics.CloseSession()
	}()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	for {
		_ = conn.SetReadDeadline(time.Now().Add(s.cfg.IdleTimeout))
		var msg protocol.Message
		if err := conn.ReadJSON(&msg); err != nil {
			if errors.Is(err, io.EOF) || strings.Contains(err.Error(), "use of closed network connection") {
				return
			}
			s.writeError(conn, "", err.Error())
			return
		}
		session.Touch()
		s.metrics.Message()

		if !session.AllowMessage() {
			s.writeError(conn, msg.RequestID, "rate limit exceeded")
			continue
		}
		if err := s.handleMessage(ctx, conn, session, msg); err != nil {
			s.writeError(conn, msg.RequestID, err.Error())
		}
	}
}

func (s *Server) handleMessage(ctx context.Context, conn *wsConn, session *Session, msg protocol.Message) error {
	switch msg.Type {
	case protocol.TypeAuth:
		return s.handleAuth(conn, session, msg)
	case protocol.TypePing:
		return conn.WriteJSON(protocol.Response{Type: protocol.TypePong, RequestID: msg.RequestID})
	case protocol.TypeEnqueue:
		return s.handleEnqueue(ctx, conn, session, msg)
	case protocol.TypeCancel:
		return s.handleCancel(ctx, conn, session, msg)
	case protocol.TypeStatus:
		return s.handleStatus(ctx, conn, session, msg)
	default:
		return errors.New("unsupported message type: " + msg.Type)
	}
}

func (s *Server) handleAuth(conn *wsConn, session *Session, msg protocol.Message) error {
	if msg.PlayerID == "" {
		return errors.New("player_id is required")
	}
	if msg.Token != s.cfg.AuthTokenPrefix+msg.PlayerID {
		return errors.New("invalid token for mock auth")
	}
	s.sessions.BindPlayer(session, msg.PlayerID)
	if err := conn.WriteJSON(protocol.Response{
		Type:      protocol.TypeAuthed,
		RequestID: msg.RequestID,
		PlayerID:  msg.PlayerID,
	}); err != nil {
		return err
	}
	return s.pushOperationalState(conn, msg.RequestID)
}

func (s *Server) handleEnqueue(ctx context.Context, conn *wsConn, session *Session, msg protocol.Message) error {
	playerID, authed := session.Player()
	if !authed {
		return errors.New("auth is required before enqueue_match")
	}
	if s.cfg.MaintenanceEnabled {
		return conn.WriteJSON(protocol.Response{
			Type:        protocol.TypeMaintenance,
			RequestID:   msg.RequestID,
			PlayerID:    playerID,
			Status:      "enabled",
			Maintenance: true,
			Message:     s.maintenanceMessage(),
		})
	}

	msg = protocol.NormalizeEnqueue(msg)
	ticket, err := s.core.CreateTicket(ctx, coreclient.CreateTicketRequest{
		PlayerID:  playerID,
		MMRScore:  msg.MMRScore,
		MatchMode: msg.MatchMode,
		MaxWaitMS: msg.MaxWaitMS,
	})
	s.metrics.CoreRequest(err)
	if err != nil {
		return err
	}

	session.SetPending(ticket.TicketID)
	if isMatched(ticket) {
		return s.pushMatch(ctx, conn, session, msg.RequestID, ticket)
	}

	if err := conn.WriteJSON(protocol.Response{
		Type:      protocol.TypeQueued,
		RequestID: msg.RequestID,
		PlayerID:  playerID,
		TicketID:  ticket.TicketID,
		Status:    ticket.Status,
	}); err != nil {
		return err
	}

	wait := time.Duration(msg.MaxWaitMS)*time.Millisecond + 5*time.Second
	go s.pollMatch(ctx, conn, session, msg.RequestID, ticket.TicketID, wait)
	return nil
}

func (s *Server) handleCancel(ctx context.Context, conn *wsConn, session *Session, msg protocol.Message) error {
	_, authed := session.Player()
	if !authed {
		return errors.New("auth is required before cancel_match")
	}
	ticketID := msg.TicketID
	if ticketID == "" {
		ticketID = session.GetPending()
	}
	if ticketID == "" {
		return errors.New("ticket_id is required")
	}
	ticket, err := s.core.CancelTicket(ctx, ticketID)
	s.metrics.CoreRequest(err)
	if err != nil {
		return err
	}
	session.SetPending("")
	return conn.WriteJSON(protocol.Response{
		Type:      protocol.TypeCancelled,
		RequestID: msg.RequestID,
		TicketID:  ticket.TicketID,
		Status:    ticket.Status,
	})
}

func (s *Server) handleStatus(ctx context.Context, conn *wsConn, session *Session, msg protocol.Message) error {
	_, authed := session.Player()
	if !authed {
		return errors.New("auth is required before match_status")
	}
	ticketID := msg.TicketID
	if ticketID == "" {
		ticketID = session.GetPending()
	}
	if ticketID == "" {
		return errors.New("ticket_id is required")
	}
	ticket, err := s.core.GetTicket(ctx, ticketID)
	s.metrics.CoreRequest(err)
	if err != nil {
		return err
	}
	if isMatched(ticket) {
		return s.pushMatch(ctx, conn, session, msg.RequestID, ticket)
	}
	return conn.WriteJSON(protocol.Response{
		Type:      protocol.TypeStatus,
		RequestID: msg.RequestID,
		TicketID:  ticket.TicketID,
		Status:    ticket.Status,
		MatchID:   ticket.MatchID,
		RoomID:    ticket.RoomID,
	})
}

func (s *Server) pollMatch(ctx context.Context, conn *wsConn, session *Session, requestID string, ticketID string, maxWait time.Duration) {
	ticker := time.NewTicker(s.cfg.MatchPollInterval)
	defer ticker.Stop()
	timer := time.NewTimer(maxWait)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			_ = conn.WriteJSON(protocol.Response{
				Type:      protocol.TypeStatus,
				RequestID: requestID,
				TicketID:  ticketID,
				Status:    "waiting",
				Message:   "match polling timeout in ArenaGate",
			})
			return
		case <-ticker.C:
			ticket, err := s.core.GetTicket(ctx, ticketID)
			s.metrics.CoreRequest(err)
			if err != nil {
				s.writeError(conn, requestID, err.Error())
				continue
			}
			if isMatched(ticket) {
				_ = s.pushMatch(ctx, conn, session, requestID, ticket)
				return
			}
			if ticket.Status == "cancelled" || ticket.Status == "timeout" {
				session.SetPending("")
				_ = conn.WriteJSON(protocol.Response{
					Type:      protocol.TypeStatus,
					RequestID: requestID,
					TicketID:  ticket.TicketID,
					Status:    ticket.Status,
				})
				return
			}
		}
	}
}

func (s *Server) pushMatch(ctx context.Context, conn *wsConn, session *Session, requestID string, ticket *coreclient.Ticket) error {
	response := protocol.Response{
		Type:      protocol.TypeFound,
		RequestID: requestID,
		TicketID:  ticket.TicketID,
		Status:    ticket.Status,
		MatchID:   ticket.MatchID,
		RoomID:    ticket.RoomID,
	}

	if ticket.MatchID != "" {
		result, err := s.core.GetResult(ctx, ticket.MatchID)
		s.metrics.CoreRequest(err)
		if err != nil {
			return err
		}
		response.MatchID = result.MatchID
		response.RoomID = result.RoomID
		response.ServerID = result.ServerID
		response.ServerAddr = result.ServerAddr
		response.Players = result.PlayerIDs
		response.Status = result.Status
	}

	session.SetPending("")
	s.metrics.MatchFound()
	return conn.WriteJSON(response)
}

func (s *Server) writeError(conn *wsConn, requestID string, message string) {
	s.metrics.Error()
	_ = conn.WriteJSON(protocol.ErrorResponse(requestID, message))
}

func (s *Server) pushOperationalState(conn *wsConn, requestID string) error {
	if s.cfg.ServerNotice != "" {
		if err := conn.WriteJSON(protocol.Response{
			Type:      protocol.TypeServerNotice,
			RequestID: requestID,
			Message:   s.cfg.ServerNotice,
		}); err != nil {
			return err
		}
	}
	if s.cfg.MaintenanceEnabled {
		return conn.WriteJSON(protocol.Response{
			Type:        protocol.TypeMaintenance,
			RequestID:   requestID,
			Status:      "enabled",
			Maintenance: true,
			Message:     s.maintenanceMessage(),
		})
	}
	return nil
}

func (s *Server) maintenanceMessage() string {
	if s.cfg.MaintenanceMessage != "" {
		return s.cfg.MaintenanceMessage
	}
	return "server is under maintenance"
}

func isMatched(ticket *coreclient.Ticket) bool {
	return ticket != nil && (ticket.Status == "matched" || ticket.MatchID != "")
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = jsonNewEncoder(w).Encode(payload)
}
