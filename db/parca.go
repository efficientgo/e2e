package e2edb

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/efficientgo/core/errors"
	"github.com/efficientgo/e2e"
	"github.com/efficientgo/e2e/db/parcaconfig"
	"gopkg.in/yaml.v2"
)

type Parca struct {
	e2e.InstrumentedRunnable

	configHeader string
}

func NewParca(env e2e.Environment, name string, opts ...Option) *Parca {
	o := options{image: "ghcr.io/parca-dev/parca:main-4e20a666"}
	for _, opt := range opts {
		opt(&o)
	}

	f := e2e.NewInstrumentedRunnable(env, name).WithPorts(map[string]int{"http": 7070}, "http").Future()

	args := map[string]string{
		"--config-path": filepath.Join(f.InternalDir(), "data", "parca.yml"),
	}
	if o.flagOverride != nil {
		args = e2e.MergeFlagsWithoutRemovingEmpty(args, o.flagOverride)
	}

	config := `
object_storage:
  bucket:
    type: "FILESYSTEM"
    config:
      directory: "./data"
scrape_configs:
`
	if err := os.WriteFile(filepath.Join(f.Dir(), "data", "parca.yml"), []byte(config), 0600); err != nil {
		return &Parca{InstrumentedRunnable: e2e.NewErrInstrumentedRunnable(name, errors.Wrap(err, "create prometheus config failed"))}
	}

	return &Parca{
		configHeader: config,
		InstrumentedRunnable: f.Init(e2e.StartOptions{
			Image:     o.image,
			Command:   e2e.NewCommand("/parca", e2e.BuildArgs(args)...),
			User:      strconv.Itoa(os.Getuid()),
			Readiness: e2e.NewTCPReadinessProbe("http"),
		})}
}

// SetScrapeConfigs updates Parca with new configuration  marsh
func (p *Parca) SetScrapeConfigs(scrapeJobs []parcaconfig.ScrapeConfig) error {
	c := p.configHeader

	b, err := yaml.Marshal(struct {
		ScrapeConfigs []parcaconfig.ScrapeConfig `yaml:"scrape_configs,omitempty"`
	}{ScrapeConfigs: scrapeJobs})
	if err != nil {
		return err
	}

	config := fmt.Sprintf("%v\n%v", c, b)
	if err := os.WriteFile(filepath.Join(p.Dir(), "data", "parca.yml"), []byte(config), 0600); err != nil {
		return errors.Wrap(err, "creating parca config failed")
	}

	if p.IsRunning() {
		// Reload configuration.
		return p.Exec(e2e.NewCommand("kill", "-SIGHUP", "1"))
	}
	return nil
}
