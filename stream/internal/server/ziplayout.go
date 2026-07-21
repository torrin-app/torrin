package server

import (
	"encoding/binary"
	"io"
)

const (
	zipLocalSig   = 0x04034b50
	zipCentralSig = 0x02014b50
	zip64EOCDSig  = 0x06064b50
	zip64LocSig   = 0x07064b50
	zipEOCDSig    = 0x06054b50
	zipUTF8Flag   = 0x0800
	zip64ID       = 0x0001
	zip64Marker   = 0xffffffff
	dosEpochDate  = 0x0021
)

type zipEntry struct {
	name string
	size int64
	crc  uint32
}

type zipSeg struct {
	off     int64
	size    int64
	hdr     []byte
	fileIdx int
}

type zipLayout struct {
	segs  []zipSeg
	total int64
}

func le16(b []byte, v uint16) []byte { return binary.LittleEndian.AppendUint16(b, v) }
func le32(b []byte, v uint32) []byte { return binary.LittleEndian.AppendUint32(b, v) }
func le64(b []byte, v uint64) []byte { return binary.LittleEndian.AppendUint64(b, v) }

func localHeader(e zipEntry) []byte {
	z64 := e.size >= zip64Marker
	name := []byte(e.name)
	b := le32(nil, zipLocalSig)
	if z64 {
		b = le16(b, 45)
	} else {
		b = le16(b, 20)
	}
	b = le16(b, zipUTF8Flag)
	b = le16(b, 0)
	b = le16(b, 0)
	b = le16(b, dosEpochDate)
	b = le32(b, e.crc)
	if z64 {
		b = le32(b, zip64Marker)
		b = le32(b, zip64Marker)
	} else {
		b = le32(b, uint32(e.size))
		b = le32(b, uint32(e.size))
	}
	b = le16(b, uint16(len(name)))
	if z64 {
		b = le16(b, 20)
	} else {
		b = le16(b, 0)
	}
	b = append(b, name...)
	if z64 {
		b = le16(b, zip64ID)
		b = le16(b, 16)
		b = le64(b, uint64(e.size))
		b = le64(b, uint64(e.size))
	}
	return b
}

func centralHeader(e zipEntry, localOff int64) []byte {
	z64size := e.size >= zip64Marker
	z64off := localOff >= zip64Marker
	z64 := z64size || z64off
	name := []byte(e.name)

	var extra []byte
	if z64 {
		var body []byte
		if z64size {
			body = le64(body, uint64(e.size))
			body = le64(body, uint64(e.size))
		}
		if z64off {
			body = le64(body, uint64(localOff))
		}
		extra = le16(nil, zip64ID)
		extra = le16(extra, uint16(len(body)))
		extra = append(extra, body...)
	}

	b := le32(nil, zipCentralSig)
	b = le16(b, 45)
	if z64 {
		b = le16(b, 45)
	} else {
		b = le16(b, 20)
	}
	b = le16(b, zipUTF8Flag)
	b = le16(b, 0)
	b = le16(b, 0)
	b = le16(b, dosEpochDate)
	b = le32(b, e.crc)
	if z64size {
		b = le32(b, zip64Marker)
		b = le32(b, zip64Marker)
	} else {
		b = le32(b, uint32(e.size))
		b = le32(b, uint32(e.size))
	}
	b = le16(b, uint16(len(name)))
	b = le16(b, uint16(len(extra)))
	b = le16(b, 0)
	b = le16(b, 0)
	b = le16(b, 0)
	b = le32(b, 0)
	if z64off {
		b = le32(b, zip64Marker)
	} else {
		b = le32(b, uint32(localOff))
	}
	b = append(b, name...)
	b = append(b, extra...)
	return b
}

func endRecords(entries int, cdSize, cdOff int64) []byte {
	z64 := cdOff >= zip64Marker || cdSize >= zip64Marker
	var b []byte
	if z64 {
		z64Off := cdOff + cdSize
		b = le32(b, zip64EOCDSig)
		b = le64(b, 44)
		b = le16(b, 45)
		b = le16(b, 45)
		b = le32(b, 0)
		b = le32(b, 0)
		b = le64(b, uint64(entries))
		b = le64(b, uint64(entries))
		b = le64(b, uint64(cdSize))
		b = le64(b, uint64(cdOff))
		b = le32(b, zip64LocSig)
		b = le32(b, 0)
		b = le64(b, uint64(z64Off))
		b = le32(b, 1)
	}
	cnt := uint16(entries)
	if entries >= 0xffff {
		cnt = 0xffff
	}
	b = le32(b, zipEOCDSig)
	b = le16(b, 0)
	b = le16(b, 0)
	b = le16(b, cnt)
	b = le16(b, cnt)
	if z64 {
		b = le32(b, zip64Marker)
		b = le32(b, zip64Marker)
	} else {
		b = le32(b, uint32(cdSize))
		b = le32(b, uint32(cdOff))
	}
	b = le16(b, 0)
	return b
}

func buildZipLayout(entries []zipEntry) *zipLayout {
	l := &zipLayout{}
	localOffs := make([]int64, len(entries))
	var off int64
	for i, e := range entries {
		localOffs[i] = off
		h := localHeader(e)
		l.segs = append(l.segs, zipSeg{off: off, size: int64(len(h)), hdr: h})
		off += int64(len(h))
		l.segs = append(l.segs, zipSeg{off: off, size: e.size, fileIdx: i})
		off += e.size
	}
	cdOff := off
	var cd []byte
	for i, e := range entries {
		cd = append(cd, centralHeader(e, localOffs[i])...)
	}
	trailer := append(cd, endRecords(len(entries), int64(len(cd)), cdOff)...)
	l.segs = append(l.segs, zipSeg{off: cdOff, size: int64(len(trailer)), hdr: trailer})
	l.total = cdOff + int64(len(trailer))
	return l
}

func (l *zipLayout) writeRange(w io.Writer, start, end int64, fetch func(idx int, off, length int64) (io.ReadCloser, error)) error {
	for _, s := range l.segs {
		segEnd := s.off + s.size - 1
		if segEnd < start || s.off > end {
			continue
		}
		a := max(start, s.off)
		b := min(end, segEnd)
		if s.hdr != nil {
			if _, err := w.Write(s.hdr[a-s.off : b-s.off+1]); err != nil {
				return err
			}
			continue
		}
		rc, err := fetch(s.fileIdx, a-s.off, b-a+1)
		if err != nil {
			return err
		}
		_, err = streamCopy(w, rc)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
