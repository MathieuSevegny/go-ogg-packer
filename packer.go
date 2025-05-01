package packer

/*
#cgo pkg-config: opus ogg
#cgo darwin CFLAGS: -I./opus
#include <stdlib.h>
#include "lib/ogg/ogg.h"
#include "lib/opus/opus.h"
*/
import "C"
import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"unsafe"
)

const (
	serialNo          = 99999 // const for testing similarity in active development phase. Should be `rand.New(rand.NewSource(time.Now().UTC().Unix() % 0x80000000)).Int31()` in real world.
	initBufferSize    = 4096
	maxFrameSize      = 5760
	defaultSampleRate = 48000
	successExitCode   = C.int(0)
	errorExitCode     = C.int(-1)
)

type Packer struct {
	channelCount uint8
	sampleRate   uint32
	packetNo     int64
	granulePos   int64
	streamState  *C.ogg_stream_state
	buffer       []byte
	opusDecoder  *C.OpusDecoder
}

func New(channelCount uint8, sampleRate uint32) (*Packer, error) {
	p := Packer{
		channelCount: channelCount,
		sampleRate:   sampleRate,
		packetNo:     1,
		granulePos:   0,
		streamState:  nil,
		buffer:       nil,
		opusDecoder:  nil,
	}

	if err := p.init(); err != nil {
		return nil, fmt.Errorf("init ogg packer: %w", err)
	}

	runtime.SetFinalizer(&p, (*Packer).Close)

	return &p, nil
}

func (p *Packer) AddChunk(data []byte, eos bool, samplesCount int) error {
	bufLen := maxFrameSize * int16(p.channelCount) // here should be array as in C sources?
	cBufLen := C.short(bufLen)

	var numSamplesPerChannel C.int
	if samplesCount < 0 {
		numSamplesPerChannel = C.opus_decode(
			p.opusDecoder,
			(*C.uchar)(unsafe.Pointer(&data[0])),
			C.int(len(data)),
			&cBufLen,
			maxFrameSize,
			0,
		)
		if numSamplesPerChannel < 0 {
			return errors.New("count number of sampler per channel failed")
		}
	} else {
		numSamplesPerChannel = C.int(uint32(samplesCount*defaultSampleRate) / (p.sampleRate * uint32(p.channelCount)))
	}

	if err := p.sendPacketToOggStream(data, false, eos); err != nil {
		return fmt.Errorf("send header data to ogg stream: %w", err)
	}

	p.granulePos += int64(numSamplesPerChannel)

	return nil
}

func (p *Packer) ReadPages() ([]byte, error) {
	page := (*C.ogg_page)(C.malloc(C.sizeof_ogg_page))
	defer C.free(unsafe.Pointer(page))

	for {
		exitCode := C.ogg_stream_pageout(p.streamState, page)
		if exitCode == errorExitCode {
			return nil, errors.New("read pages from ogg stream failed")
		}
		if exitCode == successExitCode {
			break
		}

		if err := p.addBuffer(page); err != nil {
			return nil, fmt.Errorf("add page to buffer: %w", err)
		}
	}

	return p.readBuffer(), nil
}

func (p *Packer) FlushPages() ([]byte, error) {
	page := (*C.ogg_page)(C.malloc(C.sizeof_ogg_page))
	defer C.free(unsafe.Pointer(page))

	for {
		exitCode := C.ogg_stream_flush(p.streamState, page)
		if exitCode == errorExitCode {
			return nil, errors.New("flush pages from ogg stream failed")
		}
		if exitCode == successExitCode {
			break
		}

		if err := p.addBuffer(page); err != nil {
			return nil, fmt.Errorf("add page to buffer: %w", err)
		}
	}

	return p.readBuffer(), nil
}

