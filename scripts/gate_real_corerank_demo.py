import base64
import json
import os
import socket
import struct
import subprocess
import sys
import time
import urllib.error
import urllib.request


ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
WORKSPACE_ROOT = os.path.abspath(os.path.join(ROOT, ".."))
CORERANK_ROOT = os.path.join(WORKSPACE_ROOT, "CoreRank")


def find_free_addr():
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    try:
        sock.bind(("127.0.0.1", 0))
        host, port = sock.getsockname()
        return f"{host}:{port}"
    finally:
        sock.close()


def check_redis(addr):
    host, port = addr.rsplit(":", 1)
    try:
        with socket.create_connection((host, int(port)), timeout=2):
            return True
    except OSError:
        return False


def request(base_url, method, path, payload=None):
    data = None
    headers = {}
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(base_url + path, data=data, method=method, headers=headers)
    with urllib.request.urlopen(req, timeout=5) as resp:
        return json.loads(resp.read().decode("utf-8"))


def wait_http_ready(base_url, health_path, name):
    for _ in range(80):
        try:
            request(base_url, "GET", health_path)
            return
        except Exception:
            time.sleep(0.25)
    raise RuntimeError(f"{name} did not become ready at {base_url}{health_path}")


def build_binary(root, env, package, output_name):
    tmp_dir = os.path.join(ROOT, "tmp")
    os.makedirs(tmp_dir, exist_ok=True)
    suffix = f"{os.getpid()}_{int(time.time() * 1000)}"
    exe_name = f"{output_name}-{suffix}.exe" if os.name == "nt" else f"{output_name}-{suffix}"
    exe_path = os.path.join(tmp_dir, exe_name)
    subprocess.run(["go", "build", "-o", exe_path, package], cwd=root, env=env, check=True)
    return exe_path


def start_process(exe_path, cwd, env, log_name):
    tmp_dir = os.path.join(ROOT, "tmp")
    os.makedirs(tmp_dir, exist_ok=True)
    log_path = os.path.join(tmp_dir, log_name)
    log_file = open(log_path, "w", encoding="utf-8")
    proc = subprocess.Popen(
        [exe_path],
        cwd=cwd,
        env=env,
        stdout=log_file,
        stderr=subprocess.STDOUT,
    )
    return proc, log_file, log_path


def terminate(proc, log_file=None):
    if proc is None:
        return
    proc.terminate()
    try:
        proc.wait(timeout=5)
    except subprocess.TimeoutExpired:
        proc.kill()
    if log_file is not None:
        log_file.close()


class WSClient:
    def __init__(self, host, path):
        self.sock = socket.create_connection(host, timeout=5)
        key = base64.b64encode(os.urandom(16)).decode("ascii")
        request_text = (
            f"GET {path} HTTP/1.1\r\n"
            f"Host: {host[0]}:{host[1]}\r\n"
            "Upgrade: websocket\r\n"
            "Connection: Upgrade\r\n"
            f"Sec-WebSocket-Key: {key}\r\n"
            "Sec-WebSocket-Version: 13\r\n"
            "\r\n"
        )
        self.sock.sendall(request_text.encode("ascii"))
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


def wait_for_type(client, msg_type, timeout=12):
    deadline = time.time() + timeout
    while time.time() < deadline:
        msg = client.recv_json(timeout=max(0.5, deadline - time.time()))
        if msg.get("type") == msg_type:
            return msg
        if msg.get("type") == "error":
            raise RuntimeError(f"received error response: {msg}")
    raise RuntimeError(f"did not receive {msg_type}")


def assert_type(msg, expected_type):
    actual = msg.get("type")
    if actual != expected_type:
        raise RuntimeError(f"expected {expected_type}, got {msg}")


