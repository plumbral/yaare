package workspace

import (
	"net"
	"strconv"
	"strings"
)

type ForwardRule struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	ListenAddress string `json:"listenAddress"`
	ListenPort    int    `json:"listenPort"`
	TargetAddress string `json:"targetAddress"`
	TargetPort    int    `json:"targetPort"`
	Enabled       bool   `json:"enabled"`
}

func ValidateForwards(rules []ForwardRule) error {
	if len(rules) > 32 {
		return Fail("FORWARD_LIMIT", "每个连接最多保存 32 条转发规则。")
	}
	ids, listeners := map[string]bool{}, map[string]bool{}
	for _, r := range rules {
		if !idPattern.MatchString(r.ID) || ids[r.ID] || !cleanText(r.Name, 64) {
			return Fail("INVALID_FORWARD", "请填写有效且不重复的转发规则编号和名称。")
		}
		ids[r.ID] = true
		if r.Kind != "local" && r.Kind != "remote" && r.Kind != "dynamic" {
			return Fail("INVALID_FORWARD", "请选择本地、远程或动态转发。")
		}
		if (net.ParseIP(r.ListenAddress) == nil && r.ListenAddress != "localhost") || r.ListenPort < 1 || r.ListenPort > 65535 {
			return Fail("INVALID_FORWARD", "监听地址应为 IP 或 localhost，端口应为 1–65535。")
		}
		if r.Kind == "dynamic" {
			if r.TargetAddress != "" || r.TargetPort != 0 {
				return Fail("INVALID_FORWARD", "动态转发不应指定固定目标。")
			}
		} else if !addressPattern.MatchString(r.TargetAddress) || r.TargetPort < 1 || r.TargetPort > 65535 {
			return Fail("INVALID_FORWARD", "请检查转发目标地址和端口。")
		}
		scope := "local"
		if r.Kind == "remote" {
			scope = "remote"
		}
		key := scope + ":" + net.JoinHostPort(r.ListenAddress, strconv.Itoa(r.ListenPort))
		if r.Enabled && listeners[key] {
			return Fail("FORWARD_CONFLICT", "启用的转发规则不能使用同一监听地址和端口。")
		}
		if r.Enabled {
			listeners[key] = true
		}
	}
	return nil
}

func ValidateNetworks(hosts []Host) error {
	byID := map[string]Host{}
	for _, h := range hosts {
		byID[h.ID] = h
	}
	for _, h := range hosts {
		if err := ValidateProxy(h.Proxy); err != nil {
			return err
		}
		if h.TimeoutSeconds != 0 && (h.TimeoutSeconds < 3 || h.TimeoutSeconds > 120) {
			return Fail("INVALID_TIMEOUT", "连接超时应为 3–120 秒。")
		}
		if len(h.JumpHostIDs) > 8 {
			return Fail("JUMP_LIMIT", "最多使用 8 台跳板机。")
		}
		seen := map[string]bool{h.ID: true}
		for _, id := range h.JumpHostIDs {
			if !idPattern.MatchString(id) || seen[id] {
				return Fail("JUMP_CYCLE", "跳板链路不能包含自身或重复主机。")
			}
			if _, ok := byID[id]; !ok {
				return Fail("JUMP_UNAVAILABLE", "跳板机配置已不存在，请重新选择。")
			}
			seen[id] = true
		}
		if err := ValidateForwards(h.Forwards); err != nil {
			return err
		}
	}
	state := map[string]int{}
	var visit func(string, int) error
	visit = func(id string, depth int) error {
		if state[id] == 1 {
			return Fail("JUMP_CYCLE", "跳板机配置不能形成循环引用。")
		}
		if depth > 64 {
			return Fail("JUMP_LIMIT", "跳板引用层级过深。")
		}
		if state[id] == 2 {
			return nil
		}
		state[id] = 1
		for _, next := range byID[id].JumpHostIDs {
			if err := visit(next, depth+1); err != nil {
				return err
			}
		}
		state[id] = 2
		return nil
	}
	for _, h := range hosts {
		if err := visit(h.ID, 0); err != nil {
			return err
		}
	}
	return nil
}

func ForwardAddress(r ForwardRule) string {
	return net.JoinHostPort(strings.Trim(r.ListenAddress, "[]"), strconv.Itoa(r.ListenPort))
}
