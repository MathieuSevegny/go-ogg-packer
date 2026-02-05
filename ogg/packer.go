package ogg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"time"

	opus "gopkg.in/hraban/opus.v2"
)

const (
	serialNo       = 99999 // const for testing similarity in active development phase. Should be `rand.New(rand.NewSource(time.Now().UTC().Unix() % 0x80000000)).Int31()` in real world
	initBufferSize = 4096
	maxFrameSize   = 5760
)

type Packer struct {
	channelCount uint8
	sampleRate   uint32
	packetNo     int64
	granulePos   int64
	buffer       bytes.Buffer
	oggEncoder   *Encoder
	opusDecoder  *opus.Decoder
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
	var numSamplesPerChannel int
	if samplesCount < 0 {
		var err error
		buf := make([]int16, maxFrameSize*int16(p.channelCount))

		numSamplesPerChannel, err = p.opusDecoder.Decode(data, buf)
		if err != nil {
			return fmt.Errorf("decode chunk data with opus decoder: %w", err)
		}
	} else {
		numSamplesPerChannel = int(samplesCount) / int(p.channelCount)
	}

	p.granulePos += int64(numSamplesPerChannel)

	if err := p.sendPacketToOggStream(data, false, eos); err != nil {
		return fmt.Errorf("send header data to ogg stream: %w", err)
	}

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
	p.oggEncoder = NewEncoder(serialNo, &p.buffer)

	d, err := opus.NewDecoder(int(p.sampleRate), int(p.channelCount))
	if err != nil {
		return fmt.Errorf("create opus decoder: %w", err)
	}
	p.opusDecoder = d

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
	p.opusDecoder = nil
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

// CreateSkeletonTrack builds a minimal skeleton-style packet containing
// a simple identifier and the duration in nanoseconds (little-endian).
// The returned byte slice can be used as a packet payload in an Ogg stream.
func CreateSkeletonTrack(d time.Duration) []byte {
	// Layout: 80 bytes total as specified in the provided diagram.
	// Identifier 'fishead\0' (8 bytes), followed by version fields,
	// presentation time numerator/denominator, basetime fields, UTC,
	// reserved space, then segment length and content byte offset.
	const prefix = "fishead"
	b := make([]byte, 80)
	copy(b, []byte(prefix))

	// Ensure null termination at byte 7 (copy leaves it zero if prefix is 7 bytes)

	// Version major (uint16) and version minor (uint16) at bytes 8-11
	binary.LittleEndian.PutUint16(b[8:10], 1)  // major
	binary.LittleEndian.PutUint16(b[10:12], 0) // minor

	// Presentation time: store as 32-bit numerator/denominator in
	// little-endian (use microseconds to avoid overflowing 32-bit).
	binary.LittleEndian.PutUint32(b[12:16], uint32(d.Microseconds()))
	// bytes 16-19 left zero per layout
	binary.LittleEndian.PutUint32(b[20:24], uint32(1000000))

	// Basetime numerator/denominator left as zero at 28-35 and 36-43

	// UTC (seconds since epoch) as 32-bit at bytes 44-47
	binary.LittleEndian.PutUint32(b[44:48], uint32(time.Now().Unix()))

	// Segment length in bytes at 64-67 (leave 0)
	// Content byte offset at 72-75 (leave 0)

	return b
}

// Duration returns the duration of the audio accumulated in the packer.
func (p *Packer) Duration() time.Duration {
	if p.sampleRate == 0 {
		return 0
	}
	return time.Duration(p.granulePos) * time.Second / time.Duration(p.sampleRate)
}

// AddSkeleton creates a skeleton packet for the provided duration and
// inserts it into the ogg stream as a regular packet.
func (p *Packer) AddSkeleton(d time.Duration) error {
	pkt := CreateSkeletonTrack(d)
	if err := p.sendPacketToOggStream(pkt, false, false); err != nil {
		return fmt.Errorf("send skeleton packet to ogg stream: %w", err)
	}
	return nil
}
