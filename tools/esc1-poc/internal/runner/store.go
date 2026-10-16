// Copyright 2026 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"sort"
	"sync"
)

// answerSpace owns just this run's temporary files. One shared counter prevents
// parallel queries and merge passes from each spending the entire disk budget.
// Files contain graph evidence, so directories and files are private to the user.
type answerSpace struct {
	directory     string
	limits        Limits
	mutex         sync.Mutex
	bytes         int64
	peak          int64
	observeAnswer func(string, map[string]any) error
	// inspectTestAnswer lets the editor read the tested file's complete answer
	// before temporary files are removed. Normal analysis leaves it empty.
	inspectTestAnswer func(context.Context, Transaction, string, CompleteAnswer) error
	testTarget        string
}

type answerFile struct {
	space    *answerSpace
	path     string
	bytes    int64
	rows     int64
	checksum []byte
}

type answerWriter struct {
	answer    *answerFile
	file      *os.File
	buffer    *bufio.Writer
	rowBuffer bytes.Buffer
	encoder   *gob.Encoder
	checksum  hash.Hash
}

func newAnswerSpace(parent string, limits Limits) (*answerSpace, error) {
	directory, err := os.MkdirTemp(parent, "calculation-")
	if err != nil {
		return nil, err
	}
	return &answerSpace{directory: directory, limits: limits}, nil
}

func (s *answerSpace) close() { _ = os.RemoveAll(s.directory) }

func (s *answerSpace) create() (*answerWriter, error) {
	file, err := os.CreateTemp(s.directory, "answers-")
	if err != nil {
		return nil, err
	}
	answer := &answerFile{space: s, path: file.Name()}
	checksum := sha256.New()
	writer := &answerWriter{answer: answer, file: file, buffer: bufio.NewWriterSize(io.MultiWriter(file, checksum), 64*1024), checksum: checksum}
	writer.encoder = gob.NewEncoder(&writer.rowBuffer)
	return writer, nil
}

func (s *answerWriter) append(row map[string]any) error {
	if err := validateRows(Rows{row}); err != nil {
		// Rows arrive one at a time, so number the problem by its place in
		// the whole answer rather than within this single row.
		var problem *AnswerValueError
		if errors.As(err, &problem) {
			problem.Row = s.answer.rows + 1
		}
		return err
	}
	// The driver has already supplied this row. Reject clearly oversized values
	// before asking Gob to allocate another complete encoded copy of them.
	remaining := s.answer.space.limits.RowBytes
	if !fitsRowBudget(row, &remaining) {
		return fmt.Errorf("one answer exceeds the %d-byte row limit", s.answer.space.limits.RowBytes)
	}
	s.rowBuffer.Reset()
	if err := s.encoder.Encode(row); err != nil {
		return err
	}
	encoded := s.rowBuffer.Bytes()
	size := int64(len(encoded)) + 8
	if size > s.answer.space.limits.RowBytes {
		return fmt.Errorf("one answer exceeds the %d-byte row limit", s.answer.space.limits.RowBytes)
	}
	if s.answer.rows >= s.answer.space.limits.ResultRows {
		return fmt.Errorf("answer exceeds the %d-row limit", s.answer.space.limits.ResultRows)
	}
	s.answer.space.mutex.Lock()
	if size > s.answer.space.limits.DiskBytes-s.answer.space.bytes {
		s.answer.space.mutex.Unlock()
		return fmt.Errorf("temporary answers exceed the %d-byte disk budget", s.answer.space.limits.DiskBytes)
	}
	s.answer.space.bytes += size
	if s.answer.space.bytes > s.answer.space.peak {
		s.answer.space.peak = s.answer.space.bytes
	}
	s.answer.space.mutex.Unlock()
	s.answer.bytes += size
	s.answer.rows++
	if err := binary.Write(s.buffer, binary.LittleEndian, uint64(len(encoded))); err != nil {
		return err
	}
	_, err := s.buffer.Write(encoded)
	return err
}

