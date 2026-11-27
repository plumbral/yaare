package ipc

import (
	"context"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"yaare/internal/app/vault"

	"github.com/egoist/mygo"
	"yaare/internal/adapters/sshconfig"
	"yaare/internal/domain/workspace"
)

type ConfigImportRow struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Address      string   `json:"address"`
	Username     string   `json:"username"`
	Port         int      `json:"port"`
	KeyNames     []string `json:"keyNames"`
	KeyAvailable []bool   `json:"keyAvailable"`
	Jumps        []string `json:"jumps"`
	Warnings     []string `json:"warnings"`
	Error        string   `json:"error"`
	Duplicate    bool     `json:"duplicate"`
}
type ConfigImportPreview struct {
	Token    string            `json:"token"`
	Source   string            `json:"source"`
	Rows     []ConfigImportRow `json:"rows"`
	Warnings []string          `json:"warnings"`
}
type ConfigImportChoice struct {
	ID       string `json:"id"`
	KeyIndex int    `json:"keyIndex"`
}
type configImportRepository interface {
	Load(context.Context) workspace.Report
	ImportHosts(context.Context, int64, []workspace.Host, []workspace.SavedKey) (workspace.Result, error)
}
type configImportPlan struct {
	token    string
	revision int64
	expires  time.Time
	hosts    []sshconfig.Host
	rows     []ConfigImportRow
	existing map[string]workspace.Host
}
type SSHConfigService struct {
	repository configImportRepository
	mu         sync.Mutex
	plans      map[*callerScope]configImportPlan
}

func NewSSHConfigService(repository configImportRepository) *SSHConfigService {
	return &SSHConfigService{repository: repository, plans: map[*callerScope]configImportPlan{}}
}

