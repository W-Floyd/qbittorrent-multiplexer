# qBittorrent Multiplexer <!-- omit from toc -->
## Combine multiple qBittorrent instances <!-- omit from toc -->

- [Introduction](#introduction)
  - [Features](#features)
    - [To Do](#to-do)
- [Installation](#installation)
  - [Native](#native)
  - [Docker](#docker)
- [Configuration](#configuration)

# Introduction

I run multiple instances of qBittorrent for performance reasons, but find it annoying to keep an eye on them.
This is a simple application to combine multiple instances into the same frontend.
It authenticates with the API for each instance and intelegantly passes requests through and/or combines them to allow a single view.

## Features

- Torrent lists and details
- Adding torrents to least busy instance
- Summed statistics
  - Total transfer rates
  - All time uploaded/downloaded
  - Session uploaded/downloaded
- Exclude keys from torrent lists to improve client performance (e.g. remove magnets)

### To Do

Everything else! Notably:

- Deleting torrents
  - Need to parse and split out `hashes` field to all relevant instances
- Preferences
  - May not bother, may try to inject an option into the settings page to select the instance to modify at any given time
- Client authentication
  - Backend is done of course, you can use a reverse proxy instead to require authentication if needed

# Installation

## Native
It's a Go program, clone and install as usual.

For example:

```sh
git clone http://github.com/W-Floyd/qbittorrent-multiplexer && cd qbittorrent-multiplexer
go get
go build .
./qbittorrent-multiplexer 
```

## Docker

See the included `docker-compose.yaml` file and adjoining files (`vpn` and `qbittorrent`) for an example case

# Configuration

Configuration can be done environment variables and flags (in that order).
Please see example configs and docker compose files for an idea.
The Go code is pretty easy to read, check `config.go`, and the tops of `multiplexer.go` and `qbittorrent.go` for more details.