# 🐍 yasssm

Website and docs: [yasssm.luizlab.com.br](https://yasssm.luizlab.com.br)

Yet Another Simple Self-hosted Server Manager: manage your Linux or macOS server from any device on your private network. One Go binary, server-rendered pages, no JavaScript, TOTP login.

It shows and manages:

- Docker Compose stacks and their containers: start, stop, restart, pull and recreate, logs (the latest lines or one day, highlighted)
- uptime, load, memory and disk
- services: systemd on Linux, your launchd agents on macOS (read only)
- cron: your crontab, the system cron files and (Linux) systemd timers, with the code they run (read only)

Every action asks for confirmation and shows the exact commands before running them.

## Build

Needs Go. `make` downloads the Tailwind CLI on first use.

```sh
make build-linux   # bin/yasssm-linux-amd64
make build-darwin  # bin/yasssm-darwin-arm64
make test lint
```

## Install

Run it as your user, who must be able to use `docker`.

Linux (systemd user service):

```sh
install -m 755 bin/yasssm-linux-amd64 ~/.local/bin/yasssm
yasssm totp-init        # shows a QR code for your authenticator app
cp deploy/yasssm.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now yasssm
loginctl enable-linger  # keep it running after logout and start it at boot
```

macOS (launchd agent; starts when you log in):

```sh
install -m 755 bin/yasssm-darwin-arm64 ~/.local/bin/yasssm
yasssm totp-init
sed "s|HOME|$HOME|g" deploy/com.yasssm.plist > ~/Library/LaunchAgents/com.yasssm.plist
launchctl load ~/Library/LaunchAgents/com.yasssm.plist
```

`yasssm totp-qr` shows the QR code again. `yasssm totp-init -force` makes a new secret.

## Access

yasssm listens on `127.0.0.1:8090` only. To reach it from your devices, publish it on your tailnet with Tailscale. The server must be a tagged node and your user its operator (`sudo tailscale set --operator=$USER`):

```sh
tailscale serve --service=svc:yasssm --https=443 --bg http://127.0.0.1:8090
```

Approve the service once in the Tailscale admin console. It is then at `https://yasssm.<your-tailnet>.ts.net`. Don't expose it to the internet: Docker access is as good as root.

## Configuration

| Variable | Default |
|---|---|
| `YASSSM_ADDR` | `127.0.0.1:8090` |
| `YASSSM_STACKS_DIR` | `$HOME` (one stack per subfolder) |
| `YASSSM_TOTP_FILE` | `yasssm/totp` in your config dir (`~/.config` on Linux, `~/Library/Application Support` on macOS) |
| `YASSSM_SECURE_COOKIE` | set to `1` when behind HTTPS |

## Credits

UI built with [templ](https://templ.guide) and [shadcn-templ](https://shadcn-templ.com). Fonts: Inter and Fira Code (OFL). Icons: [Phosphor](https://phosphoricons.com). App icon: snake from [Noto Emoji](https://github.com/googlefonts/noto-emoji) (Apache 2.0).

## License

[GPL-3.0](LICENSE). The website in `site/` is built with `make site` and published to GitHub Pages on every push to `main`.
