package main

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	docker "github.com/moby/moby/client"
	"github.com/samber/lo"
)

var watchEvents = map[events.Action]bool{
	events.ActionDie:                   true,
	events.ActionStart:                 true,
	events.ActionCreate:                true,
	events.ActionStop:                  true,
	events.ActionHealthStatus:          true,
	events.ActionHealthStatusHealthy:   true,
	events.ActionHealthStatusUnhealthy: true,
}

type Docker struct {
	interval time.Duration

	docker *docker.Client

	up     chan struct{}
	routes chan []Route
}

func NewDocker(interval time.Duration) (*Docker, error) {
	cli, err := docker.New(docker.FromEnv)
	if err != nil {
		return nil, err
	}

	d := &Docker{
		interval: interval,
		docker:   cli,
		up:       make(chan struct{}, 1),
		routes:   make(chan []Route),
	}

	d.watch()
	d.emit()
	d.timer()

	return d, nil
}

func (d *Docker) RouteUps() <-chan []Route {
	return d.routes
}

func (d *Docker) timer() {
	t := time.Tick(d.interval)
	go func() {
		for ; ; <-t {
			d.notify()
		}
	}()
}

func (d *Docker) watch() {
	slog.Info("starting docker watch")
	evs := d.docker.Events(context.Background(), docker.EventsListOptions{
		Since: fmt.Sprint(time.Now().Unix()),
	})
	go func() {
		defer func() {
			time.Sleep(5 * time.Second)
			d.watch()
		}()
		for {
			select {
			case err := <-evs.Err:
				slog.Error("docker watch", "err", err.Error())
				return

			case msg, ok := <-evs.Messages:
				if !ok {
					return
				}
				if msg.Type != "container" {
					continue
				}

				if !watchEvents[msg.Action] {
					continue
				}

				d.notify()
			}
		}
	}()
}

func (d *Docker) emit() {
	go func() {
		for range d.up {
			r, err := d.Routes(context.Background())
			if err != nil {
				slog.Error(err.Error())
			} else {
				d.routes <- r
			}
		}
	}()
}
func (d *Docker) notify() {
	select {
	case d.up <- struct{}{}:
	default:
	}
}

func (d *Docker) Routes(ctx context.Context) ([]Route, error) {
	res := []Route{}

	containers, err := d.docker.ContainerList(ctx, docker.ContainerListOptions{})
	if err != nil {
		return nil, fmt.Errorf("docker container list: %w", err)
	}

	for _, ctr := range containers.Items {
		cr, err := d.containerRoutes(ctx, ctr)
		if err != nil {
			slog.Error(err.Error(), "container_id", ctr.ID, "container_name", ctr.Names)
		}

		res = append(res, cr...)
	}

	return res, nil
}

func (d *Docker) containerRoutes(ctx context.Context, ctr container.Summary) ([]Route, error) {
	if ctr.State != container.StateRunning {
		return nil, nil
	}

	if ctr.Labels["bgpAdvertise"] != "true" {
		return nil, nil
	}

	if ctr.Health.Status != container.NoHealthcheck && ctr.Health.Status != container.Healthy {
		return nil, nil
	}

	var res []Route
	var err error

	communities := []uint32{}
	if v, ok := ctr.Labels["bgpCommunities"]; ok {
		communities, err = parseCommunities(strings.Split(v, ","))
		if err != nil {
			return nil, err
		}
	}
	asPrependN := 0
	if v, ok := ctr.Labels["bgpAsPrependN"]; ok {
		asPrependN, err = strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("bgpAsPrependN: %w", err)
		}
	}
	networks := []string{}
	if v, ok := ctr.Labels["bgpNetworks"]; ok {
		networks = strings.Split(v, ",")
	}

	inspect, err := d.docker.ContainerInspect(ctx, ctr.ID, docker.ContainerInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("docker container %s inspect: %w", ctr.ID, err)
	}

	for netName, net := range inspect.Container.NetworkSettings.Networks {
		if len(networks) > 0 && !slices.Contains(networks, netName) {
			continue
		}

		if net.IPAddress.IsValid() {
			res = append(res, Route{
				Dest:        lo.Must(net.IPAddress.Prefix(net.IPAddress.BitLen())),
				Communities: communities,
				ASPrependN:  asPrependN,
			})
		}
		if net.GlobalIPv6Address.IsValid() {
			res = append(res, Route{
				Dest:        lo.Must(net.GlobalIPv6Address.Prefix(net.GlobalIPv6Address.BitLen())),
				Communities: communities,
				ASPrependN:  asPrependN,
			})
		}
	}

	return res, nil
}
