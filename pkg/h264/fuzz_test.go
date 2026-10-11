package openh264

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

func splitNALs(data []byte) [][]byte {
	sc := []byte{0, 0, 0, 1}
	var out [][]byte
	for len(data) > 0 {
		i := bytes.Index(data[1:], sc)
		if i < 0 {
			out = append(out, data)
			break
		}
		out = append(out, data[:i+1])
		data = data[i+1:]
	}
	return out
}

// newQuietDecoder returns a decoder with logging disabled, for tests that
// feed deliberately corrupted streams (the decoder would otherwise log every
// rejected syntax element to stderr).
func newQuietDecoder() (*Decoder, error) {
	dec, err := NewDecoder(nil)
	if err != nil {
		return nil, err
	}
	quiet := int32(api.WELS_LOG_QUIET)
	dec.Raw().SetOption(api.DECODER_OPTION_TRACE_LEVEL, &quiet)
	return dec, nil
}

// FuzzDecode checks that corrupted bitstreams never crash the decoder.
func FuzzDecode(f *testing.F) {
	for _, name := range []string{"BA_MW_D.264", "test_qcif_cabac.264", "CVFC1_Sony_C.jsv", "MR1_BT_A.h264"} {
		if data, err := os.ReadFile(filepath.Join("../../res", name)); err == nil {
			f.Add(data)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		dec, err := newQuietDecoder()
		if err != nil {
			t.Fatal(err)
		}
		defer dec.Close()
		for _, nal := range splitNALs(data) {
			dec.Decode(nal)
		}
		dec.Flush()
	})
}
