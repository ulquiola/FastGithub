package config

import "testing"

func TestJoinDestination(t *testing.T) {
	cases := []struct {
		scheme, host, dest, want string
	}{
		{"https", "github.com", "", "https://github.com/"},
		{"https", "fonts.googleapis.com", "https://fonts.geekzu.org/", "https://fonts.geekzu.org/"},
		{"https", "github.com", "/mirror/", "https://github.com/mirror/"},
		{"http", "github.com", "", "http://github.com/"},
	}
	for _, c := range cases {
		u, err := JoinDestination(c.scheme, c.host, c.dest)
		if err != nil {
			t.Fatalf("JoinDestination(%q,%q,%q): %v", c.scheme, c.host, c.dest, err)
		}
		if u.String() != c.want {
			t.Fatalf("got %q，期望 %q", u.String(), c.want)
		}
	}
}

func TestJoinDestinationInvalid(t *testing.T) {
	if _, err := JoinDestination("https", "github.com", "://bad"); err == nil {
		t.Fatal("非法 destination 应报错")
	}
}
