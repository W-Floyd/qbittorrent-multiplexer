package main

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/Jeffail/gabs/v2"
	"github.com/W-Floyd/qbittorrent-multiplexer/qbittorrent"
)

type MergeOptions struct {
	CollisionFn       *func(dest, source interface{}) interface{}
	EntryTransformer  *func(c *Config, entry *gabs.Container) *gabs.Container
	OutputTransformer *func(c *Config, cont *gabs.Container) *gabs.Container
	RootIsArray       bool
	ArraySortFn       *func(a, b *gabs.Container) int
}

type RequestOptions struct {
	Filter   *func(c *Config, r *http.Request) bool      // Returns true if request should be made
	Callback *func(c *Config, resp *http.Response) error // Is called on each response
}

var (
	Statistics = map[*qbittorrent.Instance]struct {
		AlltimeDl *float64
		AlltimeUl *float64
	}{}

	CollisionReplace = func(dest, source interface{}) interface{} {
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
	}
	OutputTransformerTorrents = func(c *Config, cont *gabs.Container) *gabs.Container {
		for _, child := range cont.Data().([]*gabs.Container) {
			for _, key := range c.Multiplexer.Format.Info.RemoveFields {
				child.DeleteP(key)
			}
		}
		return cont
	}
	RequestCallbackTryAllCacheHash = func(c *Config, resp *http.Response) (err error) {
		if resp.Request.Form.Has("hash") {
			if resp.StatusCode == http.StatusOK {
				hash := qbittorrent.Hash(resp.Request.Form.Get("hash"))
				if hash == "" {
					return errors.New("empty hash when inspecting form")
				}
				instance := resp.Request.Context().Value(qbittorrent.ContextKeyInstance).(*qbittorrent.Instance)
				if instance == nil {
					return errors.New("empty instance when inspecting context")
				}
				qbittorrent.Locks.Torrents.Lock()
				defer qbittorrent.Locks.Torrents.Unlock()
				if torrentInstance, ok := qbittorrent.Torrents[hash]; ok {
					if instance != torrentInstance {
						log.Println("Updating hash to point to ", instance.URL.Host)
					} else {
						return nil
					}
				}
				qbittorrent.Torrents[hash] = instance
				return nil
			}
		}
		return errors.New("no hash field in form")
	}
	RequestFilterOnHash = func(c *Config, r *http.Request) bool {
		if r.Form.Has("hash") {
			hash := qbittorrent.Hash(r.Form.Get("hash"))
			if hash == "" {
				return true
			}
			qbittorrent.Locks.Torrents.Lock()
			defer qbittorrent.Locks.Torrents.Unlock()
			if instance, ok := qbittorrent.Torrents[hash]; ok {
				requestInstance := r.Context().Value(qbittorrent.ContextKeyInstance).(*qbittorrent.Instance)
				return instance == requestInstance
			}
		}
		return true
	}
	RequestCallbackTorrentInfoAdd = func(c *Config, resp *http.Response) error {

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		cont, err := gabs.ParseJSON(bodyBytes)
		if err != nil {
			return err
		}

		for _, child := range cont.Children() {
			hash := qbittorrent.Hash(strings.ReplaceAll(child.Search("hash").String(), "\"", ""))
			instance := resp.Request.Context().Value(qbittorrent.ContextKeyInstance).(*qbittorrent.Instance)
			if hash == "" {
				return errors.New("no hash found for child")
			}
			if instance == nil {
				return errors.New("no instance found from context")
			}
			qbittorrent.Locks.Torrents.Lock()
			qbittorrent.Torrents[hash] = instance
			qbittorrent.Locks.Torrents.Unlock()
		}

		return nil
	}
)

