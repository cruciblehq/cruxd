# cruxd

[![Build](https://github.com/cruciblehq/cruxd/actions/workflows/build.yml/badge.svg)](https://github.com/cruciblehq/cruxd/actions/workflows/build.yml)
[![Coverage](https://img.shields.io/endpoint?url=https://cruciblehq.github.io/cruxd/badges/coverage.json)](https://cruciblehq.github.io/cruxd/coverage/coverage.html)

The crux daemon.

## Installation

### Homebrew

```bash
brew install cruciblehq/tap/cruxd
```

### GitHub Releases

Download the latest release for your platform from
[GitHub Releases](https://github.com/cruciblehq/cruxd/releases):

```bash
# Linux (amd64)
curl -fsSL https://github.com/cruciblehq/cruxd/releases/latest/download/cruxd-linux-amd64.tar.gz | tar xz
sudo mv cruxd /usr/local/bin/

# Linux (arm64)
curl -fsSL https://github.com/cruciblehq/cruxd/releases/latest/download/cruxd-linux-arm64.tar.gz | tar xz
sudo mv cruxd /usr/local/bin/
```
