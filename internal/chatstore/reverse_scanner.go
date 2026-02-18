package chatstore

import (
	"bufio"
	"bytes"
	"io"
)

type ReverseScanner struct {
	r            io.ReadSeeker
	pos          int64
	buf          []byte
	bufSize      int
	maxSize      int
	fileSize     int64
	pending      [][]byte
	leftover     [][]byte // chunks of the current line being built (from right to left)
	leftoverSize int
	err          error
	line         []byte
}

func NewReverseScanner(r io.ReadSeeker, bufSize, maxSize int) (*ReverseScanner, error) {
	currentPos, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	if bufSize < 4096 {
		bufSize = 4096
	}
	// Cap bufSize to maxSize to avoid reading more than needed for a single line check
	// actually bufSize is just read buffer.
	return &ReverseScanner{
		r:        r,
		pos:      currentPos,
		fileSize: currentPos,
		bufSize:  bufSize,
		maxSize:  maxSize,
		buf:      make([]byte, bufSize),
	}, nil
}

func (s *ReverseScanner) Scan() bool {
	if len(s.pending) > 0 {
		s.line = s.pending[0]
		s.pending = s.pending[1:]
		return true
	}

	if s.err != nil {
		return false
	}

	// Loop until we find at least one line or hit EOF/error
	for {
		// If we are at start of file, process remaining leftover
		if s.pos == 0 {
			if s.leftoverSize > 0 {
				if s.leftoverSize > s.maxSize {
					s.err = bufio.ErrTooLong
					return false
				}
				s.line = s.dropCR(s.joinLeftover(nil))
				s.leftover = nil
				s.leftoverSize = 0
				return true
			}
			return false
		}

		// Calculate size to read
		readSize := s.bufSize
		if int64(readSize) > s.pos {
			readSize = int(s.pos)
		}

		// Read chunk
		s.pos -= int64(readSize)
		if _, err := s.r.Seek(s.pos, io.SeekStart); err != nil {
			s.err = err
			return false
		}

		n, err := io.ReadFull(s.r, s.buf[:readSize])
		if err != nil {
			s.err = err
			return false
		}
		chunk := s.buf[:n]

		// Process chunk from right to left
		right := len(chunk)
		for {
			idx := bytes.LastIndexByte(chunk[:right], '\n')
			if idx == -1 {
				// No more newlines in this chunk.
				// The remaining part `chunk[:right]` belongs to the current line being built.
				remaining := chunk[:right]
				if len(remaining) > 0 {
					// We need to copy `remaining` because `s.buf` will be overwritten next read.
					dup := make([]byte, len(remaining))
					copy(dup, remaining)
					s.leftover = append(s.leftover, dup)
					s.leftoverSize += len(remaining)
					if s.leftoverSize > s.maxSize {
						s.err = bufio.ErrTooLong
						return false
					}
				}
				break // Go read next chunk
			}

			// Found a newline at `idx`.
			// The part `chunk[idx+1:right]` is the start of the line we were building in `leftover`.
			// OR if `leftover` is empty, it's a complete line inside the chunk.
			linePart := chunk[idx+1 : right]

			// Check if this is the trailing newline of the file.
			// If we are at EOF (s.pos + n == fileSize) AND this is the last char (idx == n-1)
			// AND we haven't scanned anything else to the right (right == n)
			// AND we have no leftover (s.leftoverSize == 0)
			// Then it's just a trailing newline, so we skip yielding the empty line after it.
			if s.pos+int64(n) == s.fileSize && right == n && idx == n-1 && s.leftoverSize == 0 {
				right = idx
				continue
			}

			// If we have leftover parts, they are the suffix of this line.
			// `linePart` is the prefix found in this chunk.
			// Full line = linePart + join(leftover in reverse)

			fullLine := s.joinLeftover(linePart)
			s.pending = append(s.pending, s.dropCR(fullLine))

			// Reset leftover
			s.leftover = nil
			s.leftoverSize = 0

			// Move `right` to `idx` to continue searching in the left part
			right = idx
		}

		// If we found lines, return the first one
		if len(s.pending) > 0 {
			s.line = s.pending[0]
			s.pending = s.pending[1:]
			return true
		}
	}
}

// joinLeftover constructs the full line from `prefix` and `s.leftover` (reversed).
// s.leftover contains [suffix_part_1, suffix_part_2, ...] where part_1 is effectively to the RIGHT of prefix,
// but since we filled `leftover` as we went backwards, `leftover[0]` is the rightmost part found first?
// Wait.
// Read Chunk 1 (End of file). `...B`. Added `B` to `leftover`. `leftover=[B]`.
// Read Chunk 2 (Before Chunk 1). `A...`. Found newline. `A` is `linePart`.
// Full line is `A` + `B`.
// So `prefix` comes first.
// `leftover` chunks are added in order of reading (right to left).
// `leftover[0]` is `B` (end of line).
// `leftover[1]` is `...` (middle of line).
// So correct order is `prefix` + `leftover[last]` + ... + `leftover[0]`.
func (s *ReverseScanner) joinLeftover(prefix []byte) []byte {
	size := len(prefix) + s.leftoverSize
	buf := make([]byte, size)
	copy(buf, prefix)
	pos := len(prefix)
	for i := len(s.leftover) - 1; i >= 0; i-- {
		copy(buf[pos:], s.leftover[i])
		pos += len(s.leftover[i])
	}
	return buf
}

func (s *ReverseScanner) dropCR(data []byte) []byte {
	if len(data) > 0 && data[len(data)-1] == '\r' {
		return data[:len(data)-1]
	}
	return data
}

func (s *ReverseScanner) Bytes() []byte {
	return s.line
}

func (s *ReverseScanner) Text() string {
	return string(s.line)
}

func (s *ReverseScanner) Err() error {
	return s.err
}
