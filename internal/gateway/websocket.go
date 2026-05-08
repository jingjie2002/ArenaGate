package gateway

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

type wsConn struct {
	conn net.Conn
	r    *bufio.Reader
	mu   sync.Mutex
	max  int64
}

func acceptWebSocket(w http.ResponseWriter, r *http.Request, maxMessageBytes int64) (*wsConn, error) {
	if !headerContains(r.Header.Get("Connection"), "upgrade") || strings.ToLower(r.Header.Get("Upgrade")) != "websocket" {
		return nil, errors.New("websocket upgrade headers are required")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, errors.New("Sec-WebSocket-Key is required")
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("http server does not support hijacking")
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}

	accept := websocketAccept(key)
	_, err = fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		conn.Close()
		return nil, err
	}

	return &wsConn{conn: conn, r: rw.Reader, max: maxMessageBytes}, nil
}

func (c *wsConn) ReadJSON(target any) error {
	for {
		opcode, payload, err := c.readFrame()
		if err != nil {
			return err
		}
		switch opcode {
		case 0x1:
			return json.Unmarshal(payload, target)
		case 0x8:
			return io.EOF
		case 0x9:
			if err := c.writeFrame(0xA, payload); err != nil {
				return err
			}
		case 0xA:
			continue
		default:
			return fmt.Errorf("unsupported websocket opcode: %d", opcode)
		}
	}
}

func (c *wsConn) WriteJSON(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.writeFrame(0x1, data)
}

func (c *wsConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

func (c *wsConn) Close() error {
	_ = c.writeFrame(0x8, nil)
	return c.conn.Close()
}

func (c *wsConn) readFrame() (byte, []byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(c.r, header); err != nil {
		return 0, nil, err
	}
	if header[0]&0x80 == 0 {
		return 0, nil, errors.New("fragmented websocket messages are not supported in v1")
	}
	opcode := header[0] & 0x0F
	masked := header[1]&0x80 != 0
	if !masked {
		return 0, nil, errors.New("client websocket frames must be masked")
	}

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
	if payloadLen > c.max {
		return 0, nil, fmt.Errorf("websocket message exceeds limit: %d > %d", payloadLen, c.max)
	}

	var mask [4]byte
	if _, err := io.ReadFull(c.r, mask[:]); err != nil {
		return 0, nil, err
	}

	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(c.r, payload); err != nil {
		return 0, nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
	return opcode, payload, nil
}

func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	header := []byte{0x80 | opcode}
	length := len(payload)
	switch {
	case length < 126:
		header = append(header, byte(length))
	case length <= 65535:
		header = append(header, 126, byte(length>>8), byte(length))
	default:
		header = append(header, 127)
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(length))
		header = append(header, buf[:]...)
	}

	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := c.conn.Write(payload)
	return err
}

func websocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + websocketGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func headerContains(value string, token string) bool {
	token = strings.ToLower(token)
	for _, part := range strings.Split(value, ",") {
		if strings.TrimSpace(strings.ToLower(part)) == token {
			return true
		}
	}
	return false
}
