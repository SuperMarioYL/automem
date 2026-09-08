import os, subprocess, tempfile
with tempfile.TemporaryDirectory(prefix="automem-demo-") as folder:
    env = dict(os.environ, AUTOMEM_DIR=folder)
    def run(*args):
        return subprocess.run(["go", "run", "./cmd/automem", *args], env=env, text=True, capture_output=True, check=True).stdout
    run("capture", "--agent", "claude-code", "--cwd", "/demo/project", "examples/session.transcript")
    print(run("recall", "auth.py constructor").strip())
    print(run("stats").strip())
