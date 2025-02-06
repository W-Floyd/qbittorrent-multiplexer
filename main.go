package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"go.uber.org/zap"

	// _ "github.com/motemen/go-loghttp/global"
	"github.com/omeid/uconfig"
	"gopkg.in/yaml.v3"
)

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// loghttp.DefaultLogRequest = func(req *http.Request) {
	// 	if !strings.HasPrefix(req.URL.RequestURI(), "/api/v2/sync/maindata") {
	// 		if false ||
	// 			strings.HasPrefix(req.URL.RequestURI(), "/api/v2/torrents/delete") ||
	// 			strings.HasPrefix(req.URL.RequestURI(), "/api/v2/torrents/add") {
	// 			log.Printf("--> %s %s", req.Method, req.URL)
	// 			for name, values := range req.Header {
	// 				// Loop over all values for the name.
	// 				for _, value := range values {
	// 					log.Println(name, value)
	// 				}
	// 			}
	// 			fmt.Println("")
	// 			for k, v := range req.Form {
	// 				log.Println(k, v)
	// 			}
	// 			fmt.Println("")
	// 		}
	// 	}
	// }

	// loghttp.DefaultLogResponse = func(resp *http.Response) {
	// 	if !strings.HasPrefix(resp.Request.URL.RequestURI(), "/api/v2/sync/maindata") {
	// 		ctx := resp.Request.Context()
	// 		if start, ok := ctx.Value(loghttp.ContextKeyRequestStart).(time.Time); ok {
	// 			log.Printf("<-- %d %s (%s)", resp.StatusCode, resp.Request.URL, roundtime.Duration(time.Now().Sub(start), 2))
	// 		} else {
	// 			log.Printf("<-- %d %s", resp.StatusCode, resp.Request.URL)
	// 		}
	// 		for name, values := range resp.Header {
	// 			// Loop over all values for the name.
	// 			for _, value := range values {
	// 				log.Println(name, value)
	// 			}
	// 		}
	// 		fmt.Println("")
	// 	}
	// }

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

	// Router

	r := mux.NewRouter()

	r.PathPrefix("/").HandlerFunc(conf.HandleAll)

	// r.Use(zapchi.Logger(logger, "router"))

	errs := conf.Validate()
	if errs != nil {
		fmt.Println("Errors in config:")
		for _, err := range errs {
			fmt.Println(" -", err)
		}
		os.Exit(1)
	}

	errs = conf.Prime()
	if errs != nil {
		log.Println(errs)
	}

	srv := &http.Server{
		Addr:         conf.Multiplexer.Address + ":" + strconv.FormatUint(uint64(conf.Multiplexer.Port), 10),
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,
		Handler:      r,
	}

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
