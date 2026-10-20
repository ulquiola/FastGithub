package mitm

import "testing"

func TestBuildPAC(t *testing.T) {
	got := string(BuildPAC([]string{"github.com", "*.githubusercontent.com"}, "127.0.0.1:38457"))
	want := "function FindProxyForURL(url, host){\n" +
		"    var fastgithub = 'PROXY 127.0.0.1:38457';\n" +
		"    if (shExpMatch(host, 'github.com')) return fastgithub;\n" +
		"    if (shExpMatch(host, '*.githubusercontent.com')) return fastgithub;\n" +
		"    return 'DIRECT';\n" +
		"}\n"
	if got != want {
		t.Fatalf("PAC =\n%s\nwant =\n%s", got, want)
	}
}
