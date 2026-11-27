package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	syncapp "yaare/internal/app/sync"

	"yaare/internal/adapters/configsync"
	"yaare/internal/domain/workspace"
)

func TestRealSyncCredentialScopeCorruptionAndStalePreview(t *testing.T) {
	runRealSyncTargets(t, "credentials", func(t *testing.T, target configsync.Config, credential string) {
		aCloud, storeA, ctxA := cloudFixture(t, 91)
		bCloud, storeB, ctxB := cloudFixture(t, 92)
		a, b := NewSyncService(aCloud), NewSyncService(bCloud)
		identity := workspace.Identity{ID: workspace.NewID(), Name: "encrypted identity fixture", Username: "fixture", Authentication: "password"}
		secret := []byte(`{"Password":"Credential-Payload-Secret-Test-Only!"}`)
		sealed, e := aCloud.protector.Protect(secret, identity.ID)
		if e != nil {
			t.Fatal(e)
		}
		if e = storeA.SaveIdentity(ctxA, workspace.SealedIdentity{Identity: identity, Cipher: sealed}, false); e != nil {
			t.Fatal(e)
		}
		host := syncHost("credential linked host fixture")
		host.Username = "fixture"
		host.IdentityID = identity.ID
		editSync(t, storeA, ctxA, func(p *workspace.Snapshot) { p.Hosts = []workspace.Host{host} })
		for _, d := range []struct {
			s   *SyncService
			ctx context.Context
		}{{a, ctxA}, {b, ctxB}} {
			if _, e = d.s.Save(d.ctx, SyncSaveRequest{Target: target, Credential: credential, Password: "Sync-Credential-Encryption-Only!"}); e != nil {
				t.Fatal(e)
			}
		}
		syncNow(t, a, ctxA)
		syncNow(t, b, ctxB)
		data, e := storeB.CloudRead(ctxB)
		if e != nil || len(data.Identities) != 1 {
			t.Fatal("identity missing", e)
		}
		plain, e := bCloud.protector.Unprotect(data.Identities[0].Cipher, identity.ID)
		if e != nil || string(plain) != string(secret) {
			t.Fatal("identity was not re-encrypted for target vault", e)
		}
		clear(plain)
		r, c, e := a.load(ctxA)
		if e != nil {
			t.Fatal(e)
		}
		transport, e := a.target(c)
		if e != nil {
			t.Fatal(e)
		}
		defer transport.Close()
		good, tag, e := transport.Get(ctxA, "current.yaaresync")
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(good), "Credential-Payload-Secret") {
			t.Fatal("remote received plaintext")
		}
		metadata, _ := a.Settings(ctxA)
		raw, _ := json.Marshal(metadata)
		if bytes.Contains(raw, c.Password) || bytes.Contains(raw, c.Credential) {
			t.Fatal("read IPC leaked credential")
		}
		damaged := append([]byte(nil), good...)
		damaged[len(damaged)-1] ^= 1
		brokenTag, e := transport.Put(ctxA, "current.yaaresync", damaged, tag)
		if e != nil {
			t.Fatal(e)
		}
		before := storeA.Load(ctxA).Snapshot.Revision
		if _, e = a.Preview(ctxA); e == nil {
			t.Fatal("corrupt encrypted remote accepted")
		}
		if storeA.Load(ctxA).Snapshot.Revision != before {
			t.Fatal("corrupt remote modified local data")
		}
		if _, e = transport.Put(ctxA, "current.yaaresync", good, brokenTag); e != nil {
			t.Fatal(e)
		}
		pending, e := b.Preview(ctxB)
		if e != nil {
			t.Fatal(e)
		}
		editSync(t, storeA, ctxA, func(p *workspace.Snapshot) { p.Hosts[0].Notes = "new remote version" })
		syncNow(t, a, ctxA)
		if _, e = b.Apply(ctxB, pending.ID, nil); e == nil {
			t.Fatal("stale equal preview did not recheck remote")
		}
		if storeB.Load(ctxB).Snapshot.Hosts[0].Notes != "" {
			t.Fatal("stale preview partially applied")
		}
		old, e := syncapp.OpenDocument(good, c.Password)
		if e != nil {
			t.Fatal(e)
		}
		old.Generation = 0
		blob, e := syncapp.SealDocument(old, c.Password)
		if e != nil {
			t.Fatal(e)
		}
		_, currentTag, e := transport.Get(ctxA, "current.yaaresync")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = transport.Put(ctxA, "current.yaaresync", blob, currentTag); e != nil {
			t.Fatal(e)
		}
		if _, e = a.Preview(ctxA); e == nil {
			t.Fatal("remote rollback accepted")
		}
		state, _ := storeA.ReadSync(ctxA)
		if state.Version <= r.Version {
			t.Fatal("sync state did not advance")
		}
	})
}
