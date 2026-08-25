import sys
import time
from http.server import BaseHTTPRequestHandler, HTTPServer

# Minimal stand-in "destination" for scripts/e2e-docker-test.sh: accepts any
# POST, logs a timestamped line to stdout (read back via `docker logs` to
# assert throttle pacing), and returns 200 so the request is recorded as
# success by the app under test.

class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get('Content-Length', 0))
        body = self.rfile.read(length)
        print(f"HIT ts={time.time():.3f} body={body.decode('utf-8', 'replace')}", flush=True)
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(b'{"ok":true}')

    def log_message(self, format, *args):
        pass

HTTPServer(('0.0.0.0', 8000), Handler).serve_forever()
