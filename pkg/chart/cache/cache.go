package chart

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type fsCache struct {
	dir                  string
	chartFamilySizeLimit int
}

func NewFilesystemCache(dir string, chartFamilySizeLimit int) *fsCache {
	return &fsCache{
		dir:                  dir,
		chartFamilySizeLimit: chartFamilySizeLimit,
	}
}

func (f *fsCache) Fetch(repo, name, version string) (*bytes.Buffer, error) {
	repo, name, version = clean(repo, name, version)

	file := fmt.Sprintf("%s-%s.tgz", name, version)
	path := filepath.Join(f.dir, repo, name, file)
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// it's cool, there's just no cache entry for this one
			return nil, nil
		} else {
			return nil, FetchError(err)
		}
	}
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, FetchError(err)
	}
	return bytes.NewBuffer(data), nil
}

func (f *fsCache) Store(data []byte, repo, name, version string) error {
	repo, name, version = clean(repo, name, version)

	familyPath := filepath.Join(f.dir, repo, name)
	err := os.MkdirAll(familyPath, 0755)
	if err != nil {
		return err
	}

	// check size of dir against limit, delete oldest versions if needed
	versions, err := ioutil.ReadDir(familyPath)
	if err != nil {
		return err
	}

	size := len(data)
	if size > f.chartFamilySizeLimit {
		return fmt.Errorf(
			"%s/%s-%s is %d bytes, which is more than the budget for all cached versions of %s, %d bytes",
			repo, name, version, size, name, f.chartFamilySizeLimit,
		)
	}

	for _, fileinfo := range versions {
		size += int(fileinfo.Size())
	}

	// almost but not really LRU cache eviction via mtime sort: we're
	// not using atime so we don't know if it was read recently.
	sort.SliceStable(versions, func(i, j int) bool {
		return versions[i].ModTime().Before(versions[j].ModTime())
	})

	overhead := size - f.chartFamilySizeLimit
	for _, version := range versions {
		if overhead <= 0 {
			break
		}

		err = os.Remove(filepath.Join(familyPath, version.Name()))
		if err != nil {
			return err
		}
		overhead -= int(version.Size())
	}

	if overhead > 0 {
		return fmt.Errorf(
			"all known versions of %q deleted, but still over the size limit (overhead left: %d). yikes!",
			familyPath, overhead,
		)
	}

	filename := fmt.Sprintf("%s-%s.tgz", name, version)
	chartPath := filepath.Join(familyPath, filename)

	// atomic rename swap to avoid cache hits against partially-written charts
	tmp := fmt.Sprintf("%s_tmp", chartPath)
	err = ioutil.WriteFile(tmp, data, 0644)
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("could not write temp file for chart %q: %s", filename, err)
	}

	return os.Rename(tmp, chartPath)
}

func (f *fsCache) Clean() error {
	return os.RemoveAll(f.dir)
}

func clean(names ...string) (string, string, string) {
	for i := range names {
		// these first two aren't required, but I think there's not much risk
		// in merging https/http repos from the same host, and it makes the
		// directory names a nicer read (not http:__blorg.baz.com)
		names[i] = strings.TrimPrefix(names[i], "https://")
		names[i] = strings.TrimPrefix(names[i], "http://")
		names[i] = strings.Replace(names[i], string(filepath.Separator), "_", -1)
	}
	return names[0], names[1], names[2]
}
