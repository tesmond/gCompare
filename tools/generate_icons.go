package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
)

type icnsEntry struct {
	code string
	path string
}

type icoEntry struct {
	size uint8
	path string
}

func main() {
	if err := generateICNS("build/darwin/iconfile.icns", []icnsEntry{
		{"icp4", "build/darwin/iconfile.iconset/icon_16x16.png"},
		{"ic11", "build/darwin/iconfile.iconset/icon_16x16@2x.png"},
		{"icp5", "build/darwin/iconfile.iconset/icon_32x32.png"},
		{"ic12", "build/darwin/iconfile.iconset/icon_32x32@2x.png"},
		{"ic07", "build/darwin/iconfile.iconset/icon_128x128.png"},
		{"ic13", "build/darwin/iconfile.iconset/icon_128x128@2x.png"},
		{"ic08", "build/darwin/iconfile.iconset/icon_256x256.png"},
		{"ic14", "build/darwin/iconfile.iconset/icon_256x256@2x.png"},
		{"ic09", "build/darwin/iconfile.iconset/icon_512x512.png"},
		{"ic10", "build/darwin/iconfile.iconset/icon_512x512@2x.png"},
	}); err != nil {
		fatal(err)
	}

	if err := generateICO("build/windows/icon.ico", []icoEntry{
		{16, "build/darwin/iconfile.iconset/icon_16x16.png"},
		{32, "build/darwin/iconfile.iconset/icon_32x32.png"},
		{48, "build/linux/48x48/apps/gcompare.png"},
		{64, "build/darwin/iconfile.iconset/icon_32x32@2x.png"},
		{128, "build/darwin/iconfile.iconset/icon_128x128.png"},
		{0, "build/darwin/iconfile.iconset/icon_256x256.png"},
	}); err != nil {
		fatal(err)
	}
}

func generateICNS(path string, entries []icnsEntry) error {
	var body bytes.Buffer
	for _, entry := range entries {
		data, err := os.ReadFile(entry.path)
		if err != nil {
			return err
		}
		if _, err := body.WriteString(entry.code); err != nil {
			return err
		}
		if err := binary.Write(&body, binary.BigEndian, uint32(len(data)+8)); err != nil {
			return err
		}
		if _, err := body.Write(data); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	var out bytes.Buffer
	if _, err := out.WriteString("icns"); err != nil {
		return err
	}
	if err := binary.Write(&out, binary.BigEndian, uint32(body.Len()+8)); err != nil {
		return err
	}
	if _, err := out.Write(body.Bytes()); err != nil {
		return err
	}
	return os.WriteFile(path, out.Bytes(), 0644)
}

func generateICO(path string, entries []icoEntry) error {
	type directoryEntry struct {
		width      uint8
		height     uint8
		colorCount uint8
		reserved   uint8
		planes     uint16
		bitCount   uint16
		size       uint32
		offset     uint32
		data       []byte
	}

	dirEntries := make([]directoryEntry, 0, len(entries))
	offset := uint32(6 + len(entries)*16)
	for _, entry := range entries {
		data, err := os.ReadFile(entry.path)
		if err != nil {
			return err
		}
		dirEntries = append(dirEntries, directoryEntry{
			width:    entry.size,
			height:   entry.size,
			planes:   1,
			bitCount: 32,
			size:     uint32(len(data)),
			offset:   offset,
			data:     data,
		})
		offset += uint32(len(data))
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	var out bytes.Buffer
	if err := binary.Write(&out, binary.LittleEndian, uint16(0)); err != nil {
		return err
	}
	if err := binary.Write(&out, binary.LittleEndian, uint16(1)); err != nil {
		return err
	}
	if err := binary.Write(&out, binary.LittleEndian, uint16(len(dirEntries))); err != nil {
		return err
	}
	for _, entry := range dirEntries {
		out.WriteByte(entry.width)
		out.WriteByte(entry.height)
		out.WriteByte(entry.colorCount)
		out.WriteByte(entry.reserved)
		if err := binary.Write(&out, binary.LittleEndian, entry.planes); err != nil {
			return err
		}
		if err := binary.Write(&out, binary.LittleEndian, entry.bitCount); err != nil {
			return err
		}
		if err := binary.Write(&out, binary.LittleEndian, entry.size); err != nil {
			return err
		}
		if err := binary.Write(&out, binary.LittleEndian, entry.offset); err != nil {
			return err
		}
	}
	for _, entry := range dirEntries {
		if _, err := out.Write(entry.data); err != nil {
			return err
		}
	}
	return os.WriteFile(path, out.Bytes(), 0644)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
