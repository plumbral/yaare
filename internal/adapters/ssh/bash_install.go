package sshclient

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	filefs "yaare/internal/adapters/files"
	"yaare/internal/domain/workspace"
)

type BashIntegrationPreview struct {
	Action        string `json:"action"`
	ProfilePath   string `json:"profilePath"`
	KeyPath       string `json:"keyPath"`
	Block         string `json:"block"`
	Revision      string `json:"revision"`
	ProfileChange string `json:"profileChange"`
	KeyChange     string `json:"keyChange"`
}

type bashPlan struct {
	BashIntegrationPreview
	profile, key filefs.TextFile
}

var bashProfiles = []string{".bash_profile", ".bash_login", ".profile"}

func bashFileInfo(f filefs.FS, p string, uid uint32, key bool) (fs.FileInfo, error) {
	info, err := f.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky|0022) != 0 ||
		key && (info.Mode().Perm() != 0600 || info.Size() > 160) {
		return nil, errBashIntegration
	}
	if owner, ok := info.Sys().(*sftp.FileStat); ok && uid != owner.UID {
		return nil, errBashIntegration
	}
	return info, nil
}

func planBashIntegration(ctx context.Context, f filefs.FS, home string, uid uint32, scope string) (bashPlan, error) {
	plan := bashPlan{BashIntegrationPreview: BashIntegrationPreview{Action: "install", KeyChange: "create", KeyPath: path.Join(home, bashKeyFile)}}
	keyInfo, err := bashFileInfo(f, plan.KeyPath, uid, true)
	if err != nil {
		return bashPlan{}, err
	}
	var name string
	created := false
	if keyInfo != nil {
		plan.key, err = filefs.ReadText(ctx, f, plan.KeyPath)
		if err != nil {
			return bashPlan{}, err
		}
		fields := strings.Split(strings.TrimSuffix(plan.key.Content, "\n"), " ")
		if len(fields) != 4 || fields[0] != "yaare-bash-v1" || (fields[2] != "0" && fields[2] != "1") || !bashTokenPattern.MatchString(fields[3]) {
			return bashPlan{}, errBashIntegration
		}
		name = fields[1]
		if name != ".bash_profile" && name != ".bash_login" && name != ".profile" {
			return bashPlan{}, errBashIntegration
		}
		created = fields[2] == "1"
		plan.Action, plan.KeyChange = "remove", "delete"
	} else {
		for _, candidate := range bashProfiles {
			name = candidate
			info, e := bashFileInfo(f, path.Join(home, name), uid, false)
			if e != nil {
				return bashPlan{}, e
			}
			if info != nil {
				break
			}
		}
	}
	plan.ProfilePath = path.Join(home, name)
	info, err := bashFileInfo(f, plan.ProfilePath, uid, false)
	if err != nil {
		return bashPlan{}, err
	}
	if info != nil {
		plan.profile, err = filefs.ReadTextEncoding(ctx, f, plan.ProfilePath, "UTF-8")
		if err != nil {
			return bashPlan{}, err
		}
		if plan.profile.LineEnding != "LF" || plan.profile.BOM {
			return bashPlan{}, errBashIntegration
		}
	}
	content := plan.profile.Content
	block := bashProfileBlock()
	marked := strings.Contains(content, bashStartMarker) || strings.Contains(content, bashEndMarker)
	switch {
	case marked:
		if strings.Count(content, block) != 1 || strings.Count(content, bashStartMarker) != 1 || strings.Count(content, bashEndMarker) != 1 {
			return bashPlan{}, errBashIntegration
		}
		if keyInfo == nil {
			plan.KeyChange = "none"
		}
		plan.Action, plan.ProfileChange, plan.Block = "remove", "removeBlock", block
		if created && content == block {
			plan.ProfileChange = "delete"
		}
	case keyInfo != nil:
		plan.ProfileChange = "none"
	case info == nil:
		plan.ProfileChange, plan.Block = "create", block
	default:
		plan.ProfileChange, plan.Block = "append", block
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{scope, plan.ProfilePath, plan.Action, plan.ProfileChange, plan.profile.Version, plan.key.Version, block}, "\x00")))
	plan.Revision = hex.EncodeToString(sum[:])
	return plan, nil
}

func createBashFile(ctx context.Context, f filefs.FS, name, content string) error {
	stage := path.Join(path.Dir(name), ".yaare-edit-"+workspace.NewID()+".part")
	handle, err := f.Create(stage)
	if err != nil {
		return err
	}
	defer f.Remove(stage, false)
	defer handle.Close()
	if err = f.Chmod(stage, 0600); err != nil {
		return err
	}
	info, err := f.Lstat(stage)
	if err != nil || info.Mode() != 0600 {
		return errBashIntegration
	}
	if _, err = handle.Write([]byte(content)); err != nil {
		return err
	}
	if err = handle.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return f.Rename(stage, name, false)
}

