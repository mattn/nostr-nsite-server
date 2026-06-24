package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
	"fiatjaf.com/nostr/nip19"
	"fiatjaf.com/nostr/nipb0/blossom"
	"github.com/urfave/cli/v3"
)

// staticFiles holds the default site served at the bare base domain (usage page, etc.).
//
//go:embed static
var staticFiles embed.FS

// staticHandler serves the embedded static/ directory (index.html at "/").
var staticHandler = func() http.Handler {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}()

var gatewayCmd = &cli.Command{
	Name:  "gateway",
	Usage: "serve any nsite addressed by <npub>.<base-domain> (multi-tenant)",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "base-domain",
			Usage:    "domain suffix under which npub subdomains are served, e.g. nsite.compile-error.net",
			Required: true,
		},
		&cli.StringSliceFlag{
			Name:  "relay",
			Usage: "relays to fetch manifests from, can be given multiple times",
			Value: []string{"wss://relay.damus.io", "wss://nos.lol"},
		},
		&cli.StringFlag{
			Name:  "addr",
			Usage: "address to listen on",
			Value: ":8080",
		},
		&cli.StringFlag{
			Name:  "config",
			Usage: "path to a JSON config mapping subdomain labels to npubs",
		},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		relays := c.StringSlice("relay")
		if len(relays) == 0 {
			return fmt.Errorf("no relays given")
		}

		var mappings map[string]nostr.PubKey
		if path := c.String("config"); path != "" {
			var err error
			if mappings, err = loadConfig(path); err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			log.Printf("loaded %d subdomain mapping(s) from %s", len(mappings), path)
		}

		gw := &gateway{
			baseDomain: strings.ToLower(strings.TrimPrefix(c.String("base-domain"), ".")),
			relays:     relays,
			pool:       nostr.NewPool(),
			sites:      map[nostr.PubKey]*server{},
			mappings:   mappings,
		}

		addr := c.String("addr")
		log.Printf("serving nsite gateway for *.%s on http://localhost%s", gw.baseDomain, addr)
		return http.ListenAndServe(addr, gw)
	},
}

// gateway dispatches requests to a per-npub server based on the Host header.
// The shared relay pool is reused across all tenants; each tenant gets its own
// manifest cache and read-only signer.
type gateway struct {
	baseDomain string
	relays     []string
	pool       *nostr.Pool

	// mappings resolves friendly subdomain labels (e.g. "mattn") to a pubkey,
	// loaded from the config file. nil when no config was given.
	mappings map[string]nostr.PubKey

	mu    sync.Mutex
	sites map[nostr.PubKey]*server
}

func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	host = strings.ToLower(host)

	// the bare base domain serves the embedded default site (usage page).
	if host == g.baseDomain {
		staticHandler.ServeHTTP(w, r)
		return
	}

	// strip the base domain to isolate the leading <npub> label.
	label, ok := strings.CutSuffix(host, "."+g.baseDomain)
	if !ok || label == "" || strings.ContainsRune(label, '.') {
		http.Error(w, "unknown host", http.StatusNotFound)
		return
	}

	// a configured mapping (e.g. "mattn") takes precedence; otherwise the label
	// itself must be a valid npub.
	pk, ok := g.mappings[label]
	if !ok {
		prefix, value, err := nip19.Decode(label)
		if err != nil || prefix != "npub" {
			http.Error(w, "host is not a known name or valid npub", http.StatusNotFound)
			return
		}
		pk = value.(nostr.PubKey)
	}

	g.siteFor(pk).ServeHTTP(w, r)
}

// siteFor returns a cached per-npub server, creating one on first request.
func (g *gateway) siteFor(pk nostr.PubKey) *server {
	g.mu.Lock()
	defer g.mu.Unlock()
	if s, ok := g.sites[pk]; ok {
		return s
	}
	s := &server{
		pubkey:    pk,
		relays:    g.relays,
		pool:      g.pool,
		signer:    keyer.NewReadOnlySigner(pk),
		blossomCl: map[string]*blossom.Client{},
	}
	g.sites[pk] = s
	return s
}
