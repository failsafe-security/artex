package db

import (
	"net"
	"testing"
)

func TestMentionsToken(t *testing.T) {
	cases := []struct {
		text, token string
		want        bool
	}{
		{"Test https://app.example.com/login only", "app.example.com", true},
		{"Test https://app.example.com/login only", "example.com", true}, // parent of a named host
		{"Test APP.Example.COM", "app.example.com", true},
		{"scope: example.com, and nothing else", "example.com", true},
		{"see notexample.com", "example.com", false},
		{"see example.com.evil.test", "example.com", false},
		{"see app.example.community", "app.example.com", false},
		{"see my-example.com", "example.com", false},
		{"target 10.0.0.1:8080", "10.0.0.1", true},
		{"target http://10.0.0.1/", "10.0.0.1", true},
		{"target 110.0.0.1", "10.0.0.1", false},
		{"target 10.0.0.12", "10.0.0.1", false},
		{"target 10.0.0.1.5", "10.0.0.1", false},
		{"target 1.10.0.0.1", "10.0.0.1", false},
		{"target [2001:db8::1]:443", "2001:db8::1", true},
		{"target 2001:db8::1:5", "2001:db8::1", false},
		{"", "example.com", false},
		{"anything", "", false},
	}
	for _, c := range cases {
		if got := mentionsToken(c.text, c.token); got != c.want {
			t.Errorf("mentionsToken(%q, %q) = %v, want %v", c.text, c.token, got, c.want)
		}
	}
}

func TestMentionsNetwork(t *testing.T) {
	_, network, err := net.ParseCIDR("10.20.30.0/24")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		text string
		want bool
	}{
		{"scan 10.20.30.0/24 only", true},
		{"scan 10.20.30.77/24 only", true}, // same network, written with a host address
		{"scan 10.20.31.0/24 only", false},
		{"scan 10.20.0.0/16 only", false},
		{"scan 10.20.30.5", false},
		{"no network here", false},
	}
	for _, c := range cases {
		if got := mentionsNetwork(c.text, network); got != c.want {
			t.Errorf("mentionsNetwork(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}
