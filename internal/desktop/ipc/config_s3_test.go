package ipc

import (
	"bytes"
	"testing"
	"yaare/internal/adapters/storage/sqlite"
	"yaare/internal/domain/workspace"
)

func TestCurrentS3MergeCredentialsConflictAndRestore(t *testing.T) {
	target := fixtureS3(t, nil)
	ca, sa, ctxA := cloudFixture(t, 121)
	cb, sb, ctxB := cloudFixture(t, 122)
	a, b := NewSyncService(ca), NewSyncService(cb)
	identity := workspace.Identity{ID: workspace.NewID(), Name: "current identity", Username: "fixture", Authentication: "password"}
	secret := []byte(`{"Password":"Current-Identity-Test-Only!"}`)
	sealed, e := ca.protector.Protect(secret, identity.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = sa.SaveIdentity(ctxA, workspace.SealedIdentity{Identity: identity, Cipher: sealed}, false); e != nil {
		t.Fatal(e)
	}
	h := syncHost("current S3 host")
	h.IdentityID = identity.ID
	h.Username = identity.Username
	h.Pinned = true
	h.Icon = fixtureCustomIcon(t)
	h.Proxy = workspace.ProxyConfig{Kind: "auto"}
	scopedCommand := workspace.Command{ID: "scoped-command", Name: "Debian fixture", Command: "pwd", Group: workspace.Ungrouped, Icon: "debian"}
	editSync(t, sa, ctxA, func(p *workspace.Snapshot) {
		p.Hosts = []workspace.Host{h}
		p.PinOrder = []string{h.ID}
		p.Commands = append(p.Commands, scopedCommand)
	})
	checkCommand := func(p workspace.Snapshot) {
		t.Helper()
		for _, command := range p.Commands {
			if command == scopedCommand {
				return
			}
		}
		t.Fatal("command OS scope did not follow its command")
	}
	for _, device := range []struct {
		s     *SyncService
		c     *CloudService
		store *sqlite.Store
		left  bool
	}{{a, ca, sa, true}, {b, cb, sb, false}} {
		ctx := ctxB
		if device.left {
			ctx = ctxA
		}
		if _, e = device.s.Save(ctx, SyncSaveRequest{Target: target, Credential: storageTestSecret, Password: storageTestPassword}); e != nil {
			t.Fatal(e)
		}
	}
	syncNow(t, a, ctxA)
	statusEvents := startStatusWatch(t, ctxB, b)
	if initial := receiveStatus(t, statusEvents); initial.Applied != 0 || !initial.Task.Configured {
		t.Fatalf("unexpected initial sync status: %+v", initial)
	}
	syncNow(t, b, ctxB)
	for {
		if pushed := receiveStatus(t, statusEvents); pushed.Applied == 1 && pushed.Settings.LastSync > 0 {
			break
		}
	}
	data, e := sb.CloudRead(ctxB)
	if e != nil || len(data.Identities) != 1 || len(data.Snapshot.Hosts) != 1 || !data.Snapshot.Hosts[0].Pinned || len(data.Snapshot.PinOrder) != 1 {
		t.Fatal("current payload was not restored", e)
	}
	if data.Snapshot.Hosts[0].Icon != h.Icon || data.Snapshot.Hosts[0].Proxy != h.Proxy {
		t.Fatal("custom icon and automatic proxy choice did not reach the second workspace")
	}
	checkCommand(data.Snapshot)
	plain, e := cb.protector.Unprotect(data.Identities[0].Cipher, identity.ID)
	if e != nil || !bytes.Equal(plain, secret) {
		t.Fatal("identity was not re-encrypted for receiving vault", e)
	}
	clear(plain)
	setNote := func(p *workspace.Snapshot, note string) {
		for i := range p.Hosts {
			if p.Hosts[i].ID == h.ID {
				p.Hosts[i].Notes = note
			}
		}
	}
	readNote := func(p workspace.Snapshot) string {
		for _, host := range p.Hosts {
			if host.ID == h.ID {
				return host.Notes
			}
		}
		return ""
	}
	editSync(t, sb, ctxB, func(p *workspace.Snapshot) { setNote(p, "device B edit") })
	editSync(t, sa, ctxA, func(p *workspace.Snapshot) { p.Hosts = append(p.Hosts, syncHost("independent addition")) })
	syncNow(t, a, ctxA)
	syncNow(t, b, ctxB)
	syncNow(t, a, ctxA)
	if got := sa.Load(ctxA).Snapshot; len(got.Hosts) != 2 || readNote(got) != "device B edit" {
		t.Fatal("independent changes did not merge")
	}
	editSync(t, sa, ctxA, func(p *workspace.Snapshot) { setNote(p, "device A conflict") })
	editSync(t, sb, ctxB, func(p *workspace.Snapshot) { setNote(p, "device B conflict") })
	syncNow(t, a, ctxA)
	preview, e := b.Preview(ctxB)
	if e != nil || len(preview.Conflicts) == 0 {
		t.Fatal("missing current conflict", e)
	}
	choices := map[string]string{}
	for _, conflict := range preview.Conflicts {
		choices[conflict.ID] = "local"
	}
	if result, e := b.Apply(ctxB, preview.ID, choices); e != nil || result.State == "conflict" {
		t.Fatal("conflict resolution failed", e)
	}
	before, e := b.History(ctxB)
	if e != nil {
		t.Fatal(e)
	}
	previousNames := map[string]bool{}
	for _, entry := range before {
		previousNames[entry.Name] = true
	}
	if e = b.BackupNow(ctxB); e != nil {
		t.Fatal(e)
	}
	history, e := b.History(ctxB)
	if e != nil || len(history) == 0 {
		t.Fatal("missing current history", e)
	}
	var savedName string
	for _, entry := range history {
		if !previousNames[entry.Name] {
			savedName = entry.Name
		}
	}
	if savedName == "" || readNote(sb.Load(ctxB).Snapshot) != "device B conflict" {
		t.Fatal("current backup or conflict choice was not saved")
	}
	editSync(t, sb, ctxB, func(p *workspace.Snapshot) {
		setNote(p, "after history")
		for i := range p.Commands {
			if p.Commands[i].ID == scopedCommand.ID {
				p.Commands[i].Icon = "linux"
			}
		}
		for i := range p.Hosts {
			if p.Hosts[i].ID == h.ID {
				p.Hosts[i].Icon = "linux"
			}
		}
	})
	restore, e := b.PreviewRestore(ctxB, savedName)
	if e != nil {
		t.Fatal(e)
	}
	result, e := b.Apply(ctxB, restore.ID, nil)
	if e != nil || result.State != "restored" {
		t.Fatal("restore failed", e)
	}
	checkCommand(sb.Load(ctxB).Snapshot)
	for _, host := range sb.Load(ctxB).Snapshot.Hosts {
		if host.ID == h.ID && host.Icon != h.Icon {
			t.Fatal("history did not restore the custom icon")
		}
	}
	record, e := sb.ReadSync(ctxB)
	restoredNote := readNote(sb.Load(ctxB).Snapshot)
	if e != nil || !record.Paused || restoredNote != "device B conflict" {
		t.Fatalf("restore or automatic-sync pause failed: paused=%v note=%q err=%v", record.Paused, restoredNote, e)
	}
}
