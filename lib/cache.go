package lib

import (
	"log"
	"time"

	"github.com/boltdb/bolt"
)

type Cache interface {
	Set(ns, k string, v []byte)
	Get(ns, k string) ([]byte, bool)
	Del(ns, k string)
	Clear(ns string)
	Items(ns string, ks chan<- string)
	Close()
}

type BoltCache struct {
	Cache
	db       *bolt.DB
	diagSlow time.Duration
}

func NewBoltCache(path string) (BoltCache, error) {
	db, err := bolt.Open(path, 0666, nil)
	c := BoltCache{db: db, diagSlow: 5 * time.Millisecond}
	return c, err
}

func (c BoltCache) logWrite(op, ns, k string, start time.Time) {
	d := time.Since(start)
	if c.diagSlow > 0 && d < c.diagSlow {
		return
	}
	log.Printf("bolt write: op=%s ns=%s key=%s took=%dms", op, ns, k, d.Milliseconds())
}

func (c BoltCache) Set(ns, k string, v []byte) {
	start := time.Now()
	if err := c.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(ns))
		if err != nil {
			return err
		}
		return b.Put([]byte(k), v)
	}); err != nil {
		panic(err)
	}
	c.logWrite("set", ns, k, start)
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
	start := time.Now()
	if err := c.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(ns))
		if b != nil {
			b.Delete([]byte(k))
		}
		return nil
	}); err != nil {
		panic(err)
	}
	c.logWrite("del", ns, k, start)
}

func (c BoltCache) Clear(ns string) {
	start := time.Now()
	if err := c.db.Update(func(tx *bolt.Tx) error {
		name := []byte(ns)
		if err := tx.DeleteBucket(name); err != nil && err != bolt.ErrBucketNotFound {
			return err
		}
		_, err := tx.CreateBucketIfNotExists(name)
		return err
	}); err != nil {
		panic(err)
	}
	c.logWrite("clear", ns, "*", start)
}

func (c BoltCache) Items(ns string, ks chan<- string) {
	go func() {
		defer close(ks)
		if err := c.db.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(ns))
			if b == nil {
				return nil
			}
			return b.ForEach(func(k, _ []byte) error {
				ks <- string(k)
				return nil
			})
		}); err != nil {
			panic(err)
		}
	}()
}

func (c BoltCache) Close() {
	if c.db != nil {
		_ = c.db.Close()
	}
}
