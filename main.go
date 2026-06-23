// nostr-nsite-server serves and publishes nip-5A static sites (nsite).
//
//	nostr-nsite-server serve  -npub <npub> -relay <relay>...
//	nostr-nsite-server update -sec <nsec|hex|bunker> -server <blossom>... -relay <relay>... <directory>
package main

import (
	"context"
	"log"
	"os"

	"github.com/urfave/cli/v3"
)

func main() {
	app := &cli.Command{
		Name:  "nostr-nsite-server",
		Usage: "serve and publish nip-5A static sites (nsite)",
		Commands: []*cli.Command{
			serveCmd,
			gatewayCmd,
			updateCmd,
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
