package host

import (
	"sync"

	"github.com/jagottsicher/switchboard/internal/config"
	"github.com/jagottsicher/switchboard/internal/sessions"
)

// FromConfig returns the remote hosts of the configuration.
func FromConfig(c config.Config) []Host {
	hosts := make([]Host, len(c.Hosts))
	for i, h := range c.Hosts {
		hosts[i] = NewRemote(h.Name, h.SSH, h.Binary)
	}
	return hosts
}

// Problem is something that went wrong with one host while listing. With Warn set, the
// sessions of the host are still listed.
type Problem struct {
	Host string
	Err  error
	Warn bool
}

// ListAll lists the sessions of all hosts at the same time and sorts them together.
func ListAll(hosts []Host) ([]sessions.Info, []Problem) {
	type result struct {
		infos     []sessions.Info
		warn, err error
	}
	results := make([]result, len(hosts))
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			infos, warn, err := h.List()
			results[i] = result{infos, warn, err}
		}()
	}
	wg.Wait()
	var all []sessions.Info
	var problems []Problem
	for i, r := range results {
		name := hosts[i].Name()
		if r.err != nil {
			problems = append(problems, Problem{Host: name, Err: r.err})
			continue
		}
		if r.warn != nil {
			problems = append(problems, Problem{Host: name, Err: r.warn, Warn: true})
		}
		all = append(all, r.infos...)
	}
	sessions.Sort(all)
	return all, problems
}
