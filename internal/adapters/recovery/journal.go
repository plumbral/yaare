package recovery

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"yaare/internal/adapters/secrets"
)

const (
	maxManifestPayload = 16 * 1024 * 1024
	maxStatePayload    = 1 * 1024 * 1024
	maxSealedManifest  = 2 * 1024 * 1024
	maxSealedState     = 256 * 1024
)

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)

type Entry struct {
	ID       string
	Manifest []byte
	State    []byte
}
type Journal struct {
	mu        sync.Mutex
	root      func() (string, error)
	dir       string
	protector secrets.Protector
}

func New(root func() (string, error), protector secrets.Protector) *Journal {
	return &Journal{root: root, protector: protector}
}

func VerifyLegacy(root string, p secrets.Protector) error {
	dir := filepath.Join(root, "transfer-recovery")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	j := Journal{dir: dir}
	for _, entry := range entries {
		id, ext, found := strings.Cut(entry.Name(), ".")
		if !found || !idPattern.MatchString(id) {
			continue
		}
		maxSealed := int64(maxSealedState)
		switch ext {
		case "manifest":
			maxSealed = maxSealedManifest
		case "state":
		default:
			continue
		}
		raw, err := j.unlockRecord(p, entry.Name(), "transfer-"+ext+":"+id, maxSealed)
		clear(raw)
		if err != nil {
			return err
		}
	}
	return nil
}
func (j *Journal) Initialize() ([]Entry, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	root, err := j.root()
	if err != nil {
		return nil, err
	}
	j.dir = filepath.Join(root, "transfer-recovery")
	if err = os.MkdirAll(j.dir, 0700); err != nil {
		return nil, err
	}
	names, err := os.ReadDir(j.dir)
	if err != nil {
		return nil, err
	}
	protector, releaseScope := secrets.WithScope(j.protector)
	defer releaseScope()
	entries := []Entry{}
	loadedBytes := 0
	var problem error
	for _, name := range names {
		if name.IsDir() || !strings.HasSuffix(name.Name(), ".manifest") {
			continue
		}
		id := strings.TrimSuffix(name.Name(), ".manifest")
		if !idPattern.MatchString(id) {
			continue
		}
		if len(entries) >= 64 {
			return entries, errors.New("续传记录超过 64 项，请清理不再需要的记录。")
		}
		manifest, e := j.readFile(protector, id+".manifest", "transfer-manifest:"+id, maxSealedManifest, maxManifestPayload)
		if e != nil {
			problem = errors.New("部分续传记录无法解锁，原记录已保留；请在原系统账户中重试。")
			continue
		}
		state, stateErr := j.readFile(protector, id+".state", "transfer-state:"+id, maxSealedState, maxStatePayload)
		if stateErr != nil && !errors.Is(stateErr, os.ErrNotExist) {
			clear(manifest)
			problem = errors.New("部分续传记录无法解锁，原记录已保留；请在原系统账户中重试。")
			continue
		}
		if loadedBytes+len(manifest)+len(state) > 32*1024*1024 {
			clear(manifest)
			clear(state)
			problem = errors.New("续传记录较多，本次先载入部分记录；处理后可重新启动读取其余记录。")
			continue
		}
		loadedBytes += len(manifest) + len(state)
		entries = append(entries, Entry{ID: id, Manifest: manifest, State: state})
	}
	return entries, problem
}

func (j *Journal) unlockRecord(protector secrets.Protector, filename, purpose string, maxSealed int64) ([]byte, error) {
	file, err := os.Open(filepath.Join(j.dir, filename))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxSealed {
		return nil, errors.New("续传记录无效。")
	}
	sealed, err := io.ReadAll(io.LimitReader(file, maxSealed+1))
	if err != nil {
		return nil, err
	}
	defer clear(sealed)
	return protector.Unprotect(sealed, purpose)
}

func (j *Journal) readFile(protector secrets.Protector, filename, purpose string, maxSealed, maxPlain int64) ([]byte, error) {
	data, err := j.unlockRecord(protector, filename, purpose, maxSealed)
	if err != nil {
		return nil, err
	}
	defer clear(data)
	compressed, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer compressed.Close()
	plain, err := io.ReadAll(io.LimitReader(compressed, maxPlain+1))
	if err != nil || int64(len(plain)) > maxPlain {
		clear(plain)
		return nil, errors.New("续传记录过大或不完整。")
	}
	return plain, nil
}

func (j *Journal) writeFile(filename, purpose string, data []byte, maxPlain, maxCompressed int) error {
	if j.dir == "" || len(data) == 0 || len(data) > maxPlain {
		return errors.New("续传记录不可用。")
	}
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(data); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if buffer.Len() > maxCompressed {
		return errors.New("此任务的续传记录过大，退出后需重新传输。")
	}
	compressed := buffer.Bytes()
	defer clear(compressed)
	sealed, err := j.protector.Protect(compressed, purpose)
	if err != nil {
		return err
	}
	defer clear(sealed)
	temp, err := os.CreateTemp(j.dir, "pending-")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(sealed); err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temp.Name(), filepath.Join(j.dir, filename))
}

func (j *Journal) SaveManifest(id string, data []byte) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.dir == "" || !idPattern.MatchString(id) || len(data) == 0 || len(data) > maxManifestPayload {
		return errors.New("续传记录不可用。")
	}
	target := filepath.Join(j.dir, id+".manifest")
	if _, err := os.Stat(target); os.IsNotExist(err) {
		names, e := os.ReadDir(j.dir)
		if e != nil {
			return e
		}
		count := 0
		for _, name := range names {
			if strings.HasSuffix(name.Name(), ".manifest") {
				count++
			}
		}
		if count >= 64 {
			return errors.New("最多保留 64 项中断传输。")
		}
	}
	return j.writeFile(id+".manifest", "transfer-manifest:"+id, data, maxManifestPayload, 1024*1024)
}

func (j *Journal) SaveState(id string, data []byte) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.dir == "" || !idPattern.MatchString(id) || len(data) == 0 || len(data) > maxStatePayload {
		return errors.New("续传记录不可用。")
	}
	return j.writeFile(id+".state", "transfer-state:"+id, data, maxStatePayload, 200*1024)
}

func (j *Journal) Remove(id string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.dir == "" || !idPattern.MatchString(id) {
		return errors.New("续传记录不可用。")
	}
	var firstErr error
	for _, suffix := range []string{".state", ".manifest"} {
		err := os.Remove(filepath.Join(j.dir, id+suffix))
		if err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
