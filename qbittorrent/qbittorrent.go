package qbittorrent

import (
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/template"

	"github.com/W-Floyd/qbittorrent-docker-multiplexer/state"
	util "github.com/W-Floyd/qbittorrent-docker-multiplexer/util"
)

type Config struct {
	PortRangeStart     uint   `default:"11000" usage:"qBittorrent port range start"`
	SecretSeed         string `default:"" usage:"qBittorrent app secret seed"`
	MaximumTorrents    uint   `default:"1000" usage:"qBittorrent maximum torrents per instance"`
	ConfigTemplateFile string `default:"/config/qbittorrent.conf.tmpl" usage:"qBittorrent config GoTemplate file"`
}

func (c *Config) Validate() (errs []error) {

	if c.PortRangeStart < 1024 {
		errs = append(errs, errors.New("(qBittorrent) Port Range Start is in privileged range: "+strconv.FormatUint(uint64(c.PortRangeStart), 10)))
	}

	if c.SecretSeed == "" {
		errs = append(errs, errors.New("(qBittorrent) Empty Secret Seed key"))
	}

	if !(c.MaximumTorrents > 0) {
		errs = append(errs, errors.New("(qBittorrent) Maximum Torrents not greater than 0"+util.UintToString(c.PortRangeStart)))
	}

	if c.ConfigTemplateFile == "" {
		errs = append(errs, errors.New("(qBittorrent) Empty Config Template File key"))
	} else if _, err := os.Stat(c.ConfigTemplateFile); errors.Is(err, os.ErrNotExist) {
		errs = append(errs, errors.New("(Docker) Config Template File ("+c.ConfigTemplateFile+") does not exist"))
	} else if _, err := template.ParseFiles(c.ConfigTemplateFile); err != nil {
		errs = append(errs, errors.New("(Docker) Config Template File ("+c.ConfigTemplateFile+") count not be parsed: "), err)
	}

	return errs

}

func Port(c *Config, i *uint) uint {
	return c.PortRangeStart + *i
}

func Hostname(c *Config, i *uint) string {
	return "127.0.0.1:" + util.UintToString(Port(c, i))
}

func URL(c *Config, i *uint, baseUrl *url.URL) *url.URL {
	var output url.URL
	if baseUrl == nil {
		output = url.URL{}
	} else {
		output = *baseUrl
	}
	output.Scheme = "http"
	output.Host = Hostname(c, i)
	return &output
}

func Password(c *Config, i *uint) string {
	return util.StringToRand("password" + strconv.FormatUint(uint64(*i), 10) + c.SecretSeed)
}

func Username(c *Config, i *uint) string {
	return util.StringToRand("user" + strconv.FormatUint(uint64(*i), 10) + c.SecretSeed)
}

func GetProxy(c *Config, i *uint) (*httputil.ReverseProxy, error) {

	state.AppState.Locks.Proxies.Lock()
	defer state.AppState.Locks.Proxies.Unlock()

	if state.AppState.Proxies == nil {
		state.AppState.Proxies = map[uint]*httputil.ReverseProxy{}
	}
	proxy, ok := state.AppState.Proxies[*i]

	var err error

	if !ok {

		url := URL(c, i, nil)

		proxy = httputil.NewSingleHostReverseProxy(url)
		proxy.Transport = http.DefaultTransport

		state.AppState.Proxies[*i] = proxy

		err = AuthProxy(c, i, proxy)
		if err != nil {
			return nil, err
		}

	}

	if proxy == nil {
		err = errors.New("coultn't get client from AppState")
	}

	if err != nil {
		return nil, err
	}

	return proxy, nil

}

func AuthProxy(c *Config, i *uint, proxy *httputil.ReverseProxy) error {

	if proxy == nil {
		return errors.New("empty proxy")
	}
	if i == nil {
		return errors.New("empty instance")
	}
	if c == nil {
		return errors.New("empty config")
	}

	form := url.Values{}
	form.Add("username", Username(c, i))
	form.Add("password", Password(c, i))

	req, err := http.NewRequest(http.MethodPost, URL(c, i, &url.URL{Path: "/api/v2/auth/login"}).String(), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	resp, err := proxy.Transport.RoundTrip(req)

	if err != nil {
		return err
	} else if resp.StatusCode != http.StatusOK {
		return errors.New("failed to authenticate, status code" + strconv.Itoa(resp.StatusCode))
	}

	cookiesString := resp.Header.Get("Set-Cookie")

	cookie, err := http.ParseSetCookie(cookiesString)
	if err != nil {
		return err
	}

	state.AppState.Locks.Cookies.Lock()
	defer state.AppState.Locks.Cookies.Unlock()

	state.AppState.Cookies[*i] = *cookie

	return nil

}

func LeastBusy() uint {
	counts := map[uint]uint{}

	state.AppState.Locks.Torrents.Lock()
	defer state.AppState.Locks.Torrents.Unlock()

	for _, instance := range state.AppState.Torrents {
		counts[instance] += 1
	}

	var minimum *uint
	var minimumInstance *uint

	for i, c := range counts {
		if minimum == nil {
			minimum = &c
			minimumInstance = &i
		} else {
			if c < *minimum {
				minimum = &c
				minimumInstance = &i
			}
		}
	}

	return *minimumInstance

}
