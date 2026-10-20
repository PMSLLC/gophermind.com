package blackboard

var allowed = map[Status][]Status{
	StatusPending:       {StatusReady, StatusNeedsRevision},
	StatusReady:         {StatusClaimed, StatusNeedsRevision},
	StatusClaimed:       {StatusInProgress, StatusReady},
	StatusInProgress:    {StatusVerified, StatusFailed, StatusNeedsRevision, StatusReady},
	StatusNeedsRevision: {StatusReady, StatusEscalated},
	StatusEscalated:     {StatusReady, StatusFailed},
	StatusVerified:      {StatusNeedsRevision},
}

func transitionAllowed(from, to Status) bool {
	for _, s := range allowed[from] {
		if s == to {
			return true
		}
	}
	return false
}
