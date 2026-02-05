package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"log"

	extopus "gopkg.in/hraban/opus.v2"
	extogg "mccoy.space/g/ogg"

	packer "github.com/paveldroo/go-ogg-packer"
	"github.com/paveldroo/go-ogg-packer/opus"
)

func readPCM(fn string) []int16 {
	d, err := ioutil.ReadFile(fn)
	if err != nil {
		log.Fatalf("read pcm: %s", err)
	}
	r := bytes.NewReader(d)
	res := make([]int16, len(d)/2)
	for i := range res {
		var v int16
		binary.Read(r, binary.LittleEndian, &v)
		res[i] = v
	}
	return res
}

func main() {
	src := readPCM("testdata/48k_1ch.pcm")
	ref := readPCM("testdata/want/48k_1ch.pcm")

	p, err := packer.New()
	if err != nil {
		log.Fatal(err)
	}

	for i := 0; i < len(src); i++ {
		end := i + 2048
		if end > len(src) {
			end = len(src)
		}
		if err := p.SendPCMChunk(src[i:end]); err != nil {
			log.Fatal(err)
		}
		i = end
	}

	oggData, err := p.GetResult()
	if err != nil {
		log.Fatal(err)
	}

	// decode
	b := bytes.NewBuffer(oggData)
	og := extogg.NewDecoder(b)
	od, _ := extopus.NewDecoder(opus.SampleRate, opus.NumChannels)
	pcmBuf := make([]int16, opus.FrameSize*opus.SampleRate*opus.NumChannels/1000)
	var got []int16
	for {
		page, err := og.Decode()
		if err != nil {
			break
		}
		for _, packet := range page.Packets {
			n, err := od.Decode(packet, pcmBuf)
			if err != nil {
				continue
			}
			got = append(got, pcmBuf[:n]...)
		}
	}

	fmt.Printf("ref len=%d got len=%d\n", len(ref), len(got))
	d := 0.0
	if len(ref) == len(got) {
		for i := range ref {
			delta := float64(int(ref[i]) - int(got[i]))
			d += delta * delta
		}
		d /= float64(len(ref))
	}
	fmt.Printf("mse=%f\n", d)
	fmt.Printf("first 20 ref: %v\n", ref[:20])
	fmt.Printf("first 20 got: %v\n", got[:20])
}
