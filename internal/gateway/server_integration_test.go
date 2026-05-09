package gateway

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jingjie2002/ArenaGate/internal/config"
	"github.com/jingjie2002/ArenaGate/internal/coreclient"
	"github.com/jingjie2002/ArenaGate/internal/opsclient"
	"github.com/jingjie2002/ArenaGate/internal/protocol"
)

func TestWebSocketMatchFoundFlow(t *testing.T) {
	core := newFakeMatchCore()
	server := NewServer(config.Config{
		AuthTokenPrefix:   "dev-token:",
		IdleTimeout:       5 * time.Second,
		MatchPollInterval: 10 * time.Millisecond,
		MaxMessageBytes:   32768,
		SessionRateLimit:  20,
	}, core)

	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	p1 := newTestWSClient(t, httpServer.URL, "/ws")
	defer p1.close()
	p2 := newTestWSClient(t, httpServer.URL, "/ws")
	defer p2.close()

	p1.send(t, protocol.Message{
		Type:      protocol.TypeAuth,
		RequestID: "p1-auth",
		PlayerID:  "p1",
		Token:     "dev-token:p1",
	})
	assertResponseType(t, p1.recv(t), protocol.TypeAuthed)

	p2.send(t, protocol.Message{
		Type:      protocol.TypeAuth,
		RequestID: "p2-auth",
		PlayerID:  "p2",
		Token:     "dev-token:p2",
	})
	assertResponseType(t, p2.recv(t), protocol.TypeAuthed)

	p1.send(t, protocol.Message{
		Type:      protocol.TypeEnqueue,
		RequestID: "p1-match",
		MMRScore:  1200,
		MatchMode: "duel",
		MaxWaitMS: 1000,
	})
	queued := p1.recv(t)
	assertResponseType(t, queued, protocol.TypeQueued)
	if queued.TicketID != "ticket_1" {
		t.Fatalf("unexpected queued ticket id: %#v", queued)
	}

	p2.send(t, protocol.Message{
		Type:      protocol.TypeEnqueue,
		RequestID: "p2-match",
		MMRScore:  1210,
		MatchMode: "duel",
		MaxWaitMS: 1000,
	})

	p2Found := recvType(t, p2, protocol.TypeFound)
	p1Found := recvType(t, p1, protocol.TypeFound)
	for _, resp := range []protocol.Response{p1Found, p2Found} {
		if resp.MatchID != "match_1" || resp.RoomID != "room_1" {
			t.Fatalf("unexpected match fields: %#v", resp)
		}
		if resp.ServerID != "roomserver-1" || resp.ServerAddr != "127.0.0.1:7001" {
			t.Fatalf("unexpected server assignment: %#v", resp)
		}
		if !reflect.DeepEqual(resp.Players, []string{"p1", "p2"}) {
			t.Fatalf("unexpected matched players: %#v", resp.Players)
		}
	}
}

func TestOperationalNoticeAndMaintenanceBlockMatchEntry(t *testing.T) {
	core := newFakeMatchCore()
	server := NewServer(config.Config{
		AuthTokenPrefix:    "dev-token:",
		ServerNotice:       "SS25 season is live",
		MaintenanceEnabled: true,
		MaintenanceMessage: "ranked queue is temporarily closed",
		IdleTimeout:        5 * time.Second,
		MatchPollInterval:  10 * time.Millisecond,
		MaxMessageBytes:    32768,
		SessionRateLimit:   20,
	}, core)

	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	client := newTestWSClient(t, httpServer.URL, "/ws")
	defer client.close()

	client.send(t, protocol.Message{
		Type:      protocol.TypeAuth,
		RequestID: "auth",
		PlayerID:  "p1",
		Token:     "dev-token:p1",
	})
	assertResponseType(t, client.recv(t), protocol.TypeAuthed)
	notice := client.recv(t)
	assertResponseType(t, notice, protocol.TypeServerNotice)
	if notice.Message != "SS25 season is live" {
		t.Fatalf("unexpected server notice: %#v", notice)
	}
	maintenance := client.recv(t)
	assertResponseType(t, maintenance, protocol.TypeMaintenance)
	if !maintenance.Maintenance || maintenance.Message != "ranked queue is temporarily closed" {
		t.Fatalf("unexpected maintenance state: %#v", maintenance)
	}

	client.send(t, protocol.Message{
		Type:      protocol.TypeEnqueue,
		RequestID: "enqueue",
		MMRScore:  1200,
	})
	blocked := client.recv(t)
	assertResponseType(t, blocked, protocol.TypeMaintenance)
	if !blocked.Maintenance || blocked.Status != "enabled" {
		t.Fatalf("expected maintenance block, got %#v", blocked)
	}
	if core.CreateCount() != 0 {
		t.Fatalf("maintenance mode should block CoreRank ticket creation")
	}
}

