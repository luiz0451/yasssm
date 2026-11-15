# 🐍 yasssm

Yet Another Simple Self-hosted Server Manager. A single Go binary that renders a web UI on the server (templ + [shadcn-templ](https://shadcn-templ.com), no JavaScript) for one host:

- Docker Compose stacks found at `$YASSSM_STACKS_DIR/*/` (the config files Compose was started with, else a standard `compose.yaml`/`docker-compose.yml`) and their containers: up, restart, stop, pull & recreate
- Container start / restart / stop and logs
- System uptime, load, memory, disk
- systemd services (system and user), read only, failed first
- The user's crontab, read only

Every action goes through a confirmation page that shows the exact commands, then runs as a background job whose page refreshes until it finishes. Login is a TOTP code (RFC 6238); the server rejects reused codes and locks login for 5 minutes after 5 failures.

## Build

```sh
make build-linux     # templ generate + tailwind + go build → bin/yasssm-linux-amd64
make test lint
```

`make gen` downloads the Tailwind standalone CLI into `bin/` on first use.

The look is shadcn-templ with a custom theme in `assets/css/globals.css`: Inter for the interface and Fira Code only for machine text (paths, images, ports, commands, logs), both OFL and self-hosted in `assets/fonts/` because the CSP allows no font CDN, and [Phosphor](https://phosphoricons.com) duotone icons. `components/icon` carries only the icons the views use; `shadcn-templ add icon` would bring back Lucide, so add Phosphor icons by hand (see `icon_data.go`).

## Install (as the login user, in the docker group)

```sh
install -m 755 yasssm-linux-amd64 ~/.local/bin/yasssm
yasssm totp-init                      # prints a QR code (and the secret) for your authenticator app
cp deploy/yasssm.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now yasssm
```

`yasssm totp-qr` shows the QR code of the current secret again, e.g. to enroll a second device.

### Tailnet service

yasssm listens on loopback; [Tailscale Services](https://tailscale.com/kb/1552/tailscale-services) publish it with HTTPS under its own MagicDNS name. The host must be a tagged node and the user the Tailscale operator (`tailscale set --operator=$USER`).

```sh
tailscale serve --service=svc:yasssm --https=443 --bg http://127.0.0.1:8090
```

Approve `svc:yasssm` for the host in the admin console (Services) once; then it is `https://yasssm.<tailnet>.ts.net`. Remove with `tailscale serve clear svc:yasssm`.

## Configuration

| Variable | Default |
|---|---|
| `YASSSM_ADDR` | `127.0.0.1:8090` |
| `YASSSM_STACKS_DIR` | `$HOME` |
| `YASSSM_TOTP_FILE` | `$XDG_CONFIG_HOME/yasssm/totp` |
| `YASSSM_SECURE_COOKIE` | unset; `1` marks the session cookie `Secure` (set when behind an HTTPS proxy) |

`yasssm totp-init -force` replaces the secret; a running server picks it up on the next login.

## Security notes

Docker access is root-equivalent. Keep the listener on loopback behind `tailscale serve` (tailnet only, never Funnel). POSTs are protected by Go's `http.CrossOriginProtection` and `SameSite=Strict` cookies. The CSP forbids all scripts.
