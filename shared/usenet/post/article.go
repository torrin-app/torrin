package post

import (
	"bytes"
	"fmt"
	"hash/crc32"

	"github.com/madcowfred/yencode"
)

const partSize = 768000

type articleMeta struct {
	From, Group, MessageID, Date, Subject, Name string
	FileNum, FileTotal                          int
	PartNum, PartTotal                          int64
	FileSize, Begin                             int64
}

func buildArticle(a articleMeta, chunk []byte) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", a.From)
	fmt.Fprintf(&b, "Newsgroups: %s\r\n", a.Group)
	fmt.Fprintf(&b, "Date: %s\r\n", a.Date)
	fmt.Fprintf(&b, "Message-ID: %s\r\n", a.MessageID)
	fmt.Fprintf(&b, "Subject: %s [%d/%d] - \"%s\" yEnc (%d/%d)\r\n\r\n",
		a.Subject, a.FileNum, a.FileTotal, a.Name, a.PartNum, a.PartTotal)
	fmt.Fprintf(&b, "=ybegin part=%d total=%d line=128 size=%d name=%s\r\n",
		a.PartNum, a.PartTotal, a.FileSize, a.Name)
	fmt.Fprintf(&b, "=ypart begin=%d end=%d\r\n", a.Begin+1, a.Begin+int64(len(chunk)))
	yencode.Encode(chunk, &b)
	h := crc32.NewIEEE()
	h.Write(chunk)
	fmt.Fprintf(&b, "=yend size=%d part=%d pcrc32=%08X\r\n", len(chunk), a.PartNum, h.Sum32())
	return bytes.ReplaceAll(b.Bytes(), []byte("\r\n"), []byte("\n"))
}
