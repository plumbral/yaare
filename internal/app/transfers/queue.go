package transfers

import "sort"

type Candidate[Owner comparable, Job any] struct {
	Owner                     Owner
	Job                       Job
	Sequence                  uint64
	Active, Ready, Cancelling bool
}

func Claim[Owner comparable, Job any](candidates []Candidate[Owner, Job], limit func(Owner) int) []Job {
	var pending []Candidate[Owner, Job]
	running := make(map[Owner]int)
	active := 0
	for _, candidate := range candidates {
		if candidate.Active {
			active++
			running[candidate.Owner]++
		} else if candidate.Ready {
			pending = append(pending, candidate)
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		if pending[i].Cancelling != pending[j].Cancelling {
			return pending[i].Cancelling
		}
		return pending[i].Sequence < pending[j].Sequence
	})
	claimed := []Job{}
	for _, candidate := range pending {
		if active >= 8 {
			break
		}
		if running[candidate.Owner] >= limit(candidate.Owner) {
			continue
		}
		claimed = append(claimed, candidate.Job)
		active++
		running[candidate.Owner]++
	}
	return claimed
}
