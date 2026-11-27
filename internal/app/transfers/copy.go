package transfers

import (
	"context"
	"time"

	filefs "yaare/internal/adapters/files"
)

type Copy struct {
	Source, Destination filefs.FS
	Items               []filefs.Item
	Root, Nonce         string
	Total               int64
	Checkpoint          *filefs.CopyCheckpoint
	Conflict            func(name string, directory bool) (string, error)
	BeforeCopy          func()
	Heartbeat           func()
	OnPhase             func(string)
	OnProgress          func(filefs.Progress, *Rate)
}

type Rate struct{ Speed int64 }

func (copy Copy) Run(ctx context.Context) error {
	last := time.Time{}
	sampleTime := time.Now()
	sampleDone := copy.Checkpoint.Progress.Done
	currentPhase := ""
	phase := func(value string) {
		if currentPhase == value {
			return
		}
		currentPhase = value
		sampleTime = time.Now()
		sampleDone = copy.Checkpoint.Progress.Done
		copy.OnPhase(value)
	}
	copy.BeforeCopy()
	return filefs.CopyResumable(ctx, copy.Source, copy.Destination, copy.Items, copy.Root, copy.Nonce,
		copy.Total, copy.Checkpoint, copy.Conflict, func(p filefs.Progress) {
			copy.Heartbeat()
			now := time.Now()
			if now.Sub(last) < 100*time.Millisecond {
				return
			}
			last = now
			var rate *Rate
			if currentPhase == "transferring" && now.Sub(sampleTime) >= 250*time.Millisecond {
				rate = &Rate{Speed: int64(float64(p.Done-sampleDone) / now.Sub(sampleTime).Seconds())}
				sampleTime = now
				sampleDone = p.Done
			}
			copy.OnProgress(p, rate)
		}, phase)
}
