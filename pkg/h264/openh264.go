// Package openh264 is a cgo-free Go port of Cisco's OpenH264 codec library.
//
// Two levels of API are provided:
//
//   - The C-API mirror: CreateDecoder / CreateEncoder return the
//     api.ISVCDecoder / api.ISVCEncoder interfaces, which behave exactly like
//     the C++ ISVCDecoder / ISVCEncoder classes (see package api).
//   - A small idiomatic wrapper: Decoder and Encoder, working on Frame values
//     with tightly packed I420 planes.
//
// The port includes architecture-specific SIMD assembly kernels with Go
// fallbacks. Its audited fixed-slice encoder path can use bounded Go workers;
// decoding and other encoder modes remain sequential. Without the optional
// Go-only UnchangedMacroblocks hints, its output is bit-exact with the C reference
// implementation. Those hints may produce a different valid H.264 bitstream.
package openh264

import (
	"errors"
	"fmt"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/decoder"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/encoder"
)

// CreateDecoder is the equivalent of WelsCreateDecoder.
func CreateDecoder() (api.ISVCDecoder, error) {
	var d api.ISVCDecoder
	if rv := decoder.WelsCreateDecoder(&d); rv != 0 || d == nil {
		return nil, fmt.Errorf("openh264: WelsCreateDecoder failed (%d)", rv)
	}
	return d, nil
}

// DestroyDecoder is the equivalent of WelsDestroyDecoder.
func DestroyDecoder(d api.ISVCDecoder) { decoder.WelsDestroyDecoder(d) }

// CreateEncoder is the equivalent of WelsCreateSVCEncoder.
func CreateEncoder() (api.ISVCEncoder, error) {
	var e api.ISVCEncoder
	if rv := encoder.WelsCreateSVCEncoder(&e); rv != 0 || e == nil {
		return nil, fmt.Errorf("openh264: WelsCreateSVCEncoder failed (%d)", rv)
	}
	return e, nil
}

// DestroyEncoder is the equivalent of WelsDestroySVCEncoder.
func DestroyEncoder(e api.ISVCEncoder) { encoder.WelsDestroySVCEncoder(e) }

// GetDecoderCapability is the equivalent of WelsGetDecoderCapability.
func GetDecoderCapability() (api.SDecoderCapability, error) {
	var c api.SDecoderCapability
	if rv := decoder.WelsGetDecoderCapability(&c); rv != 0 {
		return c, fmt.Errorf("openh264: WelsGetDecoderCapability failed (%d)", rv)
	}
	return c, nil
}

// CodecVersion is the equivalent of WelsGetCodecVersion.
func CodecVersion() api.OpenH264Version { return encoder.WelsGetCodecVersion() }

// Frame is a YUV 4:2:0 picture with tightly packed planes:
// len(Y) == Width*Height, len(U) == len(V) == (Width/2)*(Height/2).
type Frame struct {
	Width, Height int
	Y, U, V       []byte
	// Timestamp is the input timestamp for encoding (milliseconds), or the
	// output timestamp reported by the decoder.
	Timestamp int64
	// UnchangedMacroblocks is a Go-only encoding hint, borrowed until Encode
	// returns. In row-major ceil(Width/16)*ceil(Height/16) order, true certifies
	// that every source Y/U/V sample in that macroblock is unchanged since the
	// immediately preceding successful encoded picture. Nil or a wrong length
	// disables the hint. Decoded frames do not carry it. Incorrect hints can
	// suppress real changes; encoders may ignore any or all hints.
	UnchangedMacroblocks []bool
	// ForcedMacroblocks, in the same order and used only with valid
	// UnchangedMacroblocks, asks for true macroblocks to repeat a reference
	// block whatever their source, displaced by ForcedShift luma rows when set;
	// see api.SSourcePicture.
	ForcedMacroblocks []bool
	ForcedShift       []int16
}

// NewFrame allocates a zeroed frame of the given (even) size.
func NewFrame(width, height int) *Frame {
	cw, ch := width/2, height/2
	return &Frame{
		Width: width, Height: height,
		Y: make([]byte, width*height),
		U: make([]byte, cw*ch),
		V: make([]byte, cw*ch),
	}
}

// DecodingError reports a non-zero DECODING_STATE. Frames returned together
// with a DecodingError may still be usable (e.g. error-concealed output).
type DecodingError struct{ State api.DECODING_STATE }

func (e *DecodingError) Error() string {
	return fmt.Sprintf("openh264: decoding state 0x%x", int32(e.State))
}

