package engine

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"
)

var backupName = regexp.MustCompile("^[0-9]{8}T[0-9]{6}\\.[0-9]{9}Z\\.json$")

func backupNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, entry := range entries {
		if entry.Type().IsRegular() && backupName.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	slices.Reverse(names)
	return names, nil
}
func backupCurrent(dir string) error {
	old, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	dir = filepath.Join(dir, "backups")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	name := time.Now().UTC().Format("20060102T150405.000000000Z") + ".json"
	if err = atomicWrite(filepath.Join(dir, name), old); err != nil {
		return err
	}
	names, err := backupNames(dir)
	if err != nil {
		return err
	}
	for i := 20; i < len(names); i++ {
		if err = os.Remove(filepath.Join(dir, names[i])); err != nil {
			return err
		}
	}
	return nil
}
func (m *Manager) Backups() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return backupNames(filepath.Join(m.dir, "backups"))
}
func (m *Manager) Restore(name string) error {
	if !backupName.MatchString(name) {
		return errors.New("Некорректное имя резервной копии")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := os.ReadFile(filepath.Join(m.dir, "backups", name))
	if err != nil {
		return err
	}
	var stored storedConfig
	if err = json.Unmarshal(data, &stored); err != nil {
		return err
	}
	c := stored.Config
	c.verifier = stored.Verifier
	if err = c.validate(); err != nil {
		return err
	}
	oldErr := m.configErr
	m.configErr = nil
	if err = m.save(c); err != nil {
		m.configErr = oldErr
	}
	return err
}
