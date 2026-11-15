#!/bin/sh
# yasssm installer for macOS (launchd): https://yasssm.luizlab.com.br
#   curl -fsSL https://yasssm.luizlab.com.br/install-macos.sh | sh
# Installs the latest release to ~/.local/bin, creates the login secret (shows a QR code) and
# runs yasssm as a launchd agent on 127.0.0.1:8090 while you are logged in. Safe to run again
# to update.
set -eu

repo=luiz0451/yasssm
bin="$HOME/.local/bin/yasssm"
label=com.yasssm
plist="$HOME/Library/LaunchAgents/$label.plist"
secret="$HOME/Library/Application Support/yasssm/totp"

fail() { echo "yasssm: $*" >&2; exit 1; }

[ "$(uname -s)" = Darwin ] || fail "this is the macOS installer; on Linux use https://yasssm.luizlab.com.br/install.sh"
case "$(uname -m)" in
arm64) arch=arm64 ;;
x86_64) arch=amd64 ;;
*) fail "no build for $(uname -m)" ;;
esac
command -v docker >/dev/null || echo "yasssm: docker not found; install Docker Desktop, OrbStack or Colima before managing stacks"

echo "Downloading yasssm (darwin-$arch)…"
mkdir -p "$(dirname "$bin")"
tmp=$(mktemp)
curl -fsSL -o "$tmp" "https://github.com/$repo/releases/latest/download/yasssm-darwin-$arch"
install -m 755 "$tmp" "$bin"
rm -f "$tmp"
xattr -d com.apple.quarantine "$bin" 2>/dev/null || true

if [ ! -f "$secret" ]; then
	echo
	echo "Scan this QR code with your authenticator app:"
	"$bin" totp-init
fi

mkdir -p "$(dirname "$plist")" "$HOME/Library/Logs"
cat >"$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>$label</string>
	<key>ProgramArguments</key><array><string>$bin</string><string>serve</string></array>
	<key>EnvironmentVariables</key>
	<dict>
		<key>YASSSM_ADDR</key><string>127.0.0.1:8090</string>
		<key>YASSSM_SECURE_COOKIE</key><string>1</string>
		<key>PATH</key><string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
	</dict>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
	<key>StandardErrorPath</key><string>$HOME/Library/Logs/yasssm.log</string>
</dict>
</plist>
EOF

launchctl bootout "gui/$(id -u)/$label" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$plist"

echo
echo "yasssm is running on http://127.0.0.1:8090"
echo "Reach it from your phone over Tailscale: https://yasssm.luizlab.com.br/docs.html#access"
