package packer

/*
#cgo pkg-config: opus
#cgo darwin CFLAGS: -I./opus
#include <stdlib.h>
#include "lib/opus/opus.h"
*/
import "C"
import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"mccoy.space/g/ogg"
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
	buffer       bytes.Buffer
	oggEncoder   *ogg.Encoder
	opusDecoder  *C.OpusDecoder
}

func New(channelCount uint8, sampleRate uint32) (*Packer, error) {
	p := Packer{
		channelCount: channelCount,
		sampleRate:   sampleRate,
		packetNo:     1,
		granulePos:   0,
		buffer:       bytes.Buffer{},
		oggEncoder:   nil,
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
	b := p.buffer.Bytes()
	if len(b) == 0 {
		return nil, errors.New("received empty ogg data buffer")
	}
	p.buffer.Reset()
	return b, nil
}

func (p *Packer) init() error {
	p.oggEncoder = ogg.NewEncoder(serialNo, &p.buffer)

	var exitCode C.int
	opusDecoder := C.opus_decoder_create(C.int(defaultSampleRate), C.int(p.channelCount), &exitCode)
	if exitCode == errorExitCode || opusDecoder == nil {
		return errors.New("create opus decoder failed")
	}
	p.opusDecoder = opusDecoder

	if err := p.addHeader(); err != nil {
		return fmt.Errorf("add header to ogg stream: %w", err)
	}

	if err := p.addTags(); err != nil {
		return fmt.Errorf("add tags packet: %w", err)
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

func (p *Packer) Close() {
	if p.opusDecoder != nil {
		C.opus_decoder_destroy(p.opusDecoder)
		p.opusDecoder = nil
	}

	p.oggEncoder = nil
	p.buffer.Reset()

	runtime.SetFinalizer(&p, nil)
}

// sendPacketToOggStream sends data to ogg stream in ogg packet format
// bos - begin of stream flag
// eos - end of stream flag
func (p *Packer) sendPacketToOggStream(data []byte, bos bool, eos bool) error {
	if bos {
		if err := p.oggEncoder.EncodeBOS(p.granulePos, [][]byte{data}); err != nil {
			return fmt.Errorf("write begin of stream packets to ogg stream: %w", err)
		}
		return nil

	}
	if eos {
		if err := p.oggEncoder.EncodeEOS(p.granulePos, [][]byte{data}); err != nil {
			return fmt.Errorf("write end of stream packets to ogg stream: %w", err)
		}
		return nil
	}

	if err := p.oggEncoder.Encode(p.granulePos, [][]byte{data}); err != nil {
		return fmt.Errorf("write packets to ogg stream: %w", err)
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
