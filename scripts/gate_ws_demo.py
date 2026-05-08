import base64
import hashlib
import http.server
import json
import os
import socket
import socketserver
import struct
import subprocess
import sys
import threading
import time
import urllib.request


ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
GATEWAY_ADDR = "127.0.0.1:18082"
CORE_ADDR = "127.0.0.1:18081"
GATEWAY_URL = f"http://{GATEWAY_ADDR}"


class FakeCoreRankState:
    def __init__(self):
        self.lock = threading.Lock()
        self.next_ticket = 1
        self.next_match = 1
        self.waiting_by_mode = {}
        self.tickets = {}
        self.results = {}

    def create_ticket(self, payload):
        with self.lock:
            ticket_id = f"ticket_{self.next_ticket}"
            self.next_ticket += 1
            mode = payload.get("match_mode") or "duel"
            player_id = payload["player_id"]
            ticket = {
                "TicketID": ticket_id,
                "PlayerID": player_id,
                "MMRScore": payload.get("mmr_score", 1000),
                "MatchMode": mode,
                "Status": "queued",
                "MatchID": "",
                "RoomID": "",
                "CreatedAt": int(time.time() * 1000),
                "UpdatedAt": int(time.time() * 1000),
                "ExpiresAt": int(time.time() * 1000) + payload.get("max_wait_ms", 30000),
            }
            waiting = self.waiting_by_mode.get(mode)
            if waiting:
                first = self.tickets[waiting]
                match_id = f"match_{self.next_match}"
                room_id = f"room_demo_{self.next_match}"
                self.next_match += 1
                players = [first["PlayerID"], player_id]
                result = {
                    "MatchID": match_id,
                    "RoomID": room_id,
                    "ServerID": "demo-room-1",
                    "ServerAddr": "127.0.0.1:7001",
                    "MatchMode": mode,
                    "PlayerIDs": players,
                    "Status": "matched",
                    "CreatedAt": int(time.time() * 1000),
                }
                first.update({"Status": "matched", "MatchID": match_id, "RoomID": room_id})
                ticket.update({"Status": "matched", "MatchID": match_id, "RoomID": room_id})
                self.results[match_id] = result
                self.waiting_by_mode.pop(mode, None)
            else:
                self.waiting_by_mode[mode] = ticket_id
            self.tickets[ticket_id] = ticket
            return ticket

    def get_ticket(self, ticket_id):
        with self.lock:
            return self.tickets[ticket_id]

    def cancel_ticket(self, ticket_id):
        with self.lock:
            ticket = self.tickets[ticket_id]
            ticket["Status"] = "cancelled"
            for mode, waiting in list(self.waiting_by_mode.items()):
                if waiting == ticket_id:
                    self.waiting_by_mode.pop(mode, None)
            return ticket

    def get_result(self, match_id):
        with self.lock:
            return self.results[match_id]


STATE = FakeCoreRankState()


