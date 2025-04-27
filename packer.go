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
	"fmt"
	"unsafe"
)

const (
	serialNo          = 99999 // const for testing similarity in active development phase. Should be `rand.New(rand.NewSource(time.Now().UTC().Unix() % 0x80000000)).Int31()` in real world.
	initBufferSize    = 4096
	maxFrameSize      = 5760
	defaultSampleRate = 48000
)

var resultCode C.int

type Packer struct {
	ChannelCount uint8
	SampleRate   uint32
	PacketNo     int64
	GranulePos   int64
	StreamState  *C.ogg_stream_state
	Buffer       []byte
	OpusDecoder  *C.OpusDecoder
}

func NewPacker(channelCount uint8, sampleRate uint32) *Packer {
	var streamState C.ogg_stream_state

	opusDecoder := C.opus_decoder_create(C.int(defaultSampleRate), C.int(channelCount), &resultCode)
	if resultCode == C.int(-1) {
		panic("opus decoder creation failed")
	}
	if opusDecoder == nil {
		panic("opusDecoder empty!")
	}

	p := Packer{
		ChannelCount: channelCount,
		SampleRate:   sampleRate,
		PacketNo:     1,
		GranulePos:   0,
		StreamState:  &streamState,
		Buffer:       make([]byte, 0, initBufferSize),
		OpusDecoder:  opusDecoder,
	}

	if resultCode = C.ogg_stream_init(p.StreamState, C.int(serialNo)); resultCode == C.int(-1) {
		panic("ogg stream init failed")
	}

	if err := p.addHeader(); err != nil {
		panic(err.Error())
	}

	p.streamFlush()
	p.addTags()
	p.streamFlush()

	return &p
}

func (p *Packer) AddChunk(data []byte, eos bool, samplesCount int) error {
	eosNumber := 0 // not end of stream
	if eos {
		eosNumber = 1 // end of stream
	}

	bufLen := maxFrameSize * int16(p.ChannelCount)
	cBufLen := C.short(bufLen)

	var numSamplesPerChannel C.int
	if samplesCount < 0 {
		numSamplesPerChannel = C.opus_decode(
			p.OpusDecoder,
			(*C.uchar)(unsafe.Pointer(&data[0])),
			C.int(len(data)),
			&cBufLen,
			maxFrameSize,
			0,
		)
		if numSamplesPerChannel < 0 {
			panic("failed to count numSamplesPerChannel")
		}
	} else {
		numSamplesPerChannel = C.int(uint32(samplesCount*defaultSampleRate) / (p.SampleRate * uint32(p.ChannelCount)))
	}

	cData := C.malloc(C.size_t(len(data)))
	defer C.free(cData)

	ptr := (*[1 << 30]byte)(cData)[:len(data):len(data)]
	copy(ptr, data)

	var packet C.ogg_packet
	packet.packet = (*C.uchar)(cData)
	packet.bytes = C.long(len(data))
	packet.b_o_s = C.long(0)
	packet.e_o_s = C.long(eosNumber)
	packet.granulepos = C.ogg_int64_t(p.GranulePos)
	packet.packetno = C.ogg_int64_t(p.PacketNo)
	p.PacketNo++
	p.GranulePos += int64(numSamplesPerChannel)

	if resultCode = C.ogg_stream_packetin(p.StreamState, &packet); resultCode == C.int(-1) {
		return fmt.Errorf("failed to add chunk packet to ogg stream")
	}

	return nil
}

func (p *Packer) addHeader() error {
	header := header(p.ChannelCount, p.SampleRate)
	cHeader := C.malloc(C.size_t(len(header)))
	defer C.free(cHeader)

	ptr := (*[1 << 30]byte)(cHeader)[:len(header):len(header)]
	copy(ptr, header)

	var packet C.ogg_packet
	packet.packet = (*C.uchar)(cHeader)
	packet.bytes = C.long(len(header))
	packet.b_o_s = C.long(1) // Beginning of stream
	packet.e_o_s = C.long(0) // Not end of stream
	packet.granulepos = C.ogg_int64_t(0)
	packet.packetno = C.ogg_int64_t(p.PacketNo)
	p.PacketNo++

	if resultCode = C.ogg_stream_packetin(p.StreamState, &packet); resultCode == C.int(-1) {
		return fmt.Errorf("failed to add header packet to ogg stream")
	}

	return nil
}

func (p *Packer) addTags() error {
	tags := make([]byte, 9)
	copy(tags, []byte("OpusTags"))

	cTags := C.malloc(C.size_t(len(tags)))
	defer C.free(cTags)

	ptr := (*[1 << 30]byte)(cTags)[:len(tags):len(tags)]
	copy(ptr, tags)

	var packet C.ogg_packet
	packet.packet = (*C.uchar)(cTags)
	packet.bytes = C.long(len(tags))
	packet.b_o_s = C.long(0)
	packet.e_o_s = C.long(0) // Not end of stream
	packet.granulepos = C.ogg_int64_t(0)
	packet.packetno = C.ogg_int64_t(p.PacketNo)
	p.PacketNo++

	if resultCode = C.ogg_stream_packetin(p.StreamState, &packet); resultCode == C.int(-1) {
		return fmt.Errorf("failed to add tags packet to ogg stream")
	}

	return nil
}

func (p *Packer) ReadPages() ([]byte, error) {
	page := (*C.ogg_page)(C.malloc(C.sizeof_ogg_page))
	defer C.free(unsafe.Pointer(page))

	for {
		resultCode = C.ogg_stream_pageout(p.StreamState, page)
		if resultCode == C.int(-1) {
			panic("ogg read pages failed")
		}
		if resultCode == C.int(0) {
			break
		}

		p.addBuffer(page)
	}

	return p.readBuffer(), nil
}

func (p *Packer) FlushPages() ([]byte, error) {
	page := (*C.ogg_page)(C.malloc(C.sizeof_ogg_page))
	defer C.free(unsafe.Pointer(page))

	for {
		resultCode = C.ogg_stream_flush(p.StreamState, page)
		if resultCode == C.int(-1) {
			panic("flush pages failed")
		}
		if resultCode == C.int(0) {
			break
		}

		p.addBuffer(page)
	}

	return p.readBuffer(), nil
}

func (p *Packer) streamFlush() {
	page := (*C.ogg_page)(C.malloc(C.sizeof_ogg_page))
	defer C.free(unsafe.Pointer(page))

	for {
		resultCode = C.ogg_stream_flush(p.StreamState, page)
		if resultCode == C.int(-1) {
			panic("ogg stream flush failed")
		}
		if resultCode == C.int(0) {
			break
		}

		p.addBuffer(page)
	}
}

func (p *Packer) Close() {
	// Some optimizer code for destroying objects
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

func (p *Packer) addBuffer(page *C.ogg_page) {
	var header []byte
	var body []byte
	if page.header_len > 0 {
		header = C.GoBytes(unsafe.Pointer(page.header), C.int(page.header_len))
	}
	if page.body_len > 0 {
		body = C.GoBytes(unsafe.Pointer(page.body), C.int(page.body_len))
	}
	p.Buffer = append(p.Buffer, header...)
	p.Buffer = append(p.Buffer, body...)
}

func (p *Packer) readBuffer() []byte {
	d := p.Buffer
	p.Buffer = make([]byte, 0, initBufferSize)
	return d
}
