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
data_dir=$(root_path "${YEN_DATA_DIR:-/var/lib/yen}")
secrets_dir=$(root_path "${YEN_SECRETS_DIR:-/etc/yen}")
channel_env=${YEN_CHANNEL_ENV_FILE:-$secrets_dir/yen.env}
provider_env=${YEN_PROVIDER_ENV_FILE:-$secrets_dir/yen-provider.env}
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
cd "$source_dir"

release_dir=$state_dir/releases/$release_id
install -d -m 0755 "$release_dir" "$data_dir" "$secrets_dir"
if [ "$channel_env" != "$secrets_dir/yen.env" ]; then
	install -m 0600 "$channel_env" "$secrets_dir/yen.env"
else
	chmod 0600 "$channel_env"
fi
go build -o "$release_dir/theoses-telegram" "$source_dir/cmd/theoses-telegram"
go build -o "$release_dir/theoses-dashboard" "$source_dir/cmd/theoses-dashboard"
go build -o "$release_dir/theoses-rpc" "$source_dir/cmd/theoses-rpc"

cat >"$release_dir/run-telegram" <<EOF
#!/bin/sh
set -eu
set -a
. "$provider_env"
set +a
exec env -i PATH=/usr/bin:/bin HOME=/root \\
	  YEN_PROVIDER="\${YEN_PROVIDER:-}" \\
	  YEN_MODEL="\${YEN_MODEL:-z-ai/glm-5.3-flash}" \\
	  YEN_AUTH_FILE="\${YEN_AUTH_FILE:-}" \\
	  YEN_ANT_LING_API_KEY="\${YEN_ANT_LING_API_KEY:-}" \\
	  YEN_OPENAI_BASE_URL="\${YEN_OPENAI_BASE_URL:-https://openrouter.ai/api/v1}" \\
	  YEN_API_KEY="\${YEN_API_KEY:-}" \\
	  YEN_OPENAI_API_KEY="\${YEN_OPENAI_API_KEY:-}" \\
	  YEN_OPENROUTER_API_KEY="\${YEN_OPENROUTER_API_KEY:-}" \\
	  YEN_RESPONSES_BASE_URL="\${YEN_RESPONSES_BASE_URL:-}" \\
	  YEN_AZURE_OPENAI_BASE_URL="\${YEN_AZURE_OPENAI_BASE_URL:-}" \\
	  YEN_AZURE_OPENAI_API_KEY="\${YEN_AZURE_OPENAI_API_KEY:-}" \\
	  YEN_GOOGLE_API_KEY="\${YEN_GOOGLE_API_KEY:-}" \\
	  YEN_GOOGLE_BASE_URL="\${YEN_GOOGLE_BASE_URL:-}" \\
	  YEN_ANTHROPIC_API_KEY="\${YEN_ANTHROPIC_API_KEY:-}" \\
	  YEN_ANTHROPIC_BASE_URL="\${YEN_ANTHROPIC_BASE_URL:-https://api.anthropic.com/v1}" \\
	  YEN_DEEPSEEK_API_KEY="\${YEN_DEEPSEEK_API_KEY:-}" \\
	  YEN_BASETEN_API_KEY="\${YEN_BASETEN_API_KEY:-}" \\
	  YEN_CEREBRAS_API_KEY="\${YEN_CEREBRAS_API_KEY:-}" \\
	  YEN_FIREWORKS_API_KEY="\${YEN_FIREWORKS_API_KEY:-}" \\
	  YEN_HF_TOKEN="\${YEN_HF_TOKEN:-}" \\
	  YEN_KIMI_API_KEY="\${YEN_KIMI_API_KEY:-}" \\
	  YEN_GROQ_API_KEY="\${YEN_GROQ_API_KEY:-}" \\
	  YEN_MISTRAL_API_KEY="\${YEN_MISTRAL_API_KEY:-}" \\
	  YEN_MINIMAX_API_KEY="\${YEN_MINIMAX_API_KEY:-}" \\
	  YEN_MINIMAX_CN_API_KEY="\${YEN_MINIMAX_CN_API_KEY:-}" \\
	  YEN_MOONSHOT_API_KEY="\${YEN_MOONSHOT_API_KEY:-}" \\
	  YEN_NVIDIA_API_KEY="\${YEN_NVIDIA_API_KEY:-}" \\
	  YEN_QWEN_TOKEN_PLAN_API_KEY="\${YEN_QWEN_TOKEN_PLAN_API_KEY:-}" \\
	  YEN_QWEN_TOKEN_PLAN_CN_API_KEY="\${YEN_QWEN_TOKEN_PLAN_CN_API_KEY:-}" \\
	  YEN_TOGETHER_API_KEY="\${YEN_TOGETHER_API_KEY:-}" \\
	  YEN_XIAOMI_API_KEY="\${YEN_XIAOMI_API_KEY:-}" \\
	  YEN_XIAOMI_TOKEN_PLAN_AMS_API_KEY="\${YEN_XIAOMI_TOKEN_PLAN_AMS_API_KEY:-}" \\
	  YEN_XIAOMI_TOKEN_PLAN_CN_API_KEY="\${YEN_XIAOMI_TOKEN_PLAN_CN_API_KEY:-}" \\
	  YEN_XIAOMI_TOKEN_PLAN_SGP_API_KEY="\${YEN_XIAOMI_TOKEN_PLAN_SGP_API_KEY:-}" \\
	  YEN_VERCEL_AI_GATEWAY_API_KEY="\${YEN_VERCEL_AI_GATEWAY_API_KEY:-}" \\
	  YEN_XAI_API_KEY="\${YEN_XAI_API_KEY:-}" \\
	  YEN_ZAI_API_KEY="\${YEN_ZAI_API_KEY:-}" \\
	  YEN_ZAI_CODING_CN_API_KEY="\${YEN_ZAI_CODING_CN_API_KEY:-}" \\
	  YEN_TAVILY_API_KEY="\${YEN_TAVILY_API_KEY:-}" \\
	  YEN_TAVILY_API_KEY_2="\${YEN_TAVILY_API_KEY_2:-}" \\
	  YEN_CLOUDFLARE_ACCOUNT_ID="\${YEN_CLOUDFLARE_ACCOUNT_ID:-}" \\
	  YEN_CLOUDFLARE_GATEWAY_ID="\${YEN_CLOUDFLARE_GATEWAY_ID:-}" \\
	  YEN_CLOUDFLARE_API_KEY="\${YEN_CLOUDFLARE_API_KEY:-}" \\
	  YEN_CLOUDFLARE_BASE_URL="\${YEN_CLOUDFLARE_BASE_URL:-}" \\
	  YEN_CLOUDFLARE_API_TOKEN="\${YEN_CLOUDFLARE_API_TOKEN:-}" \\
	  YEN_TAVILY_ENDPOINT="\${YEN_TAVILY_ENDPOINT:-}" \\
	  YEN_OPENROUTER_IMAGE_ENDPOINT="\${YEN_OPENROUTER_IMAGE_ENDPOINT:-}" \\
	  YEN_OPENROUTER_IMAGE_MODEL="\${YEN_OPENROUTER_IMAGE_MODEL:-}" \\
	  YEN_IMAGE_MODEL="\${YEN_IMAGE_MODEL:-}" \\
  YEN_TELEGRAM_BOT_TOKEN="\${YEN_TELEGRAM_BOT_TOKEN:-}" \\
  YEN_TELEGRAM_CHAT_ID="\${YEN_TELEGRAM_CHAT_ID:-}" \\
  YEN_CANONICAL_CONVERSATION_ID="\${YEN_CANONICAL_CONVERSATION_ID:-}" \\
  YEN_TELEGRAM_API_BASE="\${YEN_TELEGRAM_API_BASE:-}" \\
  YEN_DASHBOARD_TOKEN="\${YEN_DASHBOARD_TOKEN:-}" \\
  YEN_AUTO_COMPACT_TURNS="\${YEN_AUTO_COMPACT_TURNS:-}" \\
  YEN_AUTO_COMPACT_MAX_HISTORY_TURNS="\${YEN_AUTO_COMPACT_MAX_HISTORY_TURNS:-}" \\
  YEN_AUTO_COMPACT_KEEP_RECENT_TOKENS="\${YEN_AUTO_COMPACT_KEEP_RECENT_TOKENS:-}" \\
  YEN_AUTO_COMPACT_CONTEXT_WINDOW="\${YEN_AUTO_COMPACT_CONTEXT_WINDOW:-}" \\
  YEN_AUTO_COMPACT_RESERVE_TOKENS="\${YEN_AUTO_COMPACT_RESERVE_TOKENS:-}" \\
  YEN_AUTO_COMPACT_ENABLED="\${YEN_AUTO_COMPACT_ENABLED:-}" \\
  YEN_AUTO_COMPACT_OVERFLOW="\${YEN_AUTO_COMPACT_OVERFLOW:-}" \\
  YEN_AUTO_CONSOLIDATE="\${YEN_AUTO_CONSOLIDATE:-}" \\
  YEN_DATA_DIR="$data_dir" \\
  YEN_OPENAI_BASE_URL="\${YEN_OPENAI_BASE_URL:-https://openrouter.ai/api/v1}" \\
  YEN_MODEL="\${YEN_MODEL:-z-ai/glm-5.3-flash}" \\
  YEN_REASONING_EFFORT="\${YEN_REASONING_EFFORT:-}" \\
  "$release_dir/theoses-telegram"
