package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
)

type Watch struct {
	PR        int    `json:"pr"`
	Repo      string `json:"repo"`
	AutoMerge bool   `json:"autoMerge,omitempty"`
}

type State struct {
	Watches []Watch `json:"watches"`
}

func configDir() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "gh-watch"), nil
}

func configPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.json"), nil
}

// Config holds user preferences persisted to config.json.
type Config struct {
	Columns      []string `json:"columns,omitempty"`
	SortBy       string   `json:"sortBy,omitempty"`
	SortDesc     bool     `json:"sortDesc,omitempty"`
	PollInterval int      `json:"pollInterval,omitempty"` // seconds, default 60
}

var DefaultColumns = []string{"PR", "STATUS", "AUTO", "TITLE", "REVIEWS", "CHECKS", "UPDATED"}

func configFilePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func LoadConfig() (Config, error) {
	path, err := configFilePath()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Config{Columns: DefaultColumns}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, err
	}
	if len(c.Columns) == 0 {
		c.Columns = DefaultColumns
	}
	if c.PollInterval == 0 {
		c.PollInterval = 60
	}
	return c, nil
}

func SaveConfig(c Config) error {
	path, err := configFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func Load() (*State, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &State{}, nil
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (s *State) Save() error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *State) Add(pr int, repo string, autoMerge bool) {
	for _, w := range s.Watches {
		if w.PR == pr && w.Repo == repo {
			return
		}
	}
	s.Watches = append(s.Watches, Watch{PR: pr, Repo: repo, AutoMerge: autoMerge})
}

func (s *State) Remove(pr int, repo string) {
	s.Watches = slices.DeleteFunc(s.Watches, func(w Watch) bool {
		return w.PR == pr && w.Repo == repo
	})
}

func (s *State) SetAutoMerge(pr int, repo string, autoMerge bool) {
	for i := range s.Watches {
		if s.Watches[i].PR == pr && s.Watches[i].Repo == repo {
			s.Watches[i].AutoMerge = autoMerge
			return
		}
	}
}