func (c *Config) HandleAll(w http.ResponseWriter, r *http.Request) {

	r.ParseForm()

	if r.URL.Path == "/debug/leastbusy" {
		body := strings.NewReader(qbittorrent.LeastBusy().URL.Host)
		c.MakeResponse(nil, &http.Response{Body: io.NopCloser(body)}, w)
	} else if r.URL.Path == "/debug/torrents/perinstance" {
		body := []string{}

		counts := map[*qbittorrent.Instance]int{}
		for _, instance := range qbittorrent.Instances {
			counts[instance] = 0
		}

		for _, instance := range qbittorrent.Torrents {
			counts[instance] += 1
		}

		for instance, count := range counts {
			body = append(body, instance.URL.String()+" - "+strconv.Itoa(count))
		}

		c.MakeResponse(nil, &http.Response{Body: io.NopCloser(strings.NewReader(strings.Join(body, "\n")))}, w)
	} else if strings.HasPrefix(r.URL.Path, "/api/v2/sync/maindata") {
		log.Println("HandlerTorrentsInfo")
		resp, err := c.HandlerTorrentsMaindata(r)
		c.MakeResponse(err, resp, w)
	} else if strings.HasPrefix(r.URL.Path, "/api/v2/torrents/info") {
		log.Println("HandlerMergeJSON - OutputTransformer")
		resp, err := c.HandlerMergeJSON(r,
			RequestOptions{
				Callback: &RequestCallbackTorrentInfoAdd,
			},
			MergeOptions{
				RootIsArray:       true,
				ArraySortFn:       SortRootGabsArrayByKey(c, "added_on"),
				OutputTransformer: &OutputTransformerTorrents,
			},
		)
		c.MakeResponse(err, resp, w)
	} else if strings.HasPrefix(r.URL.Path, "/api/v2/torrents/add") {
		log.Println("HandlerLeastBusy")
		c.HandlerLeastBusy(w, r, RequestOptions{})
	} else if r.Form.Has("hash") {
		log.Println("HandlerTryAll")
		c.HandlerTryAll(w, r, RequestOptions{
			Callback: &RequestCallbackTryAllCacheHash,
			Filter:   &RequestFilterOnHash,
		})
	} else {
		log.Println("HandlerPassthrough")
		if strings.HasPrefix(r.URL.Path, "/api/v2") {
			log.Println("Passing through API call using Round Robin - consider making an exception for this case if appropriate")
			log.Println(r.URL.String())
		}
		c.HandlerPassthrough(w, r)
	}

}

func (c *Config) HandlerPassthrough(w http.ResponseWriter, r *http.Request) {
	i := qbittorrent.NextRoundRobin()
	err := i.Login()
	if err != nil {
		c.MakeResponse(err, nil, w)
		return
	}
	newReq := i.PrepareRequest(r)
	resp, err := i.Client.Do(newReq)
	c.MakeResponse(err, resp, w)
}

func (c *Config) HandlerLeastBusy(w http.ResponseWriter, r *http.Request, requestOptions RequestOptions) {
	i := qbittorrent.LeastBusy()
	err := i.Login()
	if err != nil {
		c.MakeResponse(err, nil, w)
		return
	}
	newReq := i.PrepareRequest(r)
	resp, err := i.Client.Do(newReq)
	c.MakeResponse(err, resp, w)
}

func (c *Config) HandlerTryAll(w http.ResponseWriter, r *http.Request, requestOptions RequestOptions) {

	resps := c.ParallelResponses(r, RequestOptions{
		Filter: requestOptions.Filter,
	})

	successCount := 0
	var resp *http.Response
	var err error

	for _, r := range resps {
		if len(r.errs) == 0 && r.response.StatusCode == http.StatusOK {
			successCount += 1
			resp = r.response
		}
	}

	if successCount == 0 {
		err = errors.New("no successful responses")
	} else if successCount > 1 {
		err = errors.New("more than 1 successful response")
	}
	if err != nil {
		c.MakeResponse(err, nil, w)
	}

	err = (*requestOptions.Callback)(c, resp)
	c.MakeResponse(err, resp, w)

}

func (c *Config) ParallelResponses(r *http.Request, requestOptions RequestOptions) (resps []struct {
	response *http.Response
	instance *qbittorrent.Instance
	errs     []error
}) {
	var g sync.WaitGroup
	for _, i := range qbittorrent.Instances {
		g.Add(1)
		go func() {
			defer g.Done()
			errs := []error{}
			newReq := r.Clone(r.Context())
			err := i.Login()
			if err != nil {
				errs = append(errs, err)
			}
			var resp *http.Response
			newReq = i.PrepareRequest(newReq)
			if requestOptions.Filter != nil && !(*requestOptions.Filter)(c, newReq) {
				return
			} else {
				resp, err = i.Client.Do(newReq)
				if err != nil {
					errs = append(errs, err)
				}
			}
			resps = append(resps, struct {
				response *http.Response
				instance *qbittorrent.Instance
				errs     []error
			}{
				response: resp,
				instance: i,
				errs:     errs,
			})
		}()
	}
	g.Wait()

	if requestOptions.Callback != nil {
		for _, resp := range resps {
			err := (*requestOptions.Callback)(c, resp.response)
			if err != nil {
				resp.errs = append(resp.errs, err)
			}
		}
	}

	return
}