// Count a lower bound on the stored bytes without allocating another buffer.
// Gob's own headers add more bytes; append checks the actual size as well.
func fitsRowBudget(value any, remaining *int64) bool {
	var cost int64 = 1
	if text, ok := value.(string); ok {
		cost += int64(len(text))
	}
	if cost > *remaining {
		return false
	}
	*remaining -= cost
	switch value := value.(type) {
	case map[string]any:
		for key, item := range value {
			if !fitsRowBudget(key, remaining) || !fitsRowBudget(item, remaining) {
				return false
			}
		}
	case []any:
		for _, item := range value {
			if !fitsRowBudget(item, remaining) {
				return false
			}
		}
	case []string:
		for _, item := range value {
			if !fitsRowBudget(item, remaining) {
				return false
			}
		}
	}
	return true
}

func (s *answerWriter) finish() (*answerFile, error) {
	if err := s.buffer.Flush(); err != nil {
		_ = s.file.Close()
		return nil, err
	}
	if err := s.file.Close(); err != nil {
		return nil, err
	}
	s.answer.checksum = s.checksum.Sum(nil)
	return s.answer, nil
}

func (s *answerFile) remove() error {
	if err := os.Remove(s.path); err != nil {
		return err
	}
	s.space.mutex.Lock()
	s.space.bytes -= s.bytes
	s.space.mutex.Unlock()
	return nil
}

// Gob is Go's standard binary value format. Unlike a JSON round trip it keeps
// exact integer IDs, decimal numbers, and list types unchanged. Each row has a
// length prefix, checked before allocation, so damaged files fail safely.
// Keep one encoder and decoder per file. Gob learns each Go value type once;
// starting again for every row made even modest result lists painfully slow.
func init() {
	gob.Register(map[string]any{})
	gob.Register([]any{})
	gob.Register([]string{})
}

type answerReader struct {
	file             *os.File
	buffer           *bufio.Reader
	limit            int64
	remaining        int64
	rowBuffer        bytes.Buffer
	decoder          *gob.Decoder
	checksum         hash.Hash
	expectedChecksum []byte
}

func (s *answerFile) open() (*answerReader, error) {
	file, err := os.Open(s.path)
	if err != nil {
		return nil, err
	}
	checksum := sha256.New()
	reader := &answerReader{file: file, buffer: bufio.NewReaderSize(io.TeeReader(file, checksum), 64*1024), limit: s.space.limits.RowBytes, remaining: s.rows, checksum: checksum, expectedChecksum: s.checksum}
	reader.decoder = gob.NewDecoder(&reader.rowBuffer)
	return reader, nil
}

func (s *answerReader) next() (map[string]any, error) {
	var length uint64
	var row map[string]any
	if s.remaining == 0 {
		if _, err := s.buffer.ReadByte(); err == io.EOF {
			// A damaged string can still decode as a valid string. Check the
			// complete file's fingerprint before treating any read as successful.
			// Publication batches remain uncommitted until this check passes.
			if !bytes.Equal(s.checksum.Sum(nil), s.expectedChecksum) {
				return nil, fmt.Errorf("temporary answer checksum does not match; stored evidence changed")
			}
			return nil, io.EOF
		} else if err != nil {
			return nil, fmt.Errorf("check complete temporary answer: %w", err)
		}
		return nil, fmt.Errorf("temporary answer contains unexpected extra data")
	}
	if err := binary.Read(s.buffer, binary.LittleEndian, &length); err != nil {
		return nil, fmt.Errorf("temporary answer ended before its promised row count: %w", err)
	}
	if length > uint64(s.limit) {
		return nil, fmt.Errorf("temporary answer contains an oversized row")
	}
	encoded := make([]byte, int(length))
	if _, err := io.ReadFull(s.buffer, encoded); err != nil {
		return nil, fmt.Errorf("read complete temporary row: %w", err)
	}
	s.rowBuffer.Reset()
	_, _ = s.rowBuffer.Write(encoded)
	if err := s.decoder.Decode(&row); err != nil {
		return nil, fmt.Errorf("decode temporary row: %w", err)
	}
	if s.rowBuffer.Len() != 0 {
		return nil, fmt.Errorf("temporary row contains unexpected extra data")
	}
	for column, value := range row {
		row[column] = restoreEmptyLists(value)
	}
	s.remaining--
	return row, nil
}

