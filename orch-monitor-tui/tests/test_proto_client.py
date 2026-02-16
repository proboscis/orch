"""Tests for protobuf client conversion functions."""

import socket
import struct
import threading
import tempfile
import shutil
from pathlib import Path

import pytest
from returns.result import Failure, Success

import hy  # noqa: F401 - Enable Hy imports

from orch_monitor.api import orch_pb2 as pb
from orch_monitor.proto_client import (
    ProtoDaemonClient,
    proto_branch_state_to_str as _proto_branch_state_to_str,
    proto_multiplexer_to_str as _proto_multiplexer_to_str,
    proto_run_to_model as _proto_run_to_model,
    proto_status_to_model as _proto_status_to_model,
)
from orch_monitor.models import Status
from orch_monitor.types import (
    ProtoDaemonConnectionRefusedError,
    ProtoDaemonSocketMissingError,
    ProtoDaemonTimeoutError,
)


class TestProtoBranchStateToStr:
    def test_clean_state(self):
        assert _proto_branch_state_to_str(pb.BRANCH_STATE_CLEAN) == "clean"

    def test_dirty_state(self):
        assert _proto_branch_state_to_str(pb.BRANCH_STATE_DIRTY) == "dirty"

    def test_merged_state(self):
        assert _proto_branch_state_to_str(pb.BRANCH_STATE_MERGED) == "merged"

    def test_conflict_state(self):
        assert _proto_branch_state_to_str(pb.BRANCH_STATE_CONFLICT) == "conflict"

    def test_ahead_state(self):
        assert _proto_branch_state_to_str(pb.BRANCH_STATE_AHEAD) == "ahead"

    def test_behind_state(self):
        assert _proto_branch_state_to_str(pb.BRANCH_STATE_BEHIND) == "behind"

    def test_diverged_state(self):
        assert _proto_branch_state_to_str(pb.BRANCH_STATE_DIVERGED) == "diverged"

    def test_synced_state(self):
        assert _proto_branch_state_to_str(pb.BRANCH_STATE_SYNCED) == "synced"

    def test_unspecified_returns_empty(self):
        assert _proto_branch_state_to_str(pb.BRANCH_STATE_UNSPECIFIED) == ""

    def test_unknown_value_returns_empty(self):
        assert _proto_branch_state_to_str(999) == ""


class TestProtoMultiplexerToStr:
    def test_tmux(self):
        assert _proto_multiplexer_to_str(pb.MULTIPLEXER_TMUX) == "tmux"

    def test_zellij(self):
        assert _proto_multiplexer_to_str(pb.MULTIPLEXER_ZELLIJ) == "zellij"

    def test_unspecified_returns_empty(self):
        assert _proto_multiplexer_to_str(pb.MULTIPLEXER_UNSPECIFIED) == ""


class TestProtoStatusToModel:
    def test_all_known_statuses(self):
        assert _proto_status_to_model(pb.RUN_STATUS_QUEUED) == Status.QUEUED
        assert _proto_status_to_model(pb.RUN_STATUS_BOOTING) == Status.BOOTING
        assert _proto_status_to_model(pb.RUN_STATUS_RUNNING) == Status.RUNNING
        assert _proto_status_to_model(pb.RUN_STATUS_BLOCKED) == Status.BLOCKED
        assert _proto_status_to_model(pb.RUN_STATUS_BLOCKED_API) == Status.BLOCKED_API
        assert _proto_status_to_model(pb.RUN_STATUS_PR_OPEN) == Status.PR_OPEN
        assert _proto_status_to_model(pb.RUN_STATUS_DONE) == Status.DONE
        assert _proto_status_to_model(pb.RUN_STATUS_FAILED) == Status.FAILED
        assert _proto_status_to_model(pb.RUN_STATUS_CANCELED) == Status.CANCELED

    def test_unspecified_returns_unknown(self):
        assert _proto_status_to_model(pb.RUN_STATUS_UNSPECIFIED) == Status.UNKNOWN


