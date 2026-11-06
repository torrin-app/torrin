package rarfs

func walkRar5(v Volume, volIndex int, byName map[string]*Entry, order *[]string) error {
	size, err := v.Size()
	if err != nil {
		return err
	}
	for pos := int64(8); pos < size; {
		buf, err := readSlice(v, pos, 4096)
		if err != nil {
			return nil
		}
		p := 4
		hsize, p, ok := uvint(buf, p)
		if !ok || hsize == 0 {
			return nil
		}
		hdrStart := p
		htype, p, ok := uvint(buf, p)
		if !ok {
			return nil
		}
		hflags, p, ok := uvint(buf, p)
		if !ok {
			return nil
		}
		if hflags&0x1 != 0 {
			_, p, _ = uvint(buf, p)
		}
		var dataSize uint64
		if hflags&0x2 != 0 {
			dataSize, p, _ = uvint(buf, p)
		}
		dataStart := pos + int64(hdrStart) + int64(hsize)
		next := dataStart + int64(dataSize)
		if htype == 5 {
			return nil
		}
		if htype == 2 && dataSize > 0 {
			parseRar5File(v, volIndex, buf, p, pos, dataStart, int64(dataSize), byName, order)
		}
		if next <= pos {
			return nil
		}
		pos = next
	}
	return nil
}

func parseRar5File(v Volume, volIndex int, buf []byte, p int, blockPos, dataStart, dataSize int64, byName map[string]*Entry, order *[]string) {
	fflags, p, ok := uvint(buf, p)
	if !ok {
		return
	}
	unpSize, p, ok := uvint(buf, p)
	if !ok {
		return
	}
	if _, p, ok = uvint(buf, p); !ok {
		return
	}
	if fflags&0x2 != 0 {
		p += 4
	}
	if fflags&0x4 != 0 {
		p += 4
	}
	compInfo, p, ok := uvint(buf, p)
	if !ok {
		return
	}
	if _, p, ok = uvint(buf, p); !ok {
		return
	}
	nameLen, p, ok := uvint(buf, p)
	if !ok {
		return
	}
	if fflags&0x1 != 0 {
		return
	}
	name := readName(v, buf, p, blockPos, int(nameLen))
	if name == "" {
		return
	}
	method := (compInfo >> 7) & 0x7
	solid := (compInfo >> 6) & 0x1
	addFrag(byName, order, name, int64(unpSize), method == 0 && solid == 0,
		frag{vol: volIndex, off: dataStart, length: dataSize})
}

func readName(v Volume, buf []byte, p int, blockPos int64, nameLen int) string {
	if nameLen <= 0 || nameLen > 4096 {
		return ""
	}
	if p+nameLen <= len(buf) {
		return string(buf[p : p+nameLen])
	}
	nb, err := readSlice(v, blockPos+int64(p), nameLen)
	if err != nil || len(nb) < nameLen {
		return ""
	}
	return string(nb[:nameLen])
}
