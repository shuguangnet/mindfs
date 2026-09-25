#!/usr/bin/env python3
"""Minimal fake ACP agent for tests: completes the ACP handshake over ndJSON,
then never answers session/prompt (simulating a hung model service).
Any session/set_* or session/cancel request is answered with an empty result."""
import json
import sys

def send(obj):
    sys.stdout.write(json.dumps(obj) + "\n")
    sys.stdout.flush()

def handle(req):
    method = req.get("method", "")
    req_id = req.get("id")
    if method == "initialize":
        send({"jsonrpc": "2.0", "id": req_id, "result": {
            "protocolVersion": 1,
            "agentInfo": {"name": "fake-hang", "version": "0.0.1"},
            "agentCapabilities": {"loadSession": True},
        }})
    elif method == "session/new":
        send({"jsonrpc": "2.0", "id": req_id, "result": {
            "sessionId": "sess-fake-1",
            "modes": {"currentModeId": "off", "availableModes": [{"id": "off", "name": "off"}]},
        }})
    elif method == "session/prompt":
        pass  # hang: no response, no stopReason
    else:
        if req_id is not None:
            send({"jsonrpc": "2.0", "id": req_id, "result": {}})

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        req = json.loads(line)
    except Exception:
        continue
    handle(req)
