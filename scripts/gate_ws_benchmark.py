import argparse
import concurrent.futures
import os
import socket
import subprocess
import sys
import threading
import time
import urllib.request

from gate_ws_demo import FakeCoreRankHandler
from gate_ws_demo import ROOT
from gate_ws_demo import ThreadingHTTPServer
from gate_ws_demo import WSClient


class BenchCoreRankServer(ThreadingHTTPServer):
    request_queue_size = 128


def find_free_port():
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def wait_gateway_ready(gateway_url):
    for _ in range(60):
        try:
            with urllib.request.urlopen(gateway_url + "/healthz", timeout=1) as resp:
                if resp.status == 200:
                    return
        except Exception:
            time.sleep(0.25)
    raise RuntimeError("ArenaGate did not become ready")


def wait_fake_core_ready(core_url):
    for _ in range(40):
        try:
            with urllib.request.urlopen(core_url + "/health", timeout=1) as resp:
                if resp.status == 200:
                    return
        except Exception:
            time.sleep(0.25)
    raise RuntimeError("fake CoreRank did not become ready")


def fetch_metrics(gateway_url):
    with urllib.request.urlopen(gateway_url + "/metrics", timeout=3) as resp:
        return resp.read().decode("utf-8")


def parse_metric_value(metrics_text, name):
    for line in metrics_text.splitlines():
        if line.startswith(name + " "):
            return int(float(line.split()[1]))
    return 0


def percentile(values, pct):
    if not values:
        return 0.0
    ordered = sorted(values)
    index = int(round((len(ordered) - 1) * pct / 100.0))
    return ordered[index]


class BenchClient:
    def __init__(self, index, host, path):
        self.index = index
        self.player_id = f"bench_p_{int(time.time() * 1000)}_{index}"
        self.client = WSClient(host, path)

    def auth(self):
        started = time.perf_counter()
        self.client.send_json({
            "type": "auth",
            "request_id": f"{self.player_id}-auth",
            "player_id": self.player_id,
            "token": "dev-token:" + self.player_id,
        })
        msg = self.client.recv_json(timeout=5)
        if msg.get("type") != "authed":
            raise RuntimeError(f"{self.player_id} auth failed: {msg}")
        return elapsed_ms(started)

    def ping_rounds(self, rounds):
        latencies = []
        for i in range(rounds):
            started = time.perf_counter()
            self.client.send_json({
                "type": "ping",
                "request_id": f"{self.player_id}-ping-{i}",
            })
            msg = self.client.recv_json(timeout=5)
            if msg.get("type") != "pong":
                raise RuntimeError(f"{self.player_id} expected pong, got {msg}")
            latencies.append(elapsed_ms(started))
        return latencies

    def enqueue_and_wait_found(self):
        started = time.perf_counter()
        self.client.send_json({
            "type": "enqueue_match",
            "request_id": f"{self.player_id}-match",
            "mmr_score": 1200 + self.index,
            "match_mode": "duel",
            "max_wait_ms": 5000,
        })
        queued = False
        deadline = time.time() + 10
        while time.time() < deadline:
            msg = self.client.recv_json(timeout=max(0.1, deadline - time.time()))
            if msg.get("type") == "match_queued":
                queued = True
                continue
            if msg.get("type") == "match_found":
                return elapsed_ms(started), queued
            if msg.get("type") == "error":
                raise RuntimeError(f"{self.player_id} enqueue error: {msg}")
        raise TimeoutError(f"{self.player_id} timed out waiting match_found")

    def close(self):
        self.client.close()


def elapsed_ms(started):
    return (time.perf_counter() - started) * 1000.0


def run_parallel(items, workers, fn):
    results = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as executor:
        future_map = {executor.submit(fn, item): item for item in items}
        for future in concurrent.futures.as_completed(future_map):
            results.append(future.result())
    return results


def build_gateway(exe, env):
    subprocess.run(["go", "build", "-o", exe, "./cmd/gateway"], cwd=ROOT, env=env, check=True)


