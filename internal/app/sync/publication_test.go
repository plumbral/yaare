package sync

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"yaare/internal/adapters/configsync"
	"yaare/internal/domain/syncmodel"
	"yaare/internal/domain/workspace"
)

type publicationTarget struct {
	configsync.Storage
	sequence *[]string
	putErr   error
}

func (p publicationTarget) Put(_ context.Context, key string, _ []byte, expected string) (string, error) {
	if key == "current.yaaresync" {
		*p.sequence = append(*p.sequence, "publish:"+expected)
		return "new-etag", p.putErr
	}
	if !strings.HasPrefix(key, "backups/") || expected != "" {
		return "", errors.New("history must use an exclusive immutable name")
	}
	*p.sequence = append(*p.sequence, "history")
	return "backup-etag", nil
}

func (p publicationTarget) PruneBackups(context.Context, int, string) error {
	*p.sequence = append(*p.sequence, "prune")
	return nil
}

func TestPublishMergeStopsAtFailedDurabilityBoundary(t *testing.T) {
	failure := errors.New("injected failure")
	for _, tc := range []struct {
		stage string
		want  []string
	}{
		{"receipt", []string{"history", "receipt"}},
		{"publish", []string{"history", "receipt", "publish:base-etag"}},
		{"commit", []string{"history", "receipt", "publish:base-etag", "commit"}},
		{"success", []string{"history", "receipt", "publish:base-etag", "commit", "prune"}},
	} {
		t.Run(tc.stage, func(t *testing.T) {
			var sequence []string
			target := publicationTarget{sequence: &sequence}
			if tc.stage == "publish" {
				target.putErr = failure
			}
			base, err := syncmodel.Canonicalize(syncmodel.Payload{Version: syncmodel.Version, Snapshot: workspace.Empty()})
			if err != nil {
				t.Fatal(err)
			}
			local := base
			local.Snapshot.Commands = []workspace.Command{{ID: "fixture", Name: "Fixture", Command: "pwd", Group: workspace.Ungrouped, Icon: "linux"}}
			local, err = syncmodel.Canonicalize(local)
			if err != nil {
				t.Fatal(err)
			}
			plan := MergePlan{Base: base, Local: local, Remote: base, ETag: "base-etag", Hash: syncmodel.PayloadHash(local),
				Generation: 8, BaseGeneration: 8, HasBaseline: true, Password: []byte("publication test password")}
			result, err := PublishMerge(t.Context(), plan, target, nil, PublicationHooks{
				Reconcile: func(context.Context, *MergePlan) (bool, error) { return false, nil },
				Materialize: func(context.Context, syncmodel.Payload, bool) (syncmodel.LocalData, error) {
					t.Fatal("the publishing device already has the merged configuration")
					return syncmodel.LocalData{}, nil
				},
				Validate: func(context.Context, syncmodel.Payload, bool) error { return nil },
				Stage:    func(string) {},
				SaveReceipt: func(_ context.Context, data []byte) error {
					sequence = append(sequence, "receipt")
					if len(data) == 0 {
						t.Fatal("empty publication receipt")
					}
					if tc.stage == "receipt" {
						return failure
					}
					return nil
				},
				Commit: func(_ context.Context, c MergeCommit) error {
					sequence = append(sequence, "commit")
					if c.Document.Generation != 9 || c.ETag != "new-etag" || len(c.State) == 0 || !c.Publishing {
						t.Fatalf("incorrect local commit metadata: generation=%d etag=%q", c.Document.Generation, c.ETag)
					}
					if tc.stage == "commit" {
						return failure
					}
					return nil
				},
			})
			if !slices.Equal(sequence, tc.want) {
				t.Fatalf("recorded %v, want %v", sequence, tc.want)
			}
			if tc.stage == "success" {
				if err != nil || result.State != "equal" {
					t.Fatal("publication did not commit", err)
				}
			} else if !errors.Is(err, failure) {
				t.Fatal("publication returned wrong failure", err)
			}
		})
	}
}