// Decoder is an idiomatic wrapper around api.ISVCDecoder.
type Decoder struct {
	d api.ISVCDecoder
}

// NewDecoder creates and initializes a decoder. A nil param selects the
// defaults used by the reference tests (AVC bitstream, error concealment
// ERROR_CON_SLICE_COPY).
func NewDecoder(param *api.SDecodingParam) (*Decoder, error) {
	d, err := CreateDecoder()
	if err != nil {
		return nil, err
	}
	if param == nil {
		param = &api.SDecodingParam{}
		param.SVideoProperty.EVideoBsType = api.VIDEO_BITSTREAM_AVC
	}
	if rv := d.Initialize(param); rv != 0 {
		DestroyDecoder(d)
		return nil, fmt.Errorf("openh264: decoder Initialize failed (%d)", rv)
	}
	return &Decoder{d: d}, nil
}

// Raw returns the underlying api.ISVCDecoder (for SetOption/GetOption etc.).
func (d *Decoder) Raw() api.ISVCDecoder { return d.d }

// Decode feeds one or more Annex-B NAL units (typically one access unit) to
// the decoder. It returns the picture that became ready for output, if any.
// Because of B-frame reordering, output can lag input; call Flush at the end
// of the stream to drain the remaining pictures.
func (d *Decoder) Decode(bitstream []byte) (*Frame, error) {
	var dst [3][]byte
	var info api.SBufferInfo
	state := d.d.DecodeFrame2(bitstream, int32(len(bitstream)), &dst, &info)
	f := frameFromBuffer(&dst, &info)
	if state != api.DsErrorFree {
		return f, &DecodingError{State: state}
	}
	return f, nil
}

// Flush signals end of stream and returns all pictures still buffered for
// reordering, in display order.
func (d *Decoder) Flush() ([]*Frame, error) {
	eos := int32(1)
	d.d.SetOption(api.DECODER_OPTION_END_OF_STREAM, &eos)
	var out []*Frame
	var dst [3][]byte
	var info api.SBufferInfo
	d.d.DecodeFrame2(nil, 0, &dst, &info)
	if f := frameFromBuffer(&dst, &info); f != nil {
		out = append(out, f)
	}
	var remaining int32
	d.d.GetOption(api.DECODER_OPTION_NUM_OF_FRAMES_REMAINING_IN_BUFFER, &remaining)
	for i := int32(0); i < remaining; i++ {
		dst = [3][]byte{}
		info = api.SBufferInfo{}
		state := d.d.FlushFrame(&dst, &info)
		if f := frameFromBuffer(&dst, &info); f != nil {
			out = append(out, f)
		}
		if state != api.DsErrorFree {
			return out, &DecodingError{State: state}
		}
	}
	return out, nil
}

// Close releases the decoder.
func (d *Decoder) Close() {
	if d.d != nil {
		d.d.Uninitialize()
		DestroyDecoder(d.d)
		d.d = nil
	}
}

func frameFromBuffer(dst *[3][]byte, info *api.SBufferInfo) *Frame {
	if info.IBufferStatus != 1 || dst[0] == nil {
		return nil
	}
	sb := &info.UsrData.SSystemBuffer
	w, h := int(sb.IWidth), int(sb.IHeight)
	f := NewFrame(w, h)
	f.Timestamp = int64(info.UiOutYuvTimeStamp)
	copyPlane(f.Y, w, h, dst[0], int(sb.IStride[0]))
	copyPlane(f.U, w/2, h/2, dst[1], int(sb.IStride[1]))
	copyPlane(f.V, w/2, h/2, dst[2], int(sb.IStride[1]))
	return f
}

func copyPlane(out []byte, w, h int, src []byte, stride int) {
	for y := 0; y < h; y++ {
		copy(out[y*w:(y+1)*w], src[y*stride:y*stride+w])
	}
}

// Encoder is an idiomatic wrapper around api.ISVCEncoder.
type Encoder struct {
	e      api.ISVCEncoder
	width  int
	height int

	// Encoding is synchronous. Reuse metadata without retaining borrowed input
	// planes or codec-owned output buffers after the call returns.
	source api.SSourcePicture
	info   api.SFrameBSInfo
}

