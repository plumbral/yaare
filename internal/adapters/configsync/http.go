package configsync

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"yaare/internal/adapters/cloudsync"
)

type HTTP struct {
	cfg           Config
	credential    []byte
	base          *url.URL
	client        *http.Client
	ownsTransport bool
}

var errNotModified = errors.New("存储未返回配置内容（HTTP 304）。")

func NewTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxConnsPerHost = 2
	return transport
}

func NewHTTP(c Config, credential []byte) (*HTTP, error) {
	return NewHTTPWithTransport(c, credential, nil)
}

func NewHTTPWithTransport(c Config, credential []byte, transport *http.Transport) (*HTTP, error) {
	u, e := url.Parse(c.Endpoint)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !ValidPrefix(c.Prefix) {
		return nil, errors.New("存储地址或目录无效。")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback())) {
		return nil, errors.New("远程存储必须使用 HTTPS；仅本机测试允许 HTTP。")
	}
	if c.Provider != "webdav" && c.Provider != "s3" && c.Provider != "yaare" {
		return nil, errors.New("存储类型无效。")
	}
	if len(c.Username) > 1024 || len(credential) > 16384 {
		return nil, errors.New("存储凭据过长。")
	}
	if c.Provider == "s3" && (c.Bucket == "" || len(c.Bucket) > 63 || strings.ContainsAny(c.Bucket, "/\\\r\n") || c.Region == "" || len(c.Region) > 128 || strings.ContainsAny(c.Region, "/\r\n")) {
		return nil, errors.New("请填写存储桶和区域。")
	}
	if c.Provider == "yaare" {
		if c.Prefix != "" || u.Path != "" && u.Path != "/" {
			return nil, errors.New("请输入 Yaare 实例地址。")
		}
		u.Path = "/api/sync/v2"
	}
	ownsTransport := transport == nil
	if ownsTransport {
		transport = NewTransport()
	}
	return &HTTP{cfg: c, credential: bytes.Clone(credential), base: u, ownsTransport: ownsTransport, client: &http.Client{Transport: transport, Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("存储地址发生重定向，请填写最终地址。")
	}}}, nil
}
func (h *HTTP) Close() error {
	if h.ownsTransport {
		h.client.CloseIdleConnections()
	}
	clear(h.credential)
	h.credential = nil
	return nil
}
func (h *HTTP) s3VirtualHosted() bool {
	return h.cfg.Provider == "s3" && h.cfg.Bucket != "" &&
		strings.HasPrefix(strings.ToLower(h.base.Hostname()), strings.ToLower(h.cfg.Bucket)+".")
}
func (h *HTTP) objectURL(name string) *url.URL {
	u := *h.base
	root := strings.TrimRight(u.Path, "/")
	if h.cfg.Provider == "s3" && !h.s3VirtualHosted() {
		root += "/" + h.cfg.Bucket
	}
	if h.cfg.Prefix != "" {
		root += "/" + strings.Trim(h.cfg.Prefix, "/")
	}
	u.Path = root + "/" + name
	u.RawPath = ""
	if h.cfg.Provider == "s3" {
		u.RawPath = awsPath(u.Path)
	}
	return &u
}
func awsEscape(value string, encodeSlash bool) string {
	const digits = "0123456789ABCDEF"
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		b := value[i]
		if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || b == '.' || b == '_' || b == '~' || (!encodeSlash && b == '/') {
			out.WriteByte(b)
		} else {
			out.WriteByte('%')
			out.WriteByte(digits[b>>4])
			out.WriteByte(digits[b&15])
		}
	}
	return out.String()
}
func awsPath(value string) string {
	return awsEscape(value, false)
}
func awsQuery(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out strings.Builder
	for _, k := range keys {
		vals := append([]string(nil), values[k]...)
		sort.Strings(vals)
		ek := awsEscape(k, true)
		for _, v := range vals {
			if out.Len() > 0 {
				out.WriteByte('&')
			}
			out.WriteString(ek)
			out.WriteByte('=')
			out.WriteString(awsEscape(v, true))
		}
	}
	return out.String()
}
func hexHash(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func mac(key []byte, s string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(s))
	return h.Sum(nil)
}
func (h *HTTP) sign(r *http.Request, body []byte, now time.Time) {
	date := now.UTC().Format("20060102T150405Z")
	day := date[:8]
	hash := hexHash(body)
	r.Header.Set("X-Amz-Date", date)
	r.Header.Set("X-Amz-Content-Sha256", hash)
	names := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	for _, name := range []string{"if-match", "if-none-match"} {
		if r.Header.Get(name) != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var headers strings.Builder
	for _, name := range names {
		value := r.Header.Get(name)
		if name == "host" {
			value = r.URL.Host
		}
		headers.WriteString(name + ":" + strings.TrimSpace(value) + "\n")
	}
	query := awsQuery(r.URL.Query())
	uri := r.URL.EscapedPath()
	if uri == "" {
		uri = "/"
	}
	canonical := r.Method + "\n" + uri + "\n" + query + "\n" + headers.String() + "\n" + strings.Join(names, ";") + "\n" + hash
	scope := day + "/" + h.cfg.Region + "/s3/aws4_request"
	prefix := append([]byte("AWS4"), h.credential...)
	key := mac(prefix, day)
	clear(prefix)
	defer clear(key)
	region := mac(key, h.cfg.Region)
	defer clear(region)
	service := mac(region, "s3")
	defer clear(service)
	signing := mac(service, "aws4_request")
	defer clear(signing)
	signature := hex.EncodeToString(mac(signing, "AWS4-HMAC-SHA256\n"+date+"\n"+scope+"\n"+hexHash([]byte(canonical))))
	r.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+h.cfg.Username+"/"+scope+", SignedHeaders="+strings.Join(names, ";")+", Signature="+signature)
}

