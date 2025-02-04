package qbittorrent

import (
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"sync"
	"time"
)

type Config struct {
	URL          string `usage:"URL of qBittorrent instance (http://hostname:port)"`
	Authenticate bool   `default:"true" usage:"Whether to authenticate"`
	Username     string `usage:"Username for auth"`
	Password     string `usage:"Password for auth"`
}

type Instance struct {
	URL    *url.URL
	Client *http.Client
	Auth   struct {
		Enabled     *bool
		Lock        sync.Mutex
		Credentials struct {
			Username *string
			Password *string
		}
	}
}

type Configs []*Config
type Hash string

var (
	Instances         []*Instance
	Torrents          map[Hash]*Instance
	RoundRobinCounter int

	Locks struct {
		Instances         sync.Mutex
		Torrents          sync.Mutex
		RoundRobinCounter sync.Mutex
	}
)

func (c Configs) Validate() (errs []error) {
	for _, config := range c {
		instance, instanceErrors := config.New()
		errs = append(errs, instanceErrors...)
		Instances = append(Instances, instance)
	}
	log.Println("Config validated")
	return
}

func (c *Config) New() (i *Instance, errs []error) {

	i = &Instance{}

	// URL
	u, err := url.Parse(c.URL)
	if err != nil {
		errs = append(errs, err)
	} else {
		i.URL = u
	}

	i.Auth.Enabled = &c.Authenticate

	// Authentication
	if *i.Auth.Enabled {
		// Credentials
		if c.Username == "" {
			errs = append(errs, errors.New("empty username"))
		} else {
			i.Auth.Credentials.Username = &c.Username
		}
		if c.Password == "" {
			errs = append(errs, errors.New("empty password"))
		} else {
			i.Auth.Credentials.Password = &c.Password
		}
	}

	if len(errs) == 0 {
		i.Client = &http.Client{
			Transport: http.DefaultTransport,
			Jar:       &cookiejar.Jar{},
		}
	}

	// Authentication
	if *i.Auth.Enabled {
		err := i.Login()
		if err != nil {
			errs = append(errs, errors.New("login failed"), err)
		}
	}

	return

}

func (i *Instance) Login() error {

	needToUpdate := false

	for _, cookie := range i.Client.Jar.Cookies(&url.URL{Path: "/api/v2/"}) {
		if !cookie.Expires.After(time.Now()) {
			needToUpdate = true
			break
		}
	}

	if !needToUpdate {
		return nil
	}

	form := url.Values{}
	form.Add("username", *i.Auth.Credentials.Username)
	form.Add("password", *i.Auth.Credentials.Password)

	req := i.MakeRequest("/api/v2/auth/login")
	req.URL.RawQuery = form.Encode()
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	resp, err := i.GetResponse(req)
	if err != nil {
		return err
	} else if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return errors.New("status code " + strconv.Itoa(resp.StatusCode) + ", body:\n" + string(body))
	}

	return nil

}

func (i *Instance) MakeRequest(method string, pathElements ...string) *http.Request {
	return &http.Request{
		Method: method,
		URL:    i.URL.JoinPath(pathElements...),
	}
}

func (i *Instance) GetResponse(r *http.Request) (resp *http.Response, err error) {
	return i.Client.Transport.RoundTrip(r)
}

func LeastBusy() *Instance {

	Locks.Torrents.Lock()
	defer Locks.Torrents.Unlock()

	Locks.Instances.Lock()
	defer Locks.Instances.Unlock()

	counts := map[*Instance]uint{}

	for _, instance := range Torrents {
		counts[instance] += 1
	}

	var minimum *uint
	var minimumInstance *Instance

	for i, c := range counts {
		if minimum == nil {
			minimum = &c
			minimumInstance = i
		} else {
			if c < *minimum {
				minimum = &c
				minimumInstance = i
			}
		}
	}

	return minimumInstance

}

func NextRoundRobin() *Instance {
	Locks.Instances.Lock()
	defer Locks.Instances.Unlock()
	Locks.RoundRobinCounter.Lock()
	defer Locks.RoundRobinCounter.Unlock()
	RoundRobinCounter += 1
	if RoundRobinCounter >= len(Instances) {
		RoundRobinCounter = 0
	}
	return Instances[RoundRobinCounter]
}