func applyBashPlan(ctx context.Context, f filefs.FS, plan bashPlan) error {
	if plan.KeyChange == "create" {
		var key [32]byte
		if _, err := rand.Read(key[:]); err != nil {
			return err
		}
		created := "0"
		if plan.ProfileChange == "create" {
			created = "1"
		}
		keyLine := "yaare-bash-v1 " + path.Base(plan.ProfilePath) + " " + created + " " + hex.EncodeToString(key[:]) + "\n"
		if err := createBashFile(ctx, f, plan.KeyPath, keyLine); err != nil {
			return err
		}
	}
	switch plan.ProfileChange {
	case "create":
		if err := createBashFile(ctx, f, plan.ProfilePath, plan.Block); err != nil {
			return err
		}
	case "append", "removeBlock":
		next := plan.profile.Content + plan.Block
		if plan.ProfileChange == "removeBlock" {
			next = strings.Replace(plan.profile.Content, plan.Block, "", 1)
		}
		saved, err := filefs.SaveText(ctx, f, plan.ProfilePath, plan.profile.Version, next, workspace.NewID())
		if err != nil {
			return err
		}
		if saved.Status != "saved" {
			return errBashChanged
		}
	case "delete":
		if err := removeBashFile(ctx, f, plan.profile); err != nil {
			return err
		}
	}
	if plan.KeyChange == "delete" {
		return removeBashFile(ctx, f, plan.key)
	}
	return nil
}

func removeBashFile(ctx context.Context, f filefs.FS, expected filefs.TextFile) error {
	current, err := filefs.ReadText(ctx, f, expected.Path)
	if err != nil {
		return err
	}
	if current.Version != expected.Version {
		return errBashChanged
	}
	return f.Remove(expected.Path, false)
}

func (s *Session) lockBashDirectory(ctx context.Context, home string) (func(), error) {
	token := workspace.NewID()
	if token == "" {
		return nil, errBashLock
	}
	script := "cd " + shellQuote(home) + " && exec 9<. && command flock -x -w 3 9 || exit 1\n" +
		"printf '%s' " + token + "\nIFS= read -r release"
	command, err := s.OpenCommand(ctx, "/bin/sh -c "+shellQuote(script))
	if err != nil {
		return nil, err
	}
	ack := make([]byte, len(token))
	if _, err = io.ReadFull(command.Output, ack); err != nil || string(ack) != token {
		command.Close()
		return nil, errBashLock
	}
	return func() {
		_ = command.Input.Close()
		command.Close()
	}, nil
}

func (s *Session) ManageBashIntegration(ctx context.Context, scope, action, revision string) (BashIntegrationPreview, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	environment, err := s.bashCommandOutput(ctx, bashConnectionLifetime+`
cd -P "$HOME" || exit 1
supported=no
case "${SHELL-}" in */bash)
  if command -v flock >/dev/null && command -v sha256sum >/dev/null && command -v od >/dev/null && command -v tr >/dev/null &&
    command stat -c '%u' . >/dev/null && __yaare_bash_lifetime_v1 >/dev/null; then supported=yes; fi;;
esac
printf '%s\n%s\n%s' "$supported" "$(command id -u)" "$PWD"`, 8192)
	if err != nil {
		return BashIntegrationPreview{}, err
	}
	supported, details, _ := strings.Cut(environment, "\n")
	uidText, home, ok := strings.Cut(details, "\n")
	uid, err := strconv.ParseUint(uidText, 10, 32)
	if !ok || err != nil || !validDirectory(home) || path.Clean(home) != home {
		return BashIntegrationPreview{}, errBashRequirements
	}
	client, release, err := s.OpenFiles(ctx)
	if err != nil {
		return BashIntegrationPreview{}, err
	}
	defer release()
	info, err := client.Lstat(home)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
		return BashIntegrationPreview{}, errBashIntegration
	}
	if owner, ok := info.Sys().(*sftp.FileStat); !ok || uint32(uid) != owner.UID {
		return BashIntegrationPreview{}, errBashIntegration
	}
	return manageBashFiles(ctx, filefs.Remote{Client: client}, home, uint32(uid), scope+":"+s.directoryDecoder.token, action, revision, supported == "yes", func() (func(), error) {
		return s.lockBashDirectory(ctx, home)
	})
}

func manageBashFiles(ctx context.Context, f filefs.FS, home string, uid uint32, scope, action, revision string, supported bool, lock func() (func(), error)) (BashIntegrationPreview, error) {
	if action != "" {
		unlock, err := lock()
		if err != nil {
			return BashIntegrationPreview{}, err
		}
		defer unlock()
	}
	plan, err := planBashIntegration(ctx, f, home, uid, scope)
	if err != nil {
		return BashIntegrationPreview{}, err
	}
	if plan.Action == "install" && !supported {
		return BashIntegrationPreview{}, errBashRequirements
	}
	if action != "" {
		if plan.Action != action || plan.Revision != revision {
			return BashIntegrationPreview{}, errBashChanged
		}
		err = applyBashPlan(ctx, f, plan)
	}
	return plan.BashIntegrationPreview, err
}
