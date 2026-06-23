package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
	"fiatjaf.com/nostr/nip19"
	"fiatjaf.com/nostr/nip5a"
	"fiatjaf.com/nostr/nipb0/blossom"
	"github.com/urfave/cli/v3"
)

var updateCmd = &cli.Command{
	Name:      "update",
	Usage:     "upload a directory to blossom and (re)publish the nsite manifest (root, kind 15128)",
	ArgsUsage: "<directory>",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "sec",
			Usage:    "secret key to sign with (nsec, hex, ncryptsec or bunker URL)",
			Required: true,
		},
		&cli.StringSliceFlag{
			Name:     "server",
			Aliases:  []string{"s"},
			Usage:    "blossom server to upload blobs to, can be given multiple times",
			Required: true,
		},
		&cli.StringSliceFlag{
			Name:     "relay",
			Aliases:  []string{"r"},
			Usage:    "relay to publish the manifest to, can be given multiple times",
			Required: true,
		},
		&cli.StringFlag{
			Name:  "title",
			Usage: "human-readable title for the site",
		},
		&cli.StringFlag{
			Name:  "description",
			Usage: "human-readable description for the site",
		},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		dir := c.Args().First()
		if dir == "" {
			return fmt.Errorf("missing directory")
		}
		if st, err := os.Stat(dir); err != nil {
			return fmt.Errorf("failed to stat %s: %w", dir, err)
		} else if !st.IsDir() {
			return fmt.Errorf("%s is not a directory", dir)
		}

		pool := nostr.NewPool()
		kr, err := keyer.New(ctx, pool, c.String("sec"), nil)
		if err != nil {
			return fmt.Errorf("invalid secret key: %w", err)
		}
		pk, err := kr.GetPublicKey(ctx)
		if err != nil {
			return fmt.Errorf("failed to get public key: %w", err)
		}

		servers := c.StringSlice("server")
		manifest := nip5a.SiteManifest{
			Pubkey:      pk,
			Root:        true,
			Paths:       make(map[string][32]byte),
			Servers:     servers,
			Title:       c.String("title"),
			Description: c.String("description"),
		}

		// upload every regular file under dir to each blossom server.
		err = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !d.Type().IsRegular() {
				return nil
			}

			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}

			var hhash string
			for _, server := range servers {
				bd, err := blossom.NewClient(server, kr).UploadFilePath(ctx, p)
				if err != nil {
					return fmt.Errorf("failed to upload %s to %s: %w", p, server, err)
				}
				hhash = bd.SHA256
				log.Printf("uploaded /%s to %s as %s", filepath.ToSlash(rel), server, hhash)
			}

			var hash [32]byte
			if _, err := hex.Decode(hash[:], []byte(hhash)); err != nil {
				return fmt.Errorf("invalid blob hash %q: %w", hhash, err)
			}
			manifest.Paths["/"+filepath.ToSlash(rel)] = hash
			return nil
		})
		if err != nil {
			return err
		}

		if len(manifest.Paths) == 0 {
			return fmt.Errorf("no files found under %s", dir)
		}

		evt := manifest.ToEvent()
		if err := kr.SignEvent(ctx, &evt); err != nil {
			return fmt.Errorf("failed to sign manifest: %w", err)
		}

		// publish the manifest to every relay.
		published := 0
		for _, url := range c.StringSlice("relay") {
			relay, err := nostr.RelayConnect(ctx, url, nostr.RelayOptions{})
			if err != nil {
				log.Printf("failed to connect to %s: %v", url, err)
				continue
			}
			if err := relay.Publish(ctx, evt); err != nil {
				log.Printf("failed to publish to %s: %v", url, err)
				continue
			}
			log.Printf("published manifest to %s", url)
			published++
		}
		if published == 0 {
			return fmt.Errorf("failed to publish manifest to any relay")
		}

		log.Printf("done: %d files, published to %d relay(s)", len(manifest.Paths), published)
		log.Printf("serve it with: nostr-nsite-server serve -npub %s -relay %s",
			nip19.EncodeNpub(pk), c.StringSlice("relay")[0])
		return nil
	},
}