func (c *Config) HandlerTorrentsMaindata(r *http.Request) (*http.Response, error) {

	g := sync.WaitGroup{}

	var resp *http.Response
	var err error

	infoChan := make(chan *gabs.Container)

	callback := func(c *Config, resp *http.Response) error {

		instance := resp.Request.Context().Value(qbittorrent.ContextKeyInstance).(*qbittorrent.Instance)
		if instance == nil {
			return errors.New("empty instance")
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		container, err := gabs.ParseJSON(bodyBytes)
		if err != nil {
			return err
		}

		if _, ok := Statistics[instance]; !ok {
			dl := 0.0
			ul := 0.0
			v := struct {
				AlltimeDl *float64
				AlltimeUl *float64
			}{&dl, &ul}
			Statistics[instance] = v
		}

		pairs := []struct {
			value *float64
			from  string
		}{
			{value: Statistics[instance].AlltimeDl, from: "alltime_dl"},
			{value: Statistics[instance].AlltimeUl, from: "alltime_ul"},
		}

		for _, pair := range pairs {
			path := "server_state." + pair.from
			if container.ExistsP(path) {
				if pair.value == nil {
					v := float64(0)
					pair.value = &v
				}
				*pair.value = container.Path(path).Data().(float64)
			}
		}

		return nil

	}

	g.Add(1)
	go func() {
		defer g.Done()
		resp, err = c.HandlerMergeJSON(r,
			RequestOptions{
				Callback: &callback,
			},
			MergeOptions{
				CollisionFn: &CollisionReplace,
			},
		)
	}()

	for _, i := range qbittorrent.Instances {
		g.Add(1)
		go func() {
			defer g.Done()
			req := &http.Request{
				Method: http.MethodGet,
				URL: &url.URL{
					Path: "/api/v2/transfer/info",
				},
			}

			newReq := i.PrepareRequest(req)
			resp, err := i.Client.Do(newReq)
			if err != nil {
				return
			}

			body, err := gabs.ParseJSONBuffer(resp.Body)
			if err != nil {
				return
			}
			infoChan <- body

		}()
	}

	go func() {
		g.Wait()
		close(infoChan)
	}()

	summedInfo := struct {
		DhtNodes    float64
		DlInfoData  float64
		DlInfoSpeed float64
		DlRateLimit float64
		UpInfoData  float64
		UpInfoSpeed float64
		UpRateLimit float64
	}{}

	pairs := []struct {
		value *float64
		from  string
		to    []string
	}{
		{value: &summedInfo.DhtNodes, from: "dht_nodes"},
		{value: &summedInfo.DhtNodes, from: "dht_nodes"},
		{value: &summedInfo.DlInfoData, from: "dl_info_data"},
		{value: &summedInfo.DlInfoSpeed, from: "dl_info_speed"},
		{value: &summedInfo.DlRateLimit, from: "dl_rate_limit"},
		{value: &summedInfo.UpInfoData, from: "up_info_data"},
		{value: &summedInfo.UpInfoSpeed, from: "up_info_speed"},
		{value: &summedInfo.UpRateLimit, from: "up_rate_limit"},
	}

	for infoCon := range infoChan {
		for _, pair := range pairs {
			*pair.value += infoCon.Path(pair.from).Data().(float64)
		}
	}

	g.Wait()
	if err != nil {
		return nil, err
	}

	bodyCon, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		return nil, err
	}

	for _, pair := range pairs {
		if len(pair.to) == 0 {
			pair.to = []string{pair.from}
		}
		for _, to := range pair.to {
			if _, err = bodyCon.SetP(*pair.value, "server_state."+to); err != nil {
				return nil, err
			}
		}
	}

	stats := struct {
		AlltimeDl float64
		AlltimeUl float64
	}{}

	for _, instance := range qbittorrent.Instances {
		if Statistics[instance].AlltimeDl != nil {
			stats.AlltimeDl += *Statistics[instance].AlltimeDl
		}
		if Statistics[instance].AlltimeUl != nil {
			stats.AlltimeUl += *Statistics[instance].AlltimeUl
		}
	}

	if _, err = bodyCon.SetP(stats.AlltimeDl, "server_state.alltime_dl"); err != nil {
		return nil, err
	}
	if _, err = bodyCon.SetP(stats.AlltimeUl, "server_state.alltime_ul"); err != nil {
		return nil, err
	}

	newBody := bodyCon.Bytes()

	resp.Body = io.NopCloser(bytes.NewBuffer(newBody))
	resp.ContentLength = int64(len(newBody))
	resp.Request = r

	return resp, nil

}

