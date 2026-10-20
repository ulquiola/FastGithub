package resolver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
)

type dohClient struct {
	endpoints []string
	client    *http.Client
}

// query 用 RFC 8484 POST 方式请求一个 DoH 端点。
func (c *dohClient) query(ctx context.Context, endpoint string, msg []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(msg))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 65535))
	if err != nil {
		return nil, err
	}
	if len(body) < 12 {
		return nil, fmt.Errorf("DNS 响应过短")
	}
	return body, nil
}
