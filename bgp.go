package main

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/osrg/gobgp/v4/api"
	"github.com/osrg/gobgp/v4/pkg/apiutil"
	"github.com/osrg/gobgp/v4/pkg/packet/bgp"
	"github.com/osrg/gobgp/v4/pkg/server"
	"github.com/samber/lo"
)

type BGP struct {
	cfg BGPConfig
	bgp *server.BgpServer
}

func NewBGP(cfg BGPConfig) (*BGP, error) {
	log := slog.Default()
	lvl := &slog.LevelVar{}
	lvl.Set(slog.LevelInfo)

	bgp := server.NewBgpServer(server.LoggerOption(log, lvl))
	go bgp.Serve()

	// global configuration
	if err := bgp.StartBgp(context.Background(), &api.StartBgpRequest{
		Global: &api.Global{
			Asn:        cfg.ASN,
			RouterId:   cfg.RouterID,
			ListenPort: 179,
		},
	}); err != nil {
		return nil, fmt.Errorf("bgp start: %w", err)
	}

	// set default policy
	lo.Must0(bgp.SetPolicyAssignment(context.Background(), &api.SetPolicyAssignmentRequest{
		Assignment: &api.PolicyAssignment{
			Direction:     api.PolicyDirection_POLICY_DIRECTION_IMPORT,
			DefaultAction: api.RouteAction_ROUTE_ACTION_ACCEPT,
		},
	}))

	lo.Must0(bgp.SetPolicyAssignment(context.Background(), &api.SetPolicyAssignmentRequest{
		Assignment: &api.PolicyAssignment{
			Direction:     api.PolicyDirection_POLICY_DIRECTION_EXPORT,
			DefaultAction: api.RouteAction_ROUTE_ACTION_ACCEPT,
		},
	}))

	// neighbors
	for _, peer := range cfg.Peers {
		if err := bgp.AddPeer(context.Background(), &api.AddPeerRequest{
			Peer: &api.Peer{
				Conf: &api.PeerConf{
					NeighborAddress: peer,
					PeerAsn:         cfg.PeerASN,
				},
			},
		}); err != nil {
			return nil, fmt.Errorf("bgp add peer: %w", err)
		}
	}

	return &BGP{
		bgp: bgp,
		cfg: cfg,
	}, nil
}

func (b *BGP) Update(wanted []Route) error {
	v4, err := b.bgpFamilyPaths(bgp.RF_IPv4_UC)
	if err != nil {
		return err
	}

	v6, err := b.bgpFamilyPaths(bgp.RF_IPv6_UC)
	if err != nil {
		return err
	}

	current := append(v4, v6...)

	toDel := []*apiutil.Path{}
NextPath:
	for _, path := range current {
		nlri, ok := path.Nlri.(*bgp.IPAddrPrefix)
		if !ok {
			return fmt.Errorf("path nlri type %T not handled", path.Nlri)
		}

		communities := []uint32{}
		asPath := []uint32{}
		for _, attr := range path.Attrs {
			switch attr := attr.(type) {
			case *bgp.PathAttributeCommunities:
				communities = attr.Value
			case *bgp.PathAttributeAsPath:
				asPath = lo.FlatMap(attr.Value, func(path bgp.AsPathParamInterface, _ int) []uint32 {
					return path.GetAS()
				})
			}
		}

		for i, route := range wanted {
			routeASPath := slices.Repeat([]uint32{b.cfg.ASN}, route.ASPrependN)
			if route.Dest != nlri.Prefix || !slices.Equal(route.Communities, communities) || !slices.Equal(routeASPath, asPath) {
				continue
			}

			wanted = slices.Delete(wanted, i, i+1)
			continue NextPath
		}

		slog.Warn("withdrawing", "route", nlri.IPAddrPrefixDefault.Prefix)
		toDel = append(toDel, path)
	}

	if len(toDel) > 0 {
		err = b.bgp.DeletePath(apiutil.DeletePathRequest{
			Paths: toDel,
		})
		if err != nil {
			return fmt.Errorf("del paths: %w", err)
		}
	}

	toAdd := []*apiutil.Path{}
	for _, route := range wanted {
		nlri, _ := bgp.NewIPAddrPrefix(route.Dest)

		attrs := []bgp.PathAttributeInterface{
			bgp.NewPathAttributeCommunities(route.Communities),
			bgp.NewPathAttributeOrigin(0),
			bgp.NewPathAttributeAsPath([]bgp.AsPathParamInterface{bgp.NewAs4PathParam(2, slices.Repeat([]uint32{b.cfg.ASN}, route.ASPrependN))}),
		}

		if route.Dest.Addr().Is6() {
			if !b.cfg.NexthopV6.IsValid() {
				slog.Error("no BGPVIP_BGP_NEXTHOP_V6 configured")
				continue
			}
			attrs = append(attrs,
				lo.Must(bgp.NewPathAttributeNextHop(b.cfg.NexthopV6)),
				lo.Must(bgp.NewPathAttributeMpReachNLRI(bgp.RF_IPv6_UC, []bgp.PathNLRI{{NLRI: nlri}}, b.cfg.NexthopV6)),
			)
		} else {
			if !b.cfg.NexthopV4.IsValid() {
				slog.Error("no BGPVIP_BGP_NEXTHOP_V4 configured")
				continue
			}
			attrs = append(attrs,
				lo.Must(bgp.NewPathAttributeNextHop(b.cfg.NexthopV4)),
			)
		}

		slog.Info("advertising", "route", route)
		toAdd = append(toAdd, &apiutil.Path{
			Family: lo.Ternary(route.Dest.Addr().Is4(), bgp.RF_IPv4_UC, bgp.RF_IPv6_UC),
			Nlri:   nlri,
			Attrs:  attrs,
		})
	}

	if len(toAdd) > 0 {
		_, err = b.bgp.AddPath(apiutil.AddPathRequest{
			Paths: toAdd,
		})
		if err != nil {
			return fmt.Errorf("add paths: %w", err)
		}
	}

	return nil
}

func (b *BGP) bgpFamilyPaths(family bgp.Family) ([]*apiutil.Path, error) {
	paths := []*apiutil.Path{}
	err := b.bgp.ListPath(apiutil.ListPathRequest{
		TableType: api.TableType_TABLE_TYPE_GLOBAL,
		Family:    family,
	}, func(prefix bgp.NLRI, p []*apiutil.Path) {
		paths = append(paths, p...)
	})
	if err != nil {
		return nil, fmt.Errorf("bgp list paths: %w", err)
	}

	return paths, nil
}
