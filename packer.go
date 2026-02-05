package packer

import (
	"fmt"

	"github.com/paveldroo/go-ogg-packer/ogg"
	"github.com/paveldroo/go-ogg-packer/opus"
)

type Packer struct {
	result      []byte
	opusEncoder *opus.Encoder
	oggPacker   *ogg.Packer
	pcmBuffer   []int16
}

func New() (*Packer, error) {
	cfg := opus.NewDefaultConfig()
	encoder, err := opus.NewEncoder(cfg)
	if err != nil {
		return nil, fmt.Errorf("create opus encoder: %s", err)
	}

	packer, err := ogg.New(uint8(cfg.NumChannels), uint32(cfg.SampleRate))
	if err != nil {
		return nil, fmt.Errorf("create ogg packer: %w", err)
	}

	return &Packer{
		opusEncoder: encoder,
		oggPacker:   packer,
	}, nil
}

func (s *Packer) SendPCMChunk(chunk []int16) error {
	s.pcmBuffer = append(s.pcmBuffer, chunk...)
	currentOpusPackets, pos, err := s.opusEncoder.Encode(s.pcmBuffer)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	s.pcmBuffer = s.pcmBuffer[pos:]
	for _, opusPacket := range currentOpusPackets {
		if err := s.oggPacker.AddChunk(opusPacket, false, pos); err != nil {
			return fmt.Errorf("add chunk: %w", err)
		}
	}
	return nil
}

func (s *Packer) GetResult() ([]byte, error) {
	defer s.oggPacker.Close()

	if err := s.flushPCMBuffer(); err != nil {
		return nil, fmt.Errorf("flush buffer: %w", err)
	}

	// Insert a skeleton track packet with the total duration before finalizing.
	if dur := s.oggPacker.Duration(); dur > 0 {
		if err := s.oggPacker.AddSkeleton(dur); err != nil {
			return nil, fmt.Errorf("add skeleton packet: %w", err)
		}
	}

	// Now write EOS for the stream (use an empty packet with samplesCount=0).
	if err := s.oggPacker.AddChunk([]byte{}, true, 0); err != nil {
		return nil, fmt.Errorf("write eos packet: %w", err)
	}

	oggPages, err := s.oggPacker.ReadPages()
	if err != nil {
		return nil, fmt.Errorf("read pages: %w", err)
	}

	s.result = oggPages

	return s.result, nil
}

func (s *Packer) flushPCMBuffer() error {
	defer func() {
		s.pcmBuffer = s.pcmBuffer[:0]
	}()

	if len(s.pcmBuffer) == 0 {
		return nil
	}

	opusPackets, err := s.opusEncoder.EncodeWithPadding(s.pcmBuffer)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	for _, opusPacket := range opusPackets {
		if err := s.oggPacker.AddChunk(opusPacket, false, -1); err != nil {
			return fmt.Errorf("add chunk: %w", err)
		}
	}

	return nil
}
