package sshconfig

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	utf16 "golang.org/x/text/encoding/unicode"
)

type Options struct{ Home, Username, IncludeDir string }
type Host struct {
	Alias, Address, Username       string
	Port, Timeout                  int
	IdentityFiles, Jumps, Warnings []string
	Error                          string
}
type Document struct {
	Hosts    []Host
	Warnings []string
}
type directive struct {
	key      string
	values   []string
	rule     matchRule
	included []directive
	dynamic  bool
}
type reader struct {
	uploaded            bool
	options             Options
	seen                map[string]bool
	files, bytes, count int
}

func Read(path string, options Options) (Document, error) {
	if options.IncludeDir == "" {
		options.IncludeDir = filepath.Join(options.Home, ".ssh")
	}
	r := reader{options: options, seen: map[string]bool{}}
	var lines []directive
	if err := r.read(path, 0, &lines); err != nil {
		return Document{}, err
	}
	return r.document(lines), nil
}

func ReadUploaded(data []byte) (Document, error) {
	if len(data) > 1<<20 {
		return Document{}, errors.New("SSH Config 读取失败或文件过大。")
	}
	r := reader{uploaded: true}
	var lines []directive
	if e := r.parse(data, "config", 0, &lines); e != nil {
		return Document{}, e
	}
	doc := r.document(lines)
	for i := range doc.Hosts {
		h := &doc.Hosts[i]
		if len(h.IdentityFiles) > 0 {
			h.Warnings = append(h.Warnings, "请单独选择或粘贴此主机的私钥。")
			h.IdentityFiles = nil
		}
	}
	return doc, nil
}
func (r *reader) document(lines []directive) Document {
	result := Document{Hosts: []Host{}, Warnings: []string{}}
	for _, alias := range configAliases(lines) {
		if host, found := r.resolve(alias, lines); found {
			result.Hosts = append(result.Hosts, host)
		}
	}
	if len(result.Hosts) == 0 {
		result.Warnings = append(result.Warnings, "没有具体的 Host 别名；通配规则只作为默认值，不会生成主机。")
	}
	return result
}

func configAliases(lines []directive) []string {
	aliases := []string{}
	aliasNames := map[string]bool{}
	var collect func([]directive)
	collect = func(lines []directive) {
		for _, d := range lines {
			if d.key == "host" {
				for _, value := range d.values {
					if value != "" && !strings.ContainsAny(value, "*?![") && !aliasNames[strings.ToLower(value)] {
						aliases = append(aliases, value)
						aliasNames[strings.ToLower(value)] = true
					}
				}
			}
			collect(d.included)
		}
	}
	collect(lines)
	return aliases
}

