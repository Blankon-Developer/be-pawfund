package cache

import "testing"

func TestCacheClientBuildKey(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		key       string
		want      string
		wantError bool
	}{
		{name: "adds prefix", prefix: "pawfund", key: " siwe:message:nonce ", want: "pawfund:siwe:message:nonce"},
		{name: "supports empty prefix", key: "key", want: "key"},
		{name: "rejects empty key", prefix: "pawfund", key: " ", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &CacheClient{keyPrefix: test.prefix}
			key, err := client.buildKey(test.key)
			if test.wantError {
				if err == nil {
					t.Fatal("buildKey() expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("buildKey() unexpected error: %v", err)
			}
			if key != test.want {
				t.Errorf("key = %q, want %q", key, test.want)
			}
		})
	}
}
