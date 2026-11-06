package execution

import "testing"

func TestHostAllowed(t *testing.T) {
	tests := []struct {
		host    string
		allowed []string
		want    bool
	}{
		{host: "api.example.com", allowed: []string{"api.example.com"}, want: true},
		{host: "api.example.com", allowed: []string{"*.example.com"}, want: true},
		{host: "example.com", allowed: []string{"*.example.com"}, want: false},
		{host: "evil-example.com", allowed: []string{"*.example.com"}, want: false},
		{host: "api.example.com", allowed: []string{"other.example.com"}, want: false},
	}

	for _, test := range tests {
		if got := HostAllowed(test.host, test.allowed); got != test.want {
			t.Errorf("HostAllowed(%q, %v) = %v, want %v", test.host, test.allowed, got, test.want)
		}
	}
}