func TestGameOpsStateBlocksMatchEntry(t *testing.T) {
	core := newFakeMatchCore()
	server := NewServer(config.Config{
		AuthTokenPrefix:   "dev-token:",
		IdleTimeout:       5 * time.Second,
		MatchPollInterval: 10 * time.Millisecond,
		MaxMessageBytes:   32768,
		SessionRateLimit:  20,
	}, core)
	server.SetOpsClient(&fakeOpsClient{
		state: opsclient.State{
			"announcement":       "GameOps announcement",
			"ranked_maintenance": "true",
		},
	})

	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	client := newTestWSClient(t, httpServer.URL, "/ws")
	defer client.close()

	client.send(t, protocol.Message{
		Type:      protocol.TypeAuth,
		RequestID: "auth",
		PlayerID:  "p1",
		Token:     "dev-token:p1",
	})
	assertResponseType(t, client.recv(t), protocol.TypeAuthed)
	notice := client.recv(t)
	assertResponseType(t, notice, protocol.TypeServerNotice)
	if notice.Message != "GameOps announcement" {
		t.Fatalf("unexpected GameOps notice: %#v", notice)
	}

	client.send(t, protocol.Message{
		Type:      protocol.TypeEnqueue,
		RequestID: "enqueue",
		MMRScore:  1200,
	})
	blocked := client.recv(t)
	assertResponseType(t, blocked, protocol.TypeMaintenance)
	if core.CreateCount() != 0 {
		t.Fatalf("GameOps maintenance should block CoreRank ticket creation")
	}
}

func TestGameOpsBannedPlayerCannotAuth(t *testing.T) {
	core := newFakeMatchCore()
	server := NewServer(config.Config{
		AuthTokenPrefix:   "dev-token:",
		IdleTimeout:       5 * time.Second,
		MatchPollInterval: 10 * time.Millisecond,
		MaxMessageBytes:   32768,
		SessionRateLimit:  20,
	}, core)
	server.SetOpsClient(&fakeOpsClient{
		players: map[string]*opsclient.PlayerState{
			"p1": {PlayerID: "p1", Status: "banned", BanReason: "abuse_report"},
		},
	})

	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	client := newTestWSClient(t, httpServer.URL, "/ws")
	defer client.close()

	client.send(t, protocol.Message{
		Type:      protocol.TypeAuth,
		RequestID: "auth",
		PlayerID:  "p1",
		Token:     "dev-token:p1",
	})
	resp := client.recv(t)
	assertResponseType(t, resp, protocol.TypeError)
	if resp.Message == "" {
		t.Fatalf("expected banned error message")
	}
}

type fakeMatchCore struct {
	mu      sync.Mutex
	tickets map[string]*coreclient.Ticket
	result  *coreclient.MatchResult
	nextID  int
}

func newFakeMatchCore() *fakeMatchCore {
	return &fakeMatchCore{
		tickets: make(map[string]*coreclient.Ticket),
	}
}

func (f *fakeMatchCore) CreateTicket(_ context.Context, req coreclient.CreateTicketRequest) (*coreclient.Ticket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.nextID++
	ticketID := "ticket_" + formatInt(int64(f.nextID))
	ticket := &coreclient.Ticket{
		TicketID:  ticketID,
		PlayerID:  req.PlayerID,
		MMRScore:  req.MMRScore,
		MatchMode: req.MatchMode,
		Status:    "queued",
	}
	f.tickets[ticketID] = ticket

	if f.nextID == 2 {
		f.result = &coreclient.MatchResult{
			MatchID:    "match_1",
			RoomID:     "room_1",
			ServerID:   "roomserver-1",
			ServerAddr: "127.0.0.1:7001",
			MatchMode:  req.MatchMode,
			PlayerIDs:  []string{"p1", "p2"},
			Status:     "matched",
		}
		for _, saved := range f.tickets {
			saved.Status = "matched"
			saved.MatchID = f.result.MatchID
			saved.RoomID = f.result.RoomID
		}
	}

	copyTicket := *ticket
	return &copyTicket, nil
}

func (f *fakeMatchCore) CreateCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.nextID
}

func (f *fakeMatchCore) GetTicket(_ context.Context, ticketID string) (*coreclient.Ticket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	ticket := f.tickets[ticketID]
	copyTicket := *ticket
	return &copyTicket, nil
}

func (f *fakeMatchCore) CancelTicket(_ context.Context, ticketID string) (*coreclient.Ticket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	ticket := f.tickets[ticketID]
	ticket.Status = "cancelled"
	copyTicket := *ticket
	return &copyTicket, nil
}

func (f *fakeMatchCore) GetResult(_ context.Context, _ string) (*coreclient.MatchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	copyResult := *f.result
	copyResult.PlayerIDs = append([]string(nil), f.result.PlayerIDs...)
	return &copyResult, nil
}