func (p *Packer) init() error {
	var streamState C.ogg_stream_state
	p.streamState = &streamState

	var exitCode C.int
	opusDecoder := C.opus_decoder_create(C.int(defaultSampleRate), C.int(p.channelCount), &exitCode)
	if exitCode == errorExitCode || opusDecoder == nil {
		return errors.New("create opus decoder failed")
	}
	p.opusDecoder = opusDecoder

	if exitCode := C.ogg_stream_init(p.streamState, C.int(serialNo)); exitCode == errorExitCode {
		return errors.New("ogg stream init failed")
	}

	if err := p.addHeader(); err != nil {
		return fmt.Errorf("add header to ogg stream: %w", err)
	}

	if err := p.streamFlush(); err != nil {
		return fmt.Errorf("stream flush: %w", err)
	}

	if err := p.addTags(); err != nil {
		return fmt.Errorf("add tags packet: %w", err)
	}

	if err := p.streamFlush(); err != nil {
		return fmt.Errorf("stream flush: %w", err)
	}

	return nil
}

func (p *Packer) addHeader() error {
	header := header(p.channelCount, p.sampleRate)
	if err := p.sendPacketToOggStream(header, true, false); err != nil {
		return fmt.Errorf("send header data to ogg stream: %w", err)
	}

	return nil
}

func (p *Packer) addTags() error {
	tags := make([]byte, 9)
	copy(tags, []byte("OpusTags"))

	if err := p.sendPacketToOggStream(tags, false, false); err != nil {
		return fmt.Errorf("send header data to ogg stream: %w", err)
	}

	return nil
}

func (p *Packer) streamFlush() error {
	page := (*C.ogg_page)(C.malloc(C.sizeof_ogg_page))
	defer C.free(unsafe.Pointer(page))

	for {
		exitCode := C.ogg_stream_flush(p.streamState, page)
		if exitCode == errorExitCode {
			return errors.New("c-level flush ogg stream failed")
		}
		if exitCode == successExitCode {
			break
		}

		if err := p.addBuffer(page); err != nil {
			return fmt.Errorf("add page to buffer: %w", err)
		}
	}

	return nil
}

func (p *Packer) Close() {
	if p.opusDecoder != nil {
		C.opus_decoder_destroy(p.opusDecoder)
		p.opusDecoder = nil
	}

	C.ogg_stream_clear(p.streamState)
	p.streamState = nil

	p.buffer = nil

	runtime.SetFinalizer(&p, nil)
}

func (p *Packer) addBuffer(page *C.ogg_page) error {
	var header []byte
	var body []byte

	if page.header_len == 0 {
		return errors.New("header length should be > 0")
	}

	header = C.GoBytes(unsafe.Pointer(page.header), C.int(page.header_len))
	p.buffer = append(p.buffer, header...)

	if page.body_len == 0 {
		return errors.New("body length should be > 0")
	}

	body = C.GoBytes(unsafe.Pointer(page.body), C.int(page.body_len))
	p.buffer = append(p.buffer, body...)

	return nil
}

func (p *Packer) readBuffer() []byte {
	b := p.buffer
	p.buffer = make([]byte, 0, initBufferSize)
	return b
}

// sendPacketToOggStream sends data to ogg stream in ogg packet format
// bos - begin of stream flag
// eos - end of stream flag
func (p *Packer) sendPacketToOggStream(data []byte, bos bool, eos bool) error {
	var (
		bosInt int8
		eosInt int8
	)
	if bos {
		bosInt = 1
	}
	if eos {
		eosInt = 1
	}

	cData := C.malloc(C.size_t(len(data)))
	defer C.free(cData)

	ptr := (*[1 << 30]byte)(cData)[:len(data):len(data)]
	copy(ptr, data)

	var packet C.ogg_packet
	packet.packet = (*C.uchar)(cData)
	packet.bytes = C.long(len(data))
	packet.b_o_s = C.long(bosInt)
	packet.e_o_s = C.long(eosInt)
	packet.granulepos = C.ogg_int64_t(p.granulePos)
	packet.packetno = C.ogg_int64_t(p.packetNo)
	p.packetNo++

	if exitCode := C.ogg_stream_packetin(p.streamState, &packet); exitCode == errorExitCode {
		return errors.New("add packet to ogg stream failed")
	}

	return nil
}

func header(channelCount uint8, sampleRate uint32) []byte {
	header := make([]byte, 19)
	copy(header, []byte("OpusHead"))

	header[8] = 1 // version number
	header[9] = channelCount

	binary.LittleEndian.PutUint16(header[10:12], 0)
	binary.LittleEndian.PutUint32(header[12:16], sampleRate)
	binary.LittleEndian.PutUint16(header[16:18], 0)

	header[18] = 0

	return header
}