// Gob decodes an empty list as a typed nil slice. JSON would turn that slice
// into null, changing its meaning when sent to PostgreSQL or another query.
// Restore empty lists throughout the freshly decoded row. A genuine null has
// no slice type and stays nil. This also preserves lists nested inside maps.
func restoreEmptyLists(value any) any {
	switch value := value.(type) {
	case []any:
		if value == nil {
			return []any{}
		}
		for index, item := range value {
			value[index] = restoreEmptyLists(item)
		}
	case []string:
		if value == nil {
			return []string{}
		}
	case map[string]any:
		for key, item := range value {
			value[key] = restoreEmptyLists(item)
		}
	}
	return value
}

func (s *answerFile) each(ctx context.Context, consume func(map[string]any) error) error {
	reader, err := s.open()
	if err != nil {
		return err
	}
	defer reader.file.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		row, err := reader.next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := consume(row); err != nil {
			return err
		}
	}
}

// A row-count limit alone is not enough: 500 unusually long IDs or property
// maps can fill a large amount of memory. Every batch also shares the query
// parameter allowance. A later read/write failure still fails the whole run.
func (s *answerFile) batches(ctx context.Context, extraBytesPerRow int64, consume func(Rows) error) error {
	var rows = Rows{}
	// Leave room for parameter names and the surrounding map/list. Publishers
	// also reserve per-row room for database-generated IDs.
	const envelopeBytes int64 = 64
	var bytes = envelopeBytes
	flush := func() error {
		if len(rows) == 0 {
			return nil
		}
		if err := consume(rows); err != nil {
			return err
		}
		rows = Rows{}
		bytes = envelopeBytes
		return nil
	}
	if err := s.each(ctx, func(row map[string]any) error {
		size := encodedSize(row) + extraBytesPerRow
		if size > s.space.limits.ParameterBytes-envelopeBytes {
			return fmt.Errorf("one publication row exceeds the parameter budget")
		}
		if len(rows) >= s.space.limits.BatchRows || size > s.space.limits.ParameterBytes-bytes {
			if err := flush(); err != nil {
				return err
			}
		}
		rows = append(rows, row)
		bytes += size
		return nil
	}); err != nil {
		return err
	}
	return flush()
}

func (s *answerFile) load(ctx context.Context, byteLimit int64) (Rows, int64, error) {
	var rows = Rows{}
	var bytes int64 = 2 // Opening and closing list brackets.
	if byteLimit < bytes {
		return nil, 0, fmt.Errorf("prerequisites exceed the parameter budget")
	}
	err := s.each(ctx, func(row map[string]any) error {
		// Measure the values sent to the next query, rather than the compact
		// file representation. Escaped text can be much larger in JSON.
		size := encodedSize(row)
		if size > byteLimit-bytes {
			return fmt.Errorf("prerequisite exceeds the remaining %d-byte parameter budget; divide the calculation into smaller independent parts", byteLimit)
		}
		bytes += size
		rows = append(rows, row)
		return nil
	})
	return rows, bytes, err
}

