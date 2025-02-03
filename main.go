package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/W-Floyd/qbittorrent-docker-multiplexer/qbittorrent"
	"github.com/W-Floyd/qbittorrent-docker-multiplexer/state"
	"github.com/W-Floyd/qbittorrent-docker-multiplexer/util"
	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/motemen/go-loghttp"
	_ "github.com/motemen/go-loghttp/global"
	"github.com/motemen/go-nuts/roundtime"
	"github.com/omeid/uconfig"
	"gopkg.in/yaml.v3"
)

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	loghttp.DefaultLogRequest = func(req *http.Request) {
		if false ||
			strings.HasPrefix(req.URL.RequestURI(), "/api/v2/torrents/delete") ||
			strings.HasPrefix(req.URL.RequestURI(), "/api/v2/torrents/add") {
			log.Printf("--> %s %s", req.Method, req.URL)
			for name, values := range req.Header {
				// Loop over all values for the name.
				for _, value := range values {
					log.Println(name, value)
				}
			}
			fmt.Println("")
			for k, v := range req.Form {
				log.Println(k, v)
			}
			fmt.Println("")
		}
	}

	loghttp.DefaultLogResponse = func(resp *http.Response) {
		loc := resp.Request.URL
		if loc != nil {
			if false ||
				strings.HasPrefix(loc.RequestURI(), "/api/v2/torrents/delete") ||
				strings.HasPrefix(loc.RequestURI(), "/api/v2/torrents/add") {
				ctx := resp.Request.Context()
				if start, ok := ctx.Value(loghttp.ContextKeyRequestStart).(time.Time); ok {
					log.Printf("<-- %d %s (%s)", resp.StatusCode, resp.Request.URL, roundtime.Duration(time.Now().Sub(start), 2))
				} else {
					log.Printf("<-- %d %s", resp.StatusCode, resp.Request.URL)
				}
				for name, values := range resp.Header {
					// Loop over all values for the name.
					for _, value := range values {
						log.Println(name, value)
					}
				}
				fmt.Println("")
			}
		}
	}
}

func main() {

	log.Println("starting up")

	logger, _ := zap.NewProduction()
	defer logger.Sync() // Flush buffer

	conf := &Config{}

	files := uconfig.Files{
		{
			"config.json",
			json.Unmarshal,
			true,
		},
		{
			"config.yaml",
			yaml.Unmarshal,
			true,
		},
	}

	_, err := uconfig.Classic(&conf, files)

	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	errs := conf.Validate()
	if errs != nil {
		fmt.Println("Errors in config:")
		for _, err := range errs {
			fmt.Println(" -", err)
		}
		os.Exit(1)
	}

	// Logging

	for i := uint(0); i < state.AppState.NumberOfClients; i++ {
		fmt.Println("Instance at 127.0.0.1:" + util.UintToString(qbittorrent.Port(&conf.QBittorrent, &i)))
		fmt.Println("Username: " + qbittorrent.Username(&conf.QBittorrent, &i))
		fmt.Println("Password: " + qbittorrent.Password(&conf.QBittorrent, &i))
		fmt.Println("")
	}

	// Docker Compose

	// client.

	// Router

	r := mux.NewRouter()

	r.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		r.ParseForm()
		r.ParseMultipartForm(131072)

		if r.URL.Path == "/api/v2/torrents/info" {
			conf.HandlerFetchMergeArray(w, r)
		} else if slices.Contains([]string{
			"/api/v2/sync/maindata",
			"/api/v2/torrents/categories",
		}, r.URL.Path) {
			conf.HandlerFetchMerge(w, r)
		} else if r.Form.Has("hash") || r.Form.Has("hashes") {
			conf.HandlerHashFinder(w, r)
		} else {
			conf.HandlerPassthrough(w, r)
		}

	})

	// r.Use(zapchi.Logger(logger, "router"))

	srv := &http.Server{
		Addr:         conf.Multiplexer.Address + ":" + strconv.FormatUint(uint64(conf.Multiplexer.Port), 10),
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,
		Handler:      r,
	}

	fmt.Println(conf.Multiplexer.ShutdownTimeout)

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Println(err)
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	<-c

	ctx, cancel := context.WithTimeout(context.Background(), conf.Multiplexer.ShutdownTimeout)
	defer cancel()
	srv.Shutdown(ctx)
	log.Println("shutting down")

	// err = ShutdownDB()
	if err != nil {
		log.Fatal(err)
	}

	os.Exit(0)

}
