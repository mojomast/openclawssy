#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  .factory/scripts/agent-browser-bootstrap.sh --session <name> <agent-browser-command> [args...]

Examples:
  .factory/scripts/agent-browser-bootstrap.sh --session "flow-a" open "http://localhost:8081/dashboard"
  .factory/scripts/agent-browser-bootstrap.sh --session "flow-a" snapshot
  .factory/scripts/agent-browser-bootstrap.sh --session "flow-a" close

Behavior:
  - Prepend Playwright fallback libs to LD_LIBRARY_PATH when available.
  - Attempt normal agent-browser launch first.
  - If launch fails with shared-library/runtime startup errors, start deterministic
    Chromium CDP fallback and rerun the command through --cdp.
  - Persist per-session mode under .factory/runtime/agent-browser.
  - Set AGENT_BROWSER_BOOTSTRAP_FORCE_CDP=1 to force deterministic CDP mode
    (useful for fallback smoke testing).
EOF
}

SESSION=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --session)
      if [[ $# -lt 2 ]]; then
        echo "error: --session requires a value" >&2
        exit 2
      fi
      SESSION="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      break
      ;;
    *)
      break
      ;;
  esac
done

if [[ -z "$SESSION" ]]; then
  echo "error: --session is required" >&2
  usage >&2
  exit 2
fi

if [[ $# -lt 1 ]]; then
  echo "error: missing agent-browser command" >&2
  usage >&2
  exit 2
fi

COMMAND=("$@")
SESSION_SLUG="$(printf '%s' "$SESSION" | tr -cs '[:alnum:]._-' '_')"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STATE_ROOT="${AGENT_BROWSER_BOOTSTRAP_STATE_DIR:-$SCRIPT_DIR/../runtime/agent-browser}"
LOG_DIR="$STATE_ROOT/logs"
STATE_FILE="$STATE_ROOT/${SESSION_SLUG}.state"

PLAYWRIGHT_FALLBACK_LIB_DIR="${PLAYWRIGHT_FALLBACK_LIB_DIR:-/home/mojo/.cache/playwright-libs/root/usr/lib/x86_64-linux-gnu}"

mkdir -p "$STATE_ROOT" "$LOG_DIR"

prepend_path() {
  local path_to_prepend="$1"
  local current_value="${2:-}"
  IFS=':' read -r -a entries <<<"$current_value"
  for item in "${entries[@]}"; do
    if [[ "$item" == "$path_to_prepend" ]]; then
      printf '%s' "$current_value"
      return 0
    fi
  done
  if [[ -n "$current_value" ]]; then
    printf '%s:%s' "$path_to_prepend" "$current_value"
  else
    printf '%s' "$path_to_prepend"
  fi
}

setup_environment() {
  if [[ -z "${AGENT_BROWSER_HOME:-}" ]]; then
    export AGENT_BROWSER_HOME="$STATE_ROOT/home-${SESSION_SLUG}"
  fi
  mkdir -p "$AGENT_BROWSER_HOME"

  if [[ -d "$PLAYWRIGHT_FALLBACK_LIB_DIR" ]]; then
    export LD_LIBRARY_PATH
    LD_LIBRARY_PATH="$(prepend_path "$PLAYWRIGHT_FALLBACK_LIB_DIR" "${LD_LIBRARY_PATH:-}")"
  fi
}

MODE=""
CDP_PORT=""
CHROMIUM_PID=""
CHROMIUM_BIN=""

load_state() {
  MODE=""
  CDP_PORT=""
  CHROMIUM_PID=""
  CHROMIUM_BIN=""

  if [[ -f "$STATE_FILE" ]]; then
    # shellcheck disable=SC1090
    source "$STATE_FILE"
  fi
}

save_state() {
  cat >"$STATE_FILE" <<EOF
MODE=${MODE}
CDP_PORT=${CDP_PORT}
CHROMIUM_PID=${CHROMIUM_PID}
CHROMIUM_BIN=${CHROMIUM_BIN}
EOF
}

select_chromium_binary() {
  local candidates=()
  local path

  for path in \
    "$HOME"/.cache/ms-playwright/chromium-*/chrome-linux/chrome \
    "$HOME"/.cache/ms-playwright/chromium_headless_shell-*/chrome-headless-shell-linux64/chrome-headless-shell; do
    if [[ -x "$path" ]]; then
      candidates+=("$path")
    fi
  done

  if [[ ${#candidates[@]} -eq 0 ]]; then
    for path in "$(command -v chromium 2>/dev/null || true)" "$(command -v chromium-browser 2>/dev/null || true)" "$(command -v google-chrome 2>/dev/null || true)"; do
      if [[ -n "$path" && -x "$path" ]]; then
        candidates+=("$path")
      fi
    done
  fi

  if [[ ${#candidates[@]} -eq 0 ]]; then
    return 1
  fi

  printf '%s\n' "${candidates[@]}" | sort -V | tail -n 1
}

derive_cdp_port() {
  local checksum
  checksum="$(printf '%s' "$SESSION" | cksum | awk '{print $1}')"
  printf '%s' $((9440 + checksum % 40))
}

agent_browser_call() {
  local extra=()
  if [[ "$MODE" == "cdp" && -n "$CDP_PORT" ]]; then
    extra+=(--cdp "$CDP_PORT")
  fi

  agent-browser --session "$SESSION" "${extra[@]}" "$@"
}

error_requires_fallback() {
  local logfile="$1"
  grep -Eiq 'libnspr4|error while loading shared libraries|failed to launch|browser process exited|failed to connect to browser|executable doesn.t exist|daemon failed to start' "$logfile"
}

start_chromium_cdp() {
  local chromium_bin="$1"
  local cdp_port="$2"
  local profile_dir="$STATE_ROOT/profile-${SESSION_SLUG}"
  local chromium_log="$LOG_DIR/chromium-${SESSION_SLUG}.log"
  local pid

  mkdir -p "$profile_dir"

  "$chromium_bin" \
    --headless=new \
    --disable-gpu \
    --no-first-run \
    --no-default-browser-check \
    --disable-dev-shm-usage \
    --remote-debugging-address=127.0.0.1 \
    --remote-debugging-port="$cdp_port" \
    --user-data-dir="$profile_dir" \
    about:blank >"$chromium_log" 2>&1 &
  pid=$!

  for _ in $(seq 1 30); do
    if curl -fsS "http://127.0.0.1:${cdp_port}/json/version" >/dev/null 2>&1; then
      printf '%s' "$pid"
      return 0
    fi
    if ! kill -0 "$pid" 2>/dev/null; then
      break
    fi
    sleep 0.2
  done

  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
  fi

  return 1
}

cleanup_fallback_browser() {
  if [[ -n "$CHROMIUM_PID" ]] && kill -0 "$CHROMIUM_PID" 2>/dev/null; then
    kill "$CHROMIUM_PID" 2>/dev/null || true
  fi
}

ensure_bootstrap() {
  local direct_log="$LOG_DIR/bootstrap-${SESSION_SLUG}-direct.log"
  local cdp_log="$LOG_DIR/bootstrap-${SESSION_SLUG}-cdp.log"
  local chromium_bin
  local force_cdp="${AGENT_BROWSER_BOOTSTRAP_FORCE_CDP:-0}"

  setup_environment
  load_state

  if [[ "$MODE" == "cdp" && -n "$CDP_PORT" ]]; then
    if curl -fsS "http://127.0.0.1:${CDP_PORT}/json/version" >/dev/null 2>&1; then
      return 0
    fi
    cleanup_fallback_browser
    rm -f "$STATE_FILE"
    load_state
  fi

  if [[ "$MODE" == "direct" ]]; then
    return 0
  fi

  if [[ "$force_cdp" != "1" ]] && agent_browser_call open about:blank >"$direct_log" 2>&1; then
    MODE="direct"
    CDP_PORT=""
    CHROMIUM_PID=""
    CHROMIUM_BIN=""
    save_state
    return 0
  fi

  if [[ "$force_cdp" != "1" ]] && grep -Eiq 'daemon failed to start' "$direct_log"; then
    sleep 0.4
    if agent_browser_call open about:blank >"$direct_log" 2>&1; then
      MODE="direct"
      CDP_PORT=""
      CHROMIUM_PID=""
      CHROMIUM_BIN=""
      save_state
      return 0
    fi
  fi

  if [[ "$force_cdp" != "1" ]] && ! error_requires_fallback "$direct_log"; then
    cat "$direct_log" >&2
    return 1
  fi

  chromium_bin="$(select_chromium_binary || true)"
  if [[ -z "$chromium_bin" ]]; then
    echo "error: Chromium fallback requested but no Chromium binary found." >&2
    echo "direct launch log: $direct_log" >&2
    return 1
  fi

  MODE="cdp"
  CDP_PORT="$(derive_cdp_port)"
  CHROMIUM_BIN="$chromium_bin"
  CHROMIUM_PID="$(start_chromium_cdp "$chromium_bin" "$CDP_PORT")"
  save_state

  if agent_browser_call open about:blank >"$cdp_log" 2>&1; then
    return 0
  fi

  if grep -Eiq 'daemon failed to start' "$cdp_log"; then
    sleep 0.4
    if agent_browser_call open about:blank >"$cdp_log" 2>&1; then
      return 0
    fi
  fi

  cat "$cdp_log" >&2
  return 1
}

run_command() {
  local command_log="$LOG_DIR/command-${SESSION_SLUG}.log"

  if agent_browser_call "${COMMAND[@]}" >"$command_log" 2>&1; then
    cat "$command_log"
    return 0
  fi

  if grep -Eiq 'daemon failed to start' "$command_log"; then
    sleep 0.4
    if agent_browser_call "${COMMAND[@]}" >"$command_log" 2>&1; then
      cat "$command_log"
      return 0
    fi
  fi

  if [[ "$MODE" != "cdp" ]] && error_requires_fallback "$command_log"; then
    MODE=""
    CDP_PORT=""
    CHROMIUM_PID=""
    CHROMIUM_BIN=""
    rm -f "$STATE_FILE"
    ensure_bootstrap
    if agent_browser_call "${COMMAND[@]}" >"$command_log" 2>&1; then
      cat "$command_log"
      return 0
    fi
  fi

  cat "$command_log" >&2
  return 1
}

if [[ "${COMMAND[0]}" == "close" ]]; then
  setup_environment
  load_state

  if [[ "$MODE" == "cdp" && -n "$CDP_PORT" ]]; then
    agent-browser --session "$SESSION" --cdp "$CDP_PORT" close >/dev/null 2>&1 || true
  else
    agent-browser --session "$SESSION" close >/dev/null 2>&1 || true
  fi

  cleanup_fallback_browser
  rm -f "$STATE_FILE"
  exit 0
fi

ensure_bootstrap
run_command
