package main

import (
	"github.com/W-Floyd/qbittorrent-docker-multiplexer/docker"
	"github.com/W-Floyd/qbittorrent-docker-multiplexer/multiplexer"
	"github.com/W-Floyd/qbittorrent-docker-multiplexer/qbittorrent"
)

type Config struct {
	Multiplexer multiplexer.Config
	QBittorrent qbittorrent.Config
	Docker      docker.Config
}

func (c Config) Validate() (errs []error) {
	errs = append(errs, c.Multiplexer.Validate()...)
	errs = append(errs, c.QBittorrent.Validate()...)
	errs = append(errs, c.Docker.Validate()...)

	return errs
}
