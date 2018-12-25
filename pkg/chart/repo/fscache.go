package repo

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

type fsCache struct {
	dir   string
	limit int
}

func NewFilesystemCache(dir string, limit int) (*fsCache, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	return &fsCache{dir: dir, limit: limit}, nil
}

func (f *fsCache) Fetch(name string) ([]byte, error) {
	name = clean(name)
	path := filepath.Join(f.dir, name)
	return ioutil.ReadFile(path)
}

func (f *fsCache) Store(name string, data []byte) error {
	if err := f.ensureLimits(len(data)); err != nil {
		return nil
	}

	name = clean(name)
	tmp, err := ioutil.TempFile(f.dir, name)
	if err != nil {
		return fmt.Errorf("failed to create tmp file: %v", err)
	}

	defer tmp.Close()
	defer os.Remove(tmp.Name())

	if err := tmp.Chmod(0644); err != nil {
		return fmt.Errorf("failed to chmod: %v", err)
	}

	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("failed to write to tmp file: %v", err)
	}

	path := filepath.Join(f.dir, name)
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("failed to rename %q to %q: %v", tmp.Name(), path, err)
	}

	return nil
}

func (f *fsCache) Clean() error {
	return os.RemoveAll(f.dir)
}

func (f *fsCache) ensureLimits(toStore int) error {
	// TODO
	return nil
}

func clean(v string) string {
	v = strings.Replace(v, ":", "-", -1)
	v = strings.Replace(v, "/", "-", -1)
	v = strings.Replace(v, string(filepath.Separator), "-", -1)
	return v
}
