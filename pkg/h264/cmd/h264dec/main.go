// Command h264dec is the Wels decoder console application, a port of
// codec/console/dec/src/h264dec.cpp (plus the file-output part of
// codec/console/dec/src/d3d9_utils.cpp).
//
// Usage:
//
//	h264dec welsdec.cfg
//	h264dec welsdec.264 out.yuv [-options file] [-trace level] [-length file] [-ec idc] [-legacy]
//	h264dec welsdec.264
//
// The Direct3D rendering module of the Windows build is not ported: decoded
// pictures are only written to the output YUV file (I420, cropped).
package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/console"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/decoder"
)

// cFloat formats v like printf ("%f") in C.
func cFloat(v float64) string {
	switch {
	case math.IsNaN(v):
		if math.Signbit(v) {
			return "-nan"
		}
		return "nan"
	case math.IsInf(v, 1):
		return "inf"
	case math.IsInf(v, -1):
		return "-inf"
	}
	return fmt.Sprintf("%f", v)
}

func readBit(pBufPtr []uint8, curBit *int32) int32 {
	nIndex := *curBit / 8
	nOffset := *curBit%8 + 1

	*curBit++
	return int32(pBufPtr[nIndex]>>(8-nOffset)) & 0x01
}

func readBits(pBufPtr []uint8, n int32, curBit *int32) int32 {
	r := int32(0)
	for i := int32(0); i < n; i++ {
		r |= readBit(pBufPtr, curBit) << (n - i - 1)
	}
	return r
}

func bsGetUe(pBufPtr []uint8, curBit *int32) int32 {
	i := int32(0)
	for readBit(pBufPtr, curBit) == 0 && i < 32 {
		i++
	}
	r := readBits(pBufPtr, i, curBit)
	r += (1 << i) - 1
	return r
}

func readFirstMbInSlice(pSliceNalPtr []uint8) int32 {
	curBit := int32(0)
	return bsGetUe(pSliceNalPtr[1:], &curBit)
}

// readPicture returns the size of the next access unit starting at bufPos.
// pSpsBuf receives the offset of the last SPS seen inside pBuf (-1 == NULL).
func readPicture(pBuf []uint8, iFileSize int32, bufPos int32, pSpsBuf *int32, sps_byte_count *int32) int32 {
	bytes_available := iFileSize - bufPos
	if bytes_available < 4 {
		return bytes_available
	}
	ptr := bufPos
	read_bytes := int32(0)
	sps_count := int32(0)
	pps_count := int32(0)
	non_idr_pict_count := int32(0)
	idr_pict_count := int32(0)
	nal_deliminator := int32(0)
	*pSpsBuf = -1
	*sps_byte_count = 0
	for read_bytes < bytes_available-4 {
		has4ByteStartCode := pBuf[ptr] == 0 && pBuf[ptr+1] == 0 && pBuf[ptr+2] == 0 && pBuf[ptr+3] == 1
		has3ByteStartCode := false
		if !has4ByteStartCode {
			has3ByteStartCode = pBuf[ptr] == 0 && pBuf[ptr+1] == 0 && pBuf[ptr+2] == 1
		}
		if has4ByteStartCode || has3ByteStartCode {
			byteOffset := int32(3)
			nal_unit_type := pBuf[ptr+3] & 0x1F
			if has4ByteStartCode {
				byteOffset = 4
				nal_unit_type = pBuf[ptr+4] & 0x1F
			}
			if nal_unit_type == 1 {
				firstMBInSlice := readFirstMbInSlice(pBuf[ptr+byteOffset:])
				non_idr_pict_count++
				if non_idr_pict_count >= 1 && idr_pict_count >= 1 && firstMBInSlice == 0 {
					return read_bytes
				}
				if non_idr_pict_count >= 2 && firstMBInSlice == 0 {
					return read_bytes
				}
			} else if nal_unit_type == 5 {
				firstMBInSlice := readFirstMbInSlice(pBuf[ptr+byteOffset:])
				idr_pict_count++
				if idr_pict_count >= 1 && non_idr_pict_count >= 1 && firstMBInSlice == 0 {
					return read_bytes
				}
				if idr_pict_count >= 2 && firstMBInSlice == 0 {
					return read_bytes
				}
			} else if nal_unit_type == 7 {
				*pSpsBuf = ptr + byteOffset
				sps_count++
				if sps_count >= 1 && (non_idr_pict_count >= 1 || idr_pict_count >= 1) {
					return read_bytes
				}
				if sps_count == 2 {
					return read_bytes
				}
			} else if nal_unit_type == 8 {
				pps_count++
				if pps_count == 1 && sps_count == 1 {
					*sps_byte_count = ptr - *pSpsBuf
				}
				if pps_count >= 1 && (non_idr_pict_count >= 1 || idr_pict_count >= 1) {
					return read_bytes
				}
			} else if nal_unit_type == 9 {
				nal_deliminator++
				if nal_deliminator == 2 {
					return read_bytes
				}
			}
			if read_bytes >= bytes_available-4 {
				return bytes_available
			}
			read_bytes += 4
			ptr += 4
		} else {
			ptr++
			read_bytes++
		}
	}
	return bytes_available
}