type fakeOpsClient struct {
	state   opsclient.State
	players map[string]*opsclient.PlayerState
}

func (f *fakeOpsClient) OpsState(context.Context) (opsclient.State, error) {
	if f.state == nil {
		return opsclient.State{}, nil
	}
	return f.state, nil
}

func (f *fakeOpsClient) PlayerState(_ context.Context, playerID string) (*opsclient.PlayerState, error) {
	if f.players == nil {
		return &opsclient.PlayerState{PlayerID: playerID, Status: "normal"}, nil
	}
	state := f.players[playerID]
	if state == nil {
		return &opsclient.PlayerState{PlayerID: playerID, Status: "normal"}, nil
	}
	copyState := *state
	return &copyState, nil
}

type testWSClient struct {
	conn net.Conn
	r    *bufio.Reader
}

func newTestWSClient(t *testing.T, baseURL string, path string) *testWSClient {
	t.Helper()

	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}
	conn, err := net.DialTimeout("tcp", parsed.Host, 2*time.Second)
	if err != nil {
		t.Fatalf("dial websocket server: %v", err)
	}

	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate websocket key: %v", err)
	}
	encodedKey := base64.StdEncoding.EncodeToString(key)
	request := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + parsed.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + encodedKey + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(request)); err != nil {
		conn.Close()
		t.Fatalf("send websocket upgrade: %v", err)
	}

	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		t.Fatalf("read websocket status: %v", err)
	}
	if status != "HTTP/1.1 101 Switching Protocols\r\n" {
		conn.Close()
		t.Fatalf("unexpected websocket status: %q", status)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			conn.Close()
			t.Fatalf("read websocket header: %v", err)
		}
		if line == "\r\n" {
			break
		}
	}

	return &testWSClient{conn: conn, r: reader}
}

func (c *testWSClient) send(t *testing.T, msg protocol.Message) {
	t.Helper()

	payload, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal websocket payload: %v", err)
	}
	if err := c.writeFrame(0x1, payload); err != nil {
		t.Fatalf("write websocket frame: %v", err)
	}
}

func (c *testWSClient) recv(t *testing.T) protocol.Response {
	t.Helper()

	if err := c.conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set websocket deadline: %v", err)
	}
	opcode, payload, err := c.readFrame()
	if err != nil {
		t.Fatalf("read websocket frame: %v", err)
	}
	if opcode != 0x1 {
		t.Fatalf("unexpected websocket opcode: %d", opcode)
	}

	var resp protocol.Response
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatalf("decode websocket response: %v payload=%s", err, string(payload))
	}
	return resp
}

func (c *testWSClient) close() {
	_ = c.writeFrame(0x8, nil)
	_ = c.conn.Close()
}

func (c *testWSClient) writeFrame(opcode byte, payload []byte) error {
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	header := []byte{0x80 | opcode}
	length := len(payload)
	switch {
	case length < 126:
		header = append(header, 0x80|byte(length))
	case length <= 65535:
		header = append(header, 0x80|126, byte(length>>8), byte(length))
	default:
		header = append(header, 0x80|127)
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(length))
		header = append(header, buf[:]...)
	}
	masked := make([]byte, len(payload))
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	if _, err := c.conn.Write(append(header, mask...)); err != nil {
		return err
	}
	_, err := c.conn.Write(masked)
	return err
}

func (c *testWSClient) readFrame() (byte, []byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(c.r, header); err != nil {
		return 0, nil, err
	}
	opcode := header[0] & 0x0F
	payloadLen := int64(header[1] & 0x7F)
	switch payloadLen {
	case 126:
		var buf [2]byte
		if _, err := io.ReadFull(c.r, buf[:]); err != nil {
			return 0, nil, err
		}
		payloadLen = int64(binary.BigEndian.Uint16(buf[:]))
	case 127:
		var buf [8]byte
		if _, err := io.ReadFull(c.r, buf[:]); err != nil {
			return 0, nil, err
		}
		payloadLen = int64(binary.BigEndian.Uint64(buf[:]))
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(c.r, payload); err != nil {
		return 0, nil, err
	}
	return opcode, payload, nil
}

func assertResponseType(t *testing.T, resp protocol.Response, expected string) {
	t.Helper()
	if resp.Type != expected {
		t.Fatalf("expected response type %s, got %#v", expected, resp)
	}
}

func recvType(t *testing.T, client *testWSClient, expected string) protocol.Response {
	t.Helper()
	for i := 0; i < 5; i++ {
		resp := client.recv(t)
		if resp.Type == expected {
			return resp
		}
		if resp.Type == protocol.TypeError {
			t.Fatalf("received error while waiting for %s: %#v", expected, resp)
		}
	}
	t.Fatalf("did not receive response type %s", expected)
	return protocol.Response{}
}
