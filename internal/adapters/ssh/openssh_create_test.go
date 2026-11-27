package sshclient

import (
	"context"
	"fmt"
	"path"
	filefs "yaare/internal/adapters/files"
	"testing"
	"time"
)

func TestRealOpenSSHCreateEmptyAndEditWithoutOverwrite(t *testing.T) {
	r := realFixture(t, "yaaretest")
	r.wait(t, "connected")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, closeClient, err := r.session.OpenFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer closeClient()
	remote := filefs.Remote{Client: client}
	name := fmt.Sprintf("新建文件-%d.txt", time.Now().UnixNano())
	target := path.Join("/home/yaaretest/files", name)
	defer client.Remove(target)
	if err = filefs.CreateEmpty(ctx, remote, "/home/yaaretest/files", name); err != nil {
		t.Fatal(err)
	}
	file, err := filefs.ReadText(ctx, remote, target)
	if err != nil || file.Content != "" || file.Size != 0 {
		t.Fatal("empty", err)
	}
	saved, err := filefs.SaveText(ctx, remote, target, file.Version, "创建后编辑 中文🦂\n", "create-validation")
	if err != nil || saved.Status != "saved" {
		t.Fatal("save", err, saved.Status)
	}
	if err = filefs.CreateEmpty(ctx, remote, "/home/yaaretest/files", name); err == nil {
		t.Fatal("accepted duplicate")
	}
	file, err = filefs.ReadText(ctx, remote, target)
	if err != nil || file.Content != "创建后编辑 中文🦂\n" {
		t.Fatal("duplicate altered bytes", err)
	}
	if err = filefs.CreateEmpty(ctx, remote, "/home/yaaretest/readonly", name); err == nil {
		t.Fatal("created in readonly directory")
	}
	if err = filefs.CreateEmpty(ctx, remote, "/home/yaaretest/missing-parent", name); err == nil {
		t.Fatal("created under missing parent")
	}
	if !r.session.Connected() {
		t.Fatal("file error disconnected terminal")
	}
}
