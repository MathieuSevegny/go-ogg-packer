package ogg_packer

import (
	"fmt"
	"testing"
)

func TestOggPacker(t *testing.T) {
	p := NewPacker(1, 48000)
	fmt.Println(p)
}
