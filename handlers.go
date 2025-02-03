package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/W-Floyd/qbittorrent-docker-multiplexer/qbittorrent"
	"github.com/W-Floyd/qbittorrent-docker-multiplexer/state"
	"golang.org/x/sync/errgroup"

	"github.com/Jeffail/gabs/v2"
)

func (c *Config) HandlerPassthrough(w http.ResponseWriter, r *http.Request) {

	if strings.HasPrefix(r.URL.RequestURI(), "/api/") {
		log.Println("Warning - Passthrough on API call: " + r.URL.RequestURI())
	}

	i := state.AppState.BalancerCount

	resp, err := c.MakeRequest(r, &i)

	if err != nil {
		log.Println(err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	c.MakeResponse(resp, w)

	state.AppState.BalancerCount += 1
	state.AppState.BalancerCount %= state.AppState.NumberOfClients

}

func (c *Config) MakeRequest(r *http.Request, i *uint) (*http.Response, error) {

	body, err := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	proxy, err := qbittorrent.GetProxy(&c.QBittorrent, i)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(
		r.Method,
		qbittorrent.URL(&c.QBittorrent, i, r.URL).String(),
		bytes.NewBuffer(body),
	)
	if err != nil {
		return nil, err
	}

	req.Header = r.Header.Clone()

	err = PrepareRequest(req, c, i)
	if err != nil {
		return nil, err
	}

	oldDirector := proxy.Director

	proxy.Director = func(r *http.Request) {
		rewriteRequestURL(req, qbittorrent.URL(&c.QBittorrent, i, r.URL))
		r.Header.Del("Referer")
	}

	// if strings.HasPrefix(r.URL.RequestURI(), "/api/v2/torrents/add") {
	// 	log.Println(req)
	// }

	resp, err := proxy.Transport.RoundTrip(req)
	proxy.Director = oldDirector

	return resp, err

}

func (c Config) MakeResponse(resp *http.Response, w http.ResponseWriter) {

	for header := range resp.Header {
		w.Header().Add(header, resp.Header.Get(header))
	}

	io.Copy(w, resp.Body)

}

func (c *Config) HandlerHashFinder(w http.ResponseWriter, r *http.Request) {

	body, err := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewBuffer(body))
	if err != nil {
		log.Println(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	hashes := []string{}

	key := "hash"

	if r.Form.Has("hash") {
		hashes = append(hashes, r.Form.Get("hash"))
	}

	if r.Form.Has("hashes") {
		key = "hashes"
		hashes = append(hashes, strings.Split(r.Form.Get("hashes"), "|")...)
	}

	state.AppState.Locks.Torrents.Lock()
	defer state.AppState.Locks.Torrents.Unlock()

	resps := []*http.Response{}

	for _, hash := range hashes {

		form, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			log.Println(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		form.Del("hash")
		form.Del("hashes")
		form.Set(key, hash)

		r.URL.RawQuery = form.Encode()

		instance, ok := state.AppState.Torrents[hash]

		if ok {

			resp, err := c.MakeRequest(r, &instance)
			if err != nil || resp.StatusCode != http.StatusOK {
				ok = false
			} else {
				resps = append(resps, resp)
			}

			r.Form.Del("")

		} else if !ok {

			log.Println("hash not known, looking up: " + hash)
			for i := uint(0); i < state.AppState.NumberOfClients; i++ {

				resp, err := c.MakeRequest(r, &i)
				if err != nil {
					log.Println(err)
					http.Error(w, err.Error(), http.StatusInternalServerError)
				} else if resp.StatusCode == http.StatusOK {
					resps = append(resps, resp)
					state.AppState.Torrents[hash] = i
				}

			}

		}

	}

	if len(resps) == 1 {
		c.MakeResponse(resps[0], w)
	} else if len(resps) > 0 {

		baseResp := resps[0]

		baseBody := ""

		for _, resp := range resps {

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				log.Println(err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}

			baseBody += string(body)

		}

		baseResp.Body = io.NopCloser(bytes.NewBuffer([]byte(baseBody)))

		c.MakeResponse(baseResp, w)

	} else {
		http.Error(w, "no responses", http.StatusInternalServerError)
	}

}

func (c *Config) Parallel(r *http.Request) (output []*struct {
	response *gabs.Container
	instance uint
}, err error) {

	g, ctx := errgroup.WithContext(context.Background())
	responses := make(chan struct {
		response *http.Response
		instance uint
	})

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Println(err)
		return
	}

	for i := uint(0); i < state.AppState.NumberOfClients; i++ {
		g.Go(func() (err error) {

			req, err := http.NewRequest(
				r.Method,
				qbittorrent.URL(&c.QBittorrent, &i, r.URL).String(),
				bytes.NewBuffer(body),
			)
			if err != nil {
				return err
			}

			resp, err := c.MakeRequest(req, &i)
			if err != nil {
				return err
			} else if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				return errors.New("Error" + string(body))
			}

			select {
			case responses <- struct {
				response *http.Response
				instance uint
			}{
				response: resp,
				instance: i,
			}:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}

		})
	}

	go func() {
		g.Wait()
		close(responses)
	}()

	for resp := range responses {
		cont, err := gabs.ParseJSONBuffer(resp.response.Body)
		if err != nil {
			return nil, err
		}

		output = append(output, &struct {
			response *gabs.Container
			instance uint
		}{
			response: cont,
			instance: resp.instance,
		})

	}

	err = g.Wait()
	if err != nil {
		return
	}

	return output, nil

}

func (c *Config) HandlerFetchMergeArray(w http.ResponseWriter, r *http.Request) {

	if r.URL.Path == "/api/v2/torrents/info" {
		state.AppState.Locks.Torrents.Lock()
		defer state.AppState.Locks.Torrents.Unlock()
		state.AppState.Torrents = map[string]uint{}
	}

	resps, err := c.Parallel(r)
	if err != nil {
		log.Println(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	output := []*gabs.Container{}
	for _, resp := range resps {
		for _, child := range resp.response.Children() {
			output = append(output, child)

			if r.URL.Path == "/api/v2/torrents/info" {
				hash := child.Path("hash").Data().(string)
				state.AppState.Torrents[hash] = resp.instance
				log.Println("Stored hash", hash)
			}
		}
	}

	if r.URL.Path == "/api/v2/torrents/info" {
		slices.SortStableFunc(output, func(a *gabs.Container, b *gabs.Container) int {
			return strings.Compare(a.Path("added_on").String(), b.Path("added_on").String())
		})
	}

	w.Write(gabs.Wrap(output).BytesIndent("", "  "))

}

func (c *Config) HandlerFetchMerge(w http.ResponseWriter, r *http.Request) {

	resps, err := c.Parallel(r)
	if err != nil {
		log.Println(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	output := &gabs.Container{}
	for _, resp := range resps {
		output.MergeFn(resp.response, func(dest, source interface{}) interface{} {
			destArr, destIsArray := dest.([]interface{})
			sourceArr, sourceIsArray := source.([]interface{})
			if destIsArray {
				if sourceIsArray {
					return append(destArr, sourceArr...)
				}
				return append(destArr, source)
			}
			if sourceIsArray {
				return append(append([]interface{}{}, dest), sourceArr...)
			}
			return source
		})
	}

	w.Write(gabs.Wrap(output).BytesIndent("", "  "))

}

// Prepepares and rewrites headers required to auth with qBittorrent
func PrepareRequest(r *http.Request, c *Config, i *uint) (err error) {
	if r == nil {
		return errors.New("empty request given")
	}
	if c == nil {
		return errors.New("empty config given")
	}
	if i == nil {
		return errors.New("empty instance given")
	}

	url := qbittorrent.URL(&c.QBittorrent, i, r.URL)

	r.Host = url.String()
	r.Header.Del("Referer")
	r.Header.Del("Origin")
	r.Header.Del("Accept-Encoding")
	r.Header.Set("Cookie", state.AppState.Cookies[*i].Raw)

	return
}

func rewriteRequestURL(req *http.Request, target *url.URL) {
	targetQuery := target.RawQuery
	req.URL.Scheme = target.Scheme
	req.URL.Host = target.Host
	req.URL.Path, req.URL.RawPath = joinURLPath(target, req.URL)
	if targetQuery == "" || req.URL.RawQuery == "" {
		req.URL.RawQuery = targetQuery + req.URL.RawQuery
	} else {
		req.URL.RawQuery = targetQuery + "&" + req.URL.RawQuery
	}
}

func joinURLPath(a, b *url.URL) (path, rawpath string) {
	if a.RawPath == "" && b.RawPath == "" {
		return singleJoiningSlash(a.Path, b.Path), ""
	}
	// Same as singleJoiningSlash, but uses EscapedPath to determine
	// whether a slash should be added
	apath := a.EscapedPath()
	bpath := b.EscapedPath()

	aslash := strings.HasSuffix(apath, "/")
	bslash := strings.HasPrefix(bpath, "/")

	switch {
	case aslash && bslash:
		return a.Path + b.Path[1:], apath + bpath[1:]
	case !aslash && !bslash:
		return a.Path + "/" + b.Path, apath + "/" + bpath
	}
	return a.Path + b.Path, apath + bpath
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}
