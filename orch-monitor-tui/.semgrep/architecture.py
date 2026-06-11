import hashlib
import subprocess


def git_shellout():
    # ruleid: tui-no-git-subprocess
    subprocess.run(["git", "config", "--get", "remote.origin.url"])


def git_shellout_variable():
    cmd = ["git", "rev-parse", "--show-toplevel"]
    # ruleid: tui-no-git-subprocess
    subprocess.run(cmd)


def git_shellout_absolute_path():
    # ruleid: tui-no-git-subprocess
    subprocess.run(["/usr/bin/git", "rev-parse"])


def git_shellout_shell_string():
    # ruleid: tui-no-git-subprocess
    subprocess.run("git rev-parse --show-toplevel", shell=True)


def repo_id_derivation():
    # ruleid: tui-no-local-repo-id-parsing
    return "remote.origin.url"


# ruleid: tui-no-remote-addr-resolution
def resolve_remote_addr():
    return None


# ruleid: tui-no-client-monitor-session-name-generation
SESSION_NAME_PREFIX = "orch-monitor"


def monitor_session_name(repo):
    # ruleid: tui-no-client-monitor-session-name-generation
    return hashlib.md5(str(repo).encode()).hexdigest()


def monitor_session_name_sha256(source):
    # ruleid: tui-no-client-monitor-session-name-generation
    return "orch-monitor-" + hashlib.sha256(str(source).encode()).hexdigest()[:6]


# ruleid: tui-no-client-monitor-session-name-generation
def get_session_name(source_value):
    return hashlib.sha256(str(source_value).encode()).hexdigest()[:6]


def client_sort(runs):
    # ruleid: tui-no-client-side-run-sort
    return sorted(runs)


# ruleid: tui-no-client-side-filtering
def filter_runs_client_side(runs, filter_state):
    return [r for r in runs if r.agent in filter_state.agents]


def filter_runs_generic_comprehension(runs, selected_agents):
    # ruleid: tui-no-client-side-filtering
    return [run for run in runs if run.agent in selected_agents]


def filter_runs_loop_append(runs, selected_agents):
    filtered = []
    # ruleid: tui-no-client-side-filtering
    for run in runs:
        if run.agent in selected_agents:
            filtered.append(run)
    return filtered


def bare_except():
    try:
        return 1
    # ruleid: tui-no-bare-except
    except:
        return 0


def allowed_bootstrap_call():
    # ok: tui-no-git-subprocess
    subprocess.run(["orch", "debug", "client-bootstrap", "--json"])


def allowed_render(bootstrap, req):
    # ok: tui-no-client-monitor-session-name-generation
    session_name = bootstrap.monitor_session_name
    # ok: tui-no-proto-status-remap
    req.list_runs.status_text.append("running")
    # ok: tui-no-client-side-filtering
    req.list_runs.agents.append("codex")
    return session_name


def allowed_static_sort(dropdown_options):
    # ok: tui-no-client-side-run-sort
    return sorted(dropdown_options)
