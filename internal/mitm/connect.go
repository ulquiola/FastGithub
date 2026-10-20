package mitm

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)

const maxConnectHeader = 8 * 1024

// ReadConnect 读取 CONNECT 请求行与请求头，返回目标主机与端口。
func ReadConnect(br *bufio.Reader) (string, int, error) {
	total := 0
	line, err := readHeaderLine(br, &total)
	if err != nil {
		return "", 0, err
	}
	fields := strings.Fields(line)
	if len(fields) != 3 || fields[0] != "CONNECT" || !strings.HasPrefix(fields[2], "HTTP/1.") {
		return "", 0, fmt.Errorf("无效的 CONNECT 请求行: %q", line)
	}
	host, portText, err := net.SplitHostPort(fields[1])
	if err != nil {
		return "", 0, fmt.Errorf("无效的 CONNECT 目标: %q", fields[1])
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("无效的 CONNECT 端口: %q", portText)
	}
	for {
		header, err := readHeaderLine(br, &total)
		if err != nil {
			return "", 0, err
		}
		if header == "" {
			break
		}
	}
	return host, port, nil
}

func readHeaderLine(br *bufio.Reader, total *int) (string, error) {
	line, err := br.ReadSlice('\n')
	*total += len(line)
	if *total > maxConnectHeader {
		return "", fmt.Errorf("CONNECT 请求头超过 %d 字节", maxConnectHeader)
	}
	if err == bufio.ErrBufferFull {
		return "", fmt.Errorf("CONNECT 请求行过长")
	}
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(line), "\r\n"), nil
}

// WriteConnectEstablished 返回 200，客户端随后开始 TLS 握手或隧道数据。
func WriteConnectEstablished(w io.Writer) error {
	_, err := io.WriteString(w, "HTTP/1.1 200 Connection Established\r\n\r\n")
	return err
}

// WriteConnectError 返回不带正文的错误响应。
func WriteConnectError(w io.Writer, status int) error {
	text := "Bad Request"
	if status == 502 {
		text = "Bad Gateway"
	}
	_, err := fmt.Fprintf(w, "HTTP/1.1 %d %s\r\nContent-Length: 0\r\n\r\n", status, text)
	return err
}
