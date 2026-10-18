package bootparam_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/bootparam"
)

func bootHeaderFixture(t *testing.T, header bootparam.SetupHeader) *bytes.Reader {
	t.Helper()

	var image bytes.Buffer
	image.Write(make([]byte, 0x1f1))

	if err := binary.Write(&image, binary.LittleEndian, header); err != nil {
		t.Fatal(err)
	}

	return bytes.NewReader(image.Bytes())
}

func validBootHeader() bootparam.SetupHeader {
	return bootparam.SetupHeader{
		SetupSects:  4,
		BootFlag:    0xaa55,
		Header:      bootparam.MagicSignature,
		Version:     0x0206,
		Code32Start: 0x100000,
		CmdlineSize: 2048,
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	header := validBootHeader()
	b, err := bootparam.New(bootHeaderFixture(t, header))
	if err != nil {
		t.Fatal(err)
	}

	if b.Hdr != header {
		t.Fatalf("decoded header = %#v, want %#v", b.Hdr, header)
	}
}

func TestNewNotbzImage(t *testing.T) {
	t.Parallel()

	header := validBootHeader()
	header.Header = 0

	if _, err := bootparam.New(bootHeaderFixture(t, header)); !errors.Is(err, bootparam.ErrorSignatureNotMatch) {
		t.Fatalf("invalid signature: got %v, want %v", err, bootparam.ErrorSignatureNotMatch)
	}
}

func TestNewOldProtocol(t *testing.T) {
	t.Parallel()

	header := validBootHeader()
	header.Version = 0x0205

	if _, err := bootparam.New(bootHeaderFixture(t, header)); !errors.Is(err, bootparam.ErrorOldProtocolVersion) {
		t.Fatalf("old protocol: got %v, want %v", err, bootparam.ErrorOldProtocolVersion)
	}
}

func TestNewTruncatedHeader(t *testing.T) {
	t.Parallel()

	image := bytes.NewReader(make([]byte, 0x1f2))
	if _, err := bootparam.New(image); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated header: got %v, want %v", err, io.ErrUnexpectedEOF)
	}
}

func TestBytes(t *testing.T) {
	t.Parallel()

	header := validBootHeader()
	b, err := bootparam.New(bootHeaderFixture(t, header))
	if err != nil {
		t.Fatal(err)
	}

	raw, err := b.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := bootparam.New(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	if decoded.Hdr != header {
		t.Fatalf("serialized header = %#v, want %#v", decoded.Hdr, header)
	}
}

func TestAddE820Entry(t *testing.T) {
	t.Parallel()

	b, err := bootparam.New(bootHeaderFixture(t, validBootHeader()))
	if err != nil {
		t.Fatal(err)
	}

	b.AddE820Entry(
		0x1234567812345678,
		0xabcdefabcdefabcd,
		bootparam.E820Ram,
	)

	rawBootParam, err := b.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	if rawBootParam[0x1E8] != 1 {
		t.Fatalf("invalid e820_entries: %d", rawBootParam[0x1E8])
	}

	actual := bootparam.E820Entry{}
	reader := bytes.NewReader(rawBootParam[0x2D0:])

	if err := binary.Read(reader, binary.LittleEndian, &actual); err != nil {
		t.Fatal(err)
	}

	if actual.Addr != 0x1234567812345678 {
		t.Fatalf("invalid e820 addr: %v", actual.Addr)
	}

	if actual.Size != 0xabcdefabcdefabcd {
		t.Fatalf("invalid e820 size: %v", actual.Size)
	}

	if actual.Type != bootparam.E820Ram {
		t.Fatalf("invalid e820 type: %v", actual.Type)
	}
}
