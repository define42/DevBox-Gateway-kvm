package gateway

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/synctest"
	"time"
)

func TestCreateWithProgressReportsActualPhasesAndBytes(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "graceful")
	var events []CreateProgress
	vm, err := m.CreateWithProgress(t.Context(), "alice", "progress", seedHelperPasswordHash(), DefaultImageID,
		func(progress CreateProgress) { events = append(events, progress) })
	if err != nil {
		t.Fatal(err)
	}
	if vm.State != "stopped" {
		t.Fatalf("creation returned before VM was ready: %+v", vm)
	}
	var phases []string
	for _, event := range events {
		phases = append(phases, event.Phase)
		if event.Phase != "copying" && (event.CopiedBytes != 0 || event.TotalBytes != 0) {
			t.Fatalf("non-copy phase claims byte progress: %+v", event)
		}
	}
	got := slices.Compact(phases)
	want := []string{"preparing", "copying", "configuring", "finalizing"}
	if !slices.Equal(got, want) {
		t.Fatalf("creation phases = %v, want %v", got, want)
	}
	info, err := os.Stat(m.config.BaseImage)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 5 || events[1].CopiedBytes != 0 || events[1].TotalBytes != info.Size() {
		t.Fatalf("copy start omitted actual file size: %+v", events)
	}
	lastCopy := events[len(events)-3]
	if lastCopy.Phase != "copying" || lastCopy.CopiedBytes != info.Size() || lastCopy.TotalBytes != info.Size() {
		t.Fatalf("copy completion omitted actual bytes: %+v", lastCopy)
	}
}

func TestCreateWithProgressCancellationRollsBackEveryPhase(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"preparing", "copying", "configuring", "finalizing"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			m := newTestManager(t, "graceful")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			_, err := m.CreateWithProgress(ctx, "alice", "cancelled", seedHelperPasswordHash(), DefaultImageID,
				func(progress CreateProgress) {
					if progress.Phase == phase {
						cancel()
					}
				})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("phase cancellation returned %v", err)
			}
			vms, err := m.List(t.Context(), "alice")
			if err != nil || len(vms) != 0 {
				t.Fatalf("cancelled creation retained a VM: %+v, %v", vms, err)
			}
			entries, err := os.ReadDir(filepath.Join(m.config.DataDir, "vms"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("cancelled creation retained disk state: %v, %v", entries, err)
			}
		})
	}
}

func TestCloneFileWithProgressPreservesData(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source, target := filepath.Join(dir, "source"), filepath.Join(dir, "target")
	data := bytes.Repeat([]byte("desktop image data\n"), 1000)
	if err := os.WriteFile(source, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var events []CreateProgress
	if err := cloneFileWithProgress(t.Context(), source, target,
		func(progress CreateProgress) { events = append(events, progress) }); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("clone changed source bytes: %v", err)
	}
	if len(events) < 2 || events[0].CopiedBytes != 0 || events[0].TotalBytes != int64(len(data)) {
		t.Fatalf("invalid initial clone progress: %+v", events)
	}
	last := events[len(events)-1]
	if last.CopiedBytes != int64(len(data)) || last.TotalBytes != int64(len(data)) {
		t.Fatalf("invalid final clone progress: %+v", last)
	}
}

func TestCopyFileWithProgressReportsWrittenBytes(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		data := bytes.Repeat([]byte("copy"), 1<<20)
		input := progressPacedReader{reader: bytes.NewReader(data)}
		var output bytes.Buffer
		var events []CreateProgress
		err := copyFileWithProgress(t.Context(), input, &output, int64(len(data)), func(progress CreateProgress) {
			if progress.CopiedBytes != int64(output.Len()) {
				t.Errorf("progress bytes %d differ from actual writes %d", progress.CopiedBytes, output.Len())
			}
			events = append(events, progress)
		})
		if err != nil || !bytes.Equal(output.Bytes(), data) {
			t.Fatalf("copy failed or changed bytes: %v", err)
		}
		if len(events) < 2 || events[0].CopiedBytes <= 0 || events[0].CopiedBytes >= int64(len(data)) {
			t.Fatalf("missing intermediate real-byte progress: %+v", events)
		}
		last := events[len(events)-1]
		if last.CopiedBytes != int64(len(data)) || last.TotalBytes != int64(len(data)) {
			t.Fatalf("copy final progress = %+v", last)
		}
	})
}

func TestCopyFileWithProgressCancellationStopsWrites(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		data := bytes.Repeat([]byte("copy"), 1<<20)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var output bytes.Buffer
		err := copyFileWithProgress(ctx, progressPacedReader{reader: bytes.NewReader(data)}, &output,
			int64(len(data)), func(CreateProgress) { cancel() })
		if !errors.Is(err, context.Canceled) || output.Len() == 0 || output.Len() >= len(data) {
			t.Fatalf("copy failed to stop at cancellation: %d of %d bytes, %v", output.Len(), len(data), err)
		}
	})
}

func TestCopyFileWithProgressRejectsTruncatedSource(t *testing.T) {
	t.Parallel()
	var events []CreateProgress
	err := copyFileWithProgress(t.Context(), bytes.NewReader([]byte("short")), io.Discard, 100,
		func(progress CreateProgress) { events = append(events, progress) })
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated source accepted: %v", err)
	}
	for _, event := range events {
		if event.CopiedBytes == event.TotalBytes {
			t.Fatalf("failed copy reported completion: %+v", event)
		}
	}
}

type progressPacedReader struct{ reader io.Reader }

func (r progressPacedReader) Read(buffer []byte) (int, error) {
	time.Sleep(60 * time.Millisecond)

	return r.reader.Read(buffer)
}
