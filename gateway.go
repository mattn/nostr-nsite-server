package main

import (
	"context"
	"fmt"
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
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		relays := c.StringSlice("relay")
		if len(relays) == 0 {
			return fmt.Errorf("no relays given")
		}

		gw := &gateway{
			baseDomain: strings.ToLower(strings.TrimPrefix(c.String("base-domain"), ".")),
			relays:     relays,
			pool:       nostr.NewPool(),
			sites:      map[nostr.PubKey]*server{},
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

	mu    sync.Mutex
	sites map[nostr.PubKey]*server
}

func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	host = strings.ToLower(host)

	// strip the base domain to isolate the leading <npub> label.
	label, ok := strings.CutSuffix(host, "."+g.baseDomain)
	if !ok || label == "" || strings.ContainsRune(label, '.') {
		http.Error(w, "unknown host", http.StatusNotFound)
		return
	}

	prefix, value, err := nip19.Decode(label)
	if err != nil || prefix != "npub" {
		http.Error(w, "host is not a valid npub", http.StatusNotFound)
		return
	}
	pk := value.(nostr.PubKey)

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