type storageStatus int

func (status storageStatus) Error() string {
	if status.Unwrap() == ErrChanged {
		return ErrChanged.Error()
	}
	return fmt.Sprintf("存储请求未完成（HTTP %d）。", int(status))
}

func (status storageStatus) Unwrap() error {
	if status == http.StatusConflict || status == http.StatusLocked {
		return ErrChanged
	}
	return nil
}

func unsupportedMethod(err error) bool {
	return errors.Is(err, storageStatus(http.StatusMethodNotAllowed)) || errors.Is(err, storageStatus(http.StatusNotImplemented))
}

func (h *HTTP) request(ctx context.Context, method string, u *url.URL, body []byte, headers map[string]string) ([]byte, http.Header, error) {
	r, e := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if e != nil {
		return nil, nil, errors.New("无法创建存储请求。")
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	if h.cfg.Provider == "s3" {
		h.sign(r, body, time.Now())
	} else {
		if h.cfg.Provider == "yaare" {
			r.Header.Set("Authorization", "Bearer "+string(h.credential))
		} else {
			plain := make([]byte, 0, len(h.cfg.Username)+1+len(h.credential))
			plain = append(plain, h.cfg.Username...)
			plain = append(plain, ':')
			plain = append(plain, h.credential...)
			header := base64.StdEncoding.AppendEncode([]byte("Basic "), plain)
			clear(plain)
			r.Header.Set("Authorization", string(header))
			clear(header)
		}
	}
	response, e := h.client.Do(r)
	if e != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, errors.New("无法连接存储服务，请检查地址、网络和证书。")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified {
		return nil, response.Header, errNotModified
	}
	if (response.StatusCode < 200 || response.StatusCode >= 300) && response.ContentLength <= 8192 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 8192))
	}
	switch response.StatusCode {
	case 404:
		return nil, nil, ErrMissing
	case http.StatusPreconditionFailed:
		return nil, nil, ErrChanged
	case 401, 403:
		return nil, nil, errors.New("存储认证失败或权限不足，请检查凭据。")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, nil, storageStatus(response.StatusCode)
	}
	data, e := io.ReadAll(io.LimitReader(response.Body, cloudsync.MaxBlob+1))
	if e != nil || len(data) > cloudsync.MaxBlob {
		return nil, nil, errors.New("存储响应不完整或超过配置大小限制。")
	}
	return data, response.Header, nil
}
func (h *HTTP) Get(ctx context.Context, name string) ([]byte, string, error) {
	data, etag, _, err := h.get(ctx, name, "")
	return data, etag, err
}

func (h *HTTP) GetCurrentIfChanged(ctx context.Context, etag string) ([]byte, string, bool, error) {
	if !strongETag(etag) {
		etag = ""
	}
	return h.get(ctx, CurrentObject, etag)
}

func strongETag(tag string) bool {
	if len(tag) < 2 || len(tag) > 1024 || tag[0] != '"' || tag[len(tag)-1] != '"' {
		return false
	}
	for index := 1; index < len(tag)-1; index++ {
		value := tag[index]
		if value <= ' ' || value == '"' || value == 127 {
			return false
		}
	}
	return true
}

