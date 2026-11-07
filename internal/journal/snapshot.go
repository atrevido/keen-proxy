package journal

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
)

type diskSnapshot struct {
	*os.File
	size int64
}

// snapshot opens the retained files before rotation can replace them and
// records their lengths so concurrent appends do not enter the export.
func (j *Journal) snapshot() ([]Entry, []diskSnapshot, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	ring := slices.Clone(j.ring)
	if j.opt.File == "" {
		return ring, nil, nil
	}
	var files []diskSnapshot
	for _, path := range []string{j.opt.File + ".1", j.opt.File} {
		f, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err == nil {
			var st os.FileInfo
			st, err = f.Stat()
			if err == nil {
				files = append(files, diskSnapshot{f, st.Size()})
				continue
			}
			f.Close()
		}
		closeSnapshots(files)
		return nil, nil, err
	}
	return ring, files, nil
}

func closeSnapshots(files []diskSnapshot) {
	for _, f := range files {
		f.Close()
	}
}

// Visit reads retained history in ID order without holding the journal lock
// during callbacks. Memory wins where disk has an older repetition count.
func (j *Journal) Visit(emit func(Entry) error) error {
	ring, files, err := j.snapshot()
	if err != nil {
		return err
	}
	defer closeSnapshots(files)

	for _, f := range files {
		err := visitEntries(io.NewSectionReader(f.File, 0, f.size), func(e Entry) error {
			return mergeJournalEntry(&ring, e, emit)
		})
		if err != nil {
			return err
		}
	}
	for _, e := range ring {
		if err := emit(e); err != nil {
			return err
		}
	}
	return nil
}

// mergeJournalEntry emits pending memory records before the next disk record,
// then prefers the in-memory record when both sources have the same ID.
func mergeJournalEntry(pending *[]Entry, disk Entry, emit func(Entry) error) error {
	ring := *pending
	for i, memory := range ring {
		if memory.ID > disk.ID {
			break
		}
		*pending = ring[i+1:]
		if memory.ID == disk.ID {
			return emit(memory)
		}
		if err := emit(memory); err != nil {
			return err
		}
	}
	return emit(disk)
}

// visitEntries ignores malformed or oversized records and keeps reading later
// entries. A filesystem error still stops the export.
func visitEntries(input io.Reader, emit func(Entry) error) error {
	reader := bufio.NewReaderSize(input, 64<<10)
	for {
		raw, err := readJournalLine(reader)
		if err != nil && err != io.EOF {
			return err
		}
		var e Entry
		if len(raw) > 0 && json.Unmarshal(raw, &e) == nil {
			if err := emit(e); err != nil {
				return err
			}
		}
		if err == io.EOF {
			return nil
		}
	}
}

// readJournalLine drains oversized lines while bounding memory use. Typical
// records fit the reader buffer and do not need a separate allocation.
func readJournalLine(reader *bufio.Reader) ([]byte, error) {
	const maxRecord = 1 << 20
	part, err := reader.ReadSlice('\n')
	if err != bufio.ErrBufferFull {
		return part, err
	}
	raw := slices.Clone(part)
	for err == bufio.ErrBufferFull {
		part, err = reader.ReadSlice('\n')
		if len(raw)+len(part) > maxRecord {
			raw = nil
		}
		if raw != nil {
			raw = append(raw, part...)
		}
	}
	return raw, err
}
