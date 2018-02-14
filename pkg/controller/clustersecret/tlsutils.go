package clustersecret

import (
	"hash/crc32"
	"io/ioutil"
)

type tlsPair struct {
	crt, key string
}

func (p tlsPair) GetAll() ([]byte, []byte, []byte, error) {
	hash := crc32.NewIEEE()

	crt, err := ioutil.ReadFile(p.crt)
	if err != nil {
		return nil, nil, nil, err
	}
	hash.Write(crt)

	key, err := ioutil.ReadFile(p.key)
	if err != nil {
		return nil, nil, nil, err
	}
	hash.Write(key)

	return crt, key, hash.Sum(nil), nil
}