class FakeCoreRankHandler(http.server.BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        return

    def do_GET(self):
        if self.path == "/health":
            self.write_json({"status": "ok"})
            return
        if self.path.startswith("/api/match/tickets/"):
            ticket_id = self.path.rsplit("/", 1)[-1]
            self.write_json(STATE.get_ticket(ticket_id))
            return
        if self.path.startswith("/api/match/results/"):
            match_id = self.path.rsplit("/", 1)[-1]
            self.write_json(STATE.get_result(match_id))
            return
        self.send_error(404)

    def do_POST(self):
        if self.path == "/api/match/tickets":
            payload = self.read_json()
            self.write_json(STATE.create_ticket(payload), status=201)
            return
        self.send_error(404)

    def do_DELETE(self):
        if self.path.startswith("/api/match/tickets/"):
            ticket_id = self.path.rsplit("/", 1)[-1]
            self.write_json(STATE.cancel_ticket(ticket_id))
            return
        self.send_error(404)

    def read_json(self):
        length = int(self.headers.get("Content-Length", "0"))
        return json.loads(self.rfile.read(length).decode("utf-8"))

    def write_json(self, payload, status=200):
        data = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


class ThreadingHTTPServer(socketserver.ThreadingMixIn, http.server.HTTPServer):
    daemon_threads = True


class WSClient:
    def __init__(self, host, path):
        self.sock = socket.create_connection(host, timeout=5)
        key = base64.b64encode(os.urandom(16)).decode("ascii")
        request = (
            f"GET {path} HTTP/1.1\r\n"
            f"Host: {host[0]}:{host[1]}\r\n"
            "Upgrade: websocket\r\n"
            "Connection: Upgrade\r\n"
            f"Sec-WebSocket-Key: {key}\r\n"
            "Sec-WebSocket-Version: 13\r\n"
            "\r\n"
        )
        self.sock.sendall(request.encode("ascii"))
        response = self._recv_until(b"\r\n\r\n")
        if b" 101 " not in response:
            raise RuntimeError(f"websocket upgrade failed: {response!r}")

    def send_json(self, payload):
        data = json.dumps(payload).encode("utf-8")
        self._send_frame(0x1, data)

    def recv_json(self, timeout=5):
        deadline = time.time() + timeout
        while time.time() < deadline:
            self.sock.settimeout(max(0.1, deadline - time.time()))
            opcode, payload = self._recv_frame()
            if opcode == 0x1:
                return json.loads(payload.decode("utf-8"))
            if opcode == 0x8:
                raise RuntimeError("websocket closed")
        raise TimeoutError("timed out waiting websocket message")

    def close(self):
        try:
            self._send_frame(0x8, b"")
        finally:
            self.sock.close()

    def _send_frame(self, opcode, payload):
        mask = os.urandom(4)
        header = bytearray([0x80 | opcode])
        length = len(payload)
        if length < 126:
            header.append(0x80 | length)
        elif length <= 65535:
            header.append(0x80 | 126)
            header.extend(struct.pack("!H", length))
        else:
            header.append(0x80 | 127)
            header.extend(struct.pack("!Q", length))
        masked = bytes(payload[i] ^ mask[i % 4] for i in range(length))
        self.sock.sendall(bytes(header) + mask + masked)

    def _recv_frame(self):
        header = self._recv_exact(2)
        opcode = header[0] & 0x0F
        length = header[1] & 0x7F
        if length == 126:
            length = struct.unpack("!H", self._recv_exact(2))[0]
        elif length == 127:
            length = struct.unpack("!Q", self._recv_exact(8))[0]
        payload = self._recv_exact(length)
        return opcode, payload

    def _recv_exact(self, size):
        chunks = []
        remaining = size
        while remaining:
            chunk = self.sock.recv(remaining)
            if not chunk:
                raise RuntimeError("socket closed")
            chunks.append(chunk)
            remaining -= len(chunk)
        return b"".join(chunks)

    def _recv_until(self, marker):
        data = b""
        while marker not in data:
            chunk = self.sock.recv(4096)
            if not chunk:
                raise RuntimeError("socket closed")
            data += chunk
        return data


def wait_gateway_ready():
    for _ in range(40):
        try:
            with urllib.request.urlopen(GATEWAY_URL + "/healthz", timeout=1) as resp:
                if resp.status == 200:
                    return
        except Exception:
            time.sleep(0.25)
    raise RuntimeError("ArenaGate did not become ready")


def wait_for_type(client, msg_type):
    for _ in range(20):
        msg = client.recv_json(timeout=2)
        if msg.get("type") == msg_type:
            return msg
    raise RuntimeError(f"did not receive {msg_type}")


def main():
    tmp_dir = os.path.join(ROOT, "tmp")
    os.makedirs(tmp_dir, exist_ok=True)
    exe = os.path.join(tmp_dir, "arenagate-gateway.exe" if os.name == "nt" else "arenagate-gateway")

    env = os.environ.copy()
    env["GOCACHE"] = env.get("GOCACHE", os.path.join(ROOT, ".gocache"))
    env["GATEWAY_ADDR"] = GATEWAY_ADDR
    env["CORE_RANK_HTTP"] = f"http://{CORE_ADDR}"
    env["MATCH_POLL_INTERVAL_MS"] = "100"
    env["IDLE_TIMEOUT_MS"] = "10000"

    subprocess.run(["go", "build", "-o", exe, "./cmd/gateway"], cwd=ROOT, env=env, check=True)

    fake_core = ThreadingHTTPServer(("127.0.0.1", 18081), FakeCoreRankHandler)
    fake_thread = threading.Thread(target=fake_core.serve_forever, daemon=True)
    fake_thread.start()

    proc = subprocess.Popen([exe], cwd=ROOT, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    p1 = None
    p2 = None
    try:
        wait_gateway_ready()
        p1 = WSClient(("127.0.0.1", 18082), "/ws")
        p2 = WSClient(("127.0.0.1", 18082), "/ws")

        p1.send_json({"type": "auth", "request_id": "p1-auth", "player_id": "p1", "token": "dev-token:p1"})
        print("p1 authed" if p1.recv_json()["type"] == "authed" else "p1 auth failed")

        p2.send_json({"type": "auth", "request_id": "p2-auth", "player_id": "p2", "token": "dev-token:p2"})
        print("p2 authed" if p2.recv_json()["type"] == "authed" else "p2 auth failed")

        p1.send_json({"type": "enqueue_match", "request_id": "p1-match", "mmr_score": 1200, "match_mode": "duel"})
        queued = p1.recv_json()
        if queued["type"] != "match_queued":
            raise RuntimeError(f"expected p1 queued, got {queued}")
        print(f"p1 queued {queued['ticket_id']}")

        p2.send_json({"type": "enqueue_match", "request_id": "p2-match", "mmr_score": 1210, "match_mode": "duel"})
        p2_found = wait_for_type(p2, "match_found")
        print(f"p2 match_found {p2_found['room_id']} {p2_found['server_addr']}")

        p1_found = wait_for_type(p1, "match_found")
        print(f"p1 match_found {p1_found['room_id']} {p1_found['server_addr']}")

        print("ArenaGate WebSocket demo completed")
    finally:
        if p1:
            p1.close()
        if p2:
            p2.close()
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
        fake_core.shutdown()


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(str(exc), file=sys.stderr)
        raise
