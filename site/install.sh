#!/bin/sh
# yasssm installer for Linux (systemd): https://yasssm.luizlab.com.br
#   curl -fsSL https://yasssm.luizlab.com.br/install.sh | sh
# Installs the latest release to ~/.local/bin, creates the login secret (shows a QR code) and
# runs yasssm as a systemd user service on 127.0.0.1:8090. Safe to run again to update.
set -eu

repo=luiz0451/yasssm
bin="$HOME/.local/bin/yasssm"
unit="$HOME/.config/systemd/user/yasssm.service"

fail() { echo "yasssm: $*" >&2; exit 1; }

[ "$(uname -s)" = Linux ] || fail "this is the Linux installer; on macOS use https://yasssm.luizlab.com.br/install-macos.sh"
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) fail "no build for $(uname -m)" ;;
esac
command -v systemctl >/dev/null || fail "needs systemd"
command -v docker >/dev/null || echo "yasssm: docker not found; install it, and let $(id -un) use it, before managing stacks"

echo "Downloading yasssm (linux-$arch)…"
mkdir -p "$(dirname "$bin")"
tmp=$(mktemp)
curl -fsSL -o "$tmp" "https://github.com/$repo/releases/latest/download/yasssm-linux-$arch"
install -m 755 "$tmp" "$bin"
rm -f "$tmp"

mkdir -p "$(dirname "$unit")"
cat >"$unit" <<EOF
[Unit]
Description=yasssm (Yet Another Simple Self-hosted Server Manager)
After=network-online.target
ConditionPathExists=%h/.config/yasssm/totp

[Service]
ExecStart=$bin serve
Environment=YASSSM_ADDR=127.0.0.1:8090
Environment=YASSSM_SECURE_COOKIE=1
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
EOF

if [ ! -f "$HOME/.config/yasssm/totp" ]; then
	echo
	echo "Scan this QR code with your authenticator app:"
	"$bin" totp-init
fi

systemctl --user daemon-reload
systemctl --user enable yasssm >/dev/null 2>&1
systemctl --user restart yasssm
loginctl enable-linger "$(id -un)" 2>/dev/null || echo "yasssm: run 'loginctl enable-linger' so it keeps running after you log out"

echo
echo "yasssm is running on http://127.0.0.1:8090"
echo "Reach it from your phone over Tailscale: https://yasssm.luizlab.com.br/docs.html#access"
