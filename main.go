package main

import (
	"fmt"
	"log/slog"
	"os"
)

func main() {
	err := run()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}

	bgp, err := NewBGP(cfg.BGP)
	if err != nil {
		return err
	}

	docker, err := NewDocker(cfg.Interval)
	if err != nil {
		return err
	}

	for routes := range docker.RouteUps() {
		err = bgp.Update(routes)
		if err != nil {
			slog.Error(err.Error())
		}
	}

	return nil
}
