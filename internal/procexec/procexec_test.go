package procexec

import (
	"testing"
	"unicode/utf16"
)

func utf16le(s string) []byte {
	var b []byte
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u), byte(u>>8))
	}
	return b
}

func TestDecode(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"utf8 crlf", []byte("Debian\r\nUbuntu\r\n"), "Debian\nUbuntu\n"},
		{"utf8 bom", append([]byte{0xEF, 0xBB, 0xBF}, "a\n"...), "a\n"},
		{"utf16le no bom", utf16le("Debian\r\n"), "Debian\n"},
		{"utf16le bom", append([]byte{0xFF, 0xFE}, utf16le("x")...), "x"},
		{"utf16le chinese", utf16le("WSL 版本: 3.0\r\n"), "WSL 版本: 3.0\n"},
		{"utf8 chinese even length", []byte("版本"), "版本"},
		{"empty", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Decode(tt.in); got != tt.want {
				t.Errorf("Decode = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTailBufferKeepsEnd(t *testing.T) {
	tb := &tailBuffer{max: 4}
	tb.Write([]byte("abc"))
	tb.Write([]byte("defg"))
	if got := string(tb.Bytes()); got != "defg" {
		t.Fatalf("tail = %q", got)
	}
}
