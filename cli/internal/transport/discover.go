//go:build !darwin

package transport

import (
	"context"
	"go.bug.st/serial/enumerator"
	"strings"
)

func Discover(ctx context.Context) ([]Candidate, error) {
	type result struct {
		c []Candidate
		e error
	}
	ch := make(chan result, 1)
	go func() {
		ports, e := enumerator.GetDetailedPortsList()
		out := []Candidate{}
		for _, p := range ports {
			if p.IsUSB && strings.EqualFold(p.VID, "0483") && strings.EqualFold(p.PID, "5740") {
				out = append(out, Candidate{p.Name, p.SerialNumber, strings.ToLower(p.VID), strings.ToLower(p.PID)})
			}
		}
		ch <- result{out, e}
	}()
	select {
	case r := <-ch:
		return r.c, r.e
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
