package multiplexer

import (
	"errors"
	"strconv"
	"time"
)

type Config struct {
	Address string `default:"0.0.0.0" usage:"Multiplexer listening address"`
	Port    uint   `default:"9955" usage:"Multiplexer port"`
	Format  struct {
		PrettyPrint bool `usage:"Whether to pretty print outputs (useful for debugging)"`
		Info        struct {
			RemoveFields []string `default:"" usage:"Fields to remove from responses (for client performance)"`
		}
	}
	ShutdownTimeout time.Duration `default:"15s"`
}

func (c Config) Validate() (errs []error) {

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
