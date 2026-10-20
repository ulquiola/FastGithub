package config

import (
	"net/url"
)

// JoinDestination 把请求的 scheme/host 与配置的 destination 合并为上游前缀。
// 语义等价 .NET 的 new Uri(baseUri, destination)。
func JoinDestination(scheme, host, destination string) (*url.URL, error) {
	base, err := url.Parse(scheme + "://" + host + "/")
	if err != nil {
		return nil, err
	}
	if destination == "" {
		return base, nil
	}
	ref, err := url.Parse(destination)
	if err != nil {
		return nil, err
	}
	if ref.IsAbs() {
		return ref, nil
	}
	return base.ResolveReference(ref), nil
}

// DestinationPrefix 是 JoinDestination 的方法形式。
func (d *Domain) DestinationPrefix(scheme, host string) (*url.URL, error) {
	return JoinDestination(scheme, host, d.Destination)
}
