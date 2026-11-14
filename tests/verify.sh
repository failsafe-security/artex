#!/bin/sh
# Exercise the real verify recipe with harmless stand-ins; no application or Docker daemon needed.
set -eu
cd "$(dirname "$0")/.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
real_grep=$(command -v grep)
real_tail=$(command -v tail)
export REAL_GREP="$real_grep" REAL_TAIL="$real_tail"
export CALL_LOG="$work/calls"

cat > "$work/tool" <<'EOF'
#!/bin/sh
set -eu
tool=${0##*/}
case "$tool" in
  docker)
    if [ "$1" = run ]; then
      for arg do command=$arg; done
      exec /bin/sh -c "$command"
    fi
    tool=image-inspect
    ;;
  node)
    if [ "$1" = -e ]; then tool=browser; fi
    ;;
esac
printf '%s\n' "$tool" >> "$CALL_LOG"
if [ "$tool" = "${FAIL_TOOL:-}" ]; then exit 42; fi
case "$tool" in
  strings) printf '%s\n' "${MARKER:-harmless binary text}" ;;
  grep) exec "$REAL_GREP" "$@" ;;
  tail) exec "$REAL_TAIL" "$@" ;;
esac
EOF
chmod +x "$work/tool"
for tool in docker sha256sum strings grep id node playwright-mcp playwright-cli nmap tail ls; do
  ln -s tool "$work/$tool"
done
export PATH="$work:$PATH"

check_failure() {
  : > "$CALL_LOG"
  if make verify COMPOSE_BIN=docker > "$work/output" 2>&1; then
    printf 'FAIL: verify accepted %s\n' "$1" >&2
    cat "$work/output" >&2
    exit 1
  fi
  if "$REAL_GREP" -q '^image-inspect$' "$CALL_LOG"; then
    printf 'FAIL: image inspection ran after %s\n' "$1" >&2
    exit 1
  fi
}

for tool in sha256sum strings grep id node playwright-mcp playwright-cli browser nmap tail ls; do
  export FAIL_TOOL="$tool"
  check_failure "$tool failure"
  if [ "$tool" = browser ] && "$REAL_GREP" -q '^nmap$' "$CALL_LOG"; then
    echo 'FAIL: checks continued after browser failure' >&2
    exit 1
  fi
  printf 'PASS: %s failure stops verification\n' "$tool"
done
unset FAIL_TOOL
for marker in verifyCaptcha tomicdd congdonglinux; do
  export MARKER="$marker"
  check_failure "$marker present"
  printf 'PASS: %s marker rejects image\n' "$marker"
done
unset MARKER
: > "$CALL_LOG"
make verify COMPOSE_BIN=docker > "$work/output" 2>&1
"$REAL_GREP" -q '^image-inspect$' "$CALL_LOG"
echo 'PASS: successful checks reach image inspection'

export FAIL_TOOL=image-inspect
if make verify COMPOSE_BIN=docker > "$work/output" 2>&1; then
  echo 'FAIL: verify accepted image inspection failure' >&2
  exit 1
fi
echo 'PASS: image inspection failure rejects verification'
