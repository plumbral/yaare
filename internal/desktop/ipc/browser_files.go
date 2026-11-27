package ipc

import (
	"archive/zip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"sync"
	filefs "yaare/internal/adapters/files"

	"yaare/internal/domain/workspace"
)

type browserLocalFolder struct {
	parent *os.Root
	name   string
	folder LocalFolder
}
type browserUpload struct {
	mu            sync.Mutex
	owner         *callerScope
	folder        *browserLocalFolder
	file          *os.File
	stage, target string
	size, offset  int64
}
type browserExportEntry struct {
	Path      string
	Directory bool
}
type browserUploadBegin struct {
	Token, Path string
	Size        int64
}
type browserUploadChunk struct {
	ID, Data string
	Offset   int64
	Finish   bool
}

func browserRelative(p string) bool {
	return p != "" && len(p) <= 4096 && !strings.ContainsAny(p, "\\\x00\r\n:") && !path.IsAbs(p) && fs.ValidPath(p)
}
func (b *BrowserServices) beginBrowserLocal(ctx context.Context) (*LocalFolder, error) {
	page := callerPage(ctx)
	if page == nil || !page.browser {
		return nil, errors.New("浏览器文件区不可用。")
	}
	b.localMu.Lock()
	defer b.localMu.Unlock()
	if current := b.local[page]; current != nil {
		out := current.folder
		return &out, nil
	}
	if b.localRoot == nil {
		return nil, errors.New("浏览器文件区尚未就绪。")
	}
	name := "scope-" + workspace.NewID()
	if e := b.localRoot.Mkdir(name, 0700); e != nil {
		return nil, e
	}
	parent, e := b.localRoot.OpenRoot(name)
	if e != nil {
		return nil, e
	}
	if e = parent.Mkdir("files", 0700); e != nil {
		parent.Close()
		return nil, e
	}
	if e = parent.Mkdir("incoming", 0700); e != nil {
		parent.Close()
		return nil, e
	}
	files, e := parent.OpenRoot("files")
	if e != nil {
		parent.Close()
		return nil, e
	}
	result := LocalFolder{Token: workspace.NewID(), Name: "浏览器文件"}
	b.local[page] = &browserLocalFolder{parent: parent, name: name, folder: result}
	b.Files.mu.Lock()
	b.Files.roots[page] = &localFolder{root: files, token: result.Token, name: result.Name}
	b.Files.mu.Unlock()
	return &result, nil
}
func (b *BrowserServices) releaseBrowserFiles(page *callerScope) {
	b.localMu.Lock()
	folder := b.local[page]
	delete(b.local, page)
	uploads := []*browserUpload{}
	for id, upload := range b.uploads {
		if upload.owner == page {
			uploads = append(uploads, upload)
			delete(b.uploads, id)
		}
	}
	b.localMu.Unlock()
	for _, upload := range uploads {
		upload.mu.Lock()
		upload.file.Close()
		upload.mu.Unlock()
	}
	if folder != nil {
		folder.parent.Close()
		_ = b.localRoot.RemoveAll(folder.name)
	}
}
func (b *BrowserServices) browserFilesCall(ctx context.Context, method string, args []json.RawMessage) (any, error) {
	page := callerPage(ctx)
	if page == nil || !page.browser {
		return nil, errors.New("请在浏览器文件区中操作。")
	}
	switch method {
	case "BrowserFiles.Begin":
		if len(args) != 0 {
			return nil, errors.New("请求参数无效。")
		}
		return b.beginBrowserLocal(ctx)
	case "BrowserFiles.UploadBegin":
		var request browserUploadBegin
		if e := decodeBrowserArgs(args, &request); e != nil {
			return nil, e
		}
		if !browserRelative(request.Path) || request.Size < 0 {
			return nil, errors.New("上传文件名或大小无效。")
		}
		b.localMu.Lock()
		defer b.localMu.Unlock()
		folder := b.local[page]
		if folder == nil || folder.folder.Token != request.Token || len(b.uploads) >= 8 {
			return nil, errors.New("文件区已变化或正在上传，请稍后重试。")
		}
		target := "files/" + request.Path
		if _, e := folder.parent.Lstat(target); e == nil {
			return nil, errors.New("文件区已有同名项目，请先重命名或删除暂存副本。 ")
		} else if !errors.Is(e, fs.ErrNotExist) {
			return nil, e
		}
		if e := folder.parent.MkdirAll(path.Dir(target), 0700); e != nil {
			return nil, e
		}
		id := workspace.NewID()
		stage := "incoming/" + id
		file, e := folder.parent.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return nil, e
		}
		b.uploads[id] = &browserUpload{owner: page, folder: folder, file: file, stage: stage, target: target, size: request.Size}
		return id, nil
	case "BrowserFiles.UploadChunk":
		var request browserUploadChunk
		if e := decodeBrowserArgs(args, &request); e != nil {
			return nil, e
		}
		if len(request.Data) > 350000 {
			return nil, errors.New("上传分块过大。")
		}
		b.localMu.Lock()
		upload := b.uploads[request.ID]
		b.localMu.Unlock()
		if upload == nil || upload.owner != page {
			return nil, errors.New("本次上传不可用，请重新添加文件。")
		}
		upload.mu.Lock()
		defer upload.mu.Unlock()
		raw, e := base64.StdEncoding.DecodeString(request.Data)
		defer clear(raw)
		if e != nil || request.Offset != upload.offset || upload.offset+int64(len(raw)) > upload.size {
			return nil, errors.New("上传内容或顺序发生变化，请重新添加文件。")
		}
		if _, e = upload.file.Write(raw); e != nil {
			return nil, errors.New("写入文件区失败，请检查剩余空间。")
		}
		upload.offset += int64(len(raw))
		if request.Finish {
			if upload.offset != upload.size {
				return nil, errors.New("上传内容尚未完整，未提交文件。")
			}
			if e = upload.file.Sync(); e == nil {
				e = upload.file.Close()
			}
			if e == nil {
				e = (filefs.Local{Root: upload.folder.parent}).Rename(upload.stage, upload.target, false)
			}
			if e != nil {
				return nil, errors.New("上传文件未能提交，请检查同名文件和空间。")
			}
			b.localMu.Lock()
			delete(b.uploads, request.ID)
			b.localMu.Unlock()
		}
		return upload.offset, nil
	case "BrowserFiles.UploadCancel":
		var id string
		if e := decodeBrowserArgs(args, &id); e != nil {
			return nil, e
		}
		b.localMu.Lock()
		upload := b.uploads[id]
		if upload != nil && upload.owner == page {
			delete(b.uploads, id)
		} else {
			upload = nil
		}
		b.localMu.Unlock()
		if upload != nil {
			upload.mu.Lock()
			upload.file.Close()
			_ = upload.folder.parent.Remove(upload.stage)
			upload.mu.Unlock()
		}
		return nil, nil
	default:
		return nil, errors.New("浏览器文件操作无效。")
	}
}

