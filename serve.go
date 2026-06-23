package main

import (
	"context"
	"fmt"
	"log"
	"mime"
	"net/http"
	"path"
	"sync"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
	"fiatjaf.com/nostr/nip19"
	"fiatjaf.com/nostr/nip5a"
	"fiatjaf.com/nostr/nipb0/blossom"
	"github.com/urfave/cli/v3"
)

// manifestTTL is how long a fetched manifest is reused before refetching.
const manifestTTL = 30 * time.Second

var serveCmd = &cli.Command{
	Name:  "serve",
	Usage: "serve a single nsite over HTTP",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "npub",
			Usage:    "npub of the site to serve",
			Required: true,
		},
		&cli.StringSliceFlag{
			Name:  "relay",
			Usage: "relays to fetch the manifest from, can be given multiple times",
			Value: []string{"wss://relay.damus.io", "wss://nos.lol"},
		},
		&cli.StringFlag{
			Name:  "addr",
			Usage: "address to listen on",
			Value: ":8080",
		},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		prefix, value, err := nip19.Decode(c.String("npub"))
		if err != nil || prefix != "npub" {
			return fmt.Errorf("invalid npub %q: %w", c.String("npub"), err)
		}
		pk := value.(nostr.PubKey)

		relays := c.StringSlice("relay")
		if len(relays) == 0 {
			return fmt.Errorf("no relays given")
		}

		srv := &server{
			pubkey:    pk,
			relays:    relays,
			pool:      nostr.NewPool(),
			signer:    keyer.NewReadOnlySigner(pk),
			blossomCl: map[string]*blossom.Client{},
		}

		// warm up the manifest once so we fail loudly if the site is unreachable.
		if _, err := srv.getManifest(ctx); err != nil {
			return fmt.Errorf("failed to fetch initial manifest for %s: %w", c.String("npub"), err)
		}

		addr := c.String("addr")
		log.Printf("serving nsite %s on http://localhost%s", c.String("npub"), addr)
		return http.ListenAndServe(addr, srv)
	},
}

type server struct {
	pubkey nostr.PubKey
	relays []string
	pool   *nostr.Pool
	signer nostr.Signer

	mu        sync.Mutex
	manifest  *nip5a.SiteManifest
	fetchedAt time.Time
	blossomCl map[string]*blossom.Client
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	mnf, err := s.getManifest(r.Context())
	if err != nil {
		http.Error(w, "failed to load site manifest: "+err.Error(), http.StatusBadGateway)
		return
	}

	reqPath := nip5a.NormalizePath(r.URL.Path)
	hash, ok := mnf.GetHashForPath(reqPath)
	if !ok {
		// common SPA fallback: serve the site index for unknown paths.
		if hash, ok = mnf.GetHashForPath("/index.html"); !ok {
			http.NotFound(w, r)
			return
		}
		reqPath = "/index.html"
	}

	data, err := s.download(r.Context(), mnf, hash)
	if err != nil {
		http.Error(w, "failed to fetch blob: "+err.Error(), http.StatusBadGateway)
		return
	}

	if ct := contentType(reqPath, data); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		w.Write(data)
	}
}

// getManifest returns a cached manifest, refetching when it is older than manifestTTL.
func (s *server) getManifest(ctx context.Context) (*nip5a.SiteManifest, error) {
	s.mu.Lock()
	if s.manifest != nil && time.Since(s.fetchedAt) < manifestTTL {
		mnf := s.manifest
		s.mu.Unlock()
		return mnf, nil
	}
	s.mu.Unlock()

	filter := nostr.Filter{
		Authors: []nostr.PubKey{s.pubkey},
		Kinds:   []nostr.Kind{nostr.KindNsiteRoot},
		Limit:   1,
	}
	res := s.pool.QuerySingle(ctx, s.relays, filter, nostr.SubscriptionOptions{Label: "nsite-server"})
	if res == nil {
		return nil, fmt.Errorf("no nsite manifest found on the given relays")
	}
	mnf, err := nip5a.ParseSiteManifest(&res.Event)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.manifest = mnf
	s.fetchedAt = time.Now()
	s.mu.Unlock()
	return mnf, nil
}

// download fetches a blob by hash, trying each blossom server advertised by the site.
func (s *server) download(ctx context.Context, mnf *nip5a.SiteManifest, hash [32]byte) ([]byte, error) {
	if len(mnf.Servers) == 0 {
		return nil, fmt.Errorf("manifest advertises no blossom servers")
	}

	var lastErr error
	for _, srv := range mnf.Servers {
		s.mu.Lock()
		cl, ok := s.blossomCl[srv]
		if !ok {
			cl = blossom.NewClient(srv, s.signer)
			s.blossomCl[srv] = cl
		}
		s.mu.Unlock()

		data, err := cl.Download(ctx, hash)
		if err != nil {
			lastErr = err
			continue
		}
		return data, nil
	}
	return nil, lastErr
}

func contentType(p string, data []byte) string {
	if ct := mime.TypeByExtension(path.Ext(p)); ct != "" {
		return ct
	}
	return http.DetectContentType(data)
}
