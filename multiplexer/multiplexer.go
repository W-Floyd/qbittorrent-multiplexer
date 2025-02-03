package multiplexer

import (
	"errors"
	"strconv"
	"time"
)

type Config struct {
	Secret  string `default:"" usage:"Multiplexer app secret"`
	Address string `default:"0.0.0.0" usage:"Multiplexer listening address"`
	Port    uint   `default:"9955" usage:"Multiplexer port"`
	Minify  struct {
		Info struct {
			Trackers bool `default:"false" usage:"Whether to remove tracker information for client performance"`
		}
	}
	ShutdownTimeout time.Duration `default:"15s"`
}

func (c Config) Validate() (errs []error) {

	if c.Secret == "" {
		errs = append(errs, errors.New("(Multiplexer) Empty Secret key"))
	}

	if c.Address == "" {
		errs = append(errs, errors.New("(Multiplexer) Empty Listening Address key"))
	}

	if c.Port < 1024 {
		errs = append(errs, errors.New("(Multiplexer) Port in privileged range: "+strconv.FormatUint(uint64(c.Port), 10)))
	}

	if !(c.ShutdownTimeout > time.Second*1) {
		errs = append(errs, errors.New("(Multiplexer) Shutdown Timeout too low"))
	}

	return errs

}
