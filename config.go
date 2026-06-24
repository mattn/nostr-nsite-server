package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
)

// config is the on-disk gateway configuration.
//
// Mappings give friendly subdomain labels to npubs, so a site can be reached at
// e.g. mattn.nsite.example.com instead of <npub>.nsite.example.com.
//
//	{
//	  "mappings": {
//	    "mattn": "npub1..."
//	  }
//	}
type config struct {
	Mappings map[string]string `json:"mappings"`
}

// loadConfig reads and parses the config file at path, resolving every mapping
// value to a pubkey so invalid npubs fail loudly at startup.
func loadConfig(path string) (map[string]nostr.PubKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	mappings := make(map[string]nostr.PubKey, len(cfg.Mappings))
	for label, npub := range cfg.Mappings {
		prefix, value, err := nip19.Decode(npub)
		if err != nil || prefix != "npub" {
			return nil, fmt.Errorf("mapping %q: invalid npub %q", label, npub)
		}
		mappings[strings.ToLower(label)] = value.(nostr.PubKey)
	}
	return mappings, nil
}