// DefaultEncoderParams returns the encoder's default extended parameters
// (GetDefaultParams), with the given size, target bitrate (bits/s) and
// frame rate filled in for a single spatial layer.
func DefaultEncoderParams(width, height, bitrate int, frameRate float32) (*api.SEncParamExt, error) {
	e, err := CreateEncoder()
	if err != nil {
		return nil, err
	}
	defer DestroyEncoder(e)
	p := &api.SEncParamExt{}
	if rv := e.GetDefaultParams(p); rv != 0 {
		return nil, fmt.Errorf("openh264: GetDefaultParams failed (%d)", rv)
	}
	p.IPicWidth, p.IPicHeight = int32(width), int32(height)
	p.ITargetBitrate = int32(bitrate)
	p.FMaxFrameRate = frameRate
	p.ISpatialLayerNum = 1
	l := &p.SSpatialLayers[0]
	l.IVideoWidth, l.IVideoHeight = int32(width), int32(height)
	l.FFrameRate = frameRate
	l.ISpatialBitrate = int32(bitrate)
	return p, nil
}

// NewEncoder creates an encoder from extended parameters (see
// DefaultEncoderParams). The input format is I420.
func NewEncoder(param *api.SEncParamExt) (*Encoder, error) {
	if param == nil {
		return nil, errors.New("openh264: nil encoder parameters")
	}
	e, err := CreateEncoder()
	if err != nil {
		return nil, err
	}
	if rv := e.InitializeExt(param); rv != 0 {
		DestroyEncoder(e)
		return nil, fmt.Errorf("openh264: encoder InitializeExt failed (%d)", rv)
	}
	format := int32(api.VideoFormatI420)
	e.SetOption(api.ENCODER_OPTION_DATAFORMAT, &format)
	return &Encoder{e: e, width: int(param.IPicWidth), height: int(param.IPicHeight)}, nil
}

// Raw returns the underlying api.ISVCEncoder (for SetOption/GetOption etc.).
func (e *Encoder) Raw() api.ISVCEncoder { return e.e }

// Encode encodes one picture and returns the produced Annex-B bitstream
// (all layers and NAL units concatenated) and the frame type. A skipped
// frame returns an empty bitstream and api.VideoFrameTypeSkip.
func (e *Encoder) Encode(f *Frame) ([]byte, api.EVideoFrameType, error) {
	if f.Width != e.width || f.Height != e.height {
		return nil, api.VideoFrameTypeInvalid, fmt.Errorf("openh264: frame is %dx%d, encoder expects %dx%d", f.Width, f.Height, e.width, e.height)
	}
	defer func() {
		e.source = api.SSourcePicture{}
		e.info = api.SFrameBSInfo{}
	}()
	pic := &e.source
	*pic = api.SSourcePicture{
		IColorFormat: int32(api.VideoFormatI420),
		IPicWidth:    int32(f.Width),
		IPicHeight:   int32(f.Height),
		UiTimeStamp:  f.Timestamp,
	}
	pic.IStride[0], pic.IStride[1], pic.IStride[2] = int32(f.Width), int32(f.Width/2), int32(f.Width/2)
	pic.PData[0], pic.PData[1], pic.PData[2] = f.Y, f.U, f.V
	pic.UnchangedMacroblocks = f.UnchangedMacroblocks
	pic.ForcedMacroblocks, pic.ForcedShift = f.ForcedMacroblocks, f.ForcedShift
	info := &e.info
	if rv := e.e.EncodeFrame(pic, info); rv != 0 {
		return nil, api.VideoFrameTypeInvalid, fmt.Errorf("openh264: EncodeFrame failed (%d)", rv)
	}
	return collectBitstream(info), info.EFrameType, nil
}

// ForceIntraFrame requests an IDR picture for the next encoded frame.
func (e *Encoder) ForceIntraFrame() error {
	if rv := e.e.ForceIntraFrame(true, -1); rv != 0 {
		return fmt.Errorf("openh264: ForceIntraFrame failed (%d)", rv)
	}
	return nil
}

// Close releases the encoder.
func (e *Encoder) Close() {
	if e.e != nil {
		e.e.Uninitialize()
		DestroyEncoder(e.e)
		e.e = nil
	}
}

func collectBitstream(info *api.SFrameBSInfo) []byte {
	if info.EFrameType == api.VideoFrameTypeSkip {
		return nil
	}
	var out []byte
	for i := 0; i < int(info.ILayerNum); i++ {
		l := &info.SLayerInfo[i]
		size := 0
		for j := 0; j < int(l.INalCount); j++ {
			size += int(l.PNalLengthInByte[j])
		}
		out = append(out, l.PBsBuf[:size]...)
	}
	return out
}
