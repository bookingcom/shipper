package repo

type Cache interface {
	// Fetch must return a error which satisfies os.IsNotExist()
	// is given name do not exist in the cache
	Fetch(name string) ([]byte, error)
	Store(name string, data []byte) error
	Clean() error
}
