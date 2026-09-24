package store

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/y3owk1n/oku/internal/infer"
)

// programMagic starts a program that an OS runs: ELF, Mach-O, a Mach-O file
// for several archs, and a Windows executable.
var programMagic = [][]byte{
	[]byte("\x7fELF"),
	{0xfe, 0xed, 0xfa, 0xce},
	{0xfe, 0xed, 0xfa, 0xcf},
	{0xce, 0xfa, 0xed, 0xfe},
	{0xcf, 0xfa, 0xed, 0xfe},
	{0xca, 0xfe, 0xba, 0xbe},
	{0xca, 0xfe, 0xba, 0xbf},
	[]byte("MZ"),
	[]byte("#!"),
}

// checkSingleFile fails when head, the start of a download that is no archive,
// cannot be the program named name. A web page never is. Unless wrapped, which
// lets a runtime run it, the file must start like a program, or be a Windows
// script, which is text.
func checkSingleFile(head []byte, name string, wrapped bool) error {
	if isWebPage(head) {
		return fmt.Errorf("the download is %w, not a program", infer.ErrWebPage)
	}

	if wrapped {
		return nil
	}

	for _, magic := range programMagic {
		if bytes.HasPrefix(head, magic) {
			return nil
		}
	}

	switch strings.ToLower(path.Ext(name)) {
	case ".cmd", ".bat", ".ps1":
		return nil
	}

	return errors.New(
		"the download is no archive and no program, since it does not start as a program for " +
			"Linux, macOS or Windows or as a #! script",
	)
}

// isWebPage reports whether head starts an HTML page.
func isWebPage(head []byte) bool {
	text := bytes.ToLower(bytes.TrimLeft(bytes.TrimPrefix(head, []byte("\xef\xbb\xbf")), " \t\r\n"))

	return bytes.HasPrefix(text, []byte("<!doctype html")) || bytes.HasPrefix(text, []byte("<html"))
}

// checkHead runs checkSingleFile on the download at file, a program that no
// runtime wraps.
func checkHead(file, name string) error {
	in, err := os.Open(file)
	if err != nil {
		return err
	}
	defer in.Close()

	data, err := decompress(in)
	if err != nil {
		return err
	}

	head, err := bufio.NewReader(data).Peek(512)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("decompress: %w", err)
	}

	return checkSingleFile(head, name, false)
}

// nativeProgram reports whether the file at path starts as a program of Linux,
// macOS or Windows, and not as a script.
func nativeProgram(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	head := make([]byte, 4)
	if n, _ := io.ReadFull(f, head); n < 2 {
		return false
	}

	for _, magic := range programMagic {
		if string(magic) != "#!" && bytes.HasPrefix(head, magic) {
			return true
		}
	}

	return false
}
