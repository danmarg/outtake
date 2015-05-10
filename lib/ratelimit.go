package lib

import (
	"log"
	"math"
	"time"
)

const windows = 1

type RateLimit struct {
	Period       time.Duration
	Rate         uint
	BackoffLimit uint
	BackoffStart time.Duration
	toks         chan struct{}
	paused       bool
}

func (r *RateLimit) Start() {
	r.paused = false
	if r.toks == nil {
		r.toks = make(chan struct{}, windows*r.Rate)
	}
	go func() {
		for true {
			for i := uint(0); i < r.Rate; i++ {
				r.toks <- struct{}{}
			}
			time.Sleep(r.Period)
			if r.paused {
				break
			}
		}
	}()
}

func (r *RateLimit) Stop() {
	r.paused = true
}

func (r *RateLimit) TryGet() bool {
	select {
	case _ = <-r.toks:
		return true
	default:
		return false
	}
}

func (r *RateLimit) DoWithBackoff(f func() (err error, fatal bool)) error {
	var err error
	var fatal bool
	for i := uint(0); i < r.BackoffLimit; i++ {
		r.Get()
		err, fatal = f()
		if err == nil || fatal {
			return err
		}
		s := time.Duration(math.Pow(float64(r.BackoffStart.Nanoseconds()), float64(i)))
		log.Println("DoWithBackoff error: sleeping for", s)
		time.Sleep(s)
	}
	return err
}

func (r *RateLimit) Get() {
	_ = <-r.toks
}
