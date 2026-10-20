package mitm

// IsTLS 判断连接首字节是否为 TLS 记录（0x16 0x03）。
func IsTLS(b []byte) bool {
	return len(b) >= 2 && b[0] == 0x16 && b[1] == 0x03
}