func (h *HTTP) get(ctx context.Context, name, validator string) ([]byte, string, bool, error) {
	if !ValidName(name) {
		return nil, "", false, errors.New("配置对象名称无效。")
	}
	headers := map[string]string{"Cache-Control": "no-cache"}
	if validator != "" {
		headers["If-None-Match"] = validator
	}
	data, header, e := h.request(ctx, "GET", h.objectURL(name), nil, headers)
	if validator != "" && errors.Is(e, errNotModified) {
		if tags := header.Values("ETag"); len(tags) == 1 && tags[0] == validator {
			return nil, validator, true, nil
		}
		delete(headers, "If-None-Match")
		data, header, e = h.request(ctx, "GET", h.objectURL(name), nil, headers)
	}
	if e != nil {
		return nil, "", false, e
	}
	etag := header.Get("ETag")
	for attempt := 0; strings.HasPrefix(etag, "W/") && attempt < 3; attempt++ {
		select {
		case <-ctx.Done():
			return nil, "", false, ctx.Err()
		case <-time.After(1100 * time.Millisecond):
		}
		data, header, e = h.request(ctx, "GET", h.objectURL(name), nil, map[string]string{"Connection": "close", "Cache-Control": "no-cache"})
		if e != nil {
			return nil, "", false, e
		}
		etag = header.Get("ETag")
	}
	if name == CurrentObject && (!strongETag(etag) || len(header.Values("ETag")) != 1) {
		return nil, "", false, ErrConditions
	}
	return data, etag, false, nil
}
func (h *HTTP) Head(ctx context.Context, name string) (string, error) {
	if !ValidName(name) {
		return "", errors.New("配置对象名称无效。")
	}
	_, header, e := h.request(ctx, "HEAD", h.objectURL(name), nil, map[string]string{"Cache-Control": "no-cache"})
	if e != nil {
		if unsupportedMethod(e) {
			_, etag, getErr := h.Get(ctx, name)
			return etag, getErr
		}
		return "", e
	}
	etag := header.Get("ETag")
	for attempt := 0; strings.HasPrefix(etag, "W/") && attempt < 3; attempt++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(1100 * time.Millisecond):
		}
		_, header, e = h.request(ctx, "HEAD", h.objectURL(name), nil, map[string]string{"Connection": "close", "Cache-Control": "no-cache"})
		if e != nil {
			return "", e
		}
		etag = header.Get("ETag")
	}
	if name == CurrentObject && (!strongETag(etag) || len(header.Values("ETag")) != 1) {
		_, etag, getErr := h.Get(ctx, name)
		return etag, getErr
	}
	return etag, nil
}
func (h *HTTP) Put(ctx context.Context, name string, data []byte, expected string) (string, error) {
	if !ValidName(name) || len(data) > cloudsync.MaxBlob {
		return "", errors.New("配置对象无效或过大。")
	}
	headers := map[string]string{"Content-Type": "application/octet-stream"}
	if h.cfg.Provider == "webdav" && (name == CurrentObject || expected != "" && expected != "*") {
		token, release, e := h.lock(ctx)
		if e != nil {
			return "", e
		}
		defer release()
		actual, e := h.Head(ctx, name)
		if expected == "" {
			if e == nil {
				return "", ErrChanged
			}
			if !errors.Is(e, ErrMissing) {
				return "", e
			}
		} else {
			if e != nil {
				return "", e
			}
			if actual != expected {
				return "", ErrChanged
			}
		}
		headers["If"] = "<" + h.objectURL("").String() + "> (" + token + ")"
	}
	if expected == "" || h.cfg.Provider == "yaare" && expected == "*" {
		headers["If-None-Match"] = "*"
	} else if expected != "*" {
		headers["If-Match"] = expected
	}
	if h.cfg.Provider == "webdav" && (name == CurrentObject || strings.HasPrefix(name, "backups/")) {
		temp, e := newProbe()
		if e != nil {
			return "", e
		}
		moved := false
		defer func() {
			if moved {
				return
			}
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			cleanupHeaders := map[string]string{}
			if token := headers["If"]; token != "" {
				cleanupHeaders["If"] = token
			}
			_, _, _ = h.request(cleanup, "DELETE", h.objectURL(temp), nil, cleanupHeaders)
		}()
		stageHeaders := map[string]string{"Content-Type": "application/octet-stream", "If-None-Match": "*"}
		if token := headers["If"]; token != "" {
			stageHeaders["If"] = token
		}
		if _, _, e = h.request(ctx, "PUT", h.objectURL(temp), data, stageHeaders); e != nil {
			return "", e
		}
		stored, _, e := h.Get(ctx, temp)
		if e != nil {
			return "", e
		}
		if !bytes.Equal(stored, data) {
			return "", ErrChanged
		}
		if e = h.move(ctx, temp, name, expected != "", headers["If"]); e != nil {
			return "", e
		}
		moved = true
		return h.Head(ctx, name)
	}
	_, respHeader, e := h.request(ctx, "PUT", h.objectURL(name), data, headers)
	if e != nil {
		return "", e
	}
	if h.cfg.Provider == "s3" || h.cfg.Provider == "yaare" {
		if tags := respHeader.Values("ETag"); len(tags) == 1 && strongETag(tags[0]) {
			return tags[0], nil
		}
	}
	stored, etag, err := h.Get(ctx, name)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(stored, data) {
		return "", ErrChanged
	}
	return etag, nil
}
func (h *HTTP) lock(ctx context.Context) (string, func(), error) {
	body := []byte(`<?xml version="1.0"?><d:lockinfo xmlns:d="DAV:"><d:lockscope><d:exclusive/></d:lockscope><d:locktype><d:write/></d:locktype><d:owner>Yaare</d:owner></d:lockinfo>`)
	_, headers, e := h.request(ctx, "LOCK", h.objectURL(""), body, map[string]string{"Depth": "infinity", "Timeout": "Second-300", "Content-Type": "application/xml"})
	if e != nil {
		if unsupportedMethod(e) {
			return "", func() {}, ErrConditions
		}
		return "", func() {}, e
	}
	token := headers.Get("Lock-Token")
	if len(token) < 3 || len(token) > 2048 || !strings.HasPrefix(token, "<") || !strings.HasSuffix(token, ">") {
		return "", func() {}, ErrConditions
	}
	return token, func() {
		unlock, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _, _ = h.request(unlock, "UNLOCK", h.objectURL(""), nil, map[string]string{"Lock-Token": token})
	}, nil
}
func newProbe() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", errors.New("无法创建存储请求。")
	}
	return "probe-" + hex.EncodeToString(b[:]), nil
}
func (h *HTTP) move(ctx context.Context, from, to string, overwrite bool, condition string) error {
	headers := map[string]string{"Destination": h.objectURL(to).String(), "Overwrite": "F"}
	if overwrite {
		headers["Overwrite"] = "T"
	}
	if condition != "" {
		headers["If"] = condition
	}
	_, _, e := h.request(ctx, "MOVE", h.objectURL(from), nil, headers)
	return e
}
func (h *HTTP) Delete(ctx context.Context, name string) error {
	if !ValidName(name) || name == CurrentObject {
		return errors.New("不能删除当前同步配置。")
	}
	_, _, e := h.request(ctx, "DELETE", h.objectURL(name), nil, nil)
	if errors.Is(e, ErrMissing) {
		return nil
	}
	return e
}

