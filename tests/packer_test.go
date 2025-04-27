package tests

import (
	"fmt"
	"testing"
	"time"

	packer "github.com/paveldroo/go-ogg-packer"
	writer "github.com/paveldroo/go-ogg-packer/buffer_writer"
	"github.com/paveldroo/go-ogg-packer/buffer_writer/opus_tools"
)

func TestOggPacker(t *testing.T) {
	converter, err := opus_tools.NewOpusConverter(opus_tools.NewDefaultConfig())
	if err != nil {
		t.Fatalf("create opus converter: %s", err.Error())
	}

	packer := packer.NewPacker(1, sampleRate)
	if err != nil {
		t.Fatalf("create ogg packer wrapper: %s", err.Error())
	}

	s16 := S16FromWav()
	audioBuffer := writer.NewAudioBuffer(converter, packer)

	for i := 0; i < len(s16); i++ {
		end := i + 2048
		if end > len(s16) {
			end = len(s16)
		}
		audioBuffer.SendS16Chunk(s16[i:end])
		i = end
	}

	audioContent, err := audioBuffer.GetResult()
	if err != nil {
		t.Fatalf("get result from audio buffer: %s", err.Error())
	}

	mustWriteOpusFile(fmt.Sprintf("testdata/result/ogg_packer_result_%d.opus", time.Now().UnixNano()), audioContent)
}
