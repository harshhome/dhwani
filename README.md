# Dhwani

Dhwani is a self-hosted Subsonic/OpenSubsonic-compatible music proxy server written in Go.

## Name Origin

**Dhwani** (ध्वनि) is a Sanskrit/Hindi word commonly used for **sound**, **tone**, or **resonance**.

## What It Does

- Exposes Subsonic/OpenSubsonic-compatible REST endpoints
- Supports Subsonic auth styles (`u+p` and `u+t+s`)
- Proxies catalog lookup and audio streaming for compatible clients
- Stores starred metadata in SQLite
- Fetches lyrics from upstream providers
- Can download tracks/albums when they are starred

## Requirements

- Go `1.23+` (for local build/run)
- Docker (optional)
- `ffmpeg` (optional, used for media tagging during download)

## Installation

### Option 1: Build from source

```bash
git clone <your-repo-url>
cd dhwani/src
go mod download
go build -o dhwani ./cmd/dhwani
```

### Option 2: Docker

```bash
git clone <your-repo-url>
cd dhwani
docker build -f deploy/docker/Dockerfile -t dhwani:latest .
```

## Configuration

Minimum required environment variables:

```bash
DHWANI_USERNAME=dhwani
DHWANI_PASSWORD=replace-this
DHWANI_INSTANCES_URL=https://your-instances-endpoint.example/instances.json
# or: DHWANI_INSTANCES_FILE=/absolute/path/to/instances.json
```

Optional download-on-star configuration:

```bash
DHWANI_DOWNLOAD_ON_STAR=true
DHWANI_DOWNLOAD_DIR=/absolute/path/to/downloads
DHWANI_DOWNLOAD_QUALITY=HI_RES_LOSSLESS,LOSSLESS
DHWANI_DOWNLOAD_RETRY_ATTEMPTS=3
```

## Run

```bash
cd src
go run ./cmd/dhwani
```

Default address: `http://0.0.0.0:8080`

## Basic Usage

```bash
BASE='http://localhost:8080'
U='dhwani'
P='replace-this'

curl "$BASE/healthz"
curl "$BASE/rest/ping.view?u=$U&p=$P&v=1.16.1&c=curl"
curl "$BASE/rest/search3.view?u=$U&p=$P&v=1.16.1&c=curl&query=artist&songCount=5"
curl -L "$BASE/rest/stream.view?u=$U&p=$P&v=1.16.1&c=curl&id=<track-id>" -o sample.audio
```

## Starring, Lyrics, and Downloads

- `star`/`unstar` endpoints persist star state to local SQLite
- `getLyrics` and `getLyricsBySongId` resolve lyrics from upstream providers
- When download-on-star is enabled, starring a song/album queues background download work