// sortAnswers sorts small chunks, then repeatedly merges at most sixteen files.
// Merge full groups as they arrive: keeping every tiny chunk until the end would
// let filenames and file-system bookkeeping grow with the entire answer. Each
// level holds at most fifteen files before carrying one merged file to the next.
// Equal keys are left adjacent so
// callers can either collapse duplicates or reject them, as their contract needs.
func sortAnswers(ctx context.Context, input *answerFile, columns ...string) (*answerFile, error) {
	var (
		chunks []*answerFile
		levels [][]*answerFile
		rows   = Rows{}
		bytes  int64
		space  = input.space
	)
	less := func(left, right map[string]any) bool {
		for _, column := range columns {
			a, b := left[column].(string), right[column].(string)
			if a != b {
				return a < b
			}
		}
		return false
	}
	merge := func(inputs []*answerFile) (*answerFile, error) {
		output, err := mergeAnswers(ctx, space, inputs, less)
		if err != nil {
			return nil, err
		}
		for _, input := range inputs {
			if err := input.remove(); err != nil {
				return nil, err
			}
		}
		return output, nil
	}
	addChunk := func(chunk *answerFile) error {
		for level := 0; ; level++ {
			if level == len(levels) {
				levels = append(levels, nil)
			}
			levels[level] = append(levels[level], chunk)
			if len(levels[level]) < 16 {
				return nil
			}
			var err error
			chunk, err = merge(levels[level])
			if err != nil {
				return err
			}
			levels[level] = nil
		}
	}
	flush := func() error {
		sort.Slice(rows, func(left, right int) bool { return less(rows[left], rows[right]) })
		writer, err := space.create()
		if err != nil {
			return err
		}
		defer writer.file.Close()
		for _, row := range rows {
			if err := writer.append(row); err != nil {
				return err
			}
		}
		chunk, err := writer.finish()
		if err != nil {
			return err
		}
		rows = Rows{}
		bytes = 0
		return addChunk(chunk)
	}
	if err := input.each(ctx, func(row map[string]any) error {
		for _, column := range columns {
			if _, ok := row[column].(string); !ok {
				return fmt.Errorf("sort column %s must contain text", column)
			}
		}
		encoded, err := json.Marshal(row)
		if err != nil {
			return err
		}
		// A conservative allowance for maps, strings, and slice bookkeeping.
		// One row may exceed this allowance; RowBytes still bounds that row.
		cost := int64(len(encoded))*8 + 256
		if bytes+cost > space.limits.SortBytes && len(rows) > 0 {
			if err := flush(); err != nil {
				return err
			}
		}
		rows = append(rows, row)
		bytes += cost
		return nil
	}); err != nil {
		return nil, err
	}
	if len(rows) > 0 || len(levels) == 0 {
		if err := flush(); err != nil {
			return nil, err
		}
	}
	if err := input.remove(); err != nil {
		return nil, err
	}
	for _, level := range levels {
		chunks = append(chunks, level...)
	}
	for len(chunks) > 1 {
		var merged []*answerFile
		for start := 0; start < len(chunks); start += 16 {
			end := min(start+16, len(chunks))
			output, err := merge(chunks[start:end])
			if err != nil {
				return nil, err
			}
			merged = append(merged, output)
		}
		chunks = merged
	}
	return chunks[0], nil
}

func mergeAnswers(ctx context.Context, space *answerSpace, inputs []*answerFile, less func(map[string]any, map[string]any) bool) (*answerFile, error) {
	var readers []*answerReader
	var heads []map[string]any
	defer func() {
		for _, reader := range readers {
			_ = reader.file.Close()
		}
	}()
	for _, input := range inputs {
		reader, err := input.open()
		if err != nil {
			return nil, err
		}
		readers = append(readers, reader)
		row, err := reader.next()
		if err != nil && err != io.EOF {
			return nil, err
		}
		heads = append(heads, row)
	}
	writer, err := space.create()
	if err != nil {
		return nil, err
	}
	defer writer.file.Close()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		selected := -1
		for index, row := range heads {
			if row != nil && (selected == -1 || less(row, heads[selected])) {
				selected = index
			}
		}
		if selected == -1 {
			return writer.finish()
		}
		if err := writer.append(heads[selected]); err != nil {
			return nil, err
		}
		row, err := readers[selected].next()
		if err != nil && err != io.EOF {
			return nil, err
		}
		heads[selected] = row
	}
}

func encodedSize(value any) int64 { encoded, _ := json.Marshal(value); return int64(len(encoded)) + 1 }
