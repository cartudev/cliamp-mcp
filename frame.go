package main

import (
	"bufio"
	"bytes"
	"io"
)

// readFrame reads one NDJSON line and drops embedded data: URLs while it
// reads. cliamp answers queue.play with up to 200 covers, which does not fit
// in an IPC frame the model can use. The covers are replaced with "omitted"
// before the line is parsed.
func readFrame(r io.Reader) ([]byte, error) {
	br := bufio.NewReaderSize(r, 64*1024)
	var out bytes.Buffer
	inString := false
	escape := false
	dropping := false
	var prefix []byte
	for {
		b, err := br.ReadByte()
		if err != nil {
			if err == io.EOF && out.Len() > 0 {
				return out.Bytes(), nil
			}
			return nil, err
		}
		if !inString {
			if b == '\n' {
				return out.Bytes(), nil
			}
			out.WriteByte(b)
			if b == '"' {
				inString = true
				escape = false
				dropping = false
				prefix = prefix[:0]
			}
			continue
		}
		if escape {
			escape = false
			if !dropping {
				if len(prefix) > 0 {
					out.Write(prefix)
					prefix = prefix[:0]
				}
				out.WriteByte('\\')
				out.WriteByte(b)
			}
			continue
		}
		if b == '\\' {
			escape = true
			continue
		}
		if b == '"' {
			if dropping {
				out.WriteString("omitted")
			} else {
				out.Write(prefix)
			}
			out.WriteByte('"')
			inString = false
			prefix = prefix[:0]
			dropping = false
			continue
		}
		if dropping {
			continue
		}
		if len(prefix) < len("data:") {
			prefix = append(prefix, b)
			if string(prefix) == "data:" {
				dropping = true
				prefix = prefix[:0]
			}
			continue
		}
		out.Write(prefix)
		prefix = prefix[:0]
		out.WriteByte(b)
	}
}
