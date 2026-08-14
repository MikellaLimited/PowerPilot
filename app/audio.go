//go:build windows

package main

import "encoding/binary"

var (
	soundCacheVolume   = -1
	soundClickScaled   []byte
	soundOpenScaled    []byte
	soundSuccessScaled []byte
)

func volumeAdjustedSound(data []byte, volume int) []byte {
	if volume >= 100 {
		return data
	}
	if volume <= 0 || len(data) < 44 {
		return nil
	}
	if soundCacheVolume != volume {
		soundCacheVolume = volume
		soundClickScaled = scalePCM16WAV(clickSound, volume)
		soundOpenScaled = scalePCM16WAV(openSound, volume)
		soundSuccessScaled = scalePCM16WAV(successSound, volume)
	}
	if sameSound(data, clickSound) {
		return soundClickScaled
	}
	if sameSound(data, openSound) {
		return soundOpenScaled
	}
	if sameSound(data, successSound) {
		return soundSuccessScaled
	}
	return scalePCM16WAV(data, volume)
}

func sameSound(a, b []byte) bool {
	return len(a) > 0 && len(b) > 0 && &a[0] == &b[0]
}

func scalePCM16WAV(src []byte, volume int) []byte {
	if len(src) < 44 {
		return src
	}
	out := append([]byte(nil), src...)
	dataStart, dataLen := wavDataChunk(out)
	if dataStart < 0 || dataLen < 2 {
		return out
	}
	gain := float64(volume) / 100.0
	end := dataStart + dataLen
	if end > len(out) {
		end = len(out)
	}
	for i := dataStart; i+1 < end; i += 2 {
		v := int16(binary.LittleEndian.Uint16(out[i : i+2]))
		nv := int(float64(v) * gain)
		if nv > 32767 {
			nv = 32767
		}
		if nv < -32768 {
			nv = -32768
		}
		binary.LittleEndian.PutUint16(out[i:i+2], uint16(int16(nv)))
	}
	return out
}

func wavDataChunk(b []byte) (int, int) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return -1, 0
	}
	p := 12
	for p+8 <= len(b) {
		id := string(b[p : p+4])
		n := int(binary.LittleEndian.Uint32(b[p+4 : p+8]))
		p += 8
		if n < 0 || p+n > len(b) {
			return -1, 0
		}
		if id == "data" {
			return p, n
		}
		p += n
		if p&1 == 1 {
			p++
		}
	}
	return -1, 0
}
