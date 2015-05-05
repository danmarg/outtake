package lib

import (
	"github.com/boltdb/bolt"
	"sync"
)

type Cache interface {
	Set(ns, k string, v []byte)
	Get(ns, k string) ([]byte, bool)
	Del(ns, k string)
	Items(ns string, ks chan<- string)
	Close()
}

type BoltCache struct {
	Cache
	db *bolt.DB
}

func NewBoltCache(path string) (BoltCache, error) {
	db, err := bolt.Open(path, 0666, nil)
	return BoltCache{db: db}, err
}

func (c BoltCache) Set(ns, k string, v []byte) {
	if err := c.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(ns))
		if err != nil {
			return err
		}
		return b.Put([]byte(k), v)
	}); err != nil {
		panic(err)
	}
}

func (c BoltCache) Get(ns, k string) ([]byte, bool) {
	var b []byte
	var ok bool
	if err := c.db.View(func(tx *bolt.Tx) error {
		bk := tx.Bucket([]byte(ns))
		if bk == nil {
			b, ok = nil, false
			return nil
		}
		v := bk.Get([]byte(k))
		if v == nil {
			b, ok = nil, false
			return nil
		}
		b, ok = v, true
		return nil
	}); err != nil {
		panic(err)
	}
	return b, ok
}

func (c BoltCache) Del(ns, k string) {
	if err := c.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(ns))
		if b != nil {
			b.Delete([]byte(k))
		}
		return nil
	}); err != nil {
		panic(err)
	}
}

func (c BoltCache) Items(ns string, ks chan<- string) {
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		if err := c.db.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(ns))
			if b == nil {
				close(ks)
				return nil
			}
			return b.ForEach(func(k, _ []byte) error {
				ks <- string(k)
				return nil
			})
		}); err != nil {
			panic(err)
		}
		wg.Done()
	}()
	wg.Wait()
	close(ks)
}