func (s *SSHConfigService) Preview(ctx context.Context, chooseFile bool) (*ConfigImportPreview, error) {
	if err := workspaceCaller(ctx); err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, errors.New("无法定位用户主目录。")
	}
	path := filepath.Join(home, ".ssh", "config")
	if !chooseFile {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("未找到 ~/.ssh/config。")
		}
	}
	if chooseFile {
		window := mygo.CallerWindow(ctx)
		if window == nil {
			return nil, errors.New("请在桌面窗口中选择配置文件。")
		}
		paths, e := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: window, Title: uiText(callerPage(ctx), "选择 SSH Config"), ButtonLabel: uiText(callerPage(ctx), "读取配置"), Filters: []mygo.FileFilter{{Name: "SSH Config", Extensions: []string{"*"}}}})
		if e != nil {
			return nil, errors.New("无法打开配置选择框。")
		}
		if len(paths) == 0 || ctx.Err() != nil {
			return nil, nil
		}
		path = paths[0]
	}
	username := os.Getenv("USERNAME")
	if current, e := user.Current(); e == nil {
		username = current.Username
	}
	if i := strings.LastIndex(username, `\`); i >= 0 {
		username = username[i+1:]
	}
	return s.previewFile(ctx, callerPage(ctx), path, sshconfig.Options{Home: home, Username: username})
}

func (s *SSHConfigService) previewFile(ctx context.Context, page *callerScope, path string, options sshconfig.Options) (*ConfigImportPreview, error) {
	doc, err := sshconfig.Read(path, options)
	if err != nil {
		return nil, err
	}
	return s.previewDocument(ctx, page, doc, filepath.Base(path))
}
func (s *SSHConfigService) previewDocument(ctx context.Context, page *callerScope, doc sshconfig.Document, name string) (*ConfigImportPreview, error) {
	report := s.repository.Load(ctx)
	if !report.Ready {
		return nil, workspace.Fail(report.Code, report.Message)
	}
	if len(doc.Hosts) > 2000 {
		return nil, errors.New("一次最多预览 2,000 个主机。")
	}
	plan := configImportPlan{token: workspace.NewID(), revision: report.Snapshot.Revision, expires: time.Now().Add(15 * time.Minute), hosts: doc.Hosts, rows: []ConfigImportRow{}, existing: map[string]workspace.Host{}}
	for _, host := range report.Snapshot.Hosts {
		plan.existing[strings.ToLower(host.Name)] = host
	}
	all := map[string]sshconfig.Host{}
	for _, host := range doc.Hosts {
		all[strings.ToLower(host.Alias)] = host
	}
	for _, host := range doc.Hosts {
		row := ConfigImportRow{ID: workspace.NewID(), Name: host.Alias, Address: host.Address, Username: host.Username, Port: host.Port, KeyNames: []string{}, KeyAvailable: []bool{}, Jumps: host.Jumps, Warnings: append([]string{}, host.Warnings...), Error: host.Error}
		_, row.Duplicate = plan.existing[strings.ToLower(host.Alias)]
		for _, keyPath := range host.IdentityFiles {
			_, _, e := vault.InspectFile(keyPath)
			row.KeyNames = append(row.KeyNames, filepath.Base(keyPath))
			row.KeyAvailable = append(row.KeyAvailable, e == nil)
			if errors.Is(e, vault.ErrEncryptedPKCS8) {
				row.Warnings = append(row.Warnings, vault.ErrEncryptedPKCS8.Error())
			} else if e != nil {
				row.Warnings = append(row.Warnings, "密钥 "+filepath.Base(keyPath)+" 不可用，可在连接时重新选择。")
			}
		}
		if row.Error == "" {
			if _, e := importRoute(host, all, map[string]bool{}, 0); e != nil {
				row.Error = e.Error()
			}
		}
		check := workspace.Host{ID: row.ID, Name: row.Name, Address: row.Address, Port: row.Port, Username: row.Username, Group: workspace.Ungrouped, System: "Linux", Icon: "linux", Authentication: "none", TimeoutSeconds: host.Timeout}
		probe := workspace.Empty()
		probe.Hosts = []workspace.Host{check}
		if e := workspace.Validate(probe, map[string]bool{}); e != nil {
			row.Error = "主机名称、地址或用户名不符合应用要求。"
		}
		plan.rows = append(plan.rows, row)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for owner, old := range s.plans {
		if time.Now().After(old.expires) {
			delete(s.plans, owner)
		}
	}
	if len(s.plans) >= 32 {
		return nil, errors.New("打开的导入预览过多，请先关闭其他预览。")
	}
	s.plans[page] = plan
	return &ConfigImportPreview{Token: plan.token, Source: name, Rows: plan.rows, Warnings: doc.Warnings}, nil
}

func importRoute(host sshconfig.Host, all map[string]sshconfig.Host, seen map[string]bool, depth int) ([]string, error) {
	key := strings.ToLower(host.Alias)
	if seen[key] || depth > 8 {
		return nil, errors.New("ProxyJump 存在循环或超过 8 跳。")
	}
	seen[key] = true
	defer delete(seen, key)
	route := []string{}
	for _, name := range host.Jumps {
		next, ok := all[strings.ToLower(name)]
		if !ok {
			return nil, errors.New("ProxyJump 引用的 Host 未在配置中定义：" + name)
		}
		if next.Error != "" {
			return nil, errors.New("跳板配置包含不支持项：" + name)
		}
		prefix, e := importRoute(next, all, seen, depth+1)
		if e != nil {
			return nil, e
		}
		route = append(route, prefix...)
		route = append(route, next.Alias)
	}
	if len(route) > 8 {
		return nil, errors.New("ProxyJump 链路超过 8 跳。")
	}
	unique := map[string]bool{}
	for _, name := range route {
		key := strings.ToLower(name)
		if unique[key] {
			return nil, errors.New("ProxyJump 链路包含重复主机。")
		}
		unique[key] = true
	}
	return route, nil
}

func (s *SSHConfigService) Discard(ctx context.Context, token string) {
	page := callerPage(ctx)
	if page == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.plans[page].token == token {
		delete(s.plans, page)
	}
}
func (s *SSHConfigService) Commit(ctx context.Context, token string, choices []ConfigImportChoice) (workspace.Result, error) {
	if err := workspaceCaller(ctx); err != nil {
		return workspace.Result{}, err
	}
	return s.commitPlan(ctx, callerPage(ctx), token, choices)
}
func (s *SSHConfigService) commitPlan(ctx context.Context, page *callerScope, token string, choices []ConfigImportChoice) (workspace.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, ok := s.plans[page]
	if !ok || plan.token != token || time.Now().After(plan.expires) {
		return workspace.Result{}, errors.New("导入预览已过期，请重新读取配置。")
	}
	selected := map[string]ConfigImportChoice{}
	ids := map[string]string{}
	all := map[string]sshconfig.Host{}
	rows := map[string]ConfigImportRow{}
	for i, row := range plan.rows {
		all[strings.ToLower(row.Name)] = plan.hosts[i]
		rows[row.ID] = row
	}
	for _, choice := range choices {
		row, found := rows[choice.ID]
		if !found || row.Error != "" || row.Duplicate || selected[choice.ID].ID != "" {
			return workspace.Result{}, errors.New("所选主机无效、重复或包含不支持的配置，请重新预览。")
		}
		if choice.KeyIndex < -1 || choice.KeyIndex >= len(row.KeyNames) {
			return workspace.Result{}, errors.New("密钥选择无效，请重新预览。")
		}
		selected[choice.ID] = choice
		ids[strings.ToLower(row.Name)] = row.ID
	}
	for name, existing := range plan.existing {
		if configured, found := all[name]; found && existing.Address == configured.Address && existing.Port == configured.Port && existing.Username == configured.Username {
			ids[name] = existing.ID
		}
	}
	hosts := []workspace.Host{}
	keys := []workspace.SavedKey{}
	for i, row := range plan.rows {
		choice, ok := selected[row.ID]
		if !ok {
			continue
		}
		raw := plan.hosts[i]
		host := workspace.Host{ID: row.ID, Name: row.Name, Address: row.Address, Port: row.Port, Username: row.Username, Group: workspace.Ungrouped, System: "Linux", Icon: "linux", Authentication: "none", TimeoutSeconds: raw.Timeout}
		if choice.KeyIndex >= 0 {
			keyPath, format, e := vault.InspectFile(raw.IdentityFiles[choice.KeyIndex])
			if errors.Is(e, vault.ErrEncryptedPKCS8) {
				return workspace.Result{}, e
			}
			if e != nil {
				return workspace.Result{}, errors.New("所选密钥不可用，请重新读取配置或选择连接时认证。")
			}
			key := workspace.SavedKey{ID: workspace.NewID(), Name: filepath.Base(keyPath), Format: format, Path: keyPath}
			keys = append(keys, key)
			host.KeyID = key.ID
			host.Authentication = "key"
		}
		route, e := importRoute(raw, all, map[string]bool{}, 0)
		if e != nil {
			return workspace.Result{}, e
		}
		for _, name := range route {
			id := ids[strings.ToLower(name)]
			if id == "" {
				return workspace.Result{}, errors.New("请同时选择跳板 Host：" + name)
			}
			host.JumpHostIDs = append(host.JumpHostIDs, id)
		}
		hosts = append(hosts, host)
	}
	result, err := s.repository.ImportHosts(ctx, plan.revision, hosts, keys)
	if err == nil {
		delete(s.plans, page)
	}
	return result, err
}
