package main

import (
	"log/slog"
	"net/netip"

	"github.com/samber/lo"
)

var _ slog.LogValuer = Route{}

type Route struct {
	Dest        netip.Prefix
	Communities []uint32
	ASPrependN  int
}

// LogValue implements slog.LogValuer.
func (r Route) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("prefix", r.Dest.String()),
		slog.Any("communities", lo.Map(r.Communities, func(c uint32, _ int) string {
			return communityToString(c)
		})),
		slog.Int("as_prepend_n", r.ASPrependN),
	)
}
