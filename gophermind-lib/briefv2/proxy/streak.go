package proxy

import "sync"

// Streak counts consecutive attempts of one node that had a critical failure.
type Streak struct {
	mu sync.Mutex
	m  map[string]int
}

// Observe records one attempt: failed increments, not failed resets. It
// returns true once the node has failed three times in a row.
func (s *Streak) Observe(node string, failed bool) (terminal bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]int{}
	}
	if !failed {
		delete(s.m, node)
		return false
	}
	s.m[node]++
	return s.m[node] >= 3
}

// Export returns a copy of the counters.
func (s *Streak) Export() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int, len(s.m))
	for k, v := range s.m {
		out[k] = v
	}
	return out
}

// Import replaces the counters.
func (s *Streak) Import(m map[string]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m = make(map[string]int, len(m))
	for k, v := range m {
		if v > 0 {
			s.m[k] = v
		}
	}
}