func (h *HTTP) Prepare(ctx context.Context, probe string, sync bool) error {
	if !ValidName(probe) || !strings.HasPrefix(probe, "probe-") {
		return errors.New("测试对象无效。")
	}
	if h.cfg.Provider == "webdav" {
		u := *h.base
		for _, part := range append(strings.Split(strings.Trim(h.cfg.Prefix, "/"), "/"), "backups") {
			if part == "" {
				continue
			}
			u.Path = strings.TrimRight(u.Path, "/") + "/" + part + "/"
			_, _, e := h.request(ctx, "MKCOL", &u, nil, nil)
			if e != nil && !errors.Is(e, storageStatus(http.StatusMethodNotAllowed)) {
				return e
			}
		}
	}
	first, e := h.Put(ctx, probe, []byte("Yaare storage capability check"), "")
	if e != nil {
		return e
	}
	defer h.Delete(context.WithoutCancel(ctx), probe)
	if h.cfg.Provider == "webdav" {
		next, e := newProbe()
		if e != nil {
			return e
		}
		if e = h.move(ctx, probe, next, false, ""); e != nil {
			return e
		}
		defer h.Delete(context.WithoutCancel(ctx), next)
		probe = next
		_, first, e = h.Get(ctx, probe)
		if e != nil {
			return e
		}
	}
	if !sync {
		return nil
	}
	if first == "" || strings.HasPrefix(first, "W/") {
		return ErrConditions
	}
	for _, invalidVersion := range []string{"", `"yaare-stale-etag"`} {
		_, e = h.Put(ctx, probe, []byte("must not replace"), invalidVersion)
		switch {
		case e == ErrChanged:
		case e == nil, unsupportedMethod(e):
			return ErrConditions
		default:
			return e
		}
	}
	_, e = h.Put(ctx, probe, []byte("Yaare storage condition verified"), first)
	if e == ErrChanged || unsupportedMethod(e) {
		return ErrConditions
	}
	return e
}

