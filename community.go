package main

import (
	"fmt"
	"strconv"
	"strings"
)

func parseCommunity(in string) (uint32, error) {
	p1, p2, ok := strings.Cut(in, ":")
	if !ok {
		return 0, fmt.Errorf("bad format for community %q, expected 123:456", in)
	}

	p1i, err := strconv.ParseInt(p1, 10, 16)
	if err != nil {
		return 0, fmt.Errorf("parse community %q: %w", in, err)
	}
	p2i, err := strconv.ParseInt(p2, 10, 16)
	if err != nil {
		return 0, fmt.Errorf("parse community %q: %w", in, err)
	}

	return (uint32(p1i) << 16) | uint32(p2i), nil
}

func parseCommunities(in []string) ([]uint32, error) {
	res := []uint32{}

	for _, i := range in {
		c, err := parseCommunity(i)
		if err != nil {
			return nil, err
		}

		res = append(res, c)
	}

	return res, nil
}

func communityToString(in uint32) string {
	return fmt.Sprintf("%d:%d", in>>16, in&0xFFFF)
}
