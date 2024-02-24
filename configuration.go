package configuration

import (
	"bufio"
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sharkpick/channels"
)

var (
	MaintenancePace  = time.Second
	DefaultShouldLog = true
)

type Configuration struct {
	filename         string
	lastupdate       int64
	parameters       map[string]string
	mutex            sync.RWMutex
	ShouldLogUpdates atomic.Bool
}

var (
	ErrEmptyParameter = errors.New("empty parameter")
)

func (c *Configuration) SetFilename(filename string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.filename != filename {
		c.filename = filename
		c.lastupdate = 0
	}
	c.update()
}

func (c *Configuration) SetKeyValue(key, value string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if stored, found := c.parameters[key]; found && stored == value {
		return
	} else if !found && c.ShouldLogUpdates.Load() {
		log.Printf("Configuration::SetKeyValue storing key '%s' with value '%s'\n", key, value)
	} else if found && c.ShouldLogUpdates.Load() {
		log.Printf("Configuration::SetKeyValue updating key '%s' value from '%s' to '%s'\n", key, stored, value)
	}
	c.parameters[key] = value
}

func (c *Configuration) Get(key string) string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.parameters[key]
}

func (c *Configuration) GetSlice(keys []string) []string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	results := make([]string, 0, len(keys))
	for _, key := range keys {
		results = append(results, c.parameters[key])
	}
	return results
}

func SplitConfigurationFileLine(s string) ([2]string, error) {
	if s = strings.TrimSpace(s); len(s) == 0 {
		return [2]string{}, ErrEmptyParameter
	} else if i := strings.IndexAny(s, "=:"); i == -1 {
		return [2]string{}, errors.New("missing delimiter (':' or '=')")
	} else {
		first, second := strings.Clone(s[:i]), strings.Clone(s[i+1:])
		return [2]string{first, second}, nil
	}
}

func (c *Configuration) Update() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.update()
}

func (c *Configuration) update() {
	if stat, err := os.Stat(c.filename); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("Configuration::Update error opening %s: %v\n", c.filename, err)
		}
		return
	} else if !stat.ModTime().After(time.Unix(0, c.lastupdate)) {
		return
	} else {
		f, err := os.Open(c.filename)
		if err != nil {
			log.Printf("Configuration::Update error opening %s: %v\n", c.filename, err)
			return
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			if split, err := SplitConfigurationFileLine(scanner.Text()); err != nil {
				if !errors.Is(err, ErrEmptyParameter) {
					if c.ShouldLogUpdates.Load() {
						log.Printf("Configuration::update error parsing %s: %v\n", scanner.Text(), err)
					}
				}
				continue
			} else {
				if stored, found := c.parameters[split[0]]; found && stored == split[1] {
					continue
				} else if !found && c.ShouldLogUpdates.Load() {
					log.Printf("Configuration::update storing key '%s' with value '%s'\n", split[0], split[1])
				} else if found && c.ShouldLogUpdates.Load() {
					log.Printf("Configuration::update updating key '%s' value from '%s' to '%s'\n", split[0], stored, split[1])
				}
				c.parameters[split[0]] = split[1]
			}
		}
		c.lastupdate = stat.ModTime().UnixNano()
	}
}

func New(filename string, shouldLog ...bool) *Configuration {
	return NewWithContext(context.Background(), filename, shouldLog...)
}

func NewWithContext(ctx context.Context, filename string, shouldLog ...bool) *Configuration {
	config := &Configuration{
		filename: filename,
	}
	config.ShouldLogUpdates.Store(func() bool {
		if len(shouldLog) == 1 {
			return shouldLog[0]
		} else {
			return DefaultShouldLog
		}
	}())
	config.update()
	go func() {
		ticker := time.NewTicker(MaintenancePace)
		defer ticker.Stop()
		for channels.ContextNotDone(ctx) {
			after := time.After(MaintenancePace)
			select {
			case <-after:
				config.Update()
			case <-ctx.Done():
				return
			}
		}
	}()
	return config
}