const (
	maxListedBackups = 4000
	maxListPages     = 100
	maxPruneDeletes  = 500
	staleProbeMaxAge = 10 * time.Minute
)

func (h *HTTP) s3ListPage(ctx context.Context, prefix, token string) ([]Object, bool, string, error) {
	u := *h.base
	if h.s3VirtualHosted() {
		u.Path = strings.TrimRight(u.Path, "/") + "/"
	} else {
		u.Path = strings.TrimRight(u.Path, "/") + "/" + h.cfg.Bucket
	}
	u.RawPath = awsPath(u.Path)
	q := url.Values{"list-type": {"2"}, "prefix": {prefix}, "max-keys": {"1000"}}
	if token != "" {
		q.Set("continuation-token", token)
	}
	u.RawQuery = awsQuery(q)
	raw, _, e := h.request(ctx, "GET", &u, nil, nil)
	if e != nil {
		return nil, false, "", e
	}
	var doc struct {
		Items []struct {
			Key  string
			Size int64
		} `xml:"Contents"`
		More bool   `xml:"IsTruncated"`
		Next string `xml:"NextContinuationToken"`
	}
	if xml.Unmarshal(raw, &doc) != nil {
		return nil, false, "", errors.New("S3 目录响应无效。")
	}
	strip := strings.TrimSuffix(prefix, "backups/")
	items := make([]Object, 0, len(doc.Items))
	for _, o := range doc.Items {
		name := strings.TrimPrefix(o.Key, strip)
		if strings.HasPrefix(o.Key, prefix) && ValidBackupName(name) {
			items = append(items, Object{name, o.Size})
		}
	}
	if doc.More && (doc.Next == "" || doc.Next == token) {
		return nil, false, "", errors.New("S3 目录分页响应无效。")
	}
	return items, doc.More, doc.Next, nil
}

