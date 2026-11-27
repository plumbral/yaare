package webserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"yaare/internal/desktop/ipc"
)

type input struct {
	Type, ID, Method string
	Args             []json.RawMessage
	Seq              uint64
}
type outgoing struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Seq   uint64 `json:"seq,omitempty"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}
type runningCall struct {
	cancel context.CancelFunc
	ack    chan uint64
}

func (s *Server) websocket(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		http.Error(w, "请求来源无效。", 403)
		return
	}
	key, session := s.auth.session(r)
	if session == nil {
		http.Error(w, "请先登录。", 401)
		return
	}
	protocols := strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",")
	valid := false
	for _, p := range protocols {
		if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(p)), []byte(session.csrf)) == 1 {
			valid = true
		}
	}
	if !valid {
		http.Error(w, "登录验证已变化，请重新打开工作区。", 403)
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), session.expires)
	defer cancel()
	number := s.number.Add(1)
	s.auth.mu.Lock()
	if s.closing || s.auth.sessions[key] != session || time.Now().After(session.expires) || len(session.cancels) >= 8 {
		s.auth.mu.Unlock()
		http.Error(w, "打开的工作区较多，请先关闭其他页面。", 429)
		return
	}
	s.connections.Add(1)
	defer s.connections.Done()
	session.cancels[number] = cancel
	s.auth.mu.Unlock()
	defer func() { s.auth.mu.Lock(); delete(session.cancels, number); s.auth.mu.Unlock() }()
	conn, e := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"yaare.v1"}, CompressionMode: websocket.CompressionDisabled})
	if e != nil {
		return
	}
	defer conn.CloseNow()
	if conn.Subprotocol() != "yaare.v1" {
		_ = conn.Close(websocket.StatusPolicyViolation, "协议不匹配")
		return
	}
	conn.SetReadLimit(2 << 20)
	owner := ipc.NewBrowserOwner(ctx)
	defer s.services.Release(owner)
	var write sync.Mutex
	send := func(msg outgoing) error {
		write.Lock()
		defer write.Unlock()
		bounded, stop := context.WithTimeout(ctx, 15*time.Second)
		defer stop()
		return wsjson.Write(bounded, conn, msg)
	}
	var mu sync.Mutex
	calls := map[string]runningCall{}
	var tasks sync.WaitGroup
	defer func() {
		cancel()
		mu.Lock()
		for _, call := range calls {
			call.cancel()
		}
		mu.Unlock()
		tasks.Wait()
	}()
	var last uint64
	for {
		var in input
		if e = wsjson.Read(ctx, conn, &in); e != nil {
			return
		}
		switch in.Type {
		case "ack":
			mu.Lock()
			call, ok := calls[in.ID]
			mu.Unlock()
			if ok {
				select {
				case call.ack <- in.Seq:
				default:
				}
			}
			continue
		case "cancel":
			mu.Lock()
			call, ok := calls[in.ID]
			mu.Unlock()
			if ok {
				call.cancel()
			}
			continue
		case "call":
		default:
			_ = conn.Close(websocket.StatusPolicyViolation, "请求无效")
			return
		}
		id, e := strconv.ParseUint(in.ID, 10, 53)
		if e != nil || id <= last {
			_ = conn.Close(websocket.StatusPolicyViolation, "请求编号无效")
			return
		}
		last = id
		mu.Lock()
		if len(calls) >= 48 {
			mu.Unlock()
			_ = send(outgoing{Type: "result", ID: in.ID, Error: "当前操作较多，请稍后重试。"})
			continue
		}
		callCtx, stop := context.WithCancel(owner)
		call := runningCall{cancel: stop, ack: make(chan uint64, 1)}
		calls[in.ID] = call
		mu.Unlock()
		tasks.Add(1)
		go func(in input, call runningCall) {
			defer tasks.Done()
			defer call.cancel()
			defer func() { mu.Lock(); delete(calls, in.ID); mu.Unlock() }()
			var value any
			var err error
			if ipc.BrowserStream(in.Method) {
				var serial sync.Mutex
				var sequence uint64
				err = s.services.Stream(callCtx, in.Method, in.Args, func(data any) error {
					serial.Lock()
					defer serial.Unlock()
					sequence++
					if e := send(outgoing{Type: "event", ID: in.ID, Seq: sequence, Data: data}); e != nil {
						return e
					}
					timer := time.NewTimer(30 * time.Second)
					defer timer.Stop()
					for {
						select {
						case ack := <-call.ack:
							if ack == sequence {
								return nil
							}
						case <-callCtx.Done():
							return callCtx.Err()
						case <-timer.C:
							return errors.New("页面接收超时，通道已停止。")
						}
					}
				})
			} else if in.Method == "BrowserBackup.Export" || in.Method == "BrowserBackup.RestorePoint" {
				var file *ipc.BrowserDownload
				file, err = s.services.PrepareBrowserArchive(callCtx, in.Method, in.Args)
				if err == nil {
					value, err = s.issueDownload(owner, key, file)
				}
			} else if in.Method == "BrowserFiles.Download" {
				var task string
				if len(in.Args) != 1 || json.Unmarshal(in.Args[0], &task) != nil {
					err = errors.New("请选择已完成的下载任务。")
				} else {
					value, err = s.prepareDownload(owner, key, task)
				}
			} else {
				value, err = s.services.Call(callCtx, in.Method, in.Args)
			}
			result := outgoing{Type: "result", ID: in.ID, Data: value}
			if err != nil {
				result.Error = err.Error()
			}
			_ = send(result)
		}(in, call)
	}
}
