package main

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/sethvargo/go-envconfig"
)

type Config struct {
	Interval time.Duration `env:"BGPVIP_INTERVAL, required"`
	BGP      BGPConfig     `env:", prefix=BGPVIP_BGP_"`
}

type BGPConfig struct {
	ASN       uint32     `env:"ASN, required"`
	RouterID  string     `env:"ROUTER_ID, required"`
	PeerASN   uint32     `env:"PEER_ASN, required"`
	Peers     []string   `env:"PEERS, required"`
	NexthopV4 netip.Addr `env:"NEXTHOP_V4"`
	NexthopV6 netip.Addr `env:"NEXTHOP_V6"`
}

func LoadConfig() (Config, error) {
	ctx := context.Background()

	var c Config
	if err := envconfig.Process(ctx, &c); err != nil {
		return c, fmt.Errorf("config: %w", err)
	}

	return c, nil
}
