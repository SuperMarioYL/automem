# vhs tape — automem: two sessions, and the second remembers the first.
# Renders to assets/demo.gif. Drives the REAL automem binary end to end:
#   capture session 1  ->  capture session 2  ->  recall  ->  stats
# so the gif shows genuine output, not a mock. The demo.yml workflow builds
# `automem` onto PATH before running this tape; locally you can too:
#   go build -o automem ./cmd/automem && PATH="$PWD:$PATH" vhs docs/demo.tape
#
# Docs: https://github.com/charmbracelet/vhs

Output assets/demo.gif

Set Shell "bash"
Set FontSize 17
Set Width 1200
Set Height 640
Set Padding 24
Set Theme "Aardvark Blue"
Set TypingSpeed 42ms

# Isolate the store so the demo is reproducible and never touches a real one.
Hide
Type "export AUTOMEM_DIR=$(mktemp -d)/.automem" Enter
Type "clear" Enter
Show

Type "# 1. Your coding agent works a session. On exit, its Stop hook captures it."
Sleep 800ms Enter
Type `printf 'User: refactor auth.py to use dataclasses\nUser: keep the old constructor working\n' | automem capture --agent claude-code --cwd ~/proj/api`
Sleep 400ms Enter
Sleep 1500ms

Type "# 2. A later, unrelated session. Also captured."
Sleep 700ms Enter
Type `printf 'User: add retry logic to the http client in client.py\n' | automem capture --agent claude-code --cwd ~/proj/api`
Sleep 400ms Enter
Sleep 1500ms

Type "# 3. New terminal, new agent session. SessionStart recalls what matters."
Sleep 700ms Enter
Type `automem recall "what did we decide about auth.py last session?"`
Sleep 400ms Enter
Sleep 2600ms

Type "# 4. Proof it's actually used — stored vs injected."
Sleep 700ms Enter
Type "automem stats"
Sleep 400ms Enter
Sleep 2800ms

Type "# One binary. No vector DB, no account, no key."
Sleep 800ms Enter
Sleep 1800ms