// cUtils is the non-display part of CUtils (d3d9_utils.cpp): the rendering
// module is Windows-only, so Process always writes the picture to the file.
type cUtils struct{}

func (cUtils) Process(pDst [3][]uint8, pInfo *api.SBufferInfo, pFp *bufio.Writer) int32 {
	if pFp != nil && pDst[0] != nil && pDst[1] != nil && pDst[2] != nil && pInfo != nil {
		var iStride [2]int32
		iWidth := pInfo.UsrData.SSystemBuffer.IWidth
		iHeight := pInfo.UsrData.SSystemBuffer.IHeight
		iStride[0] = pInfo.UsrData.SSystemBuffer.IStride[0]
		iStride[1] = pInfo.UsrData.SSystemBuffer.IStride[1]

		Write2File(pFp, pDst, iStride, iWidth, iHeight)
	}
	return 0
}

// Write2File writes one I420 picture (luma then the two chroma planes).
func Write2File(pFp *bufio.Writer, pData [3][]uint8, iStride [2]int32, iWidth int32, iHeight int32) {
	pPtr := 0
	for i := int32(0); i < iHeight; i++ {
		pFp.Write(pData[0][pPtr : pPtr+int(iWidth)])
		pPtr += int(iStride[0])
	}

	iHeight = iHeight / 2
	iWidth = iWidth / 2
	pPtr = 0
	for i := int32(0); i < iHeight; i++ {
		pFp.Write(pData[1][pPtr : pPtr+int(iWidth)])
		pPtr += int(iStride[1])
	}

	pPtr = 0
	for i := int32(0); i < iHeight; i++ {
		pFp.Write(pData[2][pPtr : pPtr+int(iWidth)])
		pPtr += int(iStride[1])
	}
}

// outFile is a FILE* opened for writing (buffered like stdio).
type outFile struct {
	f *os.File
	w *bufio.Writer
}

func createOutFile(name string) *outFile {
	f, err := os.Create(name)
	if err != nil {
		return nil
	}
	return &outFile{f: f, w: bufio.NewWriterSize(f, 1<<20)}
}

func (o *outFile) writer() *bufio.Writer {
	if o == nil {
		return nil
	}
	return o.w
}

func (o *outFile) close() {
	if o != nil {
		o.w.Flush()
		o.f.Close()
	}
}

// writeInt32 is fwrite (&v, sizeof (v), 1, pFile) on a little-endian host.
func writeInt32(pFile *outFile, v int32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(v))
	pFile.w.Write(b[:])
}

// outputFrame handles one decoded picture: write it and log resolution changes.
func outputFrame(cOutputModule cUtils, sDstBufInfo *api.SBufferInfo, pYuvFile *outFile, pOptionFile *outFile,
	iFrameCount *int32, iWidth *int32, iHeight *int32, iLastWidth *int32, iLastHeight *int32) {
	cOutputModule.Process(sDstBufInfo.PDst, sDstBufInfo, pYuvFile.writer())
	*iWidth = sDstBufInfo.UsrData.SSystemBuffer.IWidth
	*iHeight = sDstBufInfo.UsrData.SSystemBuffer.IHeight
	if pOptionFile != nil {
		if *iWidth != *iLastWidth && *iHeight != *iLastHeight {
			writeInt32(pOptionFile, *iFrameCount)
			writeInt32(pOptionFile, *iWidth)
			writeInt32(pOptionFile, *iHeight)
			*iLastWidth = *iWidth
			*iLastHeight = *iHeight
		}
	}
	*iFrameCount++
}

