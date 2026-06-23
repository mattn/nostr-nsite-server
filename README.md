# nostr-nsite-server

Serve and publish [NIP-5A](https://github.com/nostr-protocol/nips/blob/master/5A.md) static sites (nsite) over HTTP.

An nsite is a static website published on Nostr: files are stored on [blossom](https://github.com/hzrd149/blossom) servers and a signed manifest event (kind `15128`) maps paths to blob hashes. This tool fetches that manifest and the referenced blobs, and serves the site over plain HTTP — either a single site, or any user's site addressed by their npub subdomain.

## Usage

### Serve a single site

```
$ nostr-nsite-server serve \
    -npub npub1xxxxxxxxxxxx... \
    -relay wss://relay.damus.io -relay wss://nos.lol \
    -addr :8080
```

Fetches the manifest for the given npub and serves it on `http://localhost:8080`.

### Serve as a multi-tenant gateway

```
$ nostr-nsite-server gateway \
    -base-domain nsite.example.com \
    -relay wss://relay.damus.io -relay wss://nos.lol \
    -addr :8080
```

Serves any user's site addressed by `<npub>.nsite.example.com`. The npub is taken
from the Host header, decoded, and its manifest is fetched and cached per tenant.
Requests to the bare base domain return a built-in landing page (embedded from
`static/`). Point a wildcard DNS record `*.nsite.example.com` and a wildcard TLS
certificate at the server.

### Publish a site

```
$ nostr-nsite-server update \
    -sec nsec1xxxxxxxxxxxx... \
    -server https://blossom.band \
    -relay wss://relay.damus.io \
    ./my-site
```

Uploads every file under `./my-site` to the given blossom server(s) and publishes
(or replaces) the nsite manifest (root, kind `15128`) to the given relay(s).
`-sec` accepts an `nsec`, hex key, `ncryptsec`, or a NIP-46 `bunker://` URL.

## Installation

```
go install github.com/mattn/nostr-nsite-server@latest
```

Or grab a prebuilt binary for Linux / macOS / Windows from the
[releases](https://github.com/mattn/nostr-nsite-server/releases) page.

Or with Docker:

```
docker pull ghcr.io/mattn/nostr-nsite-server:latest
```

## License

MIT

## Author

Yasuhiro Matsumoto (a.k.a. mattn)
