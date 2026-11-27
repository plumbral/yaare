package sshclient

import (
	"strings"
	"time"
	"unicode"
)

type AuthPrompt struct {
	Text string `json:"text"`
	Echo bool   `json:"echo"`
}
type AuthChallenge struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Instruction string       `json:"instruction"`
	Prompts     []AuthPrompt `json:"prompts"`
	Address     string       `json:"address"`
	HostName    string       `json:"hostName"`
	Kind        string       `json:"kind"`
}

func promptText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, text)
}
func (s *Session) askAuthentication(name, instruction string, questions []string, echos []bool) ([][]byte, error) {
	if len(questions) > 16 || len(questions) != len(echos) || len(name) > 4096 || len(instruction) > 16384 {
		return nil, fault("AUTH_CHALLENGE_INVALID", "服务器认证请求超出支持范围。")
	}
	if len(questions) == 0 && strings.TrimSpace(promptText(name)) == "" && strings.TrimSpace(promptText(instruction)) == "" {
		return [][]byte{}, nil
	}
	challenge := AuthChallenge{ID: newID(), Name: promptText(name), Instruction: promptText(instruction), Prompts: []AuthPrompt{}, Address: s.authAddress, HostName: s.authName, Kind: s.authKind}
	if challenge.ID == "" {
		return nil, fault("AUTH_FAILED", "无法创建认证请求。")
	}
	for i, q := range questions {
		if len(q) > 4096 {
			return nil, fault("AUTH_CHALLENGE_INVALID", "服务器认证提示过长。")
		}
		challenge.Prompts = append(challenge.Prompts, AuthPrompt{promptText(q), echos[i]})
	}
	answer := make(chan [][]byte, 1)
	s.mu.Lock()
	s.pendingAuth = &challenge
	s.authAnswer = answer
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.pendingAuth = nil
		s.authAnswer = nil
		s.mu.Unlock()
		select {
		case values := <-answer:
			for _, value := range values {
				clear(value)
			}
		default:
		}
	}()
	if err := s.send(Event{Type: "authentication", Authentication: &challenge}); err != nil {
		return nil, err
	}
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	select {
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	case <-timer.C:
		return nil, fault("AUTH_TIMEOUT", "交互认证已超时，请重新连接。")
	case values := <-answer:
		return values, nil
	}
}
func (s *Session) AnswerAuthentication(id string, answers []string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingAuth == nil || s.pendingAuth.ID != id || s.ctx.Err() != nil || len(answers) != len(s.pendingAuth.Prompts) {
		return false
	}
	for _, a := range answers {
		if len(a) > 16384 {
			return false
		}
	}
	values := make([][]byte, len(answers))
	for i := range answers {
		values[i] = []byte(answers[i])
	}
	select {
	case s.authAnswer <- values:
		s.pendingAuth = nil
		return true
	default:
		for _, value := range values {
			clear(value)
		}
		return false
	}
}
