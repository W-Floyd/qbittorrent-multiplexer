package qbittorrent

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	URL           string        `usage:"URL of qBittorrent instance (http://hostname:port)"`
	Authenticate  bool          `usage:"Whether to authenticate"`
	Username      string        `usage:"Username for auth - implies Authenticate=true"`
	Password      string        `usage:"Password for auth - implies Authenticate=true"`
	Name          string        `usage:"Name of instance, used as a shorter alternative for user"`
	CookieTimeout time.Duration `usage:"Cookie refresh interval"`
}

type Instance struct {
	URL    *url.URL
	Client *http.Client
	Auth   struct {
		Enabled *bool
		Cookie  struct {
			Timeout time.Duration
			Expires time.Time
			Mutex   sync.Mutex
		}
		Credentials struct {
			Username *string
			Password *string
		}
	}
	Name string
}

type Configs []*Config
type Hash string
type ContextKey *string

var (
	Instances         []*Instance
	Torrents          map[Hash]*Instance = map[Hash]*Instance{}
	RoundRobinCounter int

	Locks struct {
		Instances         sync.Mutex
		Torrents          sync.Mutex
		RoundRobinCounter sync.Mutex
	}
	ContextKeyInstance = NewContextKey("instance")
)

func NewContextKey(key string) ContextKey {
	return ContextKey(&key)
}

func (c Configs) Validate() (errs []error) {

	g := sync.WaitGroup{}
	d := sync.WaitGroup{}

	errsChan := make(chan error)
	instancesChan := make(chan *Instance)

	for _, config := range c {
		g.Add(1)
		go func() {
			defer g.Done()
			instance, instanceErrors := config.New()
			for _, err := range instanceErrors {
				errsChan <- err
			}
			instancesChan <- instance
		}()
	}

	d.Add(3)
	go func() {
		defer d.Done()
		g.Wait()
		close(errsChan)
		close(instancesChan)
	}()
	go func() {
		defer d.Done()
		for instance := range instancesChan {
			Instances = append(Instances, instance)
		}
	}()
	go func() {
		defer d.Done()
		for err := range errsChan {
			errs = append(errs, err)
		}
	}()

	d.Wait()

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

	var jar *cookiejar.Jar

	// Authentication
	if *i.Auth.Enabled || c.Username != "" || c.Password != "" {
		*i.Auth.Enabled = true
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

		jar, err = cookiejar.New(nil)
		if err != nil {
			errs = append(errs, err)
		}

		if c.CookieTimeout == 0 {
			i.Auth.Cookie.Timeout = time.Minute * 15
		} else {
			i.Auth.Cookie.Timeout = c.CookieTimeout
		}

	}

	if len(errs) == 0 {
		i.Client = &http.Client{
			Transport:     http.DefaultTransport,
			Jar:           jar,
			CheckRedirect: http.DefaultClient.CheckRedirect,
		}

	}

	// Authentication
	if *i.Auth.Enabled {
		err := i.Login()
		if err != nil {
			errs = append(errs, errors.New("login failed"), err)
		}
	}

	i.Name = c.Name

	return

}

func (i *Instance) Login() error {

	if !(*i.Auth.Enabled) {
		return nil
	}

	i.Auth.Cookie.Mutex.Lock()
	defer i.Auth.Cookie.Mutex.Unlock()

	if i.Auth.Cookie.Expires.After(time.Now()) {
		return nil
	}

	form := url.Values{}
	form.Add("username", *(i.Auth.Credentials.Username))
	form.Add("password", *(i.Auth.Credentials.Password))

	req, err := i.MakeRequest(http.MethodPost, "/api/v2/auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header = http.Header{}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	newReq := i.PrepareRequest(req)

	resp, err := i.Client.Do(newReq)

	if err != nil || resp.StatusCode != http.StatusOK {
		log.Println("Authentication failed (" + i.URL.Host + ")")
		body := []byte("NONE")
		status := "NONE"
		if resp != nil {
			body, _ = io.ReadAll(resp.Body)
			status = strconv.Itoa(resp.StatusCode)
		}

		suffix := ""
		if err != nil {
			suffix = "\nError: " + err.Error()
		}

		return errors.New("Status Code:" + status + "\nBody:\n" + string(body) + suffix)
	}

	i.Auth.Cookie.Expires = time.Now().Add(i.Auth.Cookie.Timeout)

	log.Println("Authenticated (" + i.URL.Host + ")")

	return nil

}

func (i *Instance) MakeRequest(method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	req.URL.Host = i.URL.Host
	return req, err
}

func (i *Instance) PrepareRequest(r *http.Request) (newReq *http.Request) {

	ctx := r.Context()

	newReq = r.Clone(context.WithValue(ctx, ContextKeyInstance, i))

	newReq.RequestURI = ""
	newReq.URL.Scheme = i.URL.Scheme
	newReq.URL.Host = i.URL.Host
	newReq.Host = i.URL.Host

	if newReq.Header == nil {
		newReq.Header = http.Header{}
	}

	newReq.Header.Set("Referer", i.URL.JoinPath("/").String())
	newReq.Header.Del("Origin")
	newReq.Header.Del("Cookie")
	newReq.Header.Del("Accept-Encoding")

	return

}

func LeastBusy() *Instance {

	Locks.Torrents.Lock()
	defer Locks.Torrents.Unlock()

	Locks.Instances.Lock()
	defer Locks.Instances.Unlock()

	counts := map[*Instance]uint{}

	for _, instance := range Instances {
		counts[instance] = 0
	}

	for _, instance := range Torrents {
		counts[instance] += 1
	}

	var minimum *uint

	for _, c := range counts {
		if minimum == nil {
			minimum = &c
		} else {
			if c < *minimum {
				minimum = &c
			}
		}
	}

	minimumInstances := []*Instance{}

	for instance, count := range counts {
		if count == *minimum {
			minimumInstances = append(minimumInstances, instance)
		}
	}

	slices.SortStableFunc(minimumInstances, func(a, b *Instance) int {
		return strings.Compare(a.URL.Host, b.URL.Host)
	})

	return minimumInstances[0]

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
