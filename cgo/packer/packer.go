package packer

/*
#cgo pkg-config: opus ogg
#cgo darwin CFLAGS: -I./opus
#include "../lib/ogg_packer/ogg_opus_packer.h"
#include "../lib/ogg_packer/ogg/ogg.h"
#include "../lib/ogg_packer/opus/opus.h"
*/
import "C"

const initBufferSize = 4096

type Buffer struct {
	Data    []byte
	ReadIdx uint64
}

type Packer struct {
	ChannelCount uint8
	SampleRate   uint32
	PacketNo     int64
	GranulePos   int64
	StreamState  *C.ogg_stream_state
	Buffer       Buffer
	OpusDecoder  *C.OpusDecoder
}

func NewPacker() *Packer {
	buf := Buffer{
		Data:    make([]byte, 0, initBufferSize),
		ReadIdx: 0,
	}

	return &Packer{
		Buffer: buf,
	}
}
