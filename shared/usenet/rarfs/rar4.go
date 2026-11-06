package rarfs

import "encoding/binary"

func walkRar4(v Volume, volIndex int, byName map[string]*Entry, order *[]string) error {
	size, err := v.Size()
	if err != nil {
		return err
	}
	for pos := int64(0); pos+11 <= size; {
		buf, err := readSlice(v, pos, 4096)
		if err != nil || len(buf) < 11 {
			return nil
		}
		htype := buf[2]
		flags := binary.LittleEndian.Uint16(buf[3:5])
		headSize := int64(binary.LittleEndian.Uint16(buf[5:7]))
		if headSize < 7 {
			return nil
		}
		var dataSize int64
		if flags&0x8000 != 0 {
			dataSize = int64(binary.LittleEndian.Uint32(buf[7:11]))
		}
		if htype == 0x74 {
			parseRar4File(v, volIndex, buf, flags, pos, headSize, &dataSize, byName, order)
		}
		next := pos + headSize + dataSize
		if next <= pos {
			return nil
		}
		pos = next
	}
	return nil
}

func parseRar4File(v Volume, volIndex int, buf []byte, flags uint16, blockPos, headSize int64, dataSize *int64, byName map[string]*Entry, order *[]string) {
	if len(buf) < 32 {
		return
	}
	unpSize := int64(binary.LittleEndian.Uint32(buf[11:15]))
	method := buf[25]
	nameSize := int(binary.LittleEndian.Uint16(buf[26:28]))
	nameOff := 32
	if flags&0x100 != 0 {
		if len(buf) < 40 {
			return
		}
		*dataSize |= int64(binary.LittleEndian.Uint32(buf[32:36])) << 32
		unpSize |= int64(binary.LittleEndian.Uint32(buf[36:40])) << 32
		nameOff = 40
	}
	if flags&0x00e0 == 0x00e0 {
		return
	}
	name := readName(v, buf, nameOff, blockPos, nameSize)
	if name == "" {
		return
	}
	addFrag(byName, order, name, unpSize, method == 0x30 && flags&0x04 == 0,
		frag{vol: volIndex, off: blockPos + headSize, length: *dataSize})
}
