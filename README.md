# Project ssh2sock5

## Installation and Setup

### Install dependencies step-by-step

#### All dependencies for run project

```
nix
direnv - https://direnv.net/docs/hook.html
make
```

#### For Linux users

```
curl --proto '=https' --tlsv1.2 -sSf -L https://install.determinate.systems/nix | sh -s -- install
```
and press Y
after install reopen shell


- for auto activate shell
```
cd ssh2sock5
direnv allow
```
or
```
cd ssh2sock5
nix develop
```
or
```
nix shell
```

### build apk

```
make build-android
```

### run in console

```
make run
```