class TestProtoRunToModel:
    def test_basic_fields(self):
        proto_run = pb.Run(
            issue_id="issue-123",
            run_id="run-456",
            status=pb.RUN_STATUS_RUNNING,
            agent="claude",
            model="sonnet",
            branch="feature/test",
            worktree_path="/tmp/worktree",
            session_name="orch-test",
            pr_url="https://github.com/test/pr/1",
        )
        run = _proto_run_to_model(proto_run)

        assert run.issue_id == "issue-123"
        assert run.run_id == "run-456"
        assert run.status == Status.RUNNING
        assert run.agent == "claude"
        assert run.model == "sonnet"
        assert run.branch == "feature/test"
        assert run.worktree_path == "/tmp/worktree"
        assert run.session_name == "orch-test"
        assert run.pr_url == "https://github.com/test/pr/1"

    def test_elapsed_fields(self):
        proto_run = pb.Run(
            issue_id="test",
            run_id="001",
            elapsed_seconds=3600,
            elapsed_display="1h0m",
        )
        run = _proto_run_to_model(proto_run)

        assert run.elapsed_seconds == 3600
        assert run.elapsed_display == "1h0m"

    def test_branch_state(self):
        proto_run = pb.Run(
            issue_id="test",
            run_id="001",
            branch_state=pb.BRANCH_STATE_DIRTY,
        )
        run = _proto_run_to_model(proto_run)

        assert run.branch_state == "dirty"

    def test_diff_stats_present(self):
        proto_run = pb.Run(
            issue_id="test",
            run_id="001",
        )
        proto_run.diff_stats.additions = 100
        proto_run.diff_stats.deletions = 50
        proto_run.diff_stats.files_changed = 5
        proto_run.diff_stats.files.extend(["file1.py", "file2.py", "file3.py"])

        run = _proto_run_to_model(proto_run)

        assert run.additions == 100
        assert run.deletions == 50
        assert run.files_changed == 5
        assert run.files == ["file1.py", "file2.py", "file3.py"]

    def test_diff_stats_absent(self):
        proto_run = pb.Run(
            issue_id="test",
            run_id="001",
        )
        run = _proto_run_to_model(proto_run)

        assert run.additions == 0
        assert run.deletions == 0
        assert run.files_changed == 0
        assert run.files == []

    def test_multiplexer_tmux(self):
        proto_run = pb.Run(
            issue_id="test",
            run_id="001",
            multiplexer=pb.MULTIPLEXER_TMUX,
        )
        run = _proto_run_to_model(proto_run)

        assert run.multiplexer == "tmux"

    def test_multiplexer_zellij(self):
        proto_run = pb.Run(
            issue_id="test",
            run_id="001",
            multiplexer=pb.MULTIPLEXER_ZELLIJ,
        )
        run = _proto_run_to_model(proto_run)

        assert run.multiplexer == "zellij"

    def test_timestamps(self):
        proto_run = pb.Run(
            issue_id="test",
            run_id="001",
            started_at_unix=1700000000,
            updated_at_unix=1700003600,
        )
        run = _proto_run_to_model(proto_run)

        assert run.started_at is not None
        assert run.updated_at is not None
        assert run.started_at.timestamp() == 1700000000
        assert run.updated_at.timestamp() == 1700003600

    def test_timestamps_zero_returns_none(self):
        proto_run = pb.Run(
            issue_id="test",
            run_id="001",
            started_at_unix=0,
            updated_at_unix=0,
        )
        run = _proto_run_to_model(proto_run)

        assert run.started_at is None
        assert run.updated_at is None

    def test_opencode_fields(self):
        proto_run = pb.Run(
            issue_id="test",
            run_id="001",
            server_port=4096,
            opencode_session_id="sess-abc123",
            continued_from="run-000",
        )
        run = _proto_run_to_model(proto_run)

        assert run.server_port == 4096
        assert run.opencode_session_id == "sess-abc123"
        assert run.continued_from == "run-000"


