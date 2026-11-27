package secret

import "bytes"

type Bytes []byte

func (b Bytes) MarshalText() ([]byte, error) { return b, nil }

func (b *Bytes) UnmarshalText(text []byte) error {
	value := bytes.Clone(text)
	b.Clear()
	*b = value
	return nil
}

func (b *Bytes) Clear() {
	clear(*b)
	*b = nil
}