func FlushFrames(pDecoder api.ISVCDecoder, iTotal *int64, pYuvFile *outFile, pOptionFile *outFile, iFrameCount *int32,
	uiTimeStamp *uint64, iWidth *int32, iHeight *int32, iLastWidth *int32, iLastHeight *int32) {
	var pData [3][]uint8
	var sDstBufInfo api.SBufferInfo
	num_of_frames_in_buffer := int32(0)
	var cOutputModule cUtils
	pDecoder.GetOption(api.DECODER_OPTION_NUM_OF_FRAMES_REMAINING_IN_BUFFER, &num_of_frames_in_buffer)
	for i := int32(0); i < num_of_frames_in_buffer; i++ {
		iStart := common.WelsTime()
		pData = [3][]uint8{}
		sDstBufInfo = api.SBufferInfo{}
		sDstBufInfo.UiInBsTimeStamp = *uiTimeStamp
		pDecoder.FlushFrame(&pData, &sDstBufInfo)
		iEnd := common.WelsTime()
		*iTotal += iEnd - iStart
		if sDstBufInfo.IBufferStatus == 1 {
			outputFrame(cOutputModule, &sDstBufInfo, pYuvFile, pOptionFile, iFrameCount, iWidth, iHeight, iLastWidth,
				iLastHeight)
		}
	}
}