// func (c *Config) Handler

func (c *Config) HandlerMergeJSON(r *http.Request, requestOptions RequestOptions, mergeOptions MergeOptions) (*http.Response, error) {

	if mergeOptions.CollisionFn != nil && mergeOptions.RootIsArray {
		return nil, errors.New("cannot use RootIsArray and CollisionFn at the same time")
	}

	if mergeOptions.ArraySortFn != nil && !mergeOptions.RootIsArray {
		return nil, errors.New("cannot use ArraySortFn when RootIsArray is not true")
	}

	resps := c.ParallelResponses(r, requestOptions)

	var err error

	for _, resp := range resps {
		if len(resp.errs) != 0 {
			err = errors.Join(append(resp.errs, err)...)
		}
	}

	if err != nil {
		return nil, err
	}

	outputCont := &gabs.Container{}
	outputContArray := []*gabs.Container{}

	for _, resp := range resps {

		cont, err := gabs.ParseJSONBuffer(resp.response.Body)
		if err != nil {
			return nil, err
		}

		if mergeOptions.EntryTransformer != nil {
			newCont := (*mergeOptions.EntryTransformer)(c, cont)
			cont = newCont
		}

		if mergeOptions.RootIsArray {
			outputContArray = append(outputContArray, cont.Children()...)
		} else {
			if mergeOptions.CollisionFn != nil {
				outputCont.MergeFn(cont, *mergeOptions.CollisionFn)
			} else {
				outputCont.Merge(cont)
			}
		}
	}

	if mergeOptions.RootIsArray {

		if mergeOptions.ArraySortFn != nil {
			slices.SortStableFunc(outputContArray, *mergeOptions.ArraySortFn)
		}

		outputCont = gabs.Wrap(outputContArray)
	}

	if mergeOptions.OutputTransformer != nil {
		newOutput := (*mergeOptions.OutputTransformer)(c, outputCont)
		outputCont = newOutput
	}

	output := &http.Response{}
	if c.Multiplexer.Format.PrettyPrint {
		output.Body = io.NopCloser(bytes.NewBufferString(outputCont.StringIndent("", "  ")))
	} else {
		output.Body = io.NopCloser(bytes.NewBufferString(outputCont.String()))
	}

	output.Header = resps[0].response.Header.Clone()
	output.Header.Del("Content-Length")

	return output, nil

}

func (c Config) MakeResponse(err error, resp *http.Response, w http.ResponseWriter) {
	if err != nil {
		log.Println(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	} else {
		for header := range resp.Header {
			w.Header().Add(header, resp.Header.Get(header))
		}

		// if resp.Request != nil && resp.Request.Header != nil && strings.Contains(resp.Request.Header.Get("Accept-Encoding"), "gzip") {
		// 	w.Header().Add("Content-Encoding", "gzip")
		// 	newWriter := gzip.NewWriter(w)
		// 	io.Copy(newWriter, resp.Body)
		// } else {
		// 	io.Copy(w, resp.Body)
		// }

		io.Copy(w, resp.Body)

	}
}

func SortRootGabsArrayByKey(c *Config, key string) (f *func(a, b *gabs.Container) int) {
	retval := func(a, b *gabs.Container) int {
		return strings.Compare(a.Path(key).String(), b.Path(key).String())
	}
	return &retval
}
