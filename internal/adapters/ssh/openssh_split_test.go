package sshclient

import (
	"fmt"
	"strings"
	"testing"
)

func TestRealOpenSSHEightIndependentTerminalsResizeAndClose(t *testing.T) {
	runs := make([]*runResult, 8)
	for i := range runs {
		runs[i] = realFixture(t, "yaaretest")
		runs[i].wait(t, "connected")
	}
	for i, r := range runs {
		cols, rows := 48+i*7, 12+i*3
		r.session.Resize(cols, rows)
		command := fmt.Sprintf("printf '\\033[2J\\033[H'; printf 'SPLIT_%%s_READY\\n' '%d'; stty size\n", i)
		if !r.session.Write([]byte(command)) {
			t.Fatal("terminal rejected input", i)
		}
		want := fmt.Sprintf("%d %d", rows, cols)
		eventually(t, func() bool {
			return strings.Contains(r.text(), want) && strings.Contains(r.text(), fmt.Sprintf("SPLIT_%d_READY", i))
		})
	}
	for i, r := range runs {
		for j := range runs {
			if i != j && strings.Contains(r.text(), fmt.Sprintf("SPLIT_%d_READY", j)) {
				t.Fatal("cross-terminal output", i, j)
			}
		}
	}
	runs[3].session.Close()
	runs[3].ended(t)
	for i, r := range runs {
		if i == 3 {
			continue
		}
		if !r.session.Write([]byte("printf 'STILL_%s_ALIVE\\n' '" + fmt.Sprint(i) + "'\n")) {
			t.Fatal("closing sibling disconnected another terminal", i)
		}
		eventually(t, func() bool { return strings.Contains(r.text(), fmt.Sprintf("STILL_%d_ALIVE", i)) })
	}
	t.Log("Eight verified SSH sessions have independent input, PTY dimensions and output; closing one leaves seven responsive.")
}