func H264DecodeInstance(pDecoder api.ISVCDecoder, kpH264FileName string, kpOuputFileName string,
	iWidth *int32, iHeight *int32, pOptionFileName string, pLengthFileName string,
	iErrorConMethod int32, bLegacyCalling bool) {
	var pYuvFile, pOptionFile *outFile
	// Length input mode support
	var fpTrack *os.File

	if pDecoder == nil {
		return
	}

	var uiTimeStamp uint64
	var iStart, iEnd, iTotal int64
	var iSliceSize int32
	var pBuf []uint8
	uiStartCode := [4]uint8{0, 0, 0, 1}

	var pData [3][]uint8
	var sDstBufInfo api.SBufferInfo

	iBufPos := int32(0)
	var iFileSize int32
	iLastWidth, iLastHeight := int32(0), int32(0)
	iFrameCount := int32(0)
	pDecoder.SetOption(api.DECODER_OPTION_ERROR_CON_IDC, &iErrorConMethod)
	var cOutputModule cUtils
	dElapsed := 0.0
	var uLastSpsBuf [32]uint8
	iLastSpsByteCount := int32(0)

	iThreadCount := int32(1)
	pDecoder.GetOption(api.DECODER_OPTION_NUM_OF_THREADS, &iThreadCount)

	if kpH264FileName == "" {
		fmt.Fprintf(os.Stderr, "Can not find any h264 bitstream file to read..\n")
		fmt.Fprintf(os.Stderr, "----------------decoder return------------------------\n")
		return
	}
	pH264File, err := os.Open(kpH264FileName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Can not open h264 source file, check its legal path related please..\n")
		return
	}
	fmt.Fprintf(os.Stderr, "H264 source file name: %s..\n", kpH264FileName)

	defer func() {
		// label_exit:
		pH264File.Close()
		pYuvFile.close()
		pOptionFile.close()
		if fpTrack != nil {
			fpTrack.Close()
		}
	}()

	if kpOuputFileName != "" {
		pYuvFile = createOutFile(kpOuputFileName)
		if pYuvFile == nil {
			fmt.Fprintf(os.Stderr, "Can not open yuv file to output result of decoding..\n")
			// any options
			//return; // can let decoder work in quiet mode, no writing any output
		} else {
			fmt.Fprintf(os.Stderr, "Sequence output file name: %s..\n", kpOuputFileName)
		}
	} else {
		fmt.Fprintf(os.Stderr, "Can not find any output file to write..\n")
		// any options
	}

	if pOptionFileName != "" {
		pOptionFile = createOutFile(pOptionFileName)
		if pOptionFile == nil {
			fmt.Fprintf(os.Stderr, "Can not open optional file for write..\n")
		} else {
			fmt.Fprintf(os.Stderr, "Extra optional file: %s..\n", pOptionFileName)
		}
	}

	if pLengthFileName != "" {
		fpTrack, err = os.Open(pLengthFileName)
		if err != nil {
			fpTrack = nil
			stdout.WriteString("Length file open ERROR!\n")
		}
	}

	stdout.WriteString("------------------------------------------------------\n")

	if fi, err := pH264File.Stat(); err == nil {
		iFileSize = int32(fi.Size())
	}
	if iFileSize <= 4 {
		fmt.Fprintf(os.Stderr, "Current Bit Stream File is too small, read error!!!!\n")
		return
	}

	pBuf = make([]uint8, iFileSize+4)

	if n, _ := io.ReadFull(pH264File, pBuf[:iFileSize]); n != int(iFileSize) {
		fmt.Fprintf(os.Stderr, "Unable to read whole file\n")
		return
	}

	copy(pBuf[iFileSize:], uiStartCode[:])

	for {
		if iBufPos >= iFileSize {
			iEndOfStreamFlag := int32(1)
			pDecoder.SetOption(api.DECODER_OPTION_END_OF_STREAM, &iEndOfStreamFlag)
			break
		}
		// Read length from file if needed
		if fpTrack != nil {
			var pInfo [16]byte
			n, _ := io.ReadFull(fpTrack, pInfo[:])
			if n/4 < 4 {
				return // goto label_exit
			}
			iSliceSize = int32(binary.LittleEndian.Uint32(pInfo[8:12]))
			// Guard against lengths that do not fit in the file (undefined
			// behaviour in the C application).
			if iSliceSize > iFileSize-iBufPos {
				iSliceSize = iFileSize - iBufPos
			}
			if iSliceSize < 0 && iBufPos+iSliceSize < 0 {
				iSliceSize = -iBufPos
			}
		} else {
			if iThreadCount >= 1 {
				uSpsPtr := int32(-1)
				iSpsByteCount := int32(0)
				iSliceSize = readPicture(pBuf, iFileSize, iBufPos, &uSpsPtr, &iSpsByteCount)
				if iLastSpsByteCount > 0 && iSpsByteCount > 0 {
					if iSpsByteCount != iLastSpsByteCount ||
						!bytes.Equal(pBuf[uSpsPtr:uSpsPtr+iLastSpsByteCount], uLastSpsBuf[:iLastSpsByteCount]) {
						//whenever new sequence is different from preceding sequence. All pending frames must be flushed out before the new sequence can start to decode.
						FlushFrames(pDecoder, &iTotal, pYuvFile, pOptionFile, &iFrameCount, &uiTimeStamp, iWidth, iHeight,
							&iLastWidth, &iLastHeight)
					}
				}
				if iSpsByteCount > 0 && uSpsPtr >= 0 {
					if iSpsByteCount > 32 {
						iSpsByteCount = 32
					}
					iLastSpsByteCount = iSpsByteCount
					copy(uLastSpsBuf[:], pBuf[uSpsPtr:uSpsPtr+iSpsByteCount])
				}
			} else {
				i := int32(0)
				for i = 0; i < iFileSize; i++ {
					p := pBuf[iBufPos+i:]
					if (p[0] == 0 && p[1] == 0 && p[2] == 0 && p[3] == 1 && i > 0) ||
						(p[0] == 0 && p[1] == 0 && p[2] == 1 && i > 0) {
						break
					}
				}
				iSliceSize = i
			}
		}
		if iSliceSize < 4 { //too small size, no effective data, ignore
			iBufPos += iSliceSize
			continue
		}

		//for coverage test purpose
		var iEndOfStreamFlag int32
		pDecoder.GetOption(api.DECODER_OPTION_END_OF_STREAM, &iEndOfStreamFlag)
		var iCurIdrPicId int32
		pDecoder.GetOption(api.DECODER_OPTION_IDR_PIC_ID, &iCurIdrPicId)
		var iFrameNum int32
		pDecoder.GetOption(api.DECODER_OPTION_FRAME_NUM, &iFrameNum)
		var bCurAuContainLtrMarkSeFlag int32
		pDecoder.GetOption(api.DECODER_OPTION_LTR_MARKING_FLAG, &bCurAuContainLtrMarkSeFlag)
		var iFrameNumOfAuMarkedLtr int32
		pDecoder.GetOption(api.DECODER_OPTION_LTR_MARKED_FRAME_NUM, &iFrameNumOfAuMarkedLtr)
		var iFeedbackVclNalInAu int32
		pDecoder.GetOption(api.DECODER_OPTION_VCL_NAL, &iFeedbackVclNalInAu)
		var iFeedbackTidInAu int32
		pDecoder.GetOption(api.DECODER_OPTION_TEMPORAL_ID, &iFeedbackTidInAu)
		//~end for

		iStart = common.WelsTime()
		pData = [3][]uint8{}
		uiTimeStamp++
		sDstBufInfo = api.SBufferInfo{}
		sDstBufInfo.UiInBsTimeStamp = uiTimeStamp
		if !bLegacyCalling {
			pDecoder.DecodeFrameNoDelay(pBuf[iBufPos:], iSliceSize, &pData, &sDstBufInfo)
		} else {
			pDecoder.DecodeFrame2(pBuf[iBufPos:], iSliceSize, &pData, &sDstBufInfo)
		}

		iEnd = common.WelsTime()
		iTotal += iEnd - iStart
		if sDstBufInfo.IBufferStatus == 1 {
			outputFrame(cOutputModule, &sDstBufInfo, pYuvFile, pOptionFile, &iFrameCount, iWidth, iHeight, &iLastWidth,
				&iLastHeight)
		}

		if bLegacyCalling {
			iStart = common.WelsTime()
			pData = [3][]uint8{}
			sDstBufInfo = api.SBufferInfo{}
			sDstBufInfo.UiInBsTimeStamp = uiTimeStamp
			pDecoder.DecodeFrame2(nil, 0, &pData, &sDstBufInfo)
			iEnd = common.WelsTime()
			iTotal += iEnd - iStart
			if sDstBufInfo.IBufferStatus == 1 {
				outputFrame(cOutputModule, &sDstBufInfo, pYuvFile, pOptionFile, &iFrameCount, iWidth, iHeight, &iLastWidth,
					&iLastHeight)
			}
		}
		iBufPos += iSliceSize
	}
	FlushFrames(pDecoder, &iTotal, pYuvFile, pOptionFile, &iFrameCount, &uiTimeStamp, iWidth, iHeight, &iLastWidth,
		&iLastHeight)
	dElapsed = float64(iTotal) / 1e6
	fmt.Fprintf(os.Stderr, "-------------------------------------------------------\n")
	fmt.Fprintf(os.Stderr, "iWidth:\t\t%d\nheight:\t\t%d\nFrames:\t\t%d\ndecode time:\t%s sec\nFPS:\t\t%s fps\n",
		*iWidth, *iHeight, iFrameCount, cFloat(dElapsed), cFloat((float64(iFrameCount)*1.0)/dElapsed))
	fmt.Fprintf(os.Stderr, "-------------------------------------------------------\n")
}

