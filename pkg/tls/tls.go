package tls

import (
	"hash/crc32"
	"io/ioutil"
)

type Pair struct {
	CrtPath, KeyPath string
}

func (p Pair) GetAll() ([]byte, []byte, []byte, error) {
	hash := crc32.NewIEEE()

	crt, err := ioutil.ReadFile(p.CrtPath)
	if err != nil {
		return nil, nil, nil, err
	}
	hash.Write(crt)

	key, err := ioutil.ReadFile(p.KeyPath)
	if err != nil {
		return nil, nil, nil, err
	}
	hash.Write(key)

	return crt, key, hash.Sum(nil), nil
}