func (h *HTTP) webdavListBackups(ctx context.Context) ([]Object, error) {
	out := []Object{}
	raw, _, e := h.request(ctx, "PROPFIND", h.objectURL("backups/"), []byte(`<?xml version="1.0"?><d:propfind xmlns:d="DAV:"><d:prop><d:getcontentlength/></d:prop></d:propfind>`), map[string]string{"Depth": "1", "Content-Type": "application/xml"})
	if errors.Is(e, ErrMissing) {
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	var doc struct {
		Responses []struct {
			Href  string `xml:"href"`
			Props []struct {
				Prop struct {
					Size int64 `xml:"getcontentlength"`
				} `xml:"prop"`
			} `xml:"propstat"`
		} `xml:"response"`
	}
	if xml.Unmarshal(raw, &doc) != nil {
		return nil, errors.New("WebDAV 目录响应无效。")
	}
	for _, r := range doc.Responses {
		u, e := url.Parse(r.Href)
		if e != nil {
			continue
		}
		name := "backups/" + path.Base(u.Path)
		if ValidBackupName(name) {
			var size int64
			if len(r.Props) > 0 {
				size = r.Props[0].Prop.Size
			}
			out = append(out, Object{name, size})
		}
	}
	return out, nil
}

func (h *HTTP) List(ctx context.Context) ([]Object, error) {
	var out []Object
	if h.cfg.Provider == "yaare" {
		body, _, err := h.request(ctx, "GET", h.objectURL("backups"), nil, nil)
		if err != nil {
			return nil, err
		}
		if json.Unmarshal(body, &out) != nil || len(out) > 1024 {
			return nil, errors.New("历史目录无效。")
		}
		for _, o := range out {
			if !ValidBackupName(o.Name) || o.Size < 0 || o.Size > cloudsync.MaxBlob {
				return nil, errors.New("历史对象无效。")
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
		return out, nil
	}
	if h.cfg.Provider == "webdav" {
		var e error
		out, e = h.webdavListBackups(ctx)
		if e != nil {
			return nil, e
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
		if len(out) > maxListedBackups {
			out = out[:maxListedBackups]
		}
		return out, nil
	}
	prefix := strings.Trim(h.cfg.Prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	prefix += "backups/"
	token := ""
	for page := 0; page < maxListPages; page++ {
		items, more, next, e := h.s3ListPage(ctx, prefix, token)
		if e != nil {
			return nil, e
		}
		out = append(out, items...)
		if len(out) > maxListedBackups {
			copy(out, out[len(out)-maxListedBackups:])
			out = out[:maxListedBackups]
		}
		if !more {
			break
		}
		token = next
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

func (h *HTTP) PruneBackups(ctx context.Context, keepRecent int, cutoff string) error {
	if keepRecent < 1 {
		keepRecent = 1
	}
	deleted := 0
	if h.cfg.Provider == "webdav" || h.cfg.Provider == "yaare" {
		var objects []Object
		var e error
		if h.cfg.Provider == "yaare" {
			objects, e = h.List(ctx)
		} else {
			objects, e = h.webdavListBackups(ctx)
		}
		if e != nil {
			return e
		}
		sort.Slice(objects, func(i, j int) bool { return objects[i].Name > objects[j].Name })
		for i, o := range objects {
			if i == 0 || deleted >= maxPruneDeletes {
				continue
			}
			if strings.TrimPrefix(o.Name, "backups/")[:16] < cutoff || i >= keepRecent {
				if e = h.Delete(ctx, o.Name); e != nil {
					return e
				}
				deleted++
			}
		}
		if h.cfg.Provider == "webdav" {
			h.cleanupStaleProbes(ctx, staleProbeMaxAge)
		}
		return nil
	}
	prefix := strings.Trim(h.cfg.Prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	prefix += "backups/"
	var window []Object
	token := ""
	for page := 0; page < maxListPages; page++ {
		items, more, next, e := h.s3ListPage(ctx, prefix, token)
		if e != nil {
			return e
		}
		window = append(window, items...)
		if len(window) > keepRecent {
			excess := len(window) - keepRecent
			for i := 0; i < excess && deleted < maxPruneDeletes; i++ {
				if e = h.Delete(ctx, window[i].Name); e != nil {
					return e
				}
				deleted++
			}
			window = append(window[:0], window[excess:]...)
			if deleted >= maxPruneDeletes {
				return nil
			}
		}
		if !more {
			break
		}
		token = next
	}
	sort.Slice(window, func(i, j int) bool { return window[i].Name > window[j].Name })
	for i, o := range window {
		if i == 0 || deleted >= maxPruneDeletes {
			continue
		}
		if strings.TrimPrefix(o.Name, "backups/")[:16] < cutoff || i >= keepRecent {
			if e := h.Delete(ctx, o.Name); e != nil {
				return e
			}
			deleted++
		}
	}
	return nil
}

func (h *HTTP) cleanupStaleProbes(ctx context.Context, olderThan time.Duration) {
	raw, _, e := h.request(ctx, "PROPFIND", h.objectURL(""), []byte(`<?xml version="1.0"?><d:propfind xmlns:d="DAV:"><d:prop><d:getlastmodified/></d:prop></d:propfind>`), map[string]string{"Depth": "1", "Content-Type": "application/xml"})
	if e != nil {
		return
	}
	var doc struct {
		Responses []struct {
			Href  string `xml:"href"`
			Props []struct {
				Prop struct {
					Modified string `xml:"getlastmodified"`
				} `xml:"prop"`
			} `xml:"propstat"`
		} `xml:"response"`
	}
	if xml.Unmarshal(raw, &doc) != nil {
		return
	}
	now := time.Now().UTC()
	cleaned := 0
	for _, r := range doc.Responses {
		if cleaned >= 64 {
			break
		}
		u, e := url.Parse(r.Href)
		if e != nil {
			continue
		}
		name := path.Base(u.Path)
		if !strings.HasPrefix(name, "probe-") || !ValidName(name) {
			continue
		}
		var modified string
		for _, prop := range r.Props {
			if prop.Prop.Modified != "" {
				modified = strings.TrimSpace(prop.Prop.Modified)
				break
			}
		}
		if modified == "" {
			continue
		}
		ts, err := http.ParseTime(modified)
		if err != nil || now.Sub(ts) < olderThan {
			continue
		}
		_ = h.Delete(ctx, name)
		cleaned++
	}
}

func NewInstanceClient(c Config, credential []byte, t http.RoundTripper) (*HTTP, error) {
	h, e := NewHTTP(c, credential)
	if e != nil {
		return nil, e
	}
	h.client.Transport = t
	return h, nil
}
