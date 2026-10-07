"""Offline regression tests for the shipped HTTP tool (PyYAML and httpx required)."""

import contextlib
import io
import json
from pathlib import Path
import threading
import tempfile
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import patch

import httpx
import yaml


TOOL = Path(__file__).resolve().parents[2] / "tools" / "http-framework-test.yaml"
NAMESPACE = {"__name__": "http_tool_fixture"}
exec(compile(yaml.safe_load(TOOL.read_text(encoding="utf-8"))["args"][1], str(TOOL), "exec"), NAMESPACE)


class FixtureHandler(BaseHTTPRequestHandler):
    """Serve deterministic slow headers/body and count actual HTTP requests."""

    def log_message(self, *_args):
        """Suppress fixture access logs; failures are captured by assertions."""

    def do_GET(self):
        """Return a short response or stall beyond the client's read timeout."""
        self.server.requests += 1
        if self.path == "/slow-headers":
            time.sleep(0.4)
        self.send_response(200)
        self.send_header("Content-Length", "6")
        self.end_headers()
        try:
            self.wfile.write(b"abc")
            self.wfile.flush()
            if self.path == "/slow-body":
                time.sleep(0.4)
            self.wfile.write(b"def")
        except (BrokenPipeError, ConnectionResetError, ConnectionAbortedError):
            pass


class HTTPFrameworkTests(unittest.TestCase):
    """Verify failure diagnostics, validation and request count without external traffic."""

    @classmethod
    def setUpClass(cls):
        """Start an ephemeral loopback fixture; no real target is contacted."""
        cls.server = ThreadingHTTPServer(("127.0.0.1", 0), FixtureHandler)
        cls.server.requests = 0
        cls.home = tempfile.TemporaryDirectory()
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()

    @classmethod
    def tearDownClass(cls):
        """Stop and release the local fixture, including its background thread."""
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join()
        cls.home.cleanup()

    def run_tool(self, path="/ok", timeout="0.1"):
        """Run main with isolated argv/environment and capture its exit code/output."""
        url = f"http://127.0.0.1:{self.server.server_port}{path}"
        output, errors = io.StringIO(), io.StringIO()
        with patch("sys.argv", ["http-tool", "--url", url, f"--timeout={timeout}"]), \
                patch.dict("os.environ", {"HOME": self.home.name, "USERPROFILE": self.home.name}, clear=True), \
                contextlib.redirect_stdout(output), contextlib.redirect_stderr(errors):
            with self.assertRaises(SystemExit) as result:
                NAMESPACE["main"]()
        return result.exception.code, output.getvalue(), errors.getvalue()

    def test_timeout_classification_and_redaction(self):
        """Empty or sensitive exception messages still produce safe, useful metadata."""
        cases = [
            (httpx.ConnectTimeout, "HTTP_CONNECT_TIMEOUT", "connect_or_tls"),
            (httpx.ReadTimeout, "HTTP_READ_TIMEOUT", "response_headers_or_redirect"),
            (httpx.WriteTimeout, "HTTP_WRITE_TIMEOUT", "request_write"),
            (httpx.PoolTimeout, "HTTP_POOL_TIMEOUT", "connection_pool"),
            (httpx.ProxyError, "HTTP_PROXY_ERROR", "proxy"),
            (httpx.TooManyRedirects, "HTTP_REDIRECT_LIMIT", "redirect"),
        ]
        for error_type, code, phase in cases:
            for message in ("", "https://user:private-secret@example.test/?token=private-secret"):
                with self.subTest(error_type=error_type, message=message):
                    result = NAMESPACE["classify_request_error"](error_type(message))
                    self.assertEqual(result["error_code"], code)
                    self.assertEqual(result["phase"], phase)
                    self.assertFalse(result["automatic_retry"])
                    self.assertNotIn("private-secret", json.dumps(result))

    def test_dns_tls_and_unknown_errors(self):
        """Recognized transport errors have guidance without disabling certificate checks."""
        for message, code in (("getaddrinfo failed", "HTTP_DNS_ERROR"),
                              ("certificate verify failed", "HTTP_TLS_ERROR"),
                              ("UNEXPECTED_EOF", "HTTP_TLS_ERROR"),
                              ("", "HTTP_REQUEST_FAILED")):
            with self.subTest(message=message):
                result = NAMESPACE["classify_request_error"](httpx.ConnectError(message))
                self.assertEqual(result["error_code"], code)
                self.assertNotIn("allow-insecure", result["hint"])

    def test_invalid_timeout_does_not_create_client(self):
        """Invalid values fail before constructing a client or opening any connection."""
        with patch.object(httpx, "Client") as client:
            for timeout in ("0", "-1", "nan", "inf", "-inf", "abc", "1e999"):
                with self.subTest(timeout=timeout):
                    code, _, error = self.run_tool(timeout=timeout)
                    self.assertEqual(code, 2)
                    self.assertIn("positive finite", error)
            client.assert_not_called()
        self.assertEqual(NAMESPACE["parse_timeout"](""), 60)
        self.assertEqual(NAMESPACE["parse_timeout"]("0.5"), 0.5)

    def test_loopback_success_has_no_diagnostic_connection(self):
        """Normal requests must not create a second socket for diagnostic probes."""
        previous = self.server.requests
        with patch.object(NAMESPACE["socket"], "socket", wraps=NAMESPACE["socket"].socket) as sockets:
            code, output, _ = self.run_tool(timeout="2")
        self.assertEqual(code, 0, output)
        self.assertIn("abcdef", output)
        self.assertEqual(self.server.requests - previous, 1)
        # Client socket and server accept socket only; a probe would add another pair.
        self.assertEqual(sockets.call_count, 2)

    def test_real_read_timeouts_remain_failures_without_retry(self):
        """Slow headers/body stay distinguishable, incomplete, and single-attempt."""
        for path, phase, status, size in (("/slow-headers", "response_headers_or_redirect", None, 0),
                                          ("/slow-body", "response_body", 200, 3)):
            with self.subTest(path=path):
                previous = self.server.requests
                code, output, _ = self.run_tool(path)
                self.assertEqual(code, 1, output)
                self.assertEqual(self.server.requests - previous, 1)
                failure = json.loads(next(line.removeprefix("Failure: ")
                                          for line in output.splitlines() if line.startswith("Failure: ")))
                self.assertEqual(failure["error_code"], "HTTP_READ_TIMEOUT")
                self.assertEqual(failure["phase"], phase)
                self.assertEqual(failure["response_status"], status)
                self.assertEqual(failure["received_body_bytes"], size)
                self.assertFalse(failure["response_complete"])
                self.assertFalse(failure["automatic_retry"])
                self.assertIn("No automatic retry", output)


if __name__ == "__main__":
    unittest.main()
