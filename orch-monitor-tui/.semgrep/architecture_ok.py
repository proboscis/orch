import subprocess


def allowed_bootstrap_call():
    subprocess.run(["orch", "debug", "client-bootstrap", "--json"])


def allowed_render(bootstrap, req):
    session_name = bootstrap.monitor_session_name
    req.list_runs.status_text.append("running")
    req.list_runs.agents.append("codex")
    return session_name


def allowed_static_sort(dropdown_options):
    return sorted(dropdown_options)