def main():
    parser = argparse.ArgumentParser(description="Run a small ArenaGate WebSocket benchmark.")
    parser.add_argument("--clients", type=int, default=40, help="number of WebSocket clients, must be even")
    parser.add_argument("--ping-rounds", type=int, default=3, help="ping/pong rounds per client")
    parser.add_argument("--workers", type=int, default=10, help="maximum concurrent client workers")
    args = parser.parse_args()

    if args.clients <= 0 or args.clients % 2 != 0:
        raise ValueError("--clients must be a positive even number")

    tmp_dir = os.path.join(ROOT, "tmp")
    os.makedirs(tmp_dir, exist_ok=True)
    exe = os.path.join(tmp_dir, "arenagate-gateway.exe" if os.name == "nt" else "arenagate-gateway")

    gateway_port = find_free_port()
    core_port = find_free_port()
    gateway_addr = f"127.0.0.1:{gateway_port}"
    core_addr = f"127.0.0.1:{core_port}"
    gateway_url = f"http://{gateway_addr}"

    env = os.environ.copy()
    env["GOCACHE"] = env.get("GOCACHE", os.path.join(ROOT, ".gocache"))
    env["GOMODCACHE"] = env.get("GOMODCACHE", os.path.join(ROOT, ".gomodcache"))
    env["GATEWAY_ADDR"] = gateway_addr
    env["CORE_RANK_HTTP"] = f"http://{core_addr}"
    env["MATCH_POLL_INTERVAL_MS"] = "50"
    env["IDLE_TIMEOUT_MS"] = "30000"
    env["SESSION_RATE_LIMIT"] = "1000"

    build_gateway(exe, env)

    fake_core = BenchCoreRankServer(("127.0.0.1", core_port), FakeCoreRankHandler)
    fake_thread = threading.Thread(target=fake_core.serve_forever, daemon=True)
    fake_thread.start()
    wait_fake_core_ready(f"http://{core_addr}")

    proc = subprocess.Popen([exe], cwd=ROOT, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    clients = []
    try:
        wait_gateway_ready(gateway_url)
        host = ("127.0.0.1", gateway_port)

        auth_latencies = run_parallel(
            range(args.clients),
            args.workers,
            lambda index: create_and_auth(index, host, clients),
        )

        ping_latencies_nested = run_parallel(
            list(clients),
            args.workers,
            lambda bench_client: bench_client.ping_rounds(args.ping_rounds),
        )
        ping_latencies = [latency for group in ping_latencies_nested for latency in group]

        match_results = run_parallel(
            list(clients),
            args.workers,
            lambda bench_client: bench_client.enqueue_and_wait_found(),
        )
        match_latencies = [latency for latency, _ in match_results]
        queued_count = sum(1 for _, queued in match_results if queued)

        metrics_text = fetch_metrics(gateway_url)
        print("ArenaGate benchmark completed")
        print(f"clients={args.clients}")
        print(f"auth_success={len(auth_latencies)}")
        print(f"ping_messages={len(ping_latencies)}")
        print(f"match_found={len(match_latencies)}")
        print(f"queued_before_found={queued_count}")
        print(f"auth_ms_avg={sum(auth_latencies) / len(auth_latencies):.2f}")
        print(f"ping_ms_avg={sum(ping_latencies) / len(ping_latencies):.2f}")
        print(f"ping_ms_p95={percentile(ping_latencies, 95):.2f}")
        print(f"match_ms_avg={sum(match_latencies) / len(match_latencies):.2f}")
        print(f"match_ms_p95={percentile(match_latencies, 95):.2f}")
        print("metrics_snapshot:")
        for name in (
            "arenagate_active_sessions",
            "arenagate_connections_total",
            "arenagate_messages_total",
            "arenagate_core_requests_total",
            "arenagate_core_errors_total",
            "arenagate_match_found_total",
            "arenagate_errors_total",
        ):
            print(f"{name} {parse_metric_value(metrics_text, name)}")
    finally:
        for bench_client in clients:
            try:
                bench_client.close()
            except Exception:
                pass
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
        fake_core.shutdown()


def create_and_auth(index, host, clients):
    bench_client = BenchClient(index, host, "/ws")
    latency = bench_client.auth()
    clients.append(bench_client)
    return latency


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(str(exc), file=sys.stderr)
        raise
