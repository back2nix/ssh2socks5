# SSH2SOCKS5 Proxy

A lightweight SOCKS5 proxy that tunnels traffic through SSH connections. Available as both a standalone Go application and an Android app.

## Features

- SOCKS5 proxy server that works over SSH tunnels
- SSH authentication using password or private key
- Android app with persistent connection management
- Automatic reconnection on connection loss

## Requirements

- Nix package manager
- SSH key access to your server
- NekoBox app (for system-wide VPN on Android)

## SSH Setup

1. Generate SSH key (if you don't have one):
```bash
ssh-keygen -t rsa -b 4096 -f ~/.ssh/google-france-key
```

2. Copy public key to your server:
```bash
ssh-copy-id -i ~/.ssh/google-france-key.pub username@your_server
```

## Quick Start

1. Install Nix and direnv:
```bash
curl --proto '=https' --tlsv1.2 -sSf -L https://install.determinate.systems/nix | sh -s -- install
```

2. Set up the environment:
```bash
cd ssh2socks5
direnv allow
```
Or use:
```bash
nix develop
```

3. Build:

For Android app:
```bash
make build-android
```

For standalone proxy:
```bash
make build-go
```

4. Run the proxy:
```bash
make run
```
- or
```
nix run github:back2nix/ssh2socks5#ssh2socks5 -- -lport=1081 -host=35.193.63.104 -user=bg -key=/home/bg/Documents/code/backup/.ssh/google-france-key
```
- or
```
nix run .#ssh2socks5 -- -lport=1081 -host=35.193.63.104 -user=bg -key=/home/bg/Documents/code/backup/.ssh/google-france-key
```

### Решение - увеличить лимиты в SSH:
bashsudo nano /etc/ssh/sshd_config
Найдите и измените/добавьте:
```bash
MaxStartups 50:30:100
MaxSessions 100
MaxAuthTries 200
```
Перезапустите SSH:
```bash
sudo systemctl restart ssh
```



## Usage

### Desktop
Configure your applications to use the SOCKS5 proxy at `127.0.0.1:1081` (default port).

### Android
1. Install the SSH2SOCKS5 APK
2. Enter your SSH server details and private key
3. Start the proxy

Note: The app itself doesn't create a system-wide VPN. To route all device traffic through the proxy:
1. Install NekoBox from F-Droid or Google Play
2. Create a new SOCKS5 connection in NekoBox
3. Configure it to connect to `127.0.0.1:1081`
4. Enable the VPN in NekoBox to route all device traffic through the proxy

## License

MIT