// stdout is buffered like C stdio stdout; it is flushed at exit.
var stdout = bufio.NewWriter(os.Stdout)

func main() {
	iRet := decMain(os.Args)
	stdout.Flush()
	os.Exit(int(iRet))
}

func decMain(pArgV []string) int32 {
	iArgC := len(pArgV)
	var pDecoder api.ISVCDecoder

	var sDecParam api.SDecodingParam
	var strInputFile, strOutputFile, strOptionFile, strLengthFile string
	iLevelSetting := int32(api.WELS_LOG_WARNING)
	bLegacyCalling := false

	sDecParam.SVideoProperty.Size = 8 // sizeof (sDecParam.sVideoProperty)
	sDecParam.EEcActiveIdc = api.ERROR_CON_SLICE_MV_COPY_CROSS_IDR_FREEZE_RES_CHANGE

	if iArgC < 2 {
		stdout.WriteString("usage 1: h264dec.exe welsdec.cfg\n")
		stdout.WriteString("usage 2: h264dec.exe welsdec.264 out.yuv\n")
		stdout.WriteString("usage 3: h264dec.exe welsdec.264\n")
		return 1
	} else if iArgC == 2 {
		if strings.Contains(pArgV[1], ".cfg") { // read config file
			cReadCfg := console.NewCReadConfig(pArgV[1])
			defer cReadCfg.Close()
			strTag := make([]string, 4)
			strReconFile := ""

			if !cReadCfg.ExistFile() {
				fmt.Fprintf(stdout, "Specified file: %s not exist, maybe invalid path or parameter settting.\n", cReadCfg.GetFileName())
				return 1
			}

			for !cReadCfg.EndOfFile() {
				nRd := cReadCfg.ReadLine(strTag, 4)
				if nRd > 0 {
					if strTag[0] == "InputFile" {
						strInputFile = strTag[1]
					} else if strTag[0] == "OutputFile" {
						strOutputFile = strTag[1]
					} else if strTag[0] == "RestructionFile" {
						strReconFile = strTag[1]
						sDecParam.PFileNameRestructed = strReconFile
					} else if strTag[0] == "TargetDQID" {
						sDecParam.UiTargetDqLayer = uint8(console.Atol(strTag[1]))
					} else if strTag[0] == "ErrorConcealmentIdc" {
						sDecParam.EEcActiveIdc = api.ERROR_CON_IDC(console.Atol(strTag[1]))
					} else if strTag[0] == "CPULoad" {
						sDecParam.UiCpuLoad = uint32(console.Atol(strTag[1]))
					} else if strTag[0] == "VideoBitstreamType" {
						sDecParam.SVideoProperty.EVideoBsType = api.VIDEO_BITSTREAM_TYPE(console.Atol(strTag[1]))
					}
				}
			}
			if strOutputFile == "" {
				stdout.WriteString("No output file specified in configuration file.\n")
				return 1
			}
		} else if strings.Contains(pArgV[1], ".264") { // no output dump yuv file, just try to render the decoded pictures
			strInputFile = pArgV[1]
			sDecParam.UiTargetDqLayer = 0xff // (uint8_t) - 1
			sDecParam.EEcActiveIdc = api.ERROR_CON_SLICE_COPY
			sDecParam.SVideoProperty.EVideoBsType = api.VIDEO_BITSTREAM_DEFAULT
		}
	} else { //iArgC > 2
		strInputFile = pArgV[1]
		strOutputFile = pArgV[2]
		sDecParam.UiTargetDqLayer = 0xff // (uint8_t) - 1
		sDecParam.EEcActiveIdc = api.ERROR_CON_SLICE_COPY
		sDecParam.SVideoProperty.EVideoBsType = api.VIDEO_BITSTREAM_DEFAULT
		if iArgC > 3 {
			for i := 3; i < iArgC; i++ {
				cmd := pArgV[i]

				if cmd == "-options" {
					if i+1 < iArgC {
						i++
						strOptionFile = pArgV[i]
					} else {
						stdout.WriteString("options file not specified.\n")
						return 1
					}
				} else if cmd == "-trace" {
					if i+1 < iArgC {
						i++
						iLevelSetting = console.Atoi(pArgV[i])
					} else {
						stdout.WriteString("trace level not specified.\n")
						return 1
					}
				} else if cmd == "-length" {
					if i+1 < iArgC {
						i++
						strLengthFile = pArgV[i]
					} else {
						stdout.WriteString("lenght file not specified.\n")
						return 1
					}
				} else if cmd == "-ec" {
					if i+1 < iArgC {
						i++
						iEcActiveIdc := console.Atoi(pArgV[i])
						sDecParam.EEcActiveIdc = api.ERROR_CON_IDC(iEcActiveIdc)
						fmt.Fprintf(stdout, "ERROR_CON(cealment) is set to %d.\n", iEcActiveIdc)
					}
				} else if cmd == "-legacy" {
					bLegacyCalling = true
				}
			}
		}

		if strOutputFile == "" {
			stdout.WriteString("No output file specified in configuration file.\n")
			return 1
		}
	}

	if strInputFile == "" {
		stdout.WriteString("No input file specified in configuration file.\n")
		return 1
	}

	if decoder.WelsCreateDecoder(&pDecoder) != 0 || nil == pDecoder {
		stdout.WriteString("Create Decoder failed.\n")
		return 1
	}
	if iLevelSetting >= 0 {
		pDecoder.SetOption(api.DECODER_OPTION_TRACE_LEVEL, &iLevelSetting)
	}

	iThreadCount := int32(0)
	pDecoder.SetOption(api.DECODER_OPTION_NUM_OF_THREADS, &iThreadCount)

	if pDecoder.Initialize(&sDecParam) != 0 {
		stdout.WriteString("Decoder initialization failed.\n")
		return 1
	}

	iWidth := int32(0)
	iHeight := int32(0)

	H264DecodeInstance(pDecoder, strInputFile, strOutputFile, &iWidth, &iHeight, strOptionFile, strLengthFile,
		int32(sDecParam.EEcActiveIdc), bLegacyCalling)

	if pDecoder != nil {
		pDecoder.Uninitialize()

		decoder.WelsDestroyDecoder(pDecoder)
	}

	return 0
}
