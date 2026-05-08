package gateway

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

type Metrics struct {
	activeSessions    atomic.Int64
	connectionsTotal  atomic.Int64
	messagesTotal     atomic.Int64
	errorsTotal       atomic.Int64
	coreRequestsTotal atomic.Int64
	coreErrorsTotal   atomic.Int64
	matchFoundTotal   atomic.Int64
}

func NewMetrics() *Metrics {
	return &Metrics{}
}

func (m *Metrics) OpenSession() {
	m.activeSessions.Add(1)
	m.connectionsTotal.Add(1)
}

func (m *Metrics) CloseSession() {
	m.activeSessions.Add(-1)
}

func (m *Metrics) Message() {
	m.messagesTotal.Add(1)
}

func (m *Metrics) Error() {
	m.errorsTotal.Add(1)
}

func (m *Metrics) CoreRequest(err error) {
	m.coreRequestsTotal.Add(1)
	if err != nil {
		m.coreErrorsTotal.Add(1)
	}
}

func (m *Metrics) MatchFound() {
	m.matchFoundTotal.Add(1)
}

func (m *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP arenagate_active_sessions Current active gateway sessions.\n")
	fmt.Fprintf(w, "# TYPE arenagate_active_sessions gauge\n")
	fmt.Fprintf(w, "arenagate_active_sessions %d\n", m.activeSessions.Load())
	fmt.Fprintf(w, "# HELP arenagate_connections_total Total accepted WebSocket connections.\n")
	fmt.Fprintf(w, "# TYPE arenagate_connections_total counter\n")
	fmt.Fprintf(w, "arenagate_connections_total %d\n", m.connectionsTotal.Load())
	fmt.Fprintf(w, "# HELP arenagate_messages_total Total client messages handled by ArenaGate.\n")
	fmt.Fprintf(w, "# TYPE arenagate_messages_total counter\n")
	fmt.Fprintf(w, "arenagate_messages_total %d\n", m.messagesTotal.Load())
	fmt.Fprintf(w, "# HELP arenagate_errors_total Total protocol or handling errors returned to clients.\n")
	fmt.Fprintf(w, "# TYPE arenagate_errors_total counter\n")
	fmt.Fprintf(w, "arenagate_errors_total %d\n", m.errorsTotal.Load())
	fmt.Fprintf(w, "# HELP arenagate_core_requests_total Total requests sent from ArenaGate to CoreRank.\n")
	fmt.Fprintf(w, "# TYPE arenagate_core_requests_total counter\n")
	fmt.Fprintf(w, "arenagate_core_requests_total %d\n", m.coreRequestsTotal.Load())
	fmt.Fprintf(w, "# HELP arenagate_core_errors_total Total failed requests from ArenaGate to CoreRank.\n")
	fmt.Fprintf(w, "# TYPE arenagate_core_errors_total counter\n")
	fmt.Fprintf(w, "arenagate_core_errors_total %d\n", m.coreErrorsTotal.Load())
	fmt.Fprintf(w, "# HELP arenagate_match_found_total Total match_found messages sent to clients.\n")
	fmt.Fprintf(w, "# TYPE arenagate_match_found_total counter\n")
	fmt.Fprintf(w, "arenagate_match_found_total %d\n", m.matchFoundTotal.Load())
}
