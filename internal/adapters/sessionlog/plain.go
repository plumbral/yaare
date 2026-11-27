package sessionlog

import "unicode/utf8"

type plainText struct {
	mode    byte
	escaped bool
	pending []byte
	line    []rune
	cursor  int
}

func (p *plainText) feed(data []byte, emit func(string)) {
	p.pending = append(p.pending, data...)
	consumed := 0
	for consumed < len(p.pending) {
		rest := p.pending[consumed:]
		if !utf8.FullRune(rest) {
			break
		}
		r, n := utf8.DecodeRune(rest)
		consumed += n
		switch p.mode {
		case 1:
			switch r {
			case '[':
				p.mode = 2
			case ']', 'P', '^', '_', 'X':
				p.mode = 3
			default:
				p.mode = 0
			}
			continue
		case 2:
			if r >= 0x40 && r <= 0x7e {
				if r == 'K' && p.cursor < len(p.line) {
					p.line = p.line[:p.cursor]
				}
				p.mode = 0
			}
			continue
		case 3:
			if r == 7 || p.escaped && r == '\\' {
				p.mode = 0
				p.escaped = false
			} else {
				p.escaped = r == 27
			}
			continue
		}
		switch r {
		case 27:
			p.mode = 1
		case '\r':
			p.cursor = 0
		case '\b':
			if p.cursor > 0 {
				p.cursor--
			}
		case '\n':
			emit(string(p.line) + "\n")
			p.line = p.line[:0]
			p.cursor = 0
		default:
			if r < 32 && r != '\t' || r == 127 || r >= 0x80 && r <= 0x9f {
				continue
			}
			if p.cursor < len(p.line) {
				p.line[p.cursor] = r
			} else {
				p.line = append(p.line, r)
			}
			p.cursor++
			if len(p.line) >= 4096 {
				p.flush(emit)
			}
		}
	}
	if consumed > 0 {
		remaining := copy(p.pending, p.pending[consumed:])
		p.pending = p.pending[:remaining]
	}
}
func (p *plainText) flush(emit func(string)) {
	if len(p.line) > 0 {
		emit(string(p.line))
		p.line = nil
		p.cursor = 0
	}
}
