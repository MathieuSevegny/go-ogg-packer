package ogg_packer

/*
#cgo darwin CFLAGS: -I./opus
#include "ogg_opus_packer.h"
#include "ogg/ogg.h"
#include "opus/opus.h"
*/
import "C"
import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"time"
)

const (
	initBufferSize    = 4096
	maxFrameSize      = 5760
	defaultSampleRate = 48000
)

var resultCode C.int

type Buffer struct {
	Data    []byte
	ReadIdx uint64
}

type OggPacker struct {
	ChannelCount uint8
	SampleRate   uint32
	PacketNo     int64
	GranulePos   int64
	StreamState  *C.ogg_stream_state
	Buffer       Buffer
	OpusDecoder  *C.OpusDecoder
}

func NewPacker(channelCount uint8, sampleRate uint32) *OggPacker {
	buf := Buffer{
		Data:    make([]byte, 0, initBufferSize),
		ReadIdx: 0,
	}

	var streamState C.ogg_stream_state

	opusDecoder := C.opus_decoder_create(C.int(defaultSampleRate), C.int(channelCount), &resultCode)
	if resultCode == C.int(-1) {
		panic("opus decoder creation failed")
	}
	if opusDecoder == nil {
		panic("opusDecoder empty!")
	}

	p := OggPacker{
		ChannelCount: channelCount,
		SampleRate:   sampleRate,
		PacketNo:     1,
		GranulePos:   0,
		StreamState:  &streamState,
		Buffer:       buf,
		OpusDecoder:  opusDecoder,
	}

	serialNo := rand.New(rand.NewSource(time.Now().UTC().Unix() % 0x80000000)).Int31()
	if resultCode = C.ogg_stream_init(p.StreamState, C.int(serialNo)); resultCode == C.int(-1) {
		panic("ogg stream init failed")
	}

	if err := p.addHeader(); err != nil {
		panic(err.Error())
	}

	return &p
}

func (p *OggPacker) addHeader() error {
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
