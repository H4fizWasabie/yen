#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
	echo "install-side-by-side.sh must run as root" >&2
	exit 1
fi

source_dir=${YEN_SOURCE_DIR:-$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)}
root_dir=${YEN_ROOT:-}
root_path() {
	case "$1" in
		/*) printf '%s%s' "$root_dir" "$1" ;;
		*) printf '%s' "$1" ;;
	esac
}
state_dir=$(root_path "${YEN_STATE_DIR:-/opt/yen}")
data_dir=$(root_path "${YEN_DATA_DIR:-/var/lib/theoses-go-telegram}")
secrets_dir=$(root_path "${YEN_SECRETS_DIR:-/etc/theoses-go}")
channel_env=${YEN_CHANNEL_ENV_FILE:-$secrets_dir/telegram.env}
provider_env=${YEN_PROVIDER_ENV_FILE:-/home/theoses/.theoses/agent/theoses.env}
release_id=${YEN_RELEASE_ID:-$(git -C "$source_dir" rev-parse --short=7 HEAD)}
dashboard_addr=${YEN_DASHBOARD_ADDR:-127.0.0.1:30146}
start=${YEN_START:-0}
systemctl_command=${YEN_SYSTEMCTL:-systemctl}

if [ ! -f "$channel_env" ]; then
	echo "missing channel environment file: $channel_env" >&2
	exit 1
fi
if [ ! -f "$provider_env" ]; then
	echo "missing provider environment file: $provider_env" >&2
	exit 1
fi

release_dir=$state_dir/releases/$release_id
install -d -m 0755 "$release_dir" "$data_dir" "$secrets_dir"
if [ "$channel_env" != "$secrets_dir/telegram.env" ]; then
	install -m 0600 "$channel_env" "$secrets_dir/telegram.env"
else
	chmod 0600 "$channel_env"
fi
go build -o "$release_dir/theoses-telegram" "$source_dir/cmd/theoses-telegram"
go build -o "$release_dir/theoses-dashboard" "$source_dir/cmd/theoses-dashboard"

cat >"$release_dir/run-telegram" <<EOF
#!/bin/sh
set -eu
set -a
. "$provider_env"
set +a
exec env -i PATH=/usr/bin:/bin HOME=/root \\
  OPENAI_API_KEY="\${OPENAI_API_KEY:-}" \\
  THEOSES_TELEGRAM_BOT_TOKEN="\${THEOSES_TELEGRAM_BOT_TOKEN:-}" \\
  THEOSES_TELEGRAM_CHAT_ID="\${THEOSES_TELEGRAM_CHAT_ID:-}" \\
  THEOSES_CANONICAL_CONVERSATION_ID="\${THEOSES_CANONICAL_CONVERSATION_ID:-}" \\
  THEOSES_TELEGRAM_API_BASE="\${THEOSES_TELEGRAM_API_BASE:-}" \\
  THEOSES_DASHBOARD_TOKEN="\${THEOSES_DASHBOARD_TOKEN:-}" \\
  THEOSES_AUTO_COMPACT_TURNS="\${THEOSES_AUTO_COMPACT_TURNS:-}" \\
  THEOSES_AUTO_COMPACT_OVERFLOW="\${THEOSES_AUTO_COMPACT_OVERFLOW:-}" \\
  THEOSES_AUTO_CONSOLIDATE="\${THEOSES_AUTO_CONSOLIDATE:-}" \\
  THEOSES_DATA_DIR="$data_dir" \\
  THEOSES_OPENAI_BASE_URL="\${THEOSES_OPENAI_BASE_URL:-https://api.openai.com/v1}" \\
  THEOSES_MODEL="\${THEOSES_MODEL:-gpt-4o-mini}" \\
  "$release_dir/theoses-telegram"
EOF

cat >"$release_dir/run-dashboard" <<EOF
#!/bin/sh
set -eu
set -a
. "$provider_env"
set +a
exec env -i PATH=/usr/bin:/bin HOME=/root \\
  OPENAI_API_KEY="\${OPENAI_API_KEY:-}" \\
  THEOSES_CANONICAL_CONVERSATION_ID="\${THEOSES_CANONICAL_CONVERSATION_ID:-}" \\
  THEOSES_DASHBOARD_TOKEN="\${THEOSES_DASHBOARD_TOKEN:-}" \\
  THEOSES_AUTO_COMPACT_TURNS="\${THEOSES_AUTO_COMPACT_TURNS:-}" \\
  THEOSES_AUTO_COMPACT_OVERFLOW="\${THEOSES_AUTO_COMPACT_OVERFLOW:-}" \\
  THEOSES_AUTO_CONSOLIDATE="\${THEOSES_AUTO_CONSOLIDATE:-}" \\
  THEOSES_DATA_DIR="$data_dir" \\
  THEOSES_OPENAI_BASE_URL="\${THEOSES_OPENAI_BASE_URL:-https://api.openai.com/v1}" \\
  THEOSES_MODEL="\${THEOSES_MODEL:-gpt-4o-mini}" \\
  "$release_dir/theoses-dashboard" -addr "$dashboard_addr"
EOF
chmod 0755 "$release_dir/run-telegram" "$release_dir/run-dashboard"
ln -sfn "$release_dir" "$state_dir/current"

unit_dir=$(root_path /etc/systemd/system)
install -d -m 0755 "$unit_dir"
cat > "$unit_dir/yen-telegram-pilot.service" <<EOF
[Unit]
Description=Yen Go Telegram pilot (side-by-side)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
Group=root
WorkingDirectory=$release_dir
EnvironmentFile=$secrets_dir/telegram.env
ExecStart=$release_dir/run-telegram
Restart=on-failure
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

cat > "$unit_dir/yen-dashboard-pilot.service" <<EOF
[Unit]
Description=Yen Go dashboard pilot (side-by-side)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
Group=root
WorkingDirectory=$release_dir
EnvironmentFile=$secrets_dir/telegram.env
ExecStart=$release_dir/run-dashboard
Restart=on-failure
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

"$systemctl_command" daemon-reload
"$systemctl_command" enable yen-telegram-pilot.service yen-dashboard-pilot.service >/dev/null
if [ "$start" = 1 ]; then
	"$systemctl_command" restart yen-telegram-pilot.service yen-dashboard-pilot.service
fi

echo "installed Yen release $release_id"
echo "start with: systemctl restart yen-telegram-pilot.service yen-dashboard-pilot.service"
