# Split-pane example for bubble-ssh

<img src="./demo.gif" />

## Usage

From `examples/`:

```bash
go run ./split-pane -left user@host:port -right user@host:port
```

Example against [OverTheWire Bandit](https://overthewire.org/wargames/bandit/):

```bash
go run ./split-pane \
  -left bandit0@bandit.labs.overthewire.org:2220 \
  -right bandit0@bandit.labs.overthewire.org:2220
```