type BrowserDownload struct {
	folder   *os.Root
	entries  []browserExportEntry
	versions []fs.FileInfo
	Name     string
	Size     int64
}

func (b *BrowserServices) PrepareBrowserDownload(ctx context.Context, id string) (*BrowserDownload, error) {
	page := callerPage(ctx)
	b.Files.mu.Lock()
	defer b.Files.mu.Unlock()
	job := b.Files.jobs[id]
	if page == nil || !page.browser || job == nil || job.page != page || job.view.State != "completed" || job.request.Destination.Side != "local" || len(job.browserExports) == 0 {
		return nil, errors.New("本次没有可供保存的已提交文件，请核对任务是否完成或跳过了同名内容。")
	}
	folder := b.Files.roots[page]
	if folder == nil || folder.token != job.request.Destination.Token {
		return nil, errors.New("文件区已变化，请重新下载。")
	}
	out := &BrowserDownload{folder: folder.root, Name: job.view.Name + ".zip"}
	for _, entry := range job.browserExports {
		relative := strings.TrimPrefix(path.Clean("/"+entry.Path), "/")
		info, e := folder.root.Lstat(relative)
		if e != nil || info.Mode()&fs.ModeSymlink != 0 || info.IsDir() != entry.Directory || !entry.Directory && !info.Mode().IsRegular() {
			return nil, errors.New("下载文件已变化，请重新下载。")
		}
		entry.Path = relative
		out.entries = append(out.entries, entry)
		out.versions = append(out.versions, info)
	}
	if len(out.entries) == 1 && !out.entries[0].Directory {
		out.Name = path.Base(out.entries[0].Path)
		out.Size = out.versions[0].Size()
	} else {
		out.Size = -1
	}
	return out, nil
}
func (d *BrowserDownload) Write(ctx context.Context, w io.Writer) error {
	valid := func(i int) error {
		old := d.versions[i]
		now, e := d.folder.Lstat(d.entries[i].Path)
		if e != nil || !os.SameFile(old, now) || !now.ModTime().Equal(old.ModTime()) || now.Size() != old.Size() || now.Mode() != old.Mode() {
			return errors.New("下载文件已变化。")
		}
		return nil
	}
	for i := range d.entries {
		if e := valid(i); e != nil {
			return e
		}
	}
	copyFile := func(i int, w io.Writer) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		file, e := d.folder.Open(d.entries[i].Path)
		if e != nil {
			return e
		}
		defer file.Close()
		stat, e := file.Stat()
		if e != nil || !os.SameFile(stat, d.versions[i]) {
			return errors.New("下载文件已变化。")
		}
		cancel := context.AfterFunc(ctx, func() { file.Close() })
		defer cancel()
		n, e := io.Copy(w, io.LimitReader(file, d.versions[i].Size()+1))
		if e != nil {
			return e
		}
		if n != d.versions[i].Size() {
			return errors.New("下载大小发生变化。")
		}
		return valid(i)
	}
	if d.Size >= 0 {
		return copyFile(0, w)
	}
	archive := zip.NewWriter(w)
	for i, entry := range d.entries {
		if e := ctx.Err(); e != nil {
			return e
		}
		name := entry.Path
		if entry.Directory {
			name += "/"
		}
		header, e := zip.FileInfoHeader(d.versions[i])
		if e != nil {
			return e
		}
		header.Name = name
		header.Method = zip.Deflate
		if entry.Directory {
			header.Method = zip.Store
		}
		target, e := archive.CreateHeader(header)
		if e != nil {
			return e
		}
		if !entry.Directory {
			if e = copyFile(i, target); e != nil {
				return e
			}
		}
	}
	return archive.Close()
}