class TestProtoDaemonAvailability:
    def _short_socket_path(self) -> tuple[Path, Path]:
        socket_dir = Path(tempfile.mkdtemp(prefix="orch-sock-", dir="/tmp"))
        return socket_dir, socket_dir / "daemon.sock"

    def test_missing_socket_reports_typed_error(self):
        socket_dir, socket_path = self._short_socket_path()
        client = ProtoDaemonClient(socket_path, None, None, 0.2)

        try:
            result = client.check_availability()

            assert isinstance(result, Failure)
            assert isinstance(result.failure(), ProtoDaemonSocketMissingError)
            assert client.is_available() is False
        finally:
            shutil.rmtree(socket_dir, ignore_errors=True)

    def test_stale_socket_reports_connection_refused(self):
        socket_dir, socket_path = self._short_socket_path()
        stale_socket = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        try:
            stale_socket.bind(str(socket_path))
            stale_socket.close()

            client = ProtoDaemonClient(socket_path, None, None, 0.2)
            result = client.check_availability()

            assert isinstance(result, Failure)
            assert isinstance(result.failure(), ProtoDaemonConnectionRefusedError)
            assert client.is_available() is False
        finally:
            try:
                stale_socket.close()
            except OSError:
                pass
            if socket_path.exists():
                socket_path.unlink(missing_ok=True)
            shutil.rmtree(socket_dir, ignore_errors=True)

    def test_health_probe_timeout_reports_typed_error(self):
        socket_dir, socket_path = self._short_socket_path()
        ready = threading.Event()
        release = threading.Event()

        def _hanging_server():
            server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            try:
                server.bind(str(socket_path))
                server.listen(1)
                ready.set()
                conn, _ = server.accept()
                try:
                    conn.recv(4096)
                    release.wait(timeout=1.0)
                finally:
                    conn.close()
            finally:
                server.close()

        thread = threading.Thread(target=_hanging_server, daemon=True)
        thread.start()
        assert ready.wait(timeout=1.0)

        try:
            client = ProtoDaemonClient(socket_path, None, None, 0.05)
            result = client.check_availability()
            release.set()
            thread.join(timeout=1.0)

            assert isinstance(result, Failure)
            assert isinstance(result.failure(), ProtoDaemonTimeoutError)
            assert "timed out" in str(result.failure()).lower()
        finally:
            release.set()
            thread.join(timeout=1.0)
            if socket_path.exists():
                socket_path.unlink(missing_ok=True)
            shutil.rmtree(socket_dir, ignore_errors=True)

    def test_check_availability_reuses_single_connection(self):
        socket_dir, socket_path = self._short_socket_path()
        ready = threading.Event()
        done = threading.Event()
        accept_count = 0

        def _recv_exact(conn: socket.socket, size: int) -> bytes:
            data = b""
            while len(data) < size:
                chunk = conn.recv(size - len(data))
                if not chunk:
                    break
                data += chunk
            return data

        def _ping_server():
            nonlocal accept_count
            server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            try:
                server.bind(str(socket_path))
                server.listen(2)
                ready.set()

                conn, _ = server.accept()
                accept_count += 1
                try:
                    # Serve multiple requests on the same accepted connection.
                    for _ in range(2):
                        len_data = _recv_exact(conn, 4)
                        if len(len_data) < 4:
                            return
                        req_len = struct.unpack(">I", len_data)[0]
                        req_data = _recv_exact(conn, req_len)
                        if len(req_data) < req_len:
                            return

                        req = pb.Request()
                        req.ParseFromString(req_data)

                        resp = pb.Response(
                            ok=True,
                            ping=pb.PingResponse(ok=True, version="test"),
                        )
                        payload = resp.SerializeToString()
                        conn.sendall(struct.pack(">I", len(payload)) + payload)

                    # Keep socket open briefly to detect illegal second accept.
                    server.settimeout(0.2)
                    try:
                        extra_conn, _ = server.accept()
                        accept_count += 1
                        extra_conn.close()
                    except socket.timeout:
                        pass
                finally:
                    conn.close()
            finally:
                server.close()
                done.set()

        thread = threading.Thread(target=_ping_server, daemon=True)
        thread.start()
        assert ready.wait(timeout=1.0)

        try:
            client = ProtoDaemonClient(socket_path, None, None, 0.2)
            first = client.check_availability()
            second = client.check_availability()

            assert first == Success(True)
            assert second == Success(True)
            assert done.wait(timeout=1.0)
            assert accept_count == 1
        finally:
            thread.join(timeout=1.0)
            if socket_path.exists():
                socket_path.unlink(missing_ok=True)
            shutil.rmtree(socket_dir, ignore_errors=True)
