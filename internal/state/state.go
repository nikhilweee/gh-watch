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

func configPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "gh-watch", "state.json"), nil
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
