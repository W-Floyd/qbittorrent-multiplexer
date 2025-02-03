package docker

import (
	"errors"
	"os"
	"text/template"
)

type Config struct {
	ProjectName   string `default:"qbittorrent-docker-multiplexer" usage:"Docker project name"`
	DockerCompose struct {
		Qbittorrent string `default:"/config/docker-compose.yaml.tmpl" usage:"Docker Compose entry GoTemplate file for qBittorrent"`
	}
}

func (c Config) Validate() (errs []error) {

	if c.ProjectName == "" {
		errs = append(errs, errors.New("(Docker) Empty Project Name key"))
	}

	if c.DockerCompose.Qbittorrent == "" {
		errs = append(errs, errors.New("(Docker) Empty Docker Compose entry Template File key"))
	} else if _, err := os.Stat(c.DockerCompose.Qbittorrent); errors.Is(err, os.ErrNotExist) {
		errs = append(errs, errors.New("(Docker) Docker Compose entry Template File ("+c.DockerCompose.Qbittorrent+") does not exist"))
	} else if _, err := template.ParseFiles(c.DockerCompose.Qbittorrent); err != nil {
		errs = append(errs, errors.New("(Docker) Docker Compose entry Template File ("+c.DockerCompose.Qbittorrent+") count not be parsed: "), err)
	}

	return errs

}
