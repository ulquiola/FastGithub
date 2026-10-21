package resolver

import (
	"net"
	"testing"
)

func BenchmarkParseAnswer(b *testing.B) {
	resp, err := buildResponse(0x1234, "github.com", typeA, []testRecord{{
		name: "github.com", typ: typeA, ttl: 60, ip: net.ParseIP("140.82.112.3"),
	}}, 0)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := parseAnswer(resp, 0x1234, "github.com", typeA); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildQuery(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := buildQuery("api.github.com", typeA); err != nil {
			b.Fatal(err)
		}
	}
}
