package files

import (
	"context"
	"io"
	"sync"

	"github.com/pkg/sftp"
)

const transferChunk = 32 << 10
const downloadWindow = 8
const uploadWindow = 8
const sourceChanged = "源文件在传输期间发生变化，未提交目标文件。"

func localCopyTarget(dest FS) bool {
	switch target := dest.(type) {
	case Local:
		return true
	case reviewedDestination:
		return localCopyTarget(target.FS)
	default:
		return false
	}
}

func pipelinedReadAt(input io.Reader) (io.ReaderAt, func([]byte), bool) {
	switch r := input.(type) {
	case *sftp.File:
		return r, nil, true
	case *reviewedReader:
		if file, ok := r.ReadCloser.(*sftp.File); ok {
			return file, r.recordChunk, true
		}
	}
	return nil, nil, false
}

func pipelinedWriteAt(output io.Writer) (io.WriterAt, bool) {
	switch w := output.(type) {
	case remoteWriter:
		return w, true
	case *remoteWriter:
		return w, true
	case *sftp.File:
		return w, true
	default:
		return nil, false
	}
}

func streamWrite(ctx context.Context, input io.Reader, output io.Writer, dest FS, offset, size int64, probeEOF bool, onAck func([]byte)) error {
	remaining := size - offset
	if writerAt, ok := pipelinedWriteAt(output); ok && remaining > transferChunk {
		return writeUploadWindow(ctx, input, writerAt, offset, size, probeEOF, onAck)
	}
	return streamCopy(ctx, input, dest, offset, size, probeEOF, func(data []byte) error {
		count, writeErr := output.Write(data)
		if count > 0 {
			onAck(data[:count])
		}
		if writeErr == nil && count != len(data) {
			writeErr = io.ErrShortWrite
		}
		return writeErr
	})
}

func streamCopy(ctx context.Context, input io.Reader, dest FS, offset, size int64, probeEOF bool, write func([]byte) error) error {
	remaining := size - offset
	readUntil := size
	if probeEOF {
		readUntil++
	}
	readerAt, onReadChunk, canPipelineRead := pipelinedReadAt(input)
	accept := func(data []byte, err error) error {
		if cancelled := ctx.Err(); cancelled != nil {
			return cancelled
		}
		if int64(len(data)) > remaining {
			return problem(sourceChanged)
		}
		if len(data) != 0 {
			if onReadChunk != nil {
				onReadChunk(data)
			}
			if writeErr := write(data); writeErr != nil {
				return writeErr
			}
			remaining -= int64(len(data))
		}
		if err == io.EOF {
			if remaining != 0 {
				return problem(sourceChanged)
			}
			return nil
		}
		if err == nil && len(data) == 0 {
			return io.ErrNoProgress
		}
		return err
	}
	if canPipelineRead && localCopyTarget(dest) && remaining > transferChunk {
		return readDownloadWindow(ctx, readerAt, offset, readUntil, accept)
	}
	onReadChunk = nil
	buffer := make([]byte, transferChunk)
	for offset < readUntil {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, readErr := input.Read(buffer[:min(int64(len(buffer)), readUntil-offset)])
		if err := accept(buffer[:count], readErr); err != nil {
			return err
		}
		if readErr == io.EOF {
			return nil
		}
		offset += int64(count)
	}
	return nil
}

func writeUploadWindow(ctx context.Context, input io.Reader, output io.WriterAt, offset, size int64, probeEOF bool, onAck func([]byte)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var pending sync.WaitGroup
	defer pending.Wait()
	var slots [uploadWindow]struct {
		data  []byte
		count int
		done  chan error
	}
	next := offset
	var readErr error
	schedule := func(index int) bool {
		slot := &slots[index]
		want := int(min(int64(transferChunk), size-next))
		if want <= 0 {
			return false
		}
		filled := 0
		for filled < want {
			if err := ctx.Err(); err != nil {
				readErr = err
				return false
			}
			n, err := input.Read(slot.data[filled:want])
			filled += n
			if err == io.EOF {
				if filled < want {
					readErr = problem(sourceChanged)
					return false
				}
				break
			}
			if err != nil {
				readErr = err
				return false
			}
			if n == 0 {
				readErr = io.ErrNoProgress
				return false
			}
		}
		start := next
		next += int64(filled)
		slot.count = filled
		pending.Go(func() {
			if err := ctx.Err(); err != nil {
				slot.done <- err
				return
			}
			n, err := output.WriteAt(slot.data[:filled], start)
			if err == nil && n != filled {
				err = io.ErrShortWrite
			}
			slot.done <- err
		})
		return true
	}
	active := 0
	for next < size && active < len(slots) && readErr == nil && ctx.Err() == nil {
		slots[active].data = make([]byte, transferChunk)
		slots[active].done = make(chan error, 1)
		if !schedule(active) {
			break
		}
		active++
	}
	for index := 0; active != 0; index = (index + 1) % len(slots) {
		slot := &slots[index]
		select {
		case <-ctx.Done():
			return ctx.Err()
		case writeErr := <-slot.done:
			if writeErr != nil {
				return writeErr
			}
			onAck(slot.data[:slot.count])
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if next < size && readErr == nil && ctx.Err() == nil {
			if !schedule(index) {
				active--
			}
		} else {
			active--
		}
	}
	if readErr != nil {
		return readErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if probeEOF {
		var probe [1]byte
		for {
			n, err := input.Read(probe[:])
			if n > 0 {
				return problem(sourceChanged)
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			return io.ErrNoProgress
		}
	}
	return nil
}

type downloadResult struct {
	count int
	err   error
}

func readDownloadWindow(ctx context.Context, input io.ReaderAt, offset, end int64, consume func([]byte, error) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var pending sync.WaitGroup
	defer pending.Wait()
	var slots [downloadWindow]struct {
		data []byte
		done chan downloadResult
	}
	next := offset
	schedule := func(index int) {
		slot := &slots[index]
		start := next
		data := slot.data[:min(transferChunk, end-start)]
		next += int64(len(data))
		pending.Go(func() {
			if err := ctx.Err(); err != nil {
				slot.done <- downloadResult{err: err}
				return
			}
			count, err := input.ReadAt(data, start)
			slot.done <- downloadResult{count, err}
		})
	}
	active := 0
	for next < end && active < len(slots) {
		slots[active].data = make([]byte, transferChunk)
		slots[active].done = make(chan downloadResult, 1)
		schedule(active)
		active++
	}
	for index := 0; active != 0; index = (index + 1) % len(slots) {
		slot := &slots[index]
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result := <-slot.done:
			if err := consume(slot.data[:result.count], result.err); err != nil {
				return err
			}
			if result.err == io.EOF {
				return nil
			}
		}
		if next < end && ctx.Err() == nil {
			schedule(index)
		} else {
			active--
		}
	}
	return ctx.Err()
}