def main():
    if not os.path.isdir(CORERANK_ROOT):
        raise RuntimeError(f"CoreRank project not found: {CORERANK_ROOT}")

    redis_addr = os.environ.get("REDIS_ADDR", "127.0.0.1:6379")
    if not check_redis(redis_addr):
        raise RuntimeError(
            "Redis is required for real CoreRank demo. "
            "Start it first, for example: cd ..\\CoreRank; docker compose up -d corerank-redis"
        )

    grpc_addr = find_free_addr()
    core_http_addr = find_free_addr()
    core_metrics_addr = find_free_addr()
    gateway_addr = find_free_addr()
    match_mode = f"arenagate_real_{os.getpid()}_{int(time.time())}"
    server_id = f"arena-demo-room-{os.getpid()}"
    room_addr = "127.0.0.1:7001"
    suffix = f"{os.getpid()}_{int(time.time() * 1000)}"
    players = [f"p_gate_real_a_{suffix}", f"p_gate_real_b_{suffix}"]
    base_mmr = 42000 + (os.getpid() % 1000)

    common_env = os.environ.copy()
    common_env["GOCACHE"] = common_env.get("GOCACHE", os.path.join(ROOT, ".gocache"))

    core_env = common_env.copy()
    core_env["REDIS_ADDR"] = redis_addr
    core_env["GRPC_ADDR"] = grpc_addr
    core_env["HTTP_ADDR"] = core_http_addr
    core_env["METRICS_ADDR"] = core_metrics_addr

    gateway_env = common_env.copy()
    gateway_env["GATEWAY_ADDR"] = gateway_addr
    gateway_env["CORE_RANK_HTTP"] = f"http://{core_http_addr}"
    gateway_env["MATCH_POLL_INTERVAL_MS"] = "100"
    gateway_env["IDLE_TIMEOUT_MS"] = "10000"

    core_exe = build_binary(CORERANK_ROOT, common_env, "./cmd/server", "corerank-server")
    gateway_exe = build_binary(ROOT, common_env, "./cmd/gateway", "arenagate-gateway")

    core_proc = None
    gate_proc = None
    core_log = None
    gate_log = None
    p1 = None
    p2 = None

    try:
        core_proc, core_log, core_log_path = start_process(core_exe, CORERANK_ROOT, core_env, "arenagate-real-corerank.log")
        wait_http_ready(f"http://{core_http_addr}", "/health", "CoreRank")

        registered = request(f"http://{core_http_addr}", "POST", "/api/servers", {
            "server_id": server_id,
            "server_type": "room",
            "addr": room_addr,
            "region": "local",
            "match_mode": match_mode,
            "capacity": 8,
            "current_load": 0,
            "status": "active",
        })
        print(f"CoreRank registered room server {registered['server_id']} for {match_mode}")

        gate_proc, gate_log, gate_log_path = start_process(gateway_exe, ROOT, gateway_env, "arenagate-real-gateway.log")
        wait_http_ready(f"http://{gateway_addr}", "/healthz", "ArenaGate")

        p1 = WSClient(("127.0.0.1", int(gateway_addr.rsplit(":", 1)[1])), "/ws")
        p2 = WSClient(("127.0.0.1", int(gateway_addr.rsplit(":", 1)[1])), "/ws")

        p1.send_json({"type": "auth", "request_id": "p1-auth", "player_id": players[0], "token": f"dev-token:{players[0]}"})
        assert_type(p1.recv_json(), "authed")
        print(f"{players[0]} authed through ArenaGate")

        p2.send_json({"type": "auth", "request_id": "p2-auth", "player_id": players[1], "token": f"dev-token:{players[1]}"})
        assert_type(p2.recv_json(), "authed")
        print(f"{players[1]} authed through ArenaGate")

        p1.send_json({
            "type": "enqueue_match",
            "request_id": "p1-match",
            "mmr_score": base_mmr,
            "match_mode": match_mode,
            "max_wait_ms": 30000,
        })
        queued = p1.recv_json()
        assert_type(queued, "match_queued")
        print(f"{players[0]} queued by real CoreRank ticket={queued['ticket_id']}")

        p2.send_json({
            "type": "enqueue_match",
            "request_id": "p2-match",
            "mmr_score": base_mmr + 5,
            "match_mode": match_mode,
            "max_wait_ms": 30000,
        })

        p2_found = wait_for_type(p2, "match_found")
        p1_found = wait_for_type(p1, "match_found")

        for found in (p1_found, p2_found):
            if found.get("server_id") != server_id:
                raise RuntimeError(f"unexpected server_id in match_found: {found}")
            if found.get("server_addr") != room_addr:
                raise RuntimeError(f"unexpected server_addr in match_found: {found}")
            if set(found.get("players", [])) != set(players):
                raise RuntimeError(f"unexpected players in match_found: {found}")

        print(f"real CoreRank match_id={p2_found['match_id']}")
        print(f"real CoreRank room_id={p2_found['room_id']} server_addr={p2_found['server_addr']}")
        print("ArenaGate real CoreRank demo completed")
        print(f"CoreRank log: {core_log_path}")
        print(f"ArenaGate log: {gate_log_path}")
    finally:
        if p1:
            p1.close()
        if p2:
            p2.close()
        terminate(gate_proc, gate_log)
        terminate(core_proc, core_log)


if __name__ == "__main__":
    try:
        main()
    except urllib.error.HTTPError as exc:
        print(exc.read().decode("utf-8", errors="replace"), file=sys.stderr)
        raise
