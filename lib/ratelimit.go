package lib

import (
	"time"
)

const windows = 2

type RateLimit struct {
	Period time.Duration
	Rate   uint
	toks   chan struct{}
	paused bool
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

func (r *RateLimit) Get() {
	_ = <-r.toks
}