func (r *reader) read(path string, depth int, lines *[]directive) error {
	if depth > 8 || r.files >= 64 {
		return errors.New("SSH Config 的 Include 层级或文件数过多。")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return errors.New("无法定位 SSH Config。")
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return fmt.Errorf("无法读取配置文件 %s。", filepath.Base(abs))
	}
	if r.seen[canonical] {
		return fmt.Errorf("配置文件 %s 存在循环 Include。", filepath.Base(abs))
	}
	r.seen[canonical] = true
	defer delete(r.seen, canonical)
	f, err := os.Open(canonical)
	if err != nil {
		return fmt.Errorf("无法读取配置文件 %s。", filepath.Base(abs))
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1024*1024 {
		return errors.New("请选择不超过 1 MiB 的普通 SSH Config 文件。")
	}
	data, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 {
		return errors.New("SSH Config 读取失败或文件过大。")
	}
	r.bytes += len(data)
	r.files++
	if r.bytes > 4*1024*1024 {
		return errors.New("SSH Config 及 Include 文件合计过大。")
	}
	return r.parse(data, abs, depth, lines)
}
func (r *reader) parse(data []byte, abs string, depth int, lines *[]directive) error {
	if bytes.HasPrefix(data, []byte{0xff, 0xfe}) || bytes.HasPrefix(data, []byte{0xfe, 0xff}) {
		decoded, e := utf16.UTF16(utf16.LittleEndian, utf16.ExpectBOM).NewDecoder().Bytes(data)
		if e != nil {
			return errors.New("SSH Config 编码无效。")
		}
		data = decoded
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return errors.New("SSH Config 需使用 UTF-8 或带 BOM 的 UTF-16 编码。")
	}
	scan := bufio.NewScanner(bytes.NewReader(data))
	scan.Buffer(make([]byte, 4096), 65536)
	line := 0
	for scan.Scan() {
		line++
		fields, e := tokens(scan.Text())
		if e != nil {
			return fmt.Errorf("%s 第 %d 行的引号未闭合。", filepath.Base(abs), line)
		}
		if len(fields) == 0 {
			continue
		}
		key := strings.ToLower(fields[0])
		values := fields[1:]
		if len(values) == 0 {
			return fmt.Errorf("%s 第 %d 行缺少配置值。", filepath.Base(abs), line)
		}
		r.count++
		if r.count > 20000 {
			return errors.New("SSH Config 配置行过多。")
		}
		if key == "include" {
			if r.uploaded {
				return errors.New("请将 Include 文件展开为单个配置文件后导入。")
			}
			for _, pattern := range values {
				pattern = strings.ReplaceAll(pattern, "${HOME}", r.options.Home)
				if pattern == "~" {
					pattern = r.options.Home
				} else if strings.HasPrefix(pattern, "~/") || strings.HasPrefix(pattern, `~\`) {
					pattern = filepath.Join(r.options.Home, pattern[2:])
				}
				if strings.Contains(pattern, "%") || strings.Contains(pattern, "${") || strings.HasPrefix(pattern, "~") {
					*lines = append(*lines, directive{key: "include", dynamic: true})
					continue
				}
				if !filepath.IsAbs(pattern) {
					pattern = filepath.Join(r.options.IncludeDir, pattern)
				}
				matches, e := filepath.Glob(pattern)
				if e != nil {
					return errors.New("Include 文件匹配规则无效。")
				}
				slices.Sort(matches)
				for _, match := range matches {
					var included []directive
					if e = r.read(match, depth+1, &included); e != nil {
						return e
					}
					*lines = append(*lines, directive{key: "include", included: included})
				}
			}
			continue
		}
		d := directive{key: key, values: values}
		if key == "match" {
			d.rule = parseMatch(values)
		}
		*lines = append(*lines, d)
	}
	if scan.Err() != nil {
		return errors.New("SSH Config 行过长或读取未完成。")
	}
	return nil
}

func tokens(line string) ([]string, error) {
	fields := []string{}
	var token strings.Builder
	var quote byte
	started := false
	sshSyntax := false
	flush := func() {
		if started {
			if len(fields) == 0 {
				switch strings.ToLower(token.String()) {
				case "match", "host", "hostname", "user", "include":
					sshSyntax = true
				}
			}
			fields = append(fields, token.String())
			token.Reset()
			started = false
		}
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == quote || quote == 0 && (c == '"' || sshSyntax && c == '\'') {
			if quote == 0 {
				quote = c
			} else {
				quote = 0
			}
			started = true
			continue
		}
		if c == '\\' && i+1 < len(line) && (line[i+1] == '"' || sshSyntax && (line[i+1] == '\'' || line[i+1] == '\\') || (quote == 0 && (line[i+1] == ' ' || !sshSyntax && line[i+1] == '\t'))) {
			i++
			token.WriteByte(line[i])
			started = true
			continue
		}
		if quote == 0 && c == '#' && (!sshSyntax || !started) {
			break
		}
		if quote == 0 && (c == ' ' || c == '\t' || c == '\r' || c == '=' && (len(fields) == 0 || len(fields) == 1 && !started)) {
			flush()
			continue
		}
		token.WriteByte(c)
		started = true
	}
	if quote != 0 {
		return nil, errors.New("quote")
	}
	flush()
	return fields, nil
}

func (r *reader) resolve(alias string, lines []directive) (Host, bool) {
	h := Host{Alias: alias, Address: alias, Username: r.options.Username, Port: 22, Timeout: 15, IdentityFiles: []string{}, Jumps: []string{}, Warnings: []string{}}
	scan := resolution{alias: alias, localUser: r.options.Username, values: map[string][]string{}}
	scan.visit(lines, applied)
	if (scan.wantFinal || scan.mayFollowCNAME()) && scan.err == "" && len(scan.values["canonicalizehostname"]) == 0 {
		scan.prepareFinal()
		scan.visit(lines, applied)
	}
	values := scan.values
	h.Error = scan.err
	first := func(key string) string {
		if len(values[key]) > 0 {
			return values[key][0]
		}
		return ""
	}
	for _, key := range []string{"hostname", "user", "port", "connecttimeout", "proxyjump", "proxycommand"} {
		if len(values[key]) > 1 || len(values[key]) == 1 && values[key][0] == "" {
			h.Error = "配置项 " + key + " 需要一个值。"
		}
	}
	if user := first("user"); user != "" {
		h.Username = user
	}
	if host := first("hostname"); host != "" {
		h.Address = strings.ReplaceAll(strings.ReplaceAll(host, "%h", alias), "%n", alias)
	}
	if port := first("port"); port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			h.Error = "端口必须为 1–65535。"
		} else {
			h.Port = n
		}
	}
	if timeout := first("connecttimeout"); timeout != "" {
		n, e := strconv.Atoi(timeout)
		if e != nil || n < 3 || n > 120 {
			h.Warnings = append(h.Warnings, "连接超时超出 3–120 秒，使用默认 15 秒。")
		} else {
			h.Timeout = n
		}
	}
	for _, identity := range values["identityfile"] {
		if strings.EqualFold(identity, "none") {
			continue
		}
		identity = strings.NewReplacer("%%", "\x01", "%d", r.options.Home, "%u", r.options.Username, "%n", alias, "%h", h.Address, "%r", h.Username, "%p", strconv.Itoa(h.Port)).Replace(identity)
		identity = strings.ReplaceAll(identity, "${HOME}", r.options.Home)
		if strings.HasPrefix(identity, "~/") || strings.HasPrefix(identity, `~\`) {
			identity = filepath.Join(r.options.Home, identity[2:])
		}
		if strings.Contains(identity, "%") || strings.Contains(identity, "${") || strings.HasPrefix(identity, "~") {
			h.Warnings = append(h.Warnings, "一个密钥引用含不支持的路径变量，未关联。")
			continue
		}
		identity = strings.ReplaceAll(identity, "\x01", "%")
		if !filepath.IsAbs(identity) {
			identity = filepath.Join(r.options.Home, identity)
			h.Warnings = append(h.Warnings, "相对密钥路径按用户主目录解析，请核对密钥选择。")
		}
		identity = filepath.Clean(identity)
		if !slices.Contains(h.IdentityFiles, identity) {
			h.IdentityFiles = append(h.IdentityFiles, identity)
		}
	}
	if jump := first("proxyjump"); jump != "" && !strings.EqualFold(jump, "none") {
		h.Jumps = strings.Split(jump, ",")
		for _, name := range h.Jumps {
			if name == "" || strings.ContainsAny(name, "@:[]") {
				h.Error = "ProxyJump 请引用具名 Host；内联用户名或端口需先拆为 Host 配置。"
			}
		}
	}
	if command := first("proxycommand"); command != "" && !strings.EqualFold(command, "none") {
		h.Error = "ProxyCommand 尚不支持导入；不会执行或改为直连。"
	}
	for _, key := range importBlockers {
		if len(values[key]) > 0 {
			h.Error = "包含尚不支持导入的 " + key + "，请在应用内配置。"
		}
	}
	for key := range values {
		if !known[key] {
			h.Warnings = append(h.Warnings, "未导入配置项："+key)
		}
	}
	if h.Username == "" || utf8.RuneCountInString(h.Username) > 64 || utf8.RuneCountInString(alias) > 64 || strings.ContainsFunc(h.Username, unicode.IsControl) {
		h.Error = "主机别名或用户名无效。"
	}
	slices.Sort(h.Warnings)
	h.Warnings = slices.Compact(h.Warnings)
	if h.Error == "" && scan.aliasState == conditional {
		h.Error = unsupportedMatch
	}
	return h, scan.aliasState != skipped
}

var known = map[string]bool{"hostname": true, "user": true, "port": true, "connecttimeout": true, "identityfile": true, "proxyjump": true, "proxycommand": true}
var importBlockers = []string{"localforward", "remoteforward", "dynamicforward", "certificatefile", "identityagent", "pkcs11provider", "securitykeyprovider", "remotecommand", "hostkeyalias", "canonicalizehostname"}
