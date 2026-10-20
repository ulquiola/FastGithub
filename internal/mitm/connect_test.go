package mitm

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

func TestReadConnect(t *testing.T) {
	br := bufio.NewReader(strings.NewReader("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\nProxy-Connection: keep-alive\r\n\r\n"))
	host, port, err := ReadConnect(br)
	if err != nil {
		t.Fatalf("ReadConnect: %v", err)
	}
	if host != "example.com" || port != 443 {
		t.Fatalf("host=%q port=%d", host, port)
	}
}

func TestReadConnectRejectsBadInput(t *testing.T) {
	cases := []string{
		"GET / HTTP/1.1\r\n\r\n",
		"CONNECT example.com HTTP/1.1\r\n\r\n",
		"CONNECT example.com:0 HTTP/1.1\r\n\r\n",
		"CONNECT example.com:443\r\n\r\n",
	}
	for _, input := range cases {
		if _, _, err := ReadConnect(bufio.NewReader(strings.NewReader(input))); err == nil {
			t.Fatalf("ReadConnect(%q) 应报错", input)
		}
	}
}

func TestReadConnectHeaderLimit(t *testing.T) {
	input := "CONNECT example.com:443 HTTP/1.1\r\n" + strings.Repeat("X-Pad: a\r\n", 1000) + "\r\n"
	if _, _, err := ReadConnect(bufio.NewReader(strings.NewReader(input))); err == nil || !strings.Contains(err.Error(), "超过") {
		t.Fatalf("err = %v，期望超限错误", err)
	}
}

func TestWriteConnectResponses(t *testing.T) {
	var ok strings.Builder
	if err := WriteConnectEstablished(&ok); err != nil || !strings.Contains(ok.String(), "200 Connection Established") {
		t.Fatalf("ok = %q err=%v", ok.String(), err)
	}
	var bad strings.Builder
	if err := WriteConnectError(&bad, 502); err != nil || !strings.Contains(bad.String(), "502 Bad Gateway") {
		t.Fatalf("bad = %q err=%v", bad.String(), err)
	}
}

func TestReadConnectRejectsLongSingleLine(t *testing.T) {
	input := "CONNECT example.com:443 HTTP/1.1\r\n" + strings.Repeat("A", 1<<20)
	br := bufio.NewReaderSize(strings.NewReader(input), maxConnectHeader+1)
	if _, _, err := ReadConnect(br); err == nil {
		t.Fatal("超长单行应被有界读取拒绝")
	}
}

type countingSource struct {
	r io.Reader
	n int
}

func (c *countingSource) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func TestReadConnectBoundsSingleLineRead(t *testing.T) {
	src := &countingSource{r: strings.NewReader("CONNECT example.com:443 HTTP/1.1\r\n" + strings.Repeat("A", 1<<20))}
	br := bufio.NewReaderSize(src, maxConnectHeader+1)
	if _, _, err := ReadConnect(br); err == nil {
		t.Fatal("超长单行应被拒绝")
	}
	if src.n > 2*maxConnectHeader {
		t.Fatalf("读取了 %d 字节，期望有界在 %d 附近", src.n, maxConnectHeader)
	}
}
