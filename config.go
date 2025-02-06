package main

import (
	"log"
	"net/http"
	"net/url"
	"sync"

	"github.com/W-Floyd/qbittorrent-multiplexer/multiplexer"
	"github.com/W-Floyd/qbittorrent-multiplexer/qbittorrent"
)

type Config struct {
	Multiplexer multiplexer.Config
	QBittorrent qbittorrent.Configs
}

func (c Config) Validate() (errs []error) {

	g := sync.WaitGroup{}

	errsChan := make(chan error)

	g.Add(2)
	go func() {
		defer g.Done()
		for _, err := range c.Multiplexer.Validate() {
			errsChan <- err
		}
	}()
	go func() {
		defer g.Done()
		for _, err := range c.QBittorrent.Validate() {
			errsChan <- err
		}
	}()

	go func() {
		g.Wait()
		close(errsChan)
	}()

	for err := range errsChan {
		errs = append(errs, err)
	}

	return errs
}

func (c Config) Prime() (errs []error) {

	r := &http.Request{
		URL: &url.URL{
			Path: "/api/v2/torrents/info",
		},
	}

	resps := c.ParallelResponses(r, RequestOptions{
		Callback: &RequestCallbackTorrentInfoAdd,
	})

	for _, resp := range resps {
		errs = append(errs, resp.errs...)
	}

	log.Println("Torrent list primed")

	return errs
}