EOF

cat >"$release_dir/run-dashboard" <<EOF
#!/bin/sh
set -eu
set -a
. "$provider_env"
set +a
exec env -i PATH=/usr/bin:/bin HOME=/root \\
	  YEN_PROVIDER="\${YEN_PROVIDER:-}" \\
	  YEN_MODEL="\${YEN_MODEL:-z-ai/glm-5.3-flash}" \\
	  YEN_AUTH_FILE="\${YEN_AUTH_FILE:-}" \\
	  YEN_ANT_LING_API_KEY="\${YEN_ANT_LING_API_KEY:-}" \\
	  YEN_OPENAI_BASE_URL="\${YEN_OPENAI_BASE_URL:-https://openrouter.ai/api/v1}" \\
	  YEN_API_KEY="\${YEN_API_KEY:-}" \\
	  YEN_OPENAI_API_KEY="\${YEN_OPENAI_API_KEY:-}" \\
	  YEN_OPENROUTER_API_KEY="\${YEN_OPENROUTER_API_KEY:-}" \\
	  YEN_RESPONSES_BASE_URL="\${YEN_RESPONSES_BASE_URL:-}" \\
	  YEN_AZURE_OPENAI_BASE_URL="\${YEN_AZURE_OPENAI_BASE_URL:-}" \\
	  YEN_AZURE_OPENAI_API_KEY="\${YEN_AZURE_OPENAI_API_KEY:-}" \\
	  YEN_GOOGLE_API_KEY="\${YEN_GOOGLE_API_KEY:-}" \\
	  YEN_GOOGLE_BASE_URL="\${YEN_GOOGLE_BASE_URL:-}" \\
	  YEN_ANTHROPIC_API_KEY="\${YEN_ANTHROPIC_API_KEY:-}" \\
	  YEN_ANTHROPIC_BASE_URL="\${YEN_ANTHROPIC_BASE_URL:-https://api.anthropic.com/v1}" \\
	  YEN_DEEPSEEK_API_KEY="\${YEN_DEEPSEEK_API_KEY:-}" \\
	  YEN_BASETEN_API_KEY="\${YEN_BASETEN_API_KEY:-}" \\
	  YEN_CEREBRAS_API_KEY="\${YEN_CEREBRAS_API_KEY:-}" \\
	  YEN_FIREWORKS_API_KEY="\${YEN_FIREWORKS_API_KEY:-}" \\
	  YEN_HF_TOKEN="\${YEN_HF_TOKEN:-}" \\
	  YEN_KIMI_API_KEY="\${YEN_KIMI_API_KEY:-}" \\
	  YEN_GROQ_API_KEY="\${YEN_GROQ_API_KEY:-}" \\
	  YEN_MISTRAL_API_KEY="\${YEN_MISTRAL_API_KEY:-}" \\
	  YEN_MINIMAX_API_KEY="\${YEN_MINIMAX_API_KEY:-}" \\
	  YEN_MINIMAX_CN_API_KEY="\${YEN_MINIMAX_CN_API_KEY:-}" \\
	  YEN_MOONSHOT_API_KEY="\${YEN_MOONSHOT_API_KEY:-}" \\
	  YEN_NVIDIA_API_KEY="\${YEN_NVIDIA_API_KEY:-}" \\
	  YEN_QWEN_TOKEN_PLAN_API_KEY="\${YEN_QWEN_TOKEN_PLAN_API_KEY:-}" \\
	  YEN_QWEN_TOKEN_PLAN_CN_API_KEY="\${YEN_QWEN_TOKEN_PLAN_CN_API_KEY:-}" \\
	  YEN_TOGETHER_API_KEY="\${YEN_TOGETHER_API_KEY:-}" \\
	  YEN_XIAOMI_API_KEY="\${YEN_XIAOMI_API_KEY:-}" \\
	  YEN_XIAOMI_TOKEN_PLAN_AMS_API_KEY="\${YEN_XIAOMI_TOKEN_PLAN_AMS_API_KEY:-}" \\
	  YEN_XIAOMI_TOKEN_PLAN_CN_API_KEY="\${YEN_XIAOMI_TOKEN_PLAN_CN_API_KEY:-}" \\
	  YEN_XIAOMI_TOKEN_PLAN_SGP_API_KEY="\${YEN_XIAOMI_TOKEN_PLAN_SGP_API_KEY:-}" \\
	  YEN_VERCEL_AI_GATEWAY_API_KEY="\${YEN_VERCEL_AI_GATEWAY_API_KEY:-}" \\
	  YEN_XAI_API_KEY="\${YEN_XAI_API_KEY:-}" \\
	  YEN_ZAI_API_KEY="\${YEN_ZAI_API_KEY:-}" \\
	  YEN_ZAI_CODING_CN_API_KEY="\${YEN_ZAI_CODING_CN_API_KEY:-}" \\
	  YEN_TAVILY_API_KEY="\${YEN_TAVILY_API_KEY:-}" \\
	  YEN_TAVILY_API_KEY_2="\${YEN_TAVILY_API_KEY_2:-}" \\
	  YEN_CLOUDFLARE_ACCOUNT_ID="\${YEN_CLOUDFLARE_ACCOUNT_ID:-}" \\
	  YEN_CLOUDFLARE_GATEWAY_ID="\${YEN_CLOUDFLARE_GATEWAY_ID:-}" \\
	  YEN_CLOUDFLARE_API_KEY="\${YEN_CLOUDFLARE_API_KEY:-}" \\
	  YEN_CLOUDFLARE_BASE_URL="\${YEN_CLOUDFLARE_BASE_URL:-}" \\
	  YEN_CLOUDFLARE_API_TOKEN="\${YEN_CLOUDFLARE_API_TOKEN:-}" \\
	  YEN_TAVILY_ENDPOINT="\${YEN_TAVILY_ENDPOINT:-}" \\
	  YEN_OPENROUTER_IMAGE_ENDPOINT="\${YEN_OPENROUTER_IMAGE_ENDPOINT:-}" \\
	  YEN_OPENROUTER_IMAGE_MODEL="\${YEN_OPENROUTER_IMAGE_MODEL:-}" \\
	  YEN_IMAGE_MODEL="\${YEN_IMAGE_MODEL:-}" \\
  YEN_CANONICAL_CONVERSATION_ID="\${YEN_CANONICAL_CONVERSATION_ID:-}" \\
  YEN_DASHBOARD_TOKEN="\${YEN_DASHBOARD_TOKEN:-}" \\
  YEN_AUTO_COMPACT_TURNS="\${YEN_AUTO_COMPACT_TURNS:-}" \\
  YEN_AUTO_COMPACT_MAX_HISTORY_TURNS="\${YEN_AUTO_COMPACT_MAX_HISTORY_TURNS:-}" \\
  YEN_AUTO_COMPACT_KEEP_RECENT_TOKENS="\${YEN_AUTO_COMPACT_KEEP_RECENT_TOKENS:-}" \\
  YEN_AUTO_COMPACT_CONTEXT_WINDOW="\${YEN_AUTO_COMPACT_CONTEXT_WINDOW:-}" \\
  YEN_AUTO_COMPACT_RESERVE_TOKENS="\${YEN_AUTO_COMPACT_RESERVE_TOKENS:-}" \\
  YEN_AUTO_COMPACT_ENABLED="\${YEN_AUTO_COMPACT_ENABLED:-}" \\
  YEN_AUTO_COMPACT_OVERFLOW="\${YEN_AUTO_COMPACT_OVERFLOW:-}" \\
  YEN_AUTO_CONSOLIDATE="\${YEN_AUTO_CONSOLIDATE:-}" \\
  YEN_DATA_DIR="$data_dir" \\
  YEN_OPENAI_BASE_URL="\${YEN_OPENAI_BASE_URL:-https://openrouter.ai/api/v1}" \\
  YEN_MODEL="\${YEN_MODEL:-z-ai/glm-5.3-flash}" \\
  YEN_REASONING_EFFORT="\${YEN_REASONING_EFFORT:-}" \\
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
EnvironmentFile=$secrets_dir/yen.env
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
EnvironmentFile=$secrets_dir/yen.env
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
